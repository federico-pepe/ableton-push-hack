package widgets

import (
	"image"

	"github.com/federico-pepe/ableton-push-hack/core/gfx"
	"github.com/federico-pepe/ableton-push-hack/core/gfx/text"
)

// SoftButtonState is the semantic state of a soft-button label, replacing
// push-manager's original drawBotStrip string-matching against literal
// label text (e.g. label == "DELETE") to decide styling.
type SoftButtonState int

const (
	SoftNeutral SoftButtonState = iota
	SoftOn                          // green text — active/enabled
	SoftOff                         // red text — disabled/idle
	SoftConfirm                     // filled accent background — "are you sure?"
)

// SoftButton is one under-screen button label + its semantic state.
type SoftButton struct {
	Label string
	State SoftButtonState
}

// DrawBotStrip renders up to 4 soft-buttons in a row plus an optional hint
// to the right, styled by State rather than by matching label text. w is
// the strip's total pixel width, colW each button's column width, h the
// strip height, drawn with its bottom edge at y+h.
func DrawBotStrip(img *image.NRGBA, t Theme, y, w, colW, h int, buttons [4]SoftButton, hint string) {
	gfx.FillRect(img, 0, y, w, h, t.DarkGray)

	for i, b := range buttons {
		if b.Label == "" {
			continue
		}
		x := i * colW
		col := t.White
		bg := t.DarkGray
		switch b.State {
		case SoftConfirm:
			bg = t.Accent
		case SoftOff:
			col = t.OffColor
		case SoftOn:
			col = t.OnColor
		}
		if bg != t.DarkGray {
			gfx.FillRect(img, x, y, colW, h, bg)
		}
		lx := x + (colW-text.Width(b.Label))/2
		text.Draw(img, lx, y+h-4, b.Label, col)
	}

	if hint != "" {
		text.Draw(img, 4*colW+8, y+h-4, hint, t.Gray)
	}
}
