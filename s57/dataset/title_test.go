package dataset

import (
	"os"
	"testing"
)

// TestFileTitleFromCatalog verifies the cell long-name (the S-57 LFIL subfield) is
// read from CATALOG.031 into File.Title, which feeds the chart Description. US4WA1JJ's
// catalog carries it; older downloads (US4WI1DP) may omit it — best-effort.
func TestFileTitleFromCatalog(t *testing.T) {
	const encDir = "../../enc"
	if _, err := os.Stat(encDir); err != nil {
		t.Skipf("sample ENC not present at %s: %v", encDir, err)
	}
	datasets, err := GetS57Datasets(encDir)
	if err != nil {
		t.Fatalf("GetS57Datasets: %v", err)
	}
	titles := map[string]string{}
	for _, ds := range datasets {
		for _, f := range ds.Files {
			titles[f.Id] = f.Title
		}
	}
	if _, ok := titles["US4WA1JJ"]; !ok {
		t.Skip("US4WA1JJ not found in sample ENC")
	}
	if got, want := titles["US4WA1JJ"], "Anacortes and Vicinity, WA"; got != want {
		t.Errorf("US4WA1JJ title = %q, want %q", got, want)
	}
	if _, ok := titles["US4WI1DP"]; ok {
		if got, want := titles["US4WI1DP"], "Lake Michigan - Whitefish Bay and Milwaukee Bay, WI"; got != want {
			t.Errorf("US4WI1DP title = %q, want %q", got, want)
		}
	}
}
