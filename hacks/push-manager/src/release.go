package main

// release.go -- the surface goes back to Live when Live needs to be seen.
//
// A press of Push's power button makes Push3 ask "shut down?" on the
// screen, answered with the buttons. Home or an app hides that question and
// takes the answer. The press shows as a flag in the power-state SysEx on
// the Push's Live Port (see power.go), so push-manager gives the surface to
// Live then.
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

// restoreLEDsAtStart is set when start found the intercept on. The old
// process cleared the LEDs and died before it could repaint them; main
// repaints once MIDI is up.
var restoreLEDsAtStart bool

// clearStaleSurface runs once at start: whatever a hack held before a
// reboot or a power loss, nobody holds it now.
func clearStaleSurface() {
	if data := ensureMidiFilt(); data != nil && data[4] != 0 {
		// watchInterceptOff never sees this on->off: it starts after.
		ledsCleared.Store(true)
		restoreLEDsAtStart = true
	}
	if releaseSurface() {
		log.Printf("release: intercept and display were still taken from before this start; given back to Push3")
	}
}
