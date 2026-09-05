SRC = src/loopback_feed/loopback_feed.c
OUT = loopback_feed
GO_OUT = push-audio-loopback
DOCKER = docker run --rm --platform linux/amd64 \
                -v "$(CURDIR)":/work -w /work \
                debian:bullseye

.PHONY: all clean

all: $(OUT) $(GO_OUT)

# AbletonOS keeps its dynamic linker at /lib/ld-linux-x86-64.so.2, not
# /lib64/... the way Debian does — matches the same fix already needed
# for hacks/push-display's helper binaries.
$(OUT): $(SRC)
	$(DOCKER) sh -c "apt-get update -qq && apt-get install -qq -y gcc libasound2-dev >/dev/null && \
	  gcc -O2 -Wall -Wl,--dynamic-linker=/lib/ld-linux-x86-64.so.2 \
	    -o $@ $(SRC) -lasound -lm -lpthread"
	@echo "Built: $(OUT)"

# Pure Go, no cgo — plain cross-compile, no Docker needed.
$(GO_OUT): src/*.go
	cd src && GOOS=linux GOARCH=amd64 go build -o ../$(GO_OUT) .
	@echo "Built: $(GO_OUT)"

clean:
	rm -f $(OUT) $(GO_OUT)
