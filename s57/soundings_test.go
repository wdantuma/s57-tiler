package s57

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/lukeroth/gdal"
	"google.golang.org/protobuf/proto"

	"github.com/wdantuma/s57-tiler/s57/dataset"
	m "github.com/wdantuma/s57-tiler/s57/mercantile"
	"github.com/wdantuma/s57-tiler/s57/vectortile"
)

const sampleENC = "../enc"

// TestGetMvtFeatureTypePoint25D locks in the fix that 3D points (what SOUNDG
// soundings become once SPLIT_MULTIPOINT is enabled) map to the POINT geometry
// type instead of being dropped as UNKNOWN.
func TestGetMvtFeatureTypePoint25D(t *testing.T) {
	tiler := NewS57Tiler(nil, 9, 14)

	geom := gdal.Create(gdal.GT_Point25D)
	defer geom.Destroy()
	geom.AddPoint(1, 2, 5)

	got := tiler.getMvtFeatureType(&geom)
	if got == nil || *got != vectortile.Tile_POINT {
		t.Fatalf("getMvtFeatureType(Point25D) = %v, want Tile_POINT", got)
	}
}

// TestSoundingsEmittedWithDepth is the end-to-end regression guard. It runs the
// real GenerateTile path (not just GetFeatures) against the bundled NOAA sample
// and decodes the written tile, so it covers all three failure modes that hid
// soundings: the FeatureCount gate, unrecognized 3D points, and the missing
// DEPTH attribute.
func TestSoundingsEmittedWithDepth(t *testing.T) {
	if _, err := os.Stat(sampleENC + "/CATALOG.031"); err != nil {
		t.Skipf("sample ENC not present at %s: %v", sampleENC, err)
	}

	datasets, err := dataset.GetS57Datasets(sampleENC)
	if err != nil {
		t.Fatalf("GetS57Datasets: %v", err)
	}

	var soundgFile *dataset.File
	for di := range datasets {
		for fi := range datasets[di].Files {
			if _, ok := datasets[di].Files[fi].Layers["SOUNDG"]; ok {
				soundgFile = &datasets[di].Files[fi]
				break
			}
		}
		if soundgFile != nil {
			break
		}
	}
	if soundgFile == nil {
		t.Fatal("sample ENC has no SOUNDG layer")
	}

	// Find a real sounding coordinate so we tile a cell that actually contains one.
	ds := gdal.OpenDataSource(soundgFile.Path, 0)
	layer := ds.LayerByName("SOUNDG")
	feat := layer.NextFeature()
	if feat == nil {
		ds.Destroy()
		t.Fatal("SOUNDG layer has no features")
	}
	g := feat.Geometry()
	lon, lat, _ := g.Point(0)
	feat.Destroy()
	ds.Destroy()

	// zoom 11: soundings carry SCAMIN=999999, which is only visible once the map
	// scale is fine enough (roughly z10+).
	const zoom = 11
	tile := m.Tile(lon, lat, zoom)

	tiler := NewS57Tiler(datasets, 9, 14)
	tmp := t.TempDir()
	tiler.GenerateTile(tmp, *soundgFile, tile)

	pbfPath := filepath.Join(tmp, soundgFile.Id,
		strconv.Itoa(int(tile.Z)), strconv.Itoa(int(tile.X)),
		strconv.Itoa(int(tile.Y))+".pbf")
	raw, err := os.ReadFile(pbfPath)
	if err != nil {
		t.Fatalf("expected a tile at %s: %v", pbfPath, err)
	}

	var decoded vectortile.Tile
	if err := proto.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode tile: %v", err)
	}

	var soundg *vectortile.Tile_Layer
	for _, l := range decoded.Layers {
		if l.GetName() == "SOUNDG" {
			soundg = l
			break
		}
	}
	if soundg == nil || len(soundg.Features) == 0 {
		t.Fatal("decoded tile has no SOUNDG features")
	}

	for _, f := range soundg.Features {
		if f.Type == nil || *f.Type != vectortile.Tile_POINT {
			t.Errorf("sounding feature type = %v, want Tile_POINT", f.Type)
		}
		if len(f.Geometry) == 0 {
			t.Error("sounding feature has empty geometry")
		}
	}

	hasDepth := false
	for _, k := range soundg.Keys {
		if k == "DEPTH" {
			hasDepth = true
		}
	}
	if !hasDepth {
		t.Errorf("SOUNDG layer is missing the DEPTH attribute; keys=%v", soundg.Keys)
	}
}
