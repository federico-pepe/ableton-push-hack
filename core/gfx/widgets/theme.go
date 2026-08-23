// Package widgets holds composite Shadow-UI-style drawing components built
// on core/gfx and core/gfx/text — lists, label:value rows, soft-buttons, and
// a shared color Theme. Anything here operates on a plain *image.NRGBA; it
// knows nothing about shm, BGR565, or which hack is drawing. See
// discovery/shadow-ui-component-framework.md for the extraction rationale:
// this exists so push-manager's Shadow UI and keyboard-visualizer's own
// screen renderer (and any future hack that draws on Push's display) share
// one visual language instead of each hand-rolling its own.
//
// # Color is a package-wide contract, not a per-widget choice
//
// Every widget in this package — existing or future — must support the
// full Push color palette (core/push3.Palette/ColorForIndex) by default,
// with a sensible fallback when no color is given, rather than a fixed
// look a caller can't change. Concretely, that means:
//
//  1. Any color a widget draws with must be a plain color.NRGBA parameter
//     (directly, like DrawArc/DrawMeter/DrawEnvelope's col/fg/bg, or via a
//     field on a param struct, like Knob.Color) — never a hardcoded
//     literal baked into the function. A caller must always be able to
//     pass any push3.Palette entry.
//  2. A color left at its zero value (color.NRGBA{}, i.e. not set) must
//     still render something sensible, not nothing — but "sensible" is
//     picked per widget, not one blanket rule:
//     - Widgets with no natural per-instance default (DrawArc, DrawMeter,
//       DrawEnvelope, and every op internal/renderframe.defaultColor
//       covers) fall back to white — see that function's doc.
//     - Knob and everything sharing its Color field (DrawKnob,
//       DrawKnobFull, DrawKnobArc, DrawFader) fall back to Theme.Select
//       instead, via Knob.fillColor — these had a real default look
//       before Color existed, and white is itself a valid deliberate
//       choice for one of them, so defaulting to white would make "chose
//       white" and "didn't choose" indistinguishable. See Knob.Color's
//       own doc for the reasoning.
//     - SoftButton.Color falls back to its own State's default color
//       (White/OnColor/OffColor) instead, via SoftButton.labelColor —
//       same reasoning as Knob.Color, one level removed: State already
//       picks a real default, so Color only needs to handle the
//       "override it" case.
//     A new color-bearing widget picks whichever of these shapes fits —
//     but must pick one, not leave a color permanently fixed.
//  3. Theme's own defaults (Default, groupColors) are themselves resolved
//     through push3.Palette rather than hand-picked RGB literals — see
//     Default's doc.
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
