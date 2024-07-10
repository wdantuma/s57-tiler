
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
docker run -v  ./signalk-charts:/app/workdir wdantuma/s57-tiler  /app/s57-tiler --in workdir/enc --out workdir/charts
```

After processing ( may take some time ) the directory charts contains the vectortiles, the directory ```signalk-charts/charts``` should be added as a "chart path" in the [Signal K charts plugin](https://www.npmjs.com/package/@signalk/charts-plugin)





## Development



### Dependencies

go 1.20

GDAL 3.6.2

### Build

```
make builds57tiler
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
        Min zoom (default 14)
  -out string
        Output directory for vector tiles (default "./static/charts")
```
