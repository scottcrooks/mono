GO ?= go
BINARY ?= mono
GOBIN ?= $(CURDIR)/bin
GO_ENV_STAMP = .cache/.go-env.stamp
# Use workspace-local Go state for sandbox-friendly builds/toolchain downloads.
export GOCACHE := $(CURDIR)/.cache/go-build
export GOPATH := $(CURDIR)/.cache/go
export GOMODCACHE := $(GOPATH)/pkg/mod
export GOTOOLCHAIN ?= auto
GOFILES := $(shell find . -type f -name '*.go' -not -path './vendor/*' -not -path './.cache/*')
MONO_SRCS := $(shell find cmd/mono internal -type f -name '*.go')
MONO_EXTRA_DEPS := $(wildcard services.yaml)
VERSION ?= $(shell git describe --tags --exact-match 2>/dev/null || git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/scottcrooks/mono/internal/version.Version=$(VERSION) -X github.com/scottcrooks/mono/internal/version.Commit=$(COMMIT) -X github.com/scottcrooks/mono/internal/version.Date=$(DATE)

.PHONY: build test lint fix fmt fmt-check install-local smoke check

build: bin/$(BINARY)

.cache/.go-env.stamp:
	mkdir -p .cache $(GOCACHE) $(GOMODCACHE) $(GOPATH)/bin
	touch $@

# Auto-rebuild mono when source or config changes
bin/$(BINARY): $(GO_ENV_STAMP) $(MONO_SRCS) go.mod Makefile $(MONO_EXTRA_DEPS)
	mkdir -p bin
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/mono

test:
	$(GO) test ./...

lint:
	$(GO) vet ./...

fix:
	$(GO) fix ./...

fmt:
	@if [ -n "$(GOFILES)" ]; then gofmt -w $(GOFILES); fi

fmt-check:
	@if [ -n "$(GOFILES)" ]; then test -z "$$(gofmt -l $(GOFILES))"; fi

install-local:
	mkdir -p bin
	GOBIN=$(GOBIN) $(GO) install -ldflags "$(LDFLAGS)" ./cmd/mono

smoke: build
	./bin/$(BINARY) --help >/dev/null
	./bin/$(BINARY) --version >/dev/null
	./bin/$(BINARY) metadata >/dev/null

check: fmt-check lint test build
