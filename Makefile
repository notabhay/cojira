BINARY  := cojira
MODULE  := github.com/notabhay/cojira
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0")
LDFLAGS := -ldflags "-X $(MODULE)/internal/version.Version=$(VERSION)"

.PHONY: build test lint vet install clean

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
