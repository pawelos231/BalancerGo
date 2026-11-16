package discreteweighted_test

import (
	"testing"

	"load-balancer/randomness/discreteweighted"
	"load-balancer/randomness/internal/testutil"
)

func TestDifferentDecimalDenominators(t *testing.T) {
	// Weights 0.2 and 0.25 become probabilities 4/9 and 5/9; weight 0 is absent.
	definition := testutil.Profile(t, `{
  "kind": "discreteWeighted",
  "unit": "bytes",
  "entries": [
    {
      "value": "1.5",
      "weight": "0.2"
    },
    {
      "value": "3.25",
      "weight": "0.25"
    },
    {
      "value": "8",
      "weight": "0"
    }
  ]
}`)

	sampler, err := discreteweighted.NewInteger(definition)
	if err != nil {
		t.Fatal(err)
	}

	testutil.Frequencies(t, sampler, map[int64]float64{1: 4.0 / 9, 3: 5.0 / 9})
}

func TestLargeIntegerValuePreserved(t *testing.T) {
	definition := testutil.Profile(t, `{
  "kind": "discreteWeighted",
  "unit": "bytes",
  "entries": [
    {
      "value": "9007199254740993",
      "weight": "1"
    }
  ]
}`)

	sampler, err := discreteweighted.NewInteger(definition)
	if err != nil {
		t.Fatal(err)
	}

	if got := sampler.Sample(nil); got != 9007199254740993 {
		t.Fatalf("large integer rounded to %d", got)
	}

	if _, err := discreteweighted.New(definition); err == nil {
		t.Fatal("inexact float output admitted")
	}
}

func TestPreparedSamplerCopiesEntries(t *testing.T) {
	definition := testutil.Profile(t, `{
  "kind": "discreteWeighted",
  "unit": "bytes",
  "entries": [
    {
      "value": "4",
      "weight": "1"
    },
    {
      "value": "4",
      "weight": "2"
    }
  ]
}`)

	sampler, err := discreteweighted.NewInteger(definition)
	if err != nil {
		t.Fatal(err)
	}

	definition.Entries[0].Value = "99"
	definition.Entries[1].Weight = "0"

	rng := &testutil.Word{}
	if got := sampler.Sample(rng); got != 4 || rng.Draws != 0 {
		t.Fatalf("snapshot changed: value=%d draws=%d", got, rng.Draws)
	}
}
