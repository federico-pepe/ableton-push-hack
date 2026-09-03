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

```bash
# From this directory, after `make`:
scp push-braids-host root@push.local:/tmp/
scp ~/Developer/schwung-braids-main/build/dsp.so root@push.local:/tmp/
ssh root@push.local 'mkdir -p /tmp/braids-module/presets'
scp ~/Developer/schwung-braids-main/src/presets/*.braids root@push.local:/tmp/braids-module/presets/

# hacks/push-audio-loopback's virtual card must be loaded first — see
# that hack's own README for building/loading snd-aloop.ko.

ssh root@push.local
cd /tmp
./push-braids-host ./dsp.so ./braids-module hw:PHVAudio,1,0 32 44100 128 384
#                   ^dsp.so  ^module dir     ^PCM device      ^ch ^rate ^period ^buffer
```

128/384 matches Live's own default buffer and works with no missed
deadlines, as long as `hacks/push-audio-loopback`'s virtual card is
loaded with `timer_source` set. See `docs/push3-dsp-hosting.md` for why
`timer_source` matters.

Device/channel/rate/period/buffer must match whatever Live has
currently negotiated on the *other* side of the Loopback pair — check
`/proc/asound/card1/pcm0c/sub0/hw_params` (or whichever subdevice Live
has selected) the same way documented in
`hacks/push-audio-loopback/README.md`.

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

## Redeploying after a reboot

Nothing in this chain is installed as a persistent service — a Push
reboot loses the loopback card (kernel module), everything under `/tmp`
(the DSP plugin, presets, this binary), and of course the running
process. `push-manager`/`push-display` come back on their own (real
sysvinit services); the rest needs redeploying by hand:

```bash
# 1. Reload the loopback card — same .ko as before, no rebuild needed
#    unless the device's `uname -r` changed (compare against
#    hacks/push-audio-loopback/README.md's vermagic check).
scp <path-to>/snd-aloop.ko root@push.local:/tmp/
ssh root@push.local 'insmod /tmp/snd-aloop.ko id=PHVAudio timer_source=A3.0.0'

# 2. Re-copy the DSP plugin + presets (wiped along with the rest of /tmp)
scp ~/Developer/schwung-braids-main/build/dsp.so root@push.local:/tmp/
ssh root@push.local 'mkdir -p /tmp/braids-module/presets'
scp ~/Developer/schwung-braids-main/src/presets/*.braids root@push.local:/tmp/braids-module/presets/

# 3. Check what Live actually negotiated this time — its own buffer
#    setting can reset across a reboot/Live Set reload — and match it:
ssh ableton@push.local 'cat /proc/asound/card1/pcm0c/sub0/hw_params'

scp push-braids-host root@push.local:/tmp/
ssh root@push.local 'cd /tmp && nohup ./push-braids-host ./dsp.so ./braids-module hw:PHVAudio,1,0 32 44100 <period> <buffer> >/tmp/braids.log 2>&1 < /dev/null &'
```

Live's own track routing (Input = "Push Hack Virtual Audio", Monitor =
In) is saved in the Live Set and typically survives on its own — check
`/proc/asound/card1/pcm0c/sub0/hw_params` isn't `closed` before assuming
step 3 needs a manual re-route in Live's audio preferences too.

## Known limits

- Not an installable hack yet (no `deploy.sh`/`service.initd`) — a
  manually-run test binary, same status as `push-audio-loopback`'s
  `loopback_feed`. See "Redeploying after a reboot" above.
- Beyond the 8 encoders and D-Pad Left/Right (param UI) and Note
  On/Off (pad grid), no other MIDI is wired up — pitch bend and
  aftertouch are ignored.
