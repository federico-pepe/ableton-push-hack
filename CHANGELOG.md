# Changelog

All notable changes to this project are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versioning follows
[Semantic Versioning](https://semver.org/) (pre-1.0: expect breaking changes
between minor versions).

## [Unreleased]

### Added

- `hacks/push-audio-loopback`: a virtual sound card ("Push Hack Virtual
  Audio") that lets an outside process send audio to Live, or receive
  audio from it, without opening the real hardware device. Built from
  the Linux kernel's own ALSA Loopback driver, with a small rename
  patch. See `plans/2026-08-27-push-audio-virtual-device.md`.
- `hacks/push-braids-host`: a minimal Push3 host proving pad presses
  can drive a third-party DSP plugin (Braids, a Move Anything
  `plugin_api_v2` module) and reach Push 3's real speaker through
  `push-audio-loopback`'s virtual card — confirmed live on hardware.
  See `docs/push3-dsp-hosting.md`.

## [0.1.6-alpha] - 2026-08-30

### Added

- `push-catalog` supports non-service hacks via two new optional `hack.json`
  fields: `install_path` (copies the tarball's `remote-script/` payload
  there instead of `hacks/<id>/`, for hacks that don't fit the
  binary+service model, e.g. an Ableton Live Remote Script) and
  `post_install` (a one-time-setup hint surfaced in the install log — the
  daemon never drives Live's own UI). `push-catalog remove` now cleans up
  `install_path` too. See `catalog/schema.md`'s "Non-service hacks" section.
- `hack.json` gains an optional `web_ui` field (`{"label", "path"}`). Push
  Hack Catalog's own web UI reads it off an installed hack's `hack.json`
  (`push-catalog.sh`'s `catalog` op, alongside `installed_version`/`port`)
  and shows an "Open" link straight to that hack's UI — e.g. Automation and
  Keyboard Visualizer are now reachable with one tap from the same place
  you installed them, no need to know or type their port.
- Push Manager's web UI hides the preset Browser button when its
  dependency (`browser-bridge`) isn't installed (`GET
  /api/hacks/installed`), and the on-device Shadow UI does the same for its
  own Browse tab. Both are driven by one data-driven table
  (`ui_shadow.go`'s `panelDefs`, pairing each built-in Shadow UI tab with
  the hack id it depends on) rather than a hardcoded special case, so a
  future optional built-in tab gets the same install-gating for free — and
  both take effect live, without a push-manager restart, when the hack is
  installed/removed via the catalog.
- Push Manager's header carries a static Catalog link (port 7702 is
  hardcoded — push-catalog is a core hack, always installed alongside
  push-manager).
- Push Hack Catalog's web UI: catalog cards render in a 3-column
  responsive grid instead of a single stacked column, and Install/Update/
  Remove's shell output opens in a popup modal instead of a persistent log
  pane at the bottom of the page.

### Changed

- `browser-bridge` moved out of this monorepo into its own repo,
  [federico-pepe/push-hack-browser-bridge](https://github.com/federico-pepe/push-hack-browser-bridge),
  installable via Push Hack Catalog like Automation and Keyboard
  Visualizer — the framework's core install is now exactly three hacks
  (Push Manager, Push Display, Push Hack Catalog); everything else is
  optional.
- `stats.go`'s CPU breakdown (`watchedProcs`) now includes every currently
  installed hack with a binary, read live off each `hack.json`, instead of
  a hardcoded four-entry list (Ableton Index, Live, Push3, push-manager)
  that silently missed every hack split out of the monorepo (automation,
  keyboard-visualizer, push-catalog).

### Fixed

- Push Hack Catalog's output modal was visible on page load and couldn't
  be dismissed — `.modal-backdrop { display:flex }` (an author rule) beat
  the browser's built-in `[hidden] { display:none }` (a same-specificity
  user-agent rule always loses to an author rule). Added an explicit
  `.modal-backdrop[hidden] { display:none }` override.

## [0.1.5-alpha] - 2026-08-29

### Added

- `push-catalog install` now resolves a hack's `requires` before installing
  it: any required id that's itself a catalog entry and isn't installed yet
  is installed first (recursively); anything else named (the framework's
  own `push-manager`/`push-display`/`push-catalog`, or a hack not yet in
  the catalog) just gets a logged warning, since the daemon has nothing it
  could fetch for those.

### Changed

- `automation` moved out of this monorepo into its own repo,
  [`federico-pepe/push-hack-automation`](https://github.com/federico-pepe/push-hack-automation),
  now installed via Push Hack Catalog like Keyboard Visualizer. History
  preserved via `git subtree split`. Depends on `core` as a real tagged Go
  module (`core/v0.1.0`) instead of a relative `replace`. Port unchanged
  (7703).
- `catalog/catalog.json`: dropped `push-manager`/`push-display` from
  every entry's `requires` — they're the framework's own base install,
  assumed present, not something the catalog can install anyway. Added an
  `automation` entry (no `requires`). `random-preset` keeps `requires:
  ["browser-bridge"]` (a genuine catalog-to-catalog dependency).
- CLAUDE.md/README.md no longer describe hacks that live in their own
  repos (Automation, Keyboard Visualizer) — `catalog/catalog.json` is now
  the source of truth for what's installable and where its docs live.
- Push-manager OSD (intercept ON/OFF, browser-open chord) shown for 750ms
  instead of 2s — faster to dismiss.

## [0.1.4-alpha] - 2026-08-29

### Added

- `push-catalog` (renamed from `push-catalogue`, see below) can now detect
  when an installed hack is out of date: `/api/catalog` gains
  `installed_version` (read from the hack's locally installed `hack.json`)
  and `update_available` (true when it differs from the catalog's live
  `version`). The web UI shows an "update available" tag and swaps the
  Install button for Update; push-manager's on-device `CATALOG` tab shows an
  `[update: vX]` row and relabels its INSTALL soft-button to Update. Update
  reuses the existing install path (re-extract + restart the service) —
  no new command.

### Changed

- Renamed `push-catalogue` → `push-catalog` throughout (hack id, binary,
  service name `push-hack-catalog`, directory `hacks/push-catalog/`, the
  top-level `catalogue/` folder → `catalog/`, env vars
  `PUSH_CATALOGUE_REGISTRY`/`PUSH_CATALOGUE_LIB` →
  `PUSH_CATALOG_REGISTRY`/`PUSH_CATALOG_LIB`, web UI title "Push Hack
  Catalog"). Fixes the spelling; breaks any unit that already has
  `push-catalogue` installed — reinstall via `install.sh` or the old
  binary's own catalog UI pointed at the new registry URL.

## [0.1.3-alpha] - 2026-08-28

### Fixed

- `push-catalogue install` left the installed hack's directory owned by
  whatever uid/gid was baked into its release tarball (e.g. a CI runner's
  own uid) instead of `ableton:users` — `tar` run as root restores original
  ownership from the archive rather than defaulting to the current user.
  `uninstall.sh --purge` runs as the non-root `ableton` account (the Push
  has no sudo) and couldn't delete those directories. Now chowns the
  extracted hack dir to match `hacks/`'s own owner right after extraction,
  same convention push-manager's own runtime file writes already follow.
- push-manager's Shadow UI `CATALOG` tab (`catalogue_panel.go`) drew boxes
  in place of several characters — em dashes, an ellipsis, arrows/bullets in
  the bottom-strip hint — since `core/gfx/text` only has ASCII glyphs.
  Replaced with plain ASCII.

## [0.1.2-alpha] - 2026-08-28

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
