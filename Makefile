VERSION := $(shell cat VERSION)
BINARY := pelican-steam-updater
DIST := dist
GOOS ?= linux
GOARCH ?= amd64
GO_VERSION := 1.22
GO_IMAGE := golang:$(GO_VERSION)-bookworm
DOCKER_UID := $(shell id -u)
DOCKER_GID := $(shell id -g)
CACHE_DIR := .cache
MOD_CACHE := $(CACHE_DIR)/go-mod
BUILD_CACHE := $(CACHE_DIR)/go-build

DOCKER_OPTS := --rm \
	-u $(DOCKER_UID):$(DOCKER_GID) \
	-v "$(CURDIR):/src" \
	-v "$(CURDIR)/$(MOD_CACHE):/go/pkg/mod" \
	-w /src \
	-e GOCACHE=/src/$(BUILD_CACHE) \
	-e HOME=/tmp

.PHONY: build build-all clean release deps ensure-cache clean-cache help

help:
	@echo "Docker-only build targets (caches in $(CACHE_DIR)/):"
	@echo "  make deps        Update go.mod/go.sum to latest dependency versions"
	@echo "  make build       Build $(BINARY) for $(GOOS)/$(GOARCH) into $(DIST)/"
	@echo "  make build-all   Build linux amd64 + arm64 into $(DIST)/"
	@echo "  make release     Build release archives + checksums into $(DIST)/"
	@echo "  make clean       Remove $(DIST)/"
	@echo "  make clean-cache Remove $(CACHE_DIR)/ (Go module + build caches)"

ensure-cache:
	@mkdir -p $(MOD_CACHE) $(BUILD_CACHE) $(DIST)

deps: ensure-cache
	@echo "Updating dependencies to latest versions..."
	docker run $(DOCKER_OPTS) $(GO_IMAGE) sh -c 'go get -d -u ./... && go mod tidy'
	@echo "Updated go.mod and go.sum"

build: ensure-cache
	@echo "Building $(BINARY) v$(VERSION) for $(GOOS)/$(GOARCH) via Docker (uid=$(DOCKER_UID))..."
	docker run $(DOCKER_OPTS) -e CGO_ENABLED=0 $(GO_IMAGE) sh -ec '\
		go mod download && \
		GOOS=$(GOOS) GOARCH=$(GOARCH) go build \
			-trimpath \
			-ldflags "-s -w -X main.version=$(VERSION)" \
			-o /src/$(DIST)/$(BINARY)_$(GOOS)_$(GOARCH) \
			./cmd/pelican-steam-updater && \
		chmod +x /src/$(DIST)/$(BINARY)_$(GOOS)_$(GOARCH)'
	@echo "→ $(DIST)/$(BINARY)_$(GOOS)_$(GOARCH)"

build-all:
	$(MAKE) build GOOS=linux GOARCH=amd64
	$(MAKE) build GOOS=linux GOARCH=arm64

release: build-all
	@cd $(DIST) && for f in $(BINARY)_linux_*; do \
		[ -f "$$f" ] || continue; \
		tar -czf $$f.tar.gz $$f; \
	done
	@cd $(DIST) && sha256sum $(BINARY)_linux_*.tar.gz > checksums.txt 2>/dev/null || true
	@echo "Release artifacts in $(DIST)/"

clean:
	rm -rf $(DIST)

clean-cache:
	rm -rf $(CACHE_DIR)

# Legacy alias
clean-volumes: clean-cache
