# Riffs: a Riffer-4-class riff generator for Push 3

Status: planned 2026-10-17. Repo: `rgeissen/push-hack-riffs` (own repo,
catalog hack, like Stacks). Nothing of Audiomodern's code or presets is
used: Riffs is written from scratch in Go, from the manual's description
of what Riffer 4 does. Names and features follow the manual so a Riffer
user is at home; the music it makes is its own.

## What Riffer 4 does (from the manual)

- Six riff lanes, each a melodic pattern. Lane 1 is the main idea, lanes
  2-6 related variations.
- A block grid: lanes x blocks (8 or 16). Active blocks say which lane
  sections play. Mono (one lane per block) or Poly. Playback range,
  complexity, rate (1/16, ...), block shift, lock, reset, random link,
  grid Infinity (with half- and quarter-note intervals).
- Generation engines: Pulse, Hook Riffs, Guitar Hybrid, Lead Arps, Bass
  Hooks, Hybrid. Create (big change) / Remix (subtle) modes, Instant
  (hold for a temporary variation).
- Root, scale, pitch range with Pitch Lock, global and per-lane octave,
  octave randomisation.
- Fill (density) with Link, note shift left/right, swing.
- Infinity: a new generation every N passes (1-16); separate Infinity for
  the grid and the expression lanes.
- Per lane: generate, remix, solo, mute, compress/expand, lock, exclude
  from generation, swap, clear, export.
- Performance keyboard: lane triggers (whole lane), sequencer triggers
  (the lane's active blocks), ALL, Hold; 24 remix/snapshot keys with
  Generate (fills 24 remixes), Move, Quantize, Lock, Clear, Clear All,
  Hold.
- Piano roll: add/remove notes, shift in time and pitch, Fit to scale,
  pitch lock and randomise pitch/duration/velocity/bend, Infinity.
- Expression lanes: Volume (range, +/-, randomise, Infinity), Bend
  (amount, min/max, shapes, Infinity), Stutter (amount, min/max).
- Export MIDI. Presets (factory, user, favourites, tags, packs). MIDI learn,
  plug-in hosting, settings (panic, output per riff, keyboard octaves).

Not ported: plug-in hosting (Live hosts the instrument), MIDI learn (Push
is the controller), preset packs (a folder of JSON is the pack), font
size. Export MIDI writes into Live's User Library so Live's browser finds
it.

## How it plays into Live

As Stacks: two ALSA ports. `Riffs Out` carries the notes; Live connects
it to the selected track by itself. `Riffs In` takes Live's clock (Sync
on, once). Riffs runs on Live's transport and follows Push's scale
(Browser Bridge, Shift + Scale) as Stacks does. Output channel per lane is
a setting (default: all on channel 1).

## The engine (Go, no cgo)

A pattern is `blocks x 4` steps at the rate (1/16 default: a block is a
beat). A note: pitch, start step, length in steps, velocity, bend shape,
stutter count. Six lanes, a grid of lane x block flags, and the settings.

Generation: each engine is a recipe -- register, interval distribution
(step vs leap vs repeat), rhythm density profile per beat, note length
tendencies, rest probability, motif length and how much it repeats,
contour. Lane 1 is generated from the recipe; lanes 2-6 are mutations of
lane 1 (scale-wise transposition, rhythm shift, note substitution,
density change) so the sequence stays related. Remix mutates ~20% of a
lane; Instant mutates and restores on release. Seeded RNG, so a preset
reproduces.

Infinity counts passes of the pattern on the clock. Grid randomisation
respects locks and complexity. Volume/bend/stutter are per-note fields
with their own randomisers and ranges. Export writes a standard MIDI
file (type 1, one track per lane).

## Push surface

Tabs (top buttons): MAIN, GRID, LANE, NOTES, EXPR, PRESETS, KEYS, LIVE.
Eight encoders per tab, bottom buttons as actions, as in Stacks.

- MAIN: Engine, Root, Scale, Octave, Pitch Lo, Pitch Hi, Fill, Swing.
  Actions: CREATE, REMIX, INSTANT (hold), PITCH LOCK, FILL LINK, INFINITY,
  UNDO, REDO.
- GRID: Blocks, Mode (mono/poly), Complexity, Rate, Range Lo, Range Hi,
  Shift, Infinity. Actions: DICE, LOCK, RESET, RANDOM LINK, OCT DICE, OCT
  RESET, UNDO, REDO.
- LANE (the selected lane): Octave, Compress/Expand, Shift, Channel,
  Lock, Exclude, Mute, Solo. Actions: GENERATE, REMIX, CLEAR, SWAP (with
  the next tap), EXPORT, UNDO, REDO.
- NOTES: Pitch Shift, Time Shift, Length, Velocity, Pitch Lock, Randomise
  what (pitch/duration/velocity/bend), Infinity, Lane. Actions: FIT TO
  SCALE, RANDOMISE, REMIX, ...
- EXPR: Volume on, Vol Min, Vol Max, Bend Amount, Bend Max, Stutter
  Amount, Stutter Min, Stutter Max. Actions: VOL DICE, VOL +, VOL -,
  BEND DICE, BEND SHAPES, STUT DICE, infinities.
- PRESETS: browse (jog or encoder), Folder, Tag; actions LOAD, SAVE,
  FAVOURITE, PREVIEW.
- KEYS: Hold (lanes), Hold (blocks), Hold (remixes), Quantize, Move,
  Lock, Bank. Actions: GENERATE 24, CLEAR, CLEAR ALL.
- LIVE: gives the surface back.

Pad views (scene buttons 1/4 .. 1/16t):
- GRID: rows 1-6 = lanes 1-6 (lane 1 at the top, as on the screen), 8
  blocks per page (Page L/R for 9-16). A pad is a block: lit in its lane's
  hue when active, dim when not; the playing block bright. Row 7: lane
  triggers 1-6, ALL, HOLD. Row 8: sequencer triggers 1-6, ALL, HOLD.
- KEYS: rows 1-3 = remix/snapshot keys 1-24 (blue = remix, yellow =
  snapshot, amber = playing); rows 4-5 = the triggers; rows 6-8 = bank B.
- NOTES: a piano roll of the selected lane: 8 steps x 8 scale degrees,
  Page L/R for steps, Octave up/down for pitch. Tap writes or removes a
  note; hold and tap another sets the length.
- VOLUME, BEND, STUTTER: 8 steps x 8 levels, paged.

Hardware buttons: Play/Record/Metronome/Tap/Tempo/Volume pass to Live.
Shift + Help opens and closes. Repeat (held) = INSTANT. Jog turn selects
the lane; jog press = generate in the current mode. D-pad up/down selects
the lane, left/right shifts the blocks. Mute/Solo act on the selected
lane. Delete clears it; Duplicate swaps it with the next lane. Capture
exports MIDI. Octave up/down is the global octave. Undo/Redo (Shift).

Screen: Stacks' frame -- tabs, encoders, a hero band, actions. The hero
on MAIN/GRID/LANE is the six lanes stacked across the full width, blocks
in true time, notes as dots at their pitch, the playing block lit, a
playhead. On NOTES it is the selected lane's piano roll on the scale's
lanes. On KEYS the 48 slots. On PRESETS the list.

## Shared with Stacks

Riffs copies Stacks' host patterns (taking the surface on a release,
encoder steps, LED painting, the dependency watcher, raw frames, scale
following). When #33 lands and `core` is tagged, this host code moves
into `core/surface` and both hacks use it. Noted, not done here.

## Phases

1. Scaffold, engine (lanes, grid, generation, playback on Live's clock),
   HTTP state and a web panel, deploy, hear it.
2. Screen and pads: GRID view, MAIN/GRID/LANE tabs.
3. Performance keys, remixes and snapshots.
4. NOTES editing and the expression lanes.
5. Presets, export, scale following, listen-track reuse where useful.
6. Control matrix, catalog entry.
