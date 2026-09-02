package jobplan

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tom-hoover/darkroom/imaging"
)

func writeJPEG(t *testing.T, path string, c color.RGBA) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 24))
	for y := 0; y < 24; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, nil); err != nil {
		t.Fatal(err)
	}
}

// grey renders a flat tile, standing in for a real pipeline. jobplan has no
// opinion about pixels, so a renderer that ignores its input is honest here
// rather than a shortcut.
func grey(src image.Image) image.Image {
	b := src.Bounds()
	out := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	for i := range out.Pix {
		out.Pix[i] = 128
	}
	return out
}

// TestRenderFileProducesGreyJPEG pins RenderFile's own plumbing: what the
// renderer returns is exactly what lands in the file, decodable and at the
// source's dimensions. The colour science — which style does what to which
// pixel — belongs to internal/bw and internal/ciba and is covered there; at
// this layer a renderer that ignores its input is the honest fixture, so the
// assertion is on fidelity to grey's flat 128 rather than on a tone curve.
func TestRenderFileProducesGreyJPEG(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "sky.jpg")
	dst := filepath.Join(dir, "sky-test.jpg")
	writeJPEG(t, src, color.RGBA{R: 40, G: 90, B: 190, A: 255})

	if err := testCmd.RenderFile(Job{Src: src, Dst: dst}, grey, 95); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := img.(*image.Gray); !ok {
		t.Errorf("output is %T, want *image.Gray", img)
	}
	if b := img.Bounds(); b.Dx() != 32 || b.Dy() != 24 {
		t.Errorf("output size = %dx%d, want 32x24", b.Dx(), b.Dy())
	}
	// grey writes a flat 128. A JPEG q95 round trip on a uniform 32x24 block
	// perturbs a channel by at most a couple of levels, so a tolerance of 3
	// absorbs codec noise while still failing if the render were skipped and
	// the source's blue sky copied through instead.
	r, _, _, _ := img.At(16, 12).RGBA()
	if d := absDiff8(r>>8, 128); d > 3 {
		t.Errorf("pixel = %d, want 128 ± 3 (the renderer's flat grey)", r>>8)
	}
}

// absDiff8 returns the absolute difference between two 8-bit-range channel
// values carried in uint32 (as img.At(...).RGBA() yields after >>8).
func absDiff8(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

func TestRenderFileLeavesOriginalUntouched(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.jpg")
	writeJPEG(t, src, color.RGBA{R: 200, G: 200, B: 200, A: 255})
	before, _ := os.ReadFile(src)

	if err := testCmd.RenderFile(Job{Src: src, Dst: filepath.Join(dir, "a-test.jpg")}, grey, 90); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(src)
	if string(before) != string(after) {
		t.Fatal("the original was modified")
	}
}

func TestRenderFileWritesNothingOnDecodeFailure(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "broken.jpg")
	os.WriteFile(src, []byte("not a jpeg"), 0o644)
	dst := filepath.Join(dir, "broken-test.jpg")

	if err := testCmd.RenderFile(Job{Src: src, Dst: dst}, grey, 95); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("a failed render left a partial file behind")
	}
}

// RenderFile ends in os.Rename onto Dst, so a Dst equal to Src replaces the
// original with its own render and nothing survives. Scan and ScanOne refuse
// such a job; this proves the write itself refuses too, however the Job was
// built. The checksum either side is the point: the original must be untouched.
func TestRenderFileRefusesToOverwriteItsSource(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "one.jpg")
	writeJPEG(t, src, color.RGBA{R: 40, G: 90, B: 190, A: 255})
	before, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	sumBefore := sha256.Sum256(before)

	t.Chdir(dir)
	for _, dst := range []string{src, "one.jpg", "./one.jpg", filepath.Join(dir, ".", "one.jpg")} {
		if err := testCmd.RenderFile(Job{Src: src, Dst: dst}, grey, 95); err == nil {
			t.Errorf("RenderFile wrote to %q, its own source", dst)
		}
		after, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if sha256.Sum256(after) != sumBefore {
			t.Fatalf("the original was modified via dst %q (%x -> %x)", dst, sumBefore, sha256.Sum256(after))
		}
	}
}

// RenderFile carries the same guard as Scan, and it has to see through a
// symlinked destination directory too: os.Rename resolves the link before
// replacing its target, so "link/a.jpg" where link -> the source directory
// writes the render over the original. The two layers are independent defences
// only if both resolve symlinks. symlink lives in jobplan_test.go.
func TestRenderFileRefusesSymlinkedDestination(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "photos")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(real, "a.jpg")
	writeJPEG(t, src, color.RGBA{R: 40, G: 90, B: 190, A: 255})
	before, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	sumBefore := sha256.Sum256(before)
	link := filepath.Join(base, "link")
	symlink(t, real, link)

	// A hand-built Job — exactly the case this second layer exists for.
	if err := testCmd.RenderFile(Job{Src: src, Dst: filepath.Join(link, "a.jpg")}, grey, 95); err == nil {
		t.Error("RenderFile accepted a destination that is its own source through a symlink")
	} else if !strings.Contains(err.Error(), "refusing to write over the source image") {
		t.Errorf("error = %v, want the refusal", err)
	}
	after, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(after) != sumBefore {
		t.Fatalf("the original was modified (%x -> %x)", sumBefore, sha256.Sum256(after))
	}
}

// The mirror of the above: a symlinked destination that points somewhere else
// is ordinary and must still render.
func TestRenderFileWritesThroughASymlinkElsewhere(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "photos")
	elsewhere := filepath.Join(base, "scratch")
	for _, d := range []string{real, elsewhere} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	src := filepath.Join(real, "a.jpg")
	writeJPEG(t, src, color.RGBA{R: 40, G: 90, B: 190, A: 255})
	link := filepath.Join(base, "out-link")
	symlink(t, elsewhere, link)

	dst := filepath.Join(link, "a.jpg")
	if err := testCmd.RenderFile(Job{Src: src, Dst: dst}, grey, 95); err != nil {
		t.Fatalf("RenderFile through a symlink to another directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(elsewhere, "a.jpg")); err != nil {
		t.Fatalf("nothing landed in the real output directory: %v", err)
	}
}

// writeJPEGWithExif writes a JPEG to path carrying a real EXIF block, adapted
// from internal/imaging/decode_test.go's TestExifSurvivesDecodeThenWriteJPEG
// (jpegWithSegments + minimalExif) so this package can build a fixture that
// exercises the same metadata path RenderFile actually uses, without
// depending on internal/imaging's unexported test helpers.
func writeJPEGWithExif(t *testing.T, path string) {
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

	// A minimal but real EXIF block: the "Exif\0\0" identifier a JPEG APP1
	// segment carries, followed by a valid little-endian TIFF header holding
	// one Orientation tag.
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
	exif := append([]byte("Exif\x00\x00"), tiff...)

	seg := make([]byte, 4, 4+len(exif))
	seg[0], seg[1] = 0xFF, 0xE1
	binary.BigEndian.PutUint16(seg[2:4], uint16(len(exif)+2))
	seg = append(seg, exif...)

	out := append([]byte{0xFF, 0xD8}, seg...)
	out = append(out, b[2:]...)
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestExifSurvivesRenderFile covers the whole path — decode, render, encode —
// rather than the encoder alone. EXIF preservation shipped on the skyburn
// branch with no end-to-end test and a whole-branch review had to find it.
func TestExifSurvivesRenderFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.jpg")
	writeJPEGWithExif(t, src)
	j := Job{Src: src, Dst: filepath.Join(dir, "out.jpg")}
	if err := testCmd.RenderFile(j, grey, 95); err != nil {
		t.Fatal(err)
	}
	_, exif, err := imaging.Decode(j.Dst)
	if err != nil {
		t.Fatal(err)
	}
	if len(exif) == 0 {
		t.Fatal("the rendered JPEG carries no EXIF block; it was dropped somewhere between decode and encode")
	}
}
