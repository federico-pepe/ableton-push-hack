# Push Hack Virtual Audio — a working virtual sound card for Push 3

**Status: working, confirmed on real Push 3 hardware (2026-08-27).**

## Context

Push 3 standalone runs the full Ableton Live app on its own Linux system.
A hack process cannot open the real audio hardware. Live already holds
it. This plan records a different, working way to get audio in and out
of Live: a virtual sound card, built from the Linux kernel's own ALSA
Loopback driver.

The investigation looked at other options first: Ableton Link's audio
extension, and an LD_PRELOAD hook inside Live's own process. Both had
real blockers (see "Options we tried and dropped" below). The virtual
sound card is the one that works today, with no changes to Live or to
any file Ableton ships.

## What works

A virtual sound card named **"Push Hack Virtual Audio"** appears in
Live's own audio device list, on Push 3's screen, right next to the
real hardware. A user can pick it as an input or an output on any
track, the same way as any other audio device.

Audio written by an outside process reaches Live through this device.
Audio from Live can also reach an outside process the same way. A test
confirmed this end to end: an outside process played a tone into the
virtual card, and it came out of Push 3's real speaker, through a Live
track set to monitor the input.

This needs two pieces, both in `hacks/push-audio-loopback/`:

1. **`snd-aloop`, a small kernel module.** This is a real, standard part
   of the Linux kernel (`sound/drivers/aloop.c`). It is not custom code.
   Push 3's kernel supports it, but Ableton does not ship the built
   file. This hack builds it from Ableton's own GPL source release for
   Push 3, with a small patch that renames it to "Push Hack Virtual
   Audio" so a user can tell it apart from the real hardware.
2. **`loopback_feed`, a small tool.** This writes audio into the
   virtual card at a rate that matches Live's own timing. Without this
   care, the audio sounds glitchy (see below for why, and the fix).

Full build and deploy steps are in
`hacks/push-audio-loopback/README.md`.

## Why the real hardware device does not work

Push 3's speaker and headphone jack sit behind a chip called XMOS. The
XMOS chip exposes them to Linux as a normal, standard USB audio device.
Live opens this device once, for its whole lifetime, and does not
share it. A second process that tries to open the same device gets a
`Device or resource busy` error. This is normal, expected Linux
behavior for a real sound card, not a bug.

The virtual sound card avoids this problem completely. It is a
separate device from the real hardware. Live can have both open at the
same time: the real hardware for its own audio, and the virtual card
for the outside process.

## The glitch, its cause, and its fix

An early test sent a tone through the virtual card and heard it, but
with clicks and stutter. The clicks did not come from a dropped
buffer — the system reported zero buffer errors throughout the test.

The real cause: Push 3's kernel updates its internal clock every 4
milliseconds (a setting called `CONFIG_HZ=250`). The Loopback driver
uses this same clock by default. Live, though, asked for much smaller,
faster chunks of audio (about 3 milliseconds each). The two rates did
not line up, so audio arrived in uneven bursts instead of a steady
stream. This is what caused the glitch.

**The fix: use a larger audio buffer in Live's own audio settings.** A
bigger buffer holds enough audio to smooth over the mismatch between
the two clocks. With a buffer of 512 samples (up from Live's default
128), the tone came through clean, with no clicks. Live's own audio
settings on Push 3's screen control this value directly. No file
outside of Live needs to change for this fix.

We also tried to point the Loopback driver at a different, faster
clock (a real hardware timer instead of the default one). This did
not work: the audio driver behind Push 3's real hardware does not
support this kind of clock sharing, and the attempt failed right away
with an I/O error. The buffer-size fix above is the one to use.

## Options we tried and dropped

Two other approaches came before the virtual sound card. Both are
recorded here for context, in case a future need brings them back.

- **Ableton Link's audio extension.** Live on Push 3 answers on the
  standard Link network protocol and shows up as an audio peer named
  "Move". This is real and confirmed, but needs a network round trip,
  and whether it carries a full audio signal was not confirmed before
  the virtual sound card was found to work.
- **An LD_PRELOAD hook inside Live's own process.** Push 3 already runs
  one such hook, for its own display and MIDI (`hacks/push-display`).
  A new hook was tried to catch Live's own audio calls directly. This
  did not work: Live's file carries a Linux security marker
  (`cap_sys_nice`, for real-time audio scheduling), and Linux drops
  `LD_PRELOAD` for any program with this kind of marker. This is a
  standard Linux security feature, not a bug to work around.

## Known limits and next steps

The virtual card does not survive a reboot. It needs `insmod` again
after every restart, since kernel modules built outside the kernel's
own build are not part of the boot process. A future version of this
hack could add a start-up script for this, the same way
`hacks/push-display` adds its own hook to `/etc/init.d/push3`.

`loopback_feed` is a test tool today, not a finished audio engine. Real
use — for example, running a synthesizer or an effect plugin through
this virtual card — needs a proper audio host in place of this test
tool.
