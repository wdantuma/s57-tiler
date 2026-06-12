package s57

import (
	"math"
	"testing"

	m "github.com/wdantuma/s57-tiler/s57/mercantile"
)

// TestTo3857Origin checks the geographic->Web-Mercator transform at the
// unambiguous origin (lon=0, lat=0 maps to 0,0).
func TestTo3857Origin(t *testing.T) {
	tiler := NewS57Tiler(nil)
	defer tiler.Close()
	x, y := tiler.to3857(0, 0)
	if math.Abs(x) > 1 || math.Abs(y) > 1 {
		t.Errorf("to3857(0,0) = (%g,%g), want ~(0,0)", x, y)
	}
}

// TestToTileCoordinateMapsCorners is the end-to-end guard for the projection
// pipeline (to3857 + setProjection caching + the tile-unit scaling): a tile's
// upper-left corner must map to ~(0,0) and its lower-right to ~(TILE_EXTENT,
// TILE_EXTENT). Iterating distinct tiles also exercises setProjection's cache
// invalidation when the bounds change. A regression here renders features at the
// wrong position with no error signal.
func TestToTileCoordinateMapsCorners(t *testing.T) {
	tiler := NewS57Tiler(nil)
	defer tiler.Close()
	for _, tile := range []m.TileID{{X: 2614, Y: 5664, Z: 14}, {X: 0, Y: 0, Z: 1}, {X: 8, Y: 5, Z: 4}} {
		b := m.Bounds(tile)
		ulx, uly, _ := tiler.toTileCoordinate(b, b.W, b.N, 0)
		if abs32(ulx) > 1 || abs32(uly) > 1 {
			t.Errorf("tile %+v UL corner = (%d,%d), want ~(0,0)", tile, ulx, uly)
		}
		lrx, lry, _ := tiler.toTileCoordinate(b, b.E, b.S, 0)
		if abs32(lrx-TILE_EXTENT) > 1 || abs32(lry-TILE_EXTENT) > 1 {
			t.Errorf("tile %+v LR corner = (%d,%d), want ~(%d,%d)", tile, lrx, lry, TILE_EXTENT, TILE_EXTENT)
		}
	}
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
