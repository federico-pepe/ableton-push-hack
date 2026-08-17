package push3

import "strings"

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
//   Pad Note On  : velocity = palette index → color (RGB from the table)
//   CC button    : value = brightness (0-127, white hardware LEDs ignore color)
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
