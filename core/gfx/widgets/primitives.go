package widgets

// primitives.go — drawing primitives with no current caller. Added ahead of
// need per discovery/shadow-ui-component-framework.md: "add the tool, don't
// invent a use for it yet." Do not force an existing panel to adopt these
// just because they now exist.

import (
	"image"
	"image/color"
	"math"

	"github.com/federico-pepe/ableton-push-hack/core/gfx"
	"github.com/federico-pepe/ableton-push-hack/core/gfx/text"
)

// DrawBorder draws a 1px rectangle outline.
func DrawBorder(img *image.NRGBA, x, y, w, h int, col color.NRGBA) {
	gfx.FillRect(img, x, y, w, 1, col)
	gfx.FillRect(img, x, y+h-1, w, 1, col)
	gfx.FillRect(img, x, y, 1, h, col)
	gfx.FillRect(img, x+w-1, y, 1, h, col)
}

// DrawHLine draws a horizontal line of thickness 1.
func DrawHLine(img *image.NRGBA, x, y, w int, col color.NRGBA) {
	gfx.FillRect(img, x, y, w, 1, col)
}

// DrawVLine draws a vertical line of thickness 1.
func DrawVLine(img *image.NRGBA, x, y, h int, col color.NRGBA) {
	gfx.FillRect(img, x, y, 1, h, col)
}

// DrawMeter draws a horizontal bar meter: bg for the full [x,y,w,h] rect,
// fg for the filled portion, frac clamped to [0,1].
func DrawMeter(img *image.NRGBA, x, y, w, h int, frac float64, fg, bg color.NRGBA) {
	if frac < 0 {
		frac = 0
	} else if frac > 1 {
		frac = 1
	}
	gfx.FillRect(img, x, y, w, h, bg)
	fillW := int(float64(w) * frac)
	if fillW > 0 {
		gfx.FillRect(img, x, y, fillW, h, fg)
	}
}

// DrawHeader draws a full-width filled bar with left-aligned text — the
// pattern BrowserPanel.renderKeyboard's search header uses inline today.
func DrawHeader(img *image.NRGBA, t Theme, y, w, h int, s string) {
	gfx.FillRect(img, 0, y, w, h, t.CrumbBg)
	text.Draw(img, 4, y+h-3, s, t.White)
}

// DrawArc draws an arc of radius r centered at (cx,cy), sweeping from angle
// 0 (12 o'clock) clockwise through frac*360 degrees, frac clamped to [0,1].
// Single-pixel-wide, no antialiasing — BGR565's resolution doesn't warrant
// it at 10fps. Step count scales with radius so there are no gaps.
func DrawArc(img *image.NRGBA, cx, cy, r int, frac float64, col color.NRGBA) {
	if frac <= 0 {
		return
	}
	if frac > 1 {
		frac = 1
	}
	steps := r * 8
	if steps < 16 {
		steps = 16
	}
	maxAngle := frac * 2 * math.Pi
	for i := 0; i <= steps; i++ {
		angle := maxAngle * float64(i) / float64(steps)
		x := cx + int(math.Round(float64(r)*math.Sin(angle)))
		y := cy - int(math.Round(float64(r)*math.Cos(angle)))
		if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
			continue
		}
		img.Set(x, y, col)
	}
}

// Knob is a labeled value readout in the [Min,Max] range, meant to be drawn
// with DrawArc — no renderer provided yet since no current panel uses one;
// this documents the intended shape for whichever hack adds the first knob
// UI (see discovery/shadow-ui-component-framework.md's extensibility note).
type Knob struct {
	Label           string
	Value, Min, Max float64
}
