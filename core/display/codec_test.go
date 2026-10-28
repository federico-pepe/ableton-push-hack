package display

import (
	"bytes"
	"image"
	"image/color"
	"testing"
)

func TestToBGR565Length(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, VisW, VisH))
	px := ToBGR565(img)
	if len(px) != TotalBytes {
		t.Fatalf("len(px) = %d, want %d", len(px), TotalBytes)
	}
}

func TestToBGR565DuplicateFrame(t *testing.T) {
	// push_hook.c relies on both frame halves being byte-identical.
	img := image.NewNRGBA(image.Rect(0, 0, VisW, VisH))
	for y := 0; y < VisH; y++ {
		for x := 0; x < VisW; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	px := ToBGR565(img)
	half1 := px[:FrameBytes]
	half2 := px[FrameBytes:]
	if !bytes.Equal(half1, half2) {
		t.Fatal("the two frame halves are not byte-identical")
	}
}

func TestToBGR565StridePadding(t *testing.T) {
	// Columns 960..1023 (the 64px stride padding) must stay zero.
	img := image.NewNRGBA(image.Rect(0, 0, VisW, VisH))
	for y := 0; y < VisH; y++ {
		for x := 0; x < VisW; x++ {
			img.Set(x, y, color.White)
		}
	}
	px := ToBGR565(img)
	for y := 0; y < VisH; y++ {
		for x := VisW; x < Stride; x++ {
			off := (y*Stride + x) * 2
			if px[off] != 0 || px[off+1] != 0 {
				t.Fatalf("padding pixel (%d,%d) not zero", x, y)
			}
		}
	}
}

func TestBGR565RoundTrip(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, VisW, VisH))
	// Pure white survives 565 quantization exactly (all bits set both ways).
	want := color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 255}
	for y := 0; y < VisH; y++ {
		for x := 0; x < VisW; x++ {
			img.SetNRGBA(x, y, want)
		}
	}
	px := ToBGR565(img)
	got := FromBGR565(px[:FrameBytes])
	c := got.NRGBAAt(0, 0)
	if c.R != want.R || c.G != want.G || c.B != want.B {
		t.Fatalf("round-trip = %+v, want %+v", c, want)
	}
}

// The fast NRGBA path must produce exactly what the general path does.
func TestEncodeFrameMatchesGeneralPath(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, VisW, VisH))
	for i := range img.Pix {
		img.Pix[i] = byte(i*131 + i/7)
	}
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	fast := ToBGR565(img)
	// Wrap the same pixels in a type the fast path does not take.
	slow := ToBGR565(struct{ image.Image }{img})
	if !bytes.Equal(fast, slow) {
		t.Fatal("NRGBA fast path differs from the general path")
	}
	frame := make([]byte, FrameBytes)
	EncodeFrame(img, frame)
	if !bytes.Equal(frame, fast[:FrameBytes]) || !bytes.Equal(frame, fast[FrameBytes:]) {
		t.Fatal("EncodeFrame differs from ToBGR565's halves")
	}
}

func BenchmarkEncodeFrame(b *testing.B) {
	img := image.NewNRGBA(image.Rect(0, 0, VisW, VisH))
	frame := make([]byte, FrameBytes)
	for i := 0; i < b.N; i++ {
		EncodeFrame(img, frame)
	}
}

func BenchmarkToBGR565General(b *testing.B) {
	img := struct{ image.Image }{image.NewNRGBA(image.Rect(0, 0, VisW, VisH))}
	for i := 0; i < b.N; i++ {
		ToBGR565(img)
	}
}

func TestToWireBlack(t *testing.T) {
	// measured on device: black frame goes out as e7 f3 e7 ff repeated, both halves.
	px := make([]byte, TotalBytes)
	wire := make([]byte, TotalBytes)
	ToWire(wire, px)
	for _, off := range []int{0, FrameBytes} {
		if !bytes.Equal(wire[off:off+8], []byte{0xE7, 0xF3, 0xE7, 0xFF, 0xE7, 0xF3, 0xE7, 0xFF}) {
			t.Fatalf("wire[%d:] = % x", off, wire[off:off+8])
		}
	}
}

func TestDecodeRGBAMatchesFromBGR565(t *testing.T) {
	px := make([]byte, FrameBytes)
	for i := range px {
		px[i] = byte(i * 7)
	}
	want := FromBGR565(px)
	got := image.NewRGBA(image.Rect(0, 0, VisW, VisH))
	DecodeRGBA(px, got)
	if !bytes.Equal(got.Pix, want.Pix) {
		t.Fatal("DecodeRGBA differs from FromBGR565")
	}
}
