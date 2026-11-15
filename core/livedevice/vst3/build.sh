#!/bin/sh
# Builds the livedevice plugin (livedevice.so, committed next to this file)
# in Docker, then checks it against the example hack (cmd/ldexample):
# Steinberg's validator, and a playtest of each example device (a note into
# the instrument, a tone through the effect, notes out of the MIDI device).
#
#   core/livedevice/vst3/build.sh          build and test
#   core/livedevice/vst3/build.sh test     test the committed livedevice.so
set -eu
cd "$(dirname "$0")"
IMAGE=push-hack-livedevice-builder
SDK_TAG=v3.8.1_build_84 # the VST3 SDK (MIT)

docker build -q --platform linux/amd64 -t "$IMAGE" . >/dev/null
if [ ! -d .sdk/pluginterfaces ]; then
  echo "== fetching the VST3 SDK $SDK_TAG"
  git clone --quiet --depth 1 --branch "$SDK_TAG" --recursive --shallow-submodules \
    https://github.com/steinbergmedia/vst3sdk.git .sdk
fi

CORE=$(cd ../.. && pwd)
docker run --rm --platform linux/amd64 -v "$CORE":/core -v push-hack-livedevice-gomod:/root/go/pkg/mod \
  -w /core/livedevice/vst3 -e MODE="${1:-build}" "$IMAGE" sh -ec '
  cmake -S . -B .build -G Ninja -DCMAKE_BUILD_TYPE=Release >/dev/null
  if [ "$MODE" = build ]; then
    cmake --build .build > .build/log.txt 2>&1 || { grep -E "error|Error" -A3 .build/log.txt | head -40; exit 1; }
    cp .build/VST3/Release/livedevice.vst3/Contents/x86_64-linux/livedevice.so livedevice.so
    echo "== built livedevice.so ($(file -b livedevice.so | cut -d, -f1-2))"
  else
    cmake --build .build --target validator playtest > .build/log.txt 2>&1 || { tail -20 .build/log.txt; exit 1; }
  fi

  echo "== testing against cmd/ldexample"
  (cd .. && go build -o /tmp/ldexample ./cmd/ldexample)
  /tmp/ldexample -so livedevice.so -vst3 /tmp/vst3 -socket /tmp/ld.sock -classes /tmp/ld-classes.json &
  for i in $(seq 1 50); do [ -S /tmp/ld.sock ] && break; sleep 0.1; done
  B=/tmp/vst3/PushHackExample.vst3
  .build/bin/Release/validator "$B" 2>&1 | grep -E "^Result|ERROR" | head -5
  .build/bin/Release/validator "$B" 2>&1 | grep -q "0 tests failed"
  for c in "Example Sine" "Example Gain" "Example Octave"; do
    .build/bin/Release/playtest "$B" "$c" 400
  done
  # a title with a character Go escapes in JSON (\u003e) reaches the host whole
  .build/bin/Release/playtest "$B" "Example Gain" 10 | grep -qx "param: In>Out" ||
    { echo "FAIL: the parameter title of Example Gain is not In>Out"; exit 1; }
'
