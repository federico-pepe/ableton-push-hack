// Package layout expresses Push's 960x160 screen as an 8-column grid with
// optional top/bottom bars carved off first, so widgets compose against a
// shared content rect instead of every module hand-rolling its own pixel
// math. See discovery/shadow-ui-component-framework.md for the widget set
// this composes with, and DESIGN.md once it exists for the rationale behind
// picking 8 columns.
package layout

import "image"

// Cols is the number of columns the screen divides into. 8 was picked to
// match the 8 soft-buttons and 8 encoders below/above the screen, so a
// column-aligned control lines up with the physical control under it.
const Cols = 8

// ColWidth returns the pixel width of one column when w is split into Cols
// equal columns. w is a parameter rather than a hardcoded 960 so a caller
// that has already carved off, say, a side rail can still divide what is
// left of the screen into 8.
func ColWidth(w int) int { return w / Cols }

// ColSpan returns the pixel x and width covering columns
// [startCol, startCol+span) of a w-pixel-wide area. Passing span=8 covers
// the full width; span=4 with startCol=0 and startCol=4 gives the two
// halves of a 4+4 split; span=6 and span=2 give a 6+2 split, and so on —
// any combination that sums to Cols is valid, nothing enforces that it does.
func ColSpan(w, startCol, span int) (x, colW int) {
	cw := ColWidth(w)
	return startCol * cw, span * cw
}

// Bars describes the height of an optional top and/or bottom bar. A zero
// height means that bar is not drawn — the content rect simply extends to
// the screen edge on that side.
type Bars struct {
	TopH, BottomH int
}

// Content returns the rect left over in a w x h screen after Bars' top and
// bottom strips are carved off. Widgets are drawn against this rect, never
// against the full screen, so a module can add or resize a bar without
// hunting down every widget's y-coordinate.
func Content(w, h int, b Bars) image.Rectangle {
	return image.Rect(0, b.TopH, w, h-b.BottomH)
}
