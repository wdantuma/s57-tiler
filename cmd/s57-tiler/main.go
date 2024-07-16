package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

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

	// set gdal options
	os.Setenv("OGR_GEOMETRY_ACCEPT_UNCLOSED_RING", "NO")

	outputPath := flag.String("out", "./static/charts", "Output directory for vector tiles")
	inputPath := flag.String("in", "./charts", "Input path S-57 ENC's")
	minzoom := flag.Int("minzoom", 9, "Min zoom")
	maxzoom := flag.Int("maxzoom", 14, "Max zoom")
	boundsFlag := flag.String("bounds", "", "W,N,E,S")
	debug := flag.Bool("debug", false, "Show debug info")
	at := flag.String("at", "", "lon,lat")
	flag.Parse()

	if !*debug {
		os.Setenv("CPL_LOG", "/dev/null") // supress gdal errors
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

	for _, dataset := range datasets {
		for _, file := range dataset.Files {
			for z := *minzoom; z <= *maxzoom; z++ {
				var tiles map[string]m.TileID = make(map[string]m.TileID)
				if tile != nil {
					tiles = make(map[string]m.TileID)
					tiles["tile"] = *tile
				} else {
					if bounds != nil {
						tiles = tiler.GetTilesForBounds(nil, *bounds, z)
					} else {
						tiles = tiler.GetTiles(file, z)
					}
				}

				total := len(tiles)
				n := 0
				for k := range tiles {
					tiler.GenerateTile(*outputPath, file, tiles[k])
					done := float64(n) / float64(total) * 100
					fmt.Printf("\rDataset: %s, File: %s, Zoom: %d, Processed: %.0f %%    ", dataset.Id, file.Id, z, done)
					n++
				}
				fmt.Printf("\rDataset: %s, File: %s, Zoom: %d, Processed: 100 %%    \n", dataset.Id, file.Id, z)
				tiler.GenerateMetaData(*outputPath, dataset, file)
			}
		}
	}
}
