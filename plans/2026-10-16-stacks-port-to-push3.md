# Port Stacks to Push 3 as a catalog hack

Status: planned. Step 1 (the MIDI test) is done and passed on 2026-10-16.

## Goal

Stacks is a chord-progression tool for Ableton Move, built as a Schwung
module (`~/my_private_code/schwung-stacks`). This plan brings it to Push 3
Standalone as a push-hack catalog hack, `push-hack-stacks`, in its own repo.
The port also uses Push's much larger screen (960x160 color, against Move's
128x64 black and white), its 8 encoders, and its 64 pads.

## What Stacks is made of

| Part | Size | Port plan |
|---|---|---|
| `src/dsp/stacks.c` | ~4,900 lines of plain C | Keep. Build it for linux/amd64 and call it from Go through cgo. |
| `src/canvas.js` | ~2,100 lines | Rewrite in Go for 960x160. Keep it as the spec for gestures and drawing rules. |
| `src/ui.js` | lifecycle glue | Replace with the Go host's own lifecycle. |
| `src/web_ui.html` | browser panel | Keep. Add a small shim for `window.schwungRemote`. |
| `src/module.json` | manifest: params, banks of 8 knobs | Keep as the source of truth for params and banks. |

The C core needs only these things from its host:

- `log`, `get_bpm`, and `midi_inject_to_move` (the output path).
- MIDI clock: Start (`0xFA`), clock (`0xF8`), and Stop (`0xFC`).
- `move_info` (optional; it tells the core which track is selected).
- Song.abl, Move's set file. Only Read Clip uses it.
- The state file path, `/data/UserData/schwung/stacks/state.json`.

## Step 1 results (measured on the device, 2026-10-16)

The device runs Live 12.4.15b7 on AbletonOS v3.23. A Python test program
opened two ALSA sequencer clients: `Push Hack Stacks:Stacks Out` (readable)
and `Push Hack Stacks In:Stacks In` (writable).

1. **Notes reach Live.** Within about 20 seconds, Live connected its own
   input port (`Ableton Live:midi_out_5`) to `Stacks Out`, with no restart. A
   C major chord sent every 4 seconds played on the selected track. The user
   heard it, and the pads lit up. Like on Move, notes go to the track that is
   selected, so no MIDI channel needs to be chosen.
2. **Both ports show up in Push's MIDI settings.** `Stacks Out` is an input.
   `Stacks In` is an output, with a Sync switch that is off by default.
3. **Clock reaches the hack.** With Sync on, Live sent Start (event type 30)
   when the user pressed Play, about 24 clock pulses per beat while it
   played, and Stop (event type 32) when they pressed Stop. No clock arrives
   while the transport is stopped.
4. **Side finding:** `core/alsaseq/const.go` has `EvStop = 31` and
   `EvContinue = 32`. The kernel uses 32 for Stop, as the device showed, and
   31 for Continue. Any hack that uses these constants misreads Stop as
   Continue. This needs its own fix in `core/` and a new `core/v*` tag.

## Architecture

One Go binary, `push-hack-stacks`, installed by push-catalog.

- **The C core** is built with cgo for linux/amd64. The Go side provides a
  `host_api_v1_t` with `log`, `get_bpm`, and `midi_inject_to_move`. The last
  one writes to `Stacks Out`.
- **One goroutine owns the plugin.** It calls `tick` on a steady period,
  about 128 frames at 44.1 kHz, which is close to 2.9 ms. It holds
  `runtime.LockOSThread`. MIDI input goroutines only push events onto a
  channel; they never call into C. This avoids the bug described in
  `docs/push3-dsp-hosting.md`.
- **MIDI out:** the `Stacks Out` port. Live picks it up on its own.
- **Clock in:** the `Stacks In` port. The user turns on Sync for it once,
  in Push's MIDI settings. The hack's web page and screen should say so when
  no clock has arrived.
- **Pads and encoders:** subscribe to the Push 3 Live Port, which is
  normally `16:0`. Find it by name, as push-manager does. Call
  `alsaseq.WaitForBootSettle()` before any `/dev/snd` access.
- **Owning the surface:** while Stacks is on screen, turn on MIDI intercept
  (`pmclient.SetMidiFilter(true)`) so Live does not also see the pad
  presses. Turn it off when Stacks closes. push-manager's `led_restore.go`
  then repaints Push's own LEDs.
- **LEDs:** send to the Push 3 Live Port directly.
- **Screen:** go through push-manager only (`pmclient.SetMode`,
  `pmclient.PushImage`). Never map the framebuffer. Send a frame only when
  something changed.
- **State:** `/data/push-hack/hacks/stacks/state.json` would be wiped on a
  reinstall, so use a path outside the hack folder, such as
  `/data/push-hack/data/stacks/state.json`.
- **Browser panel:** serve `web_ui.html` with a `schwungRemote` shim that
  maps `setParam` and `getParam` to HTTP, and `rui_play` to a server-sent
  events stream (`core/sse`). Declare `web_ui` in `hack.json`, so Push
  Manager shows a menu-bar link.

## Screen layout (960x160)

- **Top band, about 24 px:** 8 columns of 120 px, one above each encoder.
  Each shows the parameter name and its value in full. The bank comes from
  `module.json`'s `ui_hierarchy`: Start, Chord, Voice, Feel, Clip.
- **Middle band, about 112 px:** the progression across the full width. All
  16 chord slots, each with its full name, its rhythm hits, and the
  playhead. A vertical keyboard sits on the left and lights the sounding
  notes. Each shape family gets its own color, matching its pad.
- **Bottom band, about 24 px:** labels for the 8 buttons under the screen.
  These select the bank, plus Library and Patterns.
- **Library view:** a scrolling list of genres and progressions
  (`core/gfx/widgets` ListView). It is far easier to browse 140+ entries
  here than on 128x64.
- All on-screen text must be ASCII (see CLAUDE.md, "Drawing text").

## Pad layout (8x8), first draft

| Rows | Use |
|---|---|
| 1-2 | 16 pattern slots: lit if used, bright if active |
| 3-4 | 16 chord slots, in family colors; the playing chord flashes |
| 5 | Degree pads I-VII and rest |
| 6-8 | Audition and play-chord pads, or a keyboard of the scale |

To be tested by hand on the device before it is fixed.

## Open questions

1. **Play and Record while Stacks is on screen.** Intercept blocks every
   Push button, and Move let Play (85) and Record (86) through. There are
   two options. (a) Add a per-CC pass list to the `midiflt` shm. It has 11
   reserved bytes, but this changes `push_hook.c`, which is the most
   sensitive part of the project. (b) Drive the transport through
   push-manager's `/api/live/play` and `/api/live/stop`, which need Browser
   Bridge. Record is not there yet.
2. **Frame cost.** `PushImage` encodes a PNG, and push-manager decodes it.
   Measure this at 30 fps on the device. If it costs too much CPU, propose a
   raw BGR565 endpoint for push-manager upstream.
3. **How Stacks opens.** Options: a Shadow UI tab entry, a button chord, or
   a soft button. It should not clash with push-manager's own chords
   (Shift+Settings and Shift+Set).
4. **Read Clip, Read Track, and Stamp.** Song.abl is Move only. On Push,
   Browser Bridge, which can use Live's API, could read the selected track
   and clip, and could write a clip directly. This needs new commands in
   `push-hack-browser-bridge`, a separate repo. Until then, record the clip
   with Live's own Record.
5. **Timing.** Notes go out with `QUEUE_DIRECT`, so jitter is up to one tick
   period, about 3 ms. Measure it. If needed, use an ALSA queue with
   timestamps.

## Improvements to push-hack found along the way

The port should leave push-hack better for every hack, not work around its
gaps. Each item is its own change, with tests and a CHANGELOG entry.

| # | Area | Change | Status |
|---|---|---|---|
| 1 | `core/alsaseq` | Fix the swapped `EvStop`/`EvContinue`, and pin every event type with a test | PR #31 upstream; needs a `core/v*` tag |
| 2 | `core/alsaseq` | `SetName` for the client. Push's MIDI settings show the PORT name, so this only helps diagnosis (`/proc`, `aconnect`). | PR #32 |
| 3 | `core/alsaseq` | `Subscribers` address, plus `EncodeMIDI`/`DecodeMIDI`/`SendMIDI` for raw MIDI bytes (what every ported Schwung tool speaks) | PR #32 |
| 4 | `core/alsaseq` | Real `/proc/asound/seq/clients` capture from the device as a test fixture | PR #32 |
| 5 | push-display + push-manager | Let chosen CCs (Play 85, Record 86) pass through MIDI intercept, using the reserved bytes in `midiflt` | Open question 1 |
| 6 | push-manager | Raw BGR565 frame endpoint, if PNG frames cost too much CPU | Open question 2 |
| 7 | Browser Bridge | Commands to read the selected track and clip, and to write a clip | Open question 4 |
| 8 | docs | Step 1's routing facts in `docs/push3-internals.md` | PR #32 |
| 9 | docs | `core/v0.2.1` exists upstream (it is `main` at 99bbb24), but CLAUDE.md and the docs still say v0.2.0 | To do |

## Design bar

The Push surface must be world class: an industrial-style interface where
form and function are both first-rate, not a port of the 128x64 layout.

- Design first. Render mockups of the 960x160 screen and the 8x8 pad grid
  as real images and review them before any drawing code is written.
- Every pixel earns its place: a strict grid aligned to the 8 encoders and
  the 8 buttons above and below the screen, a small, consistent set of
  colors with meaning, and no decoration.
- The screen and the pads tell one story: a chord's color on screen is the
  color of its pad.
- Deliverable: a Push control matrix (every bank, encoder, button, pad and
  gesture), like the Move one at
  https://claude.ai/artifact/3hAogpmmAxZvgFqbf38WvS, either as its own
  artifact or a second tab in that one.

## Steps

1. ~~MIDI test~~: done, see the results above.
2. **No-screen version.** Repo `push-hack-stacks` (local: `~/my_private_code/push-hack-stacks`): cgo build of `stacks.c`,
   the tick loop, the out and in ports, the HTTP server, and the browser
   panel with its shim. Usable from a phone. Run Stacks' C tests natively in
   CI, unchanged.
3. **Push surface.** The 960x160 renderer, encoder banks, pads and LEDs,
   intercept handling, and opening and closing Stacks.
4. **Browser Bridge additions** for Read Clip, Read Track, and writing
   clips.
5. **Publish:** release workflow, `release.json`, and a catalog entry
   (`catalog/PUBLISHING.md`).
