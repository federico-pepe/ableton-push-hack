# core

Shared Go module for push-hack. Every hack still builds and runs
independently; `core` stops them from re-implementing the same ALSA ioctls,
HTTP boilerplate, and SSE plumbing. See `../discovery/push-core-refactor.md`
for the extraction rationale and history, and
[`../docs/architecture.md`](../docs/architecture.md#core-shared-library-core)
for the same package table kept in sync there.

Two kinds of consumers use it:

- The core hacks in this repo (push-manager) use `require` + `replace
  ../../../core`, so they always build against the `core/` in the same
  commit.
- Hacks in their own repos (automation, keyboard-visualizer, and every
  other catalog hack) and `push-tethered-app` pin a tagged version, with
  **no** `replace`.

**Using this from a new hack in its own repo** — add to `src/go.mod`:
```
require github.com/federico-pepe/ableton-push-hack/core v0.3.0
```
Use the newest `core/vX.Y.Z` tag. `go get` resolves it from GitHub. That is
why `core/go.mod`'s module path is domain-qualified
(`github.com/federico-pepe/ableton-push-hack/core`) rather than a bare local
name.

**Releasing a `core` change:** merge it, then tag `core/vX.Y.Z` and push the
tag. Consumers in other repos see the change only after they bump their pin
(`go get github.com/federico-pepe/ableton-push-hack/core@vX.Y.Z`).

## Packages

| Package | What it's for |
|---|---|
| `push3` | Zero-import Push 3 facts: the full button/encoder MIDI map, the 128-entry named LED palette (`ColorByName`), display geometry (`VisW/VisH/Stride/FrameBytes/TotalBytes`), and encoder math (`IsEncoderCC/DecodeRel/ScaleVal/ClampInt`). Every other package here builds on this one; it imports nothing itself. |
| `gfx` | Stdlib-only (`image`/`image/draw`) drawing primitives: `FillRect`, `DrawIcon`. Kept free of `golang.org/x/image` on purpose — see `gfx/text`. |
| `gfx/text` | The one `golang.org/x/image` consumer: `Draw`/`Width`/`Truncate` for basicfont text. Split out of `gfx` so hacks that never draw text (automation, keyboard-visualizer) don't link that dependency into their binaries. |
| `gfx/widgets` | Shared Shadow-UI-style components built on `gfx`/`gfx/text`: `Theme` (named color palette), `SoftButton`/`SoftButtonState`/`DrawBotStrip` (semantic button state instead of matching on label text), `ListRow`/`ListView`/`RenderList` (scrollable list + breadcrumb + scrollbar), `KVRow`/`DrawKVRows` (label:value rows), `DrawTouchHeader`/`HeaderCol`/`DimImage`/`DrawOnOffBox` (the dim-and-header overlay Push shows while you touch a knob or press a button above the screen: per column the button function with an On/Off box, the knob name and its value), plus ahead-of-need primitives (`DrawBorder`/`DrawMeter`/`DrawArc`, a `Knob` type). Operates only on plain `image.NRGBA` — no shm, no hack-specific state — so it's usable by push-manager's Shadow UI, keyboard-visualizer's renderer, or any future screen-drawing hack. All 4 of push-manager's Shadow UI panels (Files/Stats/Midi/Browser) are migrated onto this; see `../discovery/shadow-ui-component-framework.md`. |
| `display` | `codec.go`: `ToBGR565`/`FromBGR565` pixel codecs, pure functions, no I/O. `shm.go`: `Shm`, the mmap bridge to push-display's `push_hook.so` shared-memory framebuf — **`os.O_RDWR` with no `O_CREATE`**, because push_hook.c is the sole creator and push-manager (via this type) is the sole writer; don't add `O_CREATE` here. Single consumer today (push-manager); other hacks reach the display over HTTP via `pmclient` instead, never this package directly. |
| `httpx` | The HTTP boilerplate every hack had three copies of: `WithLogging`, `WithCORS(allowMethods, next)`, `JSON`, `Error`, `NewServer` (30s read / 5min write / 120s idle timeouts), `ServeEmbedded` for a single-file embedded UI. |
| `hackcfg` | `Config` + `Load(path, defaultPort)` — the minimal id/name/version/port shape a hack.json decodes into. push-manager's config is a strict superset (allowed_roots, settings, `~` expansion) and doesn't use this; it's file-browser policy, not a shared shape. |
| `sse` | Generic `Broker[T]` (register/unregister/broadcast a channel per client) + `Serve[T]` for the SSE HTTP response loop. `NewBroker`'s `pruneDropped` flag is a real, intentional behavioral choice per caller — see the package doc comment before assuming it should be unified away. |
| `pmclient` | An HTTP client for push-manager's own API: `SetMode`, `PushImage`, `PushFrame` (raw frames, no PNG), `Push` (`PushFrame` when push-manager has it, `PushImage` when not), `DisplayStatus`, `SetMidiFilter`/`SetMidiFilterPass`, `MidiFilterStatus` (and whether push-manager supports hacks that own the surface), `Tempo`, `LiveScale` (the scale Push's Scale button sets), `LiveListen` (a track that hands the notes played on Push to a hack), and Push Hack Home for a hack that takes the whole surface: `Client.App` (its id, sent as `X-Push-Hack-App` with every write so push-manager lets the owning app's frames through), `RunSurface` (holds the session, reconnects, calls `OnFocus`/`OnBlur`; `ErrNoSwitcher` on an older push-manager), `SurfaceStatus`, `SurfaceInput`, `SurfaceLeave`, `SurfaceOpen`, `HasSwitcher`. Any hack that wants to draw on Push's screen or read Live's tempo goes through this — never through `display` or shm directly. This is `CLAUDE.md`'s "display-owning hacks" rule, enforced by the type system instead of by convention. |
| `harmony` | Push Hack Harmony, how one app tells others what it plays (the key, the scale, the chord) over MIDI: `Encode`/`Decode` for the SysEx message (with `delta`, a change announced in clock pulses before it happens), `EncodeType` for ALIVE and BYE, `Detect` (a chord named from notes), `Grouper` (notes into chords, per source), `KeyFinder` (the key of what is played), and helpers for chord roles and scales (`Third`, `Fifth`, `Seventh`, `ChordScale`). Pure Go, no I/O. Spec: [`../docs/harmony-sync.md`](../docs/harmony-sync.md). Used by Stacks (sends). |
| `alsaseq` | The ALSA sequencer layer: `/dev/snd/seq` ioctls, zero cgo. `const.go` is kernel ABI (ioctl numbers, struct byte offsets, event types) — treat it as fixed; if you think a value is wrong, diff against `/usr/include/sound/asequencer.h` before touching it, don't guess. `bootsettle.go`: `WaitForBootSettle()`, mandatory before any hack's first `/dev/snd` access (see USB-A safety below). `client.go`/`event.go`: `Client` — open a port, name the client (`SetName`), subscribe and unsubscribe, send CC/Note/SysEx. `midi.go`: raw MIDI bytes to and from events (`EncodeMIDI`/`DecodeMIDI`/`SendMIDI`) for ported tools that speak raw MIDI, and `Subscribers`, the address that reaches every port connected to yours (Live connects to a new output port by itself). `ports.go`: enumerate `/proc/asound/seq/clients`, find a port by name. `reader.go`: `Handler` + `Walk`/`ReadLoop` — decode a stream of fixed and variable-length (SysEx) events without desyncing on the variable-length ones. |

## Two rules that aren't optional

**USB-A safety.** Any code path that opens `/dev/snd/seq` — directly or via
`alsaseq.Client`/`Open` — must sit behind `alsaseq.WaitForBootSettle()` on
first access after boot. Opening ALSA seq during the ~3-15s USB-A
enumeration window after a cold power-on can wedge that port until a power
cycle. This has already bitten real deploys; it is not a hypothetical.

**Display single-writer.** Only push-manager (via `core/display.Shm`) ever
writes the shm framebuf, and it never creates it — `push_hook.so` (C,
`hacks/push-display`) is the sole creator. Every other hack reaches the
display exclusively through `core/pmclient`'s HTTP calls. Don't mmap the
framebuf from a second process; that single-writer discipline is what keeps
the shm protocol from racing.

`core/display.Mirror` is the read side of the hook's separate `mirror` file (the
frame really sent to the screen): `PROT_READ`, never created here, seqlock `Read`.
Only push-manager uses it, to serve `GET /api/display/mirror`.

## Testing

```bash
make test    # from repo root: go test ./core/...
make vet     # GOOS=linux GOARCH=amd64 go vet ./core/...
```

Everything testable off-device is tested: the codec round-trip, the ALSA
event walker (including the fixed→varlen→fixed desync case), port-list
parsing (fixtures in `alsaseq/testdata/`), the CORS/SSE behavioral
contracts. What isn't and can't be: anything that actually opens
`/dev/snd/seq` or the shm file, LED output, real display frames, boot-settle
timing, the USB-A wedge itself. Those need `hacks/*/README.md`'s per-hack
smoke checklist on real hardware.
