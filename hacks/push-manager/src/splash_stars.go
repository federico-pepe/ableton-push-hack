package main

import (
	"image"
	"image/color"
	"math/rand"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// Starfield behind the startup splash text. Stars fly out from the centre
// like the old Windows screensaver. Text stays static. ASCII only.

const (
	starCount    = 150
	starFrame    = time.Second / 30
	starRampMs   = 500
	starVersionM = 300 // version text shows after this many ms
)

type star struct{ x, y, z float64 }

func newStars(r *rand.Rand) []star {
	s := make([]star, starCount)
	for i := range s {
		s[i] = star{r.Float64()*2 - 1, r.Float64()*2 - 1, 0.05 + r.Float64()*0.95}
	}
	return s
}

// stepStars moves every star one frame closer. simMs is time since start.
func stepStars(s []star, r *rand.Rand, simMs int) {
	k := float64(simMs) / starRampMs
	if k > 1 {
		k = 1
	}
	v := 0.004 + 0.014*k*k
	for i := range s {
		s[i].z -= v
		if s[i].z <= 0.03 {
			s[i] = star{r.Float64()*2 - 1, r.Float64()*2 - 1, 1}
		}
	}
}

func drawStars(dst *image.NRGBA, s []star) {
	for _, st := range s {
		px := int(float64(dispVisW)/2 + st.x/st.z*float64(dispVisW)*0.12 + 0.5)
		py := int(float64(dispH)/2 + st.y/st.z*float64(dispH)*0.5 + 0.5)
		near := 1 - st.z
		size := 1
		if near > 0.8 {
			size = 3
		} else if near > 0.5 {
			size = 2
		}
		v := uint8((0.25 + 0.75*near) * 255)
		c := color.NRGBA{v, v, v, 255}
		for dy := 0; dy < size; dy++ {
			for dx := 0; dx < size; dx++ {
				x, y := px+dx, py+dy
				if x >= 0 && x < dispVisW && y >= 0 && y < dispH {
					dst.SetNRGBA(x, y, c)
				}
			}
		}
	}
}

// splashText draws s centred on cx, top row at top, pixel-doubled by scale.
func splashText(dst *image.NRGBA, s string, cx, top, scale int, c color.NRGBA) {
	face := basicfont.Face7x13
	b, _ := font.BoundString(face, s)
	w := (b.Max.X - b.Min.X).Round()
	h := (b.Max.Y - b.Min.Y).Round()
	if w <= 0 || h <= 0 {
		return
	}
	small := image.NewNRGBA(image.Rect(0, 0, w, h))
	d := font.Drawer{Dst: small, Src: image.NewUniform(c), Face: face,
		Dot: fixed.Point26_6{X: -b.Min.X, Y: -b.Min.Y}}
	d.DrawString(s)
	x0 := cx - w*scale/2
	for sy := 0; sy < h; sy++ {
		for sx := 0; sx < w; sx++ {
			p := small.NRGBAAt(sx, sy)
			if p.A == 0 {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					x, y := x0+sx*scale+dx, top+sy*scale+dy
					if x >= 0 && x < dispVisW && y >= 0 && y < dispH {
						dst.SetNRGBA(x, y, p)
					}
				}
			}
		}
	}
}

// renderStarSplash builds one frame: stars, black plate, text, version.
func renderStarSplash(s []star, msg, version string, simMs int) []byte {
	cv := image.NewNRGBA(image.Rect(0, 0, dispVisW, dispH))
	for i := 3; i < len(cv.Pix); i += 4 {
		cv.Pix[i] = 255
	}
	drawStars(cv, s)
	// black plate so the text stays readable
	tw := font.MeasureString(basicfont.Face7x13, msg).Round() * 2
	for y := 54; y < 106; y++ {
		for x := dispVisW/2 - tw/2 - 18; x < dispVisW/2+tw/2+18; x++ {
			if x >= 0 && x < dispVisW {
				cv.SetNRGBA(x, y, color.NRGBA{0, 0, 0, 255})
			}
		}
	}
	splashText(cv, msg, dispVisW/2, 67, 2, color.NRGBA{255, 255, 255, 255})
	if version != "" && simMs > starVersionM {
		splashText(cv, "v"+version, dispVisW/2, 142, 1, color.NRGBA{140, 140, 140, 255})
	}
	return imageToBGR565(cv)
}

// runStarSplash plays the starfield for d, then returns. Caller owns mode.
func runStarSplash(msg, version string, d time.Duration) error {
	r := rand.New(rand.NewSource(1))
	st := newStars(r)
	tick := time.NewTicker(starFrame)
	defer tick.Stop()
	start := time.Now()
	for {
		simMs := int(time.Since(start) / time.Millisecond)
		if err := shmWritePixels(renderStarSplash(st, msg, version, simMs)); err != nil {
			return err
		}
		if time.Since(start) >= d {
			return nil
		}
		stepStars(st, r, simMs)
		<-tick.C
	}
}
