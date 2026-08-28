package main

// Runs on the Push / linux CI (the package imports core/alsaseq, linux-only):
// `GOOS=linux GOARCH=amd64 go test ./...`. Covers the chord state machine —
// both-held gating, per-trigger debounce, and the two independent chords.

import (
	"testing"
	"time"
)

func resetChord() {
	chordMu.Lock()
	chordHeld = map[uint8]bool{}
	chordLastFire = map[uint8]time.Time{}
	chordMu.Unlock()
}

// helper: names of chord kinds fired by one CC event.
func fired(cc, val byte, now time.Time) []string {
	out := []string{}
	for _, c := range firedChords(cc, val, now) {
		out = append(out, c.kind)
	}
	return out
}

func TestChordsFireIndependently(t *testing.T) {
	resetChord()
	t0 := time.Now()

	// Shift alone: nothing.
	if got := fired(ccShift, 127, t0); len(got) != 0 {
		t.Fatalf("Shift alone fired %v", got)
	}
	// Shift+Add -> preset only.
	if got := fired(ccAdd, 127, t0.Add(10*time.Millisecond)); len(got) != 1 || got[0] != "preset" {
		t.Fatalf("Shift+Add fired %v, want [preset]", got)
	}
	// Shift+Swap (Shift still held, Add released first) -> drumrack only.
	fired(ccAdd, 0, t0.Add(20*time.Millisecond))
	if got := fired(ccSwap, 127, t0.Add(30*time.Millisecond)); len(got) != 1 || got[0] != "drumrack" {
		t.Fatalf("Shift+Swap fired %v, want [drumrack]", got)
	}
}

func TestChordDebouncePerTrigger(t *testing.T) {
	resetChord()
	t0 := time.Now()
	fired(ccShift, 127, t0)
	if got := fired(ccAdd, 127, t0); len(got) != 1 {
		t.Fatalf("first Add press should fire, got %v", got)
	}
	// Re-press Add inside the window: no fire.
	if got := fired(ccAdd, 127, t0.Add(200*time.Millisecond)); len(got) != 0 {
		t.Fatalf("re-fired inside debounce: %v", got)
	}
	// After the window: fires again.
	if got := fired(ccAdd, 127, t0.Add(700*time.Millisecond)); len(got) != 1 {
		t.Fatalf("should re-fire after debounce, got %v", got)
	}
	// Releasing Shift gates everything.
	fired(ccShift, 0, t0.Add(800*time.Millisecond))
	if got := fired(ccAdd, 127, t0.Add(2000*time.Millisecond)); len(got) != 0 {
		t.Fatalf("fired with Shift released: %v", got)
	}
}
