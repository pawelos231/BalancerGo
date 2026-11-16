package bernoulli_test

import (
	"encoding/binary"
	"math/big"
	"strings"
	"testing"

	"load-balancer/randomness/bernoulli"
	"load-balancer/randomness/internal/testutil"
)

func TestExactProbability(t *testing.T) {
	// A quarter of samples should be 1; the rest should be 0.
	definition := testutil.Profile(t, `{
  "kind": "bernoulli",
  "unit": "number",
  "probability": "0.25"
}`)

	sampler, err := bernoulli.NewInteger(definition)
	if err != nil {
		t.Fatal(err)
	}

	testutil.Frequencies(t, sampler, map[int64]float64{0: .75, 1: .25})
}

func TestDeterministicEndpointsDrawNothing(t *testing.T) {
	for _, p := range []string{"0", "1"} {
		definition := testutil.Profile(t, `{
  "kind": "bernoulli",
  "unit": "number",
  "probability": "`+p+`"
}`)

		sampler, err := bernoulli.NewInteger(definition)
		if err != nil {
			t.Fatal(err)
		}

		rng := &testutil.Word{}
		got := sampler.Sample(rng)
		want := int64(0)
		if p == "1" {
			want = 1
		}

		if got != want || rng.Draws != 0 {
			t.Fatalf("p=%s got=%d draws=%d", p, got, rng.Draws)
		}
	}
}

type words struct {
	values []uint64
	index  int
}

func (rng *words) Uint64() uint64 {
	value := rng.values[rng.index]
	rng.index++
	return value
}

func TestTinyProbabilityNotRoundedAway(t *testing.T) {
	p := "0." + strings.Repeat("0", 29) + "1"
	definition := testutil.Profile(t, `{
  "kind": "bernoulli",
  "unit": "number",
  "probability": "`+p+`"
}`)

	sampler, err := bernoulli.NewInteger(definition)
	if err != nil {
		t.Fatal(err)
	}

	// Selecting the final exact coordinate must produce the rare event, even
	// though its probability is much smaller than a 53-bit floating grid.
	denominator := new(big.Int).Exp(big.NewInt(10), big.NewInt(30), nil)
	coordinate := new(big.Int).Sub(denominator, big.NewInt(1))
	buffer := coordinate.FillBytes(make([]byte, 16))
	rng := &words{values: []uint64{
		binary.BigEndian.Uint64(buffer[:8]),
		binary.BigEndian.Uint64(buffer[8:]),
	}}

	if got := sampler.Sample(rng); got != 1 {
		t.Fatalf("tiny event vanished: %d", got)
	}
}
