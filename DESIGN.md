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

## Typography (2026-08-21, fonts replaced 2026-08-22)

`core/gfx/text.Draw`/`Width` is the **default** basic face; `text.NewFace`/
`DrawWith`/`WidthWith` is the opt-in styled face at an arbitrary point
size — split into two paths (not one `TextParams` field) since they're
different enough in rendering cost and shape (arbitrary size vs. integer
multiples of one glyph cell) to keep separate. Wired into the module ABI
as `Frame.Text`/`TextScaled` and `Frame.StyledText` respectively.

Both faces were originally `golang.org/x/image` built-ins
(`basicfont.Face7x13` for the default, vendored `gofont` TTFs for the
styled weights) — zero-dependency, but generic, not this product's own
look. **2026-08-22: swapped for real font files**, with different
distribution stories per face since only one of the two is freely
redistributable:

- **Basic — Tamzen7x13r**, freely licensed (Scott Fial's Tamsyn/Tamzen,
  see its own LICENSE), so it's **embedded** (`core/gfx/text/assets/`,
  `//go:embed`, still no external install) — an outline font
  (`font/opentype`, `HintingFull`) rendered at 13pt/72dpi to match the old
  bitmap's 7x13 cell exactly (`Width` still hardcodes a 7px advance;
  `DrawScaled`'s metrics-based sizing is otherwise unaffected). **Drawn
  uppercase** — `text.Draw` uppercases before sanitizing, because Tamzen's
  lowercase has no true descenders at this size and reads worse than
  all-caps does. `DrawHeader`/`DrawStatusBar`
  (`core/gfx/widgets/primitives.go`) needed their baseline offsets
  retuned: Tamzen's ink sits differently in its em box than the fixed
  bitmap's did, so a header that looked vertically centered with
  `Face7x13` sat visibly low once the face changed — don't assume the two
  fonts share a baseline constant.
- **Styled — Helvetica Neue**: `Weight` maps Regular→Thin, Bold→Medium,
  Italic→ThinItalic, BoldItalic→MediumItalic. **Not embedded** — Helvetica
  Neue's `.otf` files are Apple/Monotype-licensed, not freely
  redistributable, and this repo is public. `face.go`'s `source()` instead
  reads the four files at runtime from `PUSHAPP_STYLED_FONT_DIR` (a
  developer-populated, gitignored local directory) and falls back to the
  original vendored `gofont` TTFs when that env var or file is unset/
  missing — so a fresh clone or CI, which has neither, still builds and
  renders correctly, just with the generic weights instead of Helvetica
  Neue. Built with `font.HintingNone`, not `HintingFull`: Helvetica Neue's
  OTF is CFF/PostScript outlines with no TrueType hint program, so
  `HintingFull` was a no-op pass that left rendering *worse* on Push's
  panel (BGR565's coarse color depth crushes antialiased edges into
  visible steps, and a no-op "hint" bought nothing against that) —
  confirmed by eyeballing `modules/ui-text-demo` on real Push 3 hardware,
  not guessed from theory.

Since an outline face *can* render glyphs the old fixed bitmap couldn't,
both `Draw` and `DrawWith`/`WidthWith` sanitize to ASCII themselves rather
than relying on font coverage — the guarantee `Face7x13` gave for free is
now explicit on both paths, not just the styled one. `NewFace` still
caches by (weight, size); building an `opentype.Face` is too expensive to
do every frame.

`push-tethered-app/modules/ui-text-demo` is the live tuning bench this
swap was verified against — every encoder drives one rendering parameter
(face, weight, size, palette color, margin) so a change like the
`HintingNone` fix above can be dialed in and eyeballed on real hardware in
one session instead of guessing constants and rebuilding `cmd/screensim`
scenes each time.

## Palette

`widgets.Theme`, starting point is `widgets.Default` (push-manager's
original Shadow UI palette). Not enforced — a hack or module can build its
own `Theme`. Loose guideline, not a rule: green (`OnColor`) for
confirmation/active, red (`OffColor`/`Accent`) for cancel/destructive.

**2026-08-22: `widgets.Default` and `widgets.groupColors` no longer hold
raw RGB literals.** Both now build every entry via `push3.ColorByName`/
`ColorForIndex` (`theme.go`'s `paletteColor` helper, `softbutton.go`'s
`groupColors` init), picking the closest `push3.Palette` entry to each
original hand-picked value — e.g. `Select`'s old `{0,90,200}` became
`cobalt` (`{24,83,178}`), `Accent`'s old `{200,40,40}` became `maroon`
(`{166,52,33}`). Requested directly: colors on screen should be traceable
to a real, named Push color, the same table LED writes already use, not an
arbitrary literal a screen's full color range happens to allow. `push3`
has no dependency back on `widgets`/`gfx` (only on `core/display`), so this
added an import with no cycle risk. This is a convention for *this*
package's own defaults, not an enforced type — a hack's custom `Theme`
can still use any `color.NRGBA` it wants; the ask was specifically about
`widgets.Default`/`groupColors` and the module-facing color fields (see
push-tethered-app's `internal/renderframe.defaultColor`, which now
defaults any module's unset color field to white rather than invisible
transparent black).

## Anti-aliased primitives (2026-08-22)

`DrawArc` and the package-private `drawLine` (`primitives.go`) draw
anti-aliased by default now, not the original step-along-the-shape,
round-to-nearest-pixel approach that made every circle and diagonal line
in the package read visibly stair-stepped — a direct ask after the knob
stroke-width change below shipped and still looked jagged.

- `blendPixel(img, x, y, col, alpha)`: alpha-blends a color into an
  existing pixel (straight, non-premultiplied `image.NRGBA`), output alpha
  always 255 — the display has no alpha channel of its own to preserve.
- `drawArcWidth(img, cx, cy, r, frac, width, col)`: for every pixel within
  `width` of the ring, coverage = `width/2 + 0.5 - |dist(pixel,center) -
  r|`, clamped — a signed-distance-to-radius test, not angular stepping.
  The sweep's cut edge (for `frac < 1`) gets the same ~1px feather via
  arc-length so a partial arc's end isn't harder-edged than its round
  sides.
- `drawLineWidth(img, x1, y1, x2, y2, width, col)`: same idea against
  distance to the nearest point on the segment (clamped projection `t` in
  `[0,1]`), not distance to a rounded step point.
- `DrawArc`/`drawLine` are now thin `width=1` wrappers around these two.
  **This is a default, not a knob-only mode** — `DrawEnvelope` picks up
  anti-aliasing for free since it already calls `drawLine` per segment,
  and any hack calling `widgets.DrawArc` directly gets it too, with no
  signature change.
- `DrawKnob`/`DrawKnobFull` call the same two functions with
  `knobStroke = 2` instead of relying on a separate stroke helper — there
  used to be one (`drawArcStroke`/`drawLineStroke`, added earlier the same
  day to thicken the knob by drawing the arc twice at different radii /
  the line twice at a 1px diagonal offset), but once `drawArcWidth`/
  `drawLineWidth` existed as the general anti-aliased primitive, the
  separate crude-offset version had no reason to keep existing —
  `DrawKnob`/`DrawKnobFull` just pass a wider `width` to the same code
  path everything else uses.

Cost: `drawArcWidth` iterates a `(2r+width)²` bounding box per call
(trig per pixel) rather than `O(r)` angular steps: fine at the sizes and
frame rates here (10fps, knob radii in the tens of pixels, a handful of
calls per frame), revisit only if a much larger radius or many
simultaneous arcs shows up.

**Hardware LED palette, resolved to RGBA (2026-08-22):** `push3.Palette`/
`ColorForIndex(idx uint8) PaletteEntry` (`core/push3/colors.go`) resolves a
raw 0-127 hardware palette index to its name and `color.NRGBA`, rounding
down to the nearest of `NamedColors`' 90 named entries when the raw index
has none of its own. Added alongside the font work above so a widget or
module wanting to *preview* an LED color on screen — or offer "cycle the
palette" as a single control instead of raw RGB sliders — has one shared,
SysEx-sourced table to read, rather than a second hand-copied RGB array
drifting from `NamedColors` over time. `push-tethered-app/modules/ui-text-demo`
is the first consumer.

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
