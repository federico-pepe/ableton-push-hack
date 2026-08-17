package push3

// IsEncoderCC reports whether a CC number carries a relative encoder delta:
// encoders 1–8 = CC 71–78, volume wheel = CC 79, tempo wheel = CC 14,
// jog wheel = CC 70.
//
// The jog wheel was added 2026-08-16 after measuring CC 70 sending {1, 127} on
// tethered hardware — the same relative encoding as every other encoder, and
// what this repo's own docs/push3-button-map.md already described. It was
// previously excluded (and explicitly asserted false in the tests), which made
// jog turns decode as button presses: both 1 and 127 are non-zero, so a caller
// splitting on "encoder or button?" saw an endless stream of presses.
//
// Note CC 15 and CC 111 are NOT encoders — they are the tempo and volume
// encoders' push-buttons, sending 0/127.
func IsEncoderCC(cc uint8) bool {
	return (cc >= 71 && cc <= 79) || cc == 14 || cc == 70
}

// DecodeRel converts a Push relative-encoder CC value to a signed delta.
// Two's-complement encoding: 1..63 positive, 65..127 negative (127 = -1).
func DecodeRel(v uint8) int {
	if v < 64 {
		return int(v)
	}
	return int(v) - 128
}

// ScaleVal maps an incoming 0–127 value into [lo,hi].
func ScaleVal(v, lo, hi uint8) uint8 {
	if hi >= lo {
		return uint8(int(lo) + int(v)*(int(hi)-int(lo))/127)
	}
	// inverted range: hi < lo
	return uint8(int(lo) - int(v)*(int(lo)-int(hi))/127)
}

// ClampInt clamps v into [lo,hi], tolerating lo > hi by swapping.
func ClampInt(v, lo, hi int) int {
	if lo > hi {
		lo, hi = hi, lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
