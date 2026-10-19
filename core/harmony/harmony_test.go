package harmony

import (
	"math/rand/v2"
	"testing"
	"time"
)

func TestRotateAbs(t *testing.T) {
	for mask := 0; mask < 1<<12; mask += 37 {
		for r := 0; r < 12; r++ {
			if got := Abs(Rotate(mask, r), r); got != mask {
				t.Fatalf("Abs(Rotate(%03x, %d)) = %03x", mask, r, got)
			}
		}
	}
	if Rotate(m(0, 4, 7), 4) != m(0, 3, 8) {
		t.Fatal("C major from E is E G C")
	}
}

// Every field survives the wire, the unknown ones too.
func TestEncodeDecode(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	pick := func() int {
		if rng.IntN(5) == 0 {
			return None
		}
		return rng.IntN(12)
	}
	for i := 0; i < 2000; i++ {
		h := Harmony{Key: pick(), Scale: rng.IntN(1 << 12), Root: pick(), Chord: rng.IntN(1 << 12), Bass: pick(), Playing: rng.IntN(2) == 0, Armed: rng.IntN(2) == 0}
		d := rng.IntN(MaxDelta + 1)
		b := Encode(h, d)
		for _, c := range b[1 : len(b)-1] {
			if c > 0x7F {
				t.Fatalf("a data byte above 7F: % X", b)
			}
		}
		got, ok := Decode(b)
		if !ok || got.Type != TypeHarmony || got.Harmony != h || got.Delta != d {
			t.Fatalf("round trip: %+v %d -> %+v ok %v (% X)", h, d, got, ok, b)
		}
	}
	if b := Encode(Harmony{Key: 0, Root: 0, Bass: 0}, MaxDelta+500); func() int { m, _ := Decode(b); return m.Delta }() != MaxDelta {
		t.Fatal("a delta past the maximum is not clamped")
	}
}

// Anything that is not ours, or is broken, is refused; a type we do not
// know, or bytes we do not know, are not.
func TestDecodeRefuses(t *testing.T) {
	good := Encode(Harmony{Key: 2, Scale: 0xAB5, Root: 9, Chord: m(0, 3, 7), Bass: None, Playing: true}, 6)
	for name, b := range map[string][]byte{
		"empty":         {},
		"another maker": {0xF0, 0x43, 0x50, 0x48, 0x01, 0x01, 0xF7},
		"newer version": append(append([]byte{}, good[:4]...), append([]byte{0x02}, good[5:]...)...),
		"no end":        good[:len(good)-1],
		"short":         append(append([]byte{}, good[:9]...), 0xF7),
		"high bit":      append(append(append([]byte{}, good[:7]...), 0x85), good[8:]...),
	} {
		if _, ok := Decode(b); ok {
			t.Errorf("%s: decoded", name)
		}
	}
	if m, ok := Decode([]byte{0xF0, 0x7D, 0x50, 0x48, 0x01, 0x55, 0x01, 0x02, 0xF7}); !ok || m.Type != 0x55 {
		t.Error("an unknown type should decode with its type only")
	}
	longer := append(append([]byte{}, good[:len(good)-1]...), 0x11, 0x22, 0xF7)
	if m, ok := Decode(longer); !ok || m.Harmony.Root != 9 || m.Delta != 6 {
		t.Error("fields added by a later version should be ignored")
	}
	for _, tp := range []int{TypeAlive, TypeBye} {
		if m, ok := Decode(EncodeType(tp)); !ok || m.Type != tp {
			t.Errorf("type %d does not round trip", tp)
		}
	}
}

// Every root, every common quality, root position and inversions.
func TestDetect(t *testing.T) {
	cases := []struct {
		iv     []int
		suffix string
	}{
		{[]int{0, 4, 7}, ""}, {[]int{0, 3, 7}, "m"}, {[]int{0, 4, 7, 10}, "7"}, {[]int{0, 3, 7, 10}, "m7"},
		{[]int{0, 4, 7, 11}, "maj7"}, {[]int{0, 3, 6}, "dim"}, {[]int{0, 4, 8}, "aug"}, {[]int{0, 5, 7}, "sus4"},
		{[]int{0, 2, 7}, "sus2"}, {[]int{0, 3, 6, 10}, "m7b5"}, {[]int{0, 7}, "5"}, {[]int{0, 4, 7, 14}, "add9"},
		{[]int{0, 4, 7, 10, 14}, "9"}, {[]int{0, 3, 7, 10, 14}, "m9"}, {[]int{0, 5, 7, 10}, "7sus4"},
	}
	for root := 0; root < 12; root++ {
		for _, c := range cases {
			base := 48 + root
			notes := []int{}
			for _, i := range c.iv {
				notes = append(notes, base+i)
			}
			got, ok := Detect(notes)
			want := NoteName(root) + c.suffix
			if !ok || got.Root != root || got.Name != want {
				t.Fatalf("%v: %q root %d, want %q", notes, got.Name, got.Root, want)
			}
			// the same notes spread over octaves, root still lowest
			wide := []int{base}
			for k, n := range notes[1:] {
				wide = append(wide, n+12*(k%2))
			}
			if got, _ := Detect(wide); got.Root != root || got.Name != want {
				t.Fatalf("spread %v: %q, want %q", wide, got.Name, want)
			}
		}
		// first inversion of a triad: a slash chord on its root
		inv := []int{52 + root, 55 + root, 60 + root}
		if got, _ := Detect(inv); got.Root != root || got.Bass != PC(52+root) {
			t.Fatalf("first inversion %v: %+v", inv, got)
		}
	}
	for notes, want := range map[[4]int]string{
		{48, 52, 55, 57}: "C6", {45, 48, 52, 55}: "Am7", {52, 55, 60, 64}: "C/E",
	} {
		if got, _ := Detect(notes[:]); got.Name != want {
			t.Errorf("%v: %q, want %q", notes, got.Name, want)
		}
	}
	if got, ok := Detect([]int{62}); !ok || got.Root != 2 || got.Name != "D" {
		t.Errorf("one note: %+v", got)
	}
	if _, ok := Detect(nil); ok {
		t.Error("no notes named a chord")
	}
}

func TestChordRoles(t *testing.T) {
	if th, _ := Third(m(0, 3, 7)); th != 3 {
		t.Error("minor third")
	}
	if th, _ := Third(m(0, 5, 7)); th != 5 {
		t.Error("sus4's fourth stands in for the third")
	}
	if _, ok := Third(m(0, 7)); ok {
		t.Error("a power chord has no third")
	}
	if f, _ := Fifth(m(0, 3, 6)); f != 6 {
		t.Error("diminished fifth")
	}
	if s, ok := Seventh(m(0, 4, 7)); ok {
		t.Errorf("a triad has no seventh (%d)", s)
	}
	// the sender's own scale when it holds the chord, else the chord's mode
	if got := ChordScale(Harmony{Key: 0, Scale: 0xAB5, Root: 2, Chord: m(0, 3, 7)}); got != Rotate(0xAB5, 2) {
		t.Errorf("Dm in C major: %03x, want D dorian", got)
	}
	if got := ChordScale(Harmony{Key: None, Root: 7, Chord: m(0, 4, 7, 10)}); got != maskMixolydian {
		t.Errorf("G7 alone: %03x, want mixolydian", got)
	}
	if got := ChordScale(Harmony{Key: 0, Scale: 0xAB5, Root: 4, Chord: m(0, 4, 7)}); got != maskIonian {
		t.Errorf("E major in C major (borrowed): %03x, want ionian", got)
	}
}

func TestGrouper(t *testing.T) {
	t0 := time.Unix(0, 0)
	ms := func(n int) time.Time { return t0.Add(time.Duration(n) * time.Millisecond) }
	var g Grouper
	// a chord pressed over 12 ms is one chord, named once it settles
	g.NoteOn(60, ms(0))
	g.NoteOn(64, ms(5))
	if _, ok := g.Due(ms(20)); ok {
		t.Fatal("named before it settled")
	}
	g.NoteOn(67, ms(12))
	c, ok := g.Due(ms(45))
	if !ok || c.Name != "C" {
		t.Fatalf("got %+v %v", c, ok)
	}
	// letting go changes nothing; the empty keyboard keeps C
	g.NoteOff(64)
	g.NoteOff(60)
	if _, ok := g.Due(ms(200)); ok {
		t.Fatal("a release named a chord")
	}
	g.NoteOff(67)
	if last, _ := g.Last(); last.Name != "C" {
		t.Fatal("the last chord is lost")
	}
	// a short chord, released before it settles, still counts
	g.NoteOn(57, ms(500))
	g.NoteOn(60, ms(502))
	g.NoteOn(64, ms(503))
	g.NoteOff(57)
	g.NoteOff(60)
	g.NoteOff(64)
	if c, ok := g.Due(ms(540)); !ok || c.Name != "Am" {
		t.Fatalf("staccato chord: %+v %v", c, ok)
	}
	// the same chord again is no news
	for _, n := range []int{57, 60, 64} {
		g.NoteOn(n, ms(800))
	}
	if _, ok := g.Due(ms(900)); ok {
		t.Fatal("the same chord twice")
	}
	// a note added to a held chord makes a new one with it
	g.NoteOn(67, ms(1000))
	if c, ok := g.Due(ms(1040)); !ok || c.Name != "Am7" {
		t.Fatalf("added seventh: %+v %v", c, ok)
	}
}

func TestKeyFinder(t *testing.T) {
	maj, min := m(0, 4, 7), m(0, 3, 7)
	for _, c := range []struct {
		chords [][2]int // root, mask
		key    int
		scale  int
	}{
		{[][2]int{{0, maj}, {5, maj}, {7, maj}, {0, maj}, {9, min}, {5, maj}, {7, maj}}, 0, ScaleMajor},          // C F G C Am F G
		{[][2]int{{9, min}, {2, min}, {4, maj}, {9, min}, {5, maj}, {2, min}, {4, maj}}, 9, ScaleMinor},          // Am Dm E Am F Dm E
		{[][2]int{{7, maj}, {2, maj}, {4, min}, {0, maj}, {7, maj}, {2, maj}}, 7, ScaleMajor},                    // G D Em C
		{[][2]int{{4, min}, {0, maj}, {7, maj}, {2, maj}, {4, min}, {0, maj}, {11, maj | 1<<10}}, 4, ScaleMinor}, // Em C G D Em C B7
	} {
		var k KeyFinder
		for _, ch := range c.chords {
			k.Add(ch[0], ch[1], ch[0])
		}
		key, sc, sure, ok := k.Key()
		if !ok || key != c.key || sc != c.scale {
			t.Errorf("%v: %s %03x (%.2f, %v), want %s %03x", c.chords, NoteName(key), sc, sure, ok, NoteName(c.key), c.scale)
		}
	}
	var k KeyFinder
	k.Add(0, maj, 0)
	if _, _, _, ok := k.Key(); ok {
		t.Error("a key from one chord")
	}
}
