package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/lukeroth/gdal"
	"github.com/wdantuma/s57-tiler/s57"
	"github.com/wdantuma/s57-tiler/s57/dataset"
	m "github.com/wdantuma/s57-tiler/s57/mercantile"
)

func main() {

	driver, err := gdal.GetDriverByName("S57")
	if err != nil {
		log.Fatal(err)
	}
	driver.Register()

	// GDAL S-57 reader options (incl. SOUNDG handling) are configured in the dataset
	// package's init, so they apply consistently to the CLI and tests.

	outputPath := flag.String("out", "./static/charts", "Output directory for vector tiles")
	inputPath := flag.String("in", "./charts", "Input path S-57 ENC's")
	minzoom := flag.Int("minzoom", 9, "Min zoom")
	maxzoom := flag.Int("maxzoom", 14, "Max zoom")
	boundsFlag := flag.String("bounds", "", "W,N,E,S")
	debug := flag.Bool("debug", false, "Show debug info")
	at := flag.String("at", "", "lon,lat")
	workers := flag.Int("workers", runtime.NumCPU()-1, "Number of parallel tile workers") // keep one CPU available for system responsiveness
	dryRun := flag.Bool("dry-run", false, "Scan and report the tile count, then exit without writing any tiles")
	maxTiles := flag.Int("max-tiles", 0, "Abort before tiling if the run would exceed this many tiles (0 = no limit)")
	flag.Parse()

	if *workers < 1 {
		*workers = 1
	}

	// The scale table (mercantile.Scale) is defined for z0..z23; beyond it the
	// SCAMIN/SCAMAX filtering degrades, so clamp explicit flags into range.
	applyClamp := func(name string, zp *int) {
		v, note := clampZoomValue(name, *zp)
		*zp = v
		if note != "" {
			fmt.Println(note)
		}
	}
	applyClamp("minzoom", minzoom)
	applyClamp("maxzoom", maxzoom)
	if err := validateZoomRange(*minzoom, *maxzoom); err != nil {
		log.Fatal(err)
	}

	// Fail fast if the output directory can't be created/written, before the
	// (possibly long) scan and tiling. Skipped for a dry run, which writes nothing.
	if !*dryRun {
		if err := ensureWritableDir(*outputPath); err != nil {
			log.Fatal(err)
		}
	}

	// Honor explicit -minzoom/-maxzoom; otherwise the zoom range is derived per cell
	// from its S-57 usage band (overview→berthing) in the pre-pass below.
	userSetZoom := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "minzoom" || f.Name == "maxzoom" {
			userSetZoom = true
		}
	})

	if !*debug {
		os.Setenv("CPL_LOG", os.DevNull) // supress gdal errors
	}

	datasets, err := dataset.GetS57Datasets(*inputPath)
	if err != nil {
		log.Fatal(err)
	}
	if len(datasets) == 0 {
		fmt.Println("No datasets found")
		return
	}

	if *at != "" && *boundsFlag != "" {
		log.Fatal("at and bounds cannot be used together")
	}

	var bounds *m.Extrema = nil
	if *boundsFlag != "" {
		b, err := parseBounds(*boundsFlag)
		if err != nil {
			log.Fatal(err)
		}
		bounds = &b
	}

	var tile *m.TileID = nil
	if *at != "" {
		tile = &m.TileID{}
		parts := strings.Split(*at, ",")
		if len(parts) != 2 {
			log.Fatal("Invalid at")
		}
		x, xerr := strconv.ParseFloat(parts[0], 64)
		y, yerr := strconv.ParseFloat(parts[1], 64)
		if xerr == nil && yerr == nil {
			t := m.Tile(x, y, *minzoom)
			tile = &t
		} else {
			log.Fatal("Invalid at")
		}
	}

	// Cancel cleanly on Ctrl-C / SIGTERM so an interrupted run stops feeding work
	// instead of being killed mid-write.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	tiler := s57.NewS57Tiler(datasets)
	defer tiler.Close() // release its SRS/transform handles on normal exit

	// Pre-pass: per file, derive its zoom range and cache its layer extents (one
	// full scan), then count the tiles for a single global total + ETA. The tile
	// IDs themselves are NOT materialized — they are streamed per file during
	// generation (recomputing them from the cached extents is cheap arithmetic), so
	// a multi-million-tile run doesn't hold the whole set in memory.
	fmt.Println("Scanning…")
	var work []fileWork
	var grandTotal int64
	var reports []zoomReport
	for _, ds := range datasets {
		for _, file := range ds.Files {
			fw := fileWork{dataset: ds, file: file, minzoom: *minzoom, maxzoom: *maxzoom, single: tile}
			if tile == nil {
				zr := dataset.CellZoomRange(file)
				if !userSetZoom && bounds == nil {
					fw.minzoom, fw.maxzoom = zr.Min, zr.Max
				}
				reports = append(reports, zoomReport{
					convMin:  fw.minzoom,
					convMax:  fw.maxzoom,
					availMin: zr.Min,
					availMax: zr.Max,
				})
				if bounds != nil {
					fw.extents = []m.Extrema{*bounds}
				} else {
					// The full-scan layer extents are the same for every zoom, so
					// compute them once per file instead of reopening the cell per zoom.
					fw.extents = tiler.FileExtents(file)
				}
			}
			work = append(work, fw)
			grandTotal += fw.tileCount()
		}
	}

	if lines, hint := summarizeZooms(reports); len(lines) > 0 {
		fmt.Println("Zoom levels:")
		for _, l := range lines {
			fmt.Println(l)
		}
		if hint != "" {
			fmt.Println(hint)
		}
	}
	// Print the grand total up front: a native-zoom run over large-scale charts
	// can be very large, so surface the count before the (possibly long) tiling.
	fmt.Printf("Generating %d tiles\n", grandTotal)

	// A dry run reports the plan (zoom ranges + tile count) and stops before writing,
	// so a wrong dataset/zoom is caught without committing to a multi-hour run.
	if *dryRun {
		fmt.Println("Dry run: no tiles written.")
		return
	}

	// Guardrail against an accidental enormous run (e.g. a wrong dataset or zoom).
	if *maxTiles > 0 && grandTotal > int64(*maxTiles) {
		log.Fatalf("would generate %d tiles, exceeding -max-tiles %d; raise the limit or cap the zoom with -maxzoom", grandTotal, *maxTiles)
	}

	// Track write failures across all workers. A transient error (e.g. disk full,
	// EPERM) no longer aborts the whole multi-hour run via log.Fatal; instead we
	// keep going, surface the first error, count the rest, and exit non-zero at the
	// end so a failed run is never mistaken for a complete one.
	var failCount int64
	var failOnce sync.Once
	recordFailure := func(what string, err error) {
		atomic.AddInt64(&failCount, 1)
		failOnce.Do(func() {
			fmt.Fprintf(os.Stderr, "\nerror: %s: %v\n", what, err)
		})
	}

	prog := newProgress(grandTotal, os.Stdout)
	go prog.run()
	for _, fw := range work {
		if ctx.Err() != nil {
			break
		}
		prog.setStage(fw.file.Id)

		// One worker pool per file: each worker's tiler opens this cell's datasource
		// once and reuses it across every tile and zoom of the file, instead of the
		// old per-(file,zoom) pool that reopened the cell for every zoom level.
		jobs := make(chan m.TileID, *workers*2)
		var wg sync.WaitGroup
		for w := 0; w < *workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Per-worker tiler: GenerateTile mutates per-call state
				// (keysMap/values/lastx/lasty) and caches an open datasource, so
				// instances cannot be shared across goroutines.
				workerTiler := s57.NewS57Tiler(datasets)
				defer workerTiler.Close()
				for t := range jobs {
					if err := workerTiler.GenerateTile(*outputPath, fw.file, t); err != nil {
						recordFailure(fmt.Sprintf("tile %s z%d %d/%d", fw.file.Id, t.Z, t.X, t.Y), err)
					}
					prog.inc()
				}
			}()
		}
		fw.streamTiles(ctx, jobs)
		close(jobs)
		wg.Wait()
		if err := tiler.GenerateMetaData(*outputPath, fw.dataset, fw.file, fw.minzoom, fw.maxzoom); err != nil {
			recordFailure("metadata "+fw.file.Id, err)
		}
	}
	prog.finish()

	if ctx.Err() != nil {
		fmt.Fprintln(os.Stderr, "Interrupted; output is incomplete.")
		os.Exit(130)
	}
	if n := atomic.LoadInt64(&failCount); n > 0 {
		fmt.Fprintf(os.Stderr, "Completed with %d write failure(s); output is incomplete.\n", n)
		os.Exit(1)
	}
}

// fileWork describes the tiling for one cell: its converting zoom range and the
// cached layer extents the tiles are derived from. Tiles are streamed (recomputed
// from the extents) rather than materialized, so the global total can be counted
// without holding millions of TileIDs in memory.
type fileWork struct {
	dataset dataset.Dataset
	file    dataset.File
	minzoom int
	maxzoom int
	extents []m.Extrema // file/bounds mode; nil in -at mode
	single  *m.TileID   // -at mode: the one tile to generate
}

// tileCount returns how many tiles this file contributes to the run.
func (fw fileWork) tileCount() int64 {
	if fw.single != nil {
		return 1
	}
	var n int64
	for z := fw.minzoom; z <= fw.maxzoom; z++ {
		n += int64(len(s57.TilesForExtents(fw.extents, z)))
	}
	return n
}

// streamTiles feeds this file's tiles (all zooms) into jobs, stopping early if the
// context is cancelled. Recomputed from the cached extents — no full set is held.
func (fw fileWork) streamTiles(ctx context.Context, jobs chan<- m.TileID) {
	send := func(t m.TileID) bool {
		select {
		case jobs <- t:
			return true
		case <-ctx.Done():
			return false
		}
	}
	if fw.single != nil {
		send(*fw.single)
		return
	}
	for z := fw.minzoom; z <= fw.maxzoom; z++ {
		for _, t := range s57.TilesForExtents(fw.extents, z) {
			if !send(t) {
				return
			}
		}
	}
}
