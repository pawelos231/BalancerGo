package pareto_test

import (
	"testing"

	"load-balancer/randomness/internal/testutil"
	"load-balancer/randomness/pareto"
)

func TestTypeOneConditionalPareto(t *testing.T) {
	// Start above the Pareto scale to retain only its decreasing tail.
	testutil.Continuous(t, pareto.New, `{
  "kind": "pareto",
  "unit": "bytes",
  "min": "4",
  "max": "40",
  "scale": "2",
  "shape": "1"
}`, func(x float64) float64 {
		return 1 - 2/x
	})
}

func TestNonUnitShape(t *testing.T) {
	testutil.Continuous(t, pareto.New, `{
  "kind": "pareto",
  "unit": "bytes",
  "min": "4",
  "max": "40",
  "scale": "2",
  "shape": "2"
}`, func(x float64) float64 {
		return 1 - 4/(x*x)
	})
}
