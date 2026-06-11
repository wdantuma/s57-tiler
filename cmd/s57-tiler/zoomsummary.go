package main

import "fmt"

// zoomReport records, for one cell, the zoom range that will be generated
// (conv*) and the range the cell's data supports (avail*, the native detail
// ceiling from the compilation scale).
type zoomReport struct {
	convMin, convMax   int
	availMin, availMax int
}

// isSubset reports whether the converting range omits zoom levels the data
// supports — finer detail above (convMax<availMax) or coarser views below
// (convMin>availMin).
func (r zoomReport) isSubset() bool {
	return r.convMin > r.availMin || r.convMax < r.availMax
}

// summarizeZooms groups the reports by identical (converting, available) range
// and returns one summary line per group plus a hint. The hint is empty unless
// at least one cell omits available zoom levels; when present it names only the
// flag(s) (-minzoom/-maxzoom) that would widen the range, using the envelope
// across all subset cells. Groups are emitted in first-seen order, so output is
// deterministic for a given input.
// plural returns singular for n==1 and the simple "+s" plural otherwise.
func plural(n int, singular string) string {
	if n == 1 {
		return singular
	}
	return singular + "s"
}

func summarizeZooms(reports []zoomReport) (lines []string, hint string) {
	if len(reports) == 0 {
		return nil, ""
	}

	type group struct {
		rep   zoomReport
		count int
	}
	var order []string
	groups := map[string]*group{}
	for _, r := range reports {
		key := fmt.Sprintf("%d-%d/%d-%d", r.convMin, r.convMax, r.availMin, r.availMax)
		g, ok := groups[key]
		if !ok {
			g = &group{rep: r}
			groups[key] = g
			order = append(order, key)
		}
		g.count++
	}
	for _, key := range order {
		g := groups[key]
		r := g.rep
		line := fmt.Sprintf("  %d %s: converting z%d-z%d", g.count, plural(g.count, "chart"), r.convMin, r.convMax)
		if r.isSubset() {
			line += fmt.Sprintf("  (data available z%d-z%d)", r.availMin, r.availMax)
		}
		lines = append(lines, line)
	}

	minSubset, maxSubset := false, false
	hintMin := int(^uint(0) >> 1)
	hintMax := -1
	for _, r := range reports {
		if r.convMax < r.availMax {
			maxSubset = true
			if r.availMax > hintMax {
				hintMax = r.availMax
			}
		}
		if r.convMin > r.availMin {
			minSubset = true
			if r.availMin < hintMin {
				hintMin = r.availMin
			}
		}
	}
	switch {
	case minSubset && maxSubset:
		hint = fmt.Sprintf("The selected zoom omits detail some charts contain; "+
			"pass -minzoom %d -maxzoom %d for their full native range (more tiles).", hintMin, hintMax)
	case maxSubset:
		hint = fmt.Sprintf("The selected zoom omits finer detail some charts contain; "+
			"pass -maxzoom %d to tile their full native range (more tiles).", hintMax)
	case minSubset:
		hint = fmt.Sprintf("The selected zoom omits coarser levels some charts cover; "+
			"pass -minzoom %d to include them.", hintMin)
	}
	return lines, hint
}
