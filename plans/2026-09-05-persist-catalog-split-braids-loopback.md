# Persist + catalog-decouple push-audio-loopback / push-braids-host, add on-screen I/O picker

## Context

Two hacks — `push-audio-loopback` (virtual ALSA sound card) and `push-braids-host`
(Move Anything DSP host driving a Braids macro-oscillator off Push 3's own pad
MIDI) — currently exist only as manually-run test binaries in this monorepo.
Neither has a `hack.json`, so neither is a real installable hack: after every
reboot the user has to SSH in and manually reload the kernel module, re-copy
files out of `/tmp` (wiped on reboot), read Live's negotiated ALSA buffer
settings off `/proc`, and hand-launch the binary with matching CLI args —
steps currently just written down in `push-braids-host/README.md`.

Three goals, confirmed with the user:
1. Make both hacks installable through Push Hack Catalog, like
   keyboard-visualizer/automation/browser-bridge already are.
2. Make the install *persistent* — survive a real reboot with zero manual
   SSH steps.
3. Let the user pick, on Push's own screen, which ALSA MIDI input port and
   which ALSA audio output device `push-braids-host` uses, instead of both
   being hardcoded.

## Execution notes

- This plan file also lives at `/plans/2026-09-05-persist-catalog-split-braids-loopback.md`
  in the repo, per CLAUDE.md's doc-sync rule.
- Write all new READMEs, `hack.json` descriptions, and code comments in
  Simple English (ASD-STE100 style): short sentences, active voice, one word
  one meaning. Keep them short — say what is needed, no more.
- New GitHub repos (`push-hack-audio-loopback`, `push-hack-braids-host`) go
  under the personal `federico-pepe` account — same identity this repo's
  `origin` remote and git config already use
  (`6317270+federico-pepe@users.noreply.github.com`), not any Ableton work
  account or work email.

## Central finding that shapes the whole design

Read `hacks/push-catalog/push-catalog.sh`'s `install_service()` (the code
path that runs for every catalog-installed hack, i.e. exactly how both hacks
will run once split into their own repos). It **generates its own fixed
init.d script and ignores any `service.initd` the tarball ships**:

```sh
BIN="$dir/$bin"; CFG="$dir/hack.json"; ...
start() { nice -n 19 "$BIN" -config "$CFG" >>"$LOG" 2>&1 & echo $! >"$PIDF"; }
```

Consequences:
- **All persistence/self-healing logic must live inside the Go binaries
  themselves** — a bundled `service.initd` is never read by catalog installs.
  Neither hack needs one at all: `scripts/install.sh` already falls back to
  the same generic `-config hack.json` init.d generator when none is shipped
  (see Part A below).
- **No crash respawn exists.** Each binary must supervise itself.
- **No CLI args** — the binary only ever gets `-config <hack.json path>`.
  Everything currently passed as positional args (`dsp.so` path, PCM device,
  channels/rate/period/buffer) must move to files or be negotiated at runtime.

Other confirmed groundwork:
- `core/alsaseq.WaitForBootSettle()` exists (`core/alsaseq/bootsettle.go`) —
  must gate all `/dev/snd` access at boot per CLAUDE.md's USB-A safety rule.
  Neither hack calls it today.
- `docs/push3-dsp-hosting.md` confirms `/proc/asound/PHVAudio/pcm0c/sub0/hw_params`
  (name-keyed) works — no numeric card-index resolution needed.
- `core/pmclient.SetMidiFilter` (used by push-braids-host today) was added
  **after** the tagged `core/v0.1.0` — a new `core` tag is a prerequisite for
  the repo split.
- `core/alsaseq.EnumPorts`/`FindByName` (needed for MIDI picker) already
  predate `core/v0.1.0` — no new core work needed there.
- No PCM-device enumeration helper exists in `core/` — will be added as
  `core/alsapcm` now, ahead of a second consumer, per explicit decision (more
  audio-I/O-selecting hacks anticipated soon).
- cgo (both hacks use it — push-braids-host entirely, push-audio-loopback for
  `loopback_feed`) **cannot** use `PUBLISHING.md`'s documented
  `CGO_ENABLED=0`/`ubuntu-latest` release-workflow recipe — must run the same
  `docker run ... debian:bullseye/golang:1.25-bookworm` build both hacks'
  existing `Makefile`s already use, as a step inside GitHub Actions.

## Part A — push-audio-loopback: self-healing kernel module load

**New `hacks/push-audio-loopback/src/main.go`** (new pure-Go binary, becomes
`hack.json`'s `binary`, execed as `-config <hack.json>`):
1. `alsaseq.WaitForBootSettle()` before any `/dev/snd` access.
2. Idempotent module load via `golang.org/x/sys/unix.InitModule`/`FinitModule`
   (not shelling to insmod/lsmod): check `/proc/modules` for `snd_aloop`, then
   `/proc/asound/cards` for a card actually named `PHVAudio` (not just "some
   snd_aloop"). If wrong module loaded, try `DeleteModule` (back off on
   `EBUSY`, never force). If absent, load the bundled `.ko` for the running
   kernel with `id=PHVAudio timer_source=A3.0.0`.
3. **Vermagic check before load**: extract `vermagic=` from the bundled `.ko`
   bytes, compare against a running system module
   (`/lib/modules/$(uname -r)/kernel/sound/usb/snd-usb-audio.ko`). On
   mismatch: do not insmod, log one clear actionable line, keep looping in a
   degraded state — never `--force`.
4. **Forever monitor loop** (every ~30s): re-check `PHVAudio` is present,
   reload if it disappeared (manual `rmmod`, kernel hiccup). Log state
   transitions only.
5. `recover()` around each loop iteration — must never crash.

**Multi-kernel `.ko` bundling**: release tarball ships
`ko/<uname -r>/snd-aloop.ko` per known kernel version (built offline via the
existing Docker+Ableton-GPL-tarball recipe in the current README, one
subdirectory per firmware's kernel). Binary picks `ko/$(uname -r)/...` at
startup; missing-for-this-kernel is the same code path as vermagic mismatch
(log, don't insmod, keep looping). **Explicit limit**: a Push firmware update
that changes the kernel breaks this until a maintainer rebuilds and ships a
new `.ko` for that kernel — inherent to shipping a prebuilt kernel module,
not fixable within this plan.

**New `hacks/push-audio-loopback/hack.json`**: id/name/description/version/
binary `push-audio-loopback`, no `port` (no HTTP server).

**No `service.initd` needed**: `scripts/install.sh` (`generate_initd_script`,
`lib/common.sh:308`) already falls back to the exact same generic
`-config hack.json` init.d generator catalog's `install_service` uses when a
hack ships no custom `service.initd` (confirmed at `scripts/install.sh:319-333`).
Both install paths behave identically without one — skip it entirely.

**Dependency call**: depend on `core/alsaseq` for `WaitForBootSettle()` rather
than inlining a copy — small, tagged, avoids the exact divergence risk
`core/` was extracted to prevent.

## Part B — push-braids-host: config-driven, self-supervising, dynamic params

**B1 — `-config` flag replaces positional CLI args** in `main.go`, following
push-manager's own convention (`flag.String("config", "hack.json", ...)`,
sibling files via `filepath.Dir(*configPath)`). `dsp.so` and `module/`
(presets) become fixed paths relative to the install dir (tarball always
ships them there) instead of CLI-supplied paths. `pushManagerURL` becomes a
constant (`http://localhost:7701`) with an optional override field in the new
persisted config (below), not a CLI flag.

**B2 — new `braids-config.json`** (sibling of `hack.json`, same convention as
push-manager's `midi.json`): `midi_client`/`midi_port` (ALSA seq target),
`pcm_device` (e.g. `hw:PHVAudio,1,0`), optional `push_manager_url`. Defaults
on first run match today's hardcoded values (`Push3ClientDefault`/
`Push3PortDefault`, `hw:PHVAudio,1,0`) — zero behavior change until the user
touches the new picker. Atomic write (tmp file + rename) whenever the picker
commits a change. Lives at `/data/push-hack/hacks/push-braids-host/braids-config.json`
once catalog-installed — persists across reboot and across a catalog update
(tarball extraction doesn't wipe the dir first — **verify on-device**).
`channels`/`rate`/`period`/`buffer` are **removed entirely** — fully dynamic
per B3, never user-set.

**B3 — dynamic hw_params negotiation, supervisor architecture**
(`hacks/push-braids-host/src/audiosession.go`, new): restructure the current
monolithic render loop into a restartable "audio session" *within the same
process* (not a full process relaunch) — the DSP plugin instance and MIDI
subscription stay alive across a PCM renegotiation since they're independent
of Live's buffer size, and `bridge.h` already separates `bridge_pcm_open/close`
from plugin lifecycle. Rejected: in-place `hw_params` reconfiguration on a
running stream (ALSA semantics too risky, unproven, vs. the well-exercised
close+reopen path).
- `parseHWParams(r io.Reader)` — pure function parsing
  `/proc/asound/PHVAudio/pcm0c/sub0/hw_params` (name-keyed path), testable via
  fixture files (`src/testdata/hw_params_*.txt`, `closed` + real negotiated
  fixture) following `core/alsaseq/ports_test.go`'s exact pattern.
- `watchHWParams` goroutine, started from `main()`: (1) poll
  `/proc/asound/cards` for `PHVAudio` with backoff, logging state transitions
  only — this is how push-braids-host waits on push-audio-loopback at
  **boot time**, since catalog's `requires` only orders **installation**, not
  service start order (confirmed in `catalog/schema.md`); (2) poll
  `hw_params` until non-`closed`; (3) on parsed values differing from the
  running session's (or no session yet), tear down and reopen against the
  *user-selected* `pcm_device`; (4) keep polling for the process's life, so
  Live restarting mid-session with different params is handled continuously,
  not just once at startup.
- Preserve the existing real-time discipline exactly per new session:
  `runtime.LockOSThread()` and `bridge_set_realtime(50)` are per-thread, must
  be reapplied on every new render goroutine, not just the first — call this
  out explicitly during implementation, since silently dropping it would
  regress the glitch-on-held-notes fix already documented in `main.go`.

**B4 — on-screen I/O picker**, new `hacks/push-braids-host/src/iopage.go` +
edits to `params.go`/`display.go`/`chord.go`. Reuse the existing Shift+Device
page system (D-Pad Left/Right already switches pages 1–2) — add page 3
("I/O") rather than a new chord/mode. New `pageKind` (`pageKindParams` /
`pageKindIOPicker`) since a picker is a list, not eight knobs; render via
existing `core/gfx/widgets.RenderList`/`ListView` (already used by
push-manager's file browser — no new widget code). Two stacked lists: MIDI
input ports (`alsaseq.EnumPorts`, excluding push-braids-host's own port) and
PCM playback devices (new `core/alsapcm` package parsing `/proc/asound/cards`
+ offering `hw:<id>,0/1,X` pairs — put in `core/` now rather than hack-local,
per explicit decision to build ahead of a second consumer since more
audio-I/O-selecting hacks are anticipated; ships in the same `core` version
bump as the `SetMidiFilter` tag below). D-Pad Up/Down moves cursor, existing
page-nav toggles list focus, `CCSelect` commits + persists to
`braids-config.json`.

**B5 — applying a change**: no live ALSA-handle swap (same race-avoidance
reasoning as B3's rejected alternative — this codebase already has one
documented cross-goroutine-race bug from exactly this class of shortcut).
- PCM device change reuses B3's session-restart path directly (a device
  change is treated identically to a hw_params change).
- MIDI port change gets its own small `midiSession` wrapper
  (subscribe/close), independent of the PCM session, torn down/recreated on
  commit.
- A few hundred ms of dropout during a user-initiated, rare picker action is
  an acceptable cost versus reintroducing a race.

**B6 — new `hacks/push-braids-host/hack.json`**: no `port` — this hack is
HTTP-client-only (`core/pmclient`) today and stays that way per the user's
explicit "no new HTTP server" constraint.

**B7 — crash respawn**: `main()` checks an env var (`PBH_SUPERVISED=1`); if
unset, re-execs itself as a child and loops (wait, log exit, capped
exponential backoff, respawn forever) — pure-Go supervisor parent, all
cgo/dlopen/ALSA risk isolated in the child. Flag as a follow-up worth doing
properly at the framework level (`push-catalog.sh` gaining real supervised
respawn) — out of scope here, don't fold into this change.

**B8 — boot-settle**: add `alsaseq.WaitForBootSettle()` at the top of the
supervised child's startup, before MIDI subscribe or first `bridge_pcm_open`.

## Part C — separate-repo split + catalog entries

Two new repos, `federico-pepe/push-hack-audio-loopback` and
`push-hack-braids-host`, same shape as `push-hack-keyboard-visualizer`
(`hack.json`, `release.json` committed by CI, `Makefile`, `.github/workflows/release.yml`,
`src/go.mod` requiring `github.com/federico-pepe/ableton-push-hack/core vX.Y.Z`
with **no** `replace` directive).

**Release workflow must diverge from `PUBLISHING.md`'s documented recipe**:
cgo needs the Docker build step (`docker run ... golang:1.25-bookworm ...
CGO_ENABLED=1 go build ...`) instead of `ubuntu-latest` +
`CGO_ENABLED=0`. Copy `push-hack-keyboard-visualizer`'s workflow verbatim and
swap only the build step. push-audio-loopback's packaging step also includes
`ko/` and `loopback_feed` in the tarball. Worth a short "cgo hacks" note added
to `catalog/PUBLISHING.md` afterward so the next cgo hack author doesn't hit
the same dead end — small follow-up, not blocking.

**`catalog/catalog.json`** gets two new entries; `push-braids-host` declares
`"requires": ["push-audio-loopback"]`. Both hacks' descriptions/READMEs must
state plainly that `requires` only orders install, not boot-time start order
— B3's own runtime wait loop is what actually makes boot order safe.

## Sequencing

1. **Core version bump** — tag a new `core/vX.Y.Z` including `SetMidiFilter`
   (already merged, untagged) and the new `core/alsapcm` package (B4).
   Blocking prerequisite for Part C.
2. **push-audio-loopback persistence** (Part A), hand-deployed via SSH first
   (bypassing catalog) to validate insmod/vermagic/monitor-loop logic against
   real hardware in isolation.
3. **push-braids-host persistence** (B1–B3, B7, B8), still hand-deployed,
   validated against phase 2's already-persistent loopback card, before
   touching UI.
4. **On-screen I/O picker** (B4–B6) — purely additive once B3's session-restart
   plumbing exists.
5. **Repo split** (Part C) for both hacks, once 2–4 are hardware-validated in
   this monorepo — mechanical extraction against already-proven code.
6. **Catalog entries** (`catalog.json` PR) only after each repo's first
   tagged release is confirmed installable via `push-catalog install <id>` on
   real hardware.

## Verification

**Covered by `go vet`/unit tests (no hardware)**: `parseHWParams` fixture
tests, vermagic-extraction pure function, `braids-config.json` round-trip
(`t.TempDir()`), `core/alsapcm`'s `/proc/asound/cards` parser (own package
test, following `core/alsaseq/ports_test.go`'s fixture pattern), `go build ./...`
+ `gofmt -l` for both new Go trees. Check whether
`hacks/push-catalog/testdata/fixture-hack.tar.gz`'s `--self-test` already
exercises a `requires` chain before assuming a new fixture is needed for C3.

**Requires real Push 3 hardware** (no local ALSA-seq/kernel-module/USB-A
emulation exists): all of Part A's module load/idempotency/vermagic-refusal
behavior; `timer_source=A3.0.0` glitch behavior; B3's dynamic negotiation
against Live's actual runtime values and the mid-session Live-restart case;
B7's respawn loop (kill -SEGV the child, watch backoff/relaunch in logs); the
on-screen picker (real encoder/D-Pad/screen); end-to-end catalog
install/update/remove with `requires` auto-installing the dependency.

**Most important single test**: full `poweroff` + manual power-on (never a
warm `reboot` — CLAUDE.md's documented gotcha: the USB-A wedge only
reproduces cold) — after that cold cycle, with zero manual SSH steps, Live's
loopback track should route real audio through Braids again. This is the
entire point of Parts A/B.

## Status (2026-09-05)

All phases done and hardware-validated:

- **Part A** (push-audio-loopback persistence): done, cold-reboot tested.
- **Part B** (push-braids-host persistence, B1-B3/B7/B8): done, cold-reboot
  tested alongside Part A.
- **B4-B6** (on-screen I/O picker): done, tested live — MIDI port switch,
  audio device, and an added channel-pair picker (not in the original
  plan, requested after testing revealed the render loop always wrote to
  channels 1-2 regardless of what Live's track was set to read).
- **Part C** (repo split + catalog entries): done —
  [federico-pepe/push-hack-audio-loopback](https://github.com/federico-pepe/push-hack-audio-loopback),
  [federico-pepe/push-hack-braids](https://github.com/federico-pepe/push-hack-braids)
  (renamed from `push-braids-host` at the user's request when splitting
  out — monorepo copies under `hacks/` keep the old id, dev-only).
  `catalog/catalog.json` updated.

Two fixes found only through real hardware use, not in the original plan:
- The crash-respawn supervisor didn't forward signals to its child,
  leaving it orphaned on a service "stop" (`ETXTBSY` on the next deploy).
- push-audio-loopback's two PCM devices read as identical entries in
  Live's own device picker; `aloop-rename.patch` now names them distinctly.

Not done / deferred: the Enable/Disable toggle for Push Hack Catalog
(discussed, judged small and self-contained, deferred to a later session).

## Critical files

- `hacks/push-catalog/push-catalog.sh` (`install_service`, confirms the
  central finding)
- `hacks/push-braids-host/src/main.go`, `display.go`, `params.go`
- `hacks/push-audio-loopback/README.md` (current manual recipe to replace)
- `core/alsaseq/bootsettle.go`, `core/alsaseq/ports.go`
- `catalog/PUBLISHING.md`, `catalog/schema.md`, `catalog/catalog.json`
