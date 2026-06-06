package main

import (
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// progress is a thread-safe, single-writer progress reporter. Workers only call
// inc()/setStage(); a dedicated goroutine (run) owns all output, so concurrent
// tiling never interleaves or flickers the terminal.
type progress struct {
	total int64
	done  int64 // atomic
	start time.Time
	out   io.Writer
	isTTY bool

	mu    sync.Mutex
	stage string

	stop        chan struct{}
	doneCh      chan struct{}
	lastMilePct int // last milestone printed in non-TTY mode
}

func newProgress(total int64, out *os.File) *progress {
	return newProgressWriter(total, out, isTerminal(out))
}

// newProgressWriter is the testable core: it takes any io.Writer and an explicit
// isTTY flag instead of detecting it from a *os.File.
func newProgressWriter(total int64, out io.Writer, isTTY bool) *progress {
	return &progress{
		total:       total,
		start:       time.Now(),
		out:         out,
		isTTY:       isTTY,
		stop:        make(chan struct{}),
		doneCh:      make(chan struct{}),
		lastMilePct: -1,
	}
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func (p *progress) inc() { atomic.AddInt64(&p.done, 1) }

func (p *progress) setStage(s string) {
	p.mu.Lock()
	p.stage = s
	p.mu.Unlock()
}

// run renders on a ticker until finish() is called. Non-TTY output is throttled
// to milestone lines so logs stay readable.
func (p *progress) run() {
	defer close(p.doneCh)
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-ticker.C:
			p.render(false)
		}
	}
}

// finish stops the reporter and renders the final 100% summary line.
func (p *progress) finish() {
	close(p.stop)
	<-p.doneCh
	p.render(true)
}

func (p *progress) render(final bool) {
	done := atomic.LoadInt64(&p.done)
	total := p.total

	var pct float64
	if total > 0 {
		pct = float64(done) / float64(total) * 100
	} else {
		pct = 100
	}
	if final {
		pct = 100
		done = total
	}

	elapsed := time.Since(p.start)
	var rate float64
	if elapsed.Seconds() > 0 {
		rate = float64(done) / elapsed.Seconds()
	}
	var eta time.Duration
	if rate > 0 && done < total {
		eta = time.Duration(float64(total-done)/rate) * time.Second
	}

	p.mu.Lock()
	stage := p.stage
	p.mu.Unlock()

	if p.isTTY {
		line := fmt.Sprintf("Progress: %3.0f%%,  Tiles: %d/%d,  Rate: %.0f/s,  Elapsed: %s,  ETA %s",
			pct, done, total, rate, fmtDuration(elapsed), fmtDuration(eta))
		if stage != "" {
			line += "  Chart: " + stage
		}
		// trailing spaces clear any leftovers from a previously longer line
		fmt.Fprintf(p.out, "\r%-90s", line)
		if final {
			fmt.Fprintln(p.out)
		}
		return
	}

	// Non-TTY: only emit on a 10% milestone change, plus the final summary.
	mile := int(pct) / 10 * 10
	if final {
		fmt.Fprintf(p.out, "Done - Tiles: %d/%d, Elapsed: %s, Rate: %.0f/s\n",
			done, total, fmtDuration(elapsed), rate)
		return
	}
	// render() only ever runs in the single reporter goroutine, so lastMilePct
	// needs no synchronization.
	if mile != p.lastMilePct {
		p.lastMilePct = mile
		fmt.Fprintf(p.out, "Progress: %d%%, Tiles: %d/%d, Rate: %.0f/s, ETA: %s\n",
			mile, done, total, rate, fmtDuration(eta))
	}
}

func fmtDuration(d time.Duration) string {
	if d <= 0 {
		return "0:00"
	}
	d = d.Round(time.Second)
	m := int(d / time.Minute)
	s := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%d:%02d", m, s)
}
