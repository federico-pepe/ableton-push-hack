# Hosting third-party DSP plugins on Push 3

This is the reference for a working chain that gets audio out of a
Push 3 speaker or headphone jack from a process other than Live,
without any changes to Live or to a file Ableton ships. It ties
together two hacks and one real cross-cutting bug class.

Full background and the plan this implements:
`plans/2026-08-27-schwung-on-push3-feasibility.md` in
`push-tethered-app`. Reasoning trail only — this file is the settled
reference.

## The two hacks

1. **`hacks/push-audio-loopback`** gives Live a selectable virtual
   sound card, "Push Hack Virtual Audio", built from the Linux
   kernel's own ALSA Loopback driver. A user routes an audio track's
   input to this device, with Monitor set to In. Confirmed carrying
   real, audible audio on real hardware.
2. **`hacks/push-braids-host`** reads pad and button presses off
   Push 3's own MIDI, feeds them into a DSP plugin, and writes the
   plugin's audio into the virtual card above. Confirmed producing
   real, audible synthesis on real hardware, triggered by real pad
   presses.

## The plugin format: Move Anything `plugin_api_v2`

The DSP plugin in this chain (Braids, a Mutable Instruments
macro-oscillator port, source at `~/Developer/schwung-braids-main`)
comes from the Move Anything project, built for Ableton Move. It needs
no change to run on Push 3: the DSP code is plain, portable C++, with
no ARM-specific instructions.

A `plugin_api_v2` module exports one C function,
`move_plugin_init_v2`. A host calls it once with a `host_api_v1_t`
struct (sample rate, block size, a log callback) and gets back a
`plugin_api_v2_t` struct: a table of function pointers for
`create_instance`, `destroy_instance`, `on_midi`, `set_param`,
`get_param`, and `render_block`. Both structs are defined inline in
the plugin's own source file, not in a shared header, so a host must
match their exact byte layout by hand — see
`hacks/push-braids-host/src/bridge.h` for the layout this project
uses.

`render_block` takes any block size a caller asks for — Braids showed
no difference in output between a 128-frame block (its native Move
target) and a 512-frame block (Push 3's actual negotiated size, see
below). A DSP plugin built this way should port to a new host with no
change to its audio path, only to how the host drives it.

## Why the audio needs a virtual card, not the real hardware

Push 3's real speaker and headphone jack are one ALSA audio device,
and Live holds it open for its whole run. A second process that opens
the same device gets `Device or resource busy`, even when Live is
idle. This is normal Linux behavior for a device with one owner, not a
bug in Live.

`push-audio-loopback`'s virtual card sidesteps this: it is a second,
separate ALSA device, so Live can have both open — the real hardware
for its own audio, and the virtual card for an outside process — at
the same time.

## A real timing detail: match the buffer size

The Loopback driver's internal clock ticks every 4 milliseconds on
this kernel (`CONFIG_HZ=250`). Live's own default audio buffer (128
frames, about 3 milliseconds) is shorter than this tick, so audio
arrives in uneven bursts and sounds glitchy — with no error reported
anywhere in the audio stack. Raising Live's own audio buffer to 512
frames (about 11.6 milliseconds — set in Live's own audio settings,
visible on Push3's screen) fixed this for a plain test tone.

Any process that plays audio through this virtual card must match
whatever buffer size and channel count Live has already negotiated on
the other side of the card — check
`/proc/asound/PHVAudio/pcm0c/sub0/hw_params` for the live values, and
match them exactly, or the second side fails to open.

## Two real bugs found while building the DSP host

Both were found live, on real hardware, and both are worth watching
for in any future host built this way — neither reported an error;
both just produced silence or glitches.

### A silent goroutine race

The first version of `push-braids-host` called into the DSP plugin
from two goroutines: the MIDI read loop called `on_midi` directly,
and the render loop called `render_block`, with no coordination
between them. A DSP plugin's C++ state (voice envelopes, oscillators)
is not written to be thread-safe, and the result was total silence —
`peak=0` on every render, with the ALSA sequencer layer showing MIDI
events arriving correctly the whole time
(`/proc/asound/seq/clients`, `Alloc success` climbing).

**The fix:** the MIDI read loop only parses raw bytes and pushes them
onto a Go channel. The render loop drains that channel and makes every
call into the plugin itself, all from one goroutine. One goroutine
owns the plugin instance, with no exception.

### `SCHED_FIFO` set on the wrong thread

`sched_setscheduler(0, SCHED_FIFO, ...)` sets the policy of the
*calling* thread, not the whole process. Go's scheduler is free to
move a goroutine's later work — including its cgo calls — onto a
different OS thread than the one that made this call, unless the
goroutine calls `runtime.LockOSThread()` first.

Without this lock, real-time priority landed on some other thread than
the one running the render loop, so it did nothing useful. This
produced clean audio on short pad taps but audible glitches on held
notes — consistent with the render loop's thread occasionally missing
its ~11.6 millisecond deadline under normal (non-real-time) scheduling.
Calling `runtime.LockOSThread()` at the very start of `main()`, before
the `sched_setscheduler` call, measurably improved this on real
hardware.

## Known open issue: a fixed ~10ms write-latency floor (2026-08-28)

After both fixes above, playing more than one note at once still
glitches sometimes, though far less than before. This is not a
Braids polyphony problem and not a CPU problem — instrumenting the
render loop's own per-block wall-clock time settled that directly:

- `bridge_pcm_render` plus the stereo-to-wide-channel expansion
  (`hacks/push-braids-host/src/main.go`'s producer-side work) takes
  100–330 microseconds per block, against an 11.6 millisecond budget
  at a 512-frame period. This is nowhere near the budget, with or
  without several notes held at once — Go's garbage collector is
  disabled (`debug.SetGCPercent(-1)`) and the render loop is
  allocation-free, so this number is stable.
- `bridge_pcm_writei` — the blocking ALSA write call — is a different
  story: it consistently takes about **10 milliseconds longer than
  the nominal period time, not proportional to the period itself**:

  | Period | Nominal budget | Observed `writei` time | Excess |
  |---|---|---|---|
  | 512 frames | 11.6 ms | ~22 ms | ~10.4 ms |
  | 256 frames | 5.8 ms | ~16 ms | ~10.2 ms |

  Halving the period roughly halved the nominal budget, but the
  excess over budget stayed flat. A cost that scales with period
  would point at real per-frame processing; a **fixed** cost points
  at some overhead paid once per write call, independent of how much
  audio that call carries.

**Working theory:** ~10ms lines up closely with 2–3 ticks of the
kernel's 4-millisecond jiffies clock (`CONFIG_HZ=250`) — the same
clock granularity already responsible for the original jiffies-tick
glitch above, now showing up as a quantized, roughly fixed floor on
how promptly the Loopback driver's timer-driven cable actually frees
buffer space for a blocking writer, rather than as burst delivery.

**Practical implication:** a larger buffer helps because it shrinks
this fixed ~10ms floor's *share* of each block — this is why 128→512
frames measurably improved things. It does not remove the floor.
Shrinking the period (tried above, to help isolate the cause) makes
the floor's relative share *worse*, not better — don't do that as a
fix. The only way to remove the floor itself is raising the kernel's
`CONFIG_HZ`, which means a full custom kernel build — a much bigger,
riskier step than anything else in this hack, and not recommended
without a specific reason strong enough to justify it. An untried,
lower-risk mitigation: an even larger buffer (1024 frames or more,
trading latency for headroom) to shrink the floor's relative share
further.

**Confirmed at 1024 frames (2026-08-28):** subjectively, noticeably
fewer/less-frequent glitches on real hardware, chords included —
this mitigation works. One caveat on the "~10ms flat floor" framing
above: a long 1024-frame run (84,331 blocks, several minutes) hit a
`maxWrite` of **47.98ms** once — a single outlier, not the typical
case, but well above the ~10ms figure from the earlier 256/512 tests.
Those earlier tests only ran ~10 seconds each (a couple thousand
blocks), so they likely never sampled long enough to catch a
comparable rare tail spike. Read "~10ms" above as a lower bound /
typical figure from short sampling, not a hard ceiling — occasional
larger outliers exist at any period, this kernel's coarse timer being
the likely reason. 1024 frames is the current best-known default for
this reason: it doesn't eliminate outliers, it just makes each of
them a smaller fraction of the block they land in, which is
apparently enough to be perceptually much better even if not
perfect.

## Running a test

1. Build and load `hacks/push-audio-loopback`'s virtual card — see
   that hack's own README.
2. In Live's own audio settings on Push3's screen, set the buffer size
   to at least 512 samples.
3. Route an audio track's input to "Push Hack Virtual Audio" 1/2, with
   Monitor set to In.
4. Build and run `hacks/push-braids-host` — see that hack's own
   README for the exact command.
5. Press a pad. Real audio should come out Push 3's speaker or
   headphone jack.

## Where the code lives

- `hacks/push-audio-loopback/` — the virtual sound card and a
  low-jitter test-tone tool.
- `hacks/push-braids-host/` — the DSP host: MIDI in, Braids, audio
  out.
- `~/Developer/schwung-braids-main` — the Braids plugin source itself
  (not part of this repo; a separate Move Anything module).
