package lognormal_test

import (
	"math"
	"testing"

	"load-balancer/randomness/internal/testutil"
	"load-balancer/randomness/lognormal"
)

func TestMedianAndLogStddevOracle(t *testing.T) {
	// The underlying median is 4 bytes; spread is measured on the log scale.
	testutil.Continuous(t, lognormal.New, `{
  "kind": "lognormal",
  "unit": "bytes",
  "min": "0.5",
  "max": "30",
  "median": "4",
  "logStddev": "0.75"
}`, func(x float64) float64 {
		if x <= 0 {
			return 0
		}
		return .5 * math.Erfc(-math.Log(x/4)/.75/math.Sqrt2)
	})
}
