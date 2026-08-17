package display

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/federico-pepe/ableton-push-hack/core/push3"
)

func TestToBGR565Length(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, push3.VisW, push3.VisH))
	px := ToBGR565(img)
	if len(px) != push3.TotalBytes {
		t.Fatalf("len(px) = %d, want %d", len(px), push3.TotalBytes)
	}
}

func TestToBGR565DuplicateFrame(t *testing.T) {
	// push_hook.c relies on both frame halves being byte-identical.
	img := image.NewNRGBA(image.Rect(0, 0, push3.VisW, push3.VisH))
	for y := 0; y < push3.VisH; y++ {
		for x := 0; x < push3.VisW; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	px := ToBGR565(img)
	half1 := px[:push3.FrameBytes]
	half2 := px[push3.FrameBytes:]
	if !bytes.Equal(half1, half2) {
		t.Fatal("the two frame halves are not byte-identical")
	}
}

func TestToBGR565StridePadding(t *testing.T) {
	// Columns 960..1023 (the 64px stride padding) must stay zero.
	img := image.NewNRGBA(image.Rect(0, 0, push3.VisW, push3.VisH))
	for y := 0; y < push3.VisH; y++ {
		for x := 0; x < push3.VisW; x++ {
			img.Set(x, y, color.White)
		}
	}
	px := ToBGR565(img)
	for y := 0; y < push3.VisH; y++ {
		for x := push3.VisW; x < push3.Stride; x++ {
			off := (y*push3.Stride + x) * 2
			if px[off] != 0 || px[off+1] != 0 {
				t.Fatalf("padding pixel (%d,%d) not zero", x, y)
			}
		}
	}
}

func TestBGR565RoundTrip(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, push3.VisW, push3.VisH))
	// Pure white survives 565 quantization exactly (all bits set both ways).
	want := color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 255}
	for y := 0; y < push3.VisH; y++ {
		for x := 0; x < push3.VisW; x++ {
			img.SetNRGBA(x, y, want)
		}
	}
	px := ToBGR565(img)
	got := FromBGR565(px[:push3.FrameBytes])
	c := got.NRGBAAt(0, 0)
	if c.R != want.R || c.G != want.G || c.B != want.B {
		t.Fatalf("round-trip = %+v, want %+v", c, want)
	}
}
