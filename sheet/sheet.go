// Package sheet lays out a labelled contact sheet of rendered thumbnails.
//
// It is renderer-agnostic — the caller supplies the tile renderer — so the
// same geometry and label-fit logic can serve more than one rendering
// pipeline without being re-derived per pipeline. That sharing is deliberate:
// label-fit is easy to get wrong in a way that only shows up as clipped or
// overlapping text, which a test that recomputes the implementation's own
// arithmetic instead of reading the rendered pixels will not catch — see
// TestContactSheetLabelsFitTheirCells, in sheet_test.go, for exactly that
// failure mode and how this package guards against it. Re-deriving this
// geometry in a second package means re-deriving that guard too.
package sheet

import (
	"image"
	"image/color"
	"image/draw"
	"math"

	"github.com/tom-hoover/darkroom/imaging"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	// Padding is the gap around and between tiles, and around the sheet's
	// edge. Exported so a caller's own tests can locate a tile inside the
	// sheet without re-declaring this constant across the package boundary.
	Padding = 8

	labelHeight = 18 // room beneath each tile for its name
)

// thumbnailFor is the single place Build decides tile size, so the sheet and
// its tests cannot disagree about it.
func thumbnailFor(img image.Image, px int) image.Image {
	return imaging.Thumbnail(img, px)
}

// Build thumbnails img ONCE and renders one tile per name, tiling the results
// into a single labelled image.
//
// The source is thumbnailed once, up front, and each tile is that thumbnail
// put through render, rather than rendered at full size and shrunk
// afterwards. This is a cost decision, not a correctness one: it relies on
// render being scale-invariant, i.e. built on primitives like tone.RadiusPx
// that take their spatial parameters as a fraction of the image rather than
// a pixel count. Every caller must pin that property with a test against its
// own renderer — this package cannot verify it on their behalf, since it
// only knows render as an opaque function. Given scale invariance, the two
// orders produce nearly identical output, so the cost is the only real
// difference: one full-resolution render per name instead of one shared
// thumbnail, roughly ninety times the work for ten names on a 12MP
// photograph.
//
// render is called with the tile's own index, and names[i] labels the tile
// render(thumb, i) produced. Nothing in the layout depends on tile content,
// so a renderer that ignored i — or an index paired with the wrong label —
// still yields a well-formed sheet; TestContactSheetPairsEachLabelWithItsOwnTile
// is what makes that pairing observable.
//
// Each label is exactly the corresponding string in names — Build does not
// alter it.
func Build(img image.Image, names []string, px int, render func(thumb image.Image, i int) image.Image) image.Image {
	if len(names) == 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	thumb := thumbnailFor(img, px)
	tb := thumb.Bounds()
	tw, th := tb.Dx(), tb.Dy()

	cols := int(math.Ceil(math.Sqrt(float64(len(names)))))
	if cols > 4 {
		cols = 4
	}
	rows := (len(names) + cols - 1) / cols

	// Cells must fit the wider of the tile and its label. Names like
	// "high-key" are wider than a small thumbnail, and a label that overflows
	// either paints over the neighbouring tile or is silently clipped at the
	// sheet's edge — draw operations clip to destination bounds, so the failure
	// is invisible rather than loud.
	labelW := 0
	for _, name := range names {
		if w := font.MeasureString(basicfont.Face7x13, name).Ceil(); w > labelW {
			labelW = w
		}
	}
	cellInner := tw
	if labelW > cellInner {
		cellInner = labelW
	}
	cellW := cellInner + Padding
	cellH := th + labelHeight + Padding
	sheet := image.NewRGBA(image.Rect(0, 0, cols*cellW+Padding, rows*cellH+Padding))
	draw.Draw(sheet, sheet.Bounds(), image.NewUniform(color.RGBA{R: 26, G: 26, B: 28, A: 255}), image.Point{}, draw.Src)

	labelColour := image.NewUniform(color.RGBA{R: 225, G: 225, B: 230, A: 255})
	for i, name := range names {
		col, row := i%cols, i/cols
		x := Padding + col*cellW
		y := Padding + row*cellH

		tile := render(thumb, i)
		draw.Draw(sheet, image.Rect(x, y, x+tw, y+th), tile, tile.Bounds().Min, draw.Src)

		d := &font.Drawer{
			Dst:  sheet,
			Src:  labelColour,
			Face: basicfont.Face7x13,
			Dot:  fixed.P(x+2, y+th+13),
		}
		d.DrawString(name)
	}
	return sheet
}
