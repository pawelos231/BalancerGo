package gamma_test

import (
	"math"
	"math/big"
	"strings"
	"testing"

	"load-balancer/randomness/gamma"
	"load-balancer/randomness/internal/testutil"
	"load-balancer/randomness/profile"
)

func TestErlangTwoOracle(t *testing.T) {
	// Shape 2 sums two exponential stages with mean 3 each.
	testutil.Continuous(t, gamma.New, `{
  "kind": "gamma",
  "unit": "number",
  "min": "1",
  "max": "30",
  "shape": "2",
  "scale": "3"
}`, func(x float64) float64 {
		return 1 - math.Exp(-x/3)*(1+x/3)
	})
}

func TestShapeOneIsExponential(t *testing.T) {
	testutil.Continuous(t, gamma.New, `{
  "kind": "gamma",
  "unit": "number",
  "min": "0",
  "max": "16",
  "shape": "1",
  "scale": "2"
}`, func(x float64) float64 {
		return -math.Expm1(-x / 2)
	})
}

func TestTinyTailPreparationAndDrawsDoNotPanic(t *testing.T) {
	// These tiny lower tails previously panicked inside the Gamma inverse.
	cases := []struct {
		name  string
		shape string
		zeros int
		final string
	}{
		{name: "constructor-shape-three", shape: "3", zeros: 106, final: "1"},
		{name: "constructor-shape-ten", shape: "10", zeros: 31, final: "2"},
		{name: "draw-shape-three-103", shape: "3", zeros: 102, final: "1"},
		{name: "draw-shape-three-104", shape: "3", zeros: 103, final: "1"},
		{name: "draw-shape-four-77", shape: "4", zeros: 76, final: "1"},
		{name: "draw-shape-four-78", shape: "4", zeros: 77, final: "1"},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			maximum := "0." + strings.Repeat("0", test.zeros) + test.final
			document := `{
  "kind": "gamma",
  "unit": "number",
  "min": "0",
  "max": "` + maximum + `",
  "shape": "` + test.shape + `",
  "scale": "1"
}`
			definition := testutil.Profile(t, document)

			sampler, err := gamma.New(definition)
			if err != nil {
				// An exact definition can remain unavailable numerically.
				return
			}

			upperExact, _ := profile.ParseDecimal(maximum)
			upper, _ := upperExact.Float64()
			for _, word := range []uint64{0, 1 << 63, ^uint64(0)} {
				value := sampler.Sample(&testutil.Word{Value: word})
				if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
					t.Fatalf("invalid tiny-tail sample %g", value)
				}
				if new(big.Rat).SetFloat64(value).Cmp(upperExact) >= 0 {
					t.Fatalf("sample %g outside tiny support [0,%g)", value, upper)
				}
			}
		})
	}
}
