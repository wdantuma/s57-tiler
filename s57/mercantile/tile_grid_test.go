package mercantile

import "testing"

// Tile must never return an x/y outside the [0, 2^z) grid, even at the
// antimeridian / poles where the projection math lands exactly on (or past) the
// edge. GetTilesForBounds iterates col=ulTile.X..lrTile.X, so an off-grid value
// produced invalid/wrong tiles.
func TestTileClampsToGrid(t *testing.T) {
	for _, z := range []int{0, 1, 5, 14, 23} {
		max := int64(1)<<uint(z) - 1
		for _, lng := range []float64{180.0, -180.0, 360.0, -360.0} {
			got := Tile(lng, 0.0, z)
			if got.X < 0 || got.X > max {
				t.Errorf("Tile(%v,0,%d).X = %d, want in [0,%d]", lng, z, got.X, max)
			}
		}
		// A latitude beyond the Web-Mercator limit must also stay on-grid.
		if got := Tile(0.0, 89.9, z); got.Y < 0 || got.Y > max {
			t.Errorf("Tile(0,89.9,%d).Y = %d, want in [0,%d]", z, got.Y, max)
		}
	}
}

// An interior coordinate is unaffected by the clamp.
func TestTileInteriorUnchanged(t *testing.T) {
	if got := Tile(0.0, 0.0, 2); got != (TileID{X: 2, Y: 2, Z: 2}) {
		t.Errorf("Tile(0,0,2) = %+v, want {2,2,2}", got)
	}
}
