package main

import (
	"strings"
	"testing"
)

// TestSummarizeZoomsSubsetHint: a group whose converting max is below its
// available max is flagged as a subset, shows the available range, and triggers
// a -maxzoom hint naming the envelope max. Identical ranges collapse to one line.
func TestSummarizeZoomsSubsetHint(t *testing.T) {
	reports := []zoomReport{
		{convMin: 12, convMax: 16, availMin: 12, availMax: 19}, // subset (max)
		{convMin: 12, convMax: 16, availMin: 12, availMax: 19}, // same group
		{convMin: 10, convMax: 14, availMin: 10, availMax: 14}, // full range
	}
	lines, hint := summarizeZooms(reports)

	if len(lines) != 2 {
		t.Fatalf("expected 2 grouped lines, got %d: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "2 chart(s)") || !strings.Contains(lines[0], "z12-z16") {
		t.Errorf("group line 0 = %q, want 2 charts converting z12-z16", lines[0])
	}
	if !strings.Contains(lines[0], "available z12-z19") {
		t.Errorf("subset line should show the available range, got %q", lines[0])
	}
	if strings.Contains(lines[1], "available") {
		t.Errorf("full-range line should not show an available range, got %q", lines[1])
	}
	if hint == "" {
		t.Fatal("expected a hint when a group is a subset")
	}
	if !strings.Contains(hint, "-maxzoom 19") {
		t.Errorf("hint should suggest -maxzoom 19, got %q", hint)
	}
	if strings.Contains(hint, "-minzoom") {
		t.Errorf("hint should not mention -minzoom when only the max is a subset, got %q", hint)
	}
}

// TestSummarizeZoomsNoHintWhenFull: when every cell covers its full available
// range, no hint is produced and no "available" suffix is shown.
func TestSummarizeZoomsNoHintWhenFull(t *testing.T) {
	reports := []zoomReport{
		{convMin: 10, convMax: 14, availMin: 10, availMax: 14},
		{convMin: 13, convMax: 17, availMin: 13, availMax: 17},
	}
	lines, hint := summarizeZooms(reports)

	if hint != "" {
		t.Errorf("expected no hint when nothing is a subset, got %q", hint)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
	}
	for _, l := range lines {
		if strings.Contains(l, "available") {
			t.Errorf("full-range line should omit available, got %q", l)
		}
	}
}

// TestSummarizeZoomsMinSubset: a narrowed low end (override) yields a -minzoom hint.
func TestSummarizeZoomsMinSubset(t *testing.T) {
	reports := []zoomReport{
		{convMin: 14, convMax: 16, availMin: 12, availMax: 16}, // subset (min only)
	}
	_, hint := summarizeZooms(reports)
	if !strings.Contains(hint, "-minzoom 12") {
		t.Errorf("hint should suggest -minzoom 12, got %q", hint)
	}
	if strings.Contains(hint, "-maxzoom") {
		t.Errorf("hint should not mention -maxzoom when only the min is a subset, got %q", hint)
	}
}

func TestSummarizeZoomsEmpty(t *testing.T) {
	lines, hint := summarizeZooms(nil)
	if lines != nil || hint != "" {
		t.Errorf("empty input should give empty output, got lines=%v hint=%q", lines, hint)
	}
}
