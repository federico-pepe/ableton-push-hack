# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> **Doc sync rule:** Keep this file, all `docs/` files, and `README.md` in sync with every code change. If a change affects behaviour, APIs, architecture, or known issues — update the relevant docs in the same commit.

## Project

`push-hack` — extensible hack framework for Ableton Push 3 (Intel Linux, runs full Ableton Live). Deploys via SSH. Never modifies system partition. Hacks: Push Manager (web file browser + display control), Push Display (LD_PRELOAD display hook), Browser Bridge (Live MIDI Remote Script to load `.adv`/`.adg` presets — **one-time manual activation required**), Automation (LFO/CC curve sequencer, port 7703), Keyboard Visualizer (on-screen piano keyboard sourced from Live's post-transform notes, port 7702).

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

### Push Manager (`hacks/push-manager/`)
Go binary, no runtime deps. ~8–15MB RSS. Port 7701. See `hacks/push-manager/README.md` for full API.

| File | Role |
|------|------|
| `src/main.go` | HTTP server, routes, middleware |
| `src/files.go` | Filesystem ops with path traversal guard |
| `src/stats.go` | CPU/memory/disk/uptime/IP stats; top processes (Ableton Index, Live, Push3, push-manager) |
| `src/presets.go` | Preset index: scans `.adv`/`.adg` under Core Library, Factory Packs, User Library. In-memory cache + `presets.json`. `QueryPresets(PresetFilter)`, `presetFacets()`. Metadata (favourites, tags) in `preset_meta.json`. |
| `src/live_bridge.go` | One-shot TCP to `127.0.0.1:7704` (Browser Bridge). `liveLoad(name, category)` → `load:<root>:<name>`. Also: `livePlay()`, `liveStop()`, `liveIsPlaying()`, `liveTempo()`, `liveBeat()`. |
| `src/display.go` | Shared-memory bridge to push_hook.so. Mmaps `framebuf`. Three modes: 0=passthrough, 1=bar, 2=takeover. OSD subsystem: single-line and multi-line renderers. Startup splash on fresh hook attach. Screenshot: `shmReadFrame`+`bgr565ToImage` read the framebuf back and `png.Encode` it — captures only push-manager-owned frames (Shadow UI/OSD/image/testpattern), not the native Ableton UI (never copied into shm in passthrough). |
| `src/midi.go` | ALSA seq subscriber + LED output — pure Go ioctls (no cgo). **Boot-settle:** defers `/dev/snd` access until uptime ≥ 30s (USB-A safety). **Auto-detect:** `detectPush3Port()` scans `/proc/asound/seq/clients` by name on each connection attempt — handles shifted client numbers (e.g. 20 instead of 16) when USB MIDI devices are connected at boot; disabled once user manually subscribes. LED config system (trigger/momentary/exclusive modes, animations). Chords: Shift+Settings=intercept toggle, Shift+Set=open browser. |
| `src/remap.go` | MIDI remapping. `MidiMapping` (src→out CC/Note), `applyRemap()` called from `processFixedEvent` — transforms a Push control's value and sends to a user-selected writable ALSA port via `sendSeqCCTo`/`sendSeqNoteTo` (reuses `midiOutFd`, no new port). Absolute sources scale velocity into `[min,max]`; relative encoders (CC 71-79/14) accumulate deltas (`decodeRel`, `remapAccum`) clamped to range. Gated by `remapEnabled` + optional `remapRequireIntercept`. Persisted in `midi.json` via `midiPersistData`. |
| `src/ui/index.html` | Single-file SPA — file browser, display control, MIDI monitor, LED panel, MIDI mapping panel (learn/manual + writable-port dropdown), preset browser tab. |
| `src/ui_shadow.go` | On-device Shadow UI (10fps, Push 3 display). Four panels: FilePanel, StatsPanel, MidiPanel, BrowserPanel. Activated by MIDI intercept; triggered by Shift+Set chord. While active it fully owns the 4 under-screen soft-buttons' LEDs — the generic trigger/momentary dispatch in `midi.go` is suppressed for CC 20–23 (`isScreenBotCC`), so panels drive them directly. Browser: SEARCH opens the on-screen keyboard (DONE lit green → white on exit); FILTER/REFRESH are momentary (green while held → white on release). MidiPanel has a MONITOR sub-view (Bot3): live event log read from `midiRing`, soft-buttons toggle the display-filter categories (Bot1-4 Sens/SysEx/CC/Note + Bot5 Chan Pressure — same classification as the web UI). Extra soft-buttons beyond the primary 4 use the optional `extraBots` interface (buttons 5-8, CC24-27); `isScreenBotCC` now covers CC20-27. Re-press the MIDI tab to exit the sub-view. (Input port is *not* selectable on-device — subscribing away from the Push port would kill the Shadow UI's own MIDI feed; change it from the web UI only.) |
| `src/live_log.go` | Support-detection marker. Polls `/proc` for the Live process (`findWatchedPIDs`); when a new Live instance appears, waits an 8s grace (Live truncates its `Log.txt` on launch) then appends one native-format line `…: info: push-hack loaded: <id> v<ver>, …` to the newest `/data/.config/Ableton/Live */Log.txt`. Lists all deployed hacks + versions (scans `/data/push-hack/hacks/*/hack.json`). Re-marks on Live restart. Independent of push-display so it works with push-manager alone. |

**Key routes:** `/api/list`, `/api/download`, `/api/upload`, `/api/delete`, `/api/rename`, `/api/copy`, `/api/unmount`, `/api/stats`, `/api/assets/<path>`, `/api/display/{status,mode,image,testpattern,screenshot}` (`screenshot` = GET, PNG of current framebuf, `X-Display-Mode` header), `/api/midi/{events,stream,filter,ports,subscribe,chords,led,palette,mapping,mapping/config}` (`ports?writable=1` lists output destinations), `/api/presets`, `/api/presets/{refresh,facets,meta}`, `/api/live/load`, `/api/live/tempo`, `/api/live/playing`, `/api/live/play`, `/api/live/stop`.

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
| `src/engine.go` | `AutoLane`, `AutoState`, `CurvePoint`. 50Hz playback goroutine. Linear + Catmull-Rom interpolation. `TransportSync bool`. SSE broadcast (20Hz). Persistence to `automation.json`. |
| `src/midi.go` | ALSA seq output + input. Two ports: **Push Hack Automation** (output → Live 128:2), **Push Hack Clock** (input ← Push3 16:0). Reads MIDI clock (24 PPQN) for BPM: ring buffer of 24 tick timestamps, `BPM = 60.0 / elapsed`. `onPlayButtonPress()` handles CC85 val=127 — sole transport toggle when synced. `detectLivePort()` scans for `"Ableton Live"` writable port. Boot-settle (30s) same as push-manager. |
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

| File | Role |
|------|------|
| `src/main.go` | Minimal HTTP server (`GET /api/status` only). `-push-manager` flag sets push-manager base URL (default `http://localhost:7701`). Boot-settle then starts the ALSA read loop and render loop as goroutines. |
| `src/midi.go` | Creates a single writable ALSA seq port, **Keyboard Viz In** (`capWrite\|capSubsWrite`, same pattern as push-manager's own "Push Manager In", `hacks/push-manager/src/midi.go:838-848`). Live initiates the connection when the user routes a track's MIDI Out to this port from Push's own screen — no subscribe needed on our side for that. Separately, `maintainPush3Subscription()` self-subscribes this same port to Push 3's hardware port ("Ableton Push 3 Live Port", auto-detected by name, re-checked every 30s in case the client number shifts) so it also receives CC49/50 for the takeover chord — one ALSA seq port can receive from multiple senders, so this avoids a second visible ALSA client. Because Push 3's raw pad stream now shares the port with Live's routed notes, `processSeqBuf` reads each event's ALSA source client (offset 12 in the fixed 28-byte event) and filters Note On/Off by `isPush3Client()` — only non-Push3 sources (i.e. Live) update the held-notes `[128]bool`, so a pad press doesn't double up with Live's post-transform note. CC49/50 are forwarded to `onChordCC` (chord.go) regardless of source. Boot-settle (30s) same as push-manager/automation. |
| `src/chord.go` | Shift+Note (CC49+CC50) chord state machine — 500ms-debounced (same pattern as push-manager's `chordCCPressed`/`chordCCReleased`), calls `toggleTakeover()` when both are held together. Also holds `detectPush3Port()`, used by `midi.go`'s subscription maintenance. |
| `src/render.go` | Draws the keyboard into a plain `image.NRGBA` (960×160, fixed 49-key window in v1: notes 36-84, centered on middle C). Display takeover is **off by default**; `toggleTakeover()` (called from chord.go) flips it — on: `POST /api/display/mode {"mode":2}` + immediate frame; off: `{"mode":0}`, handing the screen back to the native Push UI. While on, the 10fps render loop pushes a new frame via `POST /api/display/image` only when held notes changed. Never touches shared memory directly; it's an HTTP client of push-manager for display purposes only. |

Runs independent of MIDI intercept — never reads pad Note On/Off from Push's raw hardware MIDI (only watches CC49/50 for the toggle chord, see `chord.go`), so the pad grid keeps playing into Live normally throughout regardless of takeover state. One-time manual setup: route a Live track's MIDI Out to "Keyboard Viz In" from Push's own screen (stock Live routing, no script/M4L install). Display takeover itself is toggled live via **Shift + Note** on the hardware — off by default so the native Push UI isn't disturbed until asked for.

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
