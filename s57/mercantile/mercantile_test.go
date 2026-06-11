package mercantile

import (
	"math"
	"testing"
)

// TestScaleNoZeroGapsMonotonic guards the scale table: every zoom z0..z23 must
// map to a positive, strictly-decreasing denominator. This catches a missing
// case (e.g. z15 previously fell through to the default 0, which broke the
// SCAMIN/SCAMAX comparison in includeFeatureInTile at that zoom).
func TestScaleNoZeroGapsMonotonic(t *testing.T) {
	prev := int32(math.MaxInt32)
	for z := uint64(0); z <= 23; z++ {
		s := Scale(TileID{Z: z})
		if s <= 0 {
			t.Errorf("Scale(z=%d) = %d; want > 0 (no gaps z0..z23)", z, s)
		}
		if s >= prev {
			t.Errorf("Scale not strictly decreasing at z=%d: %d >= %d", z, s, prev)
		}
		prev = s
	}
	if got := Scale(TileID{Z: 15}); got != 24000 {
		t.Errorf("Scale(z=15) = %d; want 24000", got)
	}
}

// TestZoomForScale locks the Scale inverse, including the clamps for a
// missing/garbage compilation scale and out-of-table denominators.
func TestZoomForScale(t *testing.T) {
	cases := []struct {
		scale int32
		want  uint64
	}{
		{2000, 19},
		{4000, 18},
		{8000, 17},
		{15000, 16},
		{24000, 15},
		{35000, 14},
		{90000, 13},
		{0, 0},         // missing DSPM_CSCL
		{-5, 0},        // garbage
		{1, 23},        // finer than the table -> deepest zoom
		{600000000, 0}, // coarser than z0 -> 0
	}
	for _, c := range cases {
		if got := ZoomForScale(c.scale); got != c.want {
			t.Errorf("ZoomForScale(%d) = %d; want %d", c.scale, got, c.want)
		}
	}
}

// TestZoomForScaleRoundTrips ties Scale and ZoomForScale together: because the
// table is gap-free and strictly decreasing, the inverse is exact at every table
// point, so the two functions cannot drift apart.
func TestZoomForScaleRoundTrips(t *testing.T) {
	for z := uint64(0); z <= 23; z++ {
		if got := ZoomForScale(Scale(TileID{Z: z})); got != z {
			t.Errorf("ZoomForScale(Scale(z=%d)) = %d; want %d", z, got, z)
		}
	}
}
