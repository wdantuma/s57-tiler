package dataset

import (
	"os"
	"testing"
)

func TestBandZoom(t *testing.T) {
	cases := []struct {
		band             int
		wantMin, wantMax int
	}{
		{1, 5, 9}, {2, 7, 11}, {3, 9, 12}, {4, 10, 14},
		{5, 12, 16}, {6, 13, 17}, {0, 9, 14}, {99, 9, 14},
	}
	for _, c := range cases {
		gotMin, gotMax := BandZoom(c.band)
		if gotMin != c.wantMin || gotMax != c.wantMax {
			t.Errorf("BandZoom(%d) = (%d,%d), want (%d,%d)", c.band, gotMin, gotMax, c.wantMin, c.wantMax)
		}
	}
}

// TestUsageBandFromDSID checks that the usage band is read from the cell's DSID
// record. US4WA1JJ is a NOAA Approach cell (DSID_INTU = 4).
func TestUsageBandFromDSID(t *testing.T) {
	const encDir = "../../enc"
	if _, err := os.Stat(encDir); err != nil {
		t.Skipf("sample ENC not present at %s: %v", encDir, err)
	}
	datasets, err := GetS57Datasets(encDir)
	if err != nil {
		t.Fatalf("GetS57Datasets: %v", err)
	}
	found := false
	for _, ds := range datasets {
		for _, f := range ds.Files {
			if f.Id == "US4WA1JJ" {
				found = true
				if got := UsageBand(f); got != 4 {
					t.Errorf("UsageBand(%s) = %d, want 4 (Approach)", f.Id, got)
				}
			}
		}
	}
	if !found {
		t.Skip("US4WA1JJ not found in sample ENC")
	}
}

// TestCellZoom verifies per-cell zoom derivation: a marine cell keeps the
// usage-band mapping unchanged, while a large-scale Inland ENC (DSID_INTU=7,
// 1:2000) is tiled to a higher, scale-derived range instead of the coarse 9–14
// default that left it blank in Freeboard. The expected (12,16) is the capped
// derivation: ZoomForScale(2000)=19 -> cap 16, min = 16-4 floored at 12.
func TestCellZoom(t *testing.T) {
	const encDir = "../../enc"
	if _, err := os.Stat(encDir); err != nil {
		t.Skipf("sample ENC not present at %s: %v", encDir, err)
	}
	datasets, err := GetS57Datasets(encDir)
	if err != nil {
		t.Fatalf("GetS57Datasets: %v", err)
	}

	want := map[string]struct {
		band             int
		minZoom, maxZoom int
	}{
		"US4WA1JJ": {4, 10, 14}, // marine Approach: BandZoom path, unchanged
		"1R7WAD01": {0, 12, 19}, // inland 1:2000: band 7 -> 0 -> native scale z19 (no cap)
	}

	seen := map[string]bool{}
	for _, ds := range datasets {
		for _, f := range ds.Files {
			w, ok := want[f.Id]
			if !ok {
				continue
			}
			seen[f.Id] = true
			if got := UsageBand(f); got != w.band {
				t.Errorf("UsageBand(%s) = %d, want %d", f.Id, got, w.band)
			}
			gotMin, gotMax := CellZoom(f)
			if gotMin != w.minZoom || gotMax != w.maxZoom {
				t.Errorf("CellZoom(%s) = (%d,%d), want (%d,%d)", f.Id, gotMin, gotMax, w.minZoom, w.maxZoom)
			}
		}
	}
	for id := range want {
		if !seen[id] {
			t.Logf("cell %s not found in sample ENC; that assertion was skipped", id)
		}
	}
}

// TestCellZoomRange verifies the default range runs up to each cell's native
// compilation scale with no cap: the inland 1:2000 cell tiles to z19, while the
// bundled marine cells stay at their band default (which already meets/exceeds
// their native scale).
func TestCellZoomRange(t *testing.T) {
	const encDir = "../../enc"
	if _, err := os.Stat(encDir); err != nil {
		t.Skipf("sample ENC not present at %s: %v", encDir, err)
	}
	datasets, err := GetS57Datasets(encDir)
	if err != nil {
		t.Fatalf("GetS57Datasets: %v", err)
	}

	want := map[string]ZoomRange{
		"1R7WAD01": {Min: 12, Max: 19}, // inland 1:2000 -> native z19 (no cap)
		"US4WA1JJ": {Min: 10, Max: 14}, // marine 1:45000 -> band default already at native z14
		"US4WI1DP": {Min: 10, Max: 14}, // marine 1:90000 -> native z13, kept at band max 14
	}

	seen := map[string]bool{}
	for _, ds := range datasets {
		for _, f := range ds.Files {
			w, ok := want[f.Id]
			if !ok {
				continue
			}
			seen[f.Id] = true
			got := CellZoomRange(f)
			if got != w {
				t.Errorf("CellZoomRange(%s) = %+v, want %+v", f.Id, got, w)
			}
			if got.Min > got.Max {
				t.Errorf("CellZoomRange(%s): min %d > max %d", f.Id, got.Min, got.Max)
			}
		}
	}
	for id := range want {
		if !seen[id] {
			t.Logf("cell %s not found in sample ENC; that assertion was skipped", id)
		}
	}
}
