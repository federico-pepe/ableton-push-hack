package harmony

import "sort"

// chords.go -- naming a chord from the notes someone plays.

// shape is a chord quality: its mask above the root and its ASCII suffix
// (names reach Push's screen, which draws ASCII only).
type shape struct {
	mask   int
	suffix string
}

func m(iv ...int) int {
	r := 0
	for _, i := range iv {
		r |= 1 << (i % 12)
	}
	return r
}

// shapes, most common first: on a tie the earlier one names the chord.
var shapes = []shape{
	{m(0, 4, 7), ""}, {m(0, 3, 7), "m"}, {m(0, 4, 7, 10), "7"}, {m(0, 3, 7, 10), "m7"},
	{m(0, 4, 7, 11), "maj7"}, {m(0, 7), "5"}, {m(0, 5, 7), "sus4"}, {m(0, 2, 7), "sus2"},
	{m(0, 3, 6), "dim"}, {m(0, 4, 8), "aug"}, {m(0, 3, 6, 10), "m7b5"}, {m(0, 3, 6, 9), "dim7"},
	{m(0, 4, 7, 9), "6"}, {m(0, 3, 7, 9), "m6"}, {m(0, 2, 4, 7), "add9"}, {m(0, 2, 3, 7), "madd9"},
	{m(0, 2, 4, 7, 10), "9"}, {m(0, 2, 3, 7, 10), "m9"}, {m(0, 2, 4, 7, 11), "maj9"},
	{m(0, 5, 7, 10), "7sus4"}, {m(0, 3, 7, 11), "mMaj7"}, {m(0, 2, 4, 7, 9), "6/9"},
	{m(0, 4, 8, 10), "7#5"}, {m(0, 4, 6, 10), "7b5"}, {m(0, 1, 4, 7, 10), "7b9"},
	{m(0, 3, 4, 7, 10), "7#9"}, {m(0, 4, 6, 7, 11), "maj7#11"},
	{m(0, 2, 5, 7, 10), "11"}, {m(0, 2, 3, 5, 7, 10), "m11"}, {m(0, 4, 7, 9, 10), "13"},
	{m(0, 4, 10), "7"}, {m(0, 3, 10), "m7"}, {m(0, 4, 11), "maj7"}, // without a fifth
	{m(0), ""},
}

// Chord is a named chord.
type Chord struct {
	Root, Mask, Bass int // Mask relative to Root; Bass a pitch class
	Name             string
}

// Name is the chord's ASCII name: root, quality, and "/bass" when the
// lowest note is not the root ("C", "Am7", "F#m7b5", "C/E"). A mask no
// shape matches is named by its root alone.
func Name(root, mask, bass int) string {
	if root < 0 {
		return ""
	}
	s := NoteName(root)
	for _, sh := range shapes {
		if sh.mask == mask&0xFFF {
			s += sh.suffix
			break
		}
	}
	if bass >= 0 && bass != root {
		s += "/" + NoteName(bass)
	}
	return s
}

// Detect names the chord of notes (MIDI note numbers, any order and
// octave). The lowest note is the bass. Each pitch class played is tried
// as the root, against every shape: notes the shape has score, notes it
// lacks or adds cost, and the bass as root wins a tie -- so C E G A is C6
// when C is lowest and Am7 when A is. ok is false for no notes.
func Detect(notes []int) (Chord, bool) {
	if len(notes) == 0 {
		return Chord{Root: None, Bass: None}, false
	}
	lo := notes[0]
	set := 0
	for _, n := range notes {
		lo = min(lo, n)
		set |= 1 << PC(n)
	}
	bass := PC(lo)
	best, bestScore, bestShape := Chord{Root: bass, Mask: Rotate(set, bass), Bass: bass}, -1<<30, len(shapes)-1
	for r := 0; r < 12; r++ {
		if !Has(set, r) {
			continue
		}
		rel := Rotate(set, r)
		for i, sh := range shapes {
			hit := Count(rel & sh.mask)
			extra := Count(rel &^ sh.mask)
			miss := Count(sh.mask &^ rel)
			score := hit*100 - extra*70 - miss*50 - i // the order only breaks ties
			if r == bass {
				score += 60
			}
			if score > bestScore {
				bestScore, bestShape = score, i
				best = Chord{Root: r, Mask: rel, Bass: bass}
			}
		}
	}
	// named after the shape that won; Mask keeps the notes as played
	best.Name = NoteName(best.Root) + shapes[bestShape].suffix
	if best.Bass != best.Root {
		best.Name += "/" + NoteName(best.Bass)
	}
	return best, true
}

func sortedNotes(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}
