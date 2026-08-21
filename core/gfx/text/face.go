package text

// face.go — opt-in alternate faces, additive to the package's default
// Face7x13. `golang.org/x/image` is already a dependency (for Face7x13
// itself) and already vendors the four gofont TTFs used here, so this adds
// no new dependency and no font file to ship.
//
// Face7x13 stays the default because it is a fixed 1-bit bitmap: cheap,
// deterministic, and — the part that matters — the reason ASCII-only used
// to be enforceable just by the font having no other glyphs. An
// antialiased outline face can render arbitrary Unicode if the font file
// has it, so DrawWith/WidthWith sanitize to ASCII themselves rather than
// relying on font coverage, the same guarantee Face7x13 gave for free.

import (
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
)

// Weight selects a style among the four gofont variants.
type Weight int

const (
	Regular Weight = iota
	Bold
	Italic
	BoldItalic
)

func (w Weight) source() []byte {
	switch w {
	case Bold:
		return gobold.TTF
	case Italic:
		return goitalic.TTF
	case BoldItalic:
		return gobolditalic.TTF
	default:
		return goregular.TTF
	}
}

type faceKey struct {
	w    Weight
	size float64
}

var (
	facesMu sync.Mutex
	parsed  = map[Weight]*opentype.Font{}
	faces   = map[faceKey]font.Face{}
)

// NewFace returns a font.Face for weight at size points, 72 DPI (so size
// points is approximately size pixels — Push's panel has no meaningful
// physical DPI). Faces are expensive to build and are cached: every caller
// asking for the same (weight, size) gets the same instance back.
func NewFace(w Weight, size float64) (font.Face, error) {
	facesMu.Lock()
	defer facesMu.Unlock()

	key := faceKey{w, size}
	if f, ok := faces[key]; ok {
		return f, nil
	}

	pf, ok := parsed[w]
	if !ok {
		var err error
		pf, err = opentype.Parse(w.source())
		if err != nil {
			return nil, err
		}
		parsed[w] = pf
	}

	f, err := opentype.NewFace(pf, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}
	faces[key] = f
	return f, nil
}
