# Architecture

This document describes the internal structure of `push-hack`: the deploy
framework, the hack layout convention, the shared `core/` library, and each
core hack's file layout. For safety rules, build/deploy commands, and quick
facts, see [CLAUDE.md](../CLAUDE.md). For HTTP routes and wire protocols, see
[api-reference.md](api-reference.md).

## Framework layer (`scripts/`, `lib/`)

The deploy system works over SSH. `lib/common.sh` holds the shared SSH
helpers: `push_exec` and `push_exec_root` use `-n` to stop stdin consumption
in loops. The library also detects Push paths and installs or removes
services.

Push uses sysvinit, not systemd. Stop a service before you copy its binary
over SCP — a running binary is locked on Linux. The framework copies regular
binaries as user `ableton`. It copies `.so` files as `root`, through
`push_copy_root`.

`check_connection()` clears a stale SSH host key on its own
(`clear_host_key()` runs `ssh-keygen -R`) when a Push OS update regenerated
the device key. It detects the string `REMOTE HOST IDENTIFICATION HAS
CHANGED` and retries the connection.

## Hack structure (`hacks/<hack-id>/`)

Each hack folder holds:

- `hack.json` — metadata: id, name, version, port, allowed_roots, binary,
  enabled.
- `service.initd` — an optional custom init.d template. Placeholders:
  `{{SVC_NAME}}`, `{{HACK_DIR}}`, `{{LOG_DIR}}`, `{{PORT}}`.
- `remote-script/` — an optional payload. `install.sh` copies it to
  `<remote_hack_dir>/remote-script`.

`install.sh` deploys the binary to `/data/push-hack/hacks/<id>/` and
registers the service at `/etc/init.d/push-hack-<id>`.

## Core shared library (`core/`)

`core/` is a nested Go module
(`github.com/federico-pepe/ableton-push-hack/core`, its own `go.mod`).
push-manager, automation, and keyboard-visualizer each pull it in with
`require` + `replace ../../../core` in their own `go.mod`. This keeps each
hack independently buildable. A third-party hack in its own repo could
`require` the same path without a `replace` and resolve it straight from
GitHub.

See `discovery/push-core-refactor.md` for the extraction plan and the
reasoning behind it.

| Package | Contents |
|---|---|
| `core/push3` | Zero-import Push 3 facts: the full button and encoder MIDI map (`buttons.go`), a 128-entry named LED palette plus `ColorByName` (`colors.go`), display geometry `VisW`/`VisH`/`Stride`/`FrameBytes`/`TotalBytes` (`geometry.go`), and encoder helpers `IsEncoderCC`/`DecodeRel`/`ScaleVal`/`ClampInt` (`encoder.go`, tested in `encoder_test.go`). push-manager's `push3_buttons.go` re-exports the button and encoder constants as package-`main` aliases (`const CCShift = push3.CCShift`, and so on), so its roughly 180 existing call sites across `midi.go` and `ui_shadow.go` did not need to change. `core/push3` stays the single source of truth. |
| `core/gfx`, `core/gfx/text` | `gfx` holds stdlib-only image primitives (`FillRect`, `DrawIcon`). `gfx/text` holds the only consumer of `golang.org/x/image` (`DrawText`, `TextWidth`, `Truncate`, `basicfont`), split out so automation's and keyboard-visualizer's zero-external-dependency binaries stay that way — checked with `go list -deps`. Everything drawn through `gfx/text` must be ASCII; see [CLAUDE.md's "Drawing text" rule](../CLAUDE.md#drawing-text--ascii-only). |
| `core/gfx/widgets` | Shared Shadow-UI drawing components built on `gfx` and `gfx/text`: `Theme` (a named color palette), `SoftButton`/`DrawBotStrip` (button state driven by semantics, not by matching label text), `ListRow`/`ListView`/`RenderList` (a scrollable list with breadcrumb and scrollbar — a generalized form of push-manager's `FilePanel`/`BrowserPanel`), `KVRow`/`DrawKVRows` (label:value rows — a generalized form of `StatsPanel`/`MidiPanel`), and ahead-of-need primitives (`DrawBorder`/`DrawMeter`/`DrawArc`, `Knob`). It works only on plain `image.NRGBA` — no shared memory, no hack-specific state — so any hack that draws on Push's screen can use it. See `discovery/shadow-ui-component-framework.md`. All 4 push-manager panels use it now; the `Panel` interface's old `BotStrip()` method is gone, replaced by `SoftBotStrip()`. |
| `core/display` | `codec.go` holds `ToBGR565`/`FromBGR565` pixel codecs (tested in `codec_test.go` for the duplicate-frame invariant, stride padding, and round-trip). `shm.go` holds the `Shm` struct, which wraps the push_hook.so shared-memory mmap (`Ensure`/`Connected`/`Mode`/`SetMode`/`ReadFrame`/`WritePixels`/`CompareAndSetMode`/`FrameSeq`). It opens with `os.O_RDWR` and no `O_CREATE` — push_hook.c is the sole creator, push-manager is the sole writer (see "Display-owning hacks" in CLAUDE.md). push-manager is the only consumer; other hacks reach the display through `core/pmclient` instead. |
| `core/httpx` | `WithLogging`, `WithCORS(allowMethods, next)` (allowMethods is the one setting that differs per hack, pinned by `middleware_test.go`), `JSON`, `Error`, `NewServer` (a 30s read / 5min write / 120s idle timeout set), and `ServeEmbedded` (the single-file `handleUI` shared by automation and keyboard-visualizer; push-manager's three-file UI keeps its own handler). |
| `core/hackcfg` | `Config` and `Load(path, defaultPort)` — the minimal id/name/version/port shape that automation and keyboard-visualizer both used. push-manager's config is a strict superset (it adds allowed_roots, settings, `~` expansion) and stays separate. |
| `core/sse` | A generic `Broker[T]` (`Register`/`Unregister`/`Broadcast`) plus a `Serve[T]` SSE HTTP helper. `NewBroker`'s `pruneDropped` parameter keeps a real behavioral difference: automation drops a client whose channel is full (`true`); keyboard-visualizer does not (`false`). Pinned by `broker_test.go`. |
| `core/pmclient` | An HTTP client for push-manager's display and tempo API — `SetMode`, `PushImage`, `DisplayStatus`, `Tempo`. It turns the "display-owning hacks go through push-manager's HTTP API" rule into one the compiler enforces. Used by keyboard-visualizer (display takeover, dependency watcher) and automation (BPM poll fallback). |
| `core/alsaseq` | The ALSA sequencer layer: `/dev/snd/seq` ioctls, no cgo. `const.go` holds the kernel ABI (ioctl numbers, struct offsets, event types), moved verbatim and diffed against all three hacks' prior copies. `bootsettle.go` holds `WaitForBootSettle`, which defers `/dev/snd` access past the USB-A cold-boot enumeration window. `client.go` holds `Client`/`Open`/`CreatePort`/`Subscribe`/`Addr`/`FD`/`Close`. `event.go` holds `WriteEvent`/`SendCC`/`SendNote`/`SendSysEx`. `ports.go` holds `Port`/`ParseClients`/`EnumPorts`/`FindByName`, tested against `testdata/seq_clients_*.txt` fixtures (a bare case and a shifted-client-number case; hand-constructed, not yet captured off real hardware). `reader.go` holds the `Handler` interface plus `Walk`/`ReadLoop` — the shared event decoder that fixed automation's SysEx desync bug (its own walker had no variable-length branch; `reader_test.go`'s `TestWalkFixedVarlenFixed` is the regression test). |

## Push Manager (`hacks/push-manager/`)

A Go binary with no runtime dependencies, about 8-15MB RSS, on port 7701. See
`hacks/push-manager/README.md` for the full API and
[api-reference.md](api-reference.md) for its routes.

| File | Role |
|------|------|
| `src/main.go` | HTTP server, routes, middleware. |
| `src/files.go` | Filesystem operations, with a path traversal guard. |
| `src/stats.go` | CPU, memory, disk, uptime, and IP stats, plus top processes. `watchedProcs()` combines a fixed set (Ableton Index, Live, Push3) with every currently installed hack that has a binary, read live off each `hack.json`. An installed or removed hack appears or disappears from the CPU breakdown with no push-manager restart. |
| `src/presets.go` | The preset index. It scans `.adv`/`.adg` files under Core Library, Factory Packs, and User Library, with an in-memory cache plus `presets.json`. `QueryPresets(PresetFilter)` and `presetFacets()` read it. Metadata (favourites, tags) lives in `preset_meta.json`. |
| `src/live_bridge.go` | A one-shot TCP client to `127.0.0.1:7704` (Browser Bridge). `liveLoad(name, category)` sends `load:<root>:<name>`. Also: `livePlay()`, `liveStop()`, `liveIsPlaying()`, `liveTempo()`, `liveBeat()`. |
| `src/display.go` | The shared-memory bridge to push_hook.so — a thin wrapper over `core/display.Shm` (`var shm = &coredisplay.Shm{}`). `shmGetMode`/`shmSetMode`/`shmReadFrame`/`shmWritePixels` delegate straight through, so `ui_shadow.go`'s call sites did not need to change. Three modes: 0 = passthrough, 1 = bar, 2 = takeover. The OSD subsystem (single-line and multi-line renderers) has no second consumer and stayed local. A startup splash shows on each fresh hook attach, through `Shm.OnConnect`. A screenshot reads the framebuffer back with `shmReadFrame` plus `bgr565ToImage` (which calls `core/display.FromBGR565`) and encodes it with `png.Encode`. It captures only push-manager-owned frames (Shadow UI, OSD, or a pushed image), not the native Ableton UI — passthrough mode never copies that UI into shared memory. |
| `src/midi.go` | The ALSA seq subscriber and LED output, built on `core/alsaseq`. Boot-settle: `alsaseq.WaitForBootSettle()` defers `/dev/snd` access until uptime is at least 30s (see USB-A port safety in CLAUDE.md). Auto-detect: `detectPush3Port()` calls `alsaseq.FindByName()` on each connection attempt, which handles a shifted client number (for example 20 instead of 16) when other USB MIDI devices are connected at boot; auto-detect turns off once the user subscribes manually. Also holds the LED config system (trigger, momentary, and exclusive modes, plus animations) and chord handling (Shift+Settings toggles intercept, Shift+Set opens the browser). |
| `src/remap.go` | MIDI remapping. `MidiMapping` maps a source CC/Note to an output CC/Note. `applyRemap()`, called from `processFixedEvent`, transforms a Push control's value and sends it to a user-selected writable ALSA port through `sendSeqCCTo`/`sendSeqNoteTo` (this reuses `midiOut`, the shared `*alsaseq.Client` — no new port opens). An absolute source scales velocity into `[min,max]`. A relative encoder (CC 71-79 or CC 14) accumulates deltas through `push3.DecodeRel` and `remapAccum`, clamped to its range. The feature is gated by `remapEnabled` plus the optional `remapRequireIntercept`, and persists to `midi.json` through `midiPersistData`. |
| `src/ui/index.html`, `app.css`, `app.js` | A three-file SPA (all three files are embedded separately at `main.go:19`, not bundled into one). It covers the file browser, display control, MIDI monitor, LED panel, a MIDI mapping panel (learn/manual entry plus a writable-port dropdown), and the preset browser tab. The header's `#hack-links` span (`display:contents`, so each `<a>` stays a flex item of `.header-inner`) fills from `/api/ui/tabs` — one link per installed hack that declares `web_ui` with its Web switch on, Catalog included, so no port is hardcoded. The Display page has a third sub-tab, **Tabs** (`#dtab-tabs`), which edits that same list: up/down reorder (not drag-and-drop, since this page also runs on a phone) plus Shadow/Web checkboxes per entry, with anything past the 8-tab hardware limit greyed out. `uiTabsDirty` stops the 10s poll from overwriting an unsaved edit. |
| `src/ui_shadow.go` | The on-device Shadow UI, at about 30fps on the Push 3 display (raised from 10fps on 2026-08-18; an on-device CPU check is still pending). It has five built-in panels — FilePanel, StatsPanel, MidiPanel, BrowserPanel, CatalogPanel (`src/catalog_panel.go`, below) — declared in one data-driven table, `panelDefs` (stable id, label, hack-id dependency, constructor). No tab's screen position is hardcoded: `ui_tabs.go` resolves the user's order and switches into `shadowUI.tabs`, a tab's CC is `CCScreenTopN` of its index, and `shadowRegisterLEDs`/`shadowUIHandleCC`/`drawPanelTabs` all work off that resolved, already-filtered list. `buildTabs()` reuses a surviving tab's existing Panel across a reorder, so a browser cursor or a fetched catalog is not thrown away; `shadowUIReload()` re-resolves the list live when the web UI saves. An external hack's tab enters the same list as a `RemotePanel`. `shadowUISwitchToBrowse` looks the Browse tab up by id, since the user can reorder or switch it off. The first four panels render through `core/gfx/widgets` (`KVRow` for Stats/Midi, `ListView`/`RenderList` for Files/Browser, `SoftButton` for every panel's bottom strip). Icon resolution (`loadSuiIcon`, `iconNameForEntry`, `iconNameForPreset`, reading `/opt/push3/.../Images/Browser/`) and panel-specific input handling (`HandleCC`, cursor and scroll math) stay local by design; see `discovery/shadow-ui-component-framework.md` for that scope decision. MIDI intercept activates the Shadow UI; the Shift+Set chord toggles it. While active, it fully owns the LEDs of the 4 under-screen soft-buttons — the generic trigger/momentary dispatch in `midi.go` is suppressed for CC 20-23 (`isScreenBotCC`), so panels drive them directly. In the Browser panel, SEARCH opens the on-screen keyboard (DONE lights green, then white on exit); FILTER and REFRESH are momentary (green while held, white on release). MidiPanel has a MONITOR sub-view (Bot3): a live event log read from `midiRing`, with soft-buttons toggling the display-filter categories (Bot1-4 for Sens/SysEx/CC/Note, Bot5 for Chan Pressure — the same classification the web UI uses). Extra soft-buttons past the primary 4 use the optional `extraBots` interface (buttons 5-8, CC24-27); `isScreenBotCC` now covers CC20-27. Re-press the MIDI tab to exit the sub-view. The input port is not selectable on-device, since subscribing away from the Push port would kill the Shadow UI's own MIDI feed; change it from the web UI only. |
| `src/catalog_panel.go` | The CATALOG tab — a thin on-device client of the `push-catalog` hack's HTTP API at `http://127.0.0.1:7702` (hardcoded, same-device only). It never installs anything itself. It polls `/api/catalog` and `/api/installed` (self-healing every 10s while the tab is visible; `/api/installed` returns `[{id, enabled}]`, matching the web UI's own decode), renders a scrollable list through `core/gfx/widgets.RenderList` (the breadcrumb doubles as the selected hack's provenance line — `catalogOrigin` prints `owner/repo` plus `by <author>` when the author is not just the repo owner), tags an installed hack whose `update_available` is true with an `[update: vX]` row (its INSTALL soft-button then relabels to `Update`, since installing over an existing hack already re-extracts and restarts it), and tags an installed-but-disabled hack with `[disabled]`. It posts `/api/install`, `/api/remove`, `/api/enable`, or `/api/disable` for the selected hack from the bottom-strip soft-buttons (Bot1 Install/Update, Bot2 Remove, Bot3 Enable/Disable — all async, so a multi-second download never blocks the render or MIDI threads). It degrades gracefully, with an `EmptyText` message, if `push-catalog` is not installed or not running. |
| `src/ui_tabs.go` | The one ordered list behind both navigations, with two switches per entry (Shadow and Web). `candidateEntries()` merges the built-in `panelDefs` with every installed, enabled hack that declares `web_ui` and/or `shadow_ui` (read live off disk; `hackNav.Enabled` comes from `hacks_nav.go`'s `hackEnabled` — a hack disabled through the catalog is skipped entirely, rather than left as a dead link or tab). `resolveUIEntries()` applies the user's saved order and switches from `<hackdir>/ui_tabs.json`, drops stale ids, and appends any never-seen id that is switched on. `activeShadowTabs()` is the hardware subset — switched on, dependency installed, capped at `maxShadowTabs` (8, since Push has 8 top buttons). Routes: `GET`/`POST /api/ui/tabs`. A `POST` calls `shadowUIReload()`, so a save applies to a running Shadow UI with no restart. The built-in `catalog` entry carries its own menu-bar link (`catalogPort` = 7702, a core-hack fact), so the Catalog link survives an installed catalog that predates `web_ui` or uses a different id. `portConflicts()`/`annotatePortConflicts()` flag every port claimed by more than one installed, enabled hack (a disabled hack binds nothing, so it cannot flag a healthy port-mate as a false positive) — surfaced per-entry (`port_conflict`) and as a whole map (`port_conflicts`), shown as a banner plus a row note in the web UI, and logged once at startup by `warnPortConflicts()`. Built-ins are excluded, since they point at a port rather than bind one, and nothing is probed over the network. `app.js` renders one header link per port, and the first entry in the user's order wins. Pinned by `ui_tabs_test.go`. |
| `src/remote_panel.go` | `RemotePanel` — a Shadow UI tab owned by another hack. push-manager acts as a dumb terminal: it GETs `http://127.0.0.1:<port><path>` every 300ms for a `remoteView` (`title`/`status`/`rows`/`cursor`/`buttons`/`hint`), renders it with `core/gfx/widgets.RenderList`, and POSTs every press back as `{cc, value}` — the hack owns all state. This is deliberately not a pixel protocol: encoding and decoding a PNG 30 times a second would cost CPU on a device whose whole point is not stealing CPU from Live. The contract is documented in push-manager's README. |
| `src/hacks_nav.go` | `hackInstalled(id)` (checked live, shared with `ui_shadow.go`'s `panelAvailable`) and `GET /api/hacks/installed` let the web UI mirror the Shadow UI's own install-gating — a feature button whose dependency is not installed (the preset Browser tab needs `browser-bridge`) hides itself, polled live every 10s from `app.js`. It also decodes both nav hooks off each installed `hack.json` — `web_ui` and `shadow_ui`, sharing one `hackUI` shape (`{label, path}`, with the port read from the hack's own `port`) — which `ui_tabs.go` turns into menu-bar links and Shadow UI tabs. `hackEnabled(id)` mirrors push-catalog.sh's own `cmd_installed` semantics: "enabled" means a boot-autostart `rc<N>.d/S<NN><svc>` symlink exists, not that the hack is currently running (a hack with no init.d service, such as a Remote Script, is always enabled). It is computed fresh into each `hackNav.Enabled`, not read off `hack.json`; `ui_tabs.go` uses it to drop a disabled hack's nav entry entirely instead of leaving a dead link. `hacksDir`/`initdDir`/`rcdGlob` are variables only so `hacks_nav_test.go` can point them at a temp directory. |
| `src/live_log.go` | A support-detection marker. It polls `/proc` for the Live process (`findWatchedPIDs`); when a new Live instance appears, it waits an 8s grace period (Live truncates its `Log.txt` on launch), then appends one native-format line — `...: info: push-hack loaded: <id> v<ver>, ...` — to the newest `/data/.config/Ableton/Live */Log.txt`, listing every deployed hack and version (scanned from `/data/push-hack/hacks/*/hack.json`). It re-marks on each Live restart, and works with push-manager alone, independent of push-display. |

**File ownership:** push-manager runs as root and chowns every file it
creates to match its parent directory's owner. This keeps ownership at
`ableton:users`.

**USB drives:** these auto-mount to `/run/media/<label>-<device>`. After a
`syscall.Unmount`, push-manager deletes `/tmp/.automount-<name>` so the drive
can re-mount after a replug.

## Push Display (`hacks/push-display/`)

An LD_PRELOAD hook — a C shared library injected into the Push3 process only
(it checks that `/proc/self/comm` equals `Push3`). It intercepts
`libusb_bulk_transfer` for display overlay and takeover, and
`snd_seq_event_input` for MIDI neutralization. It has an 8s boot grace window
before it activates. `make splash` regenerates `src/splash_data.h`.

For the shared-memory layout and display geometry, see
[api-reference.md](api-reference.md).

Browser Bridge (the `PushHackBrowser` MIDI Remote Script that push-manager's
`live_bridge.go` talks to over TCP port 7704) lives outside this repo — see
[federico-pepe/push-hack-browser-bridge](https://github.com/federico-pepe/push-hack-browser-bridge).
It installs through Push Hack Catalog, like Automation and Keyboard
Visualizer.

## Push Hack Catalog (`hacks/push-catalog/`)

A Go binary with no runtime dependencies, on port 7702. It is an on-device
installer for community hacks: browse and install from a phone, with no SSH
or build toolchain needed. It does almost nothing itself — it serves one page
and shells out to an embedded `push-catalog.sh` for every action, so the
install logic has exactly one home (`go:embed`).

**Model:** Push Hack Catalog hosts no binaries. `catalog/catalog.json` (in
this repo) is an index of pointers — each entry names a hack's own
`github_repo`. That repo publishes its own GitHub Releases and keeps a
`release.json` at its root. The daemon fetches that file live on every
install, and on every `/api/catalog` listing (to read each entry's live
`version` and `released_at`), downloads the release tarball it points to, and
extracts it — the tarball's own `hack.json` plus binary — straight into
`/data/push-hack/hacks/<id>/`. There is no sha256 pin and no signing: the
trust boundary is "this repo is on GitHub, and its catalog entry was
PR-reviewed once." See `catalog/ARCHITECTURE.md` for the full model and
`catalog/PUBLISHING.md` for how a hack author publishes into it.

| File | Role |
|------|------|
| `push-catalog.sh` (embedded into the binary through `make embed`) | Holds all the logic. `q()`/`rq()` are python3-only JSON readers, with no `jq` dependency (matching the framework installer's own choice). `q`'s `catalog` operation passes through `github_repo` (who maintains the hack, shown in the web cards and the Shadow UI breadcrumb) and fetches each hack's live `release.json` through `urllib` to add `version` and `released_at` to the listing, falling back to `null` per entry rather than failing the whole listing. Given an optional `hacks_dir` argument, it also reads that hack's locally installed `hack.json` for `installed_version` (it sets `update_available` when this differs from the live `version`), `port`, and `web_ui` (so the web UI can render an "Open" link) — `cmd_catalog` always passes `$PUSH_HACK_DIR/hacks`. `fetch_release()` pulls a hack's `release.json` from `raw.githubusercontent.com`, or from a `release_url` override for local or dev entries. `cmd_install` fetches the release, downloads the tarball, runs `tar -xzf` into `hacks/`, reads the extracted `hack.json`, and calls `install_service` — this is also how an update applies, through the same command, with no separate code path. `cmd_remove` removes a hack. `--self-test` runs offline: it checks catalog parsing, the catalog-enrichment operation (including `installed_version`/`update_available`), and a checked-in `testdata/fixture-hack.tar.gz` that exercises the fetch/extract path — all through `file://` overrides, with no real network access and no touch to `/etc/init.d`. |
| `src/main.go` | The HTTP server. See [api-reference.md](api-reference.md) for its routes. `GET /api/installed` returns `[{id, enabled}]`, where `enabled` reads the init.d service's actual running state (through `status`'s printed text, since its exit code is always 0). A hack id is validated against `^[a-z0-9][a-z0-9-]{0,63}$` before it ever reaches the shell. |
| `src/index.html` | A single-file, responsive web UI. Catalog cards sit in a 3-column grid that collapses to fewer columns on narrow or phone widths, through CSS grid `auto-fill`. Each card shows the hack's name, description, author, the `github_repo` it installs from (linked to `homepage`), live version, last-updated date, and `requires` tags. An installed hack that declares `web_ui` in its `hack.json` gets an "Open" link straight to that hack's own UI (`installed_port` and `web_ui` are read off the installed copy by `push-catalog.sh`'s `catalog` operation, alongside `installed_version`). Install, Update, and Remove buttons (Update shows, with an "update available" tag, when `update_available` is true) open a popup modal that shows the shell output live, instead of a persistent log pane. |

## On Push (deployed layout)

```
/data/push-hack/
├── hacks/push-manager/   push-manager binary + hack.json
├── hacks/push-display/   push_hook.so, framebuf shm, midiflt shm
└── logs/                 push-manager.log, push-hook.log
```

## Push 3 — key facts

- **OS:** AbletonOS, kernel 5.15.48 real-time, x86_64 Intel.
- **Init:** sysvinit runlevel 5, not systemd.
- **SSH:** `ableton@push.local` for normal use, `root@push.local` for service
  install. There is no sudo.
- **Writable:** `/data` (ext4, 201GB). User content lives at
  `/data/Music/Ableton/`.
- **Read-only:** `/opt`. Never write there.
- **MIDI routing:** ALSA seq, not libusb. Subscribe to "Ableton Push 3 Live
  Port" (usually client 16:0, auto-detected by name). The `CREATE_PORT` ioctl
  requires `portInfo[addr.client] = ownClientID`, or the kernel returns
  `EPERM`. MIDI blocking works through a hook on `snd_seq_event_input`, which
  sets the event type to `NONE` when `midiflt->enabled` is true.
- **USB drives:** auto-mount to `/run/media/<label>-<device>`.
  `usb-storage` is a kernel built-in.
- **Button map:** see `docs/push3-button-map.md`. Every button reports on CC
  channel 0, with 127 for press and 0 for release. The pad grid uses Notes
  36-99.
- **LED colors:** see `docs/push3-led-colors.md`, a 128-entry palette. The
  same index applies to a pad's Note velocity and a button's CC value.
  `core/push3/colors.go`'s `NamedColors` table was wrong for every entry
  until 2026-08-18 — it claimed a Push-2-derived even/odd split that does not
  hold for Push 3 (each of the 128 raw velocities is its own distinct
  color). The fix rebuilt the table from that doc's SysEx-queried data; see
  that file's header comment for the full story. Trust that doc over any
  claim `colors.go` makes about its own source.
