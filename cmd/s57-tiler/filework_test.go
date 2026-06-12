package main

import (
	"context"
	"testing"
	"time"

	m "github.com/wdantuma/s57-tiler/s57/mercantile"
)

func TestFileWorkTileCount(t *testing.T) {
	world := m.Extrema{W: -179, N: 85, E: 179, S: -85}
	if got := (fileWork{minzoom: 0, maxzoom: 0, extents: []m.Extrema{world}}).tileCount(); got != 1 {
		t.Errorf("tileCount(world, z0) = %d, want 1", got)
	}
	// -at mode: exactly one tile regardless of the zoom range.
	tile := m.TileID{X: 1, Y: 2, Z: 3}
	if got := (fileWork{single: &tile, minzoom: 0, maxzoom: 5}).tileCount(); got != 1 {
		t.Errorf("tileCount(single) = %d, want 1", got)
	}
}

// streamTiles must return promptly when the context is cancelled rather than block
// forever on a full channel, so Ctrl-C stops a run cleanly.
func TestFileWorkStreamTilesStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fw := fileWork{minzoom: 0, maxzoom: 5, extents: []m.Extrema{{W: -179, N: 85, E: 179, S: -85}}}
	jobs := make(chan m.TileID) // unbuffered, no consumer -> sends would block
	done := make(chan struct{})
	go func() {
		fw.streamTiles(ctx, jobs)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("streamTiles did not stop on a cancelled context")
	}
}
