package main

import (
	"testing"
	"time"
)

func pwr(status, level byte) []byte {
	return []byte{0xF0, 0x00, 0x21, 0x1D, 0x01, 0x01, 0x3A, status, level, 0xF7}
}

// Replies captured on device 2026-11-09.
func TestParsePowerState(t *testing.T) {
	cases := []struct {
		status, level byte
		want          powerState
		label         string
	}{
		{0x22, 0x63, powerState{Connected: true, Charge: 2, Level: 99}, "Plugged in"},
		{0x02, 0x63, powerState{Charge: 2, Level: 99}, "Discharging"},
		{0x00, 0x5E, powerState{Level: 94}, "Discharging"},
		{0x21, 0x5E, powerState{Connected: true, Charge: 1, Level: 94}, "Charging"},
		{0x2A, 0x64, powerState{Connected: true, Charge: 2, Level: 100, ShortPress: true}, "Plugged in"},
		{0x08, 0x5E, powerState{Level: 94, ShortPress: true}, "Discharging"},
		{0x10, 0x5E, powerState{Level: 94, LongPress: true}, "Discharging"},
	}
	for _, c := range cases {
		got, ok := parsePowerState(pwr(c.status, c.level))
		if !ok || got != c.want || got.Label() != c.label {
			t.Errorf("%02X %02X: got %+v %q ok=%v, want %+v %q", c.status, c.level, got, got.Label(), ok, c.want, c.label)
		}
	}
}

func TestParsePowerStateRejects(t *testing.T) {
	good := pwr(0x22, 0x63)
	bad := [][]byte{
		nil,
		good[:9],
		append(append([]byte{}, good...), 0x00),
		pwr(0x80, 0x63), // not a data byte
		pwr(0x22, 0x65), // level over 100
		{0xF0, 0x00, 0x21, 0x1D, 0x01, 0x01, 0x38, 0x22, 0x63, 0xF7}, // GET_AUDIO_CONTROLS
		{0xF0, 0x00, 0x21, 0x1D, 0x01, 0x01, 0x3A, 0x22, 0x63, 0x00},
	}
	for i, p := range bad {
		if _, ok := parsePowerState(p); ok {
			t.Errorf("case %d must not parse: % X", i, p)
		}
	}
}

// A press must fire at any battery level, plugged or not, short or long.
// The old match was only 3A 2A 64: plugged in at exactly 100%.
func TestPowerObservePress(t *testing.T) {
	now := time.Unix(1000, 0)
	for _, press := range [][]byte{pwr(0x2A, 0x64), pwr(0x28, 0x5E), pwr(0x08, 0x5E), pwr(0x10, 0x5E), pwr(0x30, 0x20)} {
		var tr powerTracker
		tr.observe(pwr(0x22, 0x5E), now)
		if !tr.observe(press, now) {
			t.Errorf("% X must be a press", press)
		}
	}
}

// The flag repeats each second until Push3 clears it. One press, one edge.
func TestPowerObserveEdge(t *testing.T) {
	var tr powerTracker
	now := time.Unix(1000, 0)
	if tr.observe(pwr(0x22, 0x63), now) {
		t.Error("rest is not a press")
	}
	if !tr.observe(pwr(0x2A, 0x63), now) {
		t.Fatal("first flagged reply must fire")
	}
	if tr.observe(pwr(0x2A, 0x63), now.Add(time.Second)) {
		t.Error("a repeat of the same press must not fire again")
	}
	if tr.observe(pwr(0x22, 0x63), now.Add(2*time.Second)) {
		t.Error("flag cleared is not a press")
	}
	if !tr.observe(pwr(0x2A, 0x63), now.Add(10*time.Second)) {
		t.Error("a second press must fire")
	}
	if tr.observe([]byte{0xF0, 0x7E, 0x7F, 0x06, 0x01, 0xF7}, now) {
		t.Error("another SysEx is not a press")
	}
}

func TestPowerLatestStale(t *testing.T) {
	var tr powerTracker
	now := time.Unix(1000, 0)
	if _, ok := tr.latest(now); ok {
		t.Error("nothing seen yet")
	}
	tr.observe(pwr(0x21, 0x5E), now)
	if st, ok := tr.latest(now.Add(time.Second)); !ok || st.Level != 94 || st.Label() != "Charging" {
		t.Errorf("fresh reply: %+v ok=%v", st, ok)
	}
	if _, ok := tr.latest(now.Add(powerStaleAfter + time.Second)); ok {
		t.Error("an old reply must not show")
	}
}
