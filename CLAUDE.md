# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> **Doc sync rule:** Keep this file, all `docs/` files, and `README.md` in sync with every code change. If a change affects behaviour, APIs, architecture, or known issues — update the relevant docs in the same commit.

## Project

`push-hack` — extensible hack framework for Ableton Push 3 (Intel Linux, runs full Ableton Live). Deploys via SSH. Never modifies system partition. Hacks: Push Manager (web file browser + display control), Push Display (LD_PRELOAD display hook), Browser Bridge (Live MIDI Remote Script to load `.adv`/`.adg` presets — **one-time manual activation required**), Automation (LFO/CC curve sequencer, port 7703), Keyboard Visualizer (on-screen piano keyboard sourced from Live's post-transform notes, port 7702).

`core/` is a shared Go module (see "Core shared library" below) that push-manager, automation and keyboard-visualizer all depend on via `require`+`replace` — extracted per `discovery/push-core-refactor.md` to kill the ALSA/HTTP/SSE triplication that had silently diverged across the three hacks.

**Core constraint:** Push is a live performance tool. Hacks must not crash it, hog CPU, or consume significant memory.

**⛔ Hard safety rules — never violate, no exceptions:**
- **Never modify `/boot/`** — bricks Push.
- **Never modify `/opt/`** — read-only system partition. Contains Push3 app, firmware, assets.
- **Never modify kernel parameters** — no `sysctl -w`, no `/proc/sys/` writes affecting stability.
- **Never write to `/etc/`** except: `/etc/udev/rules.d/99-push-hack-*.rules`, `/etc/init.d/push-hack-*`, and the LD_PRELOAD line in `/etc/init.d/push3` (all managed by install/uninstall scripts).

## Commands

### Build
```bash
cd hacks/push-manager && PATH=$PATH:/usr/local/go/bin make
cd hacks/automation && PATH=$PATH:/usr/local/go/bin make
cd hacks/push-display && make          # cross-compiles push_hook.so via Docker
```

### Test (core/ shared library)
```bash
make test    # go test ./core/...
make vet     # GOOS=linux GOARCH=amd64 go vet ./core/... — off-device, no ALSA/shm coverage
```

### Deploy
```bash
./scripts/install.sh                              # deploy all enabled hacks (pre-built)
./scripts/install.sh --hack push-manager --build  # build from source then deploy
./scripts/uninstall.sh                            # remove all hacks + services
./scripts/uninstall.sh --purge                    # also delete /data/push-hack/ data
hacks/push-display/deploy.sh                      # standalone push-display re-deploy
```

### Discovery
```bash
./scripts/discover.sh                    # probe Push OS, print filesystem map
```

## Architecture

### Framework layer (`scripts/`, `lib/`)
SSH-based deploy system. `lib/common.sh` — shared SSH helpers (`push_exec`, `push_exec_root` use `-n` to prevent stdin consumption in loops), Push path detection, service install/remove. Push uses **sysvinit**, not systemd. Stop service before SCP — running binary is locked on Linux. Regular binaries copied as `ableton`; `.so` files copied as `root` via `push_copy_root`. `check_connection()` auto-clears a stale SSH host key (`clear_host_key()` → `ssh-keygen -R`) when an OS update regenerated the device key — detects `REMOTE HOST IDENTIFICATION HAS CHANGED` and retries.

### Hack structure (`hacks/<hack-id>/`)
- `hack.json` — metadata: id, name, version, port, allowed_roots, binary, enabled
- `service.initd` — optional custom init.d template; placeholders: `{{SVC_NAME}}`, `{{HACK_DIR}}`, `{{LOG_DIR}}`, `{{PORT}}`
- `remote-script/` — optional payload copied to `<remote_hack_dir>/remote-script` by install.sh
- Binary deployed to `/data/push-hack/hacks/<id>/`; service at `/etc/init.d/push-hack-<id>`

### Core shared library (`core/`)
Nested Go module (`github.com/federico-pepe/ableton-push-hack/core`, own `go.mod`) that push-manager/automation/keyboard-visualizer each pull in via `require`+`replace ../../../core` in their own `go.mod` — hacks stay independently buildable, and a third-party hack in its own repo could `require` the same path without a `replace` and resolve it from GitHub. See `discovery/push-core-refactor.md` for the full extraction plan/rationale.

| Package | Contents |
|---|---|
| `core/push3` | Zero-import Push 3 facts: full button/encoder MIDI map (`buttons.go`), 128-entry named LED palette + `ColorByName` (`colors.go`), display geometry `VisW/VisH/Stride/FrameBytes/TotalBytes` (`geometry.go`), encoder helpers `IsEncoderCC/DecodeRel/ScaleVal/ClampInt` (`encoder.go`, tested in `encoder_test.go`). push-manager's `push3_buttons.go` re-exports the button/encoder consts as package-`main` aliases (`const CCShift = push3.CCShift`, etc.) so its ~180 existing call sites across `midi.go`/`ui_shadow.go` didn't need touching — `core/push3` is still the single source of truth. |
| `core/gfx`, `core/gfx/text` | Stdlib-only image primitives (`FillRect`, `DrawIcon`) in `gfx`; the only `golang.org/x/image` consumer (`DrawText`/`TextWidth`/`Truncate`, basicfont) split into `gfx/text` so automation and keyboard-visualizer's zero-external-dependency binaries stay that way — verified via `go list -deps`. |
| `core/gfx/widgets` | Shared Shadow-UI-style drawing components built on `gfx`/`gfx/text`: `Theme` (named color palette), `SoftButton`/`DrawBotStrip` (semantic button state instead of string-matching label text), `ListRow`/`ListView`/`RenderList` (scrollable list + breadcrumb + scrollbar, generalizing push-manager's `FilePanel`/`BrowserPanel`), `KVRow`/`DrawKVRows` (label:value rows, generalizing `StatsPanel`/`MidiPanel`), plus ahead-of-need primitives (`DrawBorder`/`DrawMeter`/`DrawArc`, `Knob`). Operates only on plain `image.NRGBA` — no shm, no hack-specific state — so any hack drawing on Push's screen can share it (keyboard-visualizer is a candidate second adopter for `Theme`, not yet done). See `discovery/shadow-ui-component-framework.md`. All 4 push-manager panels migrated (list/row rendering + `SoftBotStrip`); `Panel` interface's `BotStrip()` is gone, replaced by `SoftBotStrip()`. |
| `core/display` | `codec.go`: `ToBGR565`/`FromBGR565` pixel codecs (tested in `codec_test.go` — duplicate-frame invariant, stride padding, round-trip). `shm.go`: `Shm` struct wrapping the push_hook.so shared-memory mmap (`Ensure/Connected/Mode/SetMode/ReadFrame/WritePixels/CompareAndSetMode/FrameSeq`) — **`os.O_RDWR` with no `O_CREATE`**, push_hook.c is the sole creator, push-manager the sole writer (see "Display-owning hacks" below). Single consumer (push-manager); other hacks reach the display via `core/pmclient` instead. |
| `core/httpx` | `WithLogging`, `WithCORS(allowMethods, next)` (allowMethods is the one thing that ever diverged per-hack, pinned by `middleware_test.go`), `JSON`, `Error`, `NewServer` (30s read / 5min write / 120s idle timeout triple), `ServeEmbedded` (automation's and keyboard-visualizer's identical single-file `handleUI`; push-manager's three-file UI keeps its own handler). |
| `core/hackcfg` | `Config` + `Load(path, defaultPort)` — the minimal id/name/version/port shape automation and keyboard-visualizer both used. push-manager's config is a strict superset (allowed_roots, settings, `~` expansion) and stays put. |
| `core/sse` | Generic `Broker[T]` (`Register/Unregister/Broadcast`) + `Serve[T]` SSE HTTP helper. `NewBroker`'s `pruneDropped` parameter preserves a real behavioral difference: automation drops a client whose channel is full (`true`), keyboard-visualizer does not (`false`) — pinned by `broker_test.go`. |
| `core/pmclient` | HTTP client for push-manager's display/tempo API — `SetMode`, `PushImage`, `DisplayStatus`, `Tempo`. Turns the "display-owning hacks go through push-manager's HTTP API" rule (see below) into a compiler-enforced one. Used by keyboard-visualizer (display takeover, dependency watcher) and automation (BPM poll fallback). |
| `core/alsaseq` | The ALSA sequencer layer, `/dev/snd/seq` ioctls, no cgo. `const.go`: kernel ABI (ioctl numbers, struct offsets, event types — verbatim move, diffed against all three hacks' prior copies). `bootsettle.go`: `WaitForBootSettle` (defers `/dev/snd` access past the USB-A cold-boot enumeration window). `client.go`: `Client`/`Open`/`CreatePort`/`Subscribe`/`Addr`/`FD`/`Close`. `event.go`: `WriteEvent`/`SendCC`/`SendNote`/`SendSysEx`. `ports.go`: `Port`/`ParseClients`/`EnumPorts`/`FindByName` (tested against `testdata/seq_clients_*.txt` fixtures — bare and shifted-client-number cases; hand-constructed, not yet captured off real hardware). `reader.go`: `Handler` interface + `Walk`/`ReadLoop` — the shared event decoder that fixed automation's SysEx desync bug (its own walker had no variable-length branch; `reader_test.go`'s `TestWalkFixedVarlenFixed` is the regression test). |

### Push Manager (`hacks/push-manager/`)
Go binary, no runtime deps. ~8–15MB RSS. Port 7701. See `hacks/push-manager/README.md` for full API.

| File | Role |
|------|------|
| `src/main.go` | HTTP server, routes, middleware |
| `src/files.go` | Filesystem ops with path traversal guard |
| `src/stats.go` | CPU/memory/disk/uptime/IP stats; top processes (Ableton Index, Live, Push3, push-manager) |
| `src/presets.go` | Preset index: scans `.adv`/`.adg` under Core Library, Factory Packs, User Library. In-memory cache + `presets.json`. `QueryPresets(PresetFilter)`, `presetFacets()`. Metadata (favourites, tags) in `preset_meta.json`. |
| `src/live_bridge.go` | One-shot TCP to `127.0.0.1:7704` (Browser Bridge). `liveLoad(name, category)` → `load:<root>:<name>`. Also: `livePlay()`, `liveStop()`, `liveIsPlaying()`, `liveTempo()`, `liveBeat()`. |
| `src/display.go` | Shared-memory bridge to push_hook.so, now a thin wrapper over `core/display.Shm` (`var shm = &coredisplay.Shm{}`) — `shmGetMode`/`shmSetMode`/`shmReadFrame`/`shmWritePixels` delegate straight through so `ui_shadow.go`'s call sites didn't need touching. Three modes: 0=passthrough, 1=bar, 2=takeover. OSD subsystem: single-line and multi-line renderers (unchanged, stayed put — no second consumer). Startup splash on fresh hook attach, wired via `Shm.OnConnect`. Screenshot: `shmReadFrame`+`bgr565ToImage` (→ `core/display.FromBGR565`) read the framebuf back and `png.Encode` it — captures only push-manager-owned frames (Shadow UI/OSD/image), not the native Ableton UI (never copied into shm in passthrough). |
| `src/midi.go` | ALSA seq subscriber + LED output, now built on `core/alsaseq` (`Client`/`Open`/`CreatePort`/`Subscribe`/`WriteEvent`/`SendCC`/`SendNote`/`SendSysEx`/`Walk`/`ReadLoop` — the kernel ABI consts and raw ioctls that used to live here moved to that package). **Boot-settle:** `alsaseq.WaitForBootSettle()` defers `/dev/snd` access until uptime ≥ 30s (USB-A safety). **Auto-detect:** `detectPush3Port()` calls `alsaseq.FindByName()` on each connection attempt — handles shifted client numbers (e.g. 20 instead of 16) when USB MIDI devices are connected at boot; disabled once user manually subscribes. LED config system (trigger/momentary/exclusive modes, animations). Chords: Shift+Settings=intercept toggle, Shift+Set=open browser. |
| `src/remap.go` | MIDI remapping. `MidiMapping` (src→out CC/Note), `applyRemap()` called from `processFixedEvent` — transforms a Push control's value and sends to a user-selected writable ALSA port via `sendSeqCCTo`/`sendSeqNoteTo` (reuses `midiOut`, the shared `*alsaseq.Client`, no new port). Absolute sources scale velocity into `[min,max]`; relative encoders (CC 71-79/14) accumulate deltas (`push3.DecodeRel`, `remapAccum`) clamped to range. Gated by `remapEnabled` + optional `remapRequireIntercept`. Persisted in `midi.json` via `midiPersistData`. |
| `src/ui/index.html`, `app.css`, `app.js` | Three-file SPA (not single-file — all three embedded at `main.go:19`) — file browser, display control, MIDI monitor, LED panel, MIDI mapping panel (learn/manual + writable-port dropdown), preset browser tab. |
| `src/ui_shadow.go` | On-device Shadow UI (10fps, Push 3 display). Four panels: FilePanel, StatsPanel, MidiPanel, BrowserPanel — all four now render via `core/gfx/widgets` (`KVRow` for Stats/Midi, `ListView`/`RenderList` for Files/Browser, `SoftButton` for every panel's bottom strip; see that package's row above). `loadSuiIcon`/`iconNameForEntry`/`iconNameForPreset` (icon resolution, reads `/opt/push3/.../Images/Browser/`) and panel-specific input handling (`HandleCC`, cursor/scroll math) stay local — deliberately not extracted, per discovery/shadow-ui-component-framework.md's scope. Activated by MIDI intercept; triggered by Shift+Set chord. While active it fully owns the 4 under-screen soft-buttons' LEDs — the generic trigger/momentary dispatch in `midi.go` is suppressed for CC 20–23 (`isScreenBotCC`), so panels drive them directly. Browser: SEARCH opens the on-screen keyboard (DONE lit green → white on exit); FILTER/REFRESH are momentary (green while held → white on release). MidiPanel has a MONITOR sub-view (Bot3): live event log read from `midiRing`, soft-buttons toggle the display-filter categories (Bot1-4 Sens/SysEx/CC/Note + Bot5 Chan Pressure — same classification as the web UI). Extra soft-buttons beyond the primary 4 use the optional `extraBots` interface (buttons 5-8, CC24-27); `isScreenBotCC` now covers CC20-27. Re-press the MIDI tab to exit the sub-view. (Input port is *not* selectable on-device — subscribing away from the Push port would kill the Shadow UI's own MIDI feed; change it from the web UI only.) |
| `src/live_log.go` | Support-detection marker. Polls `/proc` for the Live process (`findWatchedPIDs`); when a new Live instance appears, waits an 8s grace (Live truncates its `Log.txt` on launch) then appends one native-format line `…: info: push-hack loaded: <id> v<ver>, …` to the newest `/data/.config/Ableton/Live */Log.txt`. Lists all deployed hacks + versions (scans `/data/push-hack/hacks/*/hack.json`). Re-marks on Live restart. Independent of push-display so it works with push-manager alone. |

**Key routes:** `/api/list`, `/api/download`, `/api/upload`, `/api/delete`, `/api/rename`, `/api/copy`, `/api/unmount`, `/api/stats`, `/api/assets/<path>`, `/api/display/{status,mode,image,screenshot}` (`screenshot` = GET, PNG of current framebuf, `X-Display-Mode` header), `/api/midi/{events,stream,filter,ports,subscribe,chords,led,palette,mapping,mapping/config}` (`ports?writable=1` lists output destinations), `/api/presets`, `/api/presets/{refresh,facets,meta}`, `/api/live/load`, `/api/live/tempo`, `/api/live/playing`, `/api/live/play`, `/api/live/stop`.

**File ownership:** push-manager runs as root; chowns all created files to match parent dir owner (ensures `ableton:users` ownership).

**USB drives:** auto-mount to `/run/media/<label>-<device>`. After `syscall.Unmount`, delete `/tmp/.automount-<name>` so drive can re-mount on replug.

### Push Display (`hacks/push-display/`)
LD_PRELOAD hook (C shared library) injected into Push3 process only (checks `/proc/self/comm == Push3`). Intercepts `libusb_bulk_transfer` for display overlay/takeover, and `snd_seq_event_input` for MIDI neutralization. 8s boot grace window before activating. `make splash` regenerates `src/splash_data.h`.

**⚠️ Ableton OS updates freeze with the hook installed.** Push3 itself drives the update and flashes co-processor firmware over the same USB/libusb path the hook interposes; the collision hangs the device mid-update (blank screen, dead buttons). An in-process kill-switch was tried and **does not work** — an LD_PRELOAD interposition can't be removed from a running process, and by the time any update signal appears Push3 is already the hooked process flashing firmware. **You must uninstall the hack (`./scripts/uninstall.sh`) before running an OS update, then reinstall after.** See README.

**Shared memory layout** (must stay in sync between `push_hook.c` and `display.go`):
```
offset  0: uint32 magic      (0x50555348 "PUSH")
offset  4: uint32 version    (1)
offset  8: uint32 mode       (0=passthrough, 1=bar, 2=takeover)
offset 12: uint32 frame_seq  (incremented by push-manager on each image write)
offset 16: uint8[655360]     BGR565 pixels (960×160, stride 1024, frame duplicated)
total: 655376 bytes, permissions 0666
```

**Display geometry:** 960×160 px, BGR565 XOR-shaped (`{0xE7,0xF3,0xE7,0xFF}` repeated), stride 1024, frame sent twice.

### Automation (`hacks/automation/`)
Go binary, no runtime deps. Port 7703. LFO-style MIDI CC automation sequencer. All lanes send MIDI CC to Live's ALSA input port (`128:2`).

| File | Role |
|------|------|
| `src/main.go` | HTTP server, routes, lifecycle. `-push-manager` flag sets push-manager base URL (default `http://localhost:7701`). |
| `src/engine.go` | `AutoLane`, `AutoState`, `CurvePoint`. 50Hz playback goroutine. Linear + Catmull-Rom interpolation. `TransportSync bool`. SSE broadcast (20Hz) via `core/sse.Broker[autoStreamPayload]` (`pruneDropped=true`). `pollTempo` uses `core/pmclient.Client.Tempo()`. Persistence to `automation.json`. |
| `src/midi.go` | ALSA seq output + input, now built on `core/alsaseq` (two `*alsaseq.Client`s: **Push Hack Automation** output → Live 128:2, **Push Hack Clock** input ← Push3 16:0). `detectLivePort()`/`detectPush3Port()` call `core/alsaseq.EnumPorts`/`FindByName`. Reads MIDI clock (24 PPQN) for BPM via `autoSeqHandler.Fixed` (an `alsaseq.Handler`, replacing the old fixed-stride-only `readMidiEvents` — sharing `alsaseq.Walk` fixed a real bug where a SysEx byte stream from Push 3 desynced the old decode). `onPlayButtonPress()` handles CC85 val=127 — sole transport toggle when synced. Boot-settle (30s, `alsaseq.WaitForBootSettle`) same as push-manager. |
| `src/ui/index.html` | Single-file SPA. Canvas curve editor per lane (click=add, drag=move, right-click=delete). Sync to Live checkbox — when on, play/stop button becomes a read-only status indicator (● Playing / ○ Stopped) driven by SSE. Max 8 lanes. |

**Key routes (port 7703):** `GET /api/auto/state`, `POST /api/auto/play`, `POST /api/auto/stop`, `POST /api/auto/transport_sync`, `POST /api/auto/lane`, `PUT /api/auto/lane/{id}`, `DELETE /api/auto/lane/{id}`, `POST /api/auto/lane/{id}/reset`, `GET /api/auto/stream` (SSE).

**BPM sync:** MIDI clock from Push3:16:0 → 24-tick ring buffer → `BPM = 60.0 / elapsed_per_beat`. Falls back to last known BPM (default 120) if no clock received in 5s. HTTP `/api/live/tempo` is still available as an alternative but not used by the engine.

**Transport sync:** when `TransportSync=true`, the **Push Play button (CC85 val=127)** is the SOLE driver of `Running` — each press toggles play/stop (Push has no stop button). MIDI Start only resets the BPM clock ring; it does NOT touch transport. WebUI play/stop button becomes a read-only SSE-driven indicator. (Earlier versions had CC85 toggle + MIDI Start/Stop + a `/api/live/playing` poller all fighting over `Running`, causing desync — now removed.)

**Persistence:** `automation.json` at `/data/push-hack/hacks/automation/automation.json`. Atomic write (tmp + rename).

### Browser Bridge (`hacks/browser-bridge/`)
MIDI Remote Script (`PushHackBrowser`) that loads presets and controls Live's transport + device parameters. **One-time manual activation required** (deploy installs the script into the User Library, but Live won't load it until you select `PushHackBrowser` in a free control-surface slot with Input/Output = None, then restart Live). Verify in Live's `Log.txt`. See `hacks/browser-bridge/README.md` for how it works.

**Commands (TCP port 7704):**
- `load:<root>:<name>` — load preset onto selected track
- `load_uri:<uri>` — load by browser URI
- `ping` — health check
- `play` / `stop` — start/stop Live's transport (fire-and-forget)
- `get_tempo` → `"%.4f\n"` — current song BPM (reply-box query)
- `get_beat` → `"%.6f\n"` — current song time in beats (reply-box query)
- `get_playing` → `"1\n"` or `"0\n"` — transport state (reply-box query)

### Keyboard Visualizer (`hacks/keyboard-visualizer/`)
Go binary, no runtime deps. Port 7702. Renders a piano-keyboard visualization on Push 3's screen driven by Live's actual sounding notes (after octave-shift / Scale-mode transforms) — not the pad grid's raw pre-transform MIDI. See `discovery/live-note-keyboard-viz.md` for the feasibility writeup and `hacks/keyboard-visualizer/README.md` for setup.

**Requires push-manager + push-display also installed and running** — see "Display-owning hacks" below. `src/depcheck.go` checks `push-manager`'s `/api/display/status` on startup and every 30s, logging a clear warning (`push-manager not reachable` vs. `push-display's framebuffer not connected` vs. OK) so a silent no-op takeover is never a mystery.

| File | Role |
|------|------|
| `src/main.go` | HTTP server: `GET /` (embedded mobile web view), `GET /vendor/tonal.min.js` (embedded), `GET /api/status`, `GET /api/notes/stream` (SSE). `-push-manager` flag sets push-manager base URL (default `http://localhost:7701`). Boot-settle then starts the ALSA read loop and render loop as goroutines. |
| `src/depcheck.go` | Polls push-manager's display status via `core/pmclient.Client.DisplayStatus()` (`{"connected": bool}` — reflects whether push-display's shm is attached) at startup and every 30s, using its own 2s-timeout client rather than `pmclient.New`'s 3s default; logs once per state transition, not every poll. Doesn't wait for boot-settle (pure HTTP, no ALSA/USB risk). |
| `src/midi.go` | Creates a single writable ALSA seq port, **Keyboard Viz In**, via `core/alsaseq.Client` (`CapWrite\|CapSubsWrite`, same pattern as push-manager's own "Push Manager In"). Live initiates the connection when the user routes a track's MIDI Out to this port from Push's own screen — no subscribe needed on our side for that. Separately, `maintainPush3Subscription()` calls `c.Subscribe()` to self-subscribe this same port to Push 3's hardware port ("Ableton Push 3 Live Port", auto-detected by `core/alsaseq.FindByName` with `requireCaps=0`, re-checked every 30s in case the client number shifts) so it also receives CC49/50 for the takeover chord — one ALSA seq port can receive from multiple senders, so this avoids a second visible ALSA client. Because Push 3's raw pad stream now shares the port with Live's routed notes, `kvSeqHandler.Fixed` (an `alsaseq.Handler`, fed by `alsaseq.Walk`/`ReadLoop`) reads each event's ALSA source client and filters Note On/Off by `isPush3Client()` — only non-Push3 sources (i.e. Live) update the held-notes `[128]bool`, so a pad press doesn't double up with Live's post-transform note. CC49/50 are forwarded to `onChordCC` (chord.go) regardless of source. Boot-settle (30s, `alsaseq.WaitForBootSettle`) same as push-manager/automation. |
| `src/chord.go` | Shift+Note (CC49+CC50) chord state machine — 500ms-debounced (same pattern as push-manager's `chordCCPressed`/`chordCCReleased`), calls `toggleTakeover()` when both are held together. Also holds `detectPush3Port()` (now `core/alsaseq.FindByName` with `requireCaps=0` — the one deliberately preserved divergence from push-manager's/automation's `CapRead`-filtered lookups), used by `midi.go`'s subscription maintenance. |
| `src/render.go` | Draws the keyboard into a plain `image.NRGBA` (960×160, fixed 49-key window in v1: notes 36-84, centered on middle C) using `core/gfx.FillRect`. Display takeover is **off by default**; `toggleTakeover()` (called from chord.go) flips it via `core/pmclient.Client.SetMode`/`PushImage` — on: mode 2 + immediate frame; off: mode 0, handing the screen back to the native Push UI. `runRenderLoop` polls held notes at 10fps: on every change it calls `broadcastNotes()` (web.go) unconditionally, and additionally pushes a frame to Push's screen only while takeover is on. Never touches shared memory directly; it's an HTTP client of push-manager (via `core/pmclient`) for display purposes only. |
| `src/web.go` | SSE broadcaster built on `core/sse.Broker[notesPayload]` (`pruneDropped=false` — a slow/stuck web client just misses an update rather than being dropped, the opposite of automation's broker) for `GET /api/notes/stream` — independent of on-device takeover state, so the web view works whether or not the Shift+Note chord is active. |
| `src/ui/index.html` | Single-file mobile-friendly web view: same 49-key keyboard driven by the SSE feed, plus **chord detection** (3+ held notes) via vendored `Tonal.Chord.detect()`. |
| `src/ui/vendor/tonal.min.js` | Vendored prebuilt browser bundle from [tonaljs/tonal](https://github.com/tonaljs/tonal) (`packages/tonal/browser/tonal.min.js`, MIT, `tonal.LICENSE` alongside) — no CDN dependency, works offline. |

Runs independent of MIDI intercept — never reads pad Note On/Off from Push's raw hardware MIDI (only watches CC49/50 for the toggle chord, see `chord.go`), so the pad grid keeps playing into Live normally throughout regardless of takeover state. One-time manual setup: route a Live track's MIDI Out to "Keyboard Viz In" from Push's own screen (stock Live routing, no script/M4L install). Display takeover itself is toggled live via **Shift + Note** on the hardware — off by default so the native Push UI isn't disturbed until asked for. The web view at `http://push.local:7702` works regardless of takeover state.

## USB-A port safety

**Fix (in `midi.go`):** `waitForBootSettle()` defers all `/dev/snd` access until uptime ≥ 30s. Opening ALSA seq during the cold-boot USB-A enumeration window (~3–15s) wedges the port permanently until power-cycle. HTTP server starts immediately; MIDI/LED/Shadow-UI come online ~30s after cold boot.

**Recovery if wedged:** full power-cycle (hold until off, wait 15s, power on with device attached).

**Testing gotcha:** wedge reproduces only on cold power-on, never on warm `reboot`. Always test with `poweroff` + manual power-on.

## On Push (deployed layout)
```
/data/push-hack/
├── hacks/push-manager/   push-manager binary + hack.json
├── hacks/push-display/   push_hook.so, framebuf shm, midiflt shm
└── logs/                 push-manager.log, push-hook.log
```

## Push 3 — Key Facts

- **OS:** AbletonOS, kernel 5.15.48 real-time, x86_64 Intel
- **Init:** sysvinit runlevel 5 — NOT systemd
- **SSH:** `ableton@push.local` (normal), `root@push.local` (service install). No sudo.
- **Writable:** `/data` (ext4, 201GB). User content: `/data/Music/Ableton/`
- **Read-only:** `/opt` — never write there
- **MIDI routing:** ALSA seq, not libusb. Subscribe to "Ableton Push 3 Live Port" (usually client 16:0, auto-detected by name). `CREATE_PORT` ioctl requires `portInfo[addr.client] = ownClientID` or kernel returns EPERM. MIDI blocking via hook intercepting `snd_seq_event_input` (sets type→NONE when `midiflt->enabled`).
- **USB drives:** auto-mount to `/run/media/<label>-<device>`; `usb-storage` is kernel built-in
- **Button map:** `docs/push3-button-map.md`. All buttons CC ch0, 127=press/0=release. Pad grid Notes 36–99.
- **LED colors:** `docs/push3-led-colors.md`. 128-entry palette; same indices for pads (Note velocity) and buttons (CC value).

## Display-owning hacks

Any hack that wants to draw on Push 3's screen (not just push-manager itself)
**must** go through push-manager's `/api/display/*` HTTP API — never mmap the
shared-memory framebuffer directly. push-manager is the sole shm writer and
push-display (the LD_PRELOAD hook) is the sole reader; that single-writer
discipline is what keeps the shm protocol from racing. Moving that ownership
into push-display itself was considered and rejected — push-display is
injected directly into the `Push3` process, and running an HTTP listener (or
any new surface) inside a component whose failure mode is a frozen screen is
a much bigger blast-radius change than it sounds, versus push-manager's HTTP
server which is a normal, separate, restartable process. See
`hacks/keyboard-visualizer/` (`src/render.go`) for the reference
implementation: `POST /api/display/mode` to enter/exit takeover,
`POST /api/display/image` to push frames, both called as a plain HTTP client.

This means **display-owning hacks have a hard runtime dependency on
push-manager + push-display also being installed and running** — declare it
explicitly:
- State it in the hack's `hack.json` description and README (see
  `hacks/keyboard-visualizer/hack.json`/`README.md`).
- Add a dependency-watcher that polls `GET /api/display/status`
  (`{"connected": bool}`) at startup and periodically, logging a clear,
  state-transition-only warning distinguishing "push-manager unreachable"
  from "push-manager up but push-display's framebuffer not connected" —
  see `hacks/keyboard-visualizer/src/depcheck.go`. Without this, a hack that
  silently no-ops every display call because a dependency isn't installed is
  very hard to diagnose from the logs.

## Adding a New Hack

1. `mkdir -p hacks/<id>/src`
2. Copy + edit `hack.json` — update id, name, port, binary
3. Go source + `Makefile` with `GOOS=linux GOARCH=amd64`
4. `./scripts/install.sh --hack <id>`

Ports: 7705+ (7701=push-manager, 7702=keyboard-visualizer, 7703=automation, 7704=browser-bridge).

## Reference Docs

`docs/` holds Push hardware/OS references (`push3-*`); each hack documents itself in its own folder README.

Push hardware / OS (`docs/`):
- `docs/push3-internals.md` — OS, filesystem, XMOS USB protocol, display, MIDI routing
- `docs/push3-button-map.md` — Push 3 button/encoder MIDI map
- `docs/push3-led-colors.md` — full 128-entry LED color palette
- `docs/push3-assets.md` — Push UI image assets (`/api/assets/<path>`)

Per-hack (in each hack folder):
- `hacks/push-manager/README.md` — full API reference, features, display control, MIDI monitor
- `hacks/automation/README.md` — API, lane types (CC + Selected), BPM/transport sync
- `hacks/browser-bridge/README.md` — how preset loading works (PushHackBrowser remote script)
- `hacks/push-display/README.md` — LD_PRELOAD display/MIDI hook, shared-memory layout, build/deploy
- `hacks/keyboard-visualizer/README.md` — Live-sourced keyboard visualizer, ALSA port routing setup

Local-only research notes live in `discovery/` (gitignored, not shipped)
