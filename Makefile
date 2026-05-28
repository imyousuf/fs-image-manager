# fs-image-manager — build / test / lint / release
#
# The Go binary embeds the production frontend bundle (web/dist) via go:embed,
# so `make build` always builds the frontend first. The binary is CGO-free
# (pure-Go sqlite via modernc.org/sqlite); never set CGO_ENABLED=1 for builds.

BINARY      := fs-image-manager
WEB_DIR     := web
WEB_DIST    := $(WEB_DIR)/dist
# Version: exact tag if HEAD is tagged, else <tag>-<n>-g<sha>, else commit sha.
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)
GO          := go
export CGO_ENABLED := 0

# Cross-compile matrix for `make release` (CGO-free linux only, per spec).
RELEASE_PLATFORMS := linux/amd64 linux/arm64
DIST_DIR    := dist

.PHONY: all build build-web test test-go test-web lint fmt sqlc-generate \
        sqlc-diff release clean tidy help

all: lint test build

## build: build the frontend, then the Go binary with the embedded bundle
build: build-web
	$(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BINARY) .
	@echo "Built $(BINARY) version $(VERSION)"

## build-web: produce the production frontend bundle in web/dist
build-web:
	cd $(WEB_DIR) && npm ci && npm run build

## test: run both the Go and frontend test suites
test: test-go test-web

## test-go: run Go tests with the race detector (needs the frontend bundle to embed)
test-go: build-web
	CGO_ENABLED=1 $(GO) test -race ./...

## test-web: run frontend typecheck + unit tests
test-web:
	cd $(WEB_DIR) && npm ci && npm run typecheck && npm run test:run

## lint: run golangci-lint (install: https://golangci-lint.run/usage/install/)
lint:
	golangci-lint run --timeout=5m

## fmt: gofmt the tree
fmt:
	gofmt -s -w .

## sqlc-generate: regenerate internal/db/store from queries+migrations
sqlc-generate:
	sqlc generate

## sqlc-diff: fail if generated sqlc code is stale (used in CI)
sqlc-diff:
	sqlc diff

## release: cross-build CGO-free release tarballs + SHA256SUMS into dist/
release: build-web
	@rm -rf $(DIST_DIR) && mkdir -p $(DIST_DIR)
	@set -e; for platform in $(RELEASE_PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		out=$(DIST_DIR)/$(BINARY)_$(VERSION)_$${os}_$${arch}; \
		echo "==> building $$out"; \
		GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $$out/$(BINARY) . ; \
		cp LICENSE README.md $$out/ 2>/dev/null || true; \
		tar -czf $(DIST_DIR)/$(BINARY)_$(VERSION)_$${os}_$${arch}.tar.gz -C $(DIST_DIR) $(BINARY)_$(VERSION)_$${os}_$${arch}; \
		rm -rf $$out; \
	done
	@cd $(DIST_DIR) && sha256sum *.tar.gz > SHA256SUMS
	@echo "Release artifacts in $(DIST_DIR)/:" && ls -1 $(DIST_DIR)

## tidy: go mod tidy
tidy:
	$(GO) mod tidy

## clean: remove build outputs
clean:
	-rm -rf $(DIST_DIR)
	-rm -f $(BINARY)

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
