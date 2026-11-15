package numeric

import (
	"math"
	"testing"

	"load-balancer/randomness/internal/testutil"
)

func TestBisectionReachesSubnormalAdjacency(t *testing.T) {
	bounds := Bounds{0, 1}
	scale := math.SmallestNonzeroFloat64 * 4
	sampler := &conditionalSampler{bounds: bounds, distribution: Distribution{CDF: func(x float64) float64 {
		return math.Min(1, x/scale)
	}}}
	value := sampler.bisect(.5)
	if value != math.SmallestNonzeroFloat64 {
		t.Fatalf("subnormal lower adjacent bracket=%g", value)
	}
}

func TestFractionNeverIncludesAnEndpoint(t *testing.T) {
	for _, word := range []uint64{0, ^uint64(0)} {
		u := Fraction(&testutil.Word{Value: word})
		if !(u > 0 && u < 1) {
			t.Fatalf("fraction=%g", u)
		}
	}
}

func TestSingleFloatConsumesNothing(t *testing.T) {
	bounds := Bounds{1, math.Nextafter(1, 2)}
	sampler, err := NewQuantile(bounds, func(float64) float64 {
		panic("singleton must not invert")
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := sampler.Sample(nil); got != 1 {
		t.Fatal(got)
	}
}
