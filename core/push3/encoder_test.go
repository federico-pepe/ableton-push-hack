package push3

import "testing"

func TestScaleVal(t *testing.T) {
	cases := []struct {
		v, lo, hi, want uint8
	}{
		{0, 0, 127, 0},
		{127, 0, 127, 127},
		{0, 40, 40, 40}, // lo == hi
		{127, 40, 40, 40},
		{0, 127, 0, 127}, // inverted range
		{127, 127, 0, 0},
	}
	for _, c := range cases {
		if got := ScaleVal(c.v, c.lo, c.hi); got != c.want {
			t.Errorf("ScaleVal(%d,%d,%d) = %d, want %d", c.v, c.lo, c.hi, got, c.want)
		}
	}
}

func TestDecodeRel(t *testing.T) {
	cases := []struct {
		v    uint8
		want int
	}{
		{1, 1},
		{63, 63},
		{65, -63},
		{127, -1},
	}
	for _, c := range cases {
		if got := DecodeRel(c.v); got != c.want {
			t.Errorf("DecodeRel(%d) = %d, want %d", c.v, got, c.want)
		}
	}
}

func TestIsEncoderCC(t *testing.T) {
	for cc := 71; cc <= 79; cc++ {
		if !IsEncoderCC(uint8(cc)) {
			t.Errorf("IsEncoderCC(%d) = false, want true", cc)
		}
	}
	if !IsEncoderCC(14) {
		t.Error("IsEncoderCC(14) = false, want true (tempo wheel)")
	}
	for _, cc := range []uint8{0, 13, 15, 70, 80, 127} {
		if IsEncoderCC(cc) {
			t.Errorf("IsEncoderCC(%d) = true, want false", cc)
		}
	}
}

func TestClampInt(t *testing.T) {
	if got := ClampInt(5, 0, 10); got != 5 {
		t.Errorf("ClampInt(5,0,10) = %d, want 5", got)
	}
	if got := ClampInt(-5, 0, 10); got != 0 {
		t.Errorf("ClampInt(-5,0,10) = %d, want 0", got)
	}
	if got := ClampInt(15, 0, 10); got != 10 {
		t.Errorf("ClampInt(15,0,10) = %d, want 10", got)
	}
	if got := ClampInt(5, 10, 0); got != 5 { // inverted lo/hi
		t.Errorf("ClampInt(5,10,0) = %d, want 5", got)
	}
}
