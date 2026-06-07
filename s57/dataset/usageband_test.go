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
