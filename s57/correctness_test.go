package s57

import (
	"math"
	"testing"

	"github.com/lukeroth/gdal"
	"github.com/wdantuma/s57-tiler/s57/dataset"
)

func TestClampToInt32(t *testing.T) {
	cases := []struct {
		in   float64
		want int32
	}{
		{0, 0},
		{100.7, 100},
		{-5.9, -5},
		{1e18, math.MaxInt32},
		{-1e18, math.MinInt32},
		{math.Inf(1), math.MaxInt32},
		{math.Inf(-1), math.MinInt32},
		{math.NaN(), 0},
	}
	for _, c := range cases {
		if got := clampToInt32(c.in); got != c.want {
			t.Errorf("clampToInt32(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

// getBounds must read the cell bounds from the cached M_COVR layer without
// opening the datasource, so a path that points nowhere still yields the bounds
// (the old code opened and immediately destroyed a datasource it never used).
func TestGetBoundsFromCachedLayerNoOpen(t *testing.T) {
	env := gdal.Envelope{}
	env.SetMinX(1)
	env.SetMinY(2)
	env.SetMaxX(3)
	env.SetMaxY(4)
	file := dataset.File{
		Id:   "T",
		Path: "/does/not/exist.000",
		Layers: map[string]dataset.Layer{
			"M_COVR": {Name: "M_COVR", Bounds: env},
		},
	}
	b := getBounds(file)
	want := []float32{1, 2, 3, 4}
	if len(b) != 4 || b[0] != want[0] || b[1] != want[1] || b[2] != want[2] || b[3] != want[3] {
		t.Errorf("getBounds = %v, want %v", b, want)
	}
	if getBounds(dataset.File{Id: "T"}) != nil {
		t.Errorf("getBounds without M_COVR should return nil")
	}
}

// Close must release the transform/SRS handles created in NewS57Tiler and be safe
// to call more than once (a double Destroy of a GDAL handle would crash).
func TestTilerCloseIdempotent(t *testing.T) {
	for i := 0; i < 25; i++ {
		tiler := NewS57Tiler(nil)
		tiler.Close()
		tiler.Close()
	}
}
