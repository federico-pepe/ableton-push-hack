BINARY    := keyboard-visualizer
SRC_DIR   := src
BUILD_DIR := build

# Target: Ableton Push 3 (Intel x86_64 Linux)
GOOS      := linux
GOARCH    := amd64

# Build flags: static binary, strip debug info to minimise size
LDFLAGS   := -s -w

.PHONY: all build clean fmt vet

all: build

build:
	@echo "Building $(BINARY) for $(GOOS)/$(GOARCH)..."
	@mkdir -p $(BUILD_DIR)
	cd $(SRC_DIR) && \
	  GOOS=$(GOOS) GOARCH=$(GOARCH) \
	  go build -ldflags "$(LDFLAGS)" \
	  -o ../$(BUILD_DIR)/$(BINARY) .
	@cp $(BUILD_DIR)/$(BINARY) $(BINARY)
	@echo "Done: $(BINARY)"

build-local:
	@echo "Building $(BINARY) for local machine..."
	@mkdir -p $(BUILD_DIR)
	cd $(SRC_DIR) && \
	  go build -ldflags "$(LDFLAGS)" \
	  -o ../$(BUILD_DIR)/$(BINARY)-local .
	@echo "Done: $(BUILD_DIR)/$(BINARY)-local"

fmt:
	cd $(SRC_DIR) && go fmt ./...

vet:
	cd $(SRC_DIR) && go vet ./...

clean:
	rm -rf $(BUILD_DIR) $(BINARY)
