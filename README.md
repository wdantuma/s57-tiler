
## s57-tiler

S57-tiler creates vectortiles from S57 ENC's wich can be used with freeboard-sk with s57 support see [https://github.com/wdantuma/freeboard-sk/tree/feat-S57-support](https://github.com/wdantuma/freeboard-sk/tree/feat-S57-support)


## Dependencies

go 1.20

GDAL 3.6.2

## Get started

```
make builds57tiler
```

```
./build/s57-tiler --in <path to directory tree container catalog.031 files> --out ./static/charts
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
