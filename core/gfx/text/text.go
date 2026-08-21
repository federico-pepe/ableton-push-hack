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

// DrawScaled draws string at pixel (x, baseline), each source pixel of the
// 1x rendering expanded to a scale x scale block — integer nearest-neighbor,
// no blur, so it still reads as "the same font, just bigger." scale<=1
// draws at 1x, identical to Draw (no temp-image/metrics path needed at that
// size, so this never duplicates Draw's own logic).
func DrawScaled(img *image.NRGBA, x, baseline, scale int, s string, col color.NRGBA) {
	if scale <= 1 {
		Draw(img, x, baseline, s, col)
		return
	}
	w := Width(s)
	if w <= 0 {
		return
	}
	m := basicfont.Face7x13.Metrics()
	ascent, descent := m.Ascent.Ceil(), m.Descent.Ceil()
	h := ascent + descent
	if h <= 0 {
		return
	}

	// Render once at 1x into a tightly-sized scratch image, then blit each
	// non-transparent pixel out as a scale x scale block. Two passes rather
	// than a scale-aware rasterizer: basicfont has no notion of scale, and
	// this reuses Draw exactly instead of reimplementing glyph layout.
	tmp := image.NewNRGBA(image.Rect(0, 0, w, h))
	Draw(tmp, 0, ascent, s, col)

	originX, originY := x, baseline-ascent*scale
	for ty := 0; ty < h; ty++ {
		for tx := 0; tx < w; tx++ {
			c := tmp.NRGBAAt(tx, ty)
			if c.A == 0 {
				continue
			}
			px, py := originX+tx*scale, originY+ty*scale
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.Set(px+dx, py+dy, c)
				}
			}
		}
	}
}

// WidthScaled returns the pixel width of s at the given scale.
func WidthScaled(s string, scale int) int {
	if scale <= 1 {
		return Width(s)
	}
	return Width(s) * scale
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
