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

## Palette

`widgets.Theme`, starting point is `widgets.Default` (push-manager's
original Shadow UI palette). Not enforced — a hack or module can build its
own `Theme`. Loose guideline, not a rule: green (`OnColor`) for
confirmation/active, red (`OffColor`/`Accent`) for cancel/destructive.

## Breadcrumb bar (2026-08-21)

`Frame.Breadcrumb` is its own op, independent of `Frame.List` — a module
can have a top bar with no scrolling list under it. `DrawBreadcrumbBar`
itself predates this: see `discovery/shadow-ui-component-framework.md`.

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
