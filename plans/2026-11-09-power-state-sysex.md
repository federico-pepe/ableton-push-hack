# Power state from the Live Port

Date: 2026-11-09. Status: done on the branch, not yet tested on device.

## Why

The notes in [Cardamien/push3-max-lab](https://github.com/Cardamien/push3-max-lab) (`docs/push-hack-notes.md`) said the battery is readable with the
`GET_POWER_STATE` SysEx (`0x3A`). We checked every point we could on a Push 3 (Push3 2.4.25-beta.1,
Live 12.4.15b7, firmware package 1.5.4).

## What we confirmed on the device

- The reply `F0 00 21 1D 01 01 3A <status> <level> F7` comes once a second. Bit `0x20` of `status` is the
  supply. Bits `0x03` are the charge state. `level` is the percent.
- Captured: plugged in 99 % `22 63`, unplugged 99 % `02 63`, unplugged 94 % `00 5E`, plugged in 94 % `21 5E`.
- All command ids and the press flags (`0x08` short, `0x10` long) match `Push2/sysex.pyc`. Charge state
  values match `Push2/battery_component.pyc`.
- The hardware sends no SysEx on its own. Only Active Sensing at rest.
- Port 80 is open (`PushWebServiceCli`). `/api/v1/` answers `401 {"error":"Unset credentials"}`.
- USB interface classes, CPU, board, BIOS and the launcher log match the notes.

## Not checked

- The device identity reply and the `0x3B` LED colour. Both need us to send, and Push3 holds the raw port.
- The long power-button press (flag `0x10`) and a press while on battery. Not triggered.
- Charge state `0` and `1` at levels above 99 %, and the value at exactly 100 %.
- Everything about the Mac helper, the tethered sockets, the USB mux and the display sync commands.
- The class name `MidiHub` and the Live audio latency numbers.

## Code change

- New `hacks/push-manager/src/power.go`: parser, `powerTracker` (edge detection, freshness).
- `midi.go` calls `power.observe` for each SysEx. `release.go` lost `isPowerPress`.
- `stats.go` `batteryInfo()` uses the last reply when it is less than 5 s old.
- Old bug: the press matched only `3A 2A 64`.

## To do on device before commit

1. Deploy `push-manager`. Open Push Manager, look at the Battery row. It must show the level and status.
2. Take the surface with Home or an app. Press the power button. The surface must return to Live and
   Push3 must show "shut down?". Do it unplugged and at a level below 100 %.
3. Hold the power button. Same result expected.
4. Press Cancel. Shift + Help must open Home again.

## Ideas, not done

- A hack could show the battery level or react to the supply being unplugged. Not planned.
- `0x3B` full-RGB LED colour. Needs a device test and a safe way to send (push-manager owns the port).
