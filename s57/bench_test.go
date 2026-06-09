package s57

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/lukeroth/gdal"

	"github.com/wdantuma/s57-tiler/s57/dataset"
	m "github.com/wdantuma/s57-tiler/s57/mercantile"
	"github.com/wdantuma/s57-tiler/s57/vectortile"
)

// Performance benchmarks for the tiling hot path. They run against the committed
// US4WA1JJ fixture so inputs are deterministic, and skip gracefully when the
// fixture is absent. The most stable regression signals are allocs/op and B/op
// (machine-independent); ns/op is directional, especially on shared CI runners.
//
// Run: make bench   (or: go test -run='^$' -bench=. -benchmem ./s57/...)

// benchFixture loads the bundled datasets and returns the US4WA1JJ cell.
func benchFixture(b *testing.B) ([]dataset.Dataset, *dataset.File) {
	b.Helper()
	if _, err := os.Stat(sampleENC + "/CATALOG.031"); err != nil {
		b.Skipf("sample ENC not present at %s: %v", sampleENC, err)
	}
	datasets, err := dataset.GetS57Datasets(sampleENC)
	if err != nil {
		b.Fatalf("GetS57Datasets: %v", err)
	}
	for di := range datasets {
		for fi := range datasets[di].Files {
			if datasets[di].Files[fi].Id == "US4WA1JJ" {
				return datasets, &datasets[di].Files[fi]
			}
		}
	}
	b.Skip("US4WA1JJ not found in sample ENC")
	return nil, nil
}

// denseTile returns the tile at the cell's centre for the given zoom. An Approach
// cell's centre is over water, so the tile is dense with depth/aid features.
func denseTile(b *testing.B, file *dataset.File, zoom int) m.TileID {
	b.Helper()
	mcovr, ok := file.Layers["M_COVR"]
	if !ok {
		b.Skip("fixture has no M_COVR layer")
	}
	lon := (mcovr.Bounds.MinX() + mcovr.Bounds.MaxX()) / 2
	lat := (mcovr.Bounds.MinY() + mcovr.Bounds.MaxY()) / 2
	return m.Tile(lon, lat, zoom)
}

// totalPoints counts the vertices in a (possibly multi-/ringed) geometry.
func totalPoints(g *gdal.Geometry) int {
	n := g.PointCount()
	for i := 0; i < g.GeometryCount(); i++ {
		sub := g.Geometry(i)
		n += totalPoints(&sub)
	}
	return n
}

// largestFeature returns the feature of layerName with the most vertices, kept
// alive (the caller must Destroy it). Two passes avoid juggling feature lifetimes.
func largestFeature(b *testing.B, ds gdal.DataSource, layerName string) *gdal.Feature {
	b.Helper()
	layer := ds.LayerByName(layerName)
	layer.ResetReading()
	bestIdx, bestN, idx := -1, -1, 0
	for {
		feat := layer.NextFeature()
		if feat == nil {
			break
		}
		g := feat.Geometry()
		if n := totalPoints(&g); n > bestN {
			bestN, bestIdx = n, idx
		}
		feat.Destroy()
		idx++
	}
	if bestIdx < 0 {
		b.Skipf("layer %s has no features", layerName)
	}
	layer.ResetReading()
	for i := 0; ; i++ {
		feat := layer.NextFeature()
		if feat == nil {
			b.Fatalf("feature %d of %s vanished between passes", bestIdx, layerName)
		}
		if i == bestIdx {
			return feat
		}
		feat.Destroy()
	}
}

// BenchmarkGenerateTile is the headline benchmark: the full per-tile pipeline the
// worker pool runs (per-layer datasource open, spatial filter, feature iteration,
// geometry simplification, MVT encode, and the .pbf write) on one dense z14 tile.
func BenchmarkGenerateTile(b *testing.B) {
	datasets, file := benchFixture(b)
	tiler := NewS57Tiler(datasets)
	tile := denseTile(b, file, 14)
	tmp := b.TempDir()

	// GenerateTile removes the output file when a tile has no features, so a
	// missing .pbf here means the chosen tile is empty — fail loudly.
	tiler.GenerateTile(tmp, *file, tile)
	pbf := filepath.Join(tmp, file.Id, strconv.Itoa(int(tile.Z)),
		strconv.Itoa(int(tile.X)), strconv.Itoa(int(tile.Y))+".pbf")
	if _, err := os.Stat(pbf); err != nil {
		b.Fatalf("dense tile z%d produced no features (%v); pick a different centre/zoom", tile.Z, err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tiler.GenerateTile(tmp, *file, tile)
	}
}

// BenchmarkToMvtGeometry isolates geometry simplification + MVT encoding (no GDAL
// I/O), per geometry type, on the densest real feature of a representative layer.
func BenchmarkToMvtGeometry(b *testing.B) {
	datasets, file := benchFixture(b)
	tiler := NewS57Tiler(datasets)
	tile := denseTile(b, file, 14)
	bounds := m.Bounds(tile)

	cases := []struct {
		name  string
		layer string
		gt    vectortile.Tile_GeomType
	}{
		{"polygon", "DEPARE", vectortile.Tile_POLYGON},
		{"line", "DEPCNT", vectortile.Tile_LINESTRING},
		{"point", "UWTROC", vectortile.Tile_POINT},
	}
	for _, c := range cases {
		func() {
			ds := gdal.OpenDataSource(file.Path, 0)
			defer ds.Destroy()
			feat := largestFeature(b, ds, c.layer)
			defer feat.Destroy()
			geom := feat.Geometry()

			b.Run(c.name, func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					_ = tiler.toMvtGeometry(c.gt, &geom, tile, bounds)
				}
			})
		}()
	}
}

// BenchmarkGetFeatures isolates the GDAL read path (spatial filter, feature
// iteration, SCAMIN/SCAMAX gating, attribute conversion) without the .pbf write.
func BenchmarkGetFeatures(b *testing.B) {
	datasets, file := benchFixture(b)
	tiler := NewS57Tiler(datasets)
	tile := denseTile(b, file, 12)
	bounds := m.Bounds(tile)

	ds := gdal.OpenDataSource(file.Path, 0)
	defer ds.Destroy()
	layer := ds.LayerByName("DEPARE")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		layer.ResetReading()
		tiler.startLayer()
		b.StartTimer()
		_ = tiler.GetFeatures(layer, tile, bounds)
	}
}

// BenchmarkGetS57Datasets measures CLI startup cost: catalog parse plus one GDAL
// datasource open per .000 to enumerate layers.
func BenchmarkGetS57Datasets(b *testing.B) {
	if _, err := os.Stat(sampleENC + "/CATALOG.031"); err != nil {
		b.Skipf("sample ENC not present at %s: %v", sampleENC, err)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		datasets, err := dataset.GetS57Datasets(sampleENC)
		if err != nil {
			b.Fatalf("GetS57Datasets: %v", err)
		}
		if len(datasets) == 0 {
			b.Fatal("no datasets loaded")
		}
	}
}
