FROM ghcr.io/osgeo/gdal:ubuntu-small-3.9.1 AS build

# Build inside the GDAL image so the binary links against the same GDAL it runs
# against. Use a pinned Go toolchain (overridable) rather than the distro's
# golang-go, which can lag go.mod's required version. TARGETARCH is provided by
# buildx for multi-arch builds; it defaults to amd64 for a plain `docker build`.
ARG GO_VERSION=1.22.5
ARG TARGETARCH=amd64
RUN apt-get update && \
    apt-get install -y --no-install-recommends build-essential git ca-certificates wget && \
    rm -rf /var/lib/apt/lists/*
RUN wget -qO- https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz | tar -C /usr/local -xz
ENV PATH=/usr/local/go/bin:$PATH

WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o build/s57-tiler ./cmd/s57-tiler
FROM ghcr.io/osgeo/gdal:ubuntu-small-3.9.1
WORKDIR /app
COPY --from=build /app/build/s57-tiler /app/s57-tiler
CMD [ "/app/s57-tiler", "--help"]