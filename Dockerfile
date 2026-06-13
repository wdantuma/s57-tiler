FROM ghcr.io/osgeo/gdal:ubuntu-small-3.9.1 AS build

# Build inside the GDAL image so the binary links against the same GDAL it runs
# against. Use a pinned Go toolchain (overridable) rather than the distro's
# golang-go, which can lag go.mod's required version.
#
# TARGETARCH is an automatic platform ARG buildx injects per target leg. Do NOT
# give it a default: a default value *shadows* buildx's injected value
# (docker/buildx#510), pinning every leg to one arch. That silently downloaded an
# amd64 Go toolchain on the arm64 leg, so cgo handed the x86-only `-m64` flag to
# the native aarch64 gcc ("gcc: unrecognized command-line option '-m64'"). Declare
# it bare and fall back to the container's native arch (via dpkg) only when it is
# genuinely empty, e.g. under the legacy DOCKER_BUILDKIT=0 builder.
ARG GO_VERSION=1.26.0
ARG TARGETARCH
# pkg-config resolves the gdal binding's `#cgo pkg-config: gdal` directive. It used
# to arrive transitively via golang-go; now that Go is a pinned tarball it must be
# installed explicitly, or the cgo build fails with "pkg-config: not found".
RUN apt-get update && \
    apt-get install -y --no-install-recommends build-essential pkg-config git ca-certificates wget && \
    rm -rf /var/lib/apt/lists/*
RUN ARCH="${TARGETARCH:-$(dpkg --print-architecture)}" && \
    wget -qO- "https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz" | tar -C /usr/local -xz
ENV PATH=/usr/local/go/bin:$PATH

WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o build/s57-tiler ./cmd/s57-tiler
FROM ghcr.io/osgeo/gdal:ubuntu-small-3.9.1
WORKDIR /app
COPY --from=build /app/build/s57-tiler /app/s57-tiler
CMD [ "/app/s57-tiler", "--help"]