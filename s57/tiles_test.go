package s57

import (
	"testing"

	m "github.com/wdantuma/s57-tiler/s57/mercantile"
)

// TestTilesForExtents checks the dedup + tiling arithmetic that replaced the old
// string-keyed GetTiles/GetTilesForBounds: overlapping extents must not produce
// duplicate tiles, and an empty input yields nothing.
func TestTilesForExtents(t *testing.T) {
	world := m.Extrema{W: -179, N: 85, E: 179, S: -85}

	got := TilesForExtents([]m.Extrema{world}, 0)
	if len(got) != 1 || got[0] != (m.TileID{X: 0, Y: 0, Z: 0}) {
		t.Fatalf("z0 single extent = %+v, want [{0 0 0}]", got)
	}

	// The same extent twice must dedup to the identical single tile.
	if dup := TilesForExtents([]m.Extrema{world, world}, 0); len(dup) != 1 {
		t.Errorf("duplicate extents = %d tiles, want 1 (deduped)", len(dup))
	}

	if none := TilesForExtents(nil, 5); len(none) != 0 {
		t.Errorf("no extents = %d tiles, want 0", len(none))
	}

	// At z1 a single point lands on exactly one tile, in range.
	one := TilesForExtents([]m.Extrema{{W: 0.1, N: 0.1, E: 0.2, S: 0.0}}, 1)
	if len(one) != 1 {
		t.Errorf("tiny extent at z1 = %d tiles, want 1", len(one))
	}
	for _, tile := range one {
		if tile.Z != 1 || tile.X < 0 || tile.X > 1 || tile.Y < 0 || tile.Y > 1 {
			t.Errorf("z1 tile %+v out of [0,2) grid", tile)
		}
	}
}
