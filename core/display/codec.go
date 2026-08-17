// Package display holds the push_hook.so shared-memory pixel codec shared by
// any hack that renders frames for Push 3's screen. See CLAUDE.md's
// "Display-owning hacks" section — hacks other than push-manager reach the
// display over push-manager's HTTP API (core/pmclient), never this codec or
// the shm directly; push-manager remains the sole shm writer.
package display

import (
	"image"
	"image/color"

	"github.com/federico-pepe/ableton-push-hack/core/push3"
)

// ToBGR565 converts img into the push_hook.c framebuf pixel format: BGR565
// little-endian, 1024-pixel stride (960 visible + 64 padding), duplicated
// into both frame halves (Push 3 sends the frame twice per update).
func ToBGR565(img image.Image) []byte {
	b := img.Bounds()
	srcW := b.Max.X - b.Min.X
	srcH := b.Max.Y - b.Min.Y
	pixels := make([]byte, push3.TotalBytes)

	for y := 0; y < push3.VisH; y++ {
		for x := 0; x < push3.VisW; x++ {
			sx := b.Min.X + x*srcW/push3.VisW
			sy := b.Min.Y + y*srcH/push3.VisH
			r16, g16, b16, _ := img.At(sx, sy).RGBA() // 0–65535
			b5 := uint16(b16 >> 11)
			g6 := uint16(g16 >> 10)
			r5 := uint16(r16 >> 11)
			v := (b5 << 11) | (g6 << 5) | r5
			off := (y*push3.Stride + x) * 2
			lo, hi := byte(v), byte(v>>8)
			pixels[off] = lo
			pixels[off+1] = hi
			// duplicate into second frame (Push 3 sends frame twice)
			pixels[push3.FrameBytes+off] = lo
			pixels[push3.FrameBytes+off+1] = hi
		}
	}
	return pixels
}

// FromBGR565 is the reverse of ToBGR565: it decodes one framebuf frame
// (push3.FrameBytes bytes, 1024-pixel stride) into a 960×160 NRGBA image,
// skipping the 64-pixel-per-row stride padding. Input is raw BGR565
// little-endian, no XOR.
func FromBGR565(px []byte) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, push3.VisW, push3.VisH))
	for y := 0; y < push3.VisH; y++ {
		for x := 0; x < push3.VisW; x++ {
			off := (y*push3.Stride + x) * 2
			v := uint16(px[off]) | uint16(px[off+1])<<8
			b5 := (v >> 11) & 0x1f
			g6 := (v >> 5) & 0x3f
			r5 := v & 0x1f
			img.SetNRGBA(x, y, color.NRGBA{
				R: byte(r5<<3 | r5>>2),
				G: byte(g6<<2 | g6>>4),
				B: byte(b5<<3 | b5>>2),
				A: 255,
			})
		}
	}
	return img
}
