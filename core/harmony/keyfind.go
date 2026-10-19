package harmony

import "math"

// keyfind.go -- the key of what someone plays, for a source that sends
// notes without a key: each chord adds its notes to a pitch-class
// histogram that slowly forgets, compared with the major and minor key
// profiles (Krumhansl-Kessler) in all twelve keys.

var (
	profileMajor = [12]float64{6.35, 2.23, 3.48, 2.33, 4.38, 4.09, 2.52, 5.19, 2.39, 3.66, 2.29, 2.88}
	profileMinor = [12]float64{6.33, 2.68, 3.52, 5.38, 2.60, 3.53, 2.54, 4.75, 3.98, 2.69, 3.34, 3.17}
)

// Major and natural-minor scale masks, the two a KeyFinder answers with.
const (
	ScaleMajor = maskIonian
	ScaleMinor = maskAeolian
)

// KeyFinder estimates a key from chords. Zero value ready.
type KeyFinder struct {
	hist [12]float64
	n    int
}

// keyMemory: each new chord weighs this much against all before it.
const keyMemory = 0.85

// Add counts a chord (mask relative to root), its bass weighed double.
func (k *KeyFinder) Add(root, mask, bass int) {
	if root < 0 {
		return
	}
	for i := range k.hist {
		k.hist[i] *= keyMemory
	}
	abs := Abs(mask, root)
	for pc := 0; pc < 12; pc++ {
		if abs&(1<<pc) != 0 {
			k.hist[pc]++
		}
	}
	k.hist[root] += 0.5
	if bass >= 0 {
		k.hist[bass] += 0.5
	}
	k.n++
}

// Key is the best key and its scale (ScaleMajor or ScaleMinor), and how
// sure (the correlation, -1..1). ok is false before three chords, or when
// nothing correlates better than 0.6.
func (k *KeyFinder) Key() (key, scale int, sure float64, ok bool) {
	best := -2.0
	for root := 0; root < 12; root++ {
		for _, p := range []struct {
			prof  *[12]float64
			scale int
		}{{&profileMajor, ScaleMajor}, {&profileMinor, ScaleMinor}} {
			if c := corr(&k.hist, p.prof, root); c > best {
				best, key, scale = c, root, p.scale
			}
		}
	}
	return key, scale, best, k.n >= 3 && best >= 0.6
}

// corr is the correlation of h with profile p rotated to root.
func corr(h, p *[12]float64, root int) float64 {
	var mh, mp float64
	for i := 0; i < 12; i++ {
		mh += h[i]
		mp += p[i]
	}
	mh, mp = mh/12, mp/12
	var sxy, sxx, syy float64
	for i := 0; i < 12; i++ {
		x, y := h[(i+root)%12]-mh, p[i]-mp
		sxy += x * y
		sxx += x * x
		syy += y * y
	}
	if sxx == 0 || syy == 0 {
		return 0
	}
	return sxy / math.Sqrt(sxx*syy)
}
