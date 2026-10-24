.DEFAULT_GOAL := check

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/bitbynik/exon/internal/cli.version=$(VERSION)

.PHONY: build test lint fmt tidy check clean

## build: static darwin/arm64 binary at bin/exon (run it on the Mac)
build:
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/exon ./cmd/exon

## test: unit tests with the race detector (needs cgo; only the release build is cgo-free)
test:
	CGO_ENABLED=1 go test -race ./...

## lint: golangci-lint
lint:
	golangci-lint run ./...

## fmt: format all Go files
fmt:
	gofumpt -w .

## tidy: sync go.mod / go.sum
tidy:
	go mod tidy

## check: everything CI runs
check: lint test build

clean:
	rm -rf bin dist
