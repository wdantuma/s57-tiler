package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	m "github.com/wdantuma/s57-tiler/s57/mercantile"
)

// maxSupportedZoom is the deepest zoom the mercantile.Scale table defines; beyond
// it the SCAMIN/SCAMAX filtering degrades, so explicit flags are clamped to it.
const maxSupportedZoom = 23

// clampZoomValue clamps a zoom flag into [0, maxSupportedZoom] and returns a note
// (empty when in range) explaining any adjustment.
func clampZoomValue(name string, z int) (int, string) {
	switch {
	case z < 0:
		return 0, fmt.Sprintf("Note: -%s %d is below 0; clamped to 0.", name, z)
	case z > maxSupportedZoom:
		return maxSupportedZoom, fmt.Sprintf("Note: -%s %d exceeds the supported maximum z%d; clamped.", name, z, maxSupportedZoom)
	}
	return z, ""
}

// validateZoomRange rejects an inverted range, which would silently generate zero
// tiles and exit "successfully".
func validateZoomRange(minZoom, maxZoom int) error {
	if minZoom > maxZoom {
		return fmt.Errorf("-minzoom %d is greater than -maxzoom %d; nothing would be generated", minZoom, maxZoom)
	}
	return nil
}

// parseBounds parses a "W,N,E,S" bounds string and validates that it is a sane
// geographic box (four numbers, lon in [-180,180], lat in [-90,90], W<E, S<N) so
// a reversed or out-of-range box doesn't silently yield empty output.
func parseBounds(s string) (m.Extrema, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return m.Extrema{}, fmt.Errorf("bounds must be W,N,E,S (4 comma-separated numbers), got %q", s)
	}
	vals := make([]float64, 4)
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return m.Extrema{}, fmt.Errorf("bounds value %q is not a number", p)
		}
		vals[i] = v
	}
	b := m.Extrema{W: vals[0], N: vals[1], E: vals[2], S: vals[3]}
	if b.W < -180 || b.W > 180 || b.E < -180 || b.E > 180 {
		return m.Extrema{}, fmt.Errorf("bounds longitude out of range [-180,180]: W=%g E=%g", b.W, b.E)
	}
	if b.N < -90 || b.N > 90 || b.S < -90 || b.S > 90 {
		return m.Extrema{}, fmt.Errorf("bounds latitude out of range [-90,90]: N=%g S=%g", b.N, b.S)
	}
	if b.W >= b.E {
		return m.Extrema{}, fmt.Errorf("bounds west (%g) must be less than east (%g)", b.W, b.E)
	}
	if b.S >= b.N {
		return m.Extrema{}, fmt.Errorf("bounds south (%g) must be less than north (%g)", b.S, b.N)
	}
	return b, nil
}

// ensureWritableDir creates the output directory if needed and probes that it is
// writable, so a bad -out fails immediately instead of after the (possibly long)
// scan and partway into tiling.
func ensureWritableDir(path string) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("output directory %q: %w", path, err)
	}
	probe := filepath.Join(path, ".s57-tiler-write-test")
	if err := os.WriteFile(probe, []byte{}, 0644); err != nil {
		return fmt.Errorf("output directory %q is not writable: %w", path, err)
	}
	os.Remove(probe)
	return nil
}
