package s57

import (
	"testing"

	m "github.com/wdantuma/s57-tiler/s57/mercantile"
)

// These benchmarks track the native-zoom pre-pass + generation hot path that the
// performance work targets: the per-file extent scan (done once per file), the
// per-zoom tile arithmetic (done while streaming), and generating one cell across
// several zooms with a single tiler (the per-file worker-pool model, where the
// cell's datasource is opened once and reused across zooms).

// BenchmarkFileExtents measures the one-per-file full-scan extent computation.
func BenchmarkFileExtents(b *testing.B) {
	datasets, file := benchFixture(b)
	tiler := NewS57Tiler(datasets)
	defer tiler.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tiler.FileExtents(*file)
	}
}

// BenchmarkTilesForExtents measures the per-zoom tile-set arithmetic that runs
// during streaming (and twice more for the up-front count) — it must stay cheap,
// since the pre-pass no longer pays a datasource reopen per zoom.
func BenchmarkTilesForExtents(b *testing.B) {
	datasets, file := benchFixture(b)
	tiler := NewS57Tiler(datasets)
	defer tiler.Close()
	extents := tiler.FileExtents(*file)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = TilesForExtents(extents, 12)
	}
}

// BenchmarkGenerateTileMultiZoom mirrors the per-file pool: one tiler generates a
// cell's centre tile across several zooms, reusing the cached datasource across
// all of them rather than reopening per zoom.
func BenchmarkGenerateTileMultiZoom(b *testing.B) {
	datasets, file := benchFixture(b)
	tiler := NewS57Tiler(datasets)
	defer tiler.Close()
	tiles := []m.TileID{denseTile(b, file, 12), denseTile(b, file, 13), denseTile(b, file, 14)}
	tmp := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, t := range tiles {
			tiler.GenerateTile(tmp, *file, t)
		}
	}
}
