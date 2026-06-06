VERSION=0.0.1
IMAGE ?= wdantuma/s57-tiler:latest

# Native host build (links the locally-installed GDAL via cgo/pkg-config).
build/s57-tiler:
	go build -o build/s57-tiler ./cmd/s57-tiler

build: build/s57-tiler

# Explicit Linux/amd64 cross artifact (requires a matching cgo+GDAL toolchain).
build/s57-tiler-linux-amd64:
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" \
		-o build/s57-tiler-linux-amd64 ./cmd/s57-tiler

# Native Raspberry Pi / linux-arm64 build. Run on a 64-bit Pi with libgdal-dev installed.
build/s57-tiler-linux-arm64:
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" \
		-o build/s57-tiler-linux-arm64 ./cmd/s57-tiler

linux-arm64: build/s57-tiler-linux-arm64

# Multi-arch Docker image (amd64 + arm64/Raspberry Pi). Use --load for a single local arch.
docker-buildx:
	docker buildx build --platform linux/amd64,linux/arm64 -t $(IMAGE) --push .

runs57tiler: build/s57-tiler
	./build/s57-tiler

clean:
	go clean
	rm build/*
