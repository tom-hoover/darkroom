package tone

import (
	"math"
	"testing"
)

func TestBlurUniformIsUnchanged(t *testing.T) {
	w, h := 16, 12
	src := make([]float64, w*h)
	for i := range src {
		src[i] = 0.42
	}
	got := BlurBox3(src, w, h, 3)
	for i, v := range got {
		if math.Abs(v-0.42) > 1e-9 {
			t.Fatalf("pixel %d = %v, want 0.42 (a uniform field must survive blurring)", i, v)
		}
	}
}

func TestBlurZeroRadiusIsIdentity(t *testing.T) {
	w, h := 4, 4
	src := []float64{
		0, 1, 0, 1,
		1, 0, 1, 0,
		0, 1, 0, 1,
		1, 0, 1, 0,
	}
	got := BlurBox3(src, w, h, 0)
	for i := range src {
		if math.Abs(got[i]-src[i]) > 1e-9 {
			t.Fatalf("pixel %d changed with radius 0", i)
		}
	}
}

func TestBlurDoesNotMutateSource(t *testing.T) {
	w, h := 8, 8
	src := make([]float64, w*h)
	src[27] = 1
	cp := append([]float64(nil), src...)
	BlurBox3(src, w, h, 2)
	for i := range src {
		if src[i] != cp[i] {
			t.Fatalf("BlurBox3 mutated its input at %d", i)
		}
	}
}

// TestBlurIntoMatchesAllocatingForm pins the only thing BlurBox3Into is
// allowed to change: who owns the buffers. Reusing one dst/tmp pair across
// several blurs is worthless if a reused buffer's leftover contents leak into
// the next result, so the same pair is deliberately used twice here, with a
// different radius, and each result is compared against the allocating form.
//
// The reversed radius order matters: a stale dst would carry the wider blur's
// output into the narrower one, which is invisible if the second call is the
// one with the wider radius.
func TestBlurIntoMatchesAllocatingForm(t *testing.T) {
	const w, h = 24, 18
	src := make([]float64, w*h)
	for i := range src {
		src[i] = float64((i*37)%11) / 10 // a non-smooth field, so a stale buffer shows
	}
	cp := append([]float64(nil), src...)

	dst := make([]float64, w*h)
	tmp := make([]float64, w*h)
	for _, radius := range []int{5, 1, 0, 3} {
		BlurBox3Into(dst, src, tmp, w, h, radius)
		want := BlurBox3(src, w, h, radius)
		for i := range want {
			if math.Abs(dst[i]-want[i]) > 1e-12 {
				t.Fatalf("radius %d, pixel %d: BlurBox3Into gave %v, BlurBox3 gave %v — the reused buffers are not being fully overwritten",
					radius, i, dst[i], want[i])
			}
		}
		for i := range src {
			if src[i] != cp[i] {
				t.Fatalf("radius %d: BlurBox3Into mutated its source at %d", radius, i)
			}
		}
	}
}

func TestBlurSpreadsAndConservesEnergy(t *testing.T) {
	w, h := 21, 21
	src := make([]float64, w*h)
	src[10*w+10] = 1 // single bright pixel at the centre
	got := BlurBox3(src, w, h, 2)

	if got[10*w+10] >= 1 {
		t.Error("centre should have dimmed")
	}
	if got[10*w+11] <= 0 {
		t.Error("energy should have spread to the neighbour")
	}
	// Clamped edges conserve total energy for an impulse away from the border.
	var sum float64
	for _, v := range got {
		sum += v
	}
	if math.Abs(sum-1) > 1e-6 {
		t.Errorf("total energy = %v, want 1", sum)
	}
	// Symmetry: the result must be identical left and right of centre.
	if math.Abs(got[10*w+9]-got[10*w+11]) > 1e-9 {
		t.Error("blur is not symmetric")
	}
}
