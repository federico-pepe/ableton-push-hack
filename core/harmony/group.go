package harmony

import "time"

// group.go -- notes into chords. Someone playing a chord presses its notes
// a few milliseconds apart; a sequencer's arpeggio plays them one by one.
// The Grouper waits until no new note has started for Window, then names
// what was played in that time together with what is still held. Lifting
// notes changes nothing (a chord let go note by note is still that chord
// until the next one), and an empty keyboard keeps the last chord.

// DefaultWindow is how long a chord's notes may take to arrive.
const DefaultWindow = 30 * time.Millisecond

// Grouper turns one source's notes into chords. Not safe for concurrent
// use: one per source, called from one goroutine.
type Grouper struct {
	Window time.Duration // 0: DefaultWindow

	held   map[int]bool
	group  map[int]bool
	lastOn time.Time
	dirty  bool
	last   Chord
	has    bool
}

// NoteOn records a note starting at at.
func (g *Grouper) NoteOn(note int, at time.Time) {
	if g.held == nil {
		g.held = map[int]bool{}
	}
	if !g.dirty { // a new group: what is held belongs to it
		g.group = map[int]bool{}
		for n := range g.held {
			g.group[n] = true
		}
	}
	g.held[note], g.group[note] = true, true
	g.lastOn, g.dirty = at, true
}

// NoteOff records a note ending.
func (g *Grouper) NoteOff(note int) { delete(g.held, note) }

// Due is the chord played, once it has settled (Window after the last note
// started) and only when it differs from the one before.
func (g *Grouper) Due(now time.Time) (Chord, bool) {
	w := g.Window
	if w <= 0 {
		w = DefaultWindow
	}
	if !g.dirty || now.Sub(g.lastOn) < w {
		return Chord{}, false
	}
	g.dirty = false
	c, ok := Detect(sortedNotes(g.group))
	if !ok || (g.has && c.Root == g.last.Root && c.Mask == g.last.Mask && c.Bass == g.last.Bass) {
		return Chord{}, false
	}
	g.last, g.has = c, true
	return c, true
}

// Pending reports whether a group is waiting to settle.
func (g *Grouper) Pending() bool { return g.dirty }

// Last is the last chord named, ok false before the first.
func (g *Grouper) Last() (Chord, bool) { return g.last, g.has }
