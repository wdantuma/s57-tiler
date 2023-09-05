VERSION=0.0.1

build/s57-tiler:
	GOARCH=amd64 GOOS=linux go build -o build/s57-tiler  ./cmd/s57-tiler	


build: build/s57-tiler

runs57tiler: build/s57-tiler
	./build/s57-tiler

clean:
	go clean
	rm build/*
