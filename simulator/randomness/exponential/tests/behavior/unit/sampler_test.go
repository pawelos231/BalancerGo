package exponential_test

import (
	"math"
	"testing"

	"load-balancer/randomness/exponential"
	"load-balancer/randomness/internal/testutil"
)

func TestMemorylessConditionalExponential(t *testing.T) {
	// Keep delays in [2ns, 20ns); the underlying mean is 4ns.
	testutil.Continuous(t, exponential.New, `{
  "kind": "exponential",
  "unit": "nanoseconds",
  "min": "2",
  "max": "20",
  "mean": "4"
}`, func(x float64) float64 {
		return -math.Expm1(-x / 4)
	})
}

func TestDeepTailAvoidsVanishingUnconditionalMass(t *testing.T) {
	// The original tail underflows; measuring from min keeps its shape available.
	definition := testutil.Profile(t, `{
  "kind": "exponential",
  "unit": "number",
  "min": "1000",
  "max": "1001",
  "mean": "1"
}`)

	sampler, err := exponential.New(definition)
	if err != nil {
		t.Fatal(err)
	}

	value := sampler.Sample(&testutil.Word{Value: 1 << 63})
	expected := 1000 - math.Log1p(-.5*(-math.Expm1(-1)))
	if math.Abs(value-expected) > 1e-12 {
		t.Fatalf("got %g want %g", value, expected)
	}
}
