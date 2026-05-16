SHELL := /bin/bash

BINARY     := skil-lock
PKG        := github.com/skills-lock/skil-lock
CMD_PKG    := $(PKG)/cmd/skil-lock
BIN_DIR    := bin
BIN_PATH   := $(BIN_DIR)/$(BINARY)

VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE       ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)

.PHONY: all
all: lint test build

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: build
build: $(BIN_DIR)
	go build -ldflags "$(LDFLAGS)" -o $(BIN_PATH) $(CMD_PKG)

$(BIN_DIR):
	@mkdir -p $(BIN_DIR)

.PHONY: test
test:
	go test -race -coverprofile=coverage.out ./...

.PHONY: lint
lint:
	@command -v golangci-lint >/dev/null 2>&1 || { \
		echo "golangci-lint not installed. Install: https://golangci-lint.run/welcome/install/"; \
		exit 1; \
	}
	golangci-lint run --timeout=5m ./...

.PHONY: run
run: build
	$(BIN_PATH) $(ARGS)

.PHONY: clean
clean:
	rm -rf $(BIN_DIR) coverage.out

.PHONY: help
help:
	@echo "Targets:"
	@echo "  tidy   - go mod tidy"
	@echo "  build  - compile to $(BIN_PATH)"
	@echo "  test   - go test with race detector + coverage"
	@echo "  lint   - golangci-lint run"
	@echo "  run    - build and run; pass args via ARGS='version'"
	@echo "  clean  - remove build artifacts"
	@echo "  all    - lint + test + build"
