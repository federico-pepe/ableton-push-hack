# Changelog

All notable changes to this project are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versioning follows
[Semantic Versioning](https://semver.org/) (pre-1.0: expect breaking changes
between minor versions).

## [Unreleased]

### Added

- push-manager can drive the Push screen over USB while Push3 is stopped. `POST /api/display/direct {"enabled": true}` claims the screen. From then on every frame push-manager writes (`/api/display/image`, Shadow UI, OSD) goes straight to the screen. `{"enabled": false}` releases it. Turn it off before you start Push3 again. It fails with 409 while Push3 is running. `GET /api/display/status` now also returns `direct`. This is the first part of a Mixxx mode (see `plans/2026-10-12-mixxx-mode.md`). Tested on device.
- After an install, update or remove from push-manager's CATALOG tab, the Shadow UI tabs update right away. A new hack's tab shows up, and a removed hack's tab goes away, without closing and opening the Shadow UI.
- push-manager's CATALOG tab shows a download bar with MB done and total while it installs a hack. push-catalog serves the numbers at `GET /api/progress`.
- Catalog: Mixxx Mode (`itsrave/push-mixxx-mode`). DJ with Mixxx on the Push, with the screen driven through push-manager's direct mode above.
- `core/display`: `USB` (`OpenUSB`/`Set`/`Close`) sends frames to the screen's USB interface, and `ToWire` applies the screen's XOR pattern. Needs a `core/v*` tag before consumers outside this repo get it.
- **Push Hack Home**, one switcher for every hack that uses Push's screen, buttons and pads, and for the Shadow UI's tools. **Shift + Help** opens it from anywhere; **Shift + Help, Help** (hold Shift, tap Help twice) opens the superstar entry directly, and goes back to Live from inside it. Home shows eight entries a page under the top buttons, the starred ones first; the pad columns open them too. STAR stars an entry, Shift + STAR makes it the superstar; both are also on the web UI's Display → Tabs page. push-manager now owns the surface and lends it to one app at a time: while Home or an app owns it, `/api/display/mode`, `/api/midi/filter` and frames from anyone but the owning app get 409, and push-manager's own chords do nothing. An app that stops while it owns the surface gives it back to Home, which says so; pressing a stopped app starts it through push-catalog, and it opens when it connects. A hack becomes an app by declaring `"surface": {"label", "color"}` in `hack.json` and holding a session (`GET /api/surface/session`, server-sent events). `core/pmclient`: `Client.App`, `RunSurface`, `SurfaceStatus`, `SurfaceInput`, `SurfaceLeave`, `SurfaceOpen`, `HasSwitcher`. Needs a `core/v*` tag before consumers get it. Design: `plans/2026-10-17-push-hack-switcher.md`.
- `push-catalog`: a hack that declares only `surface` gets a port, like one with `web_ui` or `shadow_ui`. push-manager lists only hacks with a port.
- `push-manager`: `/api/live/listen` turns a listen track on and off: a silent MIDI track whose input follows the selected track and whose output is a hack's port, so a hack gets the notes played on Push's pads, which no MIDI input carries. `core/pmclient`: `LiveListen`. Needs Browser Bridge 0.4 or later.
- `push-manager`: `GET /api/live/scale`, the Set's scale (the one Push's Scale button edits): root, name, intervals and Scale Mode. `core/pmclient`: `LiveScale`. Needs Browser Bridge 0.3 or later (`get_scale`).
- Support for hacks that own the whole Push surface:
  - `push-display`: buttons listed in the new `pass_cc` bitmap still reach Live while MIDI intercept is on (Play 85 and Record 86, for example). Push3's own LED writes are held back while intercept is on, so a hack keeps its pad colours.
  - `push-manager`: `/api/midi/filter` takes `pass_cc`. Enabling intercept through the API makes push-manager repaint Live's LEDs when it goes off. New `/api/display/frame` takes one raw frame: about 1 ms a frame on device, against about 11 ms for a PNG.
  - `core/display`: `EncodeFrame` (no allocation, 13 times faster than the general path) and `Shm.WriteFrame`. `ToBGR565` uses the fast path for 960x160 NRGBA images.
  - `core/pmclient`: `PushFrame`, `SetMidiFilterPass`, and `Status.Mode`. `MidiFilterStatus` tells whether push-manager has these features, so a hack can ask the user to update Push Hack instead of failing silently.
  - `core/push3`: `LEDButtons`, every button with an LED (push-manager's list plus the 8 scene buttons), and `IsRGBButton`, so a hack can set all of them.
  - `docs/push3-internals.md`: how Push3 writes LEDs, taking the surface on a button release, and restarting Push3 safely.
- `core/alsaseq`: raw MIDI support for ported tools. `EncodeMIDI` and `DecodeMIDI` turn raw MIDI bytes (notes, CC, program change, pressure, pitch bend, clock, start, continue, stop, song position) into sequencer events and back. `SendMIDI` sends raw bytes. `Subscribers` is the address for every port connected to yours. `SetName` names the client. New tests check the event layouts, the ioctl numbers, and the parser against a real capture from a Push 3 (`testdata/seq_clients_device_v323.txt`).
- `docs/push3-internals.md`: how a hack sends MIDI into Live and gets clock out of Live, tested on a Push 3. Live picks up a new port by itself, and a Sync switch in Push's MIDI settings sends clock to a hack's port.
- Catalog: Riffs, a riff generator and performance sequencer: six engines with 32 flavors write riffs over chord loops into six lanes, played from a block grid on Push's screen, pads and buttons. It opens from Push Hack Home (Shift + Help), so it needs push-manager and push-display with Push Hack Home.
- Catalog: Stacks, a chord-progression tool ported from Ableton Move. It uses Push's screen, pads and buttons, and plays into Live. It needs push-manager and push-display with pass-through buttons and raw frames.
- `catalog/PUBLISHING.md`: how to build a hack that has C code (cgo) as a static binary.
- `docs/push3-led-colors.md`: addendum on pad versus screen colors. It covers the incomplete named palette, the measured screen RGB for the 26 primary colors and the dim pad shades.
- `scripts/VST/je8086-syx-to-vstpreset.py` and `scripts/VST/dexed-syx-to-vstpreset.py` turn JE8086 performance dumps and Dexed cartridges into `.vstpreset` files, one per performance or voice. Both rebuild a preset the plugin saved byte for byte. The JE8086 converter also writes the 128 single patches with `--patch-template`. Tested on device: three JE8086 performances, three JE8086 patches and four Dexed voices sound like the originals.
- `scripts/VST/surge-fxp-to-vstpreset.py` converts Surge XT `.fxp` patches into `.vstpreset` files for the Push User Library, so the factory patches load from the PLUGINS tab. Tested on device with four factory patches; all 637 convert.
- `docs/vst3-on-push3.md` now covers plugins that need data files, with JE8086 (JP-8000 emulation) as the tested example: where the firmware goes on Push, and that the folder name is case-sensitive.

### Fixed

- `push-catalog`: an install or update could leave a hack stopped. The service's `stop` sent the signal and returned at once, so a hack that takes more than a second to exit still held its port when the new one started, and the new one quit with "address already in use". `stop` now waits for the hack to exit (up to 5 seconds, then `SIGKILL`). Found on a Push 3 with Stacks, which saves its state on exit. `--self-test` checks it. Hacks installed before this keep the old service script until they are reinstalled or updated.
- A hack's Shadow UI tab works again after the hack is removed and installed again through the catalog. The catalog gives it a new port, and the Shadow UI kept asking the old one until push-manager restarted.
- An install from push-manager's CATALOG tab no longer fails after 3 minutes. Large hacks (Mixxx Mode is about 170 MB) take longer over Wi-Fi. The limit is now 30 minutes.
- push-manager no longer interferes with another app that owns the screen in direct mode. While direct mode is on, the Shift+Set and Shift+Settings chords do nothing, and the Shadow UI does not open. Before, a chord in Mixxx mode cleared every LED, drew an OSD over Mixxx and opened the Shadow UI. Turning direct mode on also closes an open Shadow UI and turns MIDI intercept off, so Push3 gets its MIDI again after you switch back. This lets a hack's Shadow UI tab start an app that takes over the screen.
- Two hacks could react to one button combination: Riffs opened with Shift + Set, which is also push-manager's `browser_open` chord, so one press started the Shadow UI and Riffs at once, both drawing on the screen (device log, 2026-10-17). Push Hack Home (below) gives every app one way in, and push-manager's chords no longer fire while an app owns the surface.
- A hack that owned the surface could leave Push unusable. With its screen showing and the MIDI intercept on, Live's questions on the power button ("save changes?") could be neither seen nor answered, so only holding the power button turned Push off. The intercept flag then survived into the next start, and Live's "recover your work?" could not be answered either. push-manager now clears the intercept and the display at its start, gives both back to Push3 as soon as the power button is pressed (it watches `/data/settings/.shutdown`, which `power.sh` writes), and refuses a hack's takeover until the device is off. Only a marker that appears or changes counts as a press. At start the launcher deletes the last shutdown's marker, and that deletion is not a press: if it were, every app would be refused until push-manager restarted.
- MIDI intercept could do nothing at all. push-manager runs as root, and when it created `midiflt` before Push3 did, the file was `root`, mode 0644, and the hook (running as `ableton`) could not open it. push-manager now keeps the file at mode 0666, owned like its folder, and the hook retries the open every 2 seconds.
- `push-manager`: the comment on `/api/display/image` said it sets takeover mode; it does not.
- `core/alsaseq`: `EvStop` and `EvContinue` were swapped. The kernel uses 31 for Continue and 32 for Stop, so a hack that follows Live's MIDI clock read Stop as Continue. Checked against the kernel header and on a Push 3, where Live's Stop arrives as type 32. A new test keeps all event type numbers equal to the kernel's. Needs a `core/v*` tag before consumers get it.
- `push-display`: the hook log (`/data/push-hack/logs/push-hook.log`) no longer grows without bound. Only Push3 logs that the hook loaded (each `amidi` or `sh` child used to add a line), only the first 3 display frames are logged, and the log moves to `push-hook.log.1` once it passes 1 MiB. Needs a rebuilt `push_hook.so`.
- `core/push3`: `ColorForIndex` returns the exact hardware color for all 128 indices. It used to return the nearest named entry below an unnamed index, which was wrong for 38 values (for example 67 gave the color of 66). A new test keeps the table equal to `docs/push3-led-colors.md`. Needs a `core/v*` tag before consumers get it.
- push-manager: `POST /api/display/frame` also reaches the screen in direct mode (it only wrote shm and answered 503 before). Shift+Help and Home's pad columns do nothing while direct mode is on, so Push Hack Home cannot open over Mixxx mode.

### Changed

- `core/v0.3.0` is tagged. It holds the raw MIDI helpers in `alsaseq` (with the corrected Stop, Continue, Sensing and Song Position numbers), `display.USB`, `ToWire` and `EncodeFrame`, the `pmclient` surface and frame calls, and `push3.LEDButtons`. The consumer repos (automation, keyboard-visualizer, arrangement, audio-loopback, braids, push-tethered-app, pta-module-arrangement) pin it.
- The VST scripts moved into `scripts/VST/`: `make-vst3-preset.py`, `vst3_params.py` and the preset template. Update any saved command to `./scripts/VST/make-vst3-preset.py`.

## [0.1.12-alpha] - 2026-10-04

### Added

- `docs/vst3-on-push3.md` now has a "Build a plugin from source (Docker)" section: a build script for plugins that have no Linux `.vst3`, or one built on a newer glibc than Push has. Tested with Dexed.

### Fixed

- After the Shadow UI closed, Push's button LEDs and pad grid stayed dark until you pressed a button. This happened after a chord exit and after a preset or plugin load. push-manager now repaints them when MIDI intercept turns off: it presses Shift, then the other pad mode and your current one, straight into Push3's MIDI input. You end up in the pad mode you were in.

## [0.1.11-alpha] - 2026-10-04

### Added

- PLUGINS tab in the Shadow UI and a Plugins view in the web Browser tab (next to All, Presets and Samples). They list the VST3 plugins Live has scanned, with their `.vstpreset` presets, and load the pick onto the selected track through Browser Bridge. After a load the Shadow UI closes, like it does for presets. New routes: `GET /api/live/plugins` and `POST /api/live/load` with `"type":"plugin"`. Needs a Browser Bridge version with `list_plugins` and `load_plugin`.
- `docs/vst3-on-push3.md` and `scripts/make-vst3-preset.py` — Live on Push 3 can load Linux VST3 instruments itself. The guide shows how to turn on VST3 scanning and make a preset file without a Mac. Tested with Surge XT; the generated preset is not yet confirmed on the device. The script can list a plugin's parameters (`--list-params`, using `scripts/vst3_params.py` run on Push) and write an encoder map (`--params`, by ID or by title). A map saved on a Mac works on Push. A `.vstpreset` in the User Library loads through Browser Bridge.
- `docs/scripts.md` — full flag reference for `discover.sh`, `install.sh`, `uninstall.sh`, and `hacks/push-display/deploy.sh`, moved out of README.md.
- `docs/adding-a-hack.md` — full walkthrough for a new hack, moved out of README.md.

### Changed

- The docs now describe the split-repo layout. Optional hacks live in their own repos and pin `core` to a tagged version (`core/vX.Y.Z`, currently `v0.2.0`) with no `replace`. Only push-manager keeps `replace ../../../core`. `docs/adding-a-hack.md`, `core/README.md`, `docs/architecture.md`, and `CLAUDE.md` changed. `CLAUDE.md` now links to `catalog/catalog.json` for the list of catalog hacks, and no longer names each hack.
- MANUAL.md now points to the [Push Hack Installer](https://github.com/federico-pepe/push-hack-installer) as the easiest way to install Push Hack.
- MANUAL.md's Browser Bridge and Automation setup steps (with screenshots) moved to their own repos: [push-hack-browser-bridge](https://github.com/federico-pepe/push-hack-browser-bridge) and [push-hack-automation](https://github.com/federico-pepe/push-hack-automation).

### Fixed

- `install.sh` no longer dies with "Permission denied" creating
  `<data-dir>/push-hack/hacks` on a freshly factory-reset device. It now
  retries the top-level `mkdir` as root and hands ownership to `ableton`
  instead of failing outright.
- `install.sh` per-hack directory ownership normalization was chowning to
  `${PUSH_USER}` instead of the fixed on-device runtime user. Running
  `install.sh --user root` (e.g. to work around the mkdir failure above)
  left hack directories `root`-owned, which broke push-display's hook:
  it runs as `ableton` and could no longer create `framebuf`/`midiflt`
  inside its own hack dir ("open failed" in `push-hook.log`). Now
  hardcoded to `ableton`, independent of which user the deploy script
  connects over SSH as.

## [0.1.10-alpha] - 2026-09-08

### Changed

- Catalog hacks no longer declare a `port` in their own `hack.json`.
  `push-catalog install` now assigns one automatically — the lowest free
  integer `>= 7711` — before the hack's service ever starts, keeping a
  reinstalled/updated hack's existing port stable. Only a hack that
  declares `web_ui` or `shadow_ui` gets one; a binary alone isn't enough
  (a background service with no HTTP surface, e.g. one that just loads a
  kernel module, is left with no `port` field). 7701-7710 is reserved
  for the framework (push-manager 7701, push-catalog 7702, Browser
  Bridge's fixed Remote Script socket 7704). This makes port collisions
  between catalog hacks structurally impossible, so Push Manager's
  port-conflict banner/log warning (`portConflicts()`/`warnPortConflicts()`
  in `ui_tabs.go`) has been removed as dead weight. See
  `catalog/schema.md`.

### Fixed

- `install.sh`'s default (deploy pre-built binaries, no `--build`) failed on
  a fresh clone: push-manager and push-catalog binaries were gitignored and
  never shipped, so the installer died with
  `Binary '<name>' not found ... Run make first.` Both binaries are now
  committed to git alongside push-display's already-tracked `push_hook.so`,
  so `./scripts/install.sh` works out of the box with no Go or Docker
  toolchain installed. `--build` still rebuilds from source for developers.
  Rebuild and recommit these binaries on release.

## [0.1.9-alpha] - 2026-09-07

### Added

- Push Manager's menu bar now lists other installed hacks. Any hack that
  declares `web_ui` (`{"label", "path"}`) next to its `port` in its own
  `hack.json` gets a header link to its UI — no push-manager code per hack.
  This is the same field Push Hack Catalog already reads for its "Open"
  links, so a hack that works there needs no change.
- A hack can now own a **Shadow UI tab** the same way, by declaring
  `shadow_ui` (`{"label", "path"}`). push-manager polls
  `http://127.0.0.1:<port><path>` for a small JSON view (title, rows,
  cursor, soft-button labels) and POSTs every press back as `{cc, value}`.
  The hack owns all the state; push-manager only draws. Not a pixel
  protocol — a PNG per frame would cost an encode plus a decode 30 times a
  second on a device whose job is not stealing CPU from Live. Contract:
  push-manager's README, "Shadow UI tabs from other hacks".
- **Display → Tabs** settings page in Push Manager's web UI. One ordered
  list drives both navigations, with two switches per entry: **Shadow**
  (a tab on Push's screen) and **Web** (a link in the menu bar). Reorder
  with ↑/↓ — not drag-and-drop, since this page gets used on a phone next
  to the hardware. Saving applies to a running Shadow UI immediately, and a
  tab that survives the change keeps its state. Persisted to
  `<hackdir>/ui_tabs.json`. New routes `GET`/`POST /api/ui/tabs`.
- Push Hack Catalog always has a menu-bar entry in Push Manager. The built-in
  **Catalog** entry carries the link itself (core hack, fixed port 7702), so
  it no longer depends on the installed copy declaring `web_ui` — an older
  build, or one installed under a different id, still gets a link. It stays
  an ordinary entry: switchable and movable like any other.
- Port-conflict reporting. Two installed hacks declaring the same `port`
  means only one of them can be running, and anything pointing at that port
  reaches the wrong hack or nothing. push-manager now flags it: a banner on
  Display → Tabs naming the port and every hack claiming it, a note on each
  affected row, and one line in `push-manager.log` at startup. The menu bar
  renders one link per port, earliest in the user's order winning. Nothing is
  probed over the network — the `hack.json` files say enough, and a liveness
  poller would be a running cost for a problem you fix once.
- Push Hack Catalog shows who maintains each hack and which repo it installs
  from: `github_repo` on the web cards (linked to `homepage`), and on the
  Shadow UI's CATALOG tab as the selected hack's breadcrumb. The author is
  dropped when it is just the repo owner again, so the common case reads as
  one thing rather than the same name twice.

### Changed

- Shadow UI tab positions are no longer hardcoded. A tab's top-strip button
  is its position in the user's resolved list, so tabs can be reordered or
  switched off — including the built-in ones. `browsePanelIdx` is gone; the
  Shift+Set chord looks the Browse tab up by id and does nothing if the user
  switched it off.
- `GET /api/hacks/installed` returns `[{id, name, port, web_ui?, shadow_ui?, enabled}]`
  instead of a bare array of ids. Breaking for anything that consumed the old
  shape; in-repo the only caller was Push Manager's own `app.js`.
- Push Manager's hardcoded `Catalog` header link is gone. The link is now
  built from push-catalog's own `hack.json`, like every other hack's.

### Fixed

- A hack disabled through Push Hack Catalog (service stopped, boot-autostart
  removed, files kept) no longer leaves a dead `web_ui` link in Push
  Manager's menu bar or a `shadow_ui` tab that never answers. Push Manager
  now checks the same boot-autostart signal Push Hack Catalog itself uses to
  report a hack as enabled/disabled, and drops a disabled hack's nav entry
  entirely; re-enabling it restores the entry with its saved order/switches.
  It also stops counting a disabled hack as a port claimant in the
  port-conflict scan.

## [0.1.8-alpha] - 2026-09-06

### Added

- Shadow UI's CATALOG tab gains an Enable/Disable soft-button (Bot3), matching
  the web UI's per-hack toggle — posts `/api/enable`/`/api/disable` for the
  selected hack. An installed-but-disabled hack now also shows a `[disabled]`
  tag in the on-device list.

### Fixed

- Shadow UI's CATALOG tab no longer shows every hack as not-installed. The
  0.1.7-alpha change to `/api/installed` (bare id list to `{id, enabled}`
  objects) broke `catalog_panel.go`'s decoder, which still expected a
  `[]string` and failed silently, leaving the installed set empty — so no
  hack ever rendered green or offered Remove/Update on-device, even though
  the web UI (already reading the object shape) was unaffected.

## [0.1.7-alpha] - 2026-09-06

### Added

- Push Hack Catalog gains Enable/Disable per installed hack: `POST
  /api/disable`/`/api/enable` stop+remove (or re-add+restart) boot-autostart
  while keeping the installed files and config, for saving CPU/RAM on a
  hack you're not using right now without a full remove+reinstall.
  `/api/installed` now reports `{id, enabled}` instead of a bare id list,
  determined by the hack's `/etc/rc*.d` boot-autostart link (number-agnostic
  glob — real hardware uses Debian-style `update-rc.d`, which picked `S20`,
  not the `S99` this script's own manual-symlink fallback assumes) rather
  than a running-process check: `push-display`'s own "service" patches
  `push3`'s init.d and exits, so it never has a pidfile to check, by
  design — found live, it reported "disabled" unconditionally under an
  earlier version of this check. Disabling any of the three base hacks
  (`push-manager`/`push-display`/`push-catalog`) is refused outright —
  also found live: disabling `push-catalog` from its own web UI kills the
  server the request came in on before it can respond, and recovery
  needed SSH, not something a phone-only user has.
- `catalog/catalog.json` gains entries for `push-audio-loopback` and
  `push-braids`, both now installable via Push Hack Catalog from their
  own repos:
  [federico-pepe/push-hack-audio-loopback](https://github.com/federico-pepe/push-hack-audio-loopback),
  [federico-pepe/push-hack-braids](https://github.com/federico-pepe/push-hack-braids)
  (renamed from `push-braids-host` when split out). `push-braids`
  declares `push-audio-loopback` as a `requires`, and both are listed as
  "(alpha)" — newer, less battle-tested than the rest of the catalog.
  `hacks/push-audio-loopback/` and `hacks/push-braids-host/` are removed
  from this monorepo — same as keyboard-visualizer/automation/browser-bridge
  before them; development now happens in the split repos directly, whose
  own history covers persistence, the on-screen I/O picker, and the DSP
  host itself.
- `core/alsapcm`: enumerates ALSA sound cards (`/proc/asound/cards`) and
  their playback-capable PCM devices, for hacks that let a user pick an
  audio output device.

### Fixed

- `push-catalog`'s `install_one` reported `"ok": false` on the HTTP API for
  a completely successful install whenever the hack had no `post_install`
  message — a `[ -n "$post_install" ] && info ...` test as the function's
  last statement, with no `post_install` set, made that test's own exit
  status (1) become the function's return value. Found live via a real
  install.

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
