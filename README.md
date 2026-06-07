# s57-tiler

![Go](https://img.shields.io/badge/Go-1.22-00ADD8?logo=go&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-wdantuma%2Fs57--tiler-2496ED?logo=docker&logoColor=white)

s57-tiler converts S-57 ENC nautical charts into Mapbox Vector Tiles (`.pbf`) for use with the Signal K [freeboard-sk](https://github.com/SignalK/freeboard-sk) chart plotter.

It reads a directory of **unencrypted** S-57 ENCs (`catalog.031` / `.000` files) and writes a tree of vector tiles plus metadata. Encrypted S-63 charts are not supported. The output directory is added as a chart path in the [Signal K charts plugin](https://www.npmjs.com/package/@signalk/charts-plugin).

## Quick start (Docker)

1. Download an S-57 ENC. See [OpenCPN's chart sources](https://opencpn.org/OpenCPN/info/chartsource.html) for options.

2. Create a working directory with `enc` and `charts` subdirectories (names are case-sensitive), and extract the downloaded ENC into `enc`:

   ```text
   signalk-charts/
     enc/
     charts/
   ```

3. Run the tiler:

   ```bash
   docker run -v ./signalk-charts:/app/workdir wdantuma/s57-tiler:latest \
     /app/s57-tiler --in workdir/enc --out workdir/charts
   ```

Processing may take a while. When it finishes, `signalk-charts/charts` holds the vector tiles — add it as a chart path in the Signal K charts plugin.

## Usage

```bash
s57-tiler --in <directory of S-57 ENCs> --out <output directory>
```

| Flag | Default | Description |
|------|---------|-------------|
| `-in` | `./charts` | Directory tree of S-57 ENCs (contains `catalog.031`) |
| `-out` | `./static/charts` | Output directory for vector tiles |
| `-minzoom` | `9` | Minimum zoom level |
| `-maxzoom` | `14` | Maximum zoom level |
| `-bounds` | — | Limit output to a bounding box: `W,N,E,S` |
| `-at` | — | Generate only the tile at `lon,lat` |
| `-workers` | CPUs − 1 | Number of parallel tile workers |
| `-debug` | `false` | Show debug info (don't suppress GDAL errors) |

## Building from source

**Requirements:** Go 1.22 and the GDAL development headers (found via `pkg-config`).

```bash
# macOS
brew install gdal

# Debian / Ubuntu / Raspberry Pi OS
sudo apt-get install -y libgdal-dev build-essential
```

Build with the bundled Makefile:

| Command | Output |
|---------|--------|
| `make build` | Host-native binary |
| `make darwin-arm64` | macOS Apple Silicon (M1 baseline, runs on all M-series) |
| `make linux-arm64` | 64-bit ARM Linux (Raspberry Pi, Graviton, other SBCs) |
| `make docker-buildx` | Multi-arch Docker image (linux/amd64 + linux/arm64) |

## Testing & benchmarks

```bash
make test    # unit tests (needs the bundled enc/ fixture + GDAL)
make bench   # performance benchmarks over the tiling hot path
```

Performance is benchmarked to catch regressions. See
[docs/benchmarking.md](docs/benchmarking.md) for the suite, how to compare two runs
with `benchstat`, and the CI trend tracking.
