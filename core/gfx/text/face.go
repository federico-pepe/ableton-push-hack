package text

// face.go — opt-in alternate faces, additive to the package's default
// Face7x13. The four styled variants are Helvetica Neue (Thin/Medium
// standing in for Regular/Bold, both with their italics) — NOT embedded,
// unlike the basic face: Helvetica Neue's .otf files are Apple/Monotype-
// licensed, not freely redistributable, and both repos importing this
// package are public on GitHub. Instead, source() loads them at runtime
// from a local, gitignored directory (PUSHAPP_STYLED_FONT_DIR) that each
// developer populates themselves, falling back to the vendored gofont
// TTFs (the pre-2026-08-22 default) when that directory or file isn't
// there — so a fresh clone and CI (neither of which has Helvetica Neue)
// still build and render correctly, just with the generic font instead of
// the product's own look.
//
// Face7x13 stays the default because it is a fixed 1-bit bitmap: cheap,
// deterministic, and — the part that matters — the reason ASCII-only used
// to be enforceable just by the font having no other glyphs. An
// antialiased outline face can render arbitrary Unicode if the font file
// has it, so DrawWith/WidthWith sanitize to ASCII themselves rather than
// relying on font coverage, the same guarantee Face7x13 gave for free.

import (
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
)

// Weight selects a style among the four styled variants.
type Weight int

const (
	Regular Weight = iota
	Bold
	Italic
	BoldItalic
)

// helveticaFile is the local filename (under PUSHAPP_STYLED_FONT_DIR) each
// weight resolves to when a developer has supplied their own copy.
func (w Weight) helveticaFile() string {
	switch w {
	case Bold:
		return "HelveticaNeueMedium.otf"
	case Italic:
		return "HelveticaNeueThinItalic.otf"
	case BoldItalic:
		return "HelveticaNeueMediumItalic.otf"
	default:
		return "HelveticaNeueThin.otf"
	}
}

// gofontFallback is what source() returns when no local Helvetica Neue
// file is available — the same generic, always-vendored faces this
// package used before the Helvetica Neue swap.
func (w Weight) gofontFallback() []byte {
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

func (w Weight) source() []byte {
	if dir := os.Getenv("PUSHAPP_STYLED_FONT_DIR"); dir != "" {
		if b, err := os.ReadFile(filepath.Join(dir, w.helveticaFile())); err == nil {
			return b
		}
	}
	return w.gofontFallback()
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
		Hinting: font.HintingNone,
	})
	if err != nil {
		return nil, err
	}
	faces[key] = f
	return f, nil
}
