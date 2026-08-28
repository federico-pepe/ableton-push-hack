# A Schwung-style DSP host for Push 3

**Status: planning only. Nothing in this file is built yet — see
"Phased path" at the bottom for what to build, and in what order,
when this moves forward.**

## Context

`hacks/push-braids-host` (working, confirmed on hardware) proves the
core technical chain: pad MIDI in, a Move Anything `plugin_api_v2` DSP
module (Braids), audio out through `hacks/push-audio-loopback`'s
virtual sound card. But it conflates two things that should be
separate: "the thing that owns hardware I/O" and "the thing that runs
Braids specifically." Every future DSP module would otherwise have to
re-implement its own MIDI subscription, its own PCM open, its own
render loop — exactly the duplication Schwung itself avoids on Move.

Federico asked to mirror how Schwung actually works: one host owns
hardware, modules are DSP plugins the host loads, no module talks to
hardware directly. This file researches what Schwung actually does
(`~/Developer/schwung-main`) and proposes a Push 3 equivalent. Per
Federico's explicit request, this is a planning pass — nothing here
should be built until this plan is reviewed and a phase is chosen to
start.

## What Schwung actually does

Researched directly from `~/Developer/schwung-main`'s source and
docs (`docs/ARCHITECTURE.md`, `docs/MODULES.md`,
`src/modules/chain/README.md`).

### Track/slot model

Schwung runs **4 fixed slots** (`SHADOW_CHAIN_INSTANCES` /
`SHADOW_UI_SLOTS`, `src/schwung_shim.c:1813`), one per Move hardware
track. A slot is just an index — what runs there is whichever Signal
Chain patch is currently loaded into that slot's chain instance. A
separate, independent set of **Master FX slots** (4, soon 8) processes
the *summed* output of all 4 slots, not any one track.

### Module chaining

Each slot holds an **ordered, serial list**, not a graph:

```
MIDI source -> MIDI FX (1..8) -> Sound Generator -> Audio FX (1..8) -> slot output
```

Rendering is one function call per module per block
(`synth->render_block()`, then each FX's `process_block()` called
serially, output of FX(i) feeding FX(i+1) in place on the same
buffer) — see `chain_host.c:1898-1994`. A "patch" is a JSON file
naming which module goes in each chain position plus its saved
parameters (`src/patches/linein_freeverb.json` is a real example
in the repo). Reordering the chain is a permutation of existing
per-position state, not a teardown/reload, specifically so a running
module's internal state (envelope phase, delay taps) survives a chain
edit.

### Module manifest and installation

Every module is a directory with a `module.json` manifest
(`id`, `name`, `version`, `api_version`, `capabilities.component_type`
— `sound_generator`/`audio_fx`/`midi_fx`/`utility`/`tool`/`overtake`)
plus its `dsp.so` (or, for chain-loaded audio FX specifically,
`<module-id>.so` — a hardcoded naming exception `chain_host.c`
relies on). Modules are discovered by **directory scan** under
category subfolders (`modules/sound_generators/<id>/`,
`modules/audio_fx/<id>/`, etc.) — no host recompile needed to add one.

A companion Go web app, `schwung-manager` (serves
`http://move.local:7700`), is the actual installer: a root
`module-catalog.json` (119 entries) lists every known module with its
GitHub repo; each module repo publishes its own `release.json`
resolving to a versioned download tarball. Install = download, extract
into the right category directory, done — no dependency graph between
modules, no code signing (trust = "it's a GitHub release from a known
repo," stated explicitly in Schwung's own docs).

### Audio mixing

All-int16-with-int32-accumulation, hard-clamped. Per-slot: synth
renders, its FX chain runs serially in place, the result is scaled by
that slot's volume/fade and summed into a running bus. That bus then
runs through Master FX (same serial-chain pattern), and only at the
very last step is it summed into Move's own audio at Move's own
volume — deliberately structured so Schwung's mixing never perturbs
Move's own stock levels.

## What ports directly vs. what needs Push3-specific redesign

**Ports directly, already proven or trivially portable:**
- The `plugin_api_v2_t` DSP contract itself — validated end to end
  with Braids on real Push 3 hardware.
- The `module.json` manifest shape and category-directory convention.
- The serial chain-rendering pattern (`for fx in chain:
  fx->process_block(buf)`) — plain, hardware-agnostic C.
- The catalog.json + release.json GitHub-based install contract, if
  Push3 ever gets a module store — no Move-specific assumptions in
  that design.

**Needs real redesign, not just reuse:**
- **The "4 slots" constant** is tied to Move's 4 physical hardware
  tracks. Push 3 has no equivalent built-in track topology — this
  needs its own decision (see "Open questions" below), not a copy of
  Schwung's number.
- **Realtime constraints** (`SCHED_FIFO 90`, ~900µs/128-frame budget,
  no malloc/file I/O/dlopen on the audio thread) are properties of
  Move's SPI-callback audio model. Push 3's actual constraints are
  different and only partly measured so far — see
  `docs/push3-dsp-hosting.md`'s write-latency findings (a ~10ms fixed
  floor tied to this kernel's 4ms jiffies clock, not present on
  Move's hardware timer model at all).
- **Module install/management.** Schwung's `schwung-manager` is a
  purpose-built companion app. Push 3 already has an installed,
  working web app for exactly this kind of job —
  `hacks/push-manager` (`http://push.local:7701`) — so the open
  question isn't "build a new installer," it's whether module
  management belongs as a new page/section inside `push-manager`
  (reusing its existing server, auth-less-but-trusted-network
  posture, and deployment path) or as a genuinely separate service.
  Leans toward extending `push-manager`, but not decided here.

## Proposed Push3 host shape (not yet built)

A single long-running process, structurally mirroring
`schwung_shim.c`'s role but built from what this project already has
proven:

- **Hardware ownership**: one ALSA MIDI subscription to Push3's Live
  Port (`core/alsaseq`, already used by `push-manager`/`automation`/
  `keyboard-visualizer`/`push-braids-host`), one ALSA PCM connection
  to `hacks/push-audio-loopback`'s virtual card (the `bridge.c`
  pattern already built and proven in `push-braids-host`).
- **Module loading**: directory-scan discovery of `module.json` +
  `dsp.so` pairs, `dlopen`/`plugin_api_v2` — the exact mechanism
  `push-braids-host`'s `bridge.c` already implements for one
  hardcoded module, generalized to scan a directory instead of
  taking a fixed path.
- **Chain rendering**: reuse Schwung's serial-render pattern
  directly — it's plain C, no Move dependency. One slot to start
  (see phased path below), extending to N slots and FX chaining
  later.
- **Realtime discipline**: `runtime.LockOSThread()` +
  `SCHED_FIFO`, already validated in `push-braids-host` — carries
  forward unchanged.

## Open questions to resolve before real design work starts

- **How many slots, if any, for Push3?** Move's 4 comes from its 4
  physical tracks; Push 3 has no equivalent fixed hardware concept.
  Options: a fixed small number chosen arbitrarily (mirroring
  Schwung's convention for familiarity), one slot per MIDI channel
  (16, probably too many to be useful), or start at 1 and treat
  multi-slot as a later phase once there's a real second module to
  justify it.
- **Where does module management live?** Extend `push-manager`
  (`:7701`) with a modules page, or build a separate service. Affects
  how much of `schwung-manager`'s actual code (catalog fetch,
  install/uninstall, tarball extraction, `config.json`/`secrets`
  snapshot-restore) is worth adapting versus reimplementing.
- **Local module source vs. a real catalog.** Schwung's
  `module-catalog.json` assumes a public GitHub-release ecosystem of
  many third-party modules. Push3's equivalent doesn't exist yet — an
  early phase probably just needs "drop a module directory in place,"
  with catalog/remote-install as a much later addition once there's
  more than one or two modules worth cataloging.
- **The ~10ms write-latency floor** (`docs/push3-dsp-hosting.md`)
  is a per-process cost today (one PCM connection, one host). Whether
  it changes shape once one host is mixing several chained modules
  through one PCM connection (same connection, more CPU work per
  block, same floor) needs re-measuring once there's a second module
  to test with, not assumed to already be understood.

## Phased path (for when this moves forward)

1. **Split, don't add features.** Turn `push-braids-host` into a
   generic single-module host: same MIDI/audio/module-loading code,
   but `bridge_plugin_load`'s `.so` path becomes a directory-scan
   result instead of a CLI argument, with the directory scan
   currently expecting exactly one module. No chaining, no multi-slot
   yet — this phase is purely "stop hardcoding Braids," which is
   almost free given how `push-braids-host` is already structured.
2. **Multi-slot**, once a second real module exists to justify it —
   resolve the "how many slots" open question above with a concrete
   second module in hand, not in the abstract.
3. **Audio FX chaining** within a slot, reusing Schwung's serial
   `process_block()`-in-place pattern directly.
4. **Module management** — resolve the push-manager-extension
   question, then build whichever path that resolves to.

Each phase should get its own plan file when it actually starts,
per this repo's own `CLAUDE.md` convention — this file stays as the
research and design reasoning behind all of them, not a spec for any
one phase.
