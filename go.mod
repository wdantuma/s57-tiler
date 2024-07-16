module github.com/wdantuma/s57-tiler

go 1.22

toolchain go1.22.2

require (
	github.com/lukeroth/gdal v0.0.0-20230818145556-62d5095a1cda
	github.com/tburke/iso8211 v0.0.0-20190905204635-916caaad4cc1
	github.com/wdantuma/signalk-server-go v0.0.0-20230905090637-c7200927141e
	google.golang.org/protobuf v1.31.0
)

replace github.com/lukeroth/gdal => github.com/wdantuma/gdal v0.0.0-20240715134249-3d7dd40d7ca1
