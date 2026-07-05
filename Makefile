BINARY_NAME ?= vohive
GO_TAGS ?= with_utls nomsgpack
GOOS ?= linux
CGO_ENABLED ?= 0
GO ?= go
NPM ?= npm
GOWORK ?= off
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo "unknown")
VERSION_TAG = $(if $(filter v%,$(VERSION)),$(VERSION),v$(VERSION))
BUILD_TIME ?= $(shell date "+%Y-%m-%d %H:%M:%S")
DIST_DIR ?= dist
MAIN_PACKAGE ?= ./cmd/vohive

LDFLAGS = -s -w -X 'github.com/iniwex5/vohive/internal/global.Version=$(VERSION)' -X 'github.com/iniwex5/vohive/internal/global.BuildTime=$(BUILD_TIME)'
GO_ENV = GOWORK=$(GOWORK) CGO_ENABLED=$(CGO_ENABLED)
GO_BUILD = $(GO_ENV) $(GO) build -trimpath -buildvcs=false -tags "$(GO_TAGS)" -ldflags "$(LDFLAGS)"

AMD64_OUT = $(DIST_DIR)/$(BINARY_NAME)_$(VERSION_TAG)_linux_amd64
ARM64_OUT = $(DIST_DIR)/$(BINARY_NAME)_$(VERSION_TAG)_linux_arm64
ARMV7_OUT = $(DIST_DIR)/$(BINARY_NAME)_$(VERSION_TAG)_linux_armv7
UPX ?= $(shell command -v upx || command -v upx-ucl)
UPX_FLAGS ?= --best --lzma
ENABLE_UPX ?= 1

.PHONY: all build build-local build-amd64 build-arm64 build-armv7 build-all frontend-dist test verify clean

all: build-all

build: build-amd64

build-local: frontend-dist
	mkdir -p $(DIST_DIR)
	$(GO_BUILD) -o $(DIST_DIR)/$(BINARY_NAME) $(MAIN_PACKAGE)

build-all: build-amd64 build-arm64 build-armv7

frontend-dist:
	$(NPM) ci --prefix web
	$(NPM) run build --prefix web
	rm -rf internal/web/dist
	mkdir -p internal/web
	cp -R web/dist internal/web/dist

build-amd64: frontend-dist
ifeq ($(ENABLE_UPX),1)
	@test -n "$(UPX)" || { echo "错误: 需要安装 upx"; exit 1; }
endif
	mkdir -p $(DIST_DIR)
	$(GO_ENV) GOOS=$(GOOS) GOARCH=amd64 $(GO) build -trimpath -buildvcs=false -tags "$(GO_TAGS)" -ldflags "$(LDFLAGS)" -o $(AMD64_OUT) $(MAIN_PACKAGE)
ifeq ($(ENABLE_UPX),1)
	$(UPX) $(UPX_FLAGS) $(AMD64_OUT)
endif

build-arm64: frontend-dist
ifeq ($(ENABLE_UPX),1)
	@test -n "$(UPX)" || { echo "错误: 需要安装 upx"; exit 1; }
endif
	mkdir -p $(DIST_DIR)
	$(GO_ENV) GOOS=$(GOOS) GOARCH=arm64 $(GO) build -trimpath -buildvcs=false -tags "$(GO_TAGS)" -ldflags "$(LDFLAGS)" -o $(ARM64_OUT) $(MAIN_PACKAGE)
ifeq ($(ENABLE_UPX),1)
	$(UPX) $(UPX_FLAGS) $(ARM64_OUT)
endif

build-armv7: frontend-dist
ifeq ($(ENABLE_UPX),1)
	@test -n "$(UPX)" || { echo "错误: 需要安装 upx"; exit 1; }
endif
	mkdir -p $(DIST_DIR)
	$(GO_ENV) GOOS=$(GOOS) GOARCH=arm GOARM=7 $(GO) build -trimpath -buildvcs=false -tags "$(GO_TAGS)" -ldflags "$(LDFLAGS)" -o $(ARMV7_OUT) $(MAIN_PACKAGE)
ifeq ($(ENABLE_UPX),1)
	$(UPX) $(UPX_FLAGS) $(ARMV7_OUT)
endif

test:
	$(GO_ENV) $(GO) test -mod=mod ./...

verify:
	$(GO_ENV) $(GO) list -m -json github.com/iniwex5/vowifi-go
	$(GO_ENV) $(GO) test -mod=mod ./pkg/... ./internal/cardpolicy/... ./internal/simaid/... ./internal/smsnotify/...

clean:
	$(GO) clean
	rm -rf $(DIST_DIR)
