// led_restore.go — bring Push's button and pad LEDs back after the Shadow UI.
//
// Turning MIDI intercept on clears every button and pad LED (clearAllLEDs) so
// the Shadow UI starts clean. Push3 only re-sends an LED when its own state
// changes, so when intercept goes off the buttons stay dark and the pad grid
// stays black until the player presses something. Tested on device: Push3
// repaints the buttons on a Shift press and the pad grid on a pad-mode change.
// So on intercept OFF we press Shift, then the other pad-mode button (Note or
// Session) and then the current one, straight into Push3's own MIDI input,
// the same as a player would.
package main

import (
	"log"
	"sync/atomic"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
)

// ledsCleared is set by clearAllLEDs and cleared once restorePushLEDs ran, so
// an intercept toggle that never cleared anything does not flicker the mode.
var ledsCleared atomic.Bool

// lastModeCC is the pad-mode button (CCNote or CCSession) the player pressed
// last while intercept was off. 0 = none seen yet.
var lastModeCC atomic.Uint32

// Push3's own MIDI input, where hardware events enter the app.
const appInputPortName = "Ableton Internal Input"

// How long to wait after intercept goes off before the mode presses: the
// Shadow UI is still tearing down its own LEDs, and a plugin load may be
// running. Tune on device.
const ledRestoreDelay = 600 * time.Millisecond

// trackModeButton remembers the last pad-mode press. Called for every
// hardware button press; ignored while intercept is on, because Push3 does
// not see those presses.
func trackModeButton(cc uint8) {
	if cc != CCNote && cc != CCSession {
		return
	}
	if filt := ensureMidiFilt(); filt == nil || filt[4] == 1 {
		return
	}
	lastModeCC.Store(uint32(cc))
}

func tapAppButton(dst alsaseq.Addr, cc byte) {
	midiOutMu.Lock()
	c := midiOut
	midiOutMu.Unlock()
	if c == nil {
		return
	}
	_ = c.SendCC(dst, 0, cc, 127)
	time.Sleep(40 * time.Millisecond)
	_ = c.SendCC(dst, 0, cc, 0)
}

// restorePushLEDs repaints Push3's LEDs by toggling the pad mode away and
// back. Does nothing unless the LEDs were cleared.
func restorePushLEDs() {
	if !ledsCleared.Swap(false) {
		return
	}
	p, ok := alsaseq.FindByName(appInputPortName, alsaseq.CapWrite)
	if !ok {
		log.Printf("led_restore: %q not found, LEDs stay dark until a button press", appInputPortName)
		return
	}
	cur, other := byte(CCNote), byte(CCSession)
	if uint8(lastModeCC.Load()) == CCSession {
		cur, other = CCSession, CCNote
	}
	// Shift repaints the buttons. A chord exit already sends a real Shift,
	// but a preset or plugin load does not, so always send one. The pad grid
	// only repaints on a pad-mode change.
	tapAppButton(p.Addr, CCShift)
	time.Sleep(150 * time.Millisecond)
	tapAppButton(p.Addr, other)
	time.Sleep(150 * time.Millisecond)
	tapAppButton(p.Addr, cur)
	log.Printf("led_restore: shift, then pad mode %d -> %d -> %d", cur, other, cur)
}

// watchInterceptOff runs restorePushLEDs whenever intercept goes from on to
// off, whatever turned it off (chord, MIDI tab, web, or a preset load).
func watchInterceptOff() {
	wasOn := false
	for {
		time.Sleep(100 * time.Millisecond)
		on := false
		if filt := ensureMidiFilt(); filt != nil {
			on = filt[4] == 1
		}
		if wasOn && !on {
			go func() {
				time.Sleep(ledRestoreDelay)
				restorePushLEDs()
			}()
		}
		wasOn = on
	}
}
