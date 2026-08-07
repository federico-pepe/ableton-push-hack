package push3

import "strings"

// namedColors maps convenience names to Push 3 palette velocity/CC indices.
//
// SOURCE OF TRUTH: Push2/colors.pyc COLOR_TABLE (from Ableton's own Python code).
// Each entry: (rgb_value, velocity_index). Extracted via XPython3Exe on Push 3 hardware.
//
// TWO USES (same value sent to hardware, different meaning):
//   Pad Note On  : velocity = palette index → color (RGB from COLOR_TABLE)
//   CC button    : value = brightness (0-127, white hardware LEDs ignore color)
//
// COLOR_TABLE structure (velocity → RGB):
//   0          = off (#000000)
//   2–52 even  = 26 primary Live colors (PUSH_INDEX_TO_COLOR_INDEX display order)
//   54–78 even = muted/dark variants
//   80–89      = pastel range
//   90–93      = neutral grays
//   94–121     = very dark shades
//   122        = #21051B (very dark — only useful as CC button brightness alias)
//   123        = #595959 mid-gray pad
//   124        = #FFFFFF white pad
//   125        = #CCCCCC light gray pad
//   126        = #141414 / #0000FF (very dark or pure blue — shared slot)
//   127        = #00FF00 / #FF0000 (pure green or red — shared slot)
//
// Odd velocities 1–79 are hardware half-brightness interpolations; no COLOR_TABLE entry.
// WHITE_MIDI_VALUE = 122 (Ableton's CC brightness value for "lit white button").
//
// See docs/push3-led-colors.md for the full 128-entry table.
// NamedColors maps convenience color names to Push 3 palette velocity/CC
// indices. Exported so callers (e.g. push-manager's LED API) can list it.
var NamedColors = map[string]uint8{
	// ── Universal ─────────────────────────────────────────────────────────────
	"off": 0, // #000000

	// ── CC button brightness aliases (white LED hardware, value = brightness) ─
	"dim":       1,   // very dim button
	"half":      63,  // half brightness
	"white_btn": 122, // WHITE_MIDI_VALUE — standard lit-white button (Ableton internal)
	"full":      127, // full brightness

	// ── Pad palette: 26 primary colors (from PUSH_INDEX_TO_COLOR_INDEX) ───────
	// Even velocities 2–52; display order: alternating tonal pairs left-to-right.
	"red":         2,  // #FF4032  vivid warm red
	"red_dark":    4,  // #800400  very dark red
	"orange":      6,  // #C93C00  dark orange-red
	"red_brown":   8,  // #AC1F00  dark brownish-red
	"brown":       10, // #8C5018  warm brown
	"brown_dark":  12, // #491804  very dark brown
	"yellow":      14, // #FADC3B  vivid yellow
	"amber":       16, // #FFC516  warm amber/orange-yellow
	"lime":        18, // #B6FF0E  vivid yellow-green
	"green_vivid": 20, // #79FF18  vivid bright green
	"green":       22, // #34C216  medium green
	"olive":       24, // #4F8A04  dark olive green
	"green_vv":    26, // #62FF55  very vivid lime green
	"teal_dark":   28, // #297D53  dark teal-green
	"teal":        30, // #269E72  medium teal
	"sky":         32, // #31ADFF  sky blue
	"blue":        34, // #3663FC  vivid blue
	"blue_dark":   36, // #1A34FF  dark blue
	"navy":        38, // #1C0CE6  navy
	"navy_dark":   40, // #153999  dark navy
	"indigo":      42, // #3937FF  vivid indigo/purple-blue
	"purple":      44, // #5722FF  vivid purple
	"violet":      46, // #972BFF  violet/purple-magenta
	"magenta_dk":  48, // #852178  dark magenta/plum
	"crimson":     50, // #FF1032  crimson/vivid red-pink
	"pink_hot":    52, // #FF2BD4  vivid hot pink

	// ── Pad palette: muted/dark range (vel 54–78 even) ───────────────────────
	"maroon":     54, // #A63421
	"sienna":     56, // #995628
	"gold_dark":  58, // #876700
	"khaki":      60, // #90821F
	"green_dk":   62, // #4A8700
	"forest":     64, // #007F12
	"cobalt":     66, // #1853B2
	"purple_mid": 68, // #624BAD
	"plum":       70, // #733A67

	// ── Pad palette: pastel/light range (vel 72–89) ───────────────────────────
	"pink":        72, // #F8BCAF  light salmon/pink
	"peach":       74, // #FF9B76
	"gold_light":  76, // #FFBF5F
	"tan":         78, // #D9AF71
	"yellow_lt":   80, // #FFF480  pastel yellow
	"sage":        81, // #BCCC88
	"mint":        82, // #7CDD9F
	"cyan_lt":     83, // #80F3FF  light cyan
	"blue_lt":     84, // #68A1D3  muted blue
	"periwinkle":  85, // #858FC2
	"lavender":    86, // #CDBBE4
	"sage_dk":     87, // #859D8C
	"steel":       88, // #84909B  steel blue-gray
	"mauve":       89, // #88859D

	// ── Pad palette: neutral muted grays (vel 90–93) ─────────────────────────
	"gray_blue":  90, // #6C6A75
	"gray_mauve": 91, // #746A74
	"gray_green": 92, // #74756A
	"gray_warm":  93, // #756A6A

	// ── Pad palette: very dark shades (vel 94–121) ────────────────────────────
	"dk_wine":    94,  // #210806
	"dk_red":     95,  // #280000
	"dk_orange":  96,  // #5D1700
	"dk_maroon":  97,  // #470C00
	"dk_brown":   98,  // #3B2B14
	"dk_sienna":  99,  // #250E05
	"dk_gold":    100, // #645817
	"dk_olive":   101, // #201C07
	"dk_amber":   102, // #211902
	"dk_lime":    103, // #172101
	"dk_forest":  104, // #0F2103
	"dk_green":   105, // #061902
	"dk_teal_g":  106, // #1F3701
	"dk_emerald": 107, // #276622
	"dk_pine":    108, // #143E29
	"dk_teal":    109, // #004D36
	"dk_ocean":   110, // #134566
	"dk_cobalt":  111, // #152764
	"dk_navy":    112, // #070C20
	"dk_indigo":  113, // #030621
	"dk_void":    114, // #03011D
	"dk_navy2":   115, // #040B1E
	"dk_purple":  116, // #070721
	"dk_violet":  117, // #220D66
	"dk_magenta": 118, // #3C1166
	"dk_plum":    119, // #350D30
	"dk_crimson": 120, // #660614
	"dk_pink":    121, // #661154

	// ── Pad palette: spec colors (vel 122–127) ────────────────────────────────
	// NOTE: vel 122 for PADS = #21051B (very dark); WHITE_MIDI_VALUE=122 is for CC buttons only.
	"gray_dk2":   122, // #21051B  very dark (pad); CC white-brightness alias = white_btn above
	"gray_mid":   123, // #595959  medium gray
	"white":      124, // #FFFFFF  white
	"lgray":      125, // #CCCCCC  light gray
	"dgray":      126, // #141414  very dark gray (pad); also encodes #0000FF pure blue in Live
	"green_spec": 127, // #00FF00  pure green (pad); also encodes #FF0000 pure red in Live
}

// ColorByName looks up a convenience color name (case-insensitive) and
// returns its palette index. ok is false for an unknown name.
func ColorByName(name string) (uint8, bool) {
	idx, ok := NamedColors[strings.ToLower(name)]
	return idx, ok
}
