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

## A real timing detail: point Loopback at a real hardware timer

By default, the Loopback driver paces itself with the kernel's jiffies
clock, which ticks every 4 milliseconds on this kernel (`CONFIG_HZ=250`).
This tick is coarser than Live's own audio buffer (128 frames, about 3
milliseconds), so audio arrives in uneven bursts and sounds glitchy — with
no error anywhere in the audio stack.

The fix is the driver's own `timer_source` module parameter. It makes
Loopback follow the real hardware clock of another sound card instead of
jiffies. Push 3's own USB audio card (`hw:0`, ID `A3`) already runs a real
clock while Live uses it, so `insmod snd-aloop.ko timer_source=A3.0.0` ties
Loopback to that clock. See `hacks/push-audio-loopback/README.md` for the
full `insmod` command.

With this fix, Live's default 128-frame buffer works with no audible
glitches. You do not need a larger buffer, and you do not need to patch or
rebuild the kernel.

Any process that plays audio through this virtual card must match
whatever buffer size and channel count Live has already negotiated on
the other side of the card. Check
`/proc/asound/PHVAudio/pcm0c/sub0/hw_params` for the live values, and
match them exactly. If you do not match them, the second side fails to
open.

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

## Chords used to glitch: the fix was the same jiffies clock

Before the `timer_source` fix above, held chords glitched even after the
two bug fixes above, at every period size tried (256, 512, 1024 frames).
Measurement showed that `bridge_pcm_writei`, the blocking ALSA write call,
added a fixed ~10ms on top of the nominal period time, no matter the
period size. A fixed cost, not a cost proportional to the period, pointed
at one-time overhead per write call rather than per-frame processing.

This fixed cost came from the same jiffies clock described above: the
Loopback driver's default timer only frees buffer space for a blocking
writer once every 4 milliseconds, in a small number of ticks. Pointing
Loopback at a real hardware timer with `timer_source` removes this cost.
Confirmed on real hardware: with `timer_source` set, `push-braids-host`
runs with zero missed deadlines at Live's default 128-frame buffer, chords
included. `hacks/push-braids-host/README.md` gives the current recommended
period and buffer values.

## Running a test

1. Build and load `hacks/push-audio-loopback`'s virtual card with
   `timer_source` set — see that hack's own README for the exact
   `insmod` command.
2. Route an audio track's input to "Push Hack Virtual Audio" 1/2, with
   Monitor set to In.
3. Build and run `hacks/push-braids-host` — see that hack's own
   README for the exact command.
4. Press a pad. Real audio should come out Push 3's speaker or
   headphone jack.

## Where the code lives

- `hacks/push-audio-loopback/` — the virtual sound card and a
  low-jitter test-tone tool.
- `hacks/push-braids-host/` — the DSP host: MIDI in, Braids, audio
  out.
- `~/Developer/schwung-braids-main` — the Braids plugin source itself
  (not part of this repo; a separate Move Anything module).
