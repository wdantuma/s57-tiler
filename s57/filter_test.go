package s57

import "testing"

// scaleVisible is the SCAMIN/SCAMAX visibility predicate extracted from
// includeFeatureInTile. SCAMIN/SCAMAX are 0 when unset; scale is the tile's scale
// denominator. A feature is hidden when it is below its minimum scale (scamin <
// scale) or above its maximum scale (scamax > scale).
func TestScaleVisible(t *testing.T) {
	cases := []struct {
		name           string
		scamin, scamax float64
		scale          int32
		want           bool
	}{
		{"both unset", 0, 0, 70000, true},
		{"scamin hides when zoomed out past it", 50000, 0, 70000, false},
		{"scamin allows when within range", 50000, 0, 35000, true},
		{"scamin boundary (equal) shows", 70000, 0, 70000, true},
		{"scamax hides when zoomed in past it", 0, 100000, 70000, false},
		{"scamax allows when within range", 0, 100000, 150000, true},
		{"scamax boundary (equal) shows", 0, 70000, 70000, true},
		{"scamin takes effect even with scamax set", 50000, 200000, 100000, false},
		{"within both bounds (scamin>=scale>=scamax)", 200000, 50000, 70000, true},
	}
	for _, c := range cases {
		if got := scaleVisible(c.scamin, c.scamax, c.scale); got != c.want {
			t.Errorf("%s: scaleVisible(%g,%g,%d) = %v, want %v", c.name, c.scamin, c.scamax, c.scale, got, c.want)
		}
	}
}
