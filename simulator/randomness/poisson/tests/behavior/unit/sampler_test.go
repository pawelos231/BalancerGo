package poisson_test

import (
	"testing"

	"load-balancer/randomness/internal/testutil"
	"load-balancer/randomness/poisson"
)

func TestConditionalMeanOneOracle(t *testing.T) {
	// Retaining 0, 1, and 2 normalizes their probability ratio of 1:1:0.5.
	definition := testutil.Profile(t, `{
  "kind": "poisson",
  "unit": "number",
  "min": "0",
  "max": "3",
  "mean": "1"
}`)

	sampler, err := poisson.NewInteger(definition)
	if err != nil {
		t.Fatal(err)
	}

	testutil.Frequencies(t, sampler, map[int64]float64{0: .4, 1: .4, 2: .2})
}

func TestLargeMeanDoesNotUnderflow(t *testing.T) {
	definition := testutil.Profile(t, `{
  "kind": "poisson",
  "unit": "number",
  "min": "10000",
  "max": "10003",
  "mean": "10000"
}`)

	sampler, err := poisson.NewInteger(definition)
	if err != nil {
		t.Fatal(err)
	}

	// Relative weights avoid computing the underflowing probability at zero.
	first := 1.0
	second := 10000.0 / 10001
	third := second * 10000 / 10002
	sum := first + second + third
	testutil.Frequencies(t, sampler, map[int64]float64{
		10000: first / sum,
		10001: second / sum,
		10002: third / sum,
	})
}

func TestCountAboveSafeIntegerPreserved(t *testing.T) {
	definition := testutil.Profile(t, `{
  "kind": "poisson",
  "unit": "number",
  "min": "9007199254740993",
  "max": "9007199254740994",
  "mean": "9007199254740993"
}`)

	sampler, err := poisson.NewInteger(definition)
	if err != nil {
		t.Fatal(err)
	}

	if got := sampler.Sample(nil); got != 9007199254740993 {
		t.Fatalf("count rounded to %d", got)
	}

	if _, err := poisson.New(definition); err == nil {
		t.Fatal("inexact float count accepted")
	}
}

func TestZeroMeanAndWidthAdmission(t *testing.T) {
	// A zero mean makes zero deterministic.
	definition := testutil.Profile(t, `{
  "kind": "poisson",
  "unit": "number",
  "min": "0",
  "max": "10",
  "mean": "0"
}`)

	sampler, err := poisson.NewInteger(definition)
	if err != nil {
		t.Fatal(err)
	}

	if got := sampler.Sample(nil); got != 0 {
		t.Fatal(got)
	}

	// The sampling table supports at most 65,536 counts.
	definition = testutil.Profile(t, `{
  "kind": "poisson",
  "unit": "number",
  "min": "0",
  "max": "65537",
  "mean": "1"
}`)

	if _, err := poisson.NewInteger(definition); err == nil {
		t.Fatal("excessive support width admitted")
	}

	// A positive lower bound excludes the only outcome of a zero-mean law.
	zero, one, two := "0", "1", "2"
	definition.Mean, definition.Min, definition.Max = &zero, &one, &two
	if _, err := poisson.NewInteger(definition); err == nil {
		t.Fatal("zero-mass truncation admitted")
	}
}
