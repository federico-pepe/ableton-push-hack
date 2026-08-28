BINARY    := random-preset
SRC_DIR   := src
BUILD_DIR := build

# Target: Ableton Push 3 Standalone (Intel x86_64 Linux)
GOOS      := linux
GOARCH    := amd64
LDFLAGS   := -s -w

.PHONY: all build build-local fmt vet clean

all: build

build:
	@echo "Building $(BINARY) for $(GOOS)/$(GOARCH)..."
	@mkdir -p $(BUILD_DIR)
	cd $(SRC_DIR) && GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags "$(LDFLAGS)" -o ../$(BUILD_DIR)/$(BINARY) .
	@cp $(BUILD_DIR)/$(BINARY) $(BINARY)
	@echo "Done: $(BINARY)"

fmt:
	cd $(SRC_DIR) && go fmt ./...

vet:
	cd $(SRC_DIR) && GOOS=$(GOOS) GOARCH=$(GOARCH) go vet ./...

clean:
	rm -rf $(BUILD_DIR) $(BINARY)
