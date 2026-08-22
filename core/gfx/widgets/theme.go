// Package widgets holds composite Shadow-UI-style drawing components built
// on core/gfx and core/gfx/text — lists, label:value rows, soft-buttons, and
// a shared color Theme. Anything here operates on a plain *image.NRGBA; it
// knows nothing about shm, BGR565, or which hack is drawing. See
// discovery/shadow-ui-component-framework.md for the extraction rationale:
// this exists so push-manager's Shadow UI and keyboard-visualizer's own
// screen renderer (and any future hack that draws on Push's display) share
// one visual language instead of each hand-rolling its own.
package widgets

import (
	"image/color"

	"github.com/federico-pepe/ableton-push-hack/core/push3"
)

// Theme is a named color palette for widget rendering.
type Theme struct {
	Black, White, Gray, DarkGray color.NRGBA
	Select, DirColor, Accent     color.NRGBA
	OnColor, OffColor            color.NRGBA
	CrumbBg, CrumbCol            color.NRGBA
	StatusBg, StatusCol          color.NRGBA
}

// paletteColor looks up a push3.Palette entry by name. Panics on an unknown
// name — this only ever runs at package init against names hand-checked
// against colors.go, so a typo needs to fail loud, not draw wrong.
func paletteColor(name string) color.NRGBA {
	idx, ok := push3.ColorByName(name)
	if !ok {
		panic("widgets: unknown push3 palette color " + name)
	}
	return push3.ColorForIndex(idx).RGB
}

// Default is push-manager's existing Shadow UI palette, named and shared.
// It's a starting point, not a hardcoded singleton — a hack that wants its
// own look constructs its own Theme value and passes it to the Draw*
// functions instead.
//
// Every color is drawn from push3.Palette (the same table LED writes use),
// picked as the closest RGB match to this theme's original hand-picked
// values — the screen is full-color and isn't bound to the pad palette the
// way LEDs are, but restricting to one shared table keeps every color a
// module or widget ever draws traceable to a named, real Push color rather
// than an arbitrary literal.
var Default = Theme{
	Black:     paletteColor("off"),
	White:     paletteColor("white"),
	Gray:      paletteColor("gray_green"),
	DarkGray:  paletteColor("dgray"),
	Select:    paletteColor("cobalt"),
	DirColor:  paletteColor("lavender"),
	Accent:    paletteColor("maroon"),   // red-ish — delete confirm highlight
	OnColor:   paletteColor("green_vv"), // green — active/enabled state
	OffColor:  paletteColor("red"),      // red — disabled/idle state
	CrumbBg:   paletteColor("dgray"),
	CrumbCol:  paletteColor("lgray"),
	StatusBg:  paletteColor("dk_pine"), // dark green tint for status messages
	StatusCol: paletteColor("mint"),
}
