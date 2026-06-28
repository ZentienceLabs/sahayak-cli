BINARY  := sahayak
PKG     := github.com/zenlabs/sahayak
VERSION ?= 0.1.0-dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(PKG)/core/version.Version=$(VERSION) \
	-X $(PKG)/core/version.Commit=$(COMMIT) \
	-X $(PKG)/core/version.Date=$(DATE)

# Phase 1 is CGO-free on purpose (clean cross-compile, air-gap friendly).
export CGO_ENABLED = 0

.PHONY: build test vet fmt run clean

build: ## build the sahayak binary into ./bin
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/sahayak

test: ## run unit tests
	go test ./...

vet: ## static checks
	go vet ./...

fmt: ## format
	gofmt -w .

run: build ## build then print help
	./bin/$(BINARY) help

clean:
	rm -rf bin
