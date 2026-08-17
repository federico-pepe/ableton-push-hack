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
func Width(s string) int { return len(s) * 7 }

// Truncate truncates s to at most maxRunes runes, appending "…" if cut.
func Truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes-1]) + "…"
}
