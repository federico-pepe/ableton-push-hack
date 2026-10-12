# Mixxx mode: two modes on one Push

## Goal

Push has two modes:

- **Push mode** - stock Push3 + Live, as today.
- **Mixxx mode** - Push3 and Live are stopped. Mixxx plays DJ sets through
  the Push's own audio outputs. The Push screen shows deck state. Pads,
  buttons and encoders control Mixxx.

## Design decision: switch at runtime, not at boot

Another Push hacker (Ovrtk) shows a boot chooser by editing the startup
files in `/etc/init.d` and `/etc/rc*.d` so Push3 does not start on its own.
We do not do that:

- Our safety rules forbid writing to `/etc/` outside our own
  `push-hack-*` files and the LD_PRELOAD line.
- A broken chooser at boot leaves a Push that never starts Push3.

We get the same result at runtime instead. Push always boots into Push
mode. A switch stops Push3 with its own init script
(`/etc/init.d/push3 stop`, which install.sh already uses) and starts
Mixxx. Switching back stops Mixxx and runs `/etc/init.d/push3 start`.

"Boot into the last mode" can come later, as our own `S99push-hack-mixxx`
service that switches to Mixxx mode after Push3 has started. That needs
no change to Push3's own startup files.

## What we measured on the device (2026-10-12, read-only)

- `/etc/init.d/push3 stop` kills the launcher (`XPython3Exe
  push3-launcher.pyc`) and everything under it: Push3, Live, Ableton Index.
- **MIDI does not need Push3.** The Push's MIDI ports belong to the kernel
  driver (client 16 `"Ableton Push 3" [Kernel]`, ports Live/User/External).
  They stay up when Push3 stops.
- **Audio does not need Push3.** The only sound card is the Push's own USB
  audio (`snd-usb-audio`). Today Live holds it (`pcmC0D0p`/`pcmC0D0c`).
  Once Live is stopped, Mixxx can open it.
- **The display needs a new owner.** It is USB interface 0 (vendor class,
  bulk OUT endpoint `0x01`, bound to `usbfs`). Push3 is the only process
  that has it open. With Push3 stopped we must claim it and send frames
  ourselves. The protocol is the public Push 2 one (16-byte header
  `FF CC AA 88 ...`, XOR-shaped BGR565, 960x160, stride 1024). The screen
  goes blank if no frame arrives for about 2 s.
- Xorg `:0` runs on its own (`S09xserver-nodm`), not under Push3. Mixxx
  can put its hidden window there.
- glibc 2.35 (same as Ubuntu 22.04). No package manager. 4 threads,
  7.5 GB RAM.
- The only GL driver is `crocus`, which does not support this CPU. Mixxx
  must use software waveforms.

## Who owns the display in Mixxx mode

push-manager stays the only display owner, in both modes. We add a second
backend to `core/display`: when Push3 is not running, push-manager claims
USB interface 0 and writes frames to endpoint `0x01` itself, and sends a
keep-alive frame at least every second. `/api/display/image` works the
same in both modes, so the Mixxx hack draws through it like every other
display-owning hack.

## Phases

Each phase has a check. A failed check stops the work until we decide
again.

### 0. Spike on the device (user at the Push)

1. `/etc/init.d/push3 stop`.
2. Wait 30 s. Check that nothing starts Push3 again.
3. Press buttons. Check that events arrive on the kernel port (push-manager
   MIDI monitor).
4. Play a WAV on `hw:0` with `aplay`. Check it comes out of the Push.
5. A small Go test program claims interface 0 and sends one test frame.
   Check it shows on the screen.
6. `/etc/init.d/push3 start`. Check that Push mode comes back normally.

Gate: all six pass.

**Results (2026-10-12, run 16:05):**

- Step 2 pass. Nothing started Push3 again in 60 s.
- Step 3 pass. 1279 events arrived on 16:0. Buttons send CC on
  channel 0 as documented (Play = CC 85, Record = CC 86). **Pads stay in
  MPE mode**, which Push3 set: each note gets its own channel (1-15), with
  channel aftertouch, CC 74 and pitch bend. The Mixxx mapping must handle
  that, or switch the pads out of MPE with SysEx when Mixxx mode starts.
- Step 4 pass (user heard it on the second run, 16:11). No Live process
  held the sound card. The card has 16 output channels, S24_3LE, 44.1 to
  96 kHz, running at 44.1 kHz. The test played 440 Hz on channels 1-2 and
  660 Hz on channels 3-4 through `plughw:0,0`. Which pair is the main
  output and which is the headphones still needs a check before the cue
  output is set up.
- Step 5 pass. `usbspike` claimed interface 0 and sent 1620 frames (about
  27 fps) with no USB errors, and the test image showed. No extra setup
  message was needed.
- Step 6 pass. Push3 and Live came back on their own.

### 1. Mixxx bundle

Build in Docker `ubuntu:22.04` from the Mixxx PPA (2.4). Copy Mixxx and
every shared library it needs into `/data/push-hack/hacks/mixxx/`. Run it
on `DISPLAY=:0` with the Push sound card. Load two tracks and play both.

Gate: no audio dropouts, total CPU under 50 % with two decks playing.

### 2. USB display backend

`core/display`: claim interface 0 through usbfs ioctls (no cgo), bulk
write to `0x01`, keep-alive. push-manager picks the backend by checking
whether Push3 is running.

Gate: `/api/display/image` shows a test image with Push3 stopped. After
`push3 start`, Push3 gets the screen back.

### 3. Mixxx hack (own repo, `push-hack-mixxx`)

- `service.initd`: `start` = stop Push3, start Mixxx and the bridge.
  `stop` = stop both, start Push3.
- Mixxx controller mapping (XML + JS) on "Ableton Push 3 Live Port":
  transport, cue, sync, pitch, EQ, crossfader, hot cues on pads, LEDs.
- The mapping script sends deck state as MIDI to a port the bridge reads.
  The bridge draws two decks at 960x160 with `core/gfx/widgets` and posts
  them to `/api/display/image`. Track titles: from the script if Mixxx
  exposes them, otherwise from Mixxx's library database.
- Mixxx is GPL: publish its source with the bundle.

### 4. Switching

- Push mode to Mixxx: an entry in the Shadow UI, plus a button in the web
  UI.
- Mixxx to Push mode: a held chord handled by push-manager (it still gets
  MIDI from the kernel port). Also a web UI button, in case the controls
  stop responding.
- If Mixxx crashes, the bridge shows a message and the exit chord still
  works.

### 5. Later: boot into the last mode

Our own `S99push-hack-mixxx` service.

## Open risks

- Push3 may send a setup message to the display or to the XMOS chip at
  startup that we do not know about. Phase 0, step 5 finds out.
- LEDs and pad colours keep whatever Push3 last set until our mapping
  sets them.
- The rule "uninstall before an OS update" still applies.
