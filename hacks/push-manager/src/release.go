package main

// release.go -- push-manager clears a stale surface at its start.
//
// midiflt is a file and keeps its enabled byte across a power loss, and the
// hook reads it as soon as Push3 starts. Left on, Live's "recover your
// work?" could not be answered and the device seemed to hang at boot. So
// push-manager switches the intercept off and gives the display back
// to Push3 when it starts.

import (
	"log"
)

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
