# core

Shared Go module for push-hack. push-manager, automation, and
keyboard-visualizer each pull this in via `require`+`replace` in their own
`go.mod` — every hack still builds and runs independently; `core` just stops
them from re-implementing the same ALSA ioctls, HTTP boilerplate, and SSE
plumbing three times over. See `../discovery/push-core-refactor.md` for the
extraction rationale and history, and `../CLAUDE.md`'s "Core shared library"
section for the same table kept in sync there.

**Using this from a new hack** — add to `hacks/<id>/src/go.mod`:
```
require github.com/federico-pepe/ableton-push-hack/core v0.0.0
replace github.com/federico-pepe/ableton-push-hack/core => ../../../core
```
A third-party hack living outside this repo can `require` the same import
path with **no** `replace` — `go get` resolves it from GitHub (tag prefix
`core/vX.Y.Z`) once tagged. That's why `core/go.mod`'s module path is
domain-qualified (`github.com/federico-pepe/ableton-push-hack/core`) rather
than a bare local name.

## Packages

| Package | What it's for |
|---|---|
| `push3` | Zero-import Push 3 facts: the full button/encoder MIDI map, the 128-entry named LED palette (`ColorByName`), display geometry (`VisW/VisH/Stride/FrameBytes/TotalBytes`), and encoder math (`IsEncoderCC/DecodeRel/ScaleVal/ClampInt`). Every other package here builds on this one; it imports nothing itself. |
| `gfx` | Stdlib-only (`image`/`image/draw`) drawing primitives: `FillRect`, `DrawIcon`. Kept free of `golang.org/x/image` on purpose — see `gfx/text`. |
| `gfx/text` | The one `golang.org/x/image` consumer: `Draw`/`Width`/`Truncate` for basicfont text. Split out of `gfx` so hacks that never draw text (automation, keyboard-visualizer) don't link that dependency into their binaries. |
| `display` | `codec.go`: `ToBGR565`/`FromBGR565` pixel codecs, pure functions, no I/O. `shm.go`: `Shm`, the mmap bridge to push-display's `push_hook.so` shared-memory framebuf — **`os.O_RDWR` with no `O_CREATE`**, because push_hook.c is the sole creator and push-manager (via this type) is the sole writer; don't add `O_CREATE` here. Single consumer today (push-manager); other hacks reach the display over HTTP via `pmclient` instead, never this package directly. |
| `httpx` | The HTTP boilerplate every hack had three copies of: `WithLogging`, `WithCORS(allowMethods, next)`, `JSON`, `Error`, `NewServer` (30s read / 5min write / 120s idle timeouts), `ServeEmbedded` for a single-file embedded UI. |
| `hackcfg` | `Config` + `Load(path, defaultPort)` — the minimal id/name/version/port shape a hack.json decodes into. push-manager's config is a strict superset (allowed_roots, settings, `~` expansion) and doesn't use this; it's file-browser policy, not a shared shape. |
| `sse` | Generic `Broker[T]` (register/unregister/broadcast a channel per client) + `Serve[T]` for the SSE HTTP response loop. `NewBroker`'s `pruneDropped` flag is a real, intentional behavioral choice per caller — see the package doc comment before assuming it should be unified away. |
| `pmclient` | An HTTP client for push-manager's own API: `SetMode`, `PushImage`, `DisplayStatus`, `Tempo`. Any hack that wants to draw on Push's screen or read Live's tempo goes through this — never through `display` or shm directly. This is `CLAUDE.md`'s "display-owning hacks" rule, enforced by the type system instead of by convention. |
| `alsaseq` | The ALSA sequencer layer: `/dev/snd/seq` ioctls, zero cgo. `const.go` is kernel ABI (ioctl numbers, struct byte offsets, event types) — treat it as fixed; if you think a value is wrong, diff against `/usr/include/sound/asequencer.h` before touching it, don't guess. `bootsettle.go`: `WaitForBootSettle()`, mandatory before any hack's first `/dev/snd` access (see USB-A safety below). `client.go`/`event.go`: `Client` — open a port, subscribe, send CC/Note/SysEx. `ports.go`: enumerate `/proc/asound/seq/clients`, find a port by name. `reader.go`: `Handler` + `Walk`/`ReadLoop` — decode a stream of fixed and variable-length (SysEx) events without desyncing on the variable-length ones. |

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
