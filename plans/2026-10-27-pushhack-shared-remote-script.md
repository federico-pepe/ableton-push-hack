# PushHack: one shared Remote Script

**Status: plan agreed 2026-10-27. Decisions D1 to D4 are settled (section 10). Phase 1 built and confirmed on the device (2026-10-27). Phase 2 built and confirmed on the device. Phase 3 built and confirmed on the device: a bench update that swaps the script folder, and a forced failure that rolls it back (2026-10-27). Phase 4 (the Push 2 feature namespaces) and Phase 5 (moving the other repos) are open; Phase 5 is done for automation, arrangement and the retired Browser Bridge, and still to merge.**

## 1. Goal

Today each hack that needs Live ships its own Remote Script:

| Script | Owner | Transport | Use |
|---|---|---|---|
| `PushHackBrowser` | `push-hack-browser-bridge` | TCP `127.0.0.1:7704`, one text line per connection | load presets and samples, tempo, beat, playing, scale, plugin list |
| `PushHackArrangement` | `push-hack-arrangement` (private) | Unix socket `/tmp/push-hack-arrangement.sock`, TCP `7720` on Windows, JSON lines | arrangement snapshot, position, transport commands |

The user must enable each one by hand in a control-surface slot. Live has few slots.

Build one script, `PushHack`, that:

1. Serves many hacks at the same time.
2. Ships with Push Hack (push-manager) and installs with the self-update.
3. Replaces both scripts above.
4. Exposes the Live features that Ableton's own Push 2 script uses, so a hack author does not write a new script.

## 2. Why one script can serve many hacks

- Live loads one instance of a script per slot. The script owns one socket server. Many clients connect to it.
- `PushHackArrangement` already accepts many clients with non-blocking I/O.
- `PushHackBrowser` already binds the socket once at class level, because Live re-creates the control surface several times at start-up. It keeps a queue that `update_display()` drains on Live's engine thread.
- The Live Object Model is single-threaded. Socket threads never call the Live API. They only queue work.

## 3. Design

### 3.1 Files

```
remote-script/PushHack/                   (the payload; `resources/remote-scripts/Push2` is git-ignored reference only)
  __init__.py          create_instance(c_instance)
  PushHack.py          ControlSurface subclass: tick loop, client list, dispatch
  server.py            non-blocking listeners and clients; json or legacy mode per client
  hub.py               dispatch, subscriptions, change-only events
  legacy.py            old Browser Bridge text commands
  protocol.py          parse/serialize, error replies, version
  registry.py          namespace registration
  ns/transport.py      play, stop, tempo, loop, punch, time signature, count-in
  ns/browser.py        load, load_uri, load_sample, load_plugin, list_plugins, dump
  ns/arrangement.py    snapshot, position, set_time, set_loop, bta
  ns/listen.py         the Stacks listen track (from Browser Bridge 0.4)
  errors.py            CommandError: a failure with a protocol error code
  ns/session.py        tracks, scenes, clip slots, launch, stop, capture
  ns/track.py          select, mute, solo, arm, color, volume, pan, sends, routing
  ns/device.py         selected device, banks, parameters, on/off, move
  ns/clip.py           loop, warp, gain, pitch, quantize, fixed length, convert
  ns/notes.py          scale (phase 2); root note, drum pads, selected drum pad (phase 4)
  ns/meters.py         output levels per track
  ns/modes.py          what the user selected in the Live UI
remote-script/tests/                      pure-Python tests with a fake `song`; no Live import, no real socket path (`make test-remote-script`)
```

### 3.2 Transport

- macOS and Linux: Unix socket, mode `0600`. Windows: TCP `127.0.0.1:7720`. Same as `PushHackArrangement`.
- Push 3 on device: also TCP `127.0.0.1:7704`, because push-manager and Go hacks already use it (`live_bridge.go`). Both listeners feed the same dispatcher.
- Bind once at class level. If a bind fails, log it and keep running with the other listener.

### 3.3 Protocol

One JSON object per line, both directions.

```
-> {"id":1,"ns":"transport","cmd":"get"}
<- {"id":1,"ok":true,"data":{"playing":true,"tempo":120.0,"beat":12.5}}
-> {"id":2,"ns":"browser","cmd":"load","name":"My Bass.adv"}
<- {"id":2,"ok":true}
-> {"id":3,"cmd":"subscribe","topics":["transport","selection"]}
<- {"t":"event","topic":"transport","data":{...}}        (pushed, no id)
```

- `hello`: first message from a client is optional. The reply gives `protocol` and `script_version`. A hack uses it to check that the script is new enough.
- Errors: `{"id":n,"ok":false,"error":"unknown_cmd"}`. A bad line never drops the client.
- Events are sent only when the value changed, at most 10 per second per topic.
- Each client has an output buffer limit. A slow client is dropped, not waited for (`MAX_OUT_BYTES` from the arrangement script).
- A tick time budget protects Live. Big snapshots follow the existing `BUILD_BUDGET_TICKS_PER_SEC` rule.

### 3.4 Legacy shim (kept for one release cycle)

- Port 7704 accepts the old text commands: `ping`, `load:`, `load_uri:`, `load_plugin:`, `load_sample:`, `dump:`, `play`, `stop`, `get_tempo`, `get_beat`, `get_playing`, `list_plugins`. A line that does not start with `{` goes to the shim.
- The arrangement socket path (`/tmp/push-hack-arrangement.sock`, TCP `7720` on Windows) is a separate listener tagged `arrangement`. A client on it gets the old stream without asking, and the new protocol is not used on it. It keeps working with its current `snapshot`, `pos`, `set_time`, `play_toggle`, `bta`, `loop_toggle`, `punch_in_toggle`, `punch_out_toggle`, `set_loop` messages (`t` field), so the current arrangement hack and the PTA module work before they move to the new protocol.
- Remove the shim after both repos and push-manager use the new protocol.

## 4. Feature coverage from the Push 2 script

Source of the list: module and class names in `resources/remote-scripts/Push2/*.pyc` (Python 3.11 byte code, no source). We read names only. We do not copy or decompile that code: it is Ableton's. We build on the public Live Object Model.

| Push 2 area | Modules | PushHack namespace |
|---|---|---|
| Transport state, count-in, signature | `transport_state` | `transport` |
| Session ring, clip slots, scenes, capture and insert | `session_component`, `actions`, `session_ring_selection_linking` | `session` |
| Mute, solo, stop clips, track colors | `mute_solo_stop`, `track_list` | `track` |
| Mixer, sends, routing | `mixer_control_component`, `track_mixer_control_component`, `routing`, `master_track` | `track` |
| Devices, banks, parameters, move | `device_component`, `device_navigation`, `bank_selection_component`, `custom_bank_definitions`, `selected_track_parameter_provider`, per-device files | `device` |
| Automation | `automation` | `device` (step automation parameter read/write) |
| Clip loop, warp, gain, pitch, quantize, fixed length | `clip_control`, `timeline_navigation` | `clip` |
| Convert | `convert` | `clip` |
| Scales, drum group, slicing | `scales_component`, `drum_group_component`, `sliced_simpler`, `simpler` | `notes` |
| Browser | `browser_component`, `browser_item`, `browser_list`, `browser_modes` | `browser` |
| Meters, waveform and playhead data | `real_time_channel`, `routing` | `meters` |
| Mode state | `mode_collector` | `modes` |

Left out on purpose. They are Push 2 hardware or UI work that does not make sense in a data bridge:

- Firmware update and switch (`firmware`).
- QML error pipe and real-time data channel setup for Push 2's own display.
- LED and display brightness, pad sensitivity and velocity curve, hardware settings, setup component, visualisation settings.
- Pad, button and encoder mapping (a hack uses the Push surface through push-manager and `core/`, not through Live).
- Notification component, `user_component` (User Mode is a PTA concern).

Open question for the owner: the Push 3 on device may expose a different set of Live Object Model objects than the Push 2 reference. Each namespace needs a check on the device before it is marked done (section 7).

## 5. Install and update

### 5.1 The conflict with the self-update rules

`CLAUDE.md` says the self-update writes only:

- `push-manager`, `push-catalog` and `push_hook.so` under `/data/push-hack/hacks/<id>/`;
- its own files under `/data/push-hack/update/`.

It says "never `/etc`, never a catalog hack". The Remote Script must land in `/data/Music/Ableton/User Library/Remote Scripts/PushHack`. Removing the old scripts touches a catalog hack's `install_path`. Both break the rule as written.

**Decision needed (D1).** Options:

- **A. Extend the rule (recommended).** Allow the update to also write `.../Remote Scripts/PushHack` and nothing else in the User Library. Install it in the same step as the binaries, with the same rollback (keep the old folder as `PushHack.prev`, restore on failure). Update `CLAUDE.md`, `docs/architecture.md`, the update README.
- **B. Ship inside the release, install by push-manager at start.** The update only places the files under `/data/push-hack/hacks/push-manager/remote-script/`. push-manager copies them into the User Library when it starts and the version differs. The update rule stays true, but push-manager gains a new writer and the User Library write still happens.
- **C. Ship as a catalog hack.** No rule change, but the user installs it by hand, which fails the "default for everyone" goal.

Plan assumes A until the owner decides.

**Decision needed (D2): removing the old scripts.** Removing `PushHackBrowser` and `PushHackArrangement` from the User Library is a delete outside the project. Options: remove them automatically after `PushHack` is installed and the owner of the old folder is a known Push Hack script (check for a marker file or the exact folder name); or only warn, and show "old script still present" in the System tab with a Remove button. Recommended: warn plus a button, because a user may still run an old hack version that needs it, and the global rule says the user decides what is disposable.

### 5.2 Mechanics (assuming A)

1. `scripts/package-release.sh` copies `resources/remote-scripts/PushHack/` into the release tarball and lists each file in `manifest.json` with its hash, like the binaries.
2. `releasemeta.json` gets no change. A new Remote Script folder does not need `needs_reinstall`.
3. `update/apply.go` swaps the folder: copy to `PushHack.new`, rename old to `PushHack.prev`, rename new into place. The health check stays "new push-manager answers with the target `hack_version`". The Remote Script is a data folder, so it has no health probe of its own. Restore the old folder on failure. Built differently from the first draft: the old folder is kept in `/data/push-hack/update/remote-script.prev`, not as `PushHack.prev` next to the script, because Live lists every folder in `Remote Scripts`.
4. `scripts/install.sh` installs the same folder for fresh installs. `scripts/uninstall.sh` removes it. `--purge` is not needed because the folder holds no user data.
5. Live reads the script only at start. The System tab shows "restart Live to load PushHack vX". Push Hack never restarts Live.
6. First use: the user selects `PushHack` in one control-surface slot (Input and Output = None). Docs and the post-install message say so. This cannot be automated.
7. push-manager adds `GET /api/live/script` returning `{installed_version, running_version, protocol, old_scripts_present: [...]}`. `running_version` comes from `hello` on the socket.

### 5.3 Catalog hacks

- `push-hack-browser-bridge`: becomes a stub. Its repo drops the script. It stays in the catalog only if it adds something else, otherwise it is retired, and push-manager's preset browser requires `PushHack` instead. The "Browser Bridge installed" gating in `hacks_nav.go` (`hackInstalled("browser-bridge")`) changes to "PushHack reachable".
- `push-hack-arrangement` and `pta-module-arrangement`: switch their client to the new protocol and drop their own script. They stay private. They depend on a Push Hack release that carries `PushHack`.
- Every hack that reads Live state declares a dependency on the minimum `protocol` number.

## 6. Answer to the owner's question about the arrangement command set

`push-hack-arrangement` is private on purpose (`~/.claude/push-family.md`: must stay private, not in the catalog). `PushHack` lives in this public repo. When the arrangement script moves into `PushHack/ns/arrangement.py`, its snapshot builder and its commands become public code. That includes `snapshot.py` and `commands.py`, which today are in the private repo. The owner must choose:

- **Accept it:** the code is small, uses only Live's public API, and the family doc says only the hack itself must stay private. Then the arrangement namespace goes in this repo.
- **Keep it private:** `PushHack` supports plug-in namespaces. A private hack drops a file into `PushHack/ns/` or a `plugins/` folder, and `PushHack` loads every module it finds there. The public repo then has the loader only. This adds a loading mechanism and a way for a broken plug-in to hurt Live, so it needs an `try/except` around each import.

Plan assumes "accept it" for `transport` and the basic arrangement snapshot, and the plug-in loader for anything else. **Decision D3 for the owner.**

## 7. Phases

1. **Skeleton and transport.** `PushHack`, listeners, dispatcher, `hello`, `subscribe`, `transport` namespace, legacy text shim for 7704. Tests with a fake song. Device test: enable in a slot, old push-manager features still work through the shim.
2. **Browser and arrangement parity.** Port every command of both old scripts. Prove both old hacks work unchanged on the shim.
3. **Install path.** `package-release.sh`, `install.sh`, `uninstall.sh`, update swap and rollback, `/api/live/script`, System tab row, old-script warning. Device test: update from `0.1.15-alpha`, and a forced failure that rolls back.
4. **Push 2 feature namespaces,** one pull request each, in this order: `session`, `track`, `device`, `clip`, `notes`, `meters`, `modes`. Each one is tested on the device before merge, because the Push 3 Live may differ from Push 2.
5. **Move the other repos** to the new protocol. Remove the legacy shim in a later release.

Each phase ends with docs and a `CHANGELOG.md` entry. A release that changes `push-manager` source also commits the rebuilt binaries.

## 8. Docs to write or update

- New: `docs/pushhack-remote-script.md` (protocol, namespaces, how a hack connects).
- Update: `docs/architecture.md`, `docs/api-reference.md`, `docs/adding-a-hack.md`, push-manager README (Push Hack update section), `catalog/schema.md` (`install_path` text), `CLAUDE.md` (update rule, D1), `~/.claude/push-family.md` (shared script section).

## 9. Risks

- **Old and new scripts enabled together.** Both bind 7704 and the arrangement socket. `PushHack` logs a bind failure and keeps going. The System tab warns.
- **Live re-creates the control surface at start.** Keep class-level listeners and one shared queue, as the old scripts do.
- **A slow or broken client.** Output limits, per-line parse errors, and a per-tick time budget.
- **Push 3 differences.** The Push 2 files are a map, not a guarantee. Test each namespace on the device.
- **Live update changes the Live Object Model.** Each namespace catches errors and replies `ok:false` instead of raising.
- **Rollback.** A failed update restores `PushHack.prev`. A test must never open the real socket path, because the script deletes an existing socket file before it binds.

## 10. Decisions (settled 2026-10-27)

- D1: **A.** The update may also write `Remote Scripts/PushHack`, and nothing else in the User Library. Same rollback as the binaries.
- D2: **Warn plus a Remove button** for old scripts. No automatic delete.
- D3: **Public.** The arrangement namespace goes in this repo. No plug-in loader for now.
- D4: Phase 4 scope as in section 4. The Push 2 hardware and UI parts stay out.

## 11. Hardware settings in push-manager (dropped)

Dropped on 2026-10-27: the owner does not need it. The idea was brightness (display and LED), pad sensitivity, velocity curve and aftertouch mode, sent as SysEx from push-manager. `PushHack` could not send them anyway (no MIDI port), and nothing here depends on them.

## 12. Follow-up: the automation hack reads BPM from the Remote Script

The automation hack takes the tempo from MIDI Clock today. Change it to read the BPM from `PushHack` instead: a `transport` `get` for the first value, then a `subscribe` to the `transport` topic for changes. Reasons: MIDI Clock needs a MIDI route and a stable clock, and it gives a tempo only by averaging pulses; the Remote Script reads the exact `song.tempo` from Live. Do this after Phase 3, when `PushHack` installs with Push Hack, so the hack can depend on a minimum `protocol` number. Keep MIDI Clock as a fallback only if `hello` fails.

## 13. Release (2026-10-27)

- Released as `v0.1.16-alpha` (new updater, no script) and `v0.1.17-alpha` (with the script). The two-step did not work as planned: the update only offers the newest release, so a real 0.1.15 device is offered 0.1.17, and its updater refuses the unknown folder. Those devices need `install.sh` once. The release notes say so.
- The test device already ran the new updater (a rebuilt push-manager still labelled 0.1.15), so its update to 0.1.17 did not exercise that case. It confirmed the real GitHub path: download, hash check, restart, and the script skipped as identical.
- Lesson: when a new updater must reach older devices first, the newer release has to wait, or the older updater needs to offer an older release. Neither is possible here.

## 14. live_bridge.go on the JSON protocol (2026-10-27)

push-manager's `live_bridge.go` now uses `bridgeCall(ns, cmd, args)`: one JSON request per connection. A load waits for Live and returns the reason when it fails (before, the text protocol only acknowledged that the command was queued). The old text shim in `legacy.py` stays for hacks that still use it (an old Browser Bridge client). Remove it in a later release, once no hack depends on it.

## 15. Phase 4 built (2026-10-27)

Script 0.3.0 adds `track`, `session`, `device`, `clip`, `modes`, `meters`, `debug`, and extends `notes` and `transport`. 113 off-device tests. Checked on a Push (Live 12.4.15b7): the reads of every namespace, and writes on a test Set (track name, volume and pan; a device loaded, a parameter set and put back; a clip created, notes added, quantized and removed; a clip fired; the metronome; view focus). Later the same day, also checked on a device: all five audio-to-MIDI conversions (`harmony`, `melody`, `drums`, `simpler`, `drum_rack`), rack chains and paths on a drum kit (16 pads), `pad_to_track`, and Simpler slices with `set_simpler` and `slices_to_drum_rack` (which needs slicing mode: `not_sliced` otherwise). Found and fixed: `filled` for drum pads, the `not_sliced` code, `set_simpler`. `docs/pushhack-remote-script.md` is the reference for hack authors.

## 16. The old protocols are gone (2026-10-28)

Script 0.4.0 removes `legacy.py`, the tagged listener for the old arrangement socket, and the compat stream in the hub (`apply_compat`, `Feed.invalidate`). A first byte other than `{` gets `old_protocol` and the connection is closed, so an old client fails at once. Checked first: nothing in the Push repos still used the text commands or the old socket (automation and PTA use port 7704 with JSON). 97 tests (the legacy and compat ones are gone).
