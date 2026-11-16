package empirical_test

import (
	"testing"

	"load-balancer/randomness/empirical"
	"load-balancer/randomness/internal/testutil"
)

func TestDuplicateObservationMass(t *testing.T) {
	// Repeating 5 gives it twice the probability of 10.
	definition := testutil.Profile(t, `{
  "kind": "empirical",
  "unit": "nanoseconds",
  "observations": [
    "5",
    "5",
    "10"
  ]
}`)

	sampler, err := empirical.NewInteger(definition)
	if err != nil {
		t.Fatal(err)
	}

	testutil.Frequencies(t, sampler, map[int64]float64{5: 2.0 / 3, 10: 1.0 / 3})
}

func TestFloorOnceAndSnapshot(t *testing.T) {
	// Both exact observations floor to the same integer without a random draw.
	definition := testutil.Profile(t, `{
  "kind": "empirical",
  "unit": "bytes",
  "observations": [
    "9007199254740993.25",
    "9007199254740993.75"
  ]
}`)

	sampler, err := empirical.NewInteger(definition)
	if err != nil {
		t.Fatal(err)
	}

	definition.Observations[0] = "0"

	rng := &testutil.Word{}
	if got := sampler.Sample(rng); got != 9007199254740993 || rng.Draws != 0 {
		t.Fatalf("integer observation changed: value=%d draws=%d", got, rng.Draws)
	}

	if _, err := empirical.New(definition); err == nil {
		t.Fatal("inexact float output admitted")
	}
}
