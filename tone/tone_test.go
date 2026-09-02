package tone

import (
	"math"
	"testing"
)

// curveKs are the strengths worth asserting over: 0 (the identity the "flat"
// preset relies on), a weak curve, the range the presets actually use, and the
// maximum both pipelines' Validate permits — bw.Style and ciba.Look each cap
// Curve at 30.
var curveKs = []float64{0, 0.5, 2.5, 6, 12, 30}

// curvePivots bracket the tonal centre either side of mid grey.
var curvePivots = []float64{0.4, 0.5, 0.6}

func TestSigmoidEndpointsAreExact(t *testing.T) {
	// 300 is far past anything Validate allows; the endpoints must survive it
	// anyway, because they are what normalisation exists to guarantee.
	for _, k := range append(append([]float64{}, curveKs...), 300) {
		for _, pivot := range curvePivots {
			if got := Sigmoid(0, k, pivot); got != 0 {
				t.Errorf("Sigmoid(0, k=%g, pivot=%g) = %v, want exactly 0 — black no longer reaches true black", k, pivot, got)
			}
			if got := Sigmoid(1, k, pivot); got != 1 {
				t.Errorf("Sigmoid(1, k=%g, pivot=%g) = %v, want exactly 1 — white no longer reaches true white", k, pivot, got)
			}
		}
	}
}

func TestSigmoidZeroStrengthIsIdentity(t *testing.T) {
	for _, pivot := range curvePivots {
		for i := 0; i <= 200; i++ {
			x := float64(i) / 200
			if got := Sigmoid(x, 0, pivot); got != x {
				t.Fatalf("Sigmoid(%v, k=0, pivot=%g) = %v, want the identity — both pipelines' flat presets rely on it", x, pivot, got)
			}
		}
	}
}

func TestSigmoidIsStrictlyMonotonicAcrossARamp(t *testing.T) {
	const n = 200
	for _, k := range curveKs {
		for _, pivot := range curvePivots {
			prev := math.Inf(-1)
			for i := 0; i <= n; i++ {
				x := float64(i) / n
				v := Sigmoid(x, k, pivot)
				if math.IsNaN(v) || math.IsInf(v, 0) {
					t.Fatalf("Sigmoid(%v, k=%g, pivot=%g) = %v", x, k, pivot, v)
				}
				if v < 0 || v > 1 {
					t.Fatalf("Sigmoid(%v, k=%g, pivot=%g) = %v, outside 0..1", x, k, pivot, v)
				}
				if v <= prev {
					t.Fatalf("Sigmoid is not strictly increasing at x=%v (k=%g, pivot=%g): %v followed %v — a flat or reversed curve loses tonal separation", x, k, pivot, v, prev)
				}
				prev = v
			}
		}
	}
}

// A strength of 300 is an order of magnitude past the 30 Validate permits, and
// at that steepness the curve saturates to exactly 1.0 — in float64, not in
// arithmetic — well before x reaches 1. Equal neighbouring samples there are
// representable precision, not a defect, so this asserts non-decreasing rather
// than strictly increasing. Everything else still has to hold.
func TestSigmoidExtremeStrengthStaysFiniteAndOrdered(t *testing.T) {
	const n = 200
	for _, pivot := range curvePivots {
		prev := math.Inf(-1)
		for i := 0; i <= n; i++ {
			x := float64(i) / n
			v := Sigmoid(x, 300, pivot)
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Fatalf("Sigmoid(%v, k=300, pivot=%g) = %v", x, pivot, v)
			}
			if v < 0 || v > 1 {
				t.Fatalf("Sigmoid(%v, k=300, pivot=%g) = %v, outside 0..1", x, pivot, v)
			}
			if v < prev {
				t.Fatalf("Sigmoid went backwards at x=%v (k=300, pivot=%g): %v after %v", x, pivot, v, prev)
			}
			prev = v
		}
	}
}

// TestSigmoidSteepensAroundThePivot exists because every other sigmoid test is
// satisfied by f(x) = x. Exact endpoints, the k=0 identity, strict monotonicity
// and finiteness at extreme k are all properties the identity function has, so
// replacing the body of sigmoid with "return x" — disabling the tone curve, the
// whole point of the product — left the entire suite green. On a grey ramp that
// mutant differs from the real curve by a mean of 25/255 for "stark". Nothing
// asserted that the curve had any shape at all; this does.
//
// The shape asserted is what "increases contrast" means: values are pushed away
// from the pivot, darker below it and brighter above it. It is stated relative
// to where the pivot itself lands, f(pivot), rather than against x directly.
// That is not a weakening — it is the only correct statement for this curve.
// Normalisation pins f(0) = 0 and f(1) = 1, so with an off-centre pivot the
// whole curve is displaced: at pivot 0.4 every f(x) sits above x, and comparing
// f(x) with x would measure that displacement instead of the S-shape. Around
// pivot 0.5, where displacement is zero, the plain form does hold and
// TestSigmoidDarkensBelowMidGreyAndBrightensAbove asserts exactly it.
//
// Samples stay within 0.3 of the pivot: further out the curve runs into the
// pinned endpoint and the separation stops growing, which is the curve's
// shoulder, not a defect.
func TestSigmoidSteepensAroundThePivot(t *testing.T) {
	offsets := []float64{0.1, 0.2, 0.3}
	for _, k := range curveKs {
		if k == 0 {
			continue // the identity by contract; the "flat" preset needs it
		}
		for _, pivot := range curvePivots {
			fp := Sigmoid(pivot, k, pivot)
			for _, d := range offsets {
				lo, hi := pivot-d, pivot+d
				if lo <= 0.02 || hi >= 0.98 {
					continue // too close to the pinned endpoints to say anything
				}
				if got := Sigmoid(lo, k, pivot); fp-got <= d {
					t.Errorf("Sigmoid(%.2f, k=%g, pivot=%g) = %v: it sits %v below f(pivot)=%v, no more than the %v it started with — the curve is not darkening below the pivot, so contrast has been lost",
						lo, k, pivot, got, fp-got, fp, d)
				}
				if got := Sigmoid(hi, k, pivot); got-fp <= d {
					t.Errorf("Sigmoid(%.2f, k=%g, pivot=%g) = %v: it sits %v above f(pivot)=%v, no more than the %v it started with — the curve is not brightening above the pivot, so contrast has been lost",
						hi, k, pivot, got, got-fp, fp, d)
				}
			}
		}
	}
}

// The plain, absolute form of the same property, which holds exactly at the
// symmetric pivot: below mid grey the curve darkens, above it brightens. Stated
// separately from the test above because at pivot 0.5 there is no normalisation
// displacement to account for, so this is the strongest form available and it
// reads as the feature is described: sky goes black, cloud stays white.
func TestSigmoidDarkensBelowMidGreyAndBrightensAbove(t *testing.T) {
	const pivot = 0.5
	for _, k := range curveKs {
		if k == 0 {
			continue
		}
		for _, d := range []float64{0.1, 0.2, 0.3, 0.4} {
			lo, hi := pivot-d, pivot+d
			if got := Sigmoid(lo, k, pivot); got >= lo {
				t.Errorf("Sigmoid(%.2f, k=%g, pivot=0.5) = %v, want < %v — a tone below mid grey must be darkened, not left alone", lo, k, got, lo)
			}
			if got := Sigmoid(hi, k, pivot); got <= hi {
				t.Errorf("Sigmoid(%.2f, k=%g, pivot=0.5) = %v, want > %v — a tone above mid grey must be brightened, not left alone", hi, k, got, hi)
			}
		}
	}
}

// TestRadiusPxIsProportionalAtEveryScale pins the scale-invariance contract at
// its source. The effective fraction must match the declared one to within the
// one pixel that rounding can cost, at every size any caller might use.
func TestRadiusPxIsProportionalAtEveryScale(t *testing.T) {
	for _, frac := range []float64{0.005, 0.018, 0.02, 0.06, 0.07, 0.25} {
		for _, short := range []int{64, 256, 1024, 2404, 4096, 8192, 16384} {
			r := RadiusPx(frac, short)
			got := float64(r) / float64(short)
			if math.Abs(got-frac) > 1.0/float64(short) {
				t.Errorf("fraction %.4f at short edge %d: effective %.6f (radius %dpx) — a cap or clamp is in play",
					frac, short, got, r)
			}
		}
	}
}

// TestRadiusPxUsesWhatItIsGiven guards against RadiusPx growing a hidden
// minimum. A floor of even one pixel would make a small tile blur where the
// full-resolution render does not.
func TestRadiusPxUsesWhatItIsGiven(t *testing.T) {
	if got := RadiusPx(0, 4096); got != 0 {
		t.Errorf("RadiusPx(0, 4096) = %d, want 0", got)
	}
	if got := RadiusPx(0.02, 10); got != 0 {
		t.Errorf("RadiusPx(0.02, 10) = %d, want 0 — 0.2px rounds to none", got)
	}
}
