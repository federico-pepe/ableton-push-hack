# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> **Doc sync rule:** Keep this file, all `docs/` files, and `README.md` in sync with every code change. If a change affects behaviour, APIs, architecture, or known issues — update the relevant docs in the same commit. Save all implementation plans to `/plans/` with filename format `YYYY-MM-DD-title-of-the-plan.md`. Update [CHANGELOG.md](CHANGELOG.md)'s `## [Unreleased]` section in the same commit as any change worth noting to a future reader — new behavior, a fix, a changed API. Skip internal refactors and trivial edits.

## Project

`push-hack` — extensible hack framework for Ableton Push 3 (Intel Linux, runs full Ableton Live). Deploys via SSH. Never modifies system partition. Only three hacks are core — built from this repo, installed directly via `install.sh`: Push Manager (web file browser + display control, port 7701), Push Display (LD_PRELOAD display hook), Push Hack Catalog (on-device installer for community hacks, port 7702). Everything else is optional and installed via Push Hack Catalog rather than built from this repo — see `catalog/catalog.json` — including Automation (LFO/CC curve sequencer, port 7703), Browser Bridge (Live MIDI Remote Script to load `.adv`/`.adg` presets — **one-time manual activation required**), and Keyboard Visualizer (on-screen piano keyboard sourced from Live's post-transform notes, port 7705).

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
| `core/gfx`, `core/gfx/text` | Stdlib-only image primitives (`FillRect`, `DrawIcon`) in `gfx`; the only `golang.org/x/image` consumer (`DrawText`/`TextWidth`/`Truncate`, basicfont) split into `gfx/text` so automation and keyboard-visualizer's zero-external-dependency binaries stay that way — verified via `go list -deps`. **Everything drawn through `gfx/text` must be ASCII** — see the rule below. |
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
| `src/ui_shadow.go` | On-device Shadow UI (~30fps, Push 3 display — bumped from 10fps 2026-08-18, pending on-device CPU perf test). Five panels: FilePanel, StatsPanel, MidiPanel, BrowserPanel, CatalogPanel (`src/catalog_panel.go`, below), declared in one data-driven table (`panelDefs`: hack-id dependency + top-strip CC + constructor per tab) instead of per-tab switch-cases, so a built-in tab whose dependency isn't installed (BrowserPanel needs `browser-bridge`, now a catalog-optional hack) is skipped generically by `panelAvailable(i)` everywhere a tab is drawn, LED-lit, or dispatched to (`drawPanelTabs`, `shadowRegisterLEDs`, `shadowUIHandleCC`) — checked live via `hacks_nav.go`'s `hackInstalled`, so installing/removing it through the catalog takes effect without a push-manager restart. The first four panels render via `core/gfx/widgets` (`KVRow` for Stats/Midi, `ListView`/`RenderList` for Files/Browser, `SoftButton` for every panel's bottom strip; see that package's row above). `loadSuiIcon`/`iconNameForEntry`/`iconNameForPreset` (icon resolution, reads `/opt/push3/.../Images/Browser/`) and panel-specific input handling (`HandleCC`, cursor/scroll math) stay local — deliberately not extracted, per discovery/shadow-ui-component-framework.md's scope. Activated by MIDI intercept; triggered by Shift+Set chord. While active it fully owns the 4 under-screen soft-buttons' LEDs — the generic trigger/momentary dispatch in `midi.go` is suppressed for CC 20–23 (`isScreenBotCC`), so panels drive them directly. Browser: SEARCH opens the on-screen keyboard (DONE lit green → white on exit); FILTER/REFRESH are momentary (green while held → white on release). MidiPanel has a MONITOR sub-view (Bot3): live event log read from `midiRing`, soft-buttons toggle the display-filter categories (Bot1-4 Sens/SysEx/CC/Note + Bot5 Chan Pressure — same classification as the web UI). Extra soft-buttons beyond the primary 4 use the optional `extraBots` interface (buttons 5-8, CC24-27); `isScreenBotCC` now covers CC20-27. Re-press the MIDI tab to exit the sub-view. (Input port is *not* selectable on-device — subscribing away from the Push port would kill the Shadow UI's own MIDI feed; change it from the web UI only.) |
| `src/catalog_panel.go` | CATALOG tab — thin on-device client of the `push-catalog` hack's HTTP API (`http://127.0.0.1:7702`, hardcoded — same-device localhost only). Never installs anything itself; polls `/api/catalog`+`/api/installed` (self-heals every 10s while the tab is visible), renders a scrollable list via `core/gfx/widgets.RenderList` (an installed hack whose `update_available` came back true gets an `[update: vX]` row and its INSTALL soft-button relabels to `Update` — same POST `/api/install` action, since installing over an existing hack already re-extracts + restarts it), and posts `/api/install`/`/api/remove` for the selected hack on the bottom-strip soft-buttons (async, so a multi-second download never blocks the render/MIDI threads). Degrades gracefully if `push-catalog` isn't installed/running (`EmptyText` message) rather than erroring. |
| `src/hacks_nav.go` | `hackInstalled(id)` (checked live, shared with `ui_shadow.go`'s `panelAvailable`) and `GET /api/hacks/nav`/`GET /api/hacks/installed` — let the web UI mirror the Shadow UI's own install-gating: a header link appears per installed hack that declares `web_ui` in its `hack.json` (e.g. Push Hack Catalog), and a feature button whose dependency isn't installed (the preset Browser tab needs `browser-bridge`) hides itself. Both poll every 10s from `app.js`. |
| `src/live_log.go` | Support-detection marker. Polls `/proc` for the Live process (`findWatchedPIDs`); when a new Live instance appears, waits an 8s grace (Live truncates its `Log.txt` on launch) then appends one native-format line `…: info: push-hack loaded: <id> v<ver>, …` to the newest `/data/.config/Ableton/Live */Log.txt`. Lists all deployed hacks + versions (scans `/data/push-hack/hacks/*/hack.json`). Re-marks on Live restart. Independent of push-display so it works with push-manager alone. |

**Key routes:** `/api/list`, `/api/download`, `/api/upload`, `/api/delete`, `/api/rename`, `/api/copy`, `/api/unmount`, `/api/stats`, `/api/assets/<path>`, `/api/display/{status,mode,image,screenshot}` (`screenshot` = GET, PNG of current framebuf, `X-Display-Mode` header), `/api/midi/{events,stream,filter,ports,subscribe,chords,led,palette,mapping,mapping/config}` (`ports?writable=1` lists output destinations), `/api/presets`, `/api/presets/{refresh,facets,meta}`, `/api/live/load`, `/api/live/tempo`, `/api/live/playing`, `/api/live/play`, `/api/live/stop`, `/api/hacks/nav`, `/api/hacks/installed`.

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

Browser Bridge (the `PushHackBrowser` MIDI Remote Script push-manager's `live_bridge.go` talks to over TCP port 7704) moved out of this repo — see
[federico-pepe/push-hack-browser-bridge](https://github.com/federico-pepe/push-hack-browser-bridge), installable via Push Hack Catalog like Automation and Keyboard Visualizer.

### Push Hack Catalog (`hacks/push-catalog/`)
Go binary, no runtime deps. Port 7702. On-device installer for community hacks — browse and install from a phone, no SSH/build toolchain needed. Does almost nothing itself: serves one page and shells out to an embedded `push-catalog.sh` for every action, so the install logic has exactly one home (`go:embed`).

**Model:** Push Hack Catalog hosts no binaries. `catalog/catalog.json` (this repo) is an index of pointers — each entry names a hack's own `github_repo`. That repo publishes its own GitHub Releases and keeps a `release.json` at its root; the daemon fetches that live on every install (and on every `/api/catalog` listing, to source each entry's live `version`/`released_at`), downloads the release tarball it points at, and extracts it (the tarball's own `hack.json` + binary) straight into `/data/push-hack/hacks/<id>/`. No sha256 pin, no signing — the trust boundary is "this repo is on GitHub, its catalog entry was PR-reviewed once." See `catalog/ARCHITECTURE.md` for the full model and `catalog/PUBLISHING.md` for how a hack author publishes into it.

| File | Role |
|------|------|
| `push-catalog.sh` (embedded into the binary via `make embed`) | All the logic: `q()`/`rq()` (python3-only JSON readers — no jq dependency, mirrors the framework installer's own avoidance of it; `q`'s `catalog` op also fetches each hack's live `release.json` via urllib to enrich the listing with `version`/`released_at`, degrading to `null` per-entry rather than failing the whole listing; given an optional `hacks_dir` arg it also reads that hack's locally installed `hack.json` for `installed_version` and sets `update_available` when it differs from the live `version` — `cmd_catalog` always passes `$PUSH_HACK_DIR/hacks`), `fetch_release()` (pulls a hack's `release.json` off `raw.githubusercontent.com`, or a `release_url` override for local/dev entries), `cmd_install` (fetch release → download tarball → `tar -xzf` into `hacks/` → read the extracted `hack.json` → `install_service` — this is also how an update is applied, same command, no separate code path), `cmd_remove`, `--self-test` (offline: catalog parsing, the catalog-enrichment op incl. `installed_version`/`update_available`, and a checked-in `testdata/fixture-hack.tar.gz` exercising the fetch/extract path — all via `file://` overrides, no real network or `/etc/init.d` touched). |
| `src/main.go` | HTTP server: `GET /`, `GET /api/catalog` (proxies `push-catalog.sh catalog`), `GET /api/installed`, `POST /api/install?id=`, `POST /api/remove?id=` — hack id validated against `^[a-z0-9][a-z0-9-]{0,63}$` before it ever reaches the shell. |
| `src/index.html` | Single-file mobile web UI: catalog cards (name, description, author, live version, last-updated date, `requires` tags), Install/Update/Remove (Update shown, with an "update available" tag, when `update_available` is true), output log pane. |

**Key routes:** `GET /`, `GET /api/catalog`, `GET /api/installed`, `POST /api/install?id=<id>`, `POST /api/remove?id=<id>`.

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
- **LED colors:** `docs/push3-led-colors.md`. 128-entry palette; same indices for pads (Note velocity) and buttons (CC value). `core/push3/colors.go`'s `NamedColors` was **wrong for every entry until 2026-08-18** — it claimed a Push-2-derived even/odd split that isn't true for Push 3 (every one of the 128 raw velocities is a real, distinct colour). Fixed by rebuilding it from this doc's SysEx-queried table; see that file's own header comment for the full story. Trust this doc over any claim `colors.go` makes about its own source.

## Drawing text — ASCII only

`core/gfx/text` renders with `basicfont.Face7x13`, which has **no glyph beyond
ASCII**: an em-dash, an ellipsis, an accent or a smart quote all draw as a
missing-glyph box on the panel. Comments and docs are free to use whatever;
**strings that reach the screen are not**.

This is easy to get wrong because it is invisible everywhere except the hardware
— nothing errors, nothing logs, the frame rate stays healthy.

- `text.Truncate` is the enforcement point and marks a cut with `"..."`. It
  appended `U+2026` until **2026-08-17**, which meant every truncated filename
  and breadcrumb in push-manager's browser (`ui_shadow.go:926/939/1619`, cut at
  100–110 runes) drew a box on the panel. `core/gfx/text/text_test.go` now
  asserts every output byte is printable ASCII.
- The same fix removed a latent panic: `maxRunes <= 0` evaluated
  `runes[:maxRunes-1]`.
- Truncated strings are now 2 characters shorter than before, since the marker is
  3 runes rather than 1. Accepted deliberately.
- `text.Width` counts **bytes** while `Truncate` counts **runes**. They agree on
  ASCII, which is the only thing that renders — but a multibyte string measures
  wider than it draws. Known inconsistency, not yet resolved.

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
server which is a normal, separate, restartable process. The interface is
plain HTTP, called like any other client: `POST /api/display/mode` to
enter/exit takeover, `POST /api/display/image` to push frames.

This means **display-owning hacks have a hard runtime dependency on
push-manager + push-display also being installed and running** — declare it
explicitly:
- State it in the hack's `hack.json` description and README.
- Add a dependency-watcher that polls `GET /api/display/status`
  (`{"connected": bool}`) at startup and periodically, logging a clear,
  state-transition-only warning distinguishing "push-manager unreachable"
  from "push-manager up but push-display's framebuffer not connected".
  Without this, a hack that silently no-ops every display call because a
  dependency isn't installed is very hard to diagnose from the logs.

## Adding a New Hack

1. `mkdir -p hacks/<id>/src`
2. Copy + edit `hack.json` — update id, name, port, binary
3. Go source + `Makefile` with `GOOS=linux GOARCH=amd64`
4. `./scripts/install.sh --hack <id>`

Ports: 7706+ (7701=push-manager, 7702=push-catalog, 7703=automation, 7704=browser-bridge, 7705=keyboard-visualizer).

## Releases

This project uses Semantic Versioning for the repo as a whole (not
`hack.json`'s per-hack `version` field, which is separate and unrelated).
It is pre-1.0, so expect breaking changes between minor versions:
`vMAJOR.MINOR.PATCH[-alpha|-beta|-rc.N]`. Current stage: `-alpha`.

Update [CHANGELOG.md](CHANGELOG.md) in the same commit as the tagged code,
retitling `## [Unreleased]` to the new version and dating it. Cutting a
release:

```bash
git tag v0.1.1-alpha
git push origin v0.1.1-alpha
```

There is no tag-triggered CI release job in this repo (unlike
`push-tethered-app`) — tagging here is changelog bookkeeping, not a
publish step.

## Reference Docs

`docs/` holds Push hardware/OS references (`push3-*`); each hack documents itself in its own folder README.

Push hardware / OS (`docs/`):
- `docs/push3-internals.md` — OS, filesystem, XMOS USB protocol, display, MIDI routing
- `docs/push3-button-map.md` — Push 3 button/encoder MIDI map
- `docs/push3-led-colors.md` — full 128-entry LED color palette
- `docs/push3-assets.md` — Push UI image assets (`/api/assets/<path>`)

Per-hack (in each hack folder):
- `hacks/push-manager/README.md` — full API reference, features, display control, MIDI monitor
- `hacks/push-display/README.md` — LD_PRELOAD display/MIDI hook, shared-memory layout, build/deploy
- `hacks/push-catalog/README.md` — on-device installer API, how the catalog install flow works
- `catalog/ARCHITECTURE.md`, `catalog/schema.md`, `catalog/PUBLISHING.md` — the store's catalog model and how to publish a hack into it

Local-only research notes live in `discovery/` (gitignored, not shipped)
