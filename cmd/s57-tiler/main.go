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

	// Pre-pass: compute the tile set for every (dataset, file, zoom) up front so
	// progress can be reported against a single global total with an ETA. Tile IDs
	// are tiny, so materializing them all is cheap.
	fmt.Println("Scanning…")
	var work []workUnit
	var grandTotal int64
	var reports []zoomReport
	for _, ds := range datasets {
		for _, file := range ds.Files {
			fminzoom, fmaxzoom := *minzoom, *maxzoom
			if tile == nil {
				zr := dataset.CellZoomRange(file)
				if !userSetZoom && bounds == nil {
					fminzoom, fmaxzoom = zr.Min, zr.Max
				}
				reports = append(reports, zoomReport{
					convMin:  fminzoom,
					convMax:  fmaxzoom,
					availMin: zr.Min,
					availMax: zr.Max,
				})
			}
			for z := fminzoom; z <= fmaxzoom; z++ {
				var tiles map[string]m.TileID
				if tile != nil {
					tiles = map[string]m.TileID{"tile": *tile}
				} else if bounds != nil {
					tiles = tiler.GetTilesForBounds(nil, *bounds, z)
				} else {
					tiles = tiler.GetTiles(file, z)
				}

				ids := make([]m.TileID, 0, len(tiles))
				for _, t := range tiles {
					ids = append(ids, t)
				}
				work = append(work, workUnit{dataset: ds, file: file, z: z, tiles: ids, minzoom: fminzoom, maxzoom: fmaxzoom})
				grandTotal += int64(len(ids))
			}
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
	for _, wu := range work {
		if ctx.Err() != nil {
			break
		}
		prog.setStage(fmt.Sprintf("%s, Zoom: %d", wu.file.Id, wu.z))

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
				for tile := range jobs {
					err := runTile(func() error { return workerTiler.GenerateTile(*outputPath, wu.file, tile) })
					if err != nil {
						recordFailure(fmt.Sprintf("tile %s z%d %d/%d", wu.file.Id, tile.Z, tile.X, tile.Y), err)
					}
					prog.inc()
				}
			}()
		}
		for _, t := range wu.tiles {
			select {
			case jobs <- t:
			case <-ctx.Done():
			}
		}
		close(jobs)
		wg.Wait()
		if err := tiler.GenerateMetaData(*outputPath, wu.dataset, wu.file, wu.minzoom, wu.maxzoom); err != nil {
			recordFailure("metadata "+wu.file.Id, err)
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

// runTile runs do, converting a panic (e.g. from a pathological geometry in the
// GDAL pipeline) into an error so a single bad tile is recorded as a failure and
// the run continues, rather than crashing the whole multi-hour job.
func runTile(do func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return do()
}

// workUnit is the tile set for a single (dataset, file, zoom), materialized during
// the pre-pass so it can be both counted toward the global total and processed.
type workUnit struct {
	dataset dataset.Dataset
	file    dataset.File
	z       int
	tiles   []m.TileID
	minzoom int
	maxzoom int
}
