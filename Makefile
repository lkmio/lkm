PROJECT_NAME := lkm
BINARY_NAME := $(PROJECT_NAME)

# 默认目标平台和架构
GOOS ?= linux
GOARCH ?= amd64

ifeq ($(GOOS),windows)
    BINARY_EXT = .exe
    COMPRESS_CMD = zip -j
    COMPRESS_EXT = .zip
else
    BINARY_EXT =
    COMPRESS_CMD = tar -czf
    COMPRESS_EXT = .tar.gz
endif

BIN_DIR := bin
RESOURCES := config.json rtc.html

BINARY := $(BINARY_NAME)-$(GOOS)-$(GOARCH)$(BINARY_EXT)
COMPRESSED_BINARY := $(BIN_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH)$(COMPRESS_EXT)

GO_BUILD := go build -v

all: build

create-bin-dir:
	@echo "Creating bin directory..."
	@mkdir -p $(BIN_DIR)

build: create-bin-dir
	@echo "Building $(BINARY)..."
	@CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO_BUILD) -o  $(BIN_DIR)/$(BINARY)

compress:
	@echo "Compressing $(BINARY)..."
ifeq ($(GOOS),windows)
	@$(COMPRESS_CMD) $(COMPRESSED_BINARY) $(BIN_DIR)/$(BINARY) $(RESOURCES)
else
	@$(COMPRESS_CMD) $(COMPRESSED_BINARY) -C $(BIN_DIR)/ $(BINARY) -C ../ $(RESOURCES)
endif

clean:
	@echo "Cleaning up..."
	@rm -rf $(BIN_DIR)/$(BINARY_NAME)-*
	@rm -rf $(BIN_DIR)/$(BINARY_NAME)-*.tar.gz $(BIN_DIR)/$(BINARY_NAME)-*.zip

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

build-all: build-windows build-windows-arm64 build-linux build-linux-arm64 build-darwin build-darwin-arm64
	$(MAKE) compress-windows
	$(MAKE) compress-windows-arm64
	$(MAKE) compress-linux
	$(MAKE) compress-linux-arm64
	$(MAKE) compress-darwin
	$(MAKE) compress-darwin-arm64

compress-windows:
	@$(MAKE) compress GOOS=windows GOARCH=amd64

compress-windows-arm64:
	@$(MAKE) compress GOOS=windows GOARCH=arm64

compress-linux:
	@$(MAKE) compress GOOS=linux GOARCH=amd64

compress-linux-arm64:
	@$(MAKE) compress GOOS=linux GOARCH=arm64

compress-darwin:
	@$(MAKE) compress GOOS=darwin GOARCH=amd64

compress-darwin-arm64:
	@$(MAKE) compress GOOS=darwin GOARCH=arm64

.PHONY: all build clean build-windows build-windows-arm64 build-linux build-linux-arm64 build-darwin build-darwin-arm64 build-all compress compress-windows compress-windows-arm64 compress-linux compress-linux-arm64 compress-darwin compress-darwin-arm64 create-bin-dir