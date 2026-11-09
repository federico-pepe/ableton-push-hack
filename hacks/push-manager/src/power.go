package main

// power.go -- Push's power state, read from the Live Port.
//
// Push3 asks the XMOS "power state?" once a second (GET_POWER_STATE, 0x3A).
// The reply goes to every reader of the Live Port, push-manager too, so we
// only listen. We send nothing.
//
//	F0 00 21 1D 01 01 3A <status> <level> F7
//	status 0x20  supply plugged in
//	       0x03  charge: 0 not charging, 1 charging, 2 unavailable
//	             (seen at 99% full, plugged or not)
//	       0x08  short power-button press waiting
//	       0x10  long power-button press waiting
//	level  battery 0..100
//
// Seen on device 2026-11-09: 22 63 plugged in 99%, 02 63 unplugged 99%,
// 00 5E unplugged 94%, 21 5E plugged in and charging 94%.
// Constants from Push2/sysex.pyc and Push2/battery_component.pyc.
//
// The press flags stay set until Push3 clears them (it sends 0x39 02 or
// 0x39 05, never us). So the same flag repeats each second. observe fires
// once per press, on the edge.

import (
	"sync"
	"time"
)

const (
	powerSupplyBit = 0x20
	powerShortBit  = 0x08
	powerLongBit   = 0x10
	powerChargeMsk = 0x03
	powerCharging  = 1

	// Push3 asks every second. After this long without a reply the level is
	// stale and the stats leave the battery out.
	powerStaleAfter = 5 * time.Second
)

// powerState is one decoded GET_POWER_STATE reply.
type powerState struct {
	Connected  bool // supply plugged in
	Charge     int  // 0 not charging, 1 charging, 2 unavailable
	Level      int  // battery percent
	ShortPress bool
	LongPress  bool
}

// parsePowerState decodes a GET_POWER_STATE reply. ok is false for any
// other message, so a stray byte never reads as a press.
func parsePowerState(p []byte) (st powerState, ok bool) {
	if len(p) != 10 || p[0] != 0xF0 || p[1] != 0x00 || p[2] != 0x21 ||
		p[3] != 0x1D || p[4] != 0x01 || p[5] != 0x01 || p[6] != 0x3A ||
		p[9] != 0xF7 {
		return st, false
	}
	status, level := p[7], p[8]
	if status > 0x7F || level > 100 {
		return st, false
	}
	return powerState{
		Connected:  status&powerSupplyBit != 0,
		Charge:     int(status & powerChargeMsk),
		Level:      int(level),
		ShortPress: status&powerShortBit != 0,
		LongPress:  status&powerLongBit != 0,
	}, true
}

// Pressed reports a short or a long press waiting.
func (s powerState) Pressed() bool { return s.ShortPress || s.LongPress }

// Label is the text for the stats panel. The UI colours "Charging".
func (s powerState) Label() string {
	switch {
	case !s.Connected:
		return "Discharging"
	case s.Charge == powerCharging:
		return "Charging"
	default:
		return "Plugged in"
	}
}

// powerTracker keeps the last reply and finds press edges.
type powerTracker struct {
	mu      sync.Mutex
	st      powerState
	at      time.Time
	pressed bool
}

var power powerTracker

// observe takes a SysEx from the Live Port. It reports true once when a
// press flag goes up. A repeat of the same flag, or any other SysEx, is false.
func (t *powerTracker) observe(p []byte, now time.Time) bool {
	st, ok := parsePowerState(p)
	if !ok {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.st, t.at = st, now
	edge := st.Pressed() && !t.pressed
	t.pressed = st.Pressed()
	return edge
}

// latest returns the last reply if it is fresh.
func (t *powerTracker) latest(now time.Time) (powerState, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.at.IsZero() || now.Sub(t.at) > powerStaleAfter {
		return powerState{}, false
	}
	return t.st, true
}
