package beta_test

import (
	"math"
	"testing"

	"load-balancer/randomness/beta"
	"load-balancer/randomness/internal/testutil"
)

func TestAffineBetaTwoOracle(t *testing.T) {
	// The symmetric density peaks at 50% within [20%, 80%).
	document := `{
  "kind": "beta",
  "unit": "percentage",
  "min": "20",
  "max": "80",
  "alpha": "2",
  "beta": "2"
}`

	testutil.Continuous(t, beta.New, document, func(x float64) float64 {
		z := (x - 20) / 60
		return 3*z*z - 2*z*z*z
	})
}

func TestUniformSpecialCase(t *testing.T) {
	testutil.Continuous(t, beta.New, `{
  "kind": "beta",
  "unit": "number",
  "min": "10",
  "max": "30",
  "alpha": "1",
  "beta": "1"
}`, func(x float64) float64 {
		return (x - 10) / 20
	})
}

func TestSingularEndpointShape(t *testing.T) {
	// Alpha below 1 permits a density peak at zero.
	testutil.Continuous(t, beta.New, `{
  "kind": "beta",
  "unit": "number",
  "min": "0",
  "max": "1",
  "alpha": "0.5",
  "beta": "1"
}`, func(x float64) float64 {
		if x <= 0 {
			return 0
		}
		return math.Sqrt(x)
	})
}
