# PushHack Remote Script

PushHack is one Remote Script for every hack that needs Live. It ships with Push Hack. Hacks talk
to it over a local socket. It runs each request on Live's engine thread.

Source: `remote-script/PushHack/`. Tests: `make test-remote-script`. Design and decisions:
`plans/2026-10-27-pushhack-shared-remote-script.md`.

## Set up

Select **PushHack** in a control-surface slot in Live (Preferences, Input and Output: None) and
restart Live. Push Manager's System page shows if it runs.

## Connect

| Where | Address |
|---|---|
| Push, macOS, Linux | Unix socket `/tmp/push-hack.sock` (owner only) |
| Any system | TCP `127.0.0.1:7704` |

One JSON object per line, both ways. A client may keep the connection open (to subscribe) or use
one connection per request.

```
-> {"id":1,"ns":"transport","cmd":"get"}
<- {"id":1,"ok":true,"data":{"playing":false,"tempo":120.0, ...}}
-> {"id":2,"ns":"transport","cmd":"nope"}
<- {"id":2,"ok":false,"error":"unknown_cmd"}
```

- `id` is yours; the reply repeats it. `ns` is the namespace. `cmd` is the command. Other keys are
  the arguments.
- A failed request gets `"ok":false` and a short `error` code. A bad line never drops the client.
- Commands with no `ns` are built in: `hello`, `subscribe`, `unsubscribe`.

### hello

`{"id":1,"cmd":"hello"}` returns `protocol`, `script_version`, `namespaces` and `topics`. Check
`protocol` (now 1) and that the namespaces or topics you need exist.

### subscribe

```
-> {"id":2,"cmd":"subscribe","topics":["tempo","selection"]}
<- {"id":2,"ok":true,"data":{"topics":["selection","tempo"]}}
<- {"t":"event","topic":"tempo","data":{"tempo":120.0}}      (now, and on every change)
```

An event is sent when the value changes. A topic is not recomputed every tick: each has a minimum
interval, and the script waits longer after a slow build, so a big Set costs Live little.

### Old clients

The old Browser Bridge text commands (`ping`, `get_tempo`, `load:...`) and the old arrangement socket
`/tmp/push-hack-arrangement.sock` were removed in script 0.4.0. A client whose first byte is not `{` gets one
line, `{"ok":false,"error":"old_protocol"}`, and the script closes the connection. Use the JSON protocol.

## References

| What | How to name it |
|---|---|
| track | An index into the Set's tracks, `"return:N"`, `"master"` or `"selected"` |
| scene | An index |
| clip | `"track"` and `"scene"` (the session slot), or `"detail": true` for the clip in the detail view |
| device | `"track"` and `"device"` (an index), or `"path"`: `[device, chain, device, ...]` to go into racks |

Volume is the raw mixer value, 0 to 1 (0.85 is 0 dB). Pan is -1 to 1. Parameter values are raw
(between `min` and `max`); `display` is Live's text for the value.

## Error codes

An error reply may carry `data` with detail (for example the valid names for `bad_routing`).

`bad_json`, `old_protocol`, `bad_routing`, `wrong_track_kind`, `no_cmd`, `unknown_ns`, `unknown_cmd`, `unknown_topic`, `failed` (Live refused; see
Live's Log.txt), `bad_arg`, `no_changes`, `bad_track`, `bad_scene`, `bad_device`, `bad_param`,
`param_disabled`, `bad_pad`, `bad_view`, `no_clip`, `slot_taken`, `too_big`, `cannot_arm`,
`wrong_clip_kind`, `not_looping`, `not_a_drum_rack`, `not_a_simpler`, `not_sliced`, `not_found`, `not_loadable`,
`nothing_to_undo`, `nothing_to_redo`, `no_cue`, `nothing_to_capture`.

## Namespaces

### transport
`get` (playing, tempo, beat, signature, loop, punch, metronome, record, undo state), `play`, `stop`,
`play_toggle`, `continue`, `set_tempo {v}`, `set_time {v}`, `set_loop {start, length}`,
`set_loop_on {on}`, `set_punch_in {on}`, `set_punch_out {on}`,
`set {metronome, record_mode, session_record, overdub, groove_amount, swing}`, `undo`, `redo`,
`tap_tempo`, `jump_cue {to: "next"|"prev"}`, `capture_midi`.

### track
`list` (tracks, returns, master, selected), `get {track}`, `set {track, name, color, mute, solo, arm,
volume, pan, send: {index, value}, monitoring, fold}`, `select {track}`, `stop {track}`,
`create {kind: "midi"|"audio"|"return"}`, `routing {track}` (the current input and output type and channel, and the names Live offers), `set_routing {track, input_type, input_channel, output_type, output_channel}`.

Routing is set by display name ("Ext. In", "1/2", "Main"; case does not matter). The type is set before the channel, because the channels Live offers depend on the type. A name Live does not offer fails with `bad_routing`, and `data` has `side`, `what`, `want`, `options` (the valid names) and `changed` (what was already set). Master and return tracks have output only. A channel may be a list of names: the first one Live offers is used (for a hack that does not know which pair the user enabled).

`ensure {kind: "audio"|"midi", name, <routing keys>, monitoring}` finds the track called `name`, or creates it, and sets its routing and monitoring (monitoring: 0 In, 1 Auto, 2 Off). It returns `{track, created, changed, routing}`. Calling it again does not create a second track. If a track of that name exists but is the other kind, it fails with `wrong_track_kind`. If the routing fails after the track was created, the new track stays and the next call finds it. This changes the user's Set: call it because the user asked for it, not silently on every start.

### session
`get {track_offset, track_count, scene_offset, scene_count}`: a window of the clip grid. A slot is
`0` (no clip) or `[color, name, state, length]` (state: 0 stopped, 1 playing, 2 queued, 3 recording).
`fire_clip`, `stop_clip`, `fire_scene {scene, as_selected}`, `stop_all`, `create_clip {track, scene,
length}`, `delete_clip`, `capture_scene`, `select_scene`, `select_clip_slot`, `create_scene`,
`duplicate_scene`, `delete_scene`, `set_scene {scene, name, color, tempo}`.

### device
`list {track, path, chain}`, `params {track, device, offset, count}`, `set_param {track, device, index,
value | norm}`, `set_active {on}`, `select`, `selected`, `delete`, `move {to_index, to_track}`,
`chains`, `set_chain {chain, mute, solo, name}`.

### clip
`get`, `set {name, color, looping, muted, loop_start, loop_end, start_marker, end_marker,
launch_mode, launch_quantization, legato, velocity_amount (MIDI), gain, pitch_coarse, pitch_fine,
warping, warp_mode (audio)}`, `fire`, `stop`, `quantize {grid, amount}`, `crop`, `duplicate_loop`,
`notes {from_pitch, pitch_span, from_time, time_span}`, `add_notes {notes: [{pitch, start, duration,
velocity, mute}]}`, `remove_notes {same range}`, `convert_options`,
`convert {kind: "harmony"|"melody"|"drums"|"simpler"|"drum_rack"}` (audio clips). `harmony`, `melody` and
`drums` make a new MIDI track with a MIDI clip; `simpler` and `drum_rack` make a new track with a Simpler or a
Drum Rack. Track indexes after the source shift by one.

### notes
`scale`, `set_scale {root, name, mode}`, `drum_pads {track, device, filled}` (`filled: true` lists only
pads that hold a chain), `set_drum_pad {note, mute, solo}`, `select_drum_pad {note}`,
`pad_to_track {note}` (a new MIDI track with that pad's chain, inserted after the source track),
`slices` (Simpler), `set_simpler {playback_mode: 0 classic | 1 one-shot | 2 slicing}`,
`slices_to_drum_rack` (the Simpler must be in slicing mode; it becomes a Drum Rack on the same track).

### browser
`load {name, scope}`, `load_sample {name}`, `load_uri {uri}`, `load_plugin {plugin, preset}`,
`list_plugins`, `dump {root, depth}`. A load returns when Live has loaded the item, or why not.

### listen
`get`, `on {port}`, `off`: the silent MIDI track that follows the selected track (used by Stacks).

### arrangement
`snapshot`, `position`, `bta`, `loop_toggle`, `punch_in_toggle`, `punch_out_toggle`.

### modes
`get`, `focus {view}`, `show {view}`, `hide {view}`, `toggle_browse`, `set {follow_song, draw_mode}`.
Views: `Browser`, `Arranger`, `Session`, `Detail`, `Detail/Clip`, `Detail/DeviceChain`.

### meters
`get`: output levels (left, right) for tracks, returns and master.

### debug
`dir {root: "Live"|"song"|"app", path}`: names of Live's Python API (read only). Use it to check that
this Live has a property before you rely on it.

## Topics

| Topic | Data | Interval |
|---|---|---|
| `tempo` | `{tempo}` | every tick |
| `transport` | the `transport.get` data | every tick |
| `position`, `arrangement` | the arrangement snapshot and the playhead position | every tick, about 1 s |
| `scale` | the `notes.scale` data | 5 ticks |
| `tracks` | the `track.list` data | 5 ticks |
| `session` | the `session.get` data (24 tracks x 40 scenes) | 3 ticks |
| `device` | the selected device and its parameters | 2 ticks |
| `selection` | selected track, scene, device, detail clip, focused view | 2 ticks |
| `meters` | the `meters.get` data | every tick |

A tick is about 100 ms (Live calls the script about 10 times a second).

## Example: Braids

[push-hack-braids](https://github.com/federico-pepe/push-hack-braids) uses the script in three ways. Its web page
has a button that calls `track.ensure` twice: once for an audio track named "Braids" (`input_type` `Ext. In`, the
`input_channel` the virtual card uses, `output_type` `Main`, `monitoring` 0) and once for a MIDI track named "Braids
MIDI" (`output_type` `Braids MIDI In`, a port the hack itself opens). If Live offers exactly one stereo pair on
`Ext. In`, it takes that pair and sets its own channel offset to match. It subscribes to `tracks` and `meters` and
shows the meter of the "Braids" track as its Volume fader. The one thing the script cannot do for it is enable the
Push Hack Virtual Audio card in Push's audio preferences.

## What was checked on a Push

Script 0.3.0, Live 12.4.15b7. All reads, and these writes on a test Set: track name, volume and
pan; a device loaded, a parameter set; a clip created, notes added, quantized and removed, a clip
fired; the metronome; view focus; a drum kit loaded with its 16 pads and rack chains (read, mute,
select, a pad moved to a track); a sample loaded into a Simpler, its slices read, slicing mode set and
the Simpler converted to a Drum Rack; an audio clip converted with `harmony`, `melody`, `drums`,
`simpler` and `drum_rack`. Not checked: Windows and macOS Live (the code is the same Python), and a very
large Set (topics have a time budget for it).

## Limits and safety

- A request that would change the Set (clips, notes, devices, tracks) is one Live can undo with
  Live's own undo, but the script does not group them.
- It does not delete tracks.
- `session.get` asks for at most 2000 cells; `clip.notes` returns at most 2000 notes.
- Every Live call runs on Live's engine thread. A hack must not call in a tight loop: use topics.
