PROJECT_NAME := lkm

BINARY_NAME := $(PROJECT_NAME)

# 默认目标平台和架构
GOOS ?= linux
GOARCH ?= amd64

ifeq ($(GOOS),windows)
    BINARY_EXT = .exe
else
    BINARY_EXT =
endif

BIN_DIR := bin

BINARY := $(BIN_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH)$(BINARY_EXT)

GO_BUILD := go build -v

all: build

create-bin-dir:
	@echo "Creating bin directory..."
	@mkdir -p $(BIN_DIR)

build: create-bin-dir
	@echo "Building $(BINARY)..."
	@CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO_BUILD) -o $(BINARY)

clean:
	@echo "Cleaning up..."
	@rm -rf $(BIN_DIR)/$(BINARY_NAME)-*

build-windows:
	$(MAKE) build GOOS=windows GOARCH=amd64

build-windows-arm64:
	$(MAKE) build GOOS=windows GOARCH=arm64

build-linux:
	$(MAKE) build GOOS=linux GOARCH=amd64

build-linux-arm64:
	$(MAKE) build GOOS=linux GOARCH=arm64

build-darwin:
	$(MAKE) build GOOS=darwin GOARCH=amd64

build-darwin-arm64:
	$(MAKE) build GOOS=darwin GOARCH=arm64

build-all:
	$(MAKE) build-windows
	$(MAKE) build-windows-arm64
	$(MAKE) build-linux
	$(MAKE) build-linux-arm64
	$(MAKE) build-darwin
	$(MAKE) build-darwin-arm64

.PHONY: all build clean build-windows build-windows-arm64 build-linux build-linux-arm64 build-darwin build-darwin-arm64 build-all create-bin-dir
