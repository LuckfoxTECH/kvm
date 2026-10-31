BRANCH    ?= $(shell git rev-parse --abbrev-ref HEAD)
BUILDDATE ?= $(shell date -u +%FT%T%z)
BUILDTS   ?= $(shell date -u +%s)
REVISION  ?= $(shell git rev-parse HEAD)
VERSION_DEV ?= 0.1.5-dev
VERSION ?= 0.1.5

PROMETHEUS_TAG := github.com/prometheus/common/version
KVM_PKG_NAME := kvm

# OTA signing key path (Ed25519 private key for auto-signing at build time)
OTA_SIGNING_KEY ?=

# OTA signing public key (hex-encoded Ed25519 public key, 64 hex chars)
# Default empty = signature verification disabled (backward compatible)
OTA_PUBLIC_KEY ?=

GO_BUILD_ARGS := -tags netgo
GO_RELEASE_BUILD_ARGS := -trimpath $(GO_BUILD_ARGS)
GO_LDFLAGS := \
  -s -w \
  -X $(PROMETHEUS_TAG).Branch=$(BRANCH) \
  -X $(PROMETHEUS_TAG).BuildDate=$(BUILDDATE) \
  -X $(PROMETHEUS_TAG).Revision=$(REVISION) \
  -X $(KVM_PKG_NAME).builtTimestamp=$(BUILDTS) \
  -X $(KVM_PKG_NAME).builtOtaPublicKey=$(OTA_PUBLIC_KEY)

GO_CMD := GOOS=linux GOARCH=arm GOARM=7 go
BIN_DIR := $(shell pwd)/bin

TEST_DIRS := $(shell find . -name "*_test.go" -type f -exec dirname {} \; | sort -u)

build_dev:
	@echo "Building..."
	$(GO_CMD) build \
		-ldflags="$(GO_LDFLAGS) -X $(KVM_PKG_NAME).builtAppVersion=$(VERSION_DEV)" \
		$(GO_RELEASE_BUILD_ARGS) \
		-o $(BIN_DIR)/kvm_app cmd/main.go
	@if [ -n "$(OTA_SIGNING_KEY)" ]; then \
		echo "Signing $(BIN_DIR)/kvm_app..."; \
		go run cmd/main.go cli signer sign --key "$(OTA_SIGNING_KEY)" $(BIN_DIR)/kvm_app; \
	else \
		echo "OTA_SIGNING_KEY not set, skipping signing."; \
	fi

frontend:
	cd ui && npm ci && npm run build:device

build_release: frontend
	@echo "Building release..."
	$(GO_CMD) build \
		-ldflags="$(GO_LDFLAGS) -X $(KVM_PKG_NAME).builtAppVersion=$(VERSION)" \
		$(GO_RELEASE_BUILD_ARGS) \
		-o bin/kvm_app cmd/main.go
	@if [ -n "$(OTA_SIGNING_KEY)" ]; then \
		echo "Signing bin/kvm_app..."; \
		go run cmd/main.go cli signer sign --key "$(OTA_SIGNING_KEY)" bin/kvm_app; \
	else \
		echo "OTA_SIGNING_KEY not set, skipping signing."; \
	fi

sign:
	@echo "Signing firmware files..."
	go run cmd/main.go cli signer sign --key $(KEY) $(FILES)
