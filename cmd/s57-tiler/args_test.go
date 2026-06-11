package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClampZoomValue(t *testing.T) {
	cases := []struct {
		in       int
		want     int
		wantNote bool
	}{
		{-1, 0, true},
		{0, 0, false},
		{14, 14, false},
		{23, 23, false},
		{30, 23, true},
	}
	for _, c := range cases {
		got, note := clampZoomValue("maxzoom", c.in)
		if got != c.want || (note != "") != c.wantNote {
			t.Errorf("clampZoomValue(%d) = (%d, note=%q), want (%d, hasNote=%v)", c.in, got, note, c.want, c.wantNote)
		}
	}
}

func TestValidateZoomRange(t *testing.T) {
	if err := validateZoomRange(9, 14); err != nil {
		t.Errorf("validateZoomRange(9,14) = %v, want nil", err)
	}
	if err := validateZoomRange(5, 5); err != nil {
		t.Errorf("validateZoomRange(5,5) = %v, want nil", err)
	}
	if err := validateZoomRange(20, 5); err == nil {
		t.Error("validateZoomRange(20,5) = nil, want error")
	}
}

func TestParseBounds(t *testing.T) {
	b, err := parseBounds("5,50,6,49") // W,N,E,S
	if err != nil {
		t.Fatalf("parseBounds valid: %v", err)
	}
	if b.W != 5 || b.N != 50 || b.E != 6 || b.S != 49 {
		t.Errorf("parseBounds = %+v, want W5 N50 E6 S49", b)
	}

	bad := []string{
		"1,2,3",       // wrong count
		"a,2,3,4",     // not a number
		"6,50,5,49",   // W >= E
		"5,49,6,50",   // S >= N
		"200,50,6,49", // lon out of range
		"5,95,6,49",   // lat out of range
	}
	for _, s := range bad {
		if _, err := parseBounds(s); err == nil {
			t.Errorf("parseBounds(%q) = nil error, want error", s)
		}
	}
}

func TestEnsureWritableDir(t *testing.T) {
	if err := ensureWritableDir(filepath.Join(t.TempDir(), "sub", "charts")); err != nil {
		t.Errorf("ensureWritableDir(new dir) = %v, want nil", err)
	}
	// A path whose parent is a regular file can't be created (ENOTDIR), even as
	// root — so this is a portable "not writable" case.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ensureWritableDir(filepath.Join(blocker, "charts")); err == nil {
		t.Error("ensureWritableDir(under a file) = nil, want error")
	}
}
