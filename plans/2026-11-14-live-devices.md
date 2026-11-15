# Live devices from hacks (`core/livedevice`)

Status: built 2026-11-14, first user: Pung (`push-hack-pung`, then named Schwung).

## Goal

A hack can show its sound engines in Live as ordinary devices: found in Push's
browser, edited in Push's device view, played by Push's pads, saved with the
Set, automated. The engine itself stays in the hack's own process, out of
Live's, so a crash in a hack cannot take Live down.

## Why in core

The Pung hack built this first (2026-11-14). Nothing in it is about
Pung: a VST3 plugin that hands each block to another process, the block
protocol, a realtime loop, a control socket, device classes with parameters
and banks, browser entries for Push. Any hack that makes sound or MIDI
(Stacks, Riffs, a future one) can use the same layer. One copy, in core,
tested on its own.

## The parts

```
Live                                    the hack's service
+-------------------------------+       +----------------------------------+
| track: [<Hack> Device] ...    |       | livedevice.Server                |
|   livedevice.so (VST3 proxy)  | shm   |   one realtime loop per device   |
|   one block: events, params,  | <---> |   Provider.New(class) -> Device  |
|   input -> output, MIDI out   | sock  |   Device.Process(block)          |
+-------------------------------+       +----------------------------------+
```

- `core/livedevice` (Go, no cgo): classes (`Class`, `Param`, `PadBanks`,
  `ClassUID`), the class file, the block protocol (`Block`), the server
  (control socket, one realtime goroutine per device, stats), the plugin
  bundle (`InstallBundle`) and the browser entries (`WritePresets`).
- `core/livedevice/vst3`: the proxy, C++ on the VST3 SDK (MIT). Built in
  Docker, committed as `livedevice.so` (like `push_hook.so`). One binary for
  every hack: a hack installs it as `<Name>.vst3` with a `livedevice.json`
  next to it that names its socket and class file.
- `core/livedevice/cmd/ldexample`: a small provider (a sine instrument, a
  gain effect, an octave MIDI device) that tests the whole path without any
  hack.
- `core/livedevice/tools/restart-live.sh`: restarts Live with no one at the
  Push, for tests (Live scans plugins only at start).

## Device kinds

| Kind | Buses | In Push's browser |
|------|-------|-------------------|
| `instrument` | MIDI in, audio out | an Instrument Rack in *Instruments/<folder>* |
| `effect` | audio in and out, MIDI in | an Audio Effect Rack in *Audio Effects/<folder>* |
| `midi` | MIDI in, MIDI out, silent audio out | an Instrument Rack; other tracks take its notes with *MIDI From* |
| `midifx` | MIDI in and out, no audio | none: Live 13.0b25 files it as an audio effect |

## Facts measured on the device (2026-11-14, Live 13.0b25)

- Live scans plugins only when it starts. Touching the `.so` makes it scan the
  bundle again.
- A MIDI-in/MIDI-out VST3 with no audio is scanned as `audiofx`. Live has no
  slot for a third-party MIDI effect. The `midi` kind is the way: Live offers
  its output as *MIDI From: <track> / <rack> | <device> | <plugin>*.
- Push pages a plugin's mapped parameters 8 at a time ("Bank 1", "Bank 2",
  ...). `PadBanks` fills every bank to whole pages with placeholders, so a
  hack's bank N is Push's Bank N.
- A read-only parameter is shown as "Parameter #N 0.12" whatever its title. A
  placeholder is a plain parameter, title and value a no-break space, value
  held at 0.
- A parameter listed twice in a rack gets two independent values in Live.
  List each once.
- Live pushes the plugin's default values into it when a device loads. A
  class must carry the engine's real starting values, or a synth loads with
  volume 0.
- Recovering after a forced stop: Live asks "recover your work?" on the Push.
  `restart-live.sh` answers Yes by sending the press of the button under
  "Yes" (CC 27) to Push3's MIDI input, only after Live's log shows the
  question.

## Removing a hack

The class file lives inside the plugin bundle (`ClassesPath`). `hack.json`'s
`live_devices` names the bundle; `push-catalog remove` deletes the browser
folders listed in its `livedevice.json` and keeps the bundle, so a Set that
uses the hack's devices still opens (silent devices) instead of missing a
plugin. Live 13.0b25 crashed recovering a Set whose plugin had been removed
(measured 2026-11-14).

## The device view (push-manager)

An app with Live devices stands in for Push's device view (`hack.json`
`surface.device_view`, on by default, switched per app with Home's
`DEVICE` button or Display → Tabs). push-manager opens it while Push shows
its device view and the selected device is one of the app's, and closes it
when either stops (`devview.go`, `Surface.Follow`).

- Push's view comes from Push3's own lights: it lights the active view
  button 127, the others 14, and sends its lights to every subscriber of
  its output port ("Ableton Internal Output"). push_hook is not touched.
  Live's own views cannot tell: Mixer and Device View both show
  `Detail/DeviceChain`. Push's script runs in the Push3 process, out of
  the Remote Script's reach.
- 0 is a repaint: when Push3 gets the buttons back it sets every view light
  to 0 and then again. Read as "another view" it reopened the app at once.
- Open on the light, not on the press: an app taking the buttons before
  Push3 read the same press left Push in its old view.
- Device View in the app: Push's own device view, held until the selection
  changes or Push leaves its device view; Device View again brings the app
  back. Mixer, Clip, Session View, Add, Swap, Settings, Set, User, New: the
  app closes and the press goes on to Push. Tracks: the app's lower row and
  arrows select them in Live (as on Push's device view); Follow keeps it on
  a device of the app and closes it on any other. The app learns it stands
  in from its focus event (`pmclient.FocusInfo.DeviceView`).
- Tested on the device: `push-hack-pung/tests/device/device_view_test.py`
  (23 steps through the real Push, Live and push-manager).

## Not done

- A `midifx` class would need a Live that hosts MIDI-effect plugins.
- Bank names: Push names plugin banks "Bank N"; a hack cannot name them.
- The plugin's parameter list is fixed when Live loads the bundle. A class
  that changes needs a Live restart.
