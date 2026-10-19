BINARY    := push-manager
SRC_DIR   := src
BUILD_DIR := build

# Target: Ableton Push 3 (Intel x86_64 Linux)
GOOS      := linux
GOARCH    := amd64

# Build flags: strip debug info to minimise size. Static comes from
# CGO_ENABLED=0 below: with gcc around (golang Docker image) Go links
# dynamically, and Push has no /lib64/ld-linux loader -- "No such file".
LDFLAGS   := -s -w

.PHONY: all build clean fmt vet

all: build

build:
	@echo "Building $(BINARY) for $(GOOS)/$(GOARCH)..."
	@mkdir -p $(BUILD_DIR)
	cd $(SRC_DIR) && \
	  CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) \
	  go build -ldflags "$(LDFLAGS)" \
	  -o ../$(BUILD_DIR)/$(BINARY) .
	@cp $(BUILD_DIR)/$(BINARY) $(BINARY)
	@echo "Done: $(BINARY)"

# Build for local machine (development / testing)
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
