FROM ghcr.io/osgeo/gdal:ubuntu-small-3.9.1 AS build
RUN apt-get update 
RUN apt-get -y install golang-go build-essential git
WORKDIR /app
COPY . .
RUN go mod download
RUN GOARCH=amd64 GOOS=linux go build -o build/s57-tiler  ./cmd/s57-tiler
FROM ghcr.io/osgeo/gdal:ubuntu-small-3.9.1
WORKDIR /app
COPY --from=build /app/build/s57-tiler /app/s57-tiler
CMD [ "/app/s57-tiler", "--help"]