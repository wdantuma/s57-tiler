package s57

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/wdantuma/s57-tiler/s57/dataset"
	m "github.com/wdantuma/s57-tiler/s57/mercantile"
)

// TestGenerateTileDeterministicBytes guards that a tile encodes to identical bytes
// on every run. Layers are now emitted in sorted order, so map-iteration
// randomness can't perturb the .pbf — a regression that reintroduces nondeterminism
// (or otherwise changes the encoding) fails here. Fixture-gated.
func TestGenerateTileDeterministicBytes(t *testing.T) {
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
	tile := m.Tile(lon, lat, 14)

	read := func() []byte {
		tiler := NewS57Tiler(datasets)
		defer tiler.Close()
		tmp := t.TempDir()
		if err := tiler.GenerateTile(tmp, *file, tile); err != nil {
			t.Fatalf("GenerateTile: %v", err)
		}
		pbf := filepath.Join(tmp, file.Id, strconv.Itoa(int(tile.Z)),
			strconv.Itoa(int(tile.X)), strconv.Itoa(int(tile.Y))+".pbf")
		b, err := os.ReadFile(pbf)
		if err != nil {
			t.Fatalf("dense tile not written: %v", err)
		}
		return b
	}

	first := read()
	for i := 0; i < 4; i++ {
		if got := read(); !bytes.Equal(got, first) {
			t.Fatalf("tile bytes not deterministic: run %d (%d bytes) != run 0 (%d bytes)", i+1, len(got), len(first))
		}
	}
}
