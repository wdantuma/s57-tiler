package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func durSeconds(n int) time.Duration { return time.Duration(n) * time.Second }

func TestFmtDuration(t *testing.T) {
	cases := []struct {
		secs int
		want string
	}{
		{0, "0:00"},
		{5, "0:05"},
		{65, "1:05"},
		{600, "10:00"},
	}
	for _, c := range cases {
		got := fmtDuration(durSeconds(c.secs))
		if got != c.want {
			t.Errorf("fmtDuration(%ds) = %q, want %q", c.secs, got, c.want)
		}
	}
	if got := fmtDuration(durSeconds(-3)); got != "0:00" {
		t.Errorf("negative duration = %q, want 0:00", got)
	}
}

// TestProgressNonTTY verifies that non-TTY output emits milestone lines with
// newlines (no carriage returns) and a final summary.
func TestProgressNonTTY(t *testing.T) {
	var buf bytes.Buffer
	p := newProgressWriter(10, &buf, false)

	for i := 0; i < 10; i++ {
		p.inc()
		p.render(false)
	}
	p.render(true)

	out := buf.String()
	if strings.Contains(out, "\r") {
		t.Errorf("non-TTY output must not contain carriage returns, got:\n%q", out)
	}
	if !strings.Contains(out, "100%") && !strings.Contains(out, "Done") {
		t.Errorf("expected a completion line, got:\n%q", out)
	}
	if !strings.Contains(out, "Done - Tiles: 10/10") {
		t.Errorf("expected final summary, got:\n%q", out)
	}
	for _, label := range []string{"Progress", "Tiles", "Rate", "ETA"} {
		if !strings.Contains(out, label) {
			t.Errorf("expected %q label in output, got:\n%q", label, out)
		}
	}
	// Milestones should be emitted at most once each: lines are deduped by 10%.
	if got := strings.Count(out, "100%"); got > 1 {
		t.Errorf("milestone 100%% printed %d times, want <=1", got)
	}
}

// TestProgressTTY verifies that terminal output uses carriage returns and ends
// with a single trailing newline from finish/final render.
func TestProgressTTY(t *testing.T) {
	var buf bytes.Buffer
	p := newProgressWriter(4, &buf, true)
	p.inc()
	p.inc()
	p.render(false)
	p.render(true)

	out := buf.String()
	if !strings.Contains(out, "\r") {
		t.Errorf("TTY output should use carriage returns, got:\n%q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("final TTY render should end with newline, got:\n%q", out)
	}
	if !strings.Contains(out, "100%") {
		t.Errorf("final render should show 100%%, got:\n%q", out)
	}
}

// TestProgressZeroTotal guards the divide-by-zero / empty-input case.
func TestProgressZeroTotal(t *testing.T) {
	var buf bytes.Buffer
	p := newProgressWriter(0, &buf, true)
	p.render(true) // must not panic
	if !strings.Contains(buf.String(), "100%") {
		t.Errorf("zero-total final render should show 100%%, got:\n%q", buf.String())
	}
}
