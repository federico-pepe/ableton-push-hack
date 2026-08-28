BINARY    := push-catalogue
SRC_DIR   := src
BUILD_DIR := build

# Target: Ableton Push 3 Standalone (Intel x86_64 Linux)
GOOS      := linux
GOARCH    := amd64
LDFLAGS   := -s -w

.PHONY: all build build-local embed fmt vet clean

all: build

# The canonical installer script lives at the hack root; copy it into src/ so
# go:embed bakes it into the binary (single self-contained artifact to deploy).
embed:
	@cp push-catalogue.sh $(SRC_DIR)/push-catalogue.sh

build: embed
	@echo "Building $(BINARY) for $(GOOS)/$(GOARCH)..."
	@mkdir -p $(BUILD_DIR)
	cd $(SRC_DIR) && GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags "$(LDFLAGS)" -o ../$(BUILD_DIR)/$(BINARY) .
	@cp $(BUILD_DIR)/$(BINARY) $(BINARY)
	@echo "Done: $(BINARY)"

build-local: embed
	@mkdir -p $(BUILD_DIR)
	cd $(SRC_DIR) && go build -ldflags "$(LDFLAGS)" -o ../$(BUILD_DIR)/$(BINARY)-local .
	@echo "Done: $(BUILD_DIR)/$(BINARY)-local"

fmt:
	cd $(SRC_DIR) && go fmt ./...

vet: embed
	cd $(SRC_DIR) && go vet ./...

clean:
	rm -rf $(BUILD_DIR) $(BINARY) $(SRC_DIR)/push-catalogue.sh
