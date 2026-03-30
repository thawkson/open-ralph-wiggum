SHELL := /bin/sh

GO ?= go
BINARY_NAME ?= ralph
CMD_PATH ?= ./cmd/ralph
DIST_DIR ?= dist
CGO_ENABLED ?= 0
LOCAL_GOOS ?= $(shell $(GO) env GOOS)
LOCAL_GOARCH ?= $(shell $(GO) env GOARCH)
TARGETS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64
CHECKSUM_FILE ?= checksums.txt

.PHONY: help clean build build-all build-linux build-darwin build-windows build-targets checksums release

help:
	@echo "Available targets:"
	@echo "  make build        Build for the local platform ($(LOCAL_GOOS)/$(LOCAL_GOARCH))"
	@echo "  make build-all    Build for linux, darwin, windows on amd64 and arm64"
	@echo "  make build-linux  Build linux artifacts (amd64, arm64)"
	@echo "  make build-darwin Build macOS artifacts (amd64, arm64)"
	@echo "  make build-windows Build Windows artifacts (amd64, arm64)"
	@echo "  make checksums    Generate SHA-256 checksums for files in $(DIST_DIR)/"
	@echo "  make release      Clean, build all artifacts, and generate checksums"
	@echo "  make clean        Remove built artifacts"

clean:
	@rm -rf $(DIST_DIR)

build:
	@mkdir -p $(DIST_DIR)
	@out="$(DIST_DIR)/$(BINARY_NAME)-$(LOCAL_GOOS)-$(LOCAL_GOARCH)"; \
	if [ "$(LOCAL_GOOS)" = "windows" ]; then out="$$out.exe"; fi; \
	echo "Building $$out"; \
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(LOCAL_GOOS) GOARCH=$(LOCAL_GOARCH) $(GO) build -o "$$out" $(CMD_PATH)

build-all: build-targets

release: clean build-all checksums

build-linux:
	@$(MAKE) build-targets TARGETS="linux/amd64 linux/arm64"

build-darwin:
	@$(MAKE) build-targets TARGETS="darwin/amd64 darwin/arm64"

build-windows:
	@$(MAKE) build-targets TARGETS="windows/amd64 windows/arm64"

build-targets:
	@mkdir -p $(DIST_DIR)
	@for target in $(TARGETS); do \
		os=$${target%/*}; \
		arch=$${target#*/}; \
		out="$(DIST_DIR)/$(BINARY_NAME)-$$os-$$arch"; \
		if [ "$$os" = "windows" ]; then out="$$out.exe"; fi; \
		echo "Building $$out"; \
		CGO_ENABLED=$(CGO_ENABLED) GOOS=$$os GOARCH=$$arch $(GO) build -o "$$out" $(CMD_PATH) || exit 1; \
	done

checksums:
	@mkdir -p $(DIST_DIR)
	@if [ ! -n "$$(ls -A "$(DIST_DIR)" 2>/dev/null)" ]; then \
		echo "No artifacts found in $(DIST_DIR). Run 'make build-all' first."; \
		exit 1; \
	fi
	@cd $(DIST_DIR) && rm -f "$(CHECKSUM_FILE)" && \
	for f in *; do \
		if [ -f "$$f" ]; then \
			if command -v sha256sum >/dev/null 2>&1; then \
				sha256sum "$$f" >> "$(CHECKSUM_FILE)"; \
			elif command -v shasum >/dev/null 2>&1; then \
				shasum -a 256 "$$f" >> "$(CHECKSUM_FILE)"; \
			else \
				echo "No SHA-256 tool found (need sha256sum or shasum)."; \
				exit 1; \
			fi; \
		fi; \
	done
	@echo "Wrote $(DIST_DIR)/$(CHECKSUM_FILE)"