// Package display holds the push_hook.so shared-memory pixel codec shared by
// any hack that renders frames for Push 3's screen. See CLAUDE.md's
// "Display-owning hacks" section — hacks other than push-manager reach the
// display over push-manager's HTTP API (core/pmclient), never this codec or
// the shm directly; push-manager remains the sole shm writer.
package display

import (
	"image"
	"image/color"
)

// ToBGR565 converts img into the push_hook.c framebuf pixel format: BGR565
// little-endian, 1024-pixel stride (960 visible + 64 padding), duplicated
// into both frame halves (Push 3 sends the frame twice per update).
func ToBGR565(img image.Image) []byte {
	if n, ok := img.(*image.NRGBA); ok && n.Rect == image.Rect(0, 0, VisW, VisH) {
		pixels := make([]byte, TotalBytes)
		EncodeFrame(n, pixels[:FrameBytes])
		copy(pixels[FrameBytes:], pixels[:FrameBytes])
		return pixels
	}
	b := img.Bounds()
	srcW := b.Max.X - b.Min.X
	srcH := b.Max.Y - b.Min.Y
	pixels := make([]byte, TotalBytes)

	for y := 0; y < VisH; y++ {
		for x := 0; x < VisW; x++ {
			sx := b.Min.X + x*srcW/VisW
			sy := b.Min.Y + y*srcH/VisH
			r16, g16, b16, _ := img.At(sx, sy).RGBA() // 0–65535
			b5 := uint16(b16 >> 11)
			g6 := uint16(g16 >> 10)
			r5 := uint16(r16 >> 11)
			v := (b5 << 11) | (g6 << 5) | r5
			off := (y*Stride + x) * 2
			lo, hi := byte(v), byte(v>>8)
			pixels[off] = lo
			pixels[off+1] = hi
			// duplicate into second frame (Push 3 sends frame twice)
			pixels[FrameBytes+off] = lo
			pixels[FrameBytes+off+1] = hi
		}
	}
	return pixels
}

// FromBGR565 is the reverse of ToBGR565: it decodes one framebuf frame
// (FrameBytes bytes, 1024-pixel stride) into a 960×160 NRGBA image,
// skipping the 64-pixel-per-row stride padding. Input is raw BGR565
// little-endian, no XOR.
func FromBGR565(px []byte) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, VisW, VisH))
	for y := 0; y < VisH; y++ {
		for x := 0; x < VisW; x++ {
			off := (y*Stride + x) * 2
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

// EncodeFrame writes one frame (FrameBytes: BGR565 little-endian, 1024-pixel
// stride, padding left zero) from a 960x160 NRGBA image, reading Pix
// directly. It allocates nothing, so a hack can encode every frame into the
// same buffer -- ToBGR565's general path calls img.At per pixel, which is
// 153,600 interface calls and allocations a frame. img must be exactly
// VisW x VisH at the origin; dst must be FrameBytes long.
func EncodeFrame(img *image.NRGBA, dst []byte) {
	for y := 0; y < VisH; y++ {
		src := img.Pix[y*img.Stride : y*img.Stride+VisW*4]
		row := dst[y*Stride*2 : y*Stride*2+VisW*2]
		for x := 0; x < VisW; x++ {
			r, g, b := uint16(src[x*4]), uint16(src[x*4+1]), uint16(src[x*4+2])
			v := (b>>3)<<11 | (g>>2)<<5 | r>>3
			row[x*2] = byte(v)
			row[x*2+1] = byte(v >> 8)
		}
	}
}

// wireXOR is the Push 2/3 display signal shaping, indexed by byte offset
// from the start of the frame.
var wireXOR = [4]byte{0xE7, 0xF3, 0xE7, 0xFF}

// ToWire XORs raw BGR565 pixels (as from ToBGR565) into dst, the bytes
// that go out on display endpoint 0x01. dst must be at least len(px) long.
// push_hook.c does the same per chunk; USB does it when no hook is around.
func ToWire(dst, px []byte) {
	for i, b := range px {
		dst[i] = b ^ wireXOR[i&3]
	}
}
