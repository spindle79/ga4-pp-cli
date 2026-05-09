.PHONY: build test lint install clean

build:
	go build -o bin/ga4-pp-cli ./cmd/ga4-pp-cli

test:
	go test ./...

lint:
	golangci-lint run

install:
	go install ./cmd/ga4-pp-cli

clean:
	rm -rf bin/

build-mcp:
	go build -o bin/ga4-pp-mcp ./cmd/ga4-pp-mcp

install-mcp:
	go install ./cmd/ga4-pp-mcp

build-all: build build-mcp
