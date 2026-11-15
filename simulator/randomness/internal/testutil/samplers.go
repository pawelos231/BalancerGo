// Package testutil supplies independent distribution checks shared by family tests.
package testutil

import (
	"math"
	"math/rand"
	"sort"
	"testing"

	"load-balancer/randomness/profile"
	"load-balancer/randomness/source"
)

func Profile(t *testing.T, document string) profile.Profile {
	t.Helper()
	value, err := profile.DecodeProfile([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

type Word struct {
	Value uint64
	Draws int
}

func (word *Word) Uint64() uint64 {
	word.Draws++
	return word.Value
}

// Continuous checks an analytic CDF oracle, bounds, seeded replay, and both RNG
// extremes. The oracle is deliberately supplied independently of the sampler.
func Continuous(t *testing.T, constructor func(profile.Profile) (source.FloatSampler, error), document string, cdf func(float64) float64) {
	t.Helper()
	definition := Profile(t, document)
	sampler, err := constructor(definition)
	if err != nil {
		t.Fatal(err)
	}
	lowerExact, _ := profile.ParseDecimal(*definition.Min)
	upperExact, _ := profile.ParseDecimal(*definition.Max)
	lower, _ := lowerExact.Float64()
	upper, _ := upperExact.Float64()
	lowerCDF, upperCDF := cdf(lower), cdf(upper)
	mass := upperCDF - lowerCDF
	if mass <= 0 || math.IsNaN(mass) {
		t.Fatal("invalid independent CDF oracle")
	}
	first, second := rand.New(rand.NewSource(921)), rand.New(rand.NewSource(921))
	const count = 12000
	transformed := make([]float64, count)
	for i := range transformed {
		value := sampler.Sample(first)
		replayed := sampler.Sample(second)
		if value != replayed {
			t.Fatal("seeded replay differs")
		}
		if math.IsNaN(value) || math.IsInf(value, 0) || value < lower || value >= upper {
			t.Fatalf("sample %g outside [%g,%g)", value, lower, upper)
		}
		transformed[i] = (cdf(value) - lowerCDF) / mass
	}
	sort.Float64s(transformed)
	distance := 0.0
	for i, value := range transformed {
		distance = math.Max(distance, math.Abs(value-float64(i)/count))
		distance = math.Max(distance, math.Abs(value-float64(i+1)/count))
	}
	if distance > 0.02 {
		t.Fatalf("conditional CDF distance %g exceeds .02", distance)
	}
	for _, word := range []uint64{0, ^uint64(0)} {
		value := sampler.Sample(&Word{Value: word})
		if value < lower || value >= upper || math.IsNaN(value) {
			t.Fatalf("extreme RNG sample %g outside support", value)
		}
	}
}

func Frequencies(t *testing.T, sampler source.IntegerSampler, expected map[int64]float64) {
	t.Helper()
	rng := rand.New(rand.NewSource(793))
	const count = 30000
	actual := make(map[int64]int)
	for i := 0; i < count; i++ {
		actual[sampler.Sample(rng)]++
	}
	for value, total := range actual {
		probability, exists := expected[value]
		if !exists {
			t.Fatalf("unexpected value %d", value)
		}
		if math.Abs(float64(total)/count-probability) > 0.015 {
			t.Fatalf("value %d probability %g; want %g", value, float64(total)/count, probability)
		}
	}
	for value, probability := range expected {
		if probability > 0.02 && actual[value] == 0 {
			t.Fatalf("missing value %d", value)
		}
	}
}
