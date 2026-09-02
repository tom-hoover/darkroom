package imaging

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

// exifWithOrientation builds an EXIF block carrying one Orientation tag with
// the given value, in the requested byte order. Constructed rather than lifted
// from a photo so these tests depend on no external tool and no user files.
func exifWithOrientation(o int, bigEndian bool) []byte {
	bo := binary.ByteOrder(binary.LittleEndian)
	order := []byte{'I', 'I'}
	if bigEndian {
		bo = binary.BigEndian
		order = []byte{'M', 'M'}
	}
	t := make([]byte, 26)
	copy(t, order)
	bo.PutUint16(t[2:4], 42)
	bo.PutUint32(t[4:8], 8) // IFD0 at offset 8
	bo.PutUint16(t[8:10], 1)
	bo.PutUint16(t[10:12], 0x0112) // Orientation
	bo.PutUint16(t[12:14], 3)      // SHORT
	bo.PutUint32(t[14:18], 1)      // count
	bo.PutUint16(t[18:20], uint16(o))
	// A SHORT occupies the first two bytes of the four-byte value field; the
	// remaining two are undefined by the spec and hold junk in real files.
	// Filling them with recognisable bytes is what lets
	// TestNormaliseExifOrientationKeepsEverythingElse notice a write that
	// strays past the value — with zeros here, a mutation that clobbered them
	// would be invisible, and a first version of that test was toothless for
	// exactly that reason.
	t[20], t[21] = 0xAB, 0xCD
	return append([]byte("Exif\x00\x00"), t...)
}

// grid builds an image whose every pixel is distinguishable, so a transform can
// be checked by exact position rather than by a summary statistic.
func grid(w, h int, vals []uint8) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetGray(x, y, color.Gray{Y: vals[y*w+x]})
		}
	}
	return img
}

func gridOf(t *testing.T, img image.Image) (int, int, []uint8) {
	t.Helper()
	b := img.Bounds()
	out := make([]uint8, 0, b.Dx()*b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, _, _, _ := img.At(x, y).RGBA()
			out = append(out, uint8(r>>8))
		}
	}
	return b.Dx(), b.Dy(), out
}

func TestExifOrientationReadsTheTag(t *testing.T) {
	for o := 1; o <= 8; o++ {
		for _, big := range []bool{false, true} {
			if got := exifOrientation(exifWithOrientation(o, big)); got != o {
				t.Errorf("exifOrientation(orientation %d, bigEndian=%v) = %d", o, big, got)
			}
		}
	}
}

// TestExifOrientationDefaultsToOneOnAnythingUnreadable matters because the
// return value drives a pixel transform. Guessing on malformed metadata would
// rotate a photograph that was already the right way up, which is worse than
// leaving unreadable metadata alone.
func TestExifOrientationDefaultsToOneOnAnythingUnreadable(t *testing.T) {
	valid := exifWithOrientation(6, false)
	for name, exif := range map[string][]byte{
		"nil":                nil,
		"empty":              {},
		"identifier only":    []byte("Exif\x00\x00"),
		"bad byte order":     append([]byte("Exif\x00\x00"), 'X', 'Y', 0x2A, 0x00, 8, 0, 0, 0, 0, 0),
		"truncated header":   append([]byte("Exif\x00\x00"), 'I', 'I', 0x2A),
		"no orientation tag": append([]byte("Exif\x00\x00"), 'I', 'I', 0x2A, 0x00, 8, 0, 0, 0, 0, 0, 0, 0),
		"ifd past end":       append([]byte("Exif\x00\x00"), 'I', 'I', 0x2A, 0x00, 0xFF, 0xFF, 0, 0),
		"truncated mid-tag":  valid[:len(valid)-6],
		"out of range value": exifWithOrientation(99, false),
		"zero value":         exifWithOrientation(0, false),
	} {
		if got := exifOrientation(exif); got != 1 {
			t.Errorf("exifOrientation(%s) = %d, want 1", name, got)
		}
	}
}

// TestApplyOrientationTransforms checks all eight cases by exact pixel position
// against a hand-worked expectation, including the four that swap width and
// height. A summary statistic would not distinguish a transpose from a rotation.
func TestApplyOrientationTransforms(t *testing.T) {
	// 3x2:  10 20 30
	//       40 50 60
	src := grid(3, 2, []uint8{10, 20, 30, 40, 50, 60})

	for _, c := range []struct {
		o    int
		w, h int
		want []uint8
		what string
	}{
		{1, 3, 2, []uint8{10, 20, 30, 40, 50, 60}, "unchanged"},
		{2, 3, 2, []uint8{30, 20, 10, 60, 50, 40}, "mirrored horizontally"},
		{3, 3, 2, []uint8{60, 50, 40, 30, 20, 10}, "rotated 180"},
		{4, 3, 2, []uint8{40, 50, 60, 10, 20, 30}, "mirrored vertically"},
		{5, 2, 3, []uint8{10, 40, 20, 50, 30, 60}, "transposed"},
		{6, 2, 3, []uint8{40, 10, 50, 20, 60, 30}, "rotated 90 CW"},
		{7, 2, 3, []uint8{60, 30, 50, 20, 40, 10}, "transverse"},
		{8, 2, 3, []uint8{30, 60, 20, 50, 10, 40}, "rotated 270 CW"},
	} {
		w, h, got := gridOf(t, applyOrientation(src, c.o))
		if w != c.w || h != c.h {
			t.Errorf("orientation %d (%s): got %dx%d, want %dx%d", c.o, c.what, w, h, c.w, c.h)
			continue
		}
		if !bytes.Equal(got, c.want) {
			t.Errorf("orientation %d (%s):\n got %v\nwant %v", c.o, c.what, got, c.want)
		}
	}
}

// TestApplyOrientationLeavesOneAlone pins that the common case costs nothing
// and returns the original rather than a copy.
func TestApplyOrientationLeavesOneAlone(t *testing.T) {
	src := grid(3, 2, []uint8{10, 20, 30, 40, 50, 60})
	if got := applyOrientation(src, 1); got != image.Image(src) {
		t.Error("orientation 1 did not return the source image unchanged")
	}
	// An unknown value is also a no-op: rotating on a value nobody defined
	// would corrupt a photograph on the strength of a guess.
	if got := applyOrientation(src, 42); got != image.Image(src) {
		t.Error("an out-of-range orientation was not treated as a no-op")
	}
}

// TestNormaliseExifOrientationSetsOne is the other half of applying the
// transform. Rotating the pixels while leaving the tag saying "rotate me" makes
// every compliant viewer rotate a second time, so the two must move together.
func TestNormaliseExifOrientationSetsOne(t *testing.T) {
	for o := 1; o <= 8; o++ {
		for _, big := range []bool{false, true} {
			got := normaliseExifOrientation(exifWithOrientation(o, big))
			if v := exifOrientation(got); v != 1 {
				t.Errorf("normalising orientation %d (bigEndian=%v) left it at %d", o, big, v)
			}
		}
	}
}

// TestNormaliseExifOrientationKeepsEverythingElse pins that only the two bytes
// of the Orientation value change. The rest of the block is capture date, GPS
// and camera model, and this package exists to preserve those.
func TestNormaliseExifOrientationKeepsEverythingElse(t *testing.T) {
	in := exifWithOrientation(6, false)
	got := normaliseExifOrientation(in)
	if len(got) != len(in) {
		t.Fatalf("length changed: %d -> %d", len(in), len(got))
	}
	diffs := 0
	for i := range in {
		if in[i] != got[i] {
			diffs++
		}
	}
	if diffs != 1 {
		t.Errorf("%d bytes changed, want exactly 1 (the low byte of the SHORT value)", diffs)
	}
}

// TestNormaliseExifOrientationDoesNotMutateItsInput matters because the caller
// may still hold the original slice.
func TestNormaliseExifOrientationDoesNotMutateItsInput(t *testing.T) {
	in := exifWithOrientation(6, false)
	keep := append([]byte(nil), in...)
	normaliseExifOrientation(in)
	if !bytes.Equal(in, keep) {
		t.Error("normaliseExifOrientation modified the slice it was given")
	}
}

func TestNormaliseExifOrientationHandlesUnreadableInput(t *testing.T) {
	for name, exif := range map[string][]byte{
		"nil":             nil,
		"empty":           {},
		"identifier only": []byte("Exif\x00\x00"),
		"no tag":          append([]byte("Exif\x00\x00"), 'I', 'I', 0x2A, 0x00, 8, 0, 0, 0, 0, 0, 0, 0),
	} {
		// Must not panic, and must leave an unreadable block alone.
		got := normaliseExifOrientation(exif)
		if !bytes.Equal(got, exif) {
			t.Errorf("%s: block was altered", name)
		}
	}
}

// TestDecodeAppliesOrientationAndNormalisesTheTag is the end-to-end guarantee:
// callers get pixels the right way up, and metadata that will not rotate them
// again.
func TestDecodeAppliesOrientationAndNormalisesTheTag(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "rotated.jpg")

	// A frame with a bright top-left corner and a dark everything else, so a
	// 180-degree rotation is unmistakable at a single pixel.
	img := image.NewGray(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			v := uint8(20)
			if x < 16 && y < 12 {
				v = 240
			}
			img.SetGray(x, y, color.Gray{Y: v})
		}
	}
	writeTestJPEGWithExif(t, src, img, exifWithOrientation(3, false))

	got, exif, err := Decode(src)
	if err != nil {
		t.Fatal(err)
	}

	// Orientation 3 is a 180-degree rotation, so the bright corner must now be
	// bottom-right.
	tl, _, _, _ := got.At(4, 4).RGBA()
	br, _, _, _ := got.At(59, 43).RGBA()
	if uint8(tl>>8) > 128 {
		t.Errorf("top-left is still bright (%d) — the orientation was not applied", uint8(tl>>8))
	}
	if uint8(br>>8) < 128 {
		t.Errorf("bottom-right is not bright (%d) — the orientation was not applied", uint8(br>>8))
	}

	if o := exifOrientation(exif); o != 1 {
		t.Errorf("returned EXIF still says orientation %d; a viewer would rotate the pixels a second time", o)
	}
}

// TestDecodeThenWriteJPEGDoesNotRotateTwice is the guard against the trap this
// change creates: apply the transform on the way in, and any orientation tag
// left in the metadata makes a compliant viewer apply it again on the way out.
// Decoding the round-tripped file must give back what the first decode gave.
func TestDecodeThenWriteJPEGDoesNotRotateTwice(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.jpg")

	img := image.NewGray(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			v := uint8(20)
			if x < 16 && y < 12 {
				v = 240
			}
			img.SetGray(x, y, color.Gray{Y: v})
		}
	}
	writeTestJPEGWithExif(t, src, img, exifWithOrientation(6, false))

	first, exif, err := Decode(src)
	if err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "out.jpg")
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteJPEG(f, first, 95, exif); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	second, _, err := Decode(out)
	if err != nil {
		t.Fatal(err)
	}

	if first.Bounds() != second.Bounds() {
		t.Fatalf("round trip changed the bounds: %v -> %v — the orientation was applied twice",
			first.Bounds(), second.Bounds())
	}
	// Compare a coarse grid; JPEG is lossy so exact equality is not available.
	b := first.Bounds()
	var sum, n float64
	for y := b.Min.Y; y < b.Max.Y; y += 4 {
		for x := b.Min.X; x < b.Max.X; x += 4 {
			a, _, _, _ := first.At(x, y).RGBA()
			c, _, _, _ := second.At(x, y).RGBA()
			d := float64(a>>8) - float64(c>>8)
			if d < 0 {
				d = -d
			}
			sum += d
			n++
		}
	}
	if mean := sum / n; mean > 4 {
		t.Errorf("round trip differs by a mean of %.1f levels — the orientation was applied a second time", mean)
	}
}

// writeTestJPEGWithExif encodes img and splices an EXIF APP1 segment in
// immediately after the SOI marker, which is where a camera puts it, then
// writes the result to path.
//
// It takes the image rather than building one, unlike jpegWithSegments, because
// an orientation test needs a frame whose corners are distinguishable.
func writeTestJPEGWithExif(t *testing.T, path string, img image.Image, exif []byte) {
	t.Helper()
	var body bytes.Buffer
	if err := jpeg.Encode(&body, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	b := body.Bytes()
	if len(b) < 2 || b[0] != 0xFF || b[1] != 0xD8 {
		t.Fatal("encoder did not emit an SOI marker")
	}
	out := append([]byte{0xFF, 0xD8}, appSegment(0xE1, exif)...)
	out = append(out, b[2:]...)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}
