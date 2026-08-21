package layout

import "testing"

func TestColSpanFourFourSplit(t *testing.T) {
	x0, w0 := ColSpan(960, 0, 4)
	x1, w1 := ColSpan(960, 4, 4)
	if x0 != 0 || w0 != 480 {
		t.Errorf("left half = x%d w%d, want x0 w480", x0, w0)
	}
	if x1 != 480 || w1 != 480 {
		t.Errorf("right half = x%d w%d, want x480 w480", x1, w1)
	}
}

func TestColSpanSixTwoSplit(t *testing.T) {
	x0, w0 := ColSpan(960, 0, 6)
	x1, w1 := ColSpan(960, 6, 2)
	if x0 != 0 || w0 != 720 {
		t.Errorf("left = x%d w%d, want x0 w720", x0, w0)
	}
	if x1 != 720 || w1 != 240 {
		t.Errorf("right = x%d w%d, want x720 w240", x1, w1)
	}
}

func TestColSpanFullWidth(t *testing.T) {
	x, w := ColSpan(960, 0, Cols)
	if x != 0 || w != 960 {
		t.Errorf("full span = x%d w%d, want x0 w960", x, w)
	}
}

func TestContentNoBars(t *testing.T) {
	r := Content(960, 160, Bars{})
	if r.Min.Y != 0 || r.Max.Y != 160 {
		t.Errorf("content with no bars = %v, want full height", r)
	}
}

func TestContentTopAndBottomBars(t *testing.T) {
	r := Content(960, 160, Bars{TopH: 18, BottomH: 20})
	if r.Min.Y != 18 || r.Max.Y != 140 {
		t.Errorf("content = %v, want y in [18,140)", r)
	}
	if r.Dx() != 960 {
		t.Errorf("content width = %d, want 960 (bars are vertical only)", r.Dx())
	}
}
