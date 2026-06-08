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
	tiler := NewS57Tiler(nil)

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

	// Soundings carry SCAMIN, so they only appear once the map scale is fine
	// enough. Try finer zooms first and use the first tile that emits SOUNDG,
	// rather than hard-coding a zoom that may be too coarse for a given cell.
	tiler := NewS57Tiler(datasets)
	tmp := t.TempDir()

	var soundg *vectortile.Tile_Layer
	for z := 14; z >= 9 && soundg == nil; z-- {
		tile := m.Tile(lon, lat, z)
		tiler.GenerateTile(tmp, *soundgFile, tile)

		pbfPath := filepath.Join(tmp, soundgFile.Id,
			strconv.Itoa(int(tile.Z)), strconv.Itoa(int(tile.X)),
			strconv.Itoa(int(tile.Y))+".pbf")
		raw, err := os.ReadFile(pbfPath)
		if err != nil {
			continue // no tile written at this zoom (e.g. all features filtered)
		}
		var decoded vectortile.Tile
		if err := proto.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("decode tile: %v", err)
		}
		for _, l := range decoded.Layers {
			if l.GetName() == "SOUNDG" && len(l.Features) > 0 {
				soundg = l
				break
			}
		}
	}
	if soundg == nil {
		t.Fatal("no tile/zoom produced SOUNDG features for the sample cell")
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

// TestDryingHeightsNegativeDepthPreserved verifies that drying heights — areas
// that dry out at low water — survive tiling with their NEGATIVE depth intact.
// In NOAA ENCs these are DEPARE polygons whose DRVAL1 is negative (e.g. -4.2 =
// "dries 4.2 m above datum"), not negative SOUNDG points. Runs end-to-end on the
// bundled sample (Nisqually Reach has tidal flats) and decodes the written tile.
func TestDryingHeightsNegativeDepthPreserved(t *testing.T) {
	if _, err := os.Stat(sampleENC); err != nil {
		t.Skipf("sample ENC dir not present at %s: %v", sampleENC, err)
	}
	datasets, err := dataset.GetS57Datasets(sampleENC)
	if err != nil {
		t.Fatalf("GetS57Datasets: %v", err)
	}

	// Find a DEPARE feature with a negative DRVAL1 and a coordinate on it.
	var dryFile *dataset.File
	var lon, lat, srcDrval float64
	for di := range datasets {
		for fi := range datasets[di].Files {
			f := &datasets[di].Files[fi]
			if _, ok := f.Layers["DEPARE"]; !ok {
				continue
			}
			ds := gdal.OpenDataSource(f.Path, 0)
			layer := ds.LayerByName("DEPARE")
			layer.ResetReading()
			for {
				feat := layer.NextFeature()
				if feat == nil {
					break
				}
				idx := feat.FieldIndex("DRVAL1")
				if idx >= 0 && feat.IsFieldSet(idx) && feat.FieldAsFloat64(idx) < 0 {
					g := feat.Geometry()
					if x, y, ok := firstVertex(&g); ok {
						dryFile, lon, lat, srcDrval = f, x, y, feat.FieldAsFloat64(idx)
					}
				}
				feat.Destroy()
				if dryFile != nil {
					break
				}
			}
			ds.Destroy()
			if dryFile != nil {
				break
			}
		}
		if dryFile != nil {
			break
		}
	}
	if dryFile == nil {
		t.Skip("no DEPARE feature with negative DRVAL1 (drying height) in sample ENC")
	}

	const zoom = 14
	tile := m.Tile(lon, lat, zoom)
	tiler := NewS57Tiler(datasets)
	tmp := t.TempDir()
	tiler.GenerateTile(tmp, *dryFile, tile)

	pbfPath := filepath.Join(tmp, dryFile.Id,
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

	var depare *vectortile.Tile_Layer
	for _, l := range decoded.Layers {
		if l.GetName() == "DEPARE" {
			depare = l
			break
		}
	}
	if depare == nil {
		t.Fatal("decoded tile has no DEPARE layer")
	}

	// At least one DEPARE feature in the tile must carry a negative DRVAL1.
	var minDrval float64
	foundNeg := false
	for _, f := range depare.Features {
		for i := 0; i+1 < len(f.Tags); i += 2 {
			if depare.Keys[f.Tags[i]] != "DRVAL1" {
				continue
			}
			v := depare.Values[f.Tags[i+1]]
			if v.DoubleValue != nil && *v.DoubleValue < 0 {
				foundNeg = true
				if *v.DoubleValue < minDrval {
					minDrval = *v.DoubleValue
				}
			}
		}
	}
	if !foundNeg {
		t.Fatalf("no negative DRVAL1 in emitted DEPARE layer (source had %.1f)", srcDrval)
	}
	t.Logf("drying height preserved: source DRVAL1=%.1f, emitted min DRVAL1=%.1f", srcDrval, minDrval)
}

// TestNegativeSoundingDepthPreserved is a hermetic guard for the literal case:
// a SOUNDG point with a NEGATIVE DEPTH must be emitted as a POINT feature with the
// negative value preserved (not dropped, zeroed, or made positive). Uses a
// synthetic feature so it doesn't depend on the fixture containing one.
func TestNegativeSoundingDepthPreserved(t *testing.T) {
	const lon, lat, depth = -122.7, 47.1, -2.4

	fd := gdal.CreateFeatureDefinition("SOUNDG")
	fd.AddFieldDefinition(gdal.CreateFieldDefinition("DEPTH", gdal.FT_Real))
	feat := fd.Create()
	defer feat.Destroy()

	geom := gdal.Create(gdal.GT_Point25D)
	geom.AddPoint(lon, lat, depth)
	feat.SetGeometry(geom)
	geom.Destroy()
	feat.SetFieldFloat64(feat.FieldIndex("DEPTH"), depth)

	tiler := NewS57Tiler(nil)
	tiler.startLayer()
	tile := m.Tile(lon, lat, 12)
	mf := tiler.toMvtFeature(&feat, tile, m.Bounds(tile))

	if mf == nil {
		t.Fatal("negative sounding was dropped (toMvtFeature returned nil)")
	}
	if mf.Type == nil || *mf.Type != vectortile.Tile_POINT {
		t.Errorf("type = %v, want Tile_POINT", mf.Type)
	}
	if len(mf.Geometry) == 0 {
		t.Error("negative sounding has empty geometry")
	}

	hasKey := false
	for _, k := range tiler.keys {
		if k == "DEPTH" {
			hasKey = true
		}
	}
	if !hasKey {
		t.Errorf("DEPTH key missing; keys=%v", tiler.keys)
	}
	foundNeg := false
	for _, v := range tiler.values {
		if v.fieldType == VT_FLOAT {
			if f, ok := v.value.(float64); ok && f == depth {
				foundNeg = true
			}
		}
	}
	if !foundNeg {
		t.Errorf("negative DEPTH %.1f not preserved in emitted values: %+v", depth, tiler.values)
	}
}

// firstVertex descends a (possibly multi-) geometry to a concrete coordinate.
func firstVertex(g *gdal.Geometry) (float64, float64, bool) {
	cur := *g
	for cur.GeometryCount() > 0 {
		cur = cur.Geometry(0)
	}
	if cur.PointCount() == 0 {
		return 0, 0, false
	}
	x, y, _ := cur.Point(0)
	return x, y, true
}
