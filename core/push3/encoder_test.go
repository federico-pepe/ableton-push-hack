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
	// CC 70 (jog wheel) is a relative encoder. This assertion was previously
	// inverted — it required IsEncoderCC(70) == false, which made jog turns
	// decode as an endless stream of button presses, since both 1 and 127 are
	// non-zero. Measured 2026-08-16: CC 70 sends {1, 127}, exactly like the
	// other encoders, and this repo's own map doc already said so.
	if !IsEncoderCC(70) {
		t.Error("IsEncoderCC(70) = false, want true (jog wheel)")
	}
	// CC 15 and CC 111 are the tempo/volume encoder PUSH-BUTTONS (0/127), not
	// rotation, so they must not be treated as encoders.
	for _, cc := range []uint8{0, 13, 15, 80, 111, 127} {
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
