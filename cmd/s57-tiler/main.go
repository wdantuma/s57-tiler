package main

import (
	"flag"
	"fmt"
	"github.com/lukeroth/gdal"
	"github.com/wdantuma/s57-tiler/s57"
	"github.com/wdantuma/s57-tiler/s57/dataset"
	m "github.com/wdantuma/s57-tiler/s57/mercantile"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
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
	flag.Parse()

	if *workers < 1 {
		*workers = 1
	}

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
		bounds = &m.Extrema{}
		parts := strings.Split(*boundsFlag, ",")
		if len(parts) != 4 {
			log.Fatal("Invalid bounds")
		}
		for i, p := range parts {
			if v, err := strconv.ParseFloat(p, 64); err == nil {
				switch i {
				case 0:
					bounds.W = v
				case 1:
					bounds.N = v
				case 2:
					bounds.E = v
				case 3:
					bounds.S = v
				}
			} else {
				log.Fatal("Invalid bounds")
			}
		}
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

	tiler := s57.NewS57Tiler(datasets, *minzoom, *maxzoom)

	// Pre-pass: compute the tile set for every (dataset, file, zoom) up front so
	// progress can be reported against a single global total with an ETA. Tile IDs
	// are tiny, so materializing them all is cheap.
	fmt.Println("Scanning…")
	var work []workUnit
	var grandTotal int64
	for _, dataset := range datasets {
		for _, file := range dataset.Files {
			for z := *minzoom; z <= *maxzoom; z++ {
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
				work = append(work, workUnit{dataset: dataset, file: file, z: z, tiles: ids})
				grandTotal += int64(len(ids))
			}
		}
	}

	prog := newProgress(grandTotal, os.Stdout)
	go prog.run()
	for _, wu := range work {
		prog.setStage(fmt.Sprintf("%s, Zoom: %d", wu.file.Id, wu.z))

		jobs := make(chan m.TileID, *workers*2)
		var wg sync.WaitGroup
		for w := 0; w < *workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// Per-worker tiler: GenerateTile mutates per-call state
				// (keysMap/values/lastx/lasty) so instances cannot be shared.
				workerTiler := s57.NewS57Tiler(datasets, *minzoom, *maxzoom)
				for tile := range jobs {
					workerTiler.GenerateTile(*outputPath, wu.file, tile)
					prog.inc()
				}
			}()
		}
		for _, t := range wu.tiles {
			jobs <- t
		}
		close(jobs)
		wg.Wait()
		tiler.GenerateMetaData(*outputPath, wu.dataset, wu.file)
	}
	prog.finish()
}

// workUnit is the tile set for a single (dataset, file, zoom), materialized during
// the pre-pass so it can be both counted toward the global total and processed.
type workUnit struct {
	dataset dataset.Dataset
	file    dataset.File
	z       int
	tiles   []m.TileID
}
