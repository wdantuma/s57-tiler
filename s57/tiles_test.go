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

	// A small box well inside the NW quadrant lands on exactly the one z1 tile {0,0,1}
	// (z1 splits the world into 2x2: col 0 = west of 0°, row 0 = north of the equator).
	one := TilesForExtents([]m.Extrema{{W: -100, N: 40, E: -99, S: 39}}, 1)
	if len(one) != 1 || one[0] != (m.TileID{X: 0, Y: 0, Z: 1}) {
		t.Errorf("NW box at z1 = %+v, want [{0 0 1}]", one)
	}
}
