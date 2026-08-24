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
	text.Draw(img, 4, y+h-5, s, t.White)
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

// blendPixel alpha-blends col over the pixel at (x,y), alpha in [0,1] — the
// coverage-based anti-aliasing drawArcWidth/drawLineWidth use so a circle
// or line reads smooth instead of stair-stepped. Output alpha is always
// opaque — the display has no alpha channel of its own.
func blendPixel(img *image.NRGBA, x, y int, col color.NRGBA, alpha float64) {
	if alpha <= 0 || x < 0 || y < 0 || x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
		return
	}
	if alpha > 1 {
		alpha = 1
	}
	bg := img.NRGBAAt(x, y)
	mix := func(s, d uint8) uint8 { return uint8(float64(s)*alpha + float64(d)*(1-alpha)) }
	img.SetNRGBA(x, y, color.NRGBA{R: mix(col.R, bg.R), G: mix(col.G, bg.G), B: mix(col.B, bg.B), A: 255})
}

// drawArcSpanWidth draws a width-pixel-wide, anti-aliased ring of radius r
// centered at (cx,cy), swept clockwise from startAngle (radians, 0 = 12
// o'clock, matching DrawArc's convention) through sweepAngle radians.
// Signed-distance-to-radius coverage per pixel, rather than stepping
// through angles and rounding to the nearest pixel — that's what used to
// give every arc in this package a stair-stepped edge.
//
// The far edge (startAngle+sweepAngle) gets a ~1px feather, same as the
// near edge does implicitly by being the zero point angle is measured
// from; sweepAngle > 2*Pi behaves like exactly 2*Pi (a closed ring).
func drawArcSpanWidth(img *image.NRGBA, cx, cy, r int, startAngle, sweepAngle, width float64, col color.NRGBA) {
	if sweepAngle <= 0 || r <= 0 {
		return
	}
	if sweepAngle > 2*math.Pi {
		sweepAngle = 2 * math.Pi
	}
	bound := r + int(math.Ceil(width)) + 1
	for dy := -bound; dy <= bound; dy++ {
		for dx := -bound; dx <= bound; dx++ {
			dist := math.Hypot(float64(dx), float64(dy))
			radial := width/2 + 0.5 - math.Abs(dist-float64(r))
			if radial <= 0 {
				continue
			}
			angle := math.Atan2(float64(dx), -float64(dy)) - startAngle
			angle = math.Mod(angle, 2*math.Pi)
			if angle < 0 {
				angle += 2 * math.Pi
			}
			if angle > sweepAngle {
				// Feather the sweep's cut edge by about a pixel of arc
				// length too, so a partial arc's end doesn't look any
				// harder-edged than its circular sides.
				featherAngle := 1 / math.Max(dist, 1)
				over := (angle - sweepAngle) / featherAngle
				if over >= 1 {
					continue
				}
				radial *= 1 - over
			}
			blendPixel(img, cx+dx, cy+dy, col, radial)
		}
	}
}

// drawArcWidth draws a width-pixel-wide, anti-aliased ring of radius r
// centered at (cx,cy), swept clockwise from 12 o'clock through frac*360
// degrees, frac clamped to [0,1].
func drawArcWidth(img *image.NRGBA, cx, cy, r int, frac, width float64, col color.NRGBA) {
	if frac <= 0 {
		return
	}
	if frac > 1 {
		frac = 1
	}
	drawArcSpanWidth(img, cx, cy, r, 0, frac*2*math.Pi, width, col)
}

// DrawArc draws an anti-aliased 1px-wide arc of radius r centered at
// (cx,cy), sweeping from angle 0 (12 o'clock) clockwise through frac*360
// degrees, frac clamped to [0,1].
func DrawArc(img *image.NRGBA, cx, cy, r int, frac float64, col color.NRGBA) {
	drawArcWidth(img, cx, cy, r, frac, 1, col)
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

// Knob is a labeled value readout in the [Min,Max] range, drawn by
// DrawKnob, DrawKnobFull, DrawKnobArc, or DrawFader.
type Knob struct {
	Label           string
	Value, Min, Max float64

	// Color is the control's fill/pointer color — the sweep arc in
	// DrawKnob, the pointer line in DrawKnobFull, the fill arc in
	// DrawKnobArc, the filled bar in DrawFader. The zero value
	// (color.NRGBA{}, i.e. left unset) falls back to the Theme's Select
	// color, matching every one of these controls' look before this field
	// existed — unlike internal/renderframe's other color-bearing op
	// params, an unset Knob.Color does NOT default to white, since white
	// is itself a valid, deliberate choice a module might make (e.g.
	// push3.ColorForIndex(120)) and would otherwise be indistinguishable
	// from "not set". Every color-bearing widget in this package follows
	// this same contract — see the package doc.
	Color color.NRGBA

	// ValueScale enlarges the value readout (not Label, which always draws
	// at 1x) via text.DrawScaled — integer nearest-neighbor, same font,
	// just bigger. Zero or 1 means 1x, identical to every one of these
	// controls' look before this field existed — same zero-value-is-the-
	// old-default contract as Color above.
	ValueScale int

	// Bipolar changes DrawKnobArc's fill to grow from the middle of
	// [Min,Max] outward, instead of from Min — see DrawKnobArc's own doc.
	// False (the zero value) is DrawKnobArc's original behavior; every
	// other composition (DrawKnob, DrawKnobFull, DrawFader) ignores this
	// field entirely.
	Bipolar bool
}

// valueScale returns k.ValueScale, floored at 1 — text.DrawScaled's own
// scale<=1 fallback already treats anything at or below 1x as identical to
// Draw, so this just keeps the layout math below from special-casing 0.
func (k Knob) valueScale() int {
	if k.ValueScale <= 1 {
		return 1
	}
	return k.ValueScale
}

// fillColor resolves k.Color against Theme's own default, per Color's own
// doc: zero value falls back to t.Select, not white.
func (k Knob) fillColor(t Theme) color.NRGBA {
	if k.Color == (color.NRGBA{}) {
		return t.Select
	}
	return k.Color
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

// drawLineWidth draws a width-pixel-wide, anti-aliased line between two
// arbitrary points — distance-to-segment coverage per pixel, rather than
// stepping along the line and rounding to the nearest pixel, which is what
// used to give every line in this package a stair-stepped edge.
func drawLineWidth(img *image.NRGBA, x1, y1, x2, y2 int, width float64, col color.NRGBA) {
	fx1, fy1, fx2, fy2 := float64(x1), float64(y1), float64(x2), float64(y2)
	ex, ey := fx2-fx1, fy2-fy1
	lenSq := ex*ex + ey*ey

	pad := int(math.Ceil(width)) + 1
	minX, maxX := min(x1, x2)-pad, max(x1, x2)+pad
	minY, maxY := min(y1, y2)-pad, max(y1, y2)+pad
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			px, py := float64(x), float64(y)
			t := 0.0
			if lenSq > 0 {
				t = ((px-fx1)*ex + (py-fy1)*ey) / lenSq
				t = max(0, min(1, t))
			}
			nx, ny := fx1+t*ex, fy1+t*ey
			dist := math.Hypot(px-nx, py-ny)
			alpha := width/2 + 0.5 - dist
			blendPixel(img, x, y, col, alpha)
		}
	}
}

// drawLine draws an anti-aliased 1px-wide line between two arbitrary
// points — DrawHLine and DrawVLine only cover the axis-aligned case, which
// a knob's pointer and an envelope's segments are not.
func drawLine(img *image.NRGBA, x1, y1, x2, y2 int, col color.NRGBA) {
	drawLineWidth(img, x1, y1, x2, y2, 1, col)
}

// knobStroke is how many pixels wide a knob's arc/pointer draws by
// default — slightly thicker than DrawArc/drawLine's shared 1px default so
// a knob reads clearly at a glance.
const knobStroke = 2

// DrawKnob composes DrawArc with Knob into a radial progress indicator: an
// arc swept to k's value fraction, its numeric value centered inside, and
// its label below. The value is always drawn — a knob whose readout is
// left to the caller is exactly the gap discovery/
// shadow-ui-component-framework.md's Knob type was added ahead of need to
// eventually close.
func DrawKnob(img *image.NRGBA, t Theme, cx, cy, r int, k Knob) {
	drawArcWidth(img, cx, cy, r, 1, knobStroke, t.DarkGray) // full-circle track, so an empty knob still reads as a control
	drawArcWidth(img, cx, cy, r, k.frac(), knobStroke, k.fillColor(t))

	val := fmt.Sprintf("%.0f", k.Value)
	sc := k.valueScale()
	text.DrawScaled(img, cx-text.WidthScaled(val, sc)/2, cy+4*sc, sc, val, t.White)
	if k.Label != "" {
		text.Draw(img, cx-text.Width(k.Label)/2, cy+r+12, k.Label, t.Gray)
	}
}

// DrawKnobFull is DrawKnob's alternative reading: a full circle outline
// (not a progress sweep) plus a single pointer line showing k's value as a
// rotation angle — the traditional hardware-rotary-knob look, as distinct
// from DrawKnob's radial-progress look.
func DrawKnobFull(img *image.NRGBA, t Theme, cx, cy, r int, k Knob) {
	drawArcWidth(img, cx, cy, r, 1, knobStroke, t.DarkGray)

	angle := k.frac() * 2 * math.Pi
	x1 := cx + int(math.Round(float64(r)*0.25*math.Sin(angle)))
	y1 := cy - int(math.Round(float64(r)*0.25*math.Cos(angle)))
	x2 := cx + int(math.Round(float64(r)*0.9*math.Sin(angle)))
	y2 := cy - int(math.Round(float64(r)*0.9*math.Cos(angle)))
	drawLineWidth(img, x1, y1, x2, y2, knobStroke, k.fillColor(t))

	val := fmt.Sprintf("%.0f", k.Value)
	sc := k.valueScale()
	valueY := cy + r + 12*sc
	text.DrawScaled(img, cx-text.WidthScaled(val, sc)/2, valueY, sc, val, t.White)
	if k.Label != "" {
		text.Draw(img, cx-text.Width(k.Label)/2, valueY+12, k.Label, t.Gray)
	}
}

// knobArcStart is 7 o'clock — a gauge knob's minimum, on the clock-face
// convention DrawKnobArc's own doc uses (12 o'clock = angle 0).
// knobArcSweep is 300 degrees, clockwise from knobArcStart through 12
// o'clock to 5 o'clock (the maximum) — a 60-degree gap, centered on 6
// o'clock, is left undrawn at the bottom.
const (
	knobArcStart = 7.0 / 12.0 * 2 * math.Pi
	knobArcSweep = 300.0 / 360.0 * 2 * math.Pi
)

// DrawKnobArc is DrawKnob's gauge reading: a 300-degree arc running
// clockwise from 7 o'clock (the value's minimum, left of center) through
// 12 o'clock to 5 o'clock (the maximum, right of center), leaving a
// 60-degree gap open at the bottom — the traditional hardware-gauge look
// (a car's speedometer, a mixing-desk gain knob), as distinct from
// DrawKnob's full-circle progress ring.
//
// k.Bipolar changes the fill from "grows from the minimum" to "grows from
// the middle of [Min,Max], outward in whichever direction Value moved" —
// nothing drawn at all when Value sits exactly at the middle. For a knob
// whose Min/Max are symmetric around zero (a pan, a detune, a bipolar LFO
// offset), that middle is both the arc's 12 o'clock and the value's own
// resting/center state, so an untouched knob reads as empty rather than
// half-full.
func DrawKnobArc(img *image.NRGBA, t Theme, cx, cy, r int, k Knob) {
	drawArcSpanWidth(img, cx, cy, r, knobArcStart, knobArcSweep, knobStroke, t.DarkGray)
	if k.Bipolar {
		const center = knobArcStart + knobArcSweep/2 // 12 o'clock, same angle DrawKnobArc's own doc names
		switch f := k.frac(); {
		case f > 0.5:
			drawArcSpanWidth(img, cx, cy, r, center, (f-0.5)*knobArcSweep, knobStroke, k.fillColor(t))
		case f < 0.5:
			drawArcSpanWidth(img, cx, cy, r, knobArcStart+f*knobArcSweep, (0.5-f)*knobArcSweep, knobStroke, k.fillColor(t))
		}
		// f == 0.5 exactly: draw nothing — Value is at the middle of the range.
	} else {
		drawArcSpanWidth(img, cx, cy, r, knobArcStart, k.frac()*knobArcSweep, knobStroke, k.fillColor(t))
	}

	val := fmt.Sprintf("%.0f", k.Value)
	sc := k.valueScale()
	text.DrawScaled(img, cx-text.WidthScaled(val, sc)/2, cy+4*sc, sc, val, t.White)
	if k.Label != "" {
		text.Draw(img, cx-text.Width(k.Label)/2, cy+r+12, k.Label, t.Gray)
	}
}

// DrawFader draws a vertical linear control: DrawMeterV's fill plus a
// handle line at the fill boundary and the value readout above it.
func DrawFader(img *image.NRGBA, t Theme, x, y, w, h int, k Knob) {
	frac := k.frac()
	DrawMeterV(img, x, y, w, h, frac, k.fillColor(t), t.DarkGray)
	handleY := y + h - int(float64(h)*frac)
	gfx.FillRect(img, x-2, handleY-1, w+4, 2, t.White)

	val := fmt.Sprintf("%.0f", k.Value)
	sc := k.valueScale()
	text.DrawScaled(img, x+(w-text.WidthScaled(val, sc))/2, y-4*sc, sc, val, t.White)
	if k.Label != "" {
		text.Draw(img, x+(w-text.Width(k.Label))/2, y+h+12, k.Label, t.Gray)
	}
}

// DrawEnvelope connects points (each normalized to [0,1], 0=bottom) with
// straight, anti-aliased segments over a w x h rect at (x,y) — the basic
// shape an envelope/curve editor needs; drawLine per segment.
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
