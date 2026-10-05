# Push 3 LED Color Palette

**Hardware palette** retrieved via **SysEx command 0x04 "Get LED Color Palette Entry"** from a live Push 3 running firmware 2.4.5b8. 128 entries, indices 0–127.

```
GET http://push.local:7701/api/midi/palette
```

Returns `[{index, r, g, b, w, hex}]` — R/G/B are 8-bit (0–255), W is white balance 0–1024.
Encoding: each field packed as two 7-bit SysEx bytes: `value = lo | (hi << 7)`.

**`WHITE_MIDI_VALUE = 122`** is a constant from Push3's embedded Python (`colors.pyc`).
It matches `palette[122] = #CCCCCC` — confirmed on hardware.

---

## How MIDI Values Map to Colors

Per Push 2/3 spec §2.6.2 RGB LED Color Processing:

> the color index (0…127) is translated into 8-bit red/green/blue values using the color palette

MIDI value → hardware palette index → RGB. **Direct indexing** for both LED types:

| LED type | MIDI message | How value maps |
|----------|-------------|----------------|
| **Pad** | Note On, note 36–99 | velocity = palette index |
| **CC button** | CC | value = palette index |

Same palette table. Same indexing. `WHITE_MIDI_VALUE = 122` → `palette[122] = #CCCCCC` ✓

---

## Key Values

| Index | Hex | Notes |
|-------|-----|-------|
| 0 | `#000000` | Off / black |
| 1 | `#FF4032` | Vivid red (first Live primary color) |
| 120 | `#FFFFFF` | Pure white |
| 122 | `#CCCCCC` | `WHITE_MIDI_VALUE` — Ableton standard lit-white button |
| 125 | `#0000FF` | Pure blue |
| 126 | `#00FF00` | Pure green |
| 127 | `#FF0000` | Pure red |

**Named color slugs** (for `namedColors` in push-manager LED config):

| Slug | Index | Hex |
|------|-------|-----|
| `off` | 0 | `#000000` |
| `red` | 1 | `#FF4032` |
| `yellow` | 7 | `#FADC3B` |
| `lime` | 9 | `#B6FF0E` |
| `green` | 11 | `#34C216` |
| `sky` | 16 | `#31ADFF` |
| `blue` | 17 | `#3663FC` |
| `violet` | 23 | `#972BFF` |
| `pink` | 26 | `#FF2BD4` |
| `white` | 120 | `#FFFFFF` |
| `white_btn` | 122 | `#CCCCCC` (Ableton standard) |
| `pure_blue` | 125 | `#0000FF` |
| `pure_green` | 126 | `#00FF00` |
| `pure_red` | 127 | `#FF0000` |

---

## Full Hardware Palette (SysEx 0x04, verified 2026-05-26)

R/G/B = 8-bit. W = white balance (0–1024). Palette queried live from Push 3 hardware.

| Index | Hex | R | G | B | W | Description |
|-------|-----|---|---|---|---|-------------|
| 0 | `#000000` | 0 | 0 | 0 | 0 | Off / black |
| 1 | `#FF4032` | 255 | 64 | 50 | 2 | Vivid warm red |
| 2 | `#800400` | 128 | 4 | 0 | 4 | Very dark red |
| 3 | `#C93C00` | 201 | 60 | 0 | 6 | Dark orange-red |
| 4 | `#AC1F00` | 172 | 31 | 0 | 8 | Dark brownish-red |
| 5 | `#8C5018` | 140 | 80 | 24 | 10 | Warm brown |
| 6 | `#491804` | 73 | 24 | 4 | 12 | Very dark brown |
| 7 | `#FADC3B` | 250 | 220 | 59 | 14 | Vivid yellow |
| 8 | `#FFC516` | 255 | 197 | 22 | 16 | Warm amber |
| 9 | `#B6FF0E` | 182 | 255 | 14 | 18 | Vivid yellow-green / lime |
| 10 | `#79FF18` | 121 | 255 | 24 | 20 | Vivid bright green |
| 11 | `#34C216` | 52 | 194 | 22 | 22 | Medium green |
| 12 | `#4F8A04` | 79 | 138 | 4 | 24 | Dark olive |
| 13 | `#62FF55` | 98 | 255 | 85 | 26 | Very vivid lime green |
| 14 | `#297D53` | 41 | 125 | 83 | 28 | Dark teal-green |
| 15 | `#269E72` | 38 | 158 | 114 | 30 | Medium teal |
| 16 | `#31ADFF` | 49 | 173 | 255 | 32 | Sky blue |
| 17 | `#3663FC` | 54 | 99 | 252 | 34 | Vivid blue |
| 18 | `#1A34FF` | 26 | 52 | 255 | 36 | Dark blue |
| 19 | `#1C0CE6` | 28 | 12 | 230 | 38 | Navy |
| 20 | `#153999` | 21 | 57 | 153 | 40 | Dark navy |
| 21 | `#3937FF` | 57 | 55 | 255 | 42 | Vivid indigo/purple-blue |
| 22 | `#5722FF` | 87 | 34 | 255 | 44 | Vivid purple |
| 23 | `#972BFF` | 151 | 43 | 255 | 46 | Violet/purple-magenta |
| 24 | `#852178` | 133 | 33 | 120 | 48 | Dark magenta/plum |
| 25 | `#FF1032` | 255 | 16 | 50 | 50 | Crimson/vivid red-pink |
| 26 | `#FF2BD4` | 255 | 43 | 212 | 52 | Vivid hot pink |
| 27 | `#A63421` | 166 | 52 | 33 | 54 | Maroon |
| 28 | `#995628` | 153 | 86 | 40 | 56 | Sienna/reddish brown |
| 29 | `#876700` | 135 | 103 | 0 | 58 | Dark gold |
| 30 | `#90821F` | 144 | 130 | 31 | 60 | Khaki/dark yellow-green |
| 31 | `#4A8700` | 74 | 135 | 0 | 62 | Dark olive-green |
| 32 | `#007F12` | 0 | 127 | 18 | 64 | Forest green |
| 33 | `#1853B2` | 24 | 83 | 178 | 66 | Cobalt blue |
| 34 | `#624BAD` | 98 | 75 | 173 | 68 | Medium purple |
| 35 | `#733A67` | 115 | 58 | 103 | 70 | Plum/dark magenta |
| 36 | `#F8BCAF` | 248 | 188 | 175 | 72 | Light salmon/pink |
| 37 | `#FF9B76` | 255 | 155 | 118 | 74 | Peach |
| 38 | `#FFBF5F` | 255 | 191 | 95 | 76 | Light gold |
| 39 | `#D9AF71` | 217 | 175 | 113 | 78 | Tan |
| 40 | `#FFF480` | 255 | 244 | 128 | 80 | Pastel yellow |
| 41 | `#BFBA69` | 191 | 186 | 105 | 80 | — |
| 42 | `#BCCC88` | 188 | 204 | 136 | 81 | Sage green |
| 43 | `#AEFF99` | 174 | 255 | 153 | 81 | — |
| 44 | `#7CDD9F` | 124 | 221 | 159 | 82 | Mint |
| 45 | `#89B47D` | 137 | 180 | 125 | 82 | — |
| 46 | `#80F3FF` | 128 | 243 | 255 | 83 | Light cyan |
| 47 | `#7ACEFC` | 122 | 206 | 252 | 83 | — |
| 48 | `#68A1D3` | 104 | 161 | 211 | 84 | Muted blue |
| 49 | `#858FC2` | 133 | 143 | 194 | 85 | Periwinkle |
| 50 | `#BBAAF2` | 187 | 170 | 242 | 85 | — |
| 51 | `#CDBBE4` | 205 | 187 | 228 | 86 | Lavender |
| 52 | `#EF8BB0` | 239 | 139 | 176 | 86 | — |
| 53 | `#859D8C` | 133 | 157 | 140 | 87 | Dark sage |
| 54 | `#6B756E` | 107 | 117 | 110 | 87 | — |
| 55 | `#84909B` | 132 | 144 | 155 | 88 | Steel blue-gray |
| 56 | `#6A7075` | 106 | 112 | 117 | 88 | — |
| 57 | `#88859D` | 136 | 133 | 157 | 89 | Mauve |
| 58 | `#6C6A75` | 108 | 106 | 117 | 90 | Blue-gray |
| 59 | `#9D859C` | 157 | 133 | 156 | 90 | — |
| 60 | `#746A74` | 116 | 106 | 116 | 91 | Gray-mauve |
| 61 | `#9C9D85` | 156 | 157 | 133 | 91 | — |
| 62 | `#74756A` | 116 | 117 | 106 | 92 | Gray-green |
| 63 | `#9D8484` | 157 | 132 | 132 | 92 | — |
| 64 | `#756A6A` | 117 | 106 | 106 | 93 | Warm gray |
| 65 | `#661914` | 102 | 25 | 20 | 93 | — |
| 66 | `#210806` | 33 | 8 | 6 | 94 | Very dark wine |
| 67 | `#460300` | 70 | 3 | 0 | 94 | — |
| 68 | `#280000` | 40 | 0 | 0 | 95 | Very dark red |
| 69 | `#5D1700` | 93 | 23 | 0 | 96 | Very dark orange |
| 70 | `#200D00` | 32 | 13 | 0 | 96 | — |
| 71 | `#470C00` | 71 | 12 | 0 | 97 | Very dark maroon |
| 72 | `#1C0800` | 28 | 8 | 0 | 97 | — |
| 73 | `#3B2B14` | 59 | 43 | 20 | 98 | Very dark brown |
| 74 | `#1C130A` | 28 | 19 | 10 | 98 | — |
| 75 | `#250E05` | 37 | 14 | 5 | 99 | Very dark sienna |
| 76 | `#0D0602` | 13 | 6 | 2 | 99 | — |
| 77 | `#645817` | 100 | 88 | 23 | 100 | Very dark gold |
| 78 | `#201C07` | 32 | 28 | 7 | 101 | — |
| 79 | `#664E08` | 102 | 78 | 8 | 101 | Very dark olive |
| 80 | `#211902` | 33 | 25 | 2 | 102 | — |
| 81 | `#486605` | 72 | 102 | 5 | 102 | Very dark lime |
| 82 | `#172101` | 23 | 33 | 1 | 103 | — |
| 83 | `#306609` | 48 | 102 | 9 | 103 | Very dark forest |
| 84 | `#0F2103` | 15 | 33 | 3 | 104 | — |
| 85 | `#144D08` | 20 | 77 | 8 | 104 | Very dark green |
| 86 | `#061902` | 6 | 25 | 2 | 105 | — |
| 87 | `#1F3701` | 31 | 55 | 1 | 106 | Very dark teal-green |
| 88 | `#0A1100` | 10 | 17 | 0 | 106 | — |
| 89 | `#276622` | 39 | 102 | 34 | 107 | Very dark emerald |
| 90 | `#0C210B` | 12 | 33 | 11 | 107 | — |
| 91 | `#143E29` | 20 | 62 | 41 | 108 | Very dark pine |
| 92 | `#081910` | 8 | 25 | 16 | 108 | — |
| 93 | `#004D36` | 0 | 77 | 54 | 109 | Very dark teal |
| 94 | `#00180E` | 0 | 24 | 14 | 109 | — |
| 95 | `#134566` | 19 | 69 | 102 | 110 | Very dark ocean |
| 96 | `#061621` | 6 | 22 | 33 | 110 | — |
| 97 | `#152764` | 21 | 39 | 100 | 111 | Very dark cobalt |
| 98 | `#070C20` | 7 | 12 | 32 | 112 | Very dark navy |
| 99 | `#0A1466` | 10 | 20 | 102 | 112 | — |
| 100 | `#030621` | 3 | 6 | 33 | 113 | Very dark indigo |
| 101 | `#0B045C` | 11 | 4 | 92 | 113 | — |
| 102 | `#03011D` | 3 | 1 | 29 | 114 | Near-black |
| 103 | `#0A1C4C` | 10 | 28 | 76 | 114 | Very dark navy 2 |
| 104 | `#040B1E` | 4 | 11 | 30 | 115 | — |
| 105 | `#161666` | 22 | 22 | 102 | 115 | Very dark purple |
| 106 | `#070721` | 7 | 7 | 33 | 116 | — |
| 107 | `#220D66` | 34 | 13 | 102 | 117 | Very dark violet |
| 108 | `#0B0421` | 11 | 4 | 33 | 117 | — |
| 109 | `#3C1166` | 60 | 17 | 102 | 118 | Very dark magenta |
| 110 | `#130521` | 19 | 5 | 33 | 118 | — |
| 111 | `#350D30` | 53 | 13 | 48 | 119 | Very dark plum |
| 112 | `#11040F` | 17 | 4 | 15 | 119 | — |
| 113 | `#660614` | 102 | 6 | 20 | 120 | Very dark crimson |
| 114 | `#210206` | 33 | 2 | 6 | 120 | — |
| 115 | `#661154` | 102 | 17 | 84 | 121 | Very dark pink |
| 116 | `#21051B` | 33 | 5 | 27 | 122 | Very dark purple (≈ near-black) |
| 117 | `#000000` | 0 | 0 | 0 | 122 | Black (second black entry) |
| 118 | `#595959` | 89 | 89 | 89 | 123 | Medium gray |
| 119 | `#1A1A1A` | 26 | 26 | 26 | 123 | Dark gray |
| 120 | `#FFFFFF` | 255 | 255 | 255 | 124 | **Pure white** |
| 121 | `#595959` | 89 | 89 | 89 | 124 | Medium gray 2 |
| 122 | `#CCCCCC` | 204 | 204 | 204 | 125 | **`WHITE_MIDI_VALUE`** — Ableton standard lit button |
| 123 | `#404040` | 64 | 64 | 64 | 125 | Dark gray 2 |
| 124 | `#141414` | 20 | 20 | 20 | 126 | Near-black gray |
| 125 | `#0000FF` | 0 | 0 | 255 | 126 | Pure blue |
| 126 | `#00FF00` | 0 | 255 | 0 | 127 | Pure green |
| 127 | `#FF0000` | 255 | 0 | 0 | 127 | Pure red |

---

## Curated subsets for user-facing color pickers

The 128-entry table above is the full verified hardware palette — every
index a pad/button LED can be set to. It is **not** the set a user should
actually be offered when an app lets them pick "this track/clip's color"
from the pad grid or a swatch list. Two narrower, non-hardware-sourced
observations feed into that kind of picker, worth recording here even
though they aren't SysEx-verified the way the table above is:

- **Live itself only uses a subset of this palette for track/clip
  colors** — around 70 of the 128 entries, not all of them (Live's own
  color picker is a fixed swatch grid, not a full 128-index chooser).
- **Push's own official color-choose UI narrows further, to around 26
  entries** — evidently curated for what actually reads as distinct on
  the small, low-res pad LEDs (muddy/dark/near-duplicate hardware
  entries dropped). This 26-ish count was confirmed independently by
  hand-testing colors on real Push hardware while building a pad-grid
  track-color picker for the `gridseq` process module (see
  `push-tethered-app`'s catalog) — its `engine.py` `TRACK_COLORS` list is
  one concrete instance of such a subset, and its Shift-held /
  border-pads-light-up picker (`Engine.enter_color_picker`,
  `color_picker_grid`) mirrors what Push's own hardware color-choose flow
  does.

**Suggestion, not a rule:** if this repo's `core/push3` package ever grows
a shared "assignable item color" concept (a `TrackColors`/`ClipColors`-
style curated index list, or a reusable color-picker helper for process
modules), the `gridseq` module is worth pulling in as reference prior art
before inventing the subset from scratch again. Until then, any app
offering a track/clip-style color choice should stick to a *hand-tested-
on-hardware* subset around this size rather than exposing the raw 128 —
that's a design recommendation for readability on Push's pad LEDs, not a
hardware constraint.

---

## How to Test a Value

```bash
# Pad: Note On, pad 36 (bottom-left), velocity = palette index
curl -X POST http://push.local:7701/api/midi/led \
  -H 'Content-Type: application/json' \
  -d '{"type":"note","channel":0,"note":36,"velocity":<N>}'

# Button: CC 102 (top-left screen button), value = palette index
curl -X POST http://push.local:7701/api/midi/led \
  -H 'Content-Type: application/json' \
  -d '{"type":"cc","channel":0,"cc":102,"value":<N>}'

# Re-query hardware palette
curl http://push.local:7701/api/midi/palette | jq .
```

---

**Use the hardware palette table above as the definitive source for MIDI values.**

---

## Addendum (2026-10-05): pad colors versus screen colors

Found while building the `bcaseq` process module (`pta-module-bcaseq`). It
draws the same track colors on the pads and on the screen. They did not match.
There were three causes.

### 1. A palette with only the named entries is wrong for most dark colors

`core/push3.Palette` (and the `palette.json` that `cmd/genpalette` writes from
it) holds only the 90 named entries. `ColorForIndex` returns the nearest named
entry at or below the index. For an index with no name, such as 41, 43 or 67,
that is the wrong color. 38 of the 128 indices differ from the table above.
The 26 primary colors (1-26) are correct.

If a module draws any index above 40 on the screen, build its palette from the
"Full Hardware Palette" table in this file. `bcaseq` does this with
`scripts/gen_palette.py`, which reads the table rows and writes all 128
entries.

### 2. The screen draws a track color differently from the pad LED

The original Push draws each of the 26 primary colors on its screen with
different RGB values than the LED palette. The two are close for the reds,
yellows and greens. They differ for the blues, teals, violets and pinks. Index
25 is crimson on the pads and pink on the screen. Index 26 is hot pink on the
pads and red on the screen.

Method: in Live, set 26 MIDI tracks to palette indices 1 to 26, in order. Take
a screenshot of the Push screen. The track name text is drawn in the track
color. The text peaks at 96% of the real color. This was measured with the
solid fill of the selected track and the "No device" header. Each value in the
table is the brightest text pixel divided by 0.96. The three selected tracks
(1, 16 and 24) use their solid fill.

To match the original Push, draw the screen color from the "Screen" column, and
send the index in the "Pad" column to the LED.

### 3. Dim pad shades

The palette has "very dark" entries from index 65 on. They come in pairs of
neighbors for each hue family. On the pads, the second entry of each pair gave
the best "empty step" look for a sequencer (full color for a step that is on,
dim for an empty step, white for the selected step). Color 2 is the exception:
it uses 68.

| Index | Pad (palette) | Screen (original Push) | Dim index | Dim (palette) |
|-------|---------------|------------------------|-----------|---------------|
| 1 | `#FF4032` | `#ED5938` | 66 | `#210806` |
| 2 | `#800400` | `#D31709` | 68 | `#280000` |
| 3 | `#C93C00` | `#FB6200` | 70 | `#200D00` |
| 4 | `#AC1F00` | `#FF3300` | 72 | `#1C0800` |
| 5 | `#8C5018` | `#AA7320` | 74 | `#1C130A` |
| 6 | `#491804` | `#844913` | 76 | `#0D0602` |
| 7 | `#FADC3B` | `#F7E33E` | 78 | `#201C07` |
| 8 | `#FFC516` | `#E1C000` | 80 | `#211902` |
| 9 | `#B6FF0E` | `#93FD17` | 82 | `#172101` |
| 10 | `#79FF18` | `#00EB32` | 84 | `#0F2103` |
| 11 | `#34C216` | `#009F33` | 86 | `#061902` |
| 12 | `#4F8A04` | `#34A013` | 88 | `#0A1100` |
| 13 | `#62FF55` | `#00BE56` | 90 | `#0C210B` |
| 14 | `#297D53` | `#007450` | 92 | `#081910` |
| 15 | `#269E72` | `#00D08C` | 94 | `#00180E` |
| 16 | `#31ADFF` | `#00BBAD` | 96 | `#061621` |
| 17 | `#3663FC` | `#0072A6` | 98 | `#070C20` |
| 18 | `#1A34FF` | `#006CCE` | 100 | `#030621` |
| 19 | `#1C0CE6` | `#4A33B6` | 102 | `#03011D` |
| 20 | `#153999` | `#005C64` | 104 | `#040B1E` |
| 21 | `#3937FF` | `#5362E1` | 106 | `#070721` |
| 22 | `#5722FF` | `#AE51FF` | 108 | `#0B0421` |
| 23 | `#972BFF` | `#E559E7` | 110 | `#130521` |
| 24 | `#852178` | `#88425B` | 112 | `#11040F` |
| 25 | `#FF1032` | `#FF4B99` | 114 | `#210206` |
| 26 | `#FF2BD4` | `#FF1E33` | 116 | `#21051B` |

The dim index was chosen by eye on a Push (user test, 2026-10-05). The screen
values come from screenshots, so they have an error of about 3%.

Tool: the `colorlab-py` example module in `push-tethered-app`
(`examples/modules/colorlab-py`) lets you pick a pad color and a dim shade on
the pads and compare screen swatches on the Push. It writes a `colors.json`
file.
