package main

// release.go -- the surface goes back to Live when Live needs to be seen.
//
// A press of Push's power button makes Push3 ask "shut down?" on the
// screen, answered with the buttons. Home or an app hides that question and
// takes the answer. The press shows as one SysEx on the Push's Live Port
// (see isPowerPress), so push-manager gives the surface to Live then.
// Nothing latches: after Cancel, Shift + Help opens Home again.
//
// Also at start: push-manager clears a stale surface.
//
// midiflt is a file and keeps its enabled byte across a power loss, and the
// hook reads it as soon as Push3 starts. Left on, Live's "recover your
// work?" could not be answered and the device seemed to hang at boot. So
// push-manager switches the intercept off and gives the display back
// to Push3 when it starts.

import (
	"log"
)

// isPowerPress reports whether a SysEx is the power button going down:
// F0 00 21 1D 01 01 3A 2A 64 F7. At rest the same message carries 22 in
// the byte before 64, about once a second. Seen on device 2026-10-20: 4
// presses, 4 messages, none in 15 s of rest. Push3 asks "shut down?" about
// a second later; no file or log shows the press, see
// docs/push3-internals.md.
func isPowerPress(p []byte) bool {
	return len(p) == 10 && p[1] == 0x00 && p[2] == 0x21 && p[3] == 0x1D &&
		p[0] == 0xF0 && p[4] == 0x01 && p[5] == 0x01 &&
		p[6] == 0x3A && p[7] == 0x2A && p[8] == 0x64 && p[9] == 0xF7
}

// releaseSurface switches the MIDI intercept off and gives the display back
// to Push3. It reports whether anything had to change.
func releaseSurface() bool {
	changed := false
	if data := ensureMidiFilt(); data != nil && data[4] != 0 {
		data[4] = 0
		setMidiFiltPass(data, nil)
		changed = true
	}
	if shmGetMode() != ModePassthrough {
		if err := shmSetMode(ModePassthrough); err == nil {
			changed = true
		}
	}
	return changed
}

// clearStaleSurface runs once at start: whatever a hack held before a
// reboot or a power loss, nobody holds it now.
func clearStaleSurface() {
	if releaseSurface() {
		log.Printf("release: intercept and display were still taken from before this start; given back to Push3")
	}
}
