// Package gfx holds stdlib-only image drawing primitives shared by any hack
// that renders onto Push 3's 960x160 display. Text rendering (which needs
// golang.org/x/image) lives in the gfx/text subpackage instead, so hacks
// that don't draw text (automation, keyboard-visualizer) never link it.
package gfx

import (
	"image"
	"image/color"
	"image/draw"
)

// FillRect fills the rectangle [x,y,x+w,y+h) in img with c. No-op for
// non-positive w/h (kv's copy guarded this; pm's didn't — harmless either
// way since draw.Draw over an inverted rect already draws nothing).
func FillRect(img *image.NRGBA, x, y, w, h int, c color.NRGBA) {
	if w <= 0 || h <= 0 {
		return
	}
	draw.Draw(img, image.Rect(x, y, x+w, y+h), &image.Uniform{c}, image.Point{}, draw.Src)
}
