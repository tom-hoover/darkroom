package imaging

import (
	"encoding/binary"
	"image"
)

// orientationTag is EXIF tag 0x0112, whose value says which of eight
// transforms a viewer must apply to display the stored pixels the right way up.
const orientationTag = 0x0112

// exifOrientation returns the Orientation value in an EXIF block, or 1 when
// there is none, the block is unreadable, or the value is outside 1-8.
//
// Defaulting to 1 rather than reporting an error is deliberate: the return value
// drives a pixel transform, and guessing on malformed metadata would rotate a
// photograph that was already the right way up. Leaving unreadable metadata
// alone is the conservative failure.
func exifOrientation(exif []byte) int {
	// Every EXIF APP1 payload opens with "Exif\0\0", then a TIFF header.
	if len(exif) < 14 || string(exif[:4]) != "Exif" {
		return 1
	}
	t := exif[6:]

	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}

	off := int(bo.Uint32(t[4:8]))
	if off < 8 || off+2 > len(t) {
		return 1
	}
	n := int(bo.Uint16(t[off : off+2]))
	for i := 0; i < n; i++ {
		e := off + 2 + i*12
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:e+2]) != orientationTag {
			continue
		}
		// A SHORT value lives in the first two bytes of the 4-byte value field.
		o := int(bo.Uint16(t[e+8 : e+10]))
		if o < 1 || o > 8 {
			return 1
		}
		return o
	}
	return 1
}

// normaliseExifOrientation returns a copy of exif with its Orientation set to 1,
// leaving every other byte alone.
//
// This is the other half of applying the transform, and the two must always
// travel together: rotating the pixels while leaving a tag that still says
// "rotate me" makes every compliant viewer rotate a second time. An unreadable
// block is returned unchanged, matching exifOrientation's conservative default.
//
// Only the Orientation value is touched because the rest of the block is capture
// date, GPS and camera model — the metadata this package exists to preserve.
func normaliseExifOrientation(exif []byte) []byte {
	if len(exif) < 14 || string(exif[:4]) != "Exif" {
		return exif
	}

	var bo binary.ByteOrder
	switch string(exif[6:8]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return exif
	}

	off := int(bo.Uint32(exif[10:14]))
	base := 6 // where the TIFF header starts, and what IFD offsets are relative to
	if off < 8 || base+off+2 > len(exif) {
		return exif
	}
	n := int(bo.Uint16(exif[base+off : base+off+2]))
	for i := 0; i < n; i++ {
		e := base + off + 2 + i*12
		if e+12 > len(exif) {
			return exif
		}
		if bo.Uint16(exif[e:e+2]) != orientationTag {
			continue
		}
		out := append([]byte(nil), exif...)
		bo.PutUint16(out[e+8:e+10], 1)
		return out
	}
	return exif
}

// applyOrientation returns img transformed so that its stored pixels appear the
// way the photographer framed them.
//
// Orientation 1 and any value outside 1-8 return img itself, unmodified and
// uncopied: the overwhelming majority of images need no transform, and rotating
// on an undefined value would corrupt a photograph on the strength of a guess.
//
// Values 5 to 8 swap width and height. That matters beyond the pixels: a
// portrait photograph stored as landscape has a genuinely different short edge
// once corrected, and every spatial parameter in this codebase is a fraction of
// the short edge.
func applyOrientation(img image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return img
	}

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	// Orientations 5 to 8 transpose the frame, so the destination's width and
	// height are the source's swapped.
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}

	// RGBA regardless of the source's colour model: the pipelines downstream all
	// read through image.Image and immediately unpack to their own
	// representation, so preserving Gray or YCbCr here would buy nothing.
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))

	// src(x, y) for each destination pixel. Hand-worked from the EXIF
	// definitions and pinned pixel-by-pixel by TestApplyOrientationTransforms;
	// getting one of these subtly wrong is a mirror image nobody notices until
	// text appears in a frame.
	var at func(x, y int) (int, int)
	switch o {
	case 2: // mirrored horizontally
		at = func(x, y int) (int, int) { return w - 1 - x, y }
	case 3: // rotated 180
		at = func(x, y int) (int, int) { return w - 1 - x, h - 1 - y }
	case 4: // mirrored vertically
		at = func(x, y int) (int, int) { return x, h - 1 - y }
	case 5: // transposed (mirrored along the main diagonal)
		at = func(x, y int) (int, int) { return y, x }
	case 6: // rotated 90 clockwise
		at = func(x, y int) (int, int) { return y, h - 1 - x }
	case 7: // transverse (mirrored along the anti-diagonal)
		at = func(x, y int) (int, int) { return w - 1 - y, h - 1 - x }
	case 8: // rotated 270 clockwise
		at = func(x, y int) (int, int) { return w - 1 - y, x }
	}

	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			sx, sy := at(x, y)
			dst.Set(x, y, img.At(b.Min.X+sx, b.Min.Y+sy))
		}
	}
	return dst
}
