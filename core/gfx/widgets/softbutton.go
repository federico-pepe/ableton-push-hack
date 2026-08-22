package widgets

import (
	"image"
	"image/color"

	"github.com/federico-pepe/ableton-push-hack/core/gfx"
	"github.com/federico-pepe/ableton-push-hack/core/gfx/text"
	"github.com/federico-pepe/ableton-push-hack/core/push3"
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
//
// Group clusters buttons that belong together — e.g. four quantize values
// where only one is ever selected, or independent mute/solo toggles. 0
// means ungrouped. Membership is state a module tracks itself (see
// push-tethered-app's module.ButtonGroup); this field only drives the
// visual cue DrawBotStrip draws so grouped buttons read as one control at
// a glance. Group numbers need not be contiguous indices — any subset of
// the 8 slots can share a Group.
type SoftButton struct {
	Label string
	State SoftButtonState
	Group int
}

// groupColors cycles by (Group-1)%len(groupColors) so an arbitrary number
// of groups still gets a distinct-enough cue. Deliberately not part of
// Theme yet — revisit if a hack ever needs more than 4 concurrent groups
// or wants to override them. Colors come from push3.Palette, same rule as
// Theme's Default — never a raw RGB literal.
var groupColors = func() [4]color.NRGBA {
	names := [4]string{"sky", "orange", "violet", "amber"}
	var out [4]color.NRGBA
	for i, n := range names {
		idx, ok := push3.ColorByName(n)
		if !ok {
			panic("widgets: unknown push3 palette color " + n)
		}
		out[i] = push3.ColorForIndex(idx).RGB
	}
	return out
}()

// DrawBotStrip renders up to 8 soft-buttons in a row plus an optional hint
// to the right, styled by State rather than by matching label text. w is
// the strip's total pixel width, colW each button's column width, h the
// strip height, drawn with its bottom edge at y+h.
func DrawBotStrip(img *image.NRGBA, t Theme, y, w, colW, h int, buttons [8]SoftButton, hint string) {
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

		if b.Group != 0 {
			gc := groupColors[(b.Group-1)%len(groupColors)]
			gfx.FillRect(img, x+2, y, colW-4, 2, gc)
		}
	}

	if hint != "" {
		text.Draw(img, 4*colW+8, y+h-4, hint, t.Gray)
	}
}
