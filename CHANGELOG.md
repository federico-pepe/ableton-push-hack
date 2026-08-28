# Changelog

All notable changes to this project are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versioning follows
[Semantic Versioning](https://semver.org/) (pre-1.0: expect breaking changes
between minor versions).

## [Unreleased]

### Added

- `push-catalogue` (`hacks/push-catalogue/`, formerly `push-store`):
  on-device homebrew-style installer (port 7702) for community hacks,
  browsable and installable from a phone or from push-manager's own Shadow
  UI (new `CATALOG` tab, `src/catalogue_panel.go`). Catalog lives at
  `catalogue/catalog.json`; each hack is an independent GitHub repo that
  publishes its own `release.json` + release tarball — the daemon always
  resolves the latest release live, no central hash-pinning. `GET
  /api/catalog` and the web UI also show each hack's author, live version,
  and last-updated date (from that hack's own `release.json`). See
  `catalogue/ARCHITECTURE.md` and `catalogue/PUBLISHING.md`.

### Changed

- `keyboard-visualizer` moved out of this repo to its own
  `federico-pepe/push-hack-keyboard-visualizer`, as the first hack
  published through the new push-catalogue model. History preserved via
  `git subtree split`. Depends on `core` as a real tagged Go module
  (`core/v0.1.0`) instead of a relative `replace`. Port moved 7702 → 7705
  to make room for push-catalogue at 7702 (next after push-manager).
- Ports: 7701 = push-manager, 7702 = push-catalogue, 7703 = automation,
  7704 = browser-bridge, 7705 = keyboard-visualizer (external repo).
- Renamed `push-store` → `push-catalogue` throughout (hack id, binary,
  directory, script, `PUSH_STORE_REGISTRY` → `PUSH_CATALOGUE_REGISTRY`,
  web UI title "Push Hack Catalogue").

### Fixed

- `push-catalogue`'s `--self-test` fixture path was one directory too
  shallow and always failed; also extended to exercise the release-fetch +
  tarball-extract path and the new catalog-enrichment op, not just catalog
  parsing.
- `push-catalogue`'s web UI: a hack's `requires` list rendered unescaped,
  a stored-XSS gap if a malicious catalog entry were ever merged.
- `push-catalogue`'s generated init.d service used `start-stop-daemon -b`,
  which silently drops the invoking shell's log redirection once it
  detaches on this device's busybox — a store-installed hack ran correctly
  but wrote nothing to its log file. Switched to the same `nice`-backgrounded
  pattern the framework's own `install.sh` template already uses. Found via
  an on-device install, not by reading the code.
- keyboard-visualizer's first GitHub Actions release (v0.1.0) built cgo-enabled
  and dynamically linked against the CI runner's glibc — Go only disables cgo
  automatically when cross-compiling to a *different* OS/arch than the build
  host, so a linux/amd64 runner building for linux/amd64 doesn't get that for
  free. Binary built cleanly but couldn't even exec on Push 3. Fixed with
  explicit `CGO_ENABLED=0`; documented in `catalogue/PUBLISHING.md` so future
  hack authors don't hit the same trap.

## [0.1.0-alpha] - 2026-08-26

Retroactive baseline: this is the first tagged release, covering everything
from the initial commit (2026-07-29) through the point semantic versioning
was introduced. Dates below are per-change, not per-release — there was no
prior tag to diff against.

### Added

- `keyboard-visualizer`: a Live-sourced piano keyboard hack rendered on
  Push's screen, plus a mobile web view with chord detection. Documents
  and detects its `push-manager`/`push-display` dependency.
- `core/`: extracted as a shared Go module so both this repo and
  `push-tethered-app` reuse it via a `go.mod` `replace` instead of a fork —
  `push3` (button map, LED palette, geometry, encoder math), `gfx`/`gfx/text`
  (drawing + font rendering), `display` codec, `httpx`, `hackcfg`, `sse`
  (generic broker for automation and keyboard-visualizer), `pmclient`
  (push-manager's display/tempo HTTP API), and `alsaseq` (read loop, output
  path, port enumeration, boot-settle wait).
- `core/gfx/widgets`: a shared, Shadow-UI-style component framework —
  `KVRow`, `ListView`/`RenderList`, `SoftButton` (with `.Group` and a visual
  grouping cue), `DrawPadGrid`, `DrawKnob`/`DrawKnobFull`/`DrawKnobArc`
  (plus `Knob.Color`, `Knob.Bipolar`, `Knob.ValueScale`), `DrawFader`,
  `DrawEnvelope`, `DrawMeterV`, `DrawListCols`/`DrawScrollbarH`, and
  `DrawStatusBar`. All four existing panels (Stats, Midi, File, Browser)
  were migrated onto it, replacing ad hoc per-panel drawing and
  string-matching. `core/gfx/layout` adds the 8-column grid and
  top/bottom bar content rect the design system lays out against.
  Decisions recorded as they were made in `DESIGN.md`.
- Typography: opt-in outline fonts (`text.NewFace`/`DrawWith`/`WidthWith`)
  and integer nearest-neighbor upscaling (`text.DrawScaled`/`WidthScaled`)
  alongside the original bitmap face; the basic/styled fonts were replaced
  with Tamzen (bitmap-style default) and Helvetica Neue (opt-in styled
  weights), and a Push LED palette RGBA lookup was added alongside.
- `push-manager`: a USB Input checkbox to load/unload the `usbhid` kernel
  module on demand.
- `push3-internals`: research notes from GPL source review and live SSH
  sessions on Push 3's USB architecture, including IPC socket findings.

### Fixed

- `push3.NamedColors` — every entry in the table was wrong; corrected
  against the real device.
- Push 3's touch-sensor map corrected; the jog wheel is now recognized by
  `IsEncoderCC`.
- `gfx/text` truncation now marks a cut with `"..."` instead of U+2026 —
  the bitmap face has no glyph for it and was drawing a missing-glyph box.
- `gfx/text` Face access is now serialized (`faceMu`) — concurrent renders
  from more than one caller could corrupt the shared rasterizer's internal
  buffers and crash.
- `core/display/shm.go` restricted to a `linux` build tag; display geometry
  moved to `core/display` and stale CW/CCW comments fixed.
- `install.sh` no longer depends on `jq`/`python` (pure-bash JSON handling);
  `python3` detection fixed on Windows.
- `ensureMidiFilt` no longer caches a permanent-failure result forever;
  remaining tracked `build/` binaries untracked.

### Changed

- Shadow UI and keyboard-visualizer render loops bumped to ~30fps.
- The under-screen soft-button strip widened to all 8 buttons.
- Design system visual polish: `DrawArc`/`drawLine` anti-aliased by
  default, knob stroke thickened, and widget colors resolved through
  `push3.Palette`/`ColorForIndex` (`Theme`/`groupColors`) instead of raw
  RGB literals — the same palette-invariant `push-tethered-app` later
  adopted for its own design system.

[Unreleased]: https://github.com/federico-pepe/ableton-push-hack/compare/v0.1.0-alpha...HEAD
[0.1.0-alpha]: https://github.com/federico-pepe/ableton-push-hack/releases/tag/v0.1.0-alpha
