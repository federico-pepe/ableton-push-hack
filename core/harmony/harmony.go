// Package harmony is Push Hack Harmony: how one app tells others what it
// plays -- the key, the scale, the chord of the moment -- over MIDI, and how
// a follower names a chord from plain notes. The wire format is an open
// spec (docs/harmony-sync.md); anything that sends it, or just plays notes,
// can drive a follower such as Riffs.
//
// Pitch classes are 0-11 from C. A set of them is a 12-bit mask, bit n = n
// semitones; a scale's mask is relative to its key, a chord's to its root.
package harmony

import "math/bits"

// None marks a field that is not known (key, root or bass).
const None = -1

// Harmony is what a sender plays: the key and scale it is in, and the
// chord sounding.
type Harmony struct {
	Key     int  // 0-11, or None
	Scale   int  // mask relative to Key; 0 when unknown
	Root    int  // the chord's root 0-11, or None
	Chord   int  // mask relative to Root; 0 when unknown
	Bass    int  // the lowest note's pitch class, or None (= the root)
	Playing bool // the sender plays this now
	Armed   bool // the sender plays it when the transport starts (stopped now)
}

// Live reports whether the sender is, or will be, playing h: a follower
// follows such a sender and treats any other (closed, silent) as away.
func (h Harmony) Live() bool { return h.Playing || h.Armed }

// Valid reports whether h says anything a follower can use.
func (h Harmony) Valid() bool { return h.Root != None || h.Key != None }

// Same reports whether a and b are the same harmony (Playing aside).
func (a Harmony) Same(b Harmony) bool {
	return a.Key == b.Key && a.Scale == b.Scale && a.Root == b.Root && a.Chord == b.Chord && a.Bass == b.Bass
}

// Rotate is mask m seen from pitch class r: bit (n - r) for bit n.
func Rotate(m, r int) int {
	r = ((r % 12) + 12) % 12
	m &= 0xFFF
	return ((m >> r) | (m << (12 - r))) & 0xFFF
}

// Abs is mask m (relative to root r) as absolute pitch classes.
func Abs(m, r int) int { return Rotate(m, 12-((r%12+12)%12)) }

// Count is how many pitch classes are in mask m.
func Count(m int) int { return bits.OnesCount16(uint16(m & 0xFFF)) }

// Has reports whether interval i (any octave) is in mask m.
func Has(m, i int) bool { return m&(1<<(((i%12)+12)%12)) != 0 }

// PC is a MIDI note's pitch class.
func PC(note int) int { return ((note % 12) + 12) % 12 }

var noteNames = []string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// NoteName is pitch class pc as ASCII ("F#"), "" for None.
func NoteName(pc int) string {
	if pc < 0 {
		return ""
	}
	return noteNames[pc%12]
}

// Third is the chord's third, as an interval above the root: 4 or 3, or
// the note that takes its place in a sus chord (5, then 2); ok false when
// the chord has none of them (a power chord, a single note).
func Third(chord int) (int, bool) {
	for _, i := range []int{4, 3, 5, 2} {
		if Has(chord, i) {
			return i, true
		}
	}
	return 0, false
}

// Fifth is the chord's fifth: 7, or 6 (diminished) or 8 (augmented).
func Fifth(chord int) (int, bool) {
	for _, i := range []int{7, 6, 8} {
		if Has(chord, i) {
			return i, true
		}
	}
	return 0, false
}

// Seventh is the chord's seventh: 10 or 11, or 9 in a dim7 (no 7 or 10);
// a 6 chord's sixth counts as its seventh, the note that colours it.
func Seventh(chord int) (int, bool) {
	for _, i := range []int{10, 11, 9} {
		if Has(chord, i) {
			return i, true
		}
	}
	return 0, false
}

// Scales a follower can fall back to when a chord comes without a key:
// the mode that fits the chord's quality best.
const (
	maskIonian     = 0x0AB5
	maskMixolydian = 0x06B5
	maskAeolian    = 0x05AD
	maskDorian     = 0x06AD
	maskLocrian    = 0x056B
	maskLydian     = 0x0AD5
	maskWholeTone  = 0x0555
	maskDimHW      = 0x0B6D // half-whole diminished
)

// ChordScale is a scale (mask relative to the chord's root) that holds
// the chord: the sender's own scale when it holds every chord note, else
// the mode its quality implies -- Ionian for major, Mixolydian when there
// is a flat seventh, Dorian for minor (Aeolian with a flat sixth),
// Locrian for half-diminished, and so on.
func ChordScale(h Harmony) int {
	if h.Root == None {
		return h.Scale
	}
	if h.Key != None && h.Scale != 0 {
		own := Rotate(Abs(h.Scale, h.Key), h.Root)
		if own&h.Chord == h.Chord {
			return own
		}
	}
	c := h.Chord
	switch {
	case Has(c, 3) && Has(c, 6) && Has(c, 9) && !Has(c, 10):
		return maskDimHW
	case Has(c, 3) && Has(c, 6):
		return maskLocrian
	case Has(c, 4) && Has(c, 8):
		return maskWholeTone
	case Has(c, 3):
		if Has(c, 8) {
			return maskAeolian
		}
		return maskDorian
	case Has(c, 4) && Has(c, 6) && !Has(c, 5):
		return maskLydian
	case Has(c, 10):
		return maskMixolydian
	}
	return maskIonian
}
