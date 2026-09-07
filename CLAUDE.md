# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> **Doc sync rule:** Keep this file, all `docs/` files, and `README.md` in sync with every code change. If a change affects behaviour, APIs, architecture, or known issues — update the relevant docs in the same commit. Save all implementation plans to `/plans/` with filename format `YYYY-MM-DD-title-of-the-plan.md`. Update [CHANGELOG.md](CHANGELOG.md)'s `## [Unreleased]` section in the same commit as any change worth noting to a future reader — new behavior, a fix, a changed API. Skip internal refactors and trivial edits.

> **Writing-style rule:** When writing or editing code comments, use the `caveman` skill. When writing or editing technical documentation (`docs/`, README files, hack-level READMEs, `CHANGELOG.md`), use the `simple-english` skill.

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

Full write-up (framework layer, hack structure convention, `core/` shared
library package table, and each core hack's file-by-file layout) lives in
[docs/architecture.md](docs/architecture.md). HTTP routes and wire protocols
live in [docs/api-reference.md](docs/api-reference.md). This file keeps only
the rules that must not be violated.

**Push Display OS-update freeze:** an Ableton OS update flashes co-processor
firmware over the same USB/libusb path the push-display hook interposes.
With the hook installed, this hangs the device mid-update (blank screen,
dead buttons). An in-process kill-switch does not work — an LD_PRELOAD
interposition cannot be removed from a running process, and by the time any
update signal appears, Push3 is already the hooked process flashing
firmware. **Uninstall the hack (`./scripts/uninstall.sh`) before an OS
update, then reinstall after.** See `hacks/push-display/README.md`.

## USB-A port safety

**Fix (in `midi.go`):** `waitForBootSettle()` defers all `/dev/snd` access until uptime ≥ 30s. Opening ALSA seq during the cold-boot USB-A enumeration window (~3–15s) wedges the port permanently until power-cycle. HTTP server starts immediately; MIDI/LED/Shadow-UI come online ~30s after cold boot.

**Recovery if wedged:** full power-cycle (hold until off, wait 15s, power on with device attached).

**Testing gotcha:** wedge reproduces only on cold power-on, never on warm `reboot`. Always test with `poweroff` + manual power-on.

## On Push (deployed layout) and Push 3 key facts

See [docs/architecture.md](docs/architecture.md) for the deployed directory
layout (`/data/push-hack/...`) and Push 3 hardware/OS facts (SSH access,
writable paths, MIDI routing, button map, LED colors).

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

`docs/` holds architecture, API, and Push hardware/OS references; each hack also documents itself in its own folder README.

Framework and API (`docs/`):
- `docs/architecture.md` — framework layer, hack structure convention, `core/` shared library, per-hack file layout, deployed directory layout, Push 3 hardware/OS facts
- `docs/api-reference.md` — HTTP routes for push-manager and push-catalog, the RemotePanel contract, the push-display shared-memory protocol

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
