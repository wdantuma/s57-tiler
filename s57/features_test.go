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

// coreClasses are the navigationally important S-57 object classes that must
// survive tiling. The tiler is generic, so the failure mode is silent loss (an
// entire class vanishing with no error) — exactly what hid soundings. keyAttr is
// an attribute that must reach the tile so a renderer can symbolize the feature;
// "" means assert presence/geometry only (e.g. a plain line class).
var coreClasses = []struct{ layer, keyAttr string }{
	{"LIGHTS", "COLOUR"},
	{"BOYLAT", "CATLAM"},
	{"BCNLAT", "CATLAM"},
	{"BCNSPP", "CATSPM"},
	{"DAYMAR", "TOPSHP"},
	{"WRECKS", "CATWRK"},
	{"OBSTRN", "CATOBS"},
	{"UWTROC", "WATLEV"},
	{"DEPARE", "DRVAL1"},
	{"DEPCNT", "VALDCO"},
	{"COALNE", ""},
}

// TestCoreFeatureClassesEmitted is the end-to-end regression guard for the
// generic tiling path: each core class must reach a tile with a recognized
// geometry, non-empty geometry data, and its key attribute. A red row means that
// class silently lost its geometry or attribute during tiling.
func TestCoreFeatureClassesEmitted(t *testing.T) {
	if _, err := os.Stat(sampleENC + "/CATALOG.031"); err != nil {
		t.Skipf("sample ENC not present at %s: %v", sampleENC, err)
	}
	datasets, err := dataset.GetS57Datasets(sampleENC)
	if err != nil {
		t.Fatalf("GetS57Datasets: %v", err)
	}

	for _, tc := range coreClasses {
		t.Run(tc.layer, func(t *testing.T) {
			files := filesWithLayer(datasets, tc.layer)
			if len(files) == 0 {
				t.Skipf("no sample ENC has a %s layer", tc.layer)
			}

			// Try each cell that has the layer until one emits features (a cell
			// may have the layer but all of its features filtered by SCAMIN).
			var layer *vectortile.Tile_Layer
			for _, file := range files {
				lon, lat, ok := firstFeatureCoord(file, tc.layer)
				if !ok {
					continue
				}
				if l := findLayerInTiles(t, datasets, file, lon, lat, tc.layer); l != nil {
					layer = l
					break
				}
			}
			if layer == nil {
				t.Fatalf("no tile/zoom produced %s features in any sample cell", tc.layer)
			}

			for _, f := range layer.Features {
				if f.Type == nil || *f.Type == vectortile.Tile_UNKNOWN {
					t.Errorf("%s feature has nil/UNKNOWN geometry type", tc.layer)
				}
				if len(f.Geometry) == 0 {
					t.Errorf("%s feature has empty geometry", tc.layer)
				}
			}

			if tc.keyAttr != "" && !contains(layer.Keys, tc.keyAttr) {
				t.Errorf("%s layer missing key attribute %s; keys=%v", tc.layer, tc.keyAttr, layer.Keys)
			}

			// Internal S-57 bookkeeping must never reach a tile.
			for _, k := range layer.Keys {
				if internalS57Fields[k] {
					t.Errorf("%s layer leaked internal bookkeeping key %q", tc.layer, k)
				}
			}
		})
	}
}

// filesWithLayer returns every file across all datasets whose layer set contains
// layerName.
func filesWithLayer(datasets []dataset.Dataset, layerName string) []*dataset.File {
	var out []*dataset.File
	for di := range datasets {
		for fi := range datasets[di].Files {
			if _, ok := datasets[di].Files[fi].Layers[layerName]; ok {
				out = append(out, &datasets[di].Files[fi])
			}
		}
	}
	return out
}

// firstFeatureCoord returns a coordinate on the first feature of layerName that
// has one, so the test tiles a cell that actually contains that feature.
func firstFeatureCoord(file *dataset.File, layerName string) (float64, float64, bool) {
	ds := gdal.OpenDataSource(file.Path, 0)
	defer ds.Destroy()
	layer := ds.LayerByName(layerName)
	layer.ResetReading()
	for {
		feat := layer.NextFeature()
		if feat == nil {
			return 0, 0, false
		}
		g := feat.Geometry()
		x, y, ok := firstVertex(&g)
		feat.Destroy()
		if ok {
			return x, y, true
		}
	}
}

// findLayerInTiles tiles around (lon,lat) from fine to coarse zoom and returns
// the first decoded tile layer named layerName that has features. It searches up
// to z16 (finer than the default maxzoom) so SCAMIN-hidden detail isn't a false
// negative; GenerateTile tiles whatever tile it's given regardless of the tiler's
// min/max (those only affect metadata).
func findLayerInTiles(t *testing.T, datasets []dataset.Dataset, file *dataset.File, lon, lat float64, layerName string) *vectortile.Tile_Layer {
	t.Helper()
	tiler := NewS57Tiler(datasets, 9, 14)
	tmp := t.TempDir()
	for z := 16; z >= 9; z-- {
		tile := m.Tile(lon, lat, z)
		tiler.GenerateTile(tmp, *file, tile)

		pbfPath := filepath.Join(tmp, file.Id,
			strconv.Itoa(int(tile.Z)), strconv.Itoa(int(tile.X)),
			strconv.Itoa(int(tile.Y))+".pbf")
		raw, err := os.ReadFile(pbfPath)
		if err != nil {
			continue // no tile written at this zoom
		}
		var decoded vectortile.Tile
		if err := proto.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("decode tile: %v", err)
		}
		for _, l := range decoded.Layers {
			if l.GetName() == layerName && len(l.Features) > 0 {
				return l
			}
		}
	}
	return nil
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
