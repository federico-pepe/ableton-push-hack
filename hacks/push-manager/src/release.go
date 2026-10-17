package main

// release.go -- the surface goes back to Push3 whenever Push3 has to be
// able to talk to you.
//
// A hack that owns the surface has the display (mode 2) and the MIDI
// intercept (midiflt enabled): you see the hack's screen, and Push3 gets
// none of your button presses. Two moments need Push3 to have both:
//
//   - Push's power button. /opt/push3/power.sh touches
//     /data/settings/.shutdown and asks Live to quit. If Live asks a
//     question first ("save changes?"), it asks on Push's screen, answered
//     with Push's buttons -- hidden behind the hack's screen and swallowed
//     by the intercept. Live never quits; only holding the power button
//     ends it. Seen on device 2026-10-17 with Stacks open.
//   - The next start. midiflt is a file and keeps its enabled byte across a
//     power loss, and the hook reads it as soon as Push3 starts: Live's
//     "recover your work?" could not be answered, and the device seemed to
//     hang at boot, twice in a row.
//
// So push-manager clears both at its start, and from the power button on
// gives them back to Push3 four times a second -- overriding any writer,
// its own Shadow UI included -- and refuses a hack that asks for them.

import (
	"log"
	"os"
	"sync/atomic"
	"time"
)

const shutdownFile = "/data/settings/.shutdown" // touched by /opt/push3/power.sh

// shuttingDown is set once the power button has been pressed.
var shuttingDown atomic.Bool

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

// shutdownStamp identifies the shutdown marker as it is now: its absence,
// or its modification time. Compared with the stamp at start rather than
// with the clock, which is not to be trusted right after boot -- the file
// stays from the last shutdown.
func shutdownStamp() time.Time {
	fi, err := os.Stat(shutdownFile)
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// watchShutdown gives the surface back the moment the power button is
// pressed, and keeps it given back until the device is off.
func watchShutdown() {
	start := shutdownStamp()
	for {
		time.Sleep(250 * time.Millisecond)
		if !shuttingDown.Load() {
			if shutdownStamp().Equal(start) {
				continue
			}
			shuttingDown.Store(true)
			log.Printf("release: power button pressed; the surface goes back to Push3 until it is off")
		}
		releaseSurface()
	}
}
