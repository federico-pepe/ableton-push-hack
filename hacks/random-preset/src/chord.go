package main

// chord.go — Shift + <button> chord detection. Two chords, each firing a random
// load and a green LED flash on its own button:
//   Shift + Add  (CC49+CC32) -> random preset
//   Shift + Swap (CC49+CC33) -> random drum rack
// State machine mirrors keyboard-visualizer/push-manager: track held buttons,
// fire when Shift + the trigger are both down, debounced 500ms per trigger.

import (
	"sync"
	"time"

	"github.com/federico-pepe/ableton-push-hack/core/alsaseq"
	"github.com/federico-pepe/ableton-push-hack/core/push3"
)

const (
	ccShift = uint8(push3.CCShift) // 49
	ccAdd   = uint8(push3.CCAdd)   // 32
	ccSwap  = uint8(push3.CCSwap)  // 33

	chordDebounce = 500 * time.Millisecond
)

// chordDef: Shift + trigger fires a load of `kind`.
type chordDef struct {
	trigger uint8
	kind    string // "preset" | "drumrack"
}

var chords = []chordDef{
	{trigger: ccAdd, kind: "preset"},
	{trigger: ccSwap, kind: "drumrack"},
}

// triggers is the set of CCs the chord watcher cares about (Shift + every
// trigger). midi.go only feeds these through.
var triggers = func() map[uint8]bool {
	m := map[uint8]bool{ccShift: true}
	for _, c := range chords {
		m[c.trigger] = true
	}
	return m
}()

var (
	chordMu       sync.Mutex
	chordHeld     = map[uint8]bool{}
	chordLastFire = map[uint8]time.Time{} // per trigger
)

// onChordCC updates held-state for cc and fires any chords now satisfied, each
// off the MIDI thread (LED HTTP + load HTTP must not block the read loop).
func onChordCC(cc, val byte, pmURL string) {
	for _, c := range firedChords(cc, val, time.Now()) {
		kind := c.kind
		go loadRandom(pmURL, kind)
	}
}

// firedChords is the pure decision: update held state for cc, then fire only a
// chord whose pair this very PRESS completes — i.e. the incoming press is the
// chord's trigger with Shift already down, or Shift with the trigger already
// down. A release (val==0) never fires, and pressing an unrelated button never
// fires a chord that merely happens to be held. Debounced per trigger.
// Split out so it's unit-testable without ALSA or the network.
func firedChords(cc, val byte, now time.Time) []chordDef {
	chordMu.Lock()
	defer chordMu.Unlock()
	if val > 0 {
		chordHeld[cc] = true
	} else {
		delete(chordHeld, cc)
		return nil
	}
	var out []chordDef
	for _, c := range chords {
		completes := (cc == c.trigger && chordHeld[ccShift]) || (cc == ccShift && chordHeld[c.trigger])
		if completes && now.Sub(chordLastFire[c.trigger]) >= chordDebounce {
			chordLastFire[c.trigger] = now
			out = append(out, c)
		}
	}
	return out
}

// detectPush3Port finds "Ableton Push 3 Live Port" by name (requireCaps=0 —
// we only watch a few CCs, never do a capability-filtered read).
func detectPush3Port() (client, port byte, ok bool) {
	p, found := alsaseq.FindByName("Ableton Push 3 Live Port", 0)
	if !found {
		return 0, 0, false
	}
	return p.Addr.Client, p.Addr.Port, true
}
