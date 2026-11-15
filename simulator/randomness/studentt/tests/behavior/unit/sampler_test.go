package studentt_test

import (
	"math"
	"testing"

	"load-balancer/randomness/internal/testutil"
	"load-balancer/randomness/studentt"
)

func TestCauchyOracle(t *testing.T) {
	// One degree of freedom gives the Cauchy CDF.
	testutil.Continuous(t, studentt.New, `{
  "kind": "studentT",
  "unit": "number",
  "min": "0",
  "max": "20",
  "location": "5",
  "scale": "2",
  "degreesOfFreedom": "1"
}`, func(x float64) float64 {
		return .5 + math.Atan((x-5)/2)/math.Pi
	})
}

func TestDegreesTwoOracle(t *testing.T) {
	testutil.Continuous(t, studentt.New, `{
  "kind": "studentT",
  "unit": "number",
  "min": "0",
  "max": "20",
  "location": "5",
  "scale": "2",
  "degreesOfFreedom": "2"
}`, func(x float64) float64 {
		z := (x - 5) / 2
		return .5 + z/(2*math.Sqrt(2+z*z))
	})
}
