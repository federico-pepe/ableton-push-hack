package gfx

import "image"

// DrawIcon composites a cached icon onto img at pixel (x, y), alpha-blending
// src over dst (icon.A == 0 pixels are skipped, others render fully opaque).
func DrawIcon(img *image.NRGBA, icon *image.NRGBA, x, y int) {
	ib := icon.Bounds()
	for iy := 0; iy < ib.Dy(); iy++ {
		for ix := 0; ix < ib.Dx(); ix++ {
			src := icon.NRGBAAt(ix, iy)
			if src.A == 0 {
				continue
			}
			px := x + ix
			py := y + iy
			if px < 0 || py < 0 || px >= img.Bounds().Dx() || py >= img.Bounds().Dy() {
				continue
			}
			dst := img.NRGBAAt(px, py)
			a := uint32(src.A)
			na := 255 - a
			dst.R = uint8((uint32(src.R)*a + uint32(dst.R)*na) / 255)
			dst.G = uint8((uint32(src.G)*a + uint32(dst.G)*na) / 255)
			dst.B = uint8((uint32(src.B)*a + uint32(dst.B)*na) / 255)
			dst.A = 255
			img.SetNRGBA(px, py, dst)
		}
	}
}
