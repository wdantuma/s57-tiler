package dataset

import (
	"testing"

	"github.com/tburke/iso8211"
)

// field1 builds a catalog DataRecord whose Fields[1].SubFields are the given
// values, mirroring the layout GetS57Datasets reads (index 2 = file name, 3 =
// title, 5 = record type).
func field1(subfields ...interface{}) iso8211.DataRecord {
	return iso8211.DataRecord{Fields: []iso8211.Field{
		{},
		{SubFields: subfields},
	}}
}

func TestCatalogCellRef(t *testing.T) {
	cases := []struct {
		name      string
		rec       iso8211.DataRecord
		wantFile  string
		wantTitle string
		wantOK    bool
	}{
		{
			name:      "valid BIN .000 row",
			rec:       field1("0", "1", "ENC_ROOT/US4WA1JJ.000", "Anacortes, WA", "4", "BIN"),
			wantFile:  "ENC_ROOT/US4WA1JJ.000",
			wantTitle: "Anacortes, WA",
			wantOK:    true,
		},
		{
			name:   "not a BIN record",
			rec:    field1("0", "1", "US4WA1JJ.000", "t", "4", "ASC"),
			wantOK: false,
		},
		{
			name:   "BIN but not a .000 cell",
			rec:    field1("0", "1", "README.TXT", "t", "4", "BIN"),
			wantOK: false,
		},
		{
			name:   "too few subfields (truncated record)",
			rec:    field1("0", "1", "US4WA1JJ.000"),
			wantOK: false,
		},
		{
			name:   "too few fields",
			rec:    iso8211.DataRecord{Fields: []iso8211.Field{{}}},
			wantOK: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotFile, gotTitle, ok := catalogCellRef(c.rec)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if ok && (gotFile != c.wantFile || gotTitle != c.wantTitle) {
				t.Errorf("got (%q,%q), want (%q,%q)", gotFile, gotTitle, c.wantFile, c.wantTitle)
			}
		})
	}
}

func TestSafeCellID(t *testing.T) {
	cases := []struct {
		path   string
		wantID string
		wantOK bool
	}{
		{"/data/ENC_ROOT/US4WA1JJ.000", "US4WA1JJ", true},
		{"/data/1R7WAD01.000", "1R7WAD01", true},
		{"deep/dir/B.000", "B", true}, // a dir separator in the path is fine; the id is the base
		{"/data/...000", "", false},   // id would be ".."  -> traversal
		{"/data/..000", "", false},    // id would be "."
		{"/data/.000", "", false},     // id would be empty
		{"/data/ab", "", false},       // shorter than the ".000" suffix
	}
	for _, c := range cases {
		got, ok := safeCellID(c.path)
		if ok != c.wantOK || (ok && got != c.wantID) {
			t.Errorf("safeCellID(%q) = (%q,%v), want (%q,%v)", c.path, got, ok, c.wantID, c.wantOK)
		}
	}
}
