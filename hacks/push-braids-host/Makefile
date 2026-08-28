BINARY    := push-braids-host
REPO_ROOT := $(abspath $(CURDIR)/../..)
HACK_REL  := hacks/push-braids-host
DOCKER    := docker run --rm --platform linux/amd64 \
               -v "$(REPO_ROOT)":/work -w /work/$(HACK_REL)/src \
               golang:1.25-bookworm

.PHONY: all build clean

all: build

# cgo (dlopen + libasound) means this can't cross-compile with a plain
# `go build` the way this project's other Go hacks do — build natively
# inside a linux/amd64 container instead, same reasoning as
# hacks/push-display's C build and hacks/push-audio-loopback's
# loopback_feed.c. Mounts the whole repo (not just this directory) so
# go.mod's `replace ... => ../../../core` resolves inside the container.
build:
	@echo "Building $(BINARY) (cgo, linux/amd64, via Docker)..."
	@# AbletonOS keeps its dynamic linker at /lib/ld-linux-x86-64.so.2, not
	@# /lib64/... the way Debian does — same fix already needed for
	@# hacks/push-display and hacks/push-audio-loopback's C binaries; here
	@# it goes through -extldflags since cgo links via the external linker.
	$(DOCKER) sh -c "apt-get update -qq && apt-get install -qq -y libasound2-dev >/dev/null && \
	  CGO_ENABLED=1 go build -ldflags '-s -w -linkmode external -extldflags -Wl,--dynamic-linker=/lib/ld-linux-x86-64.so.2' -o ../$(BINARY) ."
	@echo "Done: $(BINARY)"

clean:
	rm -f $(BINARY)
