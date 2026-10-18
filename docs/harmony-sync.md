# Push Hack Harmony: one app follows another's chords

Push Hack Harmony is a small MIDI format. With it, one app tells others
what it plays: the key, the scale and the chord of the moment. An app
that follows can then play in that harmony. Riffs follows Stacks this
way, but the format is open: any app, a Live track or a keyboard can
drive a follower, and any app can follow.

The code is in [`core/harmony`](../core/harmony): the message format, chord
names from notes, a key finder.

## Ports

- An app that **sends** harmony opens a readable MIDI port named
  `<App> Harmony`, for example `Stacks Harmony`. It sends only harmony
  messages on that port, so its normal output stays notes only.
- An app that **follows** opens a writable port (Riffs: `Riffs Sync`). It
  connects that port to every port whose name ends in ` Harmony`, not its
  own. It looks again every few seconds, so apps can start in any order,
  and an app that restarts is found again.
- Anything else can send to the follower's port directly: a Live track
  (set its MIDI To to the port), a keyboard, another program.

Live connects to every readable port, so it also receives the
`<App> Harmony` port. It ignores the SysEx.

## The message

Every message is one SysEx:

```
F0 7D 50 48 01 <type> <body> F7
```

- `7D` is the MIDI manufacturer ID for non-commercial use.
- `50 48` is "PH", for Push Hack.
- `01` is the version.
- Every body byte is 7-bit (0 to 7F).

A reader ignores a type it does not know, and any bytes after the fields
it knows. So the format can grow without breaking older apps.

### Type 01: HARMONY

| Field | Bytes | Meaning |
| --- | --- | --- |
| key | 1 | the key's root, 0 (C) to 11 (B); 7F unknown |
| scale | 2 | the scale's notes as a 12-bit mask above the key: bit n = n semitones. High 5 bits, then low 7 |
| root | 1 | the chord's root, 0 to 11; 7F unknown |
| chord | 2 | the chord's notes as a 12-bit mask above its root, high 5 bits then low 7 |
| bass | 1 | the lowest note's pitch class, 0 to 11; 7F unknown |
| delta | 2 | when it applies: this many MIDI clock pulses after the message (24 a quarter note). High 7 bits, then low 7. 0 means now |
| flags | 1 | bit 0: playing now. Bit 1: armed (will play when the transport starts). Neither: the sender is silent |

Example: A minor in C major, starting 6 clocks from now, playing:
`F0 7D 50 48 01 01 00 15 35 09 01 09 7F 00 06 01 F7`.

### Type 02: ALIVE

No body. A sender sends it about once a second when it has nothing new to
say, so a follower can tell a quiet sender from one that has gone.

### Type 03: BYE

No body. The sender stops, for example because it quits.

## Timing

A chord change can arrive before it happens. Both apps count Live's MIDI
clock from the same Start (`FA`): the first clock after Start is pulse 0,
each later clock adds one.

- The **sender** sends a change `delta` pulses before it happens. Stacks
  uses 6 (a quarter of a beat).
- The **follower** applies it at its own pulse count + `delta`, before it
  plays that pulse. The two apps can hear the same clock a moment apart,
  so a follower moves the change to the nearest step boundary when it is
  within 2 pulses of one.
- What a sender cannot know in advance (a Start, a Stop, an edit, a pad
  held down) it sends with delta 0.
- When the transport stops, a sender sends the chord the next Start will
  play, marked armed.

Measured on a Push: Riffs and Stacks start each new chord within 3 ms of
each other, and every Riffs note followed the chord Stacks played.

## Notes instead of messages

A follower can also take plain notes as chords. Riffs groups notes that
start within 30 ms into one chord and names it. The chord holds after the
keys are released, until the next chord. A key finder guesses the key from
the chords played. A sender that has sent any harmony message is followed
by its messages only, and its notes are not read as chords.

## Many apps

- Each sender is a source, known by its MIDI address. A follower follows
  one source at a time. Riffs offers Auto (the first one heard, kept while
  it plays) or a source chosen by name.
- A sender that is silent (flags 0) is not followed. A sender that sends
  nothing for 3 seconds, or sends BYE, is gone.
- An app never follows its own ports.
- The framework installs each hack once, so two copies of the same hack
  cannot run at the same time. Different apps can all send and follow at
  once.

## Apps that are not on screen

With Push Hack Home only the app on screen normally plays. For sync, both
apps must play. Riffs and Stacks each have a "plays when closed" setting
(off by default) that keeps them playing while another app has the
screen.
