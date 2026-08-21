package widgets

// primitives.go — drawing primitives with no current caller. Added ahead of
// need per discovery/shadow-ui-component-framework.md: "add the tool, don't
// invent a use for it yet." Do not force an existing panel to adopt these
// just because they now exist.

import (
	"fmt"
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

// DrawMeterV is DrawMeter's vertical sibling: bg for the full rect, fg
// filling from the bottom up to frac, clamped to [0,1] — the usual reading
// for a level/volume meter, where empty means silent.
func DrawMeterV(img *image.NRGBA, x, y, w, h int, frac float64, fg, bg color.NRGBA) {
	if frac < 0 {
		frac = 0
	} else if frac > 1 {
		frac = 1
	}
	gfx.FillRect(img, x, y, w, h, bg)
	fillH := int(float64(h) * frac)
	if fillH > 0 {
		gfx.FillRect(img, x, y+h-fillH, w, fillH, fg)
	}
}

// DrawHeader draws a full-width filled bar with left-aligned text — the
// pattern BrowserPanel.renderKeyboard's search header uses inline today.
func DrawHeader(img *image.NRGBA, t Theme, y, w, h int, s string) {
	gfx.FillRect(img, 0, y, w, h, t.CrumbBg)
	text.Draw(img, 4, y+h-3, s, t.White)
}

// DrawStatusBar draws a full-width filled bar with left-aligned text, for a
// module's bottom-of-screen status/error line — StatusBg/StatusCol
// normally, OffColor background with white text when isError, matching the
// "an error takes over the strip so it stays noticeable" convention
// push-tethered-app's monitor/thru/seq/remap modules each independently
// hand-rolled before this existed.
func DrawStatusBar(img *image.NRGBA, t Theme, y, w, h int, s string, isError bool) {
	bg, col := t.StatusBg, t.StatusCol
	if isError {
		bg, col = t.OffColor, t.White
	}
	gfx.FillRect(img, 0, y, w, h, bg)
	text.Draw(img, 8, y+h-5, s, col)
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

// DrawPadGrid draws a cols x rows grid of cell-2-sized squares, cell pixels
// apart, with row 0 at the bottom — Push's pad numbering is bottom-up, so
// this is the shared math push-tethered-app's monitor and seq modules each
// independently reimplemented (see their Draw for the pre-extraction
// version) before drifting was a risk worth removing, the same reasoning
// discovery/shadow-ui-component-framework.md gives for unifying
// FilePanel/BrowserPanel's list rendering. colorAt is called once per
// cell — callers decide what a cell means (a held pad, a pattern step) and
// only hand back a color.
func DrawPadGrid(img *image.NRGBA, x, y, cell, cols, rows int, colorAt func(col, row int) color.NRGBA) {
	for row := 0; row < rows; row++ {
		cy := y + (rows-1-row)*cell
		for col := 0; col < cols; col++ {
			gfx.FillRect(img, x+col*cell, cy, cell-2, cell-2, colorAt(col, row))
		}
	}
}

// Knob is a labeled value readout in the [Min,Max] range, drawn by DrawKnob
// or DrawKnobFull.
type Knob struct {
	Label           string
	Value, Min, Max float64
}

// frac normalizes k.Value into [0,1] against [k.Min,k.Max], clamped.
// Max<=Min (a misconfigured knob) reads as 0 rather than dividing by zero.
func (k Knob) frac() float64 {
	if k.Max <= k.Min {
		return 0
	}
	f := (k.Value - k.Min) / (k.Max - k.Min)
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// drawLine draws a 1px-wide line between two arbitrary points — DrawHLine
// and DrawVLine only cover the axis-aligned case, which a knob's pointer
// and an envelope's segments are not. Same non-antialiased, step-per-pixel
// style as DrawArc: BGR565's resolution doesn't warrant more at 10fps.
func drawLine(img *image.NRGBA, x1, y1, x2, y2 int, col color.NRGBA) {
	dx, dy := x2-x1, y2-y1
	steps := int(math.Max(math.Abs(float64(dx)), math.Abs(float64(dy))))
	if steps == 0 {
		img.Set(x1, y1, col)
		return
	}
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		x := x1 + int(math.Round(float64(dx)*t))
		y := y1 + int(math.Round(float64(dy)*t))
		if x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
			continue
		}
		img.Set(x, y, col)
	}
}

// DrawKnob composes DrawArc with Knob into a radial progress indicator: an
// arc swept to k's value fraction, its numeric value centered inside, and
// its label below. The value is always drawn — a knob whose readout is
// left to the caller is exactly the gap discovery/
// shadow-ui-component-framework.md's Knob type was added ahead of need to
// eventually close.
func DrawKnob(img *image.NRGBA, t Theme, cx, cy, r int, k Knob) {
	DrawArc(img, cx, cy, r, 1, t.DarkGray) // full-circle track, so an empty knob still reads as a control
	DrawArc(img, cx, cy, r, k.frac(), t.Select)

	val := fmt.Sprintf("%.0f", k.Value)
	text.Draw(img, cx-text.Width(val)/2, cy+4, val, t.White)
	if k.Label != "" {
		text.Draw(img, cx-text.Width(k.Label)/2, cy+r+12, k.Label, t.Gray)
	}
}

// DrawKnobFull is DrawKnob's alternative reading: a full circle outline
// (not a progress sweep) plus a single pointer line showing k's value as a
// rotation angle — the traditional hardware-rotary-knob look, as distinct
// from DrawKnob's radial-progress look.
func DrawKnobFull(img *image.NRGBA, t Theme, cx, cy, r int, k Knob) {
	DrawArc(img, cx, cy, r, 1, t.DarkGray)

	angle := k.frac() * 2 * math.Pi
	x1 := cx + int(math.Round(float64(r)*0.25*math.Sin(angle)))
	y1 := cy - int(math.Round(float64(r)*0.25*math.Cos(angle)))
	x2 := cx + int(math.Round(float64(r)*0.9*math.Sin(angle)))
	y2 := cy - int(math.Round(float64(r)*0.9*math.Cos(angle)))
	drawLine(img, x1, y1, x2, y2, t.Select)

	val := fmt.Sprintf("%.0f", k.Value)
	text.Draw(img, cx-text.Width(val)/2, cy+r+12, val, t.White)
	if k.Label != "" {
		text.Draw(img, cx-text.Width(k.Label)/2, cy+r+24, k.Label, t.Gray)
	}
}

// DrawFader draws a vertical linear control: DrawMeterV's fill plus a
// handle line at the fill boundary and the value readout above it.
func DrawFader(img *image.NRGBA, t Theme, x, y, w, h int, k Knob) {
	frac := k.frac()
	DrawMeterV(img, x, y, w, h, frac, t.Select, t.DarkGray)
	handleY := y + h - int(float64(h)*frac)
	gfx.FillRect(img, x-2, handleY-1, w+4, 2, t.White)

	val := fmt.Sprintf("%.0f", k.Value)
	text.Draw(img, x+(w-text.Width(val))/2, y-4, val, t.White)
	if k.Label != "" {
		text.Draw(img, x+(w-text.Width(k.Label))/2, y+h+12, k.Label, t.Gray)
	}
}

// DrawEnvelope connects points (each normalized to [0,1], 0=bottom) with
// straight segments over a w x h rect at (x,y) — the basic shape an
// envelope/curve editor needs; drawLine per segment, same non-antialiased
// style as the rest of this package.
func DrawEnvelope(img *image.NRGBA, x, y, w, h int, points []float64, col color.NRGBA) {
	if len(points) < 2 {
		return
	}
	px := func(i int) int { return x + i*w/(len(points)-1) }
	py := func(v float64) int {
		if v < 0 {
			v = 0
		} else if v > 1 {
			v = 1
		}
		return y + h - int(v*float64(h))
	}
	for i := 0; i < len(points)-1; i++ {
		drawLine(img, px(i), py(points[i]), px(i+1), py(points[i+1]), col)
	}
}
