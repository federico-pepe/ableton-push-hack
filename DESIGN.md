# Design system decisions

Living index of decisions for the shared Push widget/design system
(`core/gfx`, `core/gfx/text`, `core/gfx/widgets`, `core/gfx/layout`), used
by both this repo's hacks and `push-tethered-app`'s modules. Links to the
per-feature discovery docs for full rationale rather than duplicating them.
See `push-tethered-app/plans/2026-08-21-design-system-screensim.md` for the
roadmap this is being built against, and `cmd/screensim` (in
push-tethered-app) for a no-hardware way to check any of this visually.

## Screen

960x160px. 8 columns (`core/gfx/layout.Cols`), matching the 8 soft-buttons
and 8 encoders either side of the screen — a column-aligned control lines
up with the physical control under it. Optional top/bottom bars are carved
off first (`layout.Bars`/`layout.Content`); everything else composes
against the remaining content rect.

## Typography (2026-08-21)

`core/gfx/text.Draw`/`Width` — `basicfont.Face7x13`, a fixed 7x13 bitmap
face — stays the **default**: cheap, deterministic, crisp at any zoom, and
it's what makes ASCII-only trivially enforceable (the font has no other
glyphs). Two additive, opt-in extensions, both answering IDEAS.md's font
questions with real code rather than just a recommendation:

- **Size**: `text.DrawScaled`/`WidthScaled` — integer nearest-neighbor
  upscaling of Face7x13 itself (each source pixel becomes an NxN block,
  no blur). Wired into the module ABI as `TextParams.Scale`
  (`Frame.TextScaled`). Cheap, no new dependency, but only integer
  multiples of the one bitmap size.
- **Different fonts / weights**: `text.NewFace`/`DrawWith`/`WidthWith` —
  antialiased outline fonts at an arbitrary point size, using
  `golang.org/x/image`'s already-vendored gofont TTFs
  (regular/bold/italic/bold-italic) via `font/opentype`. No new
  dependency, no font file to ship. Wired into the module ABI as its own
  `"styledtext"` op (`Frame.StyledText`) rather than folded into
  `TextParams` — different enough rendering cost and shape (arbitrary
  size vs. integer multiples of one bitmap) to keep separate.

Since an outline face *can* render glyphs Face7x13 can't, `DrawWith`/
`WidthWith` sanitize to ASCII themselves rather than relying on font
coverage — the same guarantee Face7x13 gave for free, made explicit
instead. `NewFace` caches by (weight, size); building an `opentype.Face`
is too expensive to do every frame.

## Palette

`widgets.Theme`, starting point is `widgets.Default` (push-manager's
original Shadow UI palette). Not enforced — a hack or module can build its
own `Theme`. Loose guideline, not a rule: green (`OnColor`) for
confirmation/active, red (`OffColor`/`Accent`) for cancel/destructive.

## Breadcrumb bar (2026-08-21)

`Frame.Breadcrumb` is its own op, independent of `Frame.List` — a module
can have a top bar with no scrolling list under it. `DrawBreadcrumbBar`
itself predates this: see `discovery/shadow-ui-component-framework.md`.

## Horizontal-scroll list (2026-08-21)

`widgets.HListView`/`DrawListCols`/`DrawScrollbarH`/`RenderListH`, kept as
separate functions mirroring `DrawListRows`/`DrawScrollbar`/`RenderList`
rather than a generalized orientation flag — same reasoning as
`DrawHLine`/`DrawVLine` staying separate. Wired into the module ABI as the
`"hlist"` Frame op, alongside the existing vertical `"list"`.

## Soft-button groups (2026-08-21)

`SoftButton.Group` (int, 0 = none) clusters an arbitrary subset of the 8
slots — not necessarily contiguous — with a thin underline in one of 4
cycling colors, drawn by `DrawBotStrip`. This is the *only* shared/visual
part: soft-buttons have no physical per-button LED, their state feedback
is the on-screen label color itself, so there is no `Host` LED API to add
for "lighting" a group.

Selection state (which button(s) in a group are on) lives entirely in the
calling module, via push-tethered-app's `module.ButtonGroup` — a plain
`Toggle`/`IsSelected` tracker, not part of the ABI. It supports both
semantics a module might want, picked per group:
- **Exclusive** (radio): `Toggle` always leaves exactly one index
  selected; re-pressing the selected button is a no-op rather than
  clearing the group to nothing.
- **Independent**: each index toggles on/off on its own — mute/solo,
  multi-select filters, etc.

## Widget set (2026-08-21)

Basics only for now — visual polish is a deliberately later pass, per
plan; the point of this round was having each control exist at all.

- `DrawMeterV` — vertical sibling of `DrawMeter`, fills bottom-up.
- `DrawKnob` — radial-progress reading: full-circle track + a sweep to
  the value fraction, value centered inside, label below. Composes the
  `Knob` type discovery/shadow-ui-component-framework.md added ahead of
  need with the existing `DrawArc`.
- `DrawKnobFull` — traditional rotary-knob reading: full circle outline
  + a single pointer line at the value's angle. Distinct primitive from
  `DrawKnob`, not a mode flag on it — a progress sweep and a rotation
  pointer are different enough readings to keep as separate functions.
- `DrawFader` — vertical linear control: `DrawMeterV`'s fill + a handle
  line + the value readout.
- `DrawEnvelope` — connects a `[]float64` of normalized points with
  straight segments; the basic shape an envelope/curve editor needs.
- `DrawPadGrid` — `cols x rows` grid of cells, row 0 at the bottom
  (Push's pad numbering is bottom-up). Extracted from `modules/monitor`
  and `modules/seq` in push-tethered-app, which had each independently
  reimplemented the identical cell-sizing/row-flip math — same drift
  risk the shadow-ui doc already removed for list rendering.

Every control above always draws its own numeric value where relevant —
IDEAS.md's "a control's value should always be displayed somewhere" is
enforced by the renderer, not left to the caller.

All shared through the module ABI as ops (`meterv`, `knob`, `knobfull`,
`fader`, `envelope`, `padgrid`) with typed `Frame` constructors — see
`internal/renderframe` and `internal/module/frame.go` in
push-tethered-app.

## Whole-frame pagination (2026-08-21)

When a module has more controls or content than fit in one pass over the
8 encoders/8 soft-buttons — e.g. 16 parameters, shown 8 at a time — the
whole screen redraws to a different page rather than anything scrolling
within a fixed layout. This needs **no new widget-system support**: a
module already redraws its full `Draw` every frame and already owns
whatever state it wants, so "page 2 of 2" is just an int field it branches
on:

```go
type myModule struct {
    page int // 0 or 1
}

func (m *myModule) Handle(ev module.Event) {
    if b, ok := ev.(module.Button); ok && b.Pressed {
        switch b.Name {
        case "D-Pad right":
            m.page = (m.page + 1) % totalPages
        case "D-Pad left":
            m.page = (m.page - 1 + totalPages) % totalPages
        }
    }
}

func (m *myModule) Draw(f *module.Frame) {
    if m.page == 0 {
        drawPageOne(f)
    } else {
        drawPageTwo(f)
    }
}
```

D-Pad left/right arrive as ordinary `Button` events (pushmap's "D-Pad
left"/"D-Pad right") — no new event type or Host API needed. This is a
documented convention, not a widget: no `DrawPageIndicator` or `Frame` op
exists for it, on purpose. (An earlier version of this note assumed
pagination meant a scrollable list overflowing past 8 rows and built a
page-indicator widget for that — reverted; whole-frame pagination is a
different, simpler problem than a scrolling list, which already has its
own scrollbar via `RenderList`.)
