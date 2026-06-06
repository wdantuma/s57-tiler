
# s57-tiler

S57-tiler creates vectortiles from S57 ENC's which can be used with freeboard-sk see [https://github.com/SignalK/freeboard-sk](https://https://github.com/SignalK/freeboard-sk)

## Quick start ( using docker)


download a S57 ENC ( see [https://opencpn.org/OpenCPN/info/chartsource.html](https://opencpn.org/OpenCPN/info/chartsource.html) for a list of possible sources), only unencrypted S57 ENC's are supported ( no S63 ).

Create a directory somewhere ( eg "signalk-charts" ) with the subdirectories "enc" and "charts"  ( case sensitive )

```
signalk-charts
   enc
   charts
```

Extract the downloaded S57 ENC (zip) in the enc directory and run

```
docker run -v  ./signalk-charts:/app/workdir wdantuma/s57-tiler:latest  /app/s57-tiler --in workdir/enc --out workdir/charts
```

After processing ( may take some time ) the directory charts contains the vectortiles, the directory ```signalk-charts/charts``` should be added as a "chart path" in the [Signal K charts plugin](https://www.npmjs.com/package/@signalk/charts-plugin)





## Development



### Dependencies

go 1.22

GDAL (development headers, discovered via `pkg-config`)

- macOS: `brew install gdal`
- Debian/Ubuntu/Raspberry Pi OS: `sudo apt-get install -y libgdal-dev build-essential`

### Build

Native build for the host platform (macOS arm64, linux/amd64, linux/arm64, …):

```
make build
```

### macOS (Apple Silicon)

```
brew install gdal
make darwin-arm64
./build/s57-tiler-darwin-arm64 --in ./enc --out ./static/charts
```

The binary targets the M1 baseline, so it runs on all M-series Macs (M1–M4 and later)
regardless of which Mac built it.

### Raspberry Pi (64-bit / arm64)

Build natively on a 64-bit Raspberry Pi OS:

```
sudo apt-get update && sudo apt-get install -y libgdal-dev build-essential golang
make linux-arm64
./build/s57-tiler-linux-arm64 --in ./enc --out ./static/charts
```

Or just `docker run ... wdantuma/s57-tiler:latest` — the published image is multi-arch
(linux/amd64 + linux/arm64), so the same command in the quickstart works on a Pi.

To build and publish the multi-arch image yourself:

```
make docker-buildx          # builds linux/amd64 + linux/arm64 and pushes
```

```
./build/s57-tiler --in <path to directory tree containing catalog.031 files> --out ./static/charts
```

More options
```
$ build/s57-tiler --help
Usage of build/s57-tiler:
  -at string
        lon,lat
  -bounds string
        W,N,E,S
  -in string
        Input path S-57 ENC's (default "./charts")
  -maxzoom int
        Max zoom (default 14)
  -minzoom int
        Min zoom (default 9)
  -out string
        Output directory for vector tiles (default "./static/charts")
  -workers int
        Number of parallel tile workers (default: number of CPUs - 1)
```
