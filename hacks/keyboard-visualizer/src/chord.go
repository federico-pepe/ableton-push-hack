package main

// chord.go — Shift+Note (CC49+CC50, docs/push3-button-map.md) chord detection
// that toggles display takeover on/off. The CC events themselves arrive via
// the "Keyboard Viz In" port's self-subscription to Push 3's hardware port
// (see maintainPush3Subscription/processSeqBuf in midi.go) — this file only
// holds the chord state machine and the Push 3 port-detection helper.
// Pattern mirrors push-manager's existing hardware-chord detector
// (hacks/push-manager/src/midi.go: chordCCPressed/chordCCReleased) but
// scoped to this single hardcoded chord.

import (
	"sync"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
	"github.com/federico-pepe/ableton-push-hack/core/push3"
)

const (
	ccShift = uint8(push3.CCShift)
	ccNote  = uint8(push3.CCNote)

	chordDebounce = 500 * time.Millisecond
)

var (
	chordMu       sync.Mutex
	chordHeld     = map[uint8]bool{}
	chordLastFire time.Time
)

// onChordCC is called for every CC49/CC50 event seen on the shared "Keyboard
// Viz In" port. When both are held together (debounced 500ms), toggles
// display takeover.
func onChordCC(cc, val byte, pushManagerURL string) {
	chordMu.Lock()
	if val > 0 {
		chordHeld[cc] = true
	} else {
		delete(chordHeld, cc)
	}
	fire := chordHeld[ccShift] && chordHeld[ccNote]
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
		toggleTakeover(pushManagerURL)
	}
}

// detectPush3Port scans /proc/asound/seq/clients for "Ableton Push 3 Live
// Port". requireCaps=0 — matches on port name alone, unlike push-manager's
// and automation's equivalents which require the R capability bit — because
// this hack only watches CC49/50 on the port, it never subscribes for a
// dedicated read.
func detectPush3Port() (client, port byte, ok bool) {
	p, found := alsaseq.FindByName("Ableton Push 3 Live Port", 0)
	if !found {
		return 0, 0, false
	}
	return p.Addr.Client, p.Addr.Port, true
}
