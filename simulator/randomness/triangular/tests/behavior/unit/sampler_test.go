package triangular_test

import (
	"testing"

	"load-balancer/randomness/internal/testutil"
	"load-balancer/randomness/triangular"
)

func TestInteriorMode(t *testing.T) {
	// The density rises from 2 to mode 4, then falls toward 10.
	testutil.Continuous(t, triangular.New, `{
  "kind": "triangular",
  "unit": "number",
  "min": "2",
  "max": "10",
  "mode": "4"
}`, func(x float64) float64 {
		if x <= 4 {
			return (x - 2) * (x - 2) / 16
		}
		return 1 - (10-x)*(10-x)/48
	})
}

func TestEndpointModes(t *testing.T) {
	t.Run("lower", func(t *testing.T) {
		testutil.Continuous(t, triangular.New, `{
  "kind": "triangular",
  "unit": "number",
  "min": "0",
  "max": "1",
  "mode": "0"
}`, func(x float64) float64 {
			return 2*x - x*x
		})
	})

	t.Run("upper", func(t *testing.T) {
		testutil.Continuous(t, triangular.New, `{
  "kind": "triangular",
  "unit": "number",
  "min": "0",
  "max": "1",
  "mode": "1"
}`, func(x float64) float64 {
			return x * x
		})
	})
}
