VERSION=0.0.3
IMAGE ?= wdantuma/s57-tiler:latest

# All Go sources (plus module files) — build targets depend on these so they
# rebuild when code changes instead of being treated as permanently up-to-date.
GOFILES := $(shell find . -name '*.go') go.mod go.sum

.PHONY: build linux-arm64 darwin-arm64 docker-buildx runs57tiler test bench clean

# Native host build (links the locally-installed GDAL via cgo/pkg-config).
build/s57-tiler: $(GOFILES)
	go build -o build/s57-tiler ./cmd/s57-tiler

build: build/s57-tiler

# Explicit Linux/amd64 cross artifact (requires a matching cgo+GDAL toolchain).
build/s57-tiler-linux-amd64: $(GOFILES)
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" \
		-o build/s57-tiler-linux-amd64 ./cmd/s57-tiler

# Native linux/arm64 (aarch64) build — any 64-bit ARM Linux host (Raspberry Pi, AWS
# Graviton, other SBCs/servers). Build on the target arch (or under emulation) with
# libgdal-dev installed; this is a cgo build, not a cross-compile from x86.
build/s57-tiler-linux-arm64: $(GOFILES)
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" \
		-o build/s57-tiler-linux-arm64 ./cmd/s57-tiler

linux-arm64: build/s57-tiler-linux-arm64

# Homebrew GDAL prefix — deferred (=) so non-macOS targets never invoke brew.
GDAL_PREFIX = $(shell brew --prefix gdal 2>/dev/null)

# Native macOS / Apple Silicon (M1–M4+) release build. Requires `brew install gdal`.
build/s57-tiler-darwin-arm64: $(GOFILES)
	@if [ -z "$(GDAL_PREFIX)" ]; then \
		echo "Homebrew GDAL not found. Run: brew install gdal"; exit 1; \
	fi
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 \
	CGO_CFLAGS="-I$(GDAL_PREFIX)/include -O3 -mcpu=apple-m1 -flto=thin" \
	CGO_CXXFLAGS="-I$(GDAL_PREFIX)/include -O3 -mcpu=apple-m1 -flto=thin" \
	CGO_LDFLAGS="-L$(GDAL_PREFIX)/lib -lgdal -Wl,-rpath,$(GDAL_PREFIX)/lib" \
	go build -trimpath -buildmode=pie -ldflags="-s -w" \
		-o build/s57-tiler-darwin-arm64 ./cmd/s57-tiler

darwin-arm64: build/s57-tiler-darwin-arm64

# Multi-arch Docker image (amd64 + arm64/Raspberry Pi). Use --load for a single local arch.
docker-buildx:
	docker buildx build --platform linux/amd64,linux/arm64 -t $(IMAGE) --push .

runs57tiler: build/s57-tiler
	./build/s57-tiler

# Run unit tests (needs the bundled enc/ fixture and a working GDAL toolchain).
test:
	go test ./...

# Run performance benchmarks. allocs/op and B/op are the stable, machine-independent
# regression signals; ns/op is directional. For a before/after comparison capture two
# runs and diff them with benchstat:
#   make bench | tee /tmp/new.txt   # then: benchstat /tmp/old.txt /tmp/new.txt
bench:
	go test -run='^$$' -bench=. -benchmem -count=10 ./s57/...

clean:
	go clean
	rm -f build/*
