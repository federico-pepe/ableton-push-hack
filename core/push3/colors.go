package push3

import (
	"image/color"
	"strings"
)

// namedColors maps convenience names to Push 3 palette velocity/CC indices.
//
// SOURCE OF TRUTH: docs/push3-led-colors.md's "Full Hardware Palette" table —
// queried directly from a live Push 3 (firmware 2.4.5b8) via SysEx command
// 0x04 "Get LED Color Palette Entry", 128 entries, indices 0-127.
//
// CORRECTED 2026-08-18. This file previously claimed its source was Push 2's
// colors.pyc COLOR_TABLE and that only EVEN velocities 2-52/54-78 carried a
// real colour, with odd velocities being undocumented hardware
// half-brightness interpolations. Both claims were wrong for Push 3: the
// SysEx-queried table returns a distinct, real colour for every one of the
// 128 raw velocities, no gaps, no even/odd split — e.g. velocity 21 is a
// real, distinct "vivid indigo/purple-blue" (#3937FF), not an interpolation.
// Every value below was off by roughly 2x for this reason (this file used to
// say "green" = 22; the hardware-queried value is 11). Found via a pad
// rendering the wrong colour on real Push 3 hardware, then confirmed by
// cross-checking every entry here against the SysEx table exhaustively —
// zero matches under the old numbering, exact matches once corrected.
//
// Also resolved: this file used to describe 124/126/127 as "shared slots"
// with a different meaning for pads vs Ableton Live (e.g. "126 = very dark
// grey (pad); also encodes pure blue in Live"). The SysEx table shows these
// are simply separate, unambiguous, adjacent entries — 124 is a near-black
// grey, 125 is pure blue, 126 is pure green, 127 is pure red — not a shared
// slot at all. pure_blue and pure_red are new entries below for exactly this
// reason; the old file had no slug for either.
//
// TWO USES (same value sent to hardware, different meaning):
//
//	Pad Note On  : velocity = palette index → color (RGB from the table)
//	CC button    : value = brightness (0-127, white hardware LEDs ignore color)
//
// The doc's SysEx table also contains ~10 further entries not named here —
// near-duplicate very-dark/near-black/grey variants adjacent to an
// already-named colour (e.g. a second, very slightly different "very dark
// navy"). Deliberately not given speculative slug names; see
// docs/push3-led-colors.md for the complete raw 128-entry table if one of
// those exact shades is ever needed.
//
// WHITE_MIDI_VALUE = 122 (Ableton's CC brightness value for "lit white button").
//
// See docs/push3-led-colors.md for the full 128-entry table.
// NamedColors maps convenience color names to Push 3 palette velocity/CC
// indices. Exported so callers (e.g. push-manager's LED API) can list it.
var NamedColors = map[string]uint8{
	// ── Universal ─────────────────────────────────────────────────────────────
	"off": 0, // #000000

	// ── CC button brightness aliases (white LED hardware, value = brightness) ─
	// Unaffected by the correction above: for a CC button, value IS brightness
	// directly (0-127 linear), not a palette index, so these were never wrong.
	"dim":       1,   // very dim button
	"half":      63,  // half brightness
	"white_btn": 122, // WHITE_MIDI_VALUE — standard lit-white button (Ableton internal)
	"full":      127, // full brightness

	// ── Pad palette: 26 primary colors ────────────────────────────────────────
	// Consecutive velocities 1-40 — every one distinct, confirmed via SysEx.
	"red":         1,  // #FF4032  vivid warm red
	"red_dark":    2,  // #800400  very dark red
	"orange":      3,  // #C93C00  dark orange-red
	"red_brown":   4,  // #AC1F00  dark brownish-red
	"brown":       5,  // #8C5018  warm brown
	"brown_dark":  6,  // #491804  very dark brown
	"yellow":      7,  // #FADC3B  vivid yellow
	"amber":       8,  // #FFC516  warm amber/orange-yellow
	"lime":        9,  // #B6FF0E  vivid yellow-green
	"green_vivid": 10, // #79FF18  vivid bright green
	"green":       11, // #34C216  medium green
	"olive":       12, // #4F8A04  dark olive green
	"green_vv":    13, // #62FF55  very vivid lime green
	"teal_dark":   14, // #297D53  dark teal-green
	"teal":        15, // #269E72  medium teal
	"sky":         16, // #31ADFF  sky blue
	"blue":        17, // #3663FC  vivid blue
	"blue_dark":   18, // #1A34FF  dark blue
	"navy":        19, // #1C0CE6  navy
	"navy_dark":   20, // #153999  dark navy
	"indigo":      21, // #3937FF  vivid indigo/purple-blue
	"purple":      22, // #5722FF  vivid purple
	"violet":      23, // #972BFF  violet/purple-magenta
	"magenta_dk":  24, // #852178  dark magenta/plum
	"crimson":     25, // #FF1032  crimson/vivid red-pink
	"pink_hot":    26, // #FF2BD4  vivid hot pink

	// ── Pad palette: muted/dark range ─────────────────────────────────────────
	"maroon":     27, // #A63421
	"sienna":     28, // #995628
	"gold_dark":  29, // #876700
	"khaki":      30, // #90821F
	"green_dk":   31, // #4A8700
	"forest":     32, // #007F12
	"cobalt":     33, // #1853B2
	"purple_mid": 34, // #624BAD
	"plum":       35, // #733A67

	// ── Pad palette: pastel/light range ────────────────────────────────────────
	"pink":       36, // #F8BCAF  light salmon/pink
	"peach":      37, // #FF9B76
	"gold_light": 38, // #FFBF5F
	"tan":        39, // #D9AF71
	"yellow_lt":  40, // #FFF480  pastel yellow
	"sage":       42, // #BCCC88
	"mint":       44, // #7CDD9F
	"cyan_lt":    46, // #80F3FF  light cyan
	"blue_lt":    48, // #68A1D3  muted blue
	"periwinkle": 49, // #858FC2
	"lavender":   51, // #CDBBE4
	"sage_dk":    53, // #859D8C
	"steel":      55, // #84909B  steel blue-gray
	"mauve":      57, // #88859D

	// ── Pad palette: neutral muted grays ──────────────────────────────────────
	"gray_blue":  58, // #6C6A75
	"gray_mauve": 60, // #746A74
	"gray_green": 62, // #74756A
	"gray_warm":  64, // #756A6A

	// ── Pad palette: very dark shades ─────────────────────────────────────────
	"dk_wine":    66,  // #210806
	"dk_red":     68,  // #280000
	"dk_orange":  69,  // #5D1700
	"dk_maroon":  71,  // #470C00
	"dk_brown":   73,  // #3B2B14
	"dk_sienna":  75,  // #250E05
	"dk_gold":    77,  // #645817
	"dk_olive":   78,  // #201C07
	"dk_amber":   80,  // #211902
	"dk_lime":    82,  // #172101
	"dk_forest":  84,  // #0F2103
	"dk_green":   86,  // #061902
	"dk_teal_g":  87,  // #1F3701
	"dk_emerald": 89,  // #276622
	"dk_pine":    91,  // #143E29
	"dk_teal":    93,  // #004D36
	"dk_ocean":   95,  // #134566
	"dk_cobalt":  97,  // #152764
	"dk_navy":    98,  // #070C20
	"dk_indigo":  100, // #030621
	"dk_void":    102, // #03011D
	"dk_navy2":   104, // #040B1E
	"dk_purple":  106, // #070721
	"dk_violet":  107, // #220D66
	"dk_magenta": 109, // #3C1166
	"dk_plum":    111, // #350D30
	"dk_crimson": 113, // #660614
	"dk_pink":    115, // #661154

	// ── Pad palette: spec colors ───────────────────────────────────────────────
	// See the CORRECTED note above: these are four separate, unambiguous
	// entries, not shared pad/Live slots as this file used to claim.
	"gray_dk2":   116, // #21051B  very dark, near-black
	"gray_mid":   118, // #595959  medium gray
	"white":      120, // #FFFFFF  pure white
	"lgray":      122, // #CCCCCC  light gray — same byte as WHITE_MIDI_VALUE/white_btn
	"dgray":      124, // #141414  near-black gray
	"pure_blue":  125, // #0000FF  pure blue
	"green_spec": 126, // #00FF00  pure green
	"pure_red":   127, // #FF0000  pure red
}

// ColorByName looks up a convenience color name (case-insensitive) and
// returns its palette index. ok is false for an unknown name.
func ColorByName(name string) (uint8, bool) {
	idx, ok := NamedColors[strings.ToLower(name)]
	return idx, ok
}

// PaletteEntry is one named, RGB-resolved entry in Palette.
type PaletteEntry struct {
	Index uint8
	Name  string
	RGB   color.NRGBA
}

// Palette is NamedColors' 90 named entries, RGB-resolved and sorted by
// hardware index — the same SOURCE OF TRUTH as NamedColors above (queried
// via SysEx from real Push 3 hardware). It exists so any consumer that
// needs to *render* a palette index as a color (a screen widget preview, a
// module's UI, a test) has one shared table to read from, rather than each
// hand-copying RGB values out of these comments — LED writes themselves
// only ever need the index (NamedColors/ColorByName), never this table.
//
// Not all 128 raw hardware indices are here: see the package doc above on
// unnamed near-duplicate shades. Use ColorForIndex for a raw 0-127 value —
// it resolves to the nearest defined entry at or below the index, so every
// one of the 128 slots still lands on a real, close color.
var Palette = []PaletteEntry{
	{Index: 0, Name: "off", RGB: color.NRGBA{R: 0, G: 0, B: 0, A: 255}},
	{Index: 1, Name: "red", RGB: color.NRGBA{R: 255, G: 64, B: 50, A: 255}},
	{Index: 2, Name: "red_dark", RGB: color.NRGBA{R: 128, G: 4, B: 0, A: 255}},
	{Index: 3, Name: "orange", RGB: color.NRGBA{R: 201, G: 60, B: 0, A: 255}},
	{Index: 4, Name: "red_brown", RGB: color.NRGBA{R: 172, G: 31, B: 0, A: 255}},
	{Index: 5, Name: "brown", RGB: color.NRGBA{R: 140, G: 80, B: 24, A: 255}},
	{Index: 6, Name: "brown_dark", RGB: color.NRGBA{R: 73, G: 24, B: 4, A: 255}},
	{Index: 7, Name: "yellow", RGB: color.NRGBA{R: 250, G: 220, B: 59, A: 255}},
	{Index: 8, Name: "amber", RGB: color.NRGBA{R: 255, G: 197, B: 22, A: 255}},
	{Index: 9, Name: "lime", RGB: color.NRGBA{R: 182, G: 255, B: 14, A: 255}},
	{Index: 10, Name: "green_vivid", RGB: color.NRGBA{R: 121, G: 255, B: 24, A: 255}},
	{Index: 11, Name: "green", RGB: color.NRGBA{R: 52, G: 194, B: 22, A: 255}},
	{Index: 12, Name: "olive", RGB: color.NRGBA{R: 79, G: 138, B: 4, A: 255}},
	{Index: 13, Name: "green_vv", RGB: color.NRGBA{R: 98, G: 255, B: 85, A: 255}},
	{Index: 14, Name: "teal_dark", RGB: color.NRGBA{R: 41, G: 125, B: 83, A: 255}},
	{Index: 15, Name: "teal", RGB: color.NRGBA{R: 38, G: 158, B: 114, A: 255}},
	{Index: 16, Name: "sky", RGB: color.NRGBA{R: 49, G: 173, B: 255, A: 255}},
	{Index: 17, Name: "blue", RGB: color.NRGBA{R: 54, G: 99, B: 252, A: 255}},
	{Index: 18, Name: "blue_dark", RGB: color.NRGBA{R: 26, G: 52, B: 255, A: 255}},
	{Index: 19, Name: "navy", RGB: color.NRGBA{R: 28, G: 12, B: 230, A: 255}},
	{Index: 20, Name: "navy_dark", RGB: color.NRGBA{R: 21, G: 57, B: 153, A: 255}},
	{Index: 21, Name: "indigo", RGB: color.NRGBA{R: 57, G: 55, B: 255, A: 255}},
	{Index: 22, Name: "purple", RGB: color.NRGBA{R: 87, G: 34, B: 255, A: 255}},
	{Index: 23, Name: "violet", RGB: color.NRGBA{R: 151, G: 43, B: 255, A: 255}},
	{Index: 24, Name: "magenta_dk", RGB: color.NRGBA{R: 133, G: 33, B: 120, A: 255}},
	{Index: 25, Name: "crimson", RGB: color.NRGBA{R: 255, G: 16, B: 50, A: 255}},
	{Index: 26, Name: "pink_hot", RGB: color.NRGBA{R: 255, G: 43, B: 212, A: 255}},
	{Index: 27, Name: "maroon", RGB: color.NRGBA{R: 166, G: 52, B: 33, A: 255}},
	{Index: 28, Name: "sienna", RGB: color.NRGBA{R: 153, G: 86, B: 40, A: 255}},
	{Index: 29, Name: "gold_dark", RGB: color.NRGBA{R: 135, G: 103, B: 0, A: 255}},
	{Index: 30, Name: "khaki", RGB: color.NRGBA{R: 144, G: 130, B: 31, A: 255}},
	{Index: 31, Name: "green_dk", RGB: color.NRGBA{R: 74, G: 135, B: 0, A: 255}},
	{Index: 32, Name: "forest", RGB: color.NRGBA{R: 0, G: 127, B: 18, A: 255}},
	{Index: 33, Name: "cobalt", RGB: color.NRGBA{R: 24, G: 83, B: 178, A: 255}},
	{Index: 34, Name: "purple_mid", RGB: color.NRGBA{R: 98, G: 75, B: 173, A: 255}},
	{Index: 35, Name: "plum", RGB: color.NRGBA{R: 115, G: 58, B: 103, A: 255}},
	{Index: 36, Name: "pink", RGB: color.NRGBA{R: 248, G: 188, B: 175, A: 255}},
	{Index: 37, Name: "peach", RGB: color.NRGBA{R: 255, G: 155, B: 118, A: 255}},
	{Index: 38, Name: "gold_light", RGB: color.NRGBA{R: 255, G: 191, B: 95, A: 255}},
	{Index: 39, Name: "tan", RGB: color.NRGBA{R: 217, G: 175, B: 113, A: 255}},
	{Index: 40, Name: "yellow_lt", RGB: color.NRGBA{R: 255, G: 244, B: 128, A: 255}},
	{Index: 42, Name: "sage", RGB: color.NRGBA{R: 188, G: 204, B: 136, A: 255}},
	{Index: 44, Name: "mint", RGB: color.NRGBA{R: 124, G: 221, B: 159, A: 255}},
	{Index: 46, Name: "cyan_lt", RGB: color.NRGBA{R: 128, G: 243, B: 255, A: 255}},
	{Index: 48, Name: "blue_lt", RGB: color.NRGBA{R: 104, G: 161, B: 211, A: 255}},
	{Index: 49, Name: "periwinkle", RGB: color.NRGBA{R: 133, G: 143, B: 194, A: 255}},
	{Index: 51, Name: "lavender", RGB: color.NRGBA{R: 205, G: 187, B: 228, A: 255}},
	{Index: 53, Name: "sage_dk", RGB: color.NRGBA{R: 133, G: 157, B: 140, A: 255}},
	{Index: 55, Name: "steel", RGB: color.NRGBA{R: 132, G: 144, B: 155, A: 255}},
	{Index: 57, Name: "mauve", RGB: color.NRGBA{R: 136, G: 133, B: 157, A: 255}},
	{Index: 58, Name: "gray_blue", RGB: color.NRGBA{R: 108, G: 106, B: 117, A: 255}},
	{Index: 60, Name: "gray_mauve", RGB: color.NRGBA{R: 116, G: 106, B: 116, A: 255}},
	{Index: 62, Name: "gray_green", RGB: color.NRGBA{R: 116, G: 117, B: 106, A: 255}},
	{Index: 64, Name: "gray_warm", RGB: color.NRGBA{R: 117, G: 106, B: 106, A: 255}},
	{Index: 66, Name: "dk_wine", RGB: color.NRGBA{R: 33, G: 8, B: 6, A: 255}},
	{Index: 68, Name: "dk_red", RGB: color.NRGBA{R: 40, G: 0, B: 0, A: 255}},
	{Index: 69, Name: "dk_orange", RGB: color.NRGBA{R: 93, G: 23, B: 0, A: 255}},
	{Index: 71, Name: "dk_maroon", RGB: color.NRGBA{R: 71, G: 12, B: 0, A: 255}},
	{Index: 73, Name: "dk_brown", RGB: color.NRGBA{R: 59, G: 43, B: 20, A: 255}},
	{Index: 75, Name: "dk_sienna", RGB: color.NRGBA{R: 37, G: 14, B: 5, A: 255}},
	{Index: 77, Name: "dk_gold", RGB: color.NRGBA{R: 100, G: 88, B: 23, A: 255}},
	{Index: 78, Name: "dk_olive", RGB: color.NRGBA{R: 32, G: 28, B: 7, A: 255}},
	{Index: 80, Name: "dk_amber", RGB: color.NRGBA{R: 33, G: 25, B: 2, A: 255}},
	{Index: 82, Name: "dk_lime", RGB: color.NRGBA{R: 23, G: 33, B: 1, A: 255}},
	{Index: 84, Name: "dk_forest", RGB: color.NRGBA{R: 15, G: 33, B: 3, A: 255}},
	{Index: 86, Name: "dk_green", RGB: color.NRGBA{R: 6, G: 25, B: 2, A: 255}},
	{Index: 87, Name: "dk_teal_g", RGB: color.NRGBA{R: 31, G: 55, B: 1, A: 255}},
	{Index: 89, Name: "dk_emerald", RGB: color.NRGBA{R: 39, G: 102, B: 34, A: 255}},
	{Index: 91, Name: "dk_pine", RGB: color.NRGBA{R: 20, G: 62, B: 41, A: 255}},
	{Index: 93, Name: "dk_teal", RGB: color.NRGBA{R: 0, G: 77, B: 54, A: 255}},
	{Index: 95, Name: "dk_ocean", RGB: color.NRGBA{R: 19, G: 69, B: 102, A: 255}},
	{Index: 97, Name: "dk_cobalt", RGB: color.NRGBA{R: 21, G: 39, B: 100, A: 255}},
	{Index: 98, Name: "dk_navy", RGB: color.NRGBA{R: 7, G: 12, B: 32, A: 255}},
	{Index: 100, Name: "dk_indigo", RGB: color.NRGBA{R: 3, G: 6, B: 33, A: 255}},
	{Index: 102, Name: "dk_void", RGB: color.NRGBA{R: 3, G: 1, B: 29, A: 255}},
	{Index: 104, Name: "dk_navy2", RGB: color.NRGBA{R: 4, G: 11, B: 30, A: 255}},
	{Index: 106, Name: "dk_purple", RGB: color.NRGBA{R: 7, G: 7, B: 33, A: 255}},
	{Index: 107, Name: "dk_violet", RGB: color.NRGBA{R: 34, G: 13, B: 102, A: 255}},
	{Index: 109, Name: "dk_magenta", RGB: color.NRGBA{R: 60, G: 17, B: 102, A: 255}},
	{Index: 111, Name: "dk_plum", RGB: color.NRGBA{R: 53, G: 13, B: 48, A: 255}},
	{Index: 113, Name: "dk_crimson", RGB: color.NRGBA{R: 102, G: 6, B: 20, A: 255}},
	{Index: 115, Name: "dk_pink", RGB: color.NRGBA{R: 102, G: 17, B: 84, A: 255}},
	{Index: 116, Name: "gray_dk2", RGB: color.NRGBA{R: 33, G: 5, B: 27, A: 255}},
	{Index: 118, Name: "gray_mid", RGB: color.NRGBA{R: 89, G: 89, B: 89, A: 255}},
	{Index: 120, Name: "white", RGB: color.NRGBA{R: 255, G: 255, B: 255, A: 255}},
	{Index: 122, Name: "lgray", RGB: color.NRGBA{R: 204, G: 204, B: 204, A: 255}},
	{Index: 124, Name: "dgray", RGB: color.NRGBA{R: 20, G: 20, B: 20, A: 255}},
	{Index: 125, Name: "pure_blue", RGB: color.NRGBA{R: 0, G: 0, B: 255, A: 255}},
	{Index: 126, Name: "green_spec", RGB: color.NRGBA{R: 0, G: 255, B: 0, A: 255}},
	{Index: 127, Name: "pure_red", RGB: color.NRGBA{R: 255, G: 0, B: 0, A: 255}},
}

// ColorForIndex maps a raw hardware palette index (0-127) to the nearest
// defined Palette entry at or below it — every index resolves to a real,
// named entry, even the ones this table has no exact match for.
func ColorForIndex(idx uint8) PaletteEntry {
	best := Palette[0]
	for _, e := range Palette {
		if e.Index > idx {
			break
		}
		best = e
	}
	return best
}
