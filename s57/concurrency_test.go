package s57

import (
	"os"
	"sync"
	"testing"

	"github.com/wdantuma/s57-tiler/s57/dataset"
	m "github.com/wdantuma/s57-tiler/s57/mercantile"
)

// TestConcurrentTilersNoRace validates the concurrency model the worker pool relies
// on: each goroutine owns its own tiler (and its cached, non-thread-safe datasource),
// nothing is shared. Run under the race detector:
//
//	go test -race -run TestConcurrentTilersNoRace ./s57/
func TestConcurrentTilersNoRace(t *testing.T) {
	if _, err := os.Stat(sampleENC + "/CATALOG.031"); err != nil {
		t.Skipf("sample ENC not present at %s: %v", sampleENC, err)
	}
	datasets, err := dataset.GetS57Datasets(sampleENC)
	if err != nil {
		t.Fatalf("GetS57Datasets: %v", err)
	}
	var file *dataset.File
	for di := range datasets {
		for fi := range datasets[di].Files {
			if datasets[di].Files[fi].Id == "US4WA1JJ" {
				file = &datasets[di].Files[fi]
			}
		}
	}
	if file == nil {
		t.Skip("US4WA1JJ not found in sample ENC")
	}
	mcovr, ok := file.Layers["M_COVR"]
	if !ok {
		t.Skip("fixture has no M_COVR layer")
	}
	lon := (mcovr.Bounds.MinX() + mcovr.Bounds.MaxX()) / 2
	lat := (mcovr.Bounds.MinY() + mcovr.Bounds.MaxY()) / 2
	center := m.Tile(lon, lat, 13)

	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tiler := NewS57Tiler(datasets) // each goroutine owns its tiler + cached datasource
			defer tiler.Close()
			tmp := t.TempDir()
			// Several neighbouring tiles, so the cached datasource is reused across tiles.
			for dx := int64(0); dx < 3; dx++ {
				tiler.GenerateTile(tmp, *file, m.TileID{X: center.X + dx, Y: center.Y, Z: center.Z})
			}
		}()
	}
	wg.Wait()
}
