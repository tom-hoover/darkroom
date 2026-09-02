package sheet

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// solid builds a uniform colour image usable as a contact-sheet source.
func solid(size int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

// names mixes bw's real style names with additional realistic look names of
// varying length, so the label-fit test strains the layout with the kind of
// label vocabulary this package is designed to serve, not just today's single
// caller. "deep-red" and "high-key" are wider than a small thumbnail; that is
// the case this test exists for.
var names = []string{"deep-red", "thunder", "etch", "high-key", "flat",
	"classic", "wet", "deep", "azo"}

// tileColour is the colour indexed paints tile i. It must be injective in i,
// so a tile drawn from the wrong index is visible, and must not collide with
// the sheet background (26,26,28) or the label ink (225,225,230), which is why
// green is pinned at a value neither uses.
func tileColour(i int) color.RGBA {
	return color.RGBA{R: uint8(40 + 20*i), G: 90, B: uint8(160 - 12*i), A: 255}
}

// indexed renders a flat tile whose colour is derived from the tile's INDEX.
// Build's geometry does not depend on tile content, and using a real
// pipeline's renderer here would couple this test to that pipeline's
// implementation instead of exercising Build in isolation — but a renderer
// that ignores its index makes the index unobservable, which is how a
// render(thumb, 0) mutation stayed green in every package. Deriving the colour
// from i is what makes the index observable without importing a pipeline.
func indexed(thumb image.Image, i int) image.Image {
	b := thumb.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), image.NewUniform(tileColour(i)), image.Point{}, draw.Src)
	return out
}

func TestContactSheetGeometry(t *testing.T) {
	src := solid(600, color.RGBA{R: 40, G: 90, B: 190, A: 255})
	sheet := Build(src, names, 64, indexed)
	b := sheet.Bounds()
	// Nine names tile as 3 across, 3 down.
	if b.Dx() < 3*64 || b.Dy() < 3*64 {
		t.Fatalf("sheet %v is too small for a 3x3 grid of 64px tiles", b)
	}
}

func TestContactSheetHandlesOneStyle(t *testing.T) {
	src := solid(200, color.RGBA{R: 200, G: 200, B: 200, A: 255})
	sheet := Build(src, names[:1], 32, indexed)
	if sheet.Bounds().Empty() {
		t.Fatal("a single-name sheet must still produce an image")
	}
}

// TestContactSheetIsNotBlank checks that Build actually composited the
// renderer's tile into the sheet, not merely its background or its label ink.
//
// The naive form of this check — any pixel brighter than half grey — would
// pass even if Build never drew a tile at all: labelColour is a fixed bright
// value painted regardless of what render returns, so "any bright pixel
// anywhere" is satisfied by the label alone, independent of the tile. Since
// indexed's output does not vary with the source either, asserting the SPECIFIC
// colour indexed returns is the only form of this check that depends on the
// tile actually landing in the sheet.
func TestContactSheetIsNotBlank(t *testing.T) {
	src := solid(200, color.RGBA{R: 240, G: 242, B: 245, A: 255})
	sheet := Build(src, names[:1], 32, indexed)
	b := sheet.Bounds()
	want := tileColour(0)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := sheet.At(x, y).RGBA()
			if uint8(r>>8) == want.R && uint8(g>>8) == want.G && uint8(bl>>8) == want.B {
				return
			}
		}
	}
	t.Fatal("sheet contains no pixel matching the tile renderer's colour — Build is not compositing the rendered tile into the sheet")
}

// TestContactSheetLabelsFitTheirCells guards legibility, which is the sheet's
// only job. A label wider than its cell paints over the neighbouring tile, and
// one past the sheet's right edge is silently clipped rather than erroring.
// names are real words of varying length, so this must run against the full
// list at a small tile size, where the margin is tightest.
//
// It measures the PIXELS the sheet actually contains rather than recomputing
// where the implementation says it draws. An earlier version recomputed the
// drawn extent as x+2+w and compared it to x+cellW, which reduces to
// w > cellInner+6 — false by construction, since cellInner >= labelW >= w, and
// blind to where DrawString actually put the glyphs. Reading the rendered band
// is the only form of this test that can fail.
func TestContactSheetLabelsFitTheirCells(t *testing.T) {
	for _, px := range []int{32, 64, 128, 256} {
		src := solid(600, color.RGBA{R: 40, G: 90, B: 190, A: 255})
		sheet := Build(src, names, px, indexed)
		sheetW, sheetH := sheet.Bounds().Dx(), sheet.Bounds().Dy()

		cols := int(math.Ceil(math.Sqrt(float64(len(names)))))
		if cols > 4 {
			cols = 4
		}
		rows := (len(names) + cols - 1) / cols
		// Recompute the geometry the same way the implementation must.
		labelW := 0
		for _, name := range names {
			if w := font.MeasureString(basicfont.Face7x13, name).Ceil(); w > labelW {
				labelW = w
			}
		}
		cellInner := px
		if labelW > cellInner {
			cellInner = labelW
		}
		cellW := cellInner + Padding
		th := thumbnailFor(src, px).Bounds().Dy()
		cellH := th + labelHeight + Padding

		// litColumn reports whether any pixel of the label band in this column
		// differs from the sheet's background. The band sits strictly beneath
		// the tiles, so anything lit there was painted by DrawString.
		litColumn := func(x, top, bottom int) bool {
			for y := top; y < bottom && y < sheetH; y++ {
				r, g, b, _ := sheet.At(x, y).RGBA()
				if r>>8 != 26 || g>>8 != 26 || b>>8 != 28 {
					return true
				}
			}
			return false
		}

		drawn := make([]int, len(names))
		for row := 0; row < rows; row++ {
			y := Padding + row*cellH
			for x := 0; x < sheetW; x++ {
				if !litColumn(x, y+th, y+cellH) {
					continue
				}
				col := -1
				if x >= Padding {
					col = (x - Padding) / cellW
				}
				i := row*cols + col
				if col < 0 || col >= cols || i >= len(names) {
					t.Errorf("px=%d: label ink at column %d of band %d belongs to no cell (sheet width %d)",
						px, x, row, sheetW)
					continue
				}
				// A label is drawn from its cell's left edge plus the two-pixel
				// inset, and is exactly as wide as the face measures it. Ink
				// anywhere else in the cell means the label has been shifted or
				// stretched out of the cell it names.
				cx := Padding + col*cellW
				w := font.MeasureString(basicfont.Face7x13, names[i]).Ceil()
				if x < cx+2 || x >= cx+2+w {
					t.Errorf("px=%d: label ink at column %d escapes cell %d (%q), whose label occupies [%d,%d) — it is painting into a neighbouring cell or off the sheet",
						px, x, i, names[i], cx+2, cx+2+w)
					continue
				}
				drawn[i]++
			}
		}
		for i, name := range names {
			if drawn[i] == 0 {
				t.Errorf("px=%d: no ink found for label %q — the assertion above would pass vacuously", px, name)
			}
		}
	}
}

// TestContactSheetPairsEachLabelWithItsOwnTile pins the per-tile index and the
// label-to-tile pairing together, in one place, because neither is observable
// without the other.
//
// Both are otherwise invisible. Build's geometry does not depend on tile
// content, so render(thumb, 0) in place of render(thumb, i) produces a
// well-formed, correctly-labelled sheet of identical pictures; and swapping
// names[i] for names[j] produces a well-formed sheet of correct pictures under
// the wrong names. Every other test here either uses a content-independent
// assertion or looks only at tile 0, where i and 0 are the same number.
//
// Cell k must therefore carry BOTH tileColour(k) across its whole tile region
// AND, pixel for pixel, the band DrawString produces for names[k]. The band is
// compared against a reference drawn with the same face, colours and dot
// offset rather than against a re-derivation of where the glyphs land:
// basicfont.Face7x13 is a 1-bit mask, so a correct sheet matches the reference
// exactly and any substituted name differs somewhere in the band.
func TestContactSheetPairsEachLabelWithItsOwnTile(t *testing.T) {
	const px = 64
	src := solid(600, color.RGBA{R: 40, G: 90, B: 190, A: 255})
	sh := Build(src, names, px, indexed)

	cols := int(math.Ceil(math.Sqrt(float64(len(names)))))
	if cols > 4 {
		cols = 4
	}
	labelW := 0
	for _, name := range names {
		if w := font.MeasureString(basicfont.Face7x13, name).Ceil(); w > labelW {
			labelW = w
		}
	}
	tb := thumbnailFor(src, px).Bounds()
	tw, th := tb.Dx(), tb.Dy()
	cellInner := tw
	if labelW > cellInner {
		cellInner = labelW
	}
	cellW := cellInner + Padding
	cellH := th + labelHeight + Padding

	// Distinct colours are what makes the index observable; if tileColour ever
	// stopped being injective this test would pass vacuously.
	seen := map[color.RGBA]int{}
	for i := range names {
		if prev, dup := seen[tileColour(i)]; dup {
			t.Fatalf("tileColour(%d) == tileColour(%d) — indices %d and %d are indistinguishable, so this test cannot see a wrong index", i, prev, i, prev)
		}
		seen[tileColour(i)] = i
	}

	for i, name := range names {
		col, row := i%cols, i/cols
		x := Padding + col*cellW
		y := Padding + row*cellH

		want := tileColour(i)
		for ty := 0; ty < th; ty++ {
			for tx := 0; tx < tw; tx++ {
				r, g, b, _ := sh.At(x+tx, y+ty).RGBA()
				if uint8(r>>8) != want.R || uint8(g>>8) != want.G || uint8(b>>8) != want.B {
					got := color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 255}
					msg := ""
					if j, ok := seen[got]; ok {
						msg = fmt.Sprintf(" — that is tile %d's colour, so cell %d was rendered from the wrong index", j, i)
					}
					t.Fatalf("cell %d (%q) pixel (%d,%d): sheet has %v, want %v%s", i, name, tx, ty, got, want, msg)
				}
			}
		}

		// The label band, against a reference drawn for THIS cell's name.
		ref := image.NewRGBA(image.Rect(0, 0, cellW, cellH-th))
		draw.Draw(ref, ref.Bounds(), image.NewUniform(color.RGBA{R: 26, G: 26, B: 28, A: 255}), image.Point{}, draw.Src)
		d := &font.Drawer{
			Dst:  ref,
			Src:  image.NewUniform(color.RGBA{R: 225, G: 225, B: 230, A: 255}),
			Face: basicfont.Face7x13,
			Dot:  fixed.P(2, 13),
		}
		d.DrawString(name)
		var ink int
		for by := 0; by < cellH-th; by++ {
			for bx := 0; bx < cellW; bx++ {
				wr, wg, wb, _ := ref.At(bx, by).RGBA()
				gr, gg, gb, _ := sh.At(x+bx, y+th+by).RGBA()
				if wr != 26<<8 || wg != 26<<8 || wb != 28<<8 {
					ink++
				}
				if wr != gr || wg != gg || wb != gb {
					t.Fatalf("cell %d label band pixel (%d,%d): sheet has (%d,%d,%d), the band drawn for %q has (%d,%d,%d) — this cell is not labelled with its own name",
						i, bx, by, gr>>8, gg>>8, gb>>8, name, wr>>8, wg>>8, wb>>8)
				}
			}
		}
		if ink == 0 {
			t.Fatalf("cell %d: the reference band for %q has no ink, so the comparison above compared two blank bands", i, name)
		}
	}
}
