package imaging

import (
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/adrium/goheif"
)

// IsSupported reports whether path has an extension this package can decode.
func IsSupported(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".heic", ".heif", ".jpg", ".jpeg", ".png":
		return true
	}
	return false
}

// Decode reads the image at path and returns it with its EXIF block, which is
// nil when the format carries none (PNG) or the file simply has none. A
// missing EXIF block is not an error: the pixels are what matter.
//
// The EXIF Orientation is APPLIED to the pixels here, and the returned block
// has its Orientation reset to 1. Neither image/jpeg nor goheif applies the tag
// themselves, so without this every caller would hold pixels that are upside
// down or sideways whenever a phone was not held level — and the two halves have
// to happen together, because rotating the pixels while passing on a tag that
// still says "rotate me" makes a compliant viewer rotate a second time.
//
// Applying it here rather than in each caller is what makes a PNG contact sheet
// come out the right way up: PNG has no orientation field at all, so a sheet
// built from unrotated pixels is wrong in a way no viewer can correct.
func Decode(path string) (image.Image, []byte, error) {
	img, exif, err := decodeRaw(path)
	if err != nil {
		return nil, nil, err
	}
	if o := exifOrientation(exif); o != 1 {
		img = applyOrientation(img, o)
		exif = normaliseExifOrientation(exif)
	}
	return img, exif, nil
}

// decodeRaw returns the stored pixels and metadata exactly as the file holds
// them, before any orientation is applied.
func decodeRaw(path string) (image.Image, []byte, error) {
	ext := strings.ToLower(filepath.Ext(path))
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	switch ext {
	case ".heic", ".heif":
		exif, err := goheif.ExtractExif(f)
		if err != nil {
			exif = nil
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, nil, err
		}
		img, err := goheif.Decode(f)
		if err != nil {
			return nil, nil, fmt.Errorf("decode: %w", err)
		}
		return img, exif, nil
	case ".jpg", ".jpeg":
		exif, err := extractJPEGExif(f)
		if err != nil {
			exif = nil
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return nil, nil, err
		}
		img, err := jpeg.Decode(f)
		if err != nil {
			return nil, nil, fmt.Errorf("decode: %w", err)
		}
		return img, exif, nil
	case ".png":
		img, err := png.Decode(f)
		if err != nil {
			return nil, nil, fmt.Errorf("decode: %w", err)
		}
		return img, nil, nil
	}
	return nil, nil, fmt.Errorf("unsupported image type %q", ext)
}

// extractJPEGExif returns the payload of the first APP1 EXIF segment, walking
// the marker chain until the start of scan. Returns an error when there is
// none, which callers treat as "no metadata" rather than as a failure.
func extractJPEGExif(r io.ReadSeeker) ([]byte, error) {
	var soi [2]byte
	if _, err := io.ReadFull(r, soi[:]); err != nil {
		return nil, err
	}
	if soi[0] != 0xFF || soi[1] != 0xD8 {
		return nil, fmt.Errorf("not a JPEG")
	}
	for {
		var hdr [4]byte
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return nil, err
		}
		if hdr[0] != 0xFF {
			return nil, fmt.Errorf("bad marker")
		}
		marker := hdr[1]
		// Start of scan or end of image: no EXIF ahead.
		if marker == 0xDA || marker == 0xD9 {
			return nil, fmt.Errorf("no EXIF segment")
		}
		length := int(hdr[2])<<8 | int(hdr[3])
		if length < 2 {
			return nil, fmt.Errorf("bad segment length")
		}
		payload := make([]byte, length-2)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}
		if marker == 0xE1 && len(payload) >= 6 && string(payload[:4]) == "Exif" {
			return payload, nil
		}
	}
}
