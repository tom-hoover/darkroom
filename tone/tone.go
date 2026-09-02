// Package tone holds the pixel-math kernel shared by the rendering pipelines.
//
// RadiusPx is the reason this package exists rather than a copy in each
// pipeline. It carries the scale-invariance contract that makes a small
// contact-sheet tile predict a full-resolution render, and tasks/lessons .md
// records that re-deriving that single line silently broke resolution
// independence at 4096px — a defect a whole-image difference test could not
// detect above a certain size. One definition, one place to get it wrong.
package tone

import "math"

// RadiusPx converts a spatial parameter expressed as a FRACTION of an image's
// short edge into a pixel count.
//
// Any cap, clamp, fast path, or narrowing conversion applied here silently
// makes every caller resolution-dependent. There is deliberately nothing else
// in this function.
func RadiusPx(fraction float64, short int) int {
	return int(math.Round(fraction * float64(short)))
}

// Sigmoid applies a normalised sigmoidal contrast curve, so that f(0) = 0 and
// f(1) = 1 exactly however strong the curve. A strength of zero is the
// identity, which is what the "flat" preset relies on.
func Sigmoid(x, k, pivot float64) float64 {
	if k == 0 {
		return x
	}
	u := func(t float64) float64 { return 1 / (1 + math.Exp(k*(pivot-t))) }
	u0, u1 := u(0), u(1)
	if u1 == u0 {
		return x
	}
	return Clamp01((u(x) - u0) / (u1 - u0))
}

// SRGBToLinear and LinearToSRGB convert between the sRGB transfer function and
// linear light.
func SRGBToLinear(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

func LinearToSRGB(c float64) float64 {
	if c <= 0.0031308 {
		return c * 12.92
	}
	return 1.055*math.Pow(c, 1/2.4) - 0.055
}

func Clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// BlurBox3 blurs a single-channel image with three box passes, which
// approximates a Gaussian closely enough for local contrast while running in
// time independent of the radius. That matters here: a full-resolution clarity
// radius is a hundred pixels or more, where a true Gaussian convolution would
// dominate the render.
//
// Edges are handled by clamping to the border pixel. The source is not
// modified.
func BlurBox3(src []float64, w, h, radius int) []float64 {
	dst := make([]float64, len(src))
	var tmp []float64
	if radius > 0 && w > 0 && h > 0 {
		tmp = make([]float64, len(src))
	}
	BlurBox3Into(dst, src, tmp, w, h, radius)
	return dst
}

// BlurBox3Into is BlurBox3 with the buffers owned by the caller: dst receives
// the result and tmp is scratch, whose contents afterwards are undefined. src
// is not modified.
//
// It exists for a caller that blurs the same image more than once. Each blur
// needs two working planes, and at 8 bytes per pixel per plane a second blur
// that allocates its own pair costs another 16 bytes/px of live heap for as
// long as the first pair has not been collected — 380MB per worker at 24MP,
// multiplied by the job count. Handing the same buffers to every blur removes
// that entirely; BlurBox3 stays for callers that blur once, where owning the
// buffers would be noise.
//
// dst, src and tmp must all have len(src) elements, and neither dst nor tmp
// may alias src.
func BlurBox3Into(dst, src, tmp []float64, w, h, radius int) {
	copy(dst, src)
	if radius <= 0 || w <= 0 || h <= 0 {
		return
	}
	for i := 0; i < 3; i++ {
		boxH(dst, tmp, w, h, radius)
		boxV(tmp, dst, w, h, radius)
	}
}

// boxH runs a horizontal moving-average over each row, writing into dst.
func boxH(src, dst []float64, w, h, r int) {
	if r > w-1 {
		r = w - 1
	}
	window := float64(2*r + 1)
	for y := 0; y < h; y++ {
		row := y * w
		// Prime the running sum for x = 0, with the left edge clamped.
		sum := src[row] * float64(r+1)
		for x := 1; x <= r; x++ {
			sum += src[row+min(x, w-1)]
		}
		for x := 0; x < w; x++ {
			dst[row+x] = sum / window
			add := src[row+min(x+r+1, w-1)]
			sub := src[row+max(x-r, 0)]
			sum += add - sub
		}
	}
}

// boxV runs a vertical moving-average over each column, writing into dst.
func boxV(src, dst []float64, w, h, r int) {
	if r > h-1 {
		r = h - 1
	}
	window := float64(2*r + 1)
	for x := 0; x < w; x++ {
		sum := src[x] * float64(r+1)
		for y := 1; y <= r; y++ {
			sum += src[min(y, h-1)*w+x]
		}
		for y := 0; y < h; y++ {
			dst[y*w+x] = sum / window
			add := src[min(y+r+1, h-1)*w+x]
			sub := src[max(y-r, 0)*w+x]
			sum += add - sub
		}
	}
}
