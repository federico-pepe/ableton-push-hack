# Live devices from hacks

A hack can show its sound engines in Live as normal devices. The user finds
them in Push's browser, edits them in Push's device view, plays them with the
pads, saves them with the Set and automates them. The engine still runs in the
hack's own process, not in Live's. If the hack crashes, Live goes on and the
device plays silence.

The layer is `core/livedevice`. Pung (`push-hack-pung`, which runs Schwung modules) is the first
hack that uses it. Design and measurements:
[plans/2026-11-14-live-devices.md](../plans/2026-11-14-live-devices.md).

## How it works

```
Live                                    the hack's process
+-------------------------------+       +----------------------------------+
| a track: [<Hack> Device] ...  |       | livedevice.Server                |
|   the livedevice plugin       | shm   |   one realtime thread per device |
|   (VST3), one per device      | <---> |   Provider.New(class) -> Device  |
|                               | sock  |   Device.Process(block)          |
+-------------------------------+       +----------------------------------+
```

- **The plugin** is one VST3 binary for every hack:
  `core/livedevice/vst3/livedevice.so` (C++, on Steinberg's VST3 SDK, which is
  MIT). A hack installs it as `/data/.vst3/<Name>.vst3`, with a
  `livedevice.json` that names the hack's socket and class file. Live sees one
  plugin class per device the hack describes.
- **Each audio block** goes through shared memory (`/dev/shm/<hack>-dev-<n>`):
  MIDI in, parameter changes, the transport, audio in. The hack answers with
  audio out and MIDI out. The plugin waits at most half a block. When the
  answer is late, the plugin plays silence for that block. Live never waits.
- **The control socket** creates and closes devices and carries their state.
  When Live quits or crashes, the socket closes and the hack frees the devices.

## Use it in a hack

```go
import "github.com/federico-pepe/ableton-push-hack/core/livedevice"

// 1. Describe the devices.
c := &livedevice.Class{Name: "My Synth", Kind: livedevice.Instrument, Ref: "synth",
    Params: []livedevice.Param{
        {Name: "Cutoff", Type: "float", Max: 1, Unit: "%", Default: 0.8},
        {Name: "Wave", Type: "enum", Options: []string{"Saw", "Square"}, Max: 1},
    },
    Banks: [][]int{{0, 1}}}
c.UID = livedevice.ClassUID("my-hack", c.Kind, c.Ref) // never change it: Sets save it
livedevice.PadBanks(c)
classes := []*livedevice.Class{c}

// 2. Install the plugin, the class file (in the bundle) and the browser entries.
so, cfg, _, _ := livedevice.InstallBundle(livedevice.VST3Dir, "MyHack", "livedevice.so",
    livedevice.BundleConfig{Vendor: "My Hack", Socket: sock,
        Presets: livedevice.PresetFolders(livedevice.UserPresets, "My Hack", classes)})
livedevice.WriteClasses(cfg.Classes, so, classes)
livedevice.WritePresets(livedevice.UserPresets, "My Hack", classes)

// 3. Serve the devices.
srv := livedevice.NewServer(livedevice.Config{Name: "my-hack", Socket: sock}, myProvider{}, classes)
srv.Listen()
mux.Handle("GET /api/devices", srv.Handler()) // optional: what each device did
```

A `Provider` makes one `Device` per instance in Live. `Device.Process` gets a
`Block`: `Events` (MIDI in), `Params` (changes, normalized 0..1), `In` and
`Out` (planar float audio), the transport, and `Emit` (MIDI out). `Process`
runs on a realtime thread at Live's own audio priority. It must not block,
allocate much, log or touch files. `State` and `SetState` are what Live saves
in the Set.

Put `"live_devices": "MyHack"` (the bundle name) in the hack's `hack.json`.
When the user removes the hack, push-catalog then removes its browser
entries. It keeps the bundle and the class file in it, so a Set that uses
the hack's devices still opens: the devices play silence instead of being
missing plugins. See [Remove a hack](#remove-a-hack).

A hack ships `livedevice.so` from the `core` version it builds with:

```sh
cp "$(go list -m -f '{{.Dir}}' github.com/federico-pepe/ableton-push-hack/core)/livedevice/vst3/livedevice.so" .
```

The small example hack `core/livedevice/cmd/ldexample` has all three useful
kinds and runs on the Push as it is.

## Device kinds

| Kind | Buses | In Push's browser |
|------|-------|-------------------|
| `Instrument` | MIDI in, audio out | an Instrument Rack in *Instruments/<folder>* |
| `Effect` | audio in and out, MIDI in | an Audio Effect Rack in *Audio Effects/<folder>* |
| `MIDI` | MIDI in, MIDI out, silent audio out | an Instrument Rack. Another track takes its notes with *MIDI From* |
| `MIDIEffect` | MIDI in and out, no audio | none: see below |

**MIDI effects.** Live on Push has no slot for a MIDI effect from another
maker. Live 13.0b25 scans a VST3 with MIDI in, MIDI out and no audio as an
audio effect, as Live 12 does. Use the `MIDI` kind: put the device on its own
track, and on the track it should play, set *MIDI From* to
`<that track>` and `<rack> | <device> | <plugin>`. This drives Live's own
instruments too.

## Your view instead of Push's device view

A hack can also draw its own view of its devices on Push's screen (a Push
Hack Home app, `surface` in `hack.json`). With `"device_view": true` in
`surface` and `live_devices` set, push-manager shows that view instead of
Push's device view whenever one of the hack's devices is selected, and the
user switches it per app (Home's `DEVICE` button). Device View, pressed in
it, goes to Push's own device view and back. Details: push-manager's
README, "The device view".

## Rules that come from measurements

All measured on a Push 3 with Live 13.0b25 on 2026-11-14.

- **Live scans plugins only when it starts.** A changed class file needs a
  Live restart. `WriteClasses` touches the plugin binary, so Live scans the
  bundle again at that start.
- **Banks.** Push shows a plugin's mapped parameters 8 at a time and names the
  pages "Bank 1", "Bank 2", and so on. To see the list, press the selected
  device's button above the screen. `PadBanks` fills each bank to whole pages
  with placeholder parameters, so your bank N is Push's Bank N. A placeholder
  shows as an empty slot.
- **List each parameter once.** Live keeps a separate value for every entry
  in a rack's parameter list. A parameter listed twice shows a stale value in
  one of the two places. `PadBanks` and `WritePresets` make sure of it.
- **Defaults are real.** When Live loads a device, it sends the plugin's
  default values to it. If a `Param.Default` is not the engine's real starting
  value, the device loads with that value instead (a synth at volume 0).
- **Do not remove a plugin that a Set has used.** When Live recovers a Set
  after a crash, it replays the Set's history. If a plugin in that history is
  gone, Live 13.0b25 crashes while it restores the Set.

## Remove a hack

`push-catalog remove` deletes the hack and its browser folders (the
`presets` list in the bundle's `livedevice.json`). The plugin bundle
`/data/.vst3/<Name>.vst3` stays, with its class file. Delete it by hand only
when no Set uses the hack's devices any more, and not just before Live
restarts after a crash: Live 13.0b25 crashed when it recovered a Set whose
plugin was gone.

## Test

- `make test` runs the Go tests. They cover classes, banks, the block layout
  (it checks the C header against the Go numbers), the browser entries, and a
  fake plugin that talks to a real server.
- `core/livedevice/vst3/build.sh` builds `livedevice.so` in Docker
  (Ubuntu 22.04, the same glibc as the Push). Then it runs Steinberg's
  validator and a playtest of each example device against `ldexample`.
  `build.sh test` checks the committed binary without building it.
- On the Push, `core/livedevice/tools/restart-live.sh` (as root) restarts Live
  with no one at the Push: Live saves its preferences, stops, and the Push's
  launcher starts it again. The script answers Yes when Live asks "Would you
  like to recover your work?". It never answers when Live says it crashed
  again while recovering: then it stops and someone decides on the Push.

## Not done

- Push names plugin banks "Bank N". A hack cannot give them names.
- The plugin's parameter list is fixed when Live loads the bundle.
- A `MIDIEffect` class waits for a Live that hosts MIDI-effect plugins.
