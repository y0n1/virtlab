.PHONY: help fetch-libvirt-schemas \
	clean build install up down status check vet \
	test lint test-coverage docs-check release-snapshot

VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')
INSTALL_DIR ?= $(HOME)/.local/bin

LDFLAGS := -s -w \
	-X github.com/y0n1/virtlab/internal/version.Version=$(VERSION) \
	-X github.com/y0n1/virtlab/internal/version.GitCommit=$(GIT_COMMIT) \
	-X github.com/y0n1/virtlab/internal/version.BuildDate=$(BUILD_DATE)

## Build the virtlab CLI tool (bin/virtlab)
build:
	@mkdir -p bin
	@go build -ldflags "$(LDFLAGS)" -o bin/virtlab ./cmd/virtlab

## Installs virtlab binary into ~/.local/bin by default (overridable via INSTALL_DIR)
install: build
	@install -d $(INSTALL_DIR)
	@install -m 0755 bin/virtlab "$(INSTALL_DIR)/virtlab"

## Bring up the lab across all 4 phases (up)
up: build
	@./bin/virtlab up

## Tear down all lab resources and revert workstation changes (down)
down: build
	@./bin/virtlab down

## Inspect status of the lab tunnel, host, and cluster (status)
status: build
	@./bin/virtlab status

## Run build, vet, and unit tests
check: build vet test

## Run go vet across cmd and internal
vet:
	@echo "==> go vet"
	@go vet ./cmd/... ./internal/...

## Run unit tests
test:
	@echo "==> go test"
	@go test ./internal/...

## Run golangci-lint across cmd and internal
lint:
	@echo "==> golangci-lint"
	@golangci-lint run ./cmd/... ./internal/...

## Verify that paths, Make targets, and virtlab commands named in docs resolve
docs-check:
	@echo "==> docs check"
	@go run ./hack/docscheck

## Run unit tests with coverage profile and display summary
test-coverage:
	@echo "==> go test with coverage"
	@go test -v -coverprofile=coverage.out -covermode=atomic ./internal/...
	@go tool cover -func=coverage.out | tail -n 1

## Build local release artifacts via GoReleaser (dry-run snapshot)
release-snapshot:
	@echo "==> goreleaser release --snapshot"
	@goreleaser release --snapshot --clean --skip=publish

## Show targets
help:
	@awk ' \
		/^## / { d = substr($$0, 4); next } \
		/^[a-zA-Z][a-zA-Z0-9_.-]*:/ && d { \
			t = $$1; sub(/:.*/, "", t); \
			if (t !~ /^_/) printf "  %-12s %s\n", t, d; \
			d = ""; \
		} \
	' $(MAKEFILE_LIST)
