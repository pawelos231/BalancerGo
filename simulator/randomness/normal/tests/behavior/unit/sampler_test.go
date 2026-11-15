package normal_test

import (
	"math"
	"testing"

	"load-balancer/randomness/internal/testutil"
	"load-balancer/randomness/normal"
)

func TestConditionalNormal(t *testing.T) {
	testutil.Continuous(t, normal.New, `{
  "kind": "normal",
  "unit": "number",
  "min": "1",
  "max": "9",
  "mean": "5",
  "stddev": "2"
}`, func(x float64) float64 {
		return .5 * math.Erfc(-(x-5)/2/math.Sqrt2)
	})
}

// This interval has many x values but only one representable survival-probability step.
// Accepting it would make every random draw return the same point.
func TestSubnormalTailProbabilityMustRemainResolved(t *testing.T) {
	definition := testutil.Profile(t, `{
  "kind": "normal",
  "unit": "number",
  "min": "38.47",
  "max": "38.57",
  "mean": "0",
  "stddev": "1"
}`)
	if sampler, err := normal.New(definition); err == nil || sampler != nil {
		t.Fatal("an unresolved probability interval must fail preparation")
	}
}

func TestUpperTailUsesSurvival(t *testing.T) {
	definition := testutil.Profile(t, `{
  "kind": "normal",
  "unit": "number",
  "min": "9",
  "max": "10",
  "mean": "0",
  "stddev": "1"
}`)
	sampler, err := normal.New(definition)
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range []uint64{0, 1 << 63, ^uint64(0)} {
		value := sampler.Sample(&testutil.Word{Value: word})
		if value < 9 || value >= 10 {
			t.Fatalf("tail sample %g", value)
		}
	}
}

func TestUnresolvedTailRejected(t *testing.T) {
	definition := testutil.Profile(t, `{
  "kind": "normal",
  "unit": "number",
  "min": "40",
  "max": "41",
  "mean": "0",
  "stddev": "1"
}`)
	if _, err := normal.New(definition); err == nil {
		t.Fatal("unresolved tail accepted")
	}
}

func TestExactIntervalWithoutFloatRejected(t *testing.T) {
	definition := testutil.Profile(t, `{
  "kind": "normal",
  "unit": "number",
  "min": "9007199254740992.1",
  "max": "9007199254740992.2",
  "mean": "1",
  "stddev": "1"
}`)
	if _, err := normal.New(definition); err == nil {
		t.Fatal("unrepresentable interval accepted")
	}
}
