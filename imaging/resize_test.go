package imaging

import (
	"image"
	"math"
	"testing"
)

// TestThumbnailMatchesShortEdge is the point of this file: Thumbnail must
// scale so the SHORT edge lands on the target, not the long one. A landscape
// and a portrait image with the same dimensions swapped both need their
// short edge — not "width" or "height" specifically — pinned to the target.
func TestThumbnailMatchesShortEdge(t *testing.T) {
	cases := []struct {
		name      string
		w, h      int
		shortEdge int
		wantW     int
		wantH     int
	}{
		// Landscape: short edge is height (3000).
		{"landscape", 4000, 3000, 300, 400, 300},
		// Portrait: short edge is width (3000) — the dimension that shrinks
		// to the target flips with orientation.
		{"portrait", 3000, 4000, 300, 300, 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := image.NewRGBA(image.Rect(0, 0, c.w, c.h))
			got := Thumbnail(src, c.shortEdge)
			gw, gh := got.Bounds().Dx(), got.Bounds().Dy()
			if gw != c.wantW || gh != c.wantH {
				t.Fatalf("Thumbnail(%dx%d, %d) = %dx%d, want %dx%d", c.w, c.h, c.shortEdge, gw, gh, c.wantW, c.wantH)
			}
			short := gw
			if gh < short {
				short = gh
			}
			if short != c.shortEdge {
				t.Fatalf("short edge is %d, want exactly %d — the long edge was matched instead", short, c.shortEdge)
			}
		})
	}
}

// TestThumbnailLeavesSmallImagesUnchanged confirms an image already at or
// below the target short edge is returned as-is, not upscaled.
func TestThumbnailLeavesSmallImagesUnchanged(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 100, 80))
	got := Thumbnail(src, 100)
	if got != image.Image(src) {
		t.Fatalf("expected the identical image back when short edge (80) <= target (100)")
	}
}

// TestThumbnailNonPositiveShortEdgeIsNoop guards a caller passing a
// meaningless target: the input comes back untouched rather than panicking
// or producing a zero-sized image.
func TestThumbnailNonPositiveShortEdgeIsNoop(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 400, 300))
	for _, shortEdge := range []int{0, -1, -1000} {
		got := Thumbnail(src, shortEdge)
		if got != image.Image(src) {
			t.Fatalf("shortEdge=%d: expected the identical image back, got a %v", shortEdge, got.Bounds())
		}
	}
}

// TestThumbnailZeroDimensionDoesNotPanic covers a degenerate input rather
// than assuming callers never produce one.
func TestThumbnailZeroDimensionDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Thumbnail panicked on a zero-dimension image: %v", r)
		}
	}()
	src := image.NewRGBA(image.Rect(0, 0, 0, 200))
	got := Thumbnail(src, 100)
	if got == nil {
		t.Fatal("expected a non-nil image back")
	}
}

// TestThumbnailPreservesAspectRatio checks the output's aspect ratio matches
// the source's to within a pixel of rounding.
func TestThumbnailPreservesAspectRatio(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4000, 2250)) // 16:9
	got := Thumbnail(src, 300)
	wantRatio := 4000.0 / 2250.0
	gotRatio := float64(got.Bounds().Dx()) / float64(got.Bounds().Dy())
	if math.Abs(wantRatio-gotRatio) > 0.01 {
		t.Fatalf("aspect ratio %.4f, want %.4f (within rounding)", gotRatio, wantRatio)
	}
}

// TestThumbnailHandlesNonZeroOrigin covers a source whose bounds don't start
// at (0,0), such as a sub-image produced by SubImage.
func TestThumbnailHandlesNonZeroOrigin(t *testing.T) {
	base := image.NewRGBA(image.Rect(0, 0, 4000, 4000))
	// A 2000x1000 window offset away from the origin: short edge 1000.
	offset := base.SubImage(image.Rect(1000, 1500, 3000, 2500))
	got := Thumbnail(offset, 100)
	b := offset.Bounds()
	w, h := b.Dx(), b.Dy()
	short := w
	if h < short {
		short = h
	}
	scale := 100.0 / float64(short)
	wantW := int(float64(w)*scale + 0.5)
	wantH := int(float64(h)*scale + 0.5)
	if got.Bounds().Dx() != wantW || got.Bounds().Dy() != wantH {
		t.Fatalf("non-zero-origin source: got %v, want %dx%d", got.Bounds(), wantW, wantH)
	}
}
