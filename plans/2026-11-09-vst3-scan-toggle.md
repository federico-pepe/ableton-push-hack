# VST3 scan toggle in the System tab

Date: 2026-11-09. Status: built and checked on device (2026-11-12).

## Goal

Add a button to the System tab of the push-manager web UI. The button turns VST3
scanning on or off on Push. It changes one flag in Live's `Preferences.cfg`
without SSH.

## Background (tested on the device)

Setup of the tests: AbletonOS with Live 12.4.25b1, 2026-11-09.

- The flag is `AreSystemPathsEnabled` in the `Vst3Preferences` block. The layout is
  the marker `\x0fVst3Preferences`, then `04 00 00 00`, then one flag byte.
- Live reads the flag only at startup. We patched the file to 1 while Live ran with
  0 in memory. No scan started in 75 seconds.
- Live's Python API has no rescan or preferences call. `app` has 38 names and
  `app.browser` has 30. Neither list has a scan call.
- So a restart of Live is the only way to apply the change.
- After `/etc/init.d/push3 stop`, the file kept our flag. Live did not write its own
  value on exit. We saw this once. Do not depend on it. Always patch while Live is
  stopped.

## Rules that apply

- Write only to `/data/.config/Ableton/` and `/data/push-hack/backup/`. Never to
  `/boot`, `/opt` or `/etc`.
- Restarting Push3 ends the Live session. The user must confirm this.
- The self-update flow must not use this feature. It never restarts Push3.

## Design

### API

| Route | Action |
|---|---|
| `GET /api/vst3` | Return the state. No write. |
| `POST /api/vst3` with `{"enabled": bool}` | Change the flag and restart the Push stack. |

`GET` returns `{"state": "on" | "off" | "unknown", "file": "<path>", "plugins": N}`.
`unknown` means that the byte layout is not what we expect. `plugins` is the number
of entries in `/data/.vst3`, so the user sees what a scan finds.

### Steps of the change

1. Find the `Preferences.cfg` of the highest Live version. Sort by version number.
2. Check the layout. If it does not match, return an error. Do not stop Live.
3. If the flag already has the wanted value, return OK. Do not restart.
4. Copy the file to `/data/push-hack/backup/Preferences.cfg.bak`. If the copy
   fails, return an error. Do not stop Live.
5. Return the HTTP response.
6. In a detached helper, run `/etc/init.d/push3 stop`. Wait 5 seconds.
7. Write a temporary file next to the original. Change the one byte. Keep the owner
   (`ableton:users`) and the mode. Rename the temporary file over the original.
8. Read the file again. If the byte is wrong, copy the backup back.
9. Run `/etc/init.d/push3 start`.

Use `stop`, a pause, then `start`. A plain `restart` did not restart Live in our
earlier test.

### Web UI

- A row in the System tab: the state (On, Off, Unknown), a button, and the plugin
  count.
- The button opens a confirm dialog. The text must say that Live restarts, that the
  session ends, and that the display and MIDI are back after about 30 seconds.
  Unsaved work is lost.
- After the user confirms, the page shows "Restarting..." and polls `GET /api/vst3`
  until push-manager answers again.
- If the state is Unknown, show the button disabled with the reason.

### Disable

Turning the flag off keeps `/data/.vst3`, the plugin database and all presets. A
preset for a plugin that Live no longer scans fails to load. The dialog says so.

## Files to change

- `hacks/push-manager/src/vst3.go` (new): read, check, patch, and the helper.
- `hacks/push-manager/src/vst3_test.go` (new): test the byte patch on sample data.
  The test must not touch `/data` or the real device paths.
- `hacks/push-manager/src/main.go`: register the two routes.
- `hacks/push-manager/src/ui/index.html`: the System tab row and the dialog.
- Rebuild `hacks/push-manager/push-manager` and commit it with the source.
- Docs: `docs/api-reference.md`, `docs/vst3-on-push3.md` (add the button, and fix
  the sentence "Live rewrites `Preferences.cfg` when it exits"),
  `hacks/push-manager/README.md`, `CHANGELOG.md` (Unreleased).

## Tests

Off the device:

- `make test-manager` style unit tests for the byte patch. Cases: flag 0 to 1,
  1 to 0, same value, wrong layout, missing marker, file with two markers (use the
  last one, as the doc script does).
- `GOOS=linux GOARCH=amd64 go vet ./...` in `hacks/push-manager/src`.

On the device (the user confirms each result):

1. `GET /api/vst3` shows the right state.
2. Turn on from off. Live restarts, `PluginScanner.txt` shows `VST3: found`
   lines, and `Log.txt` shows a scan between `Scan start` and `Scan end`.
3. Turn off from on. Live restarts, and `Scan start` and `Scan end` have nothing
   between them.
4. A layout error leaves Live running.
5. The display and MIDI come back after the restart, and push-manager answers.

## Decisions and test results (2026-11-09)

- **Which file:** use the folder with the highest Live version number. This is the
  most recent version, and the user confirmed it. Sort by version, not by text and
  not by modification time. Step 1 of the change uses this rule.
- **push-manager during the stop:** tested. `push3 stop` does not stop
  push-manager. Its process (PID 818) kept running, and it answered on port 7701
  after `push3 start`. The detached helper can still run inside push-manager.
- **Live on stop:** tested a second time. Live ran with 1 in memory, and we set the
  file to 0. After `push3 stop` the file was unchanged (same bytes, same
  modification time). Live did not write on exit in either test. The order of the
  steps stays the same, and the backup stays as a safety step.
- **After an OS update:** the first version does not restore the flag. The user
  presses the button again. The state readout shows the problem.
