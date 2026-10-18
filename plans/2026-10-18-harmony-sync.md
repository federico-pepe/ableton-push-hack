# Harmony sync: Riffs follows Stacks, or any app, over MIDI

Date: 2026-10-18. Branches: `feat/harmony-sync` in ableton-push-hack
(core), push-hack-riffs and push-hack-stacks.

## Goal

Riffs can follow what another app plays: the key, the scale and the chord
of the moment. The link is MIDI only, so any app (Stacks, another hack, a
Live clip, a keyboard) can drive Riffs, and many apps can run at once.
Sync is off by default and never changes the riff you made.

## What the code showed

- Notes on `Riffs In` already play Riffs' performance keyboard. Chords
  sent there would trigger keys, so sync needs its own port.
- Both apps go silent when they do not have the surface ("parked"). Push
  Hack Home gives the surface to one app at a time, so today Stacks and
  Riffs never play together. Riffs also stops counting the clock while
  closed, so it comes back out of time.
- Stacks is a C core (Move module) hosted from Go. `stacks_unit.c`
  includes the vendored `stacks.c`, so a small accessor after the include
  can read the sequencer without changing the vendored file.
- Stacks' `prog` state already lists every chord with its exact notes,
  and `key`/`scale` give the key. Both apps encode a scale as a 12-bit
  mask (bit n = n semitones above the key).
- Both apps count Live's clock from the same Start (0xFA), one pulse per
  0xF8. That shared count is what makes exact timing possible.
- The framework installs each hack id once, so two copies of the same
  hack cannot run today. Many different senders and followers can.

## Design

### Ports

- A sender of harmony opens a readable port named `<App> Harmony`
  (Stacks: `Stacks Harmony`). It carries only harmony messages, so Live
  never sees them.
- Riffs opens a writable port `Riffs Sync`. Notes there are chords to
  follow; SysEx there is the harmony message.
- Riffs connects `Riffs Sync` to every `* Harmony` port it finds (not its
  own), and looks again every few seconds, so apps can start in any order.
- Anything else (a Live track, a keyboard, another app) can send to
  `Riffs Sync` directly.

### The harmony message (SysEx, open spec: docs/harmony-sync.md)

`F0 7D 50 48 01 <type> ... F7`. 7D is MIDI's ID for non-commercial use,
`50 48` is "PH" (Push Hack), 01 the version.

Type 01, HARMONY:

| Field | Bytes | Meaning |
| --- | --- | --- |
| key | 1 | 0-11, 7F none |
| scale | 2 | 12-bit mask, high 5 bits then low 7 |
| root | 1 | chord root 0-11, 7F none |
| chord | 2 | 12-bit mask of the chord's notes, relative to the root |
| bass | 1 | 0-11, 7F none |
| delta | 2 | 14-bit: the change happens this many clock pulses after the message, 0 now |
| flags | 1 | bit 0: playing (the sender's transport runs) |

A receiver ignores types and trailing bytes it does not know, so the
format can grow.

### Timing

- Stacks asks its core which chord plays 6 pulses ahead, every pulse. When
  that changes, it sends HARMONY with delta 6. On Start, on a stop, and on
  any edit it sends the current chord with delta 0.
- Riffs counts Live's clock from Start itself (always, even while
  closed). A message with delta d, heard at pulse c, applies at pulse
  c + d, moved to the nearest step boundary when it is within 2 pulses
  (the two apps may hear the same pulse a moment apart). It applies
  before Riffs plays that pulse, so the first note on the downbeat
  already follows the new chord, with no added delay.
- Notes (no SysEx) are grouped: notes that start within 30 ms are one
  chord. The chord applies from the next step. A sender that sends SysEx
  is followed by its SysEx alone; its notes are ignored.

### Many sources

- Each source is its MIDI address (client:port), shown by its client name.
- Follow: Auto (the first source heard, kept while it is heard; another
  takes over only after 4 s of silence) or one chosen source.
- Notes are grouped per source, so two keyboards never mix into one
  chord.
- Riffs never follows its own ports.
- Once a second Riffs holds its sources against the ports there are: a
  sender that quit is dropped, and another port at its address (the
  kernel reuses client numbers) is a new source.

### What Riffs does with it

Sync: Off, Key, Chords. Nothing is written into the riff; pitches change
as they are played. Sync off gives the riff back exactly.

- Key: each note moves from the riff's key and scale to the incoming
  ones, by scale degree, with the smallest overall transposition (-6 to
  +5 semitones), so the line keeps its shape.
- Chords: at each step the riff's own chord (its chord loop, in its key)
  maps to the incoming chord. The whole chord moves by the smallest
  transposition between the two roots. Then each note keeps its role:
  root, third, fifth and seventh go to the incoming chord's root, third
  (or its sus note), fifth and seventh; other notes keep their scale step
  above the root, in the incoming scale (the key's scale when it is
  known, otherwise a scale that fits the chord). A slash chord's bass
  replaces the root for low notes (below MIDI 48).
- Notes keep their pitch once started; stutter repeats keep the pitch of
  their note. Pitch range and lane octaves still apply after the move.

### Keep playing when closed

- Riffs and Stacks each get a setting "Plays when closed" (off by default:
  the reported "Stacks plays when closed" stays fixed). On, the app
  plays along with Live while another app has the surface.
- Riffs, closed and not playing, still counts the clock and moves its
  playhead, so it is in time when it opens.

### Where it shows

- Riffs panel: a Sync control (Off / Key / Chords) with what it follows
  ("Am7 from Stacks"), and Settings for the source, the channel filter
  and Plays when closed.
- Riffs on Push: the ROOT cell of MAIN shows SYNC and the chord (minor
  spelled out, MIN, since the screen draws capitals only), the SCALE cell
  the source; on the KEYS tab encoder 6 is Sync, encoder 7 Plays when
  closed.
- Stacks: PLAY CLOSED on its CLIP tab, and `plays_closed` in its web API.

## Code

- `core/harmony`: pitch-class sets, chord naming from notes, the SysEx
  codec, a chord grouper. Tests.
- Riffs: port, sources, scheduler on the clock count, the pitch map in
  the player, state (sync settings are the instrument's, not a riff's:
  on `State`, not `StateCore`), panel, Push screen and button, tests.
- Stacks: `sp_seq_at` accessor in `stacks_unit.c`, the `Stacks Harmony`
  port, the sender on the engine goroutine, Plays when closed, tests that
  the accessor agrees with what the core arms, pulse by pulse.

## Tests

- Unit: codec round trips and bad input; chord naming over every root and
  quality; grouping by time and source; the pitch map (on strong beats
  every note is a chord tone of the incoming chord; Sync off is the riff
  exactly); the scheduler with the message heard a pulse early or late.
- Stacks: over many progressions, bars and offsets, the accessor's
  prediction 6 pulses ahead equals the chord the core arms.
- On the device, both apps running with Live: record Riffs Out and
  Stacks Out with times, and check every Riffs note against the Stacks
  chord sounding then; no Riffs note on a chord's first step uses the old
  chord. Then back and forth: switch the surface between the apps while
  playing, edit chords in Stacks while Riffs follows, Stop and Start,
  change tempo and Riffs' rate, Sync off gives the riff back. Notes-only
  and SysEx-only senders (a test tool), and two senders at once.

## Built (2026-10-18)

All of the above, tested: unit tests in all three repos; on the Push,
`scratchpad sync_e2e.py` (Stacks I-VI-IV-V in C, Riffs in F#): every Riffs
note followed the chord Stacks played, chord changes within 3 ms of each
other, in Key and Chords mode, at 1/8, 1/16 and 1/32, with uneven chord
lengths and an offset, edits while playing, a keyboard on Riffs Sync, a
second sender, the surface switched six times while playing, Stop and
Start, Plays when closed on and off, and Sync off giving the riff back.
Riffs and Stacks together used 2-3% CPU each while syncing.
