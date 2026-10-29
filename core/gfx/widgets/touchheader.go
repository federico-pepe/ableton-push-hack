package widgets

import (
	"image"
	"image/color"

	"github.com/federico-pepe/ableton-push-hack/core/gfx"
	"github.com/federico-pepe/ableton-push-hack/core/gfx/text"
)

// HeaderCols is the number of columns the Push 3 screen is split into: one
// per knob above the screen (and one per button above the screen). The
// column grid is a hardware fact, not a hack's choice.
const HeaderCols = 8

// DimImage scales every pixel's brightness by keep (0 = black, 1 = no
// change), in place. Alpha is untouched. It is the "shadow" Push's own UI
// puts over a screen while you touch a control.
func DimImage(img *image.NRGBA, keep float64) {
	if keep >= 1 {
		return
	}
	if keep < 0 {
		keep = 0
	}
	p := img.Pix
	for i := 0; i+3 < len(p); i += 4 {
		p[i] = uint8(float64(p[i]) * keep)
		p[i+1] = uint8(float64(p[i+1]) * keep)
		p[i+2] = uint8(float64(p[i+2]) * keep)
	}
}

// DrawOnOffBox draws a small rounded box holding label (usually "On" or
// "Off") with its top-left corner at (x, y), 15 px high. On: filled
// t.White, text in t.Black. Off: t.White border, t.Black fill, t.White text.
// It returns the box's width, so a caller can place something after it.
func DrawOnOffBox(img *image.NRGBA, t Theme, x, y int, label string, on bool) (w int) {
	const h = 15
	w = text.Width(label) + 8
	roundedRect(img, x, y, w, h, 2, t.White)
	fg := t.White
	if on {
		fg = t.Black
	} else {
		roundedRect(img, x+1, y+1, w-2, h-2, 1, t.Black)
	}
	text.Draw(img, x+4, y+h-3, label, fg)
	return w
}

// roundedRect fills a w x h rectangle, cutting each corner by radius
// pixels (radius 1 drops the corner pixel, radius 2 also the pixels next
// to it). It paints only the shape, never the corners' surroundings, so it
// works over any background.
func roundedRect(img *image.NRGBA, x, y, w, h, radius int, col color.NRGBA) {
	for row := 0; row < h; row++ {
		inset := 0
		switch edge := min(row, h-1-row); {
		case edge == 0:
			inset = radius
		case edge == 1 && radius == 2:
			inset = 1
		}
		gfx.FillRect(img, x+inset, y+row, w-2*inset, 1, col)
	}
}

// HeaderCol is one column of DrawTouchHeader. It mirrors Live's own header
// on Push: the button above the column (Label, with an On/Off box when it
// is a switch), then the knob's name and its value.
type HeaderCol struct {
	Col    int    // 0-7: which of the HeaderCols columns
	Label  string // the top button's function, e.g. "Loop"; "" = none
	Toggle bool   // the button is an on/off switch: draw an On/Off box after Label
	On     bool   // the switch's state, when Toggle
	Name   string // the knob's name, e.g. "Loop Start"; "" = no knob
	Value  string // the knob's value, e.g. "97.1.1"

	// ValueColor overrides the value's color. The zero value (left unset)
	// falls back to Theme.White -- same contract as Knob.Color.
	ValueColor color.NRGBA
}

// HeaderKeep is how much of the screen's brightness DrawTouchHeader leaves.
const HeaderKeep = 0.4

// DrawTouchHeader dims the whole img to HeaderKeep, then draws each column
// left-aligned in its 1/8 of the width: Label (with the On/Off box) on the
// first line, Name on the second, Value on the third. Label and Name use
// t.CrumbCol; Value uses ValueColor (or t.White). Text is cut to fit the
// column. Call it last, over the finished frame -- it dims what is below.
func DrawTouchHeader(img *image.NRGBA, t Theme, cols []HeaderCol) {
	DimImage(img, HeaderKeep)
	colW := img.Bounds().Dx() / HeaderCols
	maxRunes := (colW - 12) / 7
	for _, c := range cols {
		x := img.Bounds().Min.X + c.Col*colW + 8
		text.Draw(img, x, 14, text.Truncate(c.Label, maxRunes), t.CrumbCol)
		if c.Toggle {
			state := "Off"
			if c.On {
				state = "On"
			}
			DrawOnOffBox(img, t, x+text.Width(text.Truncate(c.Label, maxRunes))+6, 3, state, c.On)
		}
		if c.Name == "" {
			continue
		}
		text.Draw(img, x, 36, text.Truncate(c.Name, maxRunes), t.CrumbCol)
		vc := c.ValueColor
		if vc == (color.NRGBA{}) {
			vc = t.White
		}
		text.Draw(img, x, 52, text.Truncate(c.Value, maxRunes), vc)
	}
}
