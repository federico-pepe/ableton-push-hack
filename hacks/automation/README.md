# Reference: Automation Hack

LFO-style MIDI CC automation sequencer running on Push 3. Draw curves in a web UI; the hack loops them and sends MIDI CC values to Ableton Live for recording or real-time control.

Port: **7703** — `http://push.local:7703`

---

## How It Works

```
WebUI (browser)
  ↕ HTTP + SSE (EventSource)
automation binary (Go, port 7703, running on Push)
  └─ CC lanes → ALSA seq QUEUE_DIRECT → Ableton Live MIDI input port (128:2)

BPM sync (MIDI clock):
Push 3 hardware (16:0) → MIDI clock (24 PPQN) → automation "Push Hack Clock" input port
automation reads timestamps of 24 ticks, computes BPM = 60.0 / elapsed_per_beat

Transport sync (Push Play button):
Push 3 hardware (16:0) → CC85 val=127 → automation toggles Running (play/stop)
```

The automation binary creates two ALSA sequencer ports:
- **Push Hack Automation** (output) — sends CC events to Live's writable ALSA input port, detected by scanning `/proc/asound/seq/clients` for `"Ableton Live"`. Re-detected every 30 seconds.
- **Push Hack Clock** (input) — subscribes to Push 3 Live Port (16:0) to receive MIDI clock (24 PPQN) for BPM and CC85 (the Play button) for transport toggle.

---

## Lanes

Each lane controls one MIDI CC number.

| Setting | Description |
|---------|-------------|
| **Label** | Display name (cosmetic only) |
| **CC** | MIDI CC number (0–127) |
| **Channel** | MIDI channel, 0-indexed (0 = ch 1) |
| **Smooth** | Toggle between linear and Catmull-Rom spline interpolation |
| **Sync / Free** | Sync loops to Live's BPM; Free uses its own BPM + loop length |
| **Beats** | Loop length in beats (Sync mode) |
| **BPM / Secs** | Free-mode tempo and loop length |
| **Enable** | Mute/unmute lane without deleting |

Max 8 lanes.

---

## ⚠️ Required Setup in Live

CC values are sent to Live's internal ALSA MIDI input port — Live only routes that port to tracks if you enable it in MIDI preferences.

1. Open **Live → Preferences → Link, Tempo & MIDI**
2. Find **Push Hack Automation** in the input list
3. Set **Track** = On and **Remote** = On
4. In the track or device: use **MIDI Learn** (Cmd+M / right-click) to map a parameter to the CC number configured in the lane

---

## Curve Editor

Each lane has a canvas curve editor:

- **Click** empty area → add breakpoint at that position
- **Drag** breakpoint → move it (phase clamped to 0.001–0.999)
- **Right-click** breakpoint → delete it (minimum 1 point)
- Touch events supported

X axis = phase (0–1, one full loop). Y axis = CC value (bottom = 0, top = 127).

---

## Interpolation

**Linear:** piecewise straight lines between breakpoints. Wrap-around segment (last → first) included.

**Smooth (Catmull-Rom):** cubic spline through breakpoints with circular indexing — smooth curves, no kinks at breakpoints, no extra control points needed.

---

## Transport

### Manual mode (Sync to Live off)

Global **Play / Stop** button in the WebUI. When running, a 50Hz engine ticker advances each lane's phase and fires CC events. The current phase for each lane is streamed via SSE at 20Hz — the animated vertical playhead on each canvas tracks this.

### Sync to Live

When **Sync to Live** is enabled, the **Push Play button (CC85)** is the sole transport control:
- Push has no separate stop button — press Play to start, press Play again to stop (toggle).
- Each CC85 press (val=127) toggles `Running`; on stop, all lane phases reset.
- The WebUI Play/Stop button becomes a **read-only status indicator** (● Playing / ○ Stopped), updated live via SSE so it always mirrors Push.

This is a **single source of truth** by design: CC85 is the only thing that toggles `Running` when synced. MIDI Start/Stop events and tempo polling deliberately do **not** touch transport — earlier versions had all three fighting and the WebUI desynced.

Enable/disable via the **Sync to Live** checkbox in the transport bar, or `POST /api/auto/transport_sync`.

> **Requirement:** CC85 must reach the automation's "Push Hack Clock" input (subscribed to Push3 16:0). Verify the Play button fires CC85 in push-manager's MIDI monitor (`http://push.local:7701`) if transport doesn't respond.

---

## BPM Sync (MIDI Clock)

BPM is derived from Live's MIDI clock signal (24 PPQN) received on the "Push Hack Clock" ALSA port subscribed to Push3:16:0. Live sends MIDI clock to the Push hardware regardless of transport state.

**Formula:** BPM = `60.0 / elapsed` where `elapsed` is the time span of 24 consecutive clock ticks (= 1 beat). Implemented with a 24-slot ring buffer of nanosecond timestamps. Sanity check: `0.04s < elapsed < 10.0s` (BPM 6–1500).

**Fallback:** If no MIDI clock has been received in the last 5 seconds, the automation engine uses the last known BPM (default 120.0).

Phase advance per 20ms tick (sync mode):
```
Δphase = 0.020 * liveBPM / (beats × 60)
```

Free mode uses the lane's own `FreeBPM` and `FreeSecs` settings, ignoring Live's clock.

---

## API Reference

Base URL: `http://push.local:7703`

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/` | Web UI (single-file SPA) |
| `GET` | `/api/auto/state` | Full state: lanes, running, live_bpm, transport_sync |
| `POST` | `/api/auto/play` | Start playback |
| `POST` | `/api/auto/stop` | Stop playback |
| `POST` | `/api/auto/transport_sync` | Enable/disable Sync to Live. Body: `{"enabled": true}` |
| `GET` | `/api/auto/stream` | SSE stream: phases + running + live_bpm + transport_sync @ 20Hz |
| `POST` | `/api/auto/lane` | Create lane |
| `PUT` | `/api/auto/lane/{id}` | Update lane by integer ID |
| `DELETE` | `/api/auto/lane/{id}` | Delete lane |
| `POST` | `/api/auto/lane/{id}/reset` | Reset lane phase to 0 |

### SSE Event Format (`/api/auto/stream`)

```json
{"running": true, "phases": [0.12, 0.55], "live_bpm": 124.0, "transport_sync": false}
```

Sent every 50ms while connected.

### Lane Object

```json
{
  "id": 0,
  "label": "Filter Cutoff",
  "cc": 74,
  "channel": 0,
  "points": [
    {"phase": 0.0, "value": 0.2},
    {"phase": 0.5, "value": 0.9},
    {"phase": 1.0, "value": 0.2}
  ],
  "enabled": true,
  "smooth": true,
  "sync_mode": true,
  "beats": 4.0,
  "free_bpm": 120.0,
  "free_secs": 4.0
}
```

---

## Persistence

Lane configuration is saved to `/data/push-hack/hacks/automation/automation.json` after every change. Atomic write (tmp file + rename). State survives service restarts.

---

## ALSA Routing Details

At startup, `initMidiOut()` calls `detectLivePort()`, which scans `/proc/asound/seq/clients` for the `"Ableton Live"` client and finds its first writable (capability contains `W`) non-`"Announce"` port. Events are sent via `QUEUE_DIRECT` (bypasses subscription routing) directly to that port.

`initMidiIn()` creates the "Push Hack Clock" receive port and subscribes to Push 3 Live Port (16:0). A blocking `read()` loop in a dedicated goroutine decodes MIDI clock ticks (BPM) and CC85 (Play button → transport toggle). MIDI Start resets the clock ring only.

To verify routing on Push:
```bash
cat /proc/asound/seq/clients | grep -A5 "Ableton Live"
cat /proc/asound/seq/clients | grep -A5 "Push Hack"
```

---

## Known Issues / Limitations

- **MIDI mapping required** — see Required Setup above. Each CC must be MIDI-mapped in Live.
- **Transport sync needs CC85** — the Push Play button must reach the automation input. If it doesn't toggle, confirm CC85 fires in push-manager's MIDI monitor.
- **Max 8 lanes** — enforced in the UI.
- **BPM accuracy** — 24-tick ring buffer gives accurate average BPM; short tempo taps may lag by one beat.
- **No Shadow UI** — v2 improvement.
