package imaging

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"testing"
)

func TestExifAPP1BuildsValidSegment(t *testing.T) {
	exif := append([]byte("Exif\x00\x00"), bytes.Repeat([]byte{0xAB}, 20)...)

	seg, err := exifAPP1(exif)
	if err != nil {
		t.Fatalf("exifAPP1: %v", err)
	}
	if seg[0] != 0xFF || seg[1] != 0xE1 {
		t.Errorf("marker = % x, want ff e1", seg[:2])
	}
	if got, want := binary.BigEndian.Uint16(seg[2:4]), uint16(len(exif)+2); got != want {
		t.Errorf("length field = %d, want %d", got, want)
	}
	if !bytes.Equal(seg[4:], exif) {
		t.Error("payload does not match the EXIF block")
	}
}

// A JPEG APP1 segment length is a 16-bit field, so an EXIF block that
// does not fit must be refused rather than truncated into corruption.
func TestExifAPP1RefusesOversizeBlock(t *testing.T) {
	if _, err := exifAPP1(bytes.Repeat([]byte{0}, 0xFFFE)); err == nil {
		t.Fatal("exifAPP1 accepted an oversize block, want error")
	}
}

func TestExifWriterEmbedsExifInJPEG(t *testing.T) {
	exif := append([]byte("Exif\x00\x00"), bytes.Repeat([]byte{0xAB}, 20)...)

	var buf bytes.Buffer
	w, err := newExifWriter(&buf, exif)
	if err != nil {
		t.Fatalf("newExifWriter: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	if err := jpeg.Encode(w, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode: %v", err)
	}

	// Still a valid JPEG after the segment was spliced in.
	if _, err := jpeg.Decode(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("spliced JPEG does not decode: %v", err)
	}

	got := findAPP1(t, buf.Bytes())
	if !bytes.Equal(got, exif) {
		t.Errorf("APP1 payload = % x, want % x", got, exif)
	}
}

func TestExifWriterOmitsSegmentWhenNoExif(t *testing.T) {
	var buf bytes.Buffer
	w, err := newExifWriter(&buf, nil)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	if err := jpeg.Encode(w, img, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := jpeg.Decode(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("JPEG does not decode: %v", err)
	}
	if seg := findAPP1(t, buf.Bytes()); seg != nil {
		t.Errorf("found an APP1 segment (% x), want none", seg)
	}
}

// findAPP1 walks JPEG markers and returns the first APP1 payload, or nil.
// Walking the markers also proves the segment lengths are self-consistent.
func findAPP1(t *testing.T, data []byte) []byte {
	t.Helper()
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		t.Fatal("missing SOI marker")
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			t.Fatalf("expected a marker at offset %d, found %#x", i, data[i])
		}
		marker := data[i+1]
		if marker == 0xD8 {
			t.Fatalf("second SOI marker at offset %d", i)
		}
		if marker == 0xDA || marker == 0xD9 { // start of scan / end of image
			return nil
		}
		length := int(binary.BigEndian.Uint16(data[i+2 : i+4]))
		if marker == 0xE1 {
			return data[i+4 : i+2+length]
		}
		i += 2 + length
	}
	return nil
}
