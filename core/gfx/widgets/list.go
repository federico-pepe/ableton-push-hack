package widgets

// list.go — generic scrollable list with cursor highlight, optional icons,
// and a breadcrumb/status bar above it. Generalizes push-manager's
// FilePanel and BrowserPanel list rendering, which were two independent,
// drifting copies of the same thing (see discovery/
// shadow-ui-component-framework.md). One real drift found while unifying:
// FilePanel's scrollbar thumb height used the fixed content area height,
// BrowserPanel's used the list's actual available height (excluding the
// breadcrumb bar) — the latter is correct, DrawScrollbar here always uses
// the actual available height.

import (
	"image"
	"image/color"

	"github.com/federico-pepe/ableton-push-hack/core/gfx"
	"github.com/federico-pepe/ableton-push-hack/core/gfx/text"
)

// ListRow is one row of a ListView. Icon, if non-nil, is composited at the
// row's left edge — callers resolve and load icons themselves (e.g. from a
// filesystem-specific icon set); this package only composites an already-
// loaded image, never touches a filesystem or icon name.
type ListRow struct {
	Icon    *image.NRGBA
	Text    string
	Bg      color.NRGBA
	TextCol color.NRGBA
}

// ListView is the full state needed to render one frame of a scrollable
// list: rows, cursor/scroll position, and a breadcrumb or status message
// shown above the list. Status, when non-empty, overrides Breadcrumb.
type ListView struct {
	Rows       []ListRow
	Cursor     int
	Scroll     int
	Breadcrumb string
	Status     string
	EmptyText  string // shown instead of the list when len(Rows) == 0
}

// DrawBreadcrumbBar renders the breadcrumb/status strip at y and returns the
// y coordinate the list itself should start at (y + the bar's height).
func DrawBreadcrumbBar(img *image.NRGBA, t Theme, y, w int, breadcrumb, status string) (listY int) {
	const crumbH = 13
	bg, col, txt := t.CrumbBg, t.CrumbCol, breadcrumb
	if status != "" {
		bg, col, txt = t.StatusBg, t.StatusCol, status
	}
	gfx.FillRect(img, 0, y, w, crumbH, bg)
	text.Draw(img, 4, y+crumbH-2, text.Truncate(txt, 120), col)
	return y + crumbH
}

// DrawListRows draws rows starting at listY, rowH pixels each, until either
// rows run out or the row would fall at/past maxY. w is the row width — rows
// are drawn w-6 wide, leaving room for DrawScrollbar's 4px thumb + 2px gap.
// Returns the number of rows that fit (visRows), for DrawScrollbar's use.
func DrawListRows(img *image.NRGBA, t Theme, listY, w, rowH, maxY int, rows []ListRow, cursor, scroll int) (visRows int) {
	visRows = (maxY - listY) / rowH
	for i := 0; i < visRows; i++ {
		idx := scroll + i
		if idx >= len(rows) {
			break
		}
		r := rows[idx]
		y := listY + i*rowH

		bg, textCol := r.Bg, r.TextCol
		if idx == cursor {
			bg, textCol = t.Select, t.White
		}
		gfx.FillRect(img, 0, y, w-6, rowH, bg)

		textX := 8
		if r.Icon != nil {
			iconY := y + (rowH-r.Icon.Bounds().Dy())/2
			gfx.DrawIcon(img, r.Icon, 2, iconY)
			textX = 2 + r.Icon.Bounds().Dx() + 3
		}
		text.Draw(img, textX, y+rowH-3, r.Text, textCol)
	}
	return visRows
}

// DrawScrollbar draws a scrollbar thumb sized to visRows/total, positioned
// by scroll/total, in the 4px-wide gutter at the right edge (w-4..w).
// No-op if total <= visRows (everything fits, no scrolling needed).
func DrawScrollbar(img *image.NRGBA, t Theme, listY, listBot, w, total, visRows, scroll int) {
	if total <= visRows {
		return
	}
	avail := listBot - listY
	barH := avail * visRows / total
	if barH < 4 {
		barH = 4
	}
	barY := listY + avail*scroll/total
	gfx.FillRect(img, w-4, listY, 4, avail, t.DarkGray)
	gfx.FillRect(img, w-4, barY, 4, barH, t.Gray)
}

// HListView is the horizontal-scroll counterpart to ListView: a row of
// columns instead of a column of rows. Reuses ListRow for cell content
// (Text/Icon/Bg/TextCol apply the same way to a column as a row) rather
// than introducing a parallel type for the same four fields.
type HListView struct {
	Cols       []ListRow
	Cursor     int
	Scroll     int
	Breadcrumb string
	Status     string
	EmptyText  string
}

// DrawListCols is DrawListRows' horizontal sibling: cursor/scroll walk
// columns left-to-right instead of rows top-to-bottom. Kept as a separate
// function rather than a generalized DrawListRows(vertical bool, ...) —
// same reasoning as DrawHLine/DrawVLine being separate — so the existing,
// already-tested vertical path is untouched.
//
// listY/h place the single row of cells; colW is each cell's width; maxX is
// the rightmost x a cell may start at. Returns visCols, the number of
// columns that fit, for DrawScrollbarH's use.
func DrawListCols(img *image.NRGBA, t Theme, listY, h, colW, maxX int, cols []ListRow, cursor, scroll int) (visCols int) {
	visCols = maxX / colW
	for i := 0; i < visCols; i++ {
		idx := scroll + i
		if idx >= len(cols) {
			break
		}
		c := cols[idx]
		x := i * colW

		bg, textCol := c.Bg, c.TextCol
		if idx == cursor {
			bg, textCol = t.Select, t.White
		}
		gfx.FillRect(img, x, listY, colW-2, h-6, bg)

		textX := x + 4
		if c.Icon != nil {
			iconY := listY + (h-6-c.Icon.Bounds().Dy())/2
			gfx.DrawIcon(img, c.Icon, x+2, iconY)
			textX = x + 2 + c.Icon.Bounds().Dx() + 3
		}
		text.Draw(img, textX, listY+h-6-3, c.Text, textCol)
	}
	return visCols
}

// DrawScrollbarH is DrawScrollbar's horizontal sibling: a thumb sized to
// visCols/total, positioned by scroll/total, in a 4px-tall gutter along the
// bottom edge. No-op if total <= visCols.
func DrawScrollbarH(img *image.NRGBA, t Theme, listX, listRight, bottomY, total, visCols, scroll int) {
	if total <= visCols {
		return
	}
	avail := listRight - listX
	barW := avail * visCols / total
	if barW < 4 {
		barW = 4
	}
	barX := listX + avail*scroll/total
	gfx.FillRect(img, listX, bottomY-4, avail, 4, t.DarkGray)
	gfx.FillRect(img, barX, bottomY-4, barW, 4, t.Gray)
}

// RenderListH combines DrawBreadcrumbBar + DrawListCols + DrawScrollbarH,
// the horizontal-scroll analog of RenderList. y/w place the breadcrumb bar;
// h is the row height; colW each column's width; maxX the rightmost extent
// (usually the panel width).
func RenderListH(img *image.NRGBA, t Theme, v HListView, y, w, h, colW, maxX int) {
	listY := DrawBreadcrumbBar(img, t, y, w, v.Breadcrumb, v.Status)

	if len(v.Cols) == 0 {
		if v.EmptyText != "" {
			text.Draw(img, 8, listY+20, v.EmptyText, t.Gray)
		}
		return
	}

	visCols := DrawListCols(img, t, listY, h, colW, maxX, v.Cols, v.Cursor, v.Scroll)
	DrawScrollbarH(img, t, 0, maxX, listY+h, len(v.Cols), visCols, v.Scroll)
}

// RenderList combines DrawBreadcrumbBar + DrawListRows + DrawScrollbar —
// the common case. y is where the breadcrumb bar starts; w/maxY are the
// panel's content width/bottom (e.g. push-manager's suiW/suiContentBot).
func RenderList(img *image.NRGBA, t Theme, v ListView, y, w, rowH, maxY int) {
	listY := DrawBreadcrumbBar(img, t, y, w, v.Breadcrumb, v.Status)

	if len(v.Rows) == 0 {
		if v.EmptyText != "" {
			text.Draw(img, 8, listY+20, v.EmptyText, t.Gray)
		}
		return
	}

	visRows := DrawListRows(img, t, listY, w, rowH, maxY, v.Rows, v.Cursor, v.Scroll)
	DrawScrollbar(img, t, listY, maxY, w, len(v.Rows), visRows, v.Scroll)
}
