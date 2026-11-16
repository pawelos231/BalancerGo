package weibull_test

import (
	"math"
	"testing"

	"load-balancer/randomness/internal/testutil"
	"load-balancer/randomness/weibull"
)

func TestShapeTwoOracle(t *testing.T) {
	testutil.Continuous(t, weibull.New, `{
  "kind": "weibull",
  "unit": "number",
  "min": "1",
  "max": "15",
  "shape": "2",
  "scale": "4"
}`, func(x float64) float64 {
		return -math.Expm1(-(x / 4) * (x / 4))
	})
}

func TestShapeOneIsExponential(t *testing.T) {
	// Shape 1 reduces to an exponential law with mean 4.
	testutil.Continuous(t, weibull.New, `{
  "kind": "weibull",
  "unit": "number",
  "min": "1",
  "max": "20",
  "shape": "1",
  "scale": "4"
}`, func(x float64) float64 {
		return -math.Expm1(-x / 4)
	})
}

func TestOverflowingHazardRejected(t *testing.T) {
	definition := testutil.Profile(t, `{
  "kind": "weibull",
  "unit": "number",
  "min": "1",
  "max": "10",
  "shape": "1000",
  "scale": "0.001"
}`)

	if _, err := weibull.New(definition); err == nil {
		t.Fatal("overflowing transformed support accepted")
	}
}
