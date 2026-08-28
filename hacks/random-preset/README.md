# Random Preset

Two Push 3 chords that drop something random onto the selected track:

- **Shift + Add** (CC49+CC32) → a random **preset** (prefers Instruments)
- **Shift + Swap** (CC49+CC33) → a random **drum rack kit**

A tiny background hack — no screen, no web UI — it watches Push 3's hardware MIDI
for the chords.

## How it works

1. Subscribes (read-only) to Push 3's ALSA port, like keyboard-visualizer — never
   disturbs push-manager's MIDI.
2. On a chord (debounced 500ms per button), GETs `push-manager /api/presets` with
   the matching filter, picks one at random.
3. POSTs `push-manager /api/live/load` (→ **browser-bridge**, the only thing that
   can instantiate in Live).

**Drum rack filtering:** `filter=Drums&rack=rack` returns both full kits *and*
single-sound racks (individual hits live under `Drum Hits`/`Drum Cell` folders).
The hack drops those, so Shift+Swap always loads a full **Drum Rack kit**, never a
lone drum sound.

## Requires

- **push-manager** — preset index + `/api/live/load`.
- **browser-bridge** — with the **PushHackBrowser** remote script activated in Live
  (one-time manual step; see that hack's README). Without it, loads fail and the
  reason is logged to `/data/push-hack/logs/random-preset.log`.

## Install

Via the Push Store (browse → Install), or through the framework:

```sh
cp -r hacks/random-preset push-hack/hacks/random-preset
cd push-hack && ./scripts/install.sh --hack random-preset --build
```

Then hold **Shift + Add** with a track selected. (Building outside the framework
needs `core`; drop this into the framework's `hacks/` so `replace ../../../core`
resolves — same as every other hack here.)

## Test

```sh
cd src && GOOS=linux GOARCH=amd64 go test ./...   # chord state machine (linux-only pkg)
```
