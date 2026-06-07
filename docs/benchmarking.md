# Benchmarking & performance regressions

The tiler has a Go benchmark suite over the tile-generation hot path
(`s57/bench_test.go`). It exists to catch performance regressions — both locally
(via `benchstat`) and in CI (results are tracked over time and alert on
regression).

The benchmarks run against the committed `enc/US4WA1JJ` fixture, so inputs are
deterministic. They **skip** when the fixture or GDAL is unavailable, so they never
break a fixtureless checkout.

## What is benchmarked

| Benchmark | Covers |
|---|---|
| `BenchmarkGenerateTile` | Full per-tile pipeline on one dense z14 tile — the work a tiling worker actually does (per-layer datasource open, spatial filter, feature iteration, geometry simplification, MVT encode, `.pbf` write). The headline end-to-end number. |
| `BenchmarkToMvtGeometry/{polygon,line,point}` | Geometry simplification (`SimplifyPreservingTopology`) + MVT encoding in isolation, on the densest real feature of each type. Low-noise; sharpest signal for geometry-path changes. |
| `BenchmarkGetFeatures` | GDAL read path: spatial filter, feature iteration, SCAMIN/SCAMAX gating, attribute conversion — without the `.pbf` write. |
| `BenchmarkGetS57Datasets` | CLI startup: catalog parse + one GDAL open per `.000` to enumerate layers. |

## Running

```bash
make bench
# equivalently:
go test -run='^$' -bench=. -benchmem -count=10 ./s57/...
```

Requirements: a working GDAL toolchain (same as building — `brew install gdal` /
`apt-get install libgdal-dev`) and the committed `enc/` fixture.

## Reading the results

```
BenchmarkGenerateTile-16    1    878081917 ns/op    1698840 B/op    126920 allocs/op
```

- **`allocs/op` and `B/op` are the reliable regression signals** — they are
  deterministic and identical across machines and GDAL patch versions. Watch these
  first.
- **`ns/op` is directional.** It is meaningful on a quiet local machine for an A/B
  comparison, but noisy on shared CI runners — don't read small ns/op moves as real.

Note on magnitude: `BenchmarkGenerateTile` is ~0.9 s/op because `GenerateTile`
reopens the GDAL datasource once **per layer** per tile. That per-tile fixed cost is
real (and a natural optimization target); it's also why there is no whole-cell
benchmark — it would take tens of seconds per op. The single dense tile already
captures the per-tile cost.

## Comparing two runs (local)

```bash
go install golang.org/x/perf/cmd/benchstat@latest

# baseline (e.g. on master or before your change)
git switch master
make bench | tee /tmp/old.txt

# your change
git switch -
make bench | tee /tmp/new.txt

benchstat /tmp/old.txt /tmp/new.txt
```

`benchstat` reports the delta and whether it is statistically significant. Compare
runs taken **on the same machine** — never compare numbers across machines or GDAL
versions.

## CI trend tracking

`.github/workflows/benchmarks.yml` runs the suite in a **pinned `osgeo/gdal`
container** (so the GDAL version can't drift and masquerade as a regression):

- **push to `master`** — appends results to the `gh-pages` branch, building a trend
  over time.
- **pull requests** — runs the suite and comments a comparison; alerts if a
  benchmark regresses beyond the threshold (currently 150%). It is an alert, not a
  hard gate (`fail-on-alert: false`).

Because shared CI runners are noisy, the alert threshold is intentionally generous
and `allocs/op` / `B/op` remain the dependable signals. Bump the pinned GDAL image
tag deliberately — a GDAL change can legitimately shift `ns/op`, which is an
environment change rather than a code regression.
