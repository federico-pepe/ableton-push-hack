// Package text draws basicfont text onto an image.NRGBA. Split out of gfx
// so hacks that never draw text (automation, keyboard-visualizer) don't link
// golang.org/x/image/font/basicfont into their binaries.
package text

import (
	"image"
	"image/color"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// Draw draws string at pixel (x, baseline) using basicfont at 1x scale.
func Draw(img *image.NRGBA, x, baseline int, s string, col color.NRGBA) {
	d := &font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{col},
		Face: basicfont.Face7x13,
		Dot:  fixed.P(x, baseline),
	}
	d.DrawString(s)
}

// Width returns the pixel width of s at 1x scale.
//
// Note this counts bytes, while Truncate counts runes — they agree for ASCII,
// which is all basicfont.Face7x13 can draw anyway, but a string carrying
// multibyte characters will measure wider here than it renders.
func Width(s string) int { return len(s) * 7 }

// cutMarker ends a truncated string.
//
// ASCII, deliberately. basicfont.Face7x13 has no glyph beyond ASCII and draws a
// missing-glyph box instead, so this cannot be U+2026 — it was until 2026-08-17,
// which meant every truncated filename in push-manager's browser drew a box on
// the panel. Anything rendered through this package must stay ASCII.
const cutMarker = "..."

// Truncate shortens s to at most maxRunes runes, marking a cut with "...".
//
// The result is never longer than maxRunes runes, so callers can keep sizing
// layouts from that number.
func Truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	if maxRunes <= 0 {
		return ""
	}
	if maxRunes <= len(cutMarker) {
		// No room for content and a marker both; a partial marker at least
		// still signals "there was more". Safe to slice by byte: ASCII.
		return cutMarker[:maxRunes]
	}
	return string(runes[:maxRunes-len(cutMarker)]) + cutMarker
}
