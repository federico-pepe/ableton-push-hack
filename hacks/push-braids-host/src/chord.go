package main

// chord.go — Shift+Device (CC49+CC110, docs/push3-button-map.md) chord
// detection that toggles the on-screen param UI. Pattern mirrors
// hacks/keyboard-visualizer/src/chord.go's Shift+Note detector (itself
// modeled on push-manager's chordCCPressed/chordCCReleased) — same 500ms
// debounce, same held-set-of-two-CCs shape.

import (
	"sync"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/push3"
)

const (
	ccShift  = uint8(push3.CCShift)
	ccDevice = uint8(push3.CCDeviceView)

	chordDebounce = 500 * time.Millisecond
)

var (
	chordMu       sync.Mutex
	chordHeld     = map[uint8]bool{}
	chordLastFire time.Time
)

// onChordCC is called for every channel-0 CC event. When Shift+Device are
// held together (debounced 500ms), toggles the on-screen param UI.
func onChordCC(cc, val byte, pmURL string, st *paramState, io *ioState) {
	chordMu.Lock()
	if val > 0 {
		chordHeld[cc] = true
	} else {
		delete(chordHeld, cc)
	}
	fire := chordHeld[ccShift] && chordHeld[ccDevice]
	if fire {
		now := time.Now()
		if now.Sub(chordLastFire) < chordDebounce {
			fire = false
		} else {
			chordLastFire = now
		}
	}
	chordMu.Unlock()

	if fire {
		go toggleUI(pmURL, st, io)
	}
}
