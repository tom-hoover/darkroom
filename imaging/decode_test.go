package imaging

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func writeTestImage(t *testing.T, dir, name string) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 120, B: 240, A: 255})
		}
	}
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if filepath.Ext(name) == ".png" {
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	} else if err := jpeg.Encode(f, img, nil); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDecodeJPEGAndPNG(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.jpg", "b.png"} {
		img, _, err := Decode(writeTestImage(t, dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := img.Bounds().Dx(); got != 8 {
			t.Errorf("%s: width = %d, want 8", name, got)
		}
	}
}

func TestDecodeRejectsUnknownExtension(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "notes.txt")
	os.WriteFile(p, []byte("hello"), 0o644)
	if _, _, err := Decode(p); err == nil {
		t.Fatal("expected an error for an unsupported extension")
	}
}

func TestIsSupported(t *testing.T) {
	cases := map[string]bool{
		"a.jpg": true, "a.JPG": true, "a.jpeg": true, "a.png": true,
		"a.heic": true, "a.HEIF": true, "a.txt": false, "a": false,
	}
	for name, want := range cases {
		if got := IsSupported(name); got != want {
			t.Errorf("IsSupported(%q) = %v, want %v", name, got, want)
		}
	}
}

// EXIF preservation is spec section 5 step 6 and success criterion 4: capture
// date, GPS, and above all the orientation tag that decides which way up a
// phone photo appears. skyburn's only path to it for a JPEG source is
// extractJPEGExif feeding WriteJPEG, and the tests below are the only ones
// that exercise that wiring end to end. testdata/sample.heic carries no EXIF,
// so nothing that reads it can cover this.

// minimalExif builds a real EXIF block: the "Exif\0\0" identifier that a JPEG
// APP1 segment carries, followed by a valid little-endian TIFF header holding
// one Orientation tag. It is constructed here rather than lifted from a photo
// so the test depends on no external tool and on none of the user's files.
func minimalExif() []byte {
	tiff := []byte{
		'I', 'I', 0x2A, 0x00, // little-endian byte order, TIFF magic 42
		0x08, 0x00, 0x00, 0x00, // IFD0 starts at offset 8
		0x01, 0x00, // one directory entry
		0x12, 0x01, // tag 0x0112, Orientation
		0x03, 0x00, // type SHORT
		0x01, 0x00, 0x00, 0x00, // count 1
		0x01, 0x00, 0x00, 0x00, // value 1 (normal)
		0x00, 0x00, 0x00, 0x00, // no next IFD
	}
	return append([]byte("Exif\x00\x00"), tiff...)
}

// appSegment wraps a payload in a JPEG APP segment with its length field.
func appSegment(marker byte, payload []byte) []byte {
	seg := make([]byte, 4, 4+len(payload))
	seg[0], seg[1] = 0xFF, marker
	binary.BigEndian.PutUint16(seg[2:4], uint16(len(payload)+2))
	return append(seg, payload...)
}

// jpegWithSegments encodes a small image and splices the given APP segments in
// immediately after the SOI marker, which is where a camera puts them.
func jpegWithSegments(t *testing.T, segs ...[]byte) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: 40, G: 90, B: 190, A: 255})
		}
	}
	var body bytes.Buffer
	if err := jpeg.Encode(&body, img, nil); err != nil {
		t.Fatal(err)
	}
	b := body.Bytes()
	if len(b) < 2 || b[0] != 0xFF || b[1] != 0xD8 {
		t.Fatal("encoder did not emit an SOI marker")
	}
	out := []byte{0xFF, 0xD8}
	for _, seg := range segs {
		out = append(out, seg...)
	}
	return append(out, b[2:]...)
}

// TestExifSurvivesDecodeThenWriteJPEG is the end-to-end guarantee: metadata
// present on the source JPEG is present, byte for byte, on the rendered one.
// It fails if extractJPEGExif stops finding the segment and it fails if
// WriteJPEG stops writing the one it is handed.
func TestExifSurvivesDecodeThenWriteJPEG(t *testing.T) {
	want := minimalExif()
	dir := t.TempDir()
	src := filepath.Join(dir, "with-exif.jpg")
	if err := os.WriteFile(src, jpegWithSegments(t, appSegment(0xE1, want)), 0o644); err != nil {
		t.Fatal(err)
	}

	img, exif, err := Decode(src)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !bytes.Equal(exif, want) {
		t.Fatalf("Decode returned EXIF % x, want % x — the source's metadata was dropped on the way in", exif, want)
	}

	var out bytes.Buffer
	if err := WriteJPEG(&out, img, 90, exif); err != nil {
		t.Fatalf("WriteJPEG: %v", err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(out.Bytes())); err != nil {
		t.Fatalf("rendered JPEG does not decode: %v", err)
	}
	if got := findAPP1(t, out.Bytes()); !bytes.Equal(got, want) {
		t.Errorf("rendered JPEG carries EXIF % x, want % x — metadata was dropped on the way out", got, want)
	}
}

func TestExtractJPEGExifFindsThePayload(t *testing.T) {
	want := minimalExif()
	got, err := extractJPEGExif(bytes.NewReader(jpegWithSegments(t, appSegment(0xE1, want))))
	if err != nil {
		t.Fatalf("extractJPEGExif: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("payload = % x, want % x", got, want)
	}
}

// The EXIF segment is rarely the first one. A JFIF APP0 and an XMP APP1 ahead
// of it must both be stepped over, which is the whole point of walking the
// marker chain rather than reading a fixed offset.
func TestExtractJPEGExifSkipsSegmentsBeforeIt(t *testing.T) {
	want := minimalExif()
	data := jpegWithSegments(t,
		appSegment(0xE0, []byte("JFIF\x00\x01\x02\x00\x00\x01\x00\x01\x00\x00")),
		appSegment(0xE1, []byte("http://ns.adobe.com/xap/1.0/\x00<x:xmpmeta/>")),
		appSegment(0xE1, want),
	)
	got, err := extractJPEGExif(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("extractJPEGExif: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("payload = % x, want % x — the marker walk stopped at the wrong segment", got, want)
	}
}

func TestExtractJPEGExifReportsNoSegment(t *testing.T) {
	if _, err := extractJPEGExif(bytes.NewReader(jpegWithSegments(t))); err == nil {
		t.Fatal("expected an error for a JPEG with no EXIF segment")
	}
}

func TestExtractJPEGExifRejectsTruncatedFile(t *testing.T) {
	full := jpegWithSegments(t, appSegment(0xE1, minimalExif()))
	for _, n := range []int{0, 1, 2, 6, 12} {
		if _, err := extractJPEGExif(bytes.NewReader(full[:n])); err == nil {
			t.Errorf("truncated to %d bytes: expected an error", n)
		}
	}
}

// A JPEG with no EXIF must still decode: missing metadata is not a failure.
func TestDecodeJPEGWithoutExifStillSucceeds(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "plain.jpg")
	if err := os.WriteFile(src, jpegWithSegments(t), 0o644); err != nil {
		t.Fatal(err)
	}
	img, exif, err := Decode(src)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if img == nil {
		t.Fatal("Decode returned no image")
	}
	if exif != nil {
		t.Errorf("exif = % x, want nil", exif)
	}
}
