# push-braids-host

A minimal Push 3 standalone host proving the full audio+MIDI+DSP chain:
reads pad/button MIDI straight off Push3's own ALSA sequencer, feeds
notes into a Move Anything `plugin_api_v2` DSP module (Braids, a
Mutable Instruments macro-oscillator port —
`~/Developer/schwung-braids-main`), and writes the rendered audio into
[`hacks/push-audio-loopback`](../push-audio-loopback/)'s "Push Hack
Virtual Audio" virtual sound card.

Background and the plan this implements:
`push-tethered-app`'s `plans/2026-08-27-schwung-on-push3-feasibility.md`
("Next steps", step 3).

Has an on-screen control UI for Braids' own parameters (algorithm,
timbre, color, envelopes, volume) — see "On-screen controls" below.

## Confirmed working (2026-08-28)

- Braids' `dsp.so` builds clean for `linux/amd64` with **zero source
  changes** (portable C++, no ARM-specific code).
- Real pad presses on Push3 reach the DSP and produce real audio:
  `peak=6249`, real nonzero samples correlating with the presses,
  captured directly off the virtual card's paired device.
- Zero ALSA xruns across an 8000+ block run.

## A real bug found and fixed along the way

The first version called into the DSP plugin from two goroutines with
no synchronization: the ALSA MIDI read loop called
`bridge_plugin_on_midi` directly, while the main render loop called
`bridge_plugin_render` — both on the same C++ instance, concurrently.
Braids' internal state (voice envelopes, oscillators) is not
thread-safe, and the result was **total silence, with zero visible
error anywhere** — the ALSA sequencer layer showed events arriving
correctly (`/proc/asound/seq/clients`, `Alloc success` climbing), but
no audio ever came out.

The fix: `midiHandler.Fixed()` (the read-loop goroutine) only parses
raw MIDI bytes and pushes them onto a channel. Every `bridge_plugin_*`
call happens on the main goroutine, in the render loop, which drains
that channel before each `render_block` call. One goroutine owns the
plugin instance, full stop.

**Lesson for any future DSP-hosting code here:** never call into a
C/C++ plugin instance from more than one goroutine without an explicit
single-owner design. This class of bug produces no crash and no error
— just silence — so it can look identical to "the plugin does nothing"
or "MIDI isn't arriving" and cost real debugging time distinguishing
those from a plain data race.

## Build

```bash
cd hacks/push-braids-host
make        # cross-builds via Docker golang:1.25-bookworm, needs libasound2-dev
```

cgo (dlopen + libasound) means this can't cross-compile with a plain
`go build` the way this project's other Go hacks do — same reasoning
as `hacks/push-display`'s C build and `hacks/push-audio-loopback`'s
`loopback_feed.c`.

## Build the Braids DSP plugin

```bash
cd ~/Developer/schwung-braids-main
mkdir -p build
# native x86_64 build (matches the pattern documented in
# hacks/push-audio-loopback/README.md) — no ARM cross toolchain needed:
docker run --rm --platform linux/amd64 -v "$PWD":/build -w /build debian:bullseye sh -c '
  apt-get update -qq && apt-get install -qq -y g++ >/dev/null
  BRAIDS_SRCS="src/dsp/braids/macro_oscillator.cc src/dsp/braids/analog_oscillator.cc \
    src/dsp/braids/digital_oscillator.cc src/dsp/braids/resources.cc \
    src/dsp/braids/quantizer.cc src/dsp/stmlib/utils/random.cc"
  for s in $BRAIDS_SRCS; do g++ -O3 -fPIC -std=c++14 -DTEST -Isrc/dsp -c "$s" -o "build/$(basename "$s" .cc).o"; done
  g++ -O3 -fPIC -std=c++14 -DTEST -Isrc/dsp -c src/dsp/braids_plugin.cpp -o build/braids_plugin.o
  g++ -shared build/*.o -o build/dsp.so -lm
'
```

## Deploy and run

This hack is a normal installable service. It reads no command-line
arguments — only `-config <hack.json path>`, the same as every other
catalog-installed hack. It picks its own channel count, sample rate,
and buffer size live from whatever Live actually negotiated, instead of
a fixed value passed in. See "Persistent install" below for the full
picture.

First, install push-audio-loopback (its virtual card must exist first)
and build this hack:

```bash
cd hacks/push-braids-host
make
./scripts/install.sh --hack push-braids-host
```

Then copy the DSP plugin and its presets into the install directory —
`install.sh` does not know about these extra files yet:

```bash
ssh root@push.local 'mkdir -p /data/push-hack/hacks/push-braids-host/module/presets'
scp ~/Developer/schwung-braids-main/build/dsp.so \
  root@push.local:/data/push-hack/hacks/push-braids-host/dsp.so
scp ~/Developer/schwung-braids-main/src/presets/*.braids \
  root@push.local:/data/push-hack/hacks/push-braids-host/module/presets/
ssh root@push.local '/etc/init.d/push-hack-push-braids-host restart'
```

To actually hear it: an audio track in Live's own Set, Input =
"Push Hack Virtual Audio", Monitor = In, routed to Master — same setup
already validated for `push-audio-loopback`. Pressing a pad on Push3
should now trigger Braids and come out the real speaker/headphone
output.

## On-screen controls

Hold **Shift + Device** to toggle a param UI on Push's own screen: a
gauge knob per parameter on the current page (Algorithm, Timbre, Color,
Attack/Decay/Sustain/Release, Volume on page 1; the filter envelope on
page 2), driven by the 8 encoders above the screen. **D-Pad Left/Right**
switches pages. Turning the UI on also enables push-manager's MIDI
intercept, so pad hits drive Braids only — they stop reaching Live for
as long as the UI is on. Toggling it off (same chord) hands the screen
and MIDI back to normal.

Requires `push-manager` + `push-display` also running (they already are
if you followed `install.sh`) — see CLAUDE.md's "Display-owning hacks".
Param names/ranges/enum options are read from the plugin itself
(`bridge_plugin_get_param("chain_params")`), not hardcoded here.

### I/O picker (page 3)

**D-Pad Right** twice from page 1 reaches a third page, "I/O" — a
scrollable list to pick:

- **MIDI INPUT** — which of Push3's own three MIDI ports to read
  pad/button presses from (normally "Live Port", the default).
- **AUDIO OUTPUT** — which playback device on the system to render to.
- **AUDIO CHANNEL** — which channel pair of that device (1-2, 3-4, ...)
  the stereo signal lands on, so it can line up with whatever channel
  pair Live's own track is set to read from.

**D-Pad Up/Down** moves the list cursor, **Select** confirms the
highlighted row. A change applies immediately (MIDI resubscribes, the
audio device reopens if needed) and is saved to `braids-config.json`, so
it survives a restart. Picking the wrong MIDI port breaks Shift+Device
itself (no pad/button events reach this hack at all) — if that happens,
edit `braids-config.json` directly over SSH and restart the service; see
"Persistent install" below for where that file lives.

## Persistent install

`push-braids-host` runs as a real sysvinit service, installed like any
other catalog hack, and needs no manual steps after a reboot:

- It waits for `push-audio-loopback`'s virtual card to appear, then
  waits for Live to actually open its side, before it opens any audio
  device. This works even if the two services start in any order.
- It reads channels, sample rate, period, and buffer size straight from
  what Live negotiated (`/proc/asound/PHVAudio/pcm0c/sub0/hw_params`),
  every few seconds, for as long as it runs — so if Live restarts with
  a different buffer size, this hack reopens its audio device to match,
  with no restart needed.
- If it crashes (a DSP plugin bug, an ALSA error), a small supervisor
  built into the same binary restarts it on its own, with a short
  growing delay between tries.
- Its own settings (MIDI port, audio device, channel pair) live in
  `braids-config.json`, next to `hack.json`, so they survive a reboot
  and a catalog update. Defaults match this hack's original hardcoded
  values — nothing changes for you unless you pick different values on
  the I/O picker page (see "On-screen controls" above).

## Known limits

- The DSP plugin (`dsp.so`) and its presets are not installed by
  `install.sh` or the catalog yet — copy them in by hand once, per
  "Deploy and run" above. They live under this hack's own install
  directory, so they survive a reboot like everything else here.
- Beyond the 8 encoders and D-Pad Left/Right (param UI) and Note
  On/Off (pad grid), no other MIDI is wired up — pitch bend and
  aftertouch are ignored.
