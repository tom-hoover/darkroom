package imaging

import (
	"image"

	xdraw "golang.org/x/image/draw"
)

// Thumbnail scales img so its short edge is shortEdge pixels, preserving
// aspect ratio. Images already at or below that size are returned unchanged.
//
// CatmullRom is used because contact-sheet tiles are judged by eye: a cheaper
// filter would add aliasing that reads as a property of the style rather than
// of the resampling.
func Thumbnail(img image.Image, shortEdge int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 || shortEdge <= 0 {
		return img
	}
	short := w
	if h < short {
		short = h
	}
	if short <= shortEdge {
		return img
	}
	scale := float64(shortEdge) / float64(short)
	nw := int(float64(w)*scale + 0.5)
	nh := int(float64(h)*scale + 0.5)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
	return dst
}
