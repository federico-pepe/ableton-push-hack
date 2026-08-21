package widgets

import (
	"image"
	"image/color"
	"testing"
)

func newCanvas(w, h int) *image.NRGBA {
	return image.NewNRGBA(image.Rect(0, 0, w, h))
}

func TestDrawKVRowsStopsAtMaxY(t *testing.T) {
	img := newCanvas(960, 160)
	rows := []KVRow{
		{Label: "A", Value: "1", ValueCol: Default.White},
		{Label: "B", Value: "2", ValueCol: Default.White},
		{Label: "C", Value: "3", ValueCol: Default.White}, // should not fit
	}
	// y=0, rowH=20, maxY=45 -> rows at y=0 (ends 20) and y=20 (ends 40) fit;
	// y=40 would end at 60 > 45, dropped.
	DrawKVRows(img, Default, 0, 960, 20, 80, 45, rows)

	// Separator lines land at y+rowH-1: 19 and 39. No separator at 59.
	if img.NRGBAAt(500, 19) == (color.NRGBA{}) {
		t.Error("expected separator line for row 0 at y=19")
	}
	if img.NRGBAAt(500, 39) == (color.NRGBA{}) {
		t.Error("expected separator line for row 1 at y=39")
	}
	if img.NRGBAAt(500, 59) != (color.NRGBA{}) {
		t.Error("row 2 should have been dropped (exceeds maxY), but something was drawn at y=59")
	}
}

func TestDrawListRowsVisRowsCount(t *testing.T) {
	img := newCanvas(960, 160)
	rows := make([]ListRow, 10)
	for i := range rows {
		rows[i] = ListRow{Text: "x", Bg: Default.Black, TextCol: Default.White}
	}
	// listY=31 (typical crumb bottom), rowH=16, maxY=142 -> (142-31)/16 = 6
	got := DrawListRows(img, Default, 31, 960, 16, 142, rows, 0, 0)
	if got != 6 {
		t.Errorf("visRows = %d, want 6", got)
	}
}

func TestDrawListRowsFewerRowsThanFit(t *testing.T) {
	img := newCanvas(960, 160)
	rows := []ListRow{
		{Text: "only one", Bg: Default.Black, TextCol: Default.White},
	}
	visRows := DrawListRows(img, Default, 31, 960, 16, 142, rows, 0, 0)
	if visRows != 6 {
		t.Errorf("visRows = %d, want 6 (capacity, not row count)", visRows)
	}
	// Row 0 should be drawn (selected -> Theme.Select background).
	if img.NRGBAAt(10, 31) != Default.Select {
		t.Errorf("row 0 (cursor) background = %+v, want Select %+v", img.NRGBAAt(10, 31), Default.Select)
	}
}

func TestDrawScrollbarNoOpWhenEverythingFits(t *testing.T) {
	img := newCanvas(960, 160)
	DrawScrollbar(img, Default, 31, 142, 960, 5, 6, 0) // total(5) <= visRows(6)
	if img.NRGBAAt(957, 50) != (color.NRGBA{}) {
		t.Error("scrollbar should not draw anything when everything fits")
	}
}

func TestDrawScrollbarDrawsThumbWhenOverflowing(t *testing.T) {
	img := newCanvas(960, 160)
	DrawScrollbar(img, Default, 31, 142, 960, 20, 6, 0) // total(20) > visRows(6)
	// thumb: barH = (142-31)*6/20 = 33, positioned at scroll=0 -> y=31..64
	if img.NRGBAAt(958, 40) != Default.Gray {
		t.Errorf("scrollbar thumb = %+v, want Gray %+v", img.NRGBAAt(958, 40), Default.Gray)
	}
	// track only, below the thumb
	if img.NRGBAAt(958, 100) != Default.DarkGray {
		t.Errorf("scrollbar track = %+v, want DarkGray %+v", img.NRGBAAt(958, 100), Default.DarkGray)
	}
}

func TestDrawBotStripSoftConfirmFillsAccentBackground(t *testing.T) {
	img := newCanvas(960, 160)
	buttons := [8]SoftButton{
		{Label: "CONFIRM?", State: SoftConfirm},
	}
	DrawBotStrip(img, Default, 142, 960, 120, 18, buttons, "")
	// Check a background-only pixel near the top of the button cell, away
	// from the text baseline near the bottom, to avoid depending on glyph
	// rendering specifics.
	if img.NRGBAAt(10, 143) != Default.Accent {
		t.Errorf("SoftConfirm background = %+v, want Accent %+v", img.NRGBAAt(10, 143), Default.Accent)
	}
}

func TestDrawBotStripNeutralKeepsDarkGrayBackground(t *testing.T) {
	img := newCanvas(960, 160)
	buttons := [8]SoftButton{{Label: "OK", State: SoftNeutral}}
	DrawBotStrip(img, Default, 142, 960, 120, 18, buttons, "")
	if img.NRGBAAt(10, 143) != Default.DarkGray {
		t.Errorf("SoftNeutral background = %+v, want DarkGray %+v", img.NRGBAAt(10, 143), Default.DarkGray)
	}
}

func TestDrawMeterFracClamped(t *testing.T) {
	img := newCanvas(960, 160)
	fg, bg := color.NRGBA{255, 0, 0, 255}, color.NRGBA{0, 0, 0, 255}
	DrawMeter(img, 0, 0, 100, 10, 0.5, fg, bg)
	if img.NRGBAAt(40, 5) != fg {
		t.Errorf("pixel within filled 50%% = %+v, want fg", img.NRGBAAt(40, 5))
	}
	if img.NRGBAAt(60, 5) != bg {
		t.Errorf("pixel past filled 50%% = %+v, want bg", img.NRGBAAt(60, 5))
	}
}

func TestDrawMeterFracOverOneClampsFull(t *testing.T) {
	img := newCanvas(960, 160)
	fg := color.NRGBA{255, 0, 0, 255}
	DrawMeter(img, 0, 0, 100, 10, 1.5, fg, color.NRGBA{0, 0, 0, 255})
	if img.NRGBAAt(99, 5) != fg {
		t.Errorf("frac>1 should clamp to full bar; pixel at x=99 = %+v, want fg", img.NRGBAAt(99, 5))
	}
}

func TestDrawArcZeroFracDrawsNothing(t *testing.T) {
	img := newCanvas(100, 100)
	DrawArc(img, 50, 50, 20, 0, color.NRGBA{255, 255, 255, 255})
	if img.NRGBAAt(50, 30) != (color.NRGBA{}) {
		t.Error("frac=0 should draw nothing")
	}
}

func TestDrawArcFullCircleReachesTopAndBottom(t *testing.T) {
	img := newCanvas(100, 100)
	col := color.NRGBA{255, 255, 255, 255}
	DrawArc(img, 50, 50, 20, 1, col)
	if img.NRGBAAt(50, 30) != col { // 12 o'clock (angle 0)
		t.Error("full arc should reach the 12 o'clock point")
	}
}

func TestDrawListColsVisColsCount(t *testing.T) {
	img := newCanvas(960, 160)
	cols := make([]ListRow, 10)
	for i := range cols {
		cols[i] = ListRow{Text: "x"}
	}
	// colW=120 over maxX=960 -> 8 columns fit.
	visCols := DrawListCols(img, Default, 0, 40, 120, 960, cols, 0, 0)
	if visCols != 8 {
		t.Errorf("visCols = %d, want 8", visCols)
	}
}

func TestDrawListColsFewerColsThanFit(t *testing.T) {
	img := newCanvas(960, 160)
	cols := []ListRow{{Text: "a"}, {Text: "b"}}
	visCols := DrawListCols(img, Default, 0, 40, 120, 960, cols, 0, 0)
	if visCols != 8 {
		t.Errorf("visCols = %d, want 8 (the space that fits, not len(cols))", visCols)
	}
}

func TestDrawScrollbarHNoOpWhenEverythingFits(t *testing.T) {
	img := newCanvas(960, 160)
	DrawScrollbarH(img, Default, 0, 960, 160, 4, 8, 0)
	for x := 0; x < 960; x++ {
		if img.NRGBAAt(x, 159) != (color.NRGBA{}) {
			t.Fatal("total<=visCols should draw nothing")
		}
	}
}

func TestDrawScrollbarHDrawsThumbWhenOverflowing(t *testing.T) {
	img := newCanvas(960, 160)
	DrawScrollbarH(img, Default, 0, 960, 160, 16, 8, 0)
	found := false
	for x := 0; x < 960; x++ {
		if img.NRGBAAt(x, 159) != (color.NRGBA{}) {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("overflowing list should draw a scrollbar gutter")
	}
}

func TestDrawBotStripDrawsGroupUnderline(t *testing.T) {
	img := newCanvas(960, 160)
	var buttons [8]SoftButton
	buttons[0] = SoftButton{Label: "A", Group: 1}
	DrawBotStrip(img, Default, 0, 960, 120, 20, buttons, "")
	if got := img.NRGBAAt(4, 0); got != groupColors[0] {
		t.Errorf("group underline at (4,0) = %+v, want %+v", got, groupColors[0])
	}
}

func TestDrawBotStripNoGroupNoUnderline(t *testing.T) {
	img := newCanvas(960, 160)
	var buttons [8]SoftButton
	buttons[0] = SoftButton{Label: "A"}
	DrawBotStrip(img, Default, 0, 960, 120, 20, buttons, "")
	if got := img.NRGBAAt(4, 0); got != Default.DarkGray {
		t.Errorf("ungrouped button drew something at (4,0) = %+v, want DarkGray strip bg", got)
	}
}
