package s57

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/wdantuma/s57-tiler/s57/dataset"
	m "github.com/wdantuma/s57-tiler/s57/mercantile"
	"github.com/wdantuma/signalk-server-go/resources/charts"
)

// TestGenerateMetaDataWritesValidMetadata is hermetic (no GDAL fixture): it drives
// GenerateMetaData through the happy path and asserts a parseable metadata.json is
// produced for the cell.
func TestGenerateMetaDataWritesValidMetadata(t *testing.T) {
	tiler := NewS57Tiler(nil)
	defer tiler.Close()
	out := t.TempDir()
	file := dataset.File{Id: "TESTCELL"}
	ds := dataset.Dataset{Description: "Test Chart"}

	if err := tiler.GenerateMetaData(out, ds, file, 9, 14); err != nil {
		t.Fatalf("GenerateMetaData: unexpected error: %v", err)
	}
	path := filepath.Join(out, "TESTCELL", "metadata.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("metadata not written: %v", err)
	}
	var md charts.ChartMetaData
	if err := json.Unmarshal(b, &md); err != nil {
		t.Fatalf("metadata.json is not valid JSON: %v", err)
	}
	if md.Id != "TESTCELL" {
		t.Errorf("metadata Id = %q, want TESTCELL", md.Id)
	}
}

// TestGenerateMetaDataReturnsErrorOnUnwritablePath asserts the write path surfaces
// an error to the caller instead of calling log.Fatal (which would os.Exit the
// whole worker pool mid-run). Pointing -out at a regular file makes MkdirAll fail
// with ENOTDIR even when the test runs as root (CI runs in a root container), so
// the check is portable.
func TestGenerateMetaDataReturnsErrorOnUnwritablePath(t *testing.T) {
	tiler := NewS57Tiler(nil)
	defer tiler.Close()
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	file := dataset.File{Id: "TESTCELL"}
	if err := tiler.GenerateMetaData(blocker, dataset.Dataset{}, file, 9, 14); err == nil {
		t.Fatal("GenerateMetaData: expected an error writing under a regular file, got nil")
	}
}

// TestGenerateTileReturnsErrorOnUnwritablePath drives the tile write path
// (fixture-gated): a dense tile that really has features must surface a write error
// rather than crash the worker. Skips when the bundled ENC is absent.
func TestGenerateTileReturnsErrorOnUnwritablePath(t *testing.T) {
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

	tiler := NewS57Tiler(datasets)
	defer tiler.Close()

	// Confirm the chosen tile is non-empty, so the test exercises the write path
	// (an empty tile would take the os.Remove branch and never write).
	good := t.TempDir()
	if err := tiler.GenerateTile(good, *file, tile); err != nil {
		t.Fatalf("GenerateTile to a writable dir failed: %v", err)
	}
	pbf := filepath.Join(good, file.Id, strconv.Itoa(int(tile.Z)),
		strconv.Itoa(int(tile.X)), strconv.Itoa(int(tile.Y))+".pbf")
	if _, err := os.Stat(pbf); err != nil {
		t.Skipf("chosen tile z%d produced no features; can't exercise the write path", tile.Z)
	}

	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := tiler.GenerateTile(blocker, *file, tile); err == nil {
		t.Fatal("GenerateTile: expected a write error under a regular file, got nil")
	}
}
