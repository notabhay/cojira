BINARY  := cojira
MODULE  := github.com/notabhay/cojira
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0")
LDFLAGS := -ldflags "-X $(MODULE)/internal/version.Version=$(VERSION)"

.PHONY: build test lint vet install clean bundle-local bundle-local-windows bundle-local-windows-arm bundle-local-linux bundle-local-linux-arm bundle-local-darwin bundle-local-darwin-arm

build:
	go build $(LDFLAGS) -o $(BINARY) .

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

lint:
	golangci-lint run

install:
	go install $(LDFLAGS) .

clean:
	rm -f $(BINARY)

# Cross-compilation targets
build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY)-linux-amd64 .

build-linux-arm:
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY)-linux-arm64 .

build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY)-windows-amd64.exe .

build-all: build build-linux build-linux-arm build-windows

bundle-local:
	./scripts/build_local_bundle.sh

bundle-local-windows:
	./scripts/build_local_bundle.sh "$(VERSION)" windows amd64

bundle-local-windows-arm:
	./scripts/build_local_bundle.sh "$(VERSION)" windows arm64

bundle-local-linux:
	./scripts/build_local_bundle.sh "$(VERSION)" linux amd64

bundle-local-linux-arm:
	./scripts/build_local_bundle.sh "$(VERSION)" linux arm64

bundle-local-darwin:
	./scripts/build_local_bundle.sh "$(VERSION)" darwin amd64

bundle-local-darwin-arm:
	./scripts/build_local_bundle.sh "$(VERSION)" darwin arm64
