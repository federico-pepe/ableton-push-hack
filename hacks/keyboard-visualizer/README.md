# Keyboard Visualizer

Renders a live piano-keyboard visualization on Push 3's own screen, driven by
the notes **actually sounding in Live** — after any octave-shift or Scale/
In-Key transform — rather than the pad grid's raw pre-transform MIDI.

See `discovery/live-note-keyboard-viz.md` for the full feasibility writeup and
design rationale.

## Why not the raw pad stream

Push's pad grid always sends the same fixed chromatic notes (36-99). Push's
Octave +/- buttons and Live's Scale/Note mode change what actually plays
without changing what the pads themselves transmit — a visualizer driven by
that raw stream would show the wrong notes whenever either is active.

## How it works

1. This hack creates its own writable ALSA sequencer port, **"Keyboard Viz
   In"** (mirrors push-manager's own "Push Manager In" port pattern,
   `hacks/push-manager/src/midi.go:838-848`).
2. On Push's own screen, route a Live track's MIDI **Out** to this port
   (stock Live MIDI routing — no script install, no Max for Live device). One
   time per project/track, similar in spirit to Browser Bridge's one-time
   activation, but pure stock Live UI.
3. Whatever Live actually sends out that route — post-transform — arrives on
   the port. This hack tracks held notes (`map`-like `[128]bool`, set on Note
   On, cleared on Note Off; velocity-0 Note On is treated as Note Off) and
   renders a keyboard image.
4. The image is pushed to push-manager's existing display API
   (`POST /api/display/image`) whenever held notes change — but only while
   display takeover is switched on (see below). This hack never touches the
   shared-memory framebuffer directly.

Display takeover is **off by default** and toggled by holding **Shift + Note**
(CC49 + CC50) on Push's hardware — the same chord-detection pattern
push-manager already uses for hardware chords. Toggling on calls
`POST /api/display/mode {"mode":2}` and immediately draws the current
keyboard state; toggling off calls `{"mode":0}` to hand the screen back to
the native Push UI. To watch for this chord, "Keyboard Viz In" self-subscribes
to Push 3's own hardware MIDI port (`"Ableton Push 3 Live Port"`) in addition
to whatever Live routes to it — an ALSA seq port can receive from multiple
senders at once, so this reuses the same port rather than creating a second
visible ALSA client just to watch two CCs. Because both Push 3's raw pad
stream and Live's routed notes now land on the same port, incoming Note On/
Off events are filtered by their ALSA source client: only notes whose source
is *not* Push 3 update the visualizer's held-note state, so a pad press
doesn't also show up as its own (pre-transform) key on top of Live's
post-transform one. Only CC49/50 from Push 3 are read; MIDI intercept is
untouched.

Runs fully independent of MIDI intercept: the pad grid keeps playing into
Live normally throughout, whether or not the keyboard is currently shown.

## v1 scope / open questions

- Fixed 49-key window (4 octaves, notes 36-84, centered on middle C)
  rendered across the screen width. A scrolling/auto-ranging window sized to
  recent activity would adapt better but adds complexity — revisit if 49
  keys proves too narrow in practice.
- All senders routed to "Keyboard Viz In" are merged into one visualization
  (no per-channel/per-track distinction). Fine for the common case of routing
  one track at a time.
- No push-manager web UI integration — on-device rendering is the only
  interface in this version.

## Build & deploy

```bash
cd hacks/keyboard-visualizer && PATH=$PATH:/usr/local/go/bin make
./scripts/install.sh --hack keyboard-visualizer --build
```

## API

`GET /api/status` (port 7702) — `{"status":"ok","name":...,"version":...,"port":7702,"notes_held":<n>}`.
No other HTTP surface in v1; all real work is ALSA-in → display-API-out.
