// Package widgets holds composite Shadow-UI-style drawing components built
// on core/gfx and core/gfx/text — lists, label:value rows, soft-buttons, and
// a shared color Theme. Anything here operates on a plain *image.NRGBA; it
// knows nothing about shm, BGR565, or which hack is drawing. See
// discovery/shadow-ui-component-framework.md for the extraction rationale:
// this exists so push-manager's Shadow UI and keyboard-visualizer's own
// screen renderer (and any future hack that draws on Push's display) share
// one visual language instead of each hand-rolling its own.
package widgets

import "image/color"

// Theme is a named color palette for widget rendering.
type Theme struct {
	Black, White, Gray, DarkGray color.NRGBA
	Select, DirColor, Accent     color.NRGBA
	OnColor, OffColor            color.NRGBA
	CrumbBg, CrumbCol            color.NRGBA
	StatusBg, StatusCol          color.NRGBA
}

// Default is push-manager's existing Shadow UI palette, named and shared.
// It's a starting point, not a hardcoded singleton — a hack that wants its
// own look constructs its own Theme value and passes it to the Draw*
// functions instead.
var Default = Theme{
	Black:     color.NRGBA{0, 0, 0, 255},
	White:     color.NRGBA{255, 255, 255, 255},
	Gray:      color.NRGBA{120, 120, 120, 255},
	DarkGray:  color.NRGBA{30, 30, 30, 255},
	Select:    color.NRGBA{0, 90, 200, 255},
	DirColor:  color.NRGBA{180, 210, 255, 255},
	Accent:    color.NRGBA{200, 40, 40, 255}, // red — delete confirm highlight
	OnColor:   color.NRGBA{80, 220, 80, 255},  // green — active/enabled state
	OffColor:  color.NRGBA{255, 80, 80, 255},  // red — disabled/idle state
	CrumbBg:   color.NRGBA{20, 20, 20, 255},
	CrumbCol:  color.NRGBA{200, 200, 200, 255},
	StatusBg:  color.NRGBA{0, 60, 30, 255},  // dark green tint for status messages
	StatusCol: color.NRGBA{100, 255, 150, 255},
}
