# Makefile

BINARY_NAME=coroner
MAIN_PATH=cmd/coroner/main.go
MODULE=github.com/curiousjc/coroner

# Build metadata. Overridable from the environment so CI can pin exact values.
VERSION       ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT        ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_TIME    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# Selects where corlog writes app.log: "development" puts it in the working
# directory, anything else beside the binary.
BUILD_CONTEXT ?= development

LDFLAGS = -X main.buildContext=$(BUILD_CONTEXT) \
          -X $(MODULE)/internal/version.Version=$(VERSION) \
          -X $(MODULE)/internal/version.Commit=$(COMMIT) \
          -X $(MODULE)/internal/version.BuildTime=$(BUILD_TIME)

all: build-linux build-windows

# The front end, built into internal/webui/dist and compiled in by the webui
# tag. A plain `go build` skips both and `coroner serve` falls back to the
# static timeline page.
web:
	cd web && npm ci && npm run build

build-linux: web
	GOOS=linux GOARCH=amd64 go build -tags webui -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) $(MAIN_PATH)

build-windows: web
	GOOS=windows GOARCH=amd64 go build -tags webui -ldflags "$(LDFLAGS)" -o $(BINARY_NAME).exe $(MAIN_PATH)

# What CI builds. Same targets, but the binaries log beside themselves.
release:
	$(MAKE) all BUILD_CONTEXT=release

clean:
	rm -f $(BINARY_NAME) $(BINARY_NAME).exe
	rm -rf internal/webui/dist

test:
	go test ./...

vet:
	go vet ./...

.PHONY: all web build-linux build-windows release clean test vet
