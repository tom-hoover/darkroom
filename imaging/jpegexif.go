// Package imaging decodes camera images and writes JPEGs that keep their
// EXIF metadata — capture date, GPS, and the orientation tag that decides
// which way up a phone photo appears.
package imaging

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/jpeg"
	"io"
)

// MaxExifPayload is the largest EXIF block a JPEG APP1 segment can carry.
// The segment's 16-bit length field counts itself as well as the payload.
const MaxExifPayload = 0xFFFF - 2

// WriteJPEG encodes img to w at the given quality, prefixing the EXIF block
// when one is supplied. An oversized block is dropped rather than refused,
// since a photo without metadata still beats no photo.
func WriteJPEG(w io.Writer, img image.Image, quality int, exif []byte) error {
	if quality < 1 || quality > 100 {
		return fmt.Errorf("quality %d out of range (1-100)", quality)
	}
	if len(exif) > MaxExifPayload {
		exif = nil
	}
	ew, err := newExifWriter(w, exif)
	if err != nil {
		return err
	}
	return jpeg.Encode(ew, img, &jpeg.Options{Quality: quality})
}

// exifAPP1 builds the JPEG APP1 segment that carries an EXIF block.
func exifAPP1(exif []byte) ([]byte, error) {
	if len(exif) > MaxExifPayload {
		return nil, fmt.Errorf("EXIF block of %d bytes exceeds the %d-byte APP1 limit", len(exif), MaxExifPayload)
	}
	seg := make([]byte, 4, 4+len(exif))
	seg[0], seg[1] = 0xFF, 0xE1
	binary.BigEndian.PutUint16(seg[2:4], uint16(len(exif)+2))
	return append(seg, exif...), nil
}

// newExifWriter writes a JPEG header to w — the SOI marker followed by an
// APP1 EXIF segment — and returns a writer that swallows the SOI marker
// image/jpeg emits, so the encoder's output splices onto that header.
func newExifWriter(w io.Writer, exif []byte) (io.Writer, error) {
	header := []byte{0xFF, 0xD8}
	if len(exif) > 0 {
		seg, err := exifAPP1(exif)
		if err != nil {
			return nil, err
		}
		header = append(header, seg...)
	}
	if _, err := w.Write(header); err != nil {
		return nil, err
	}
	return &skipWriter{w: w, skip: 2}, nil
}

// skipWriter discards the first skip bytes written to it and passes the rest
// through.
type skipWriter struct {
	w    io.Writer
	skip int
}

func (s *skipWriter) Write(p []byte) (int, error) {
	if s.skip <= 0 {
		return s.w.Write(p)
	}
	if len(p) <= s.skip {
		s.skip -= len(p)
		return len(p), nil
	}
	n, err := s.w.Write(p[s.skip:])
	n += s.skip
	s.skip = 0
	return n, err
}
