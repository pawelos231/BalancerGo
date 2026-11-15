package randomness_test

import (
	"math"
	"math/big"
	"testing"

	"load-balancer/randomness"
)

// Predetermined draws make boundary and rounding behavior visible without a seed search.
type floatDraws struct {
	values []uint64
	used   int
}

func (source *floatDraws) Uint64() uint64 {
	value := source.values[source.used]
	source.used++
	return value
}

func floatProfile(minimum, maximum string) randomness.Profile {
	return randomness.Profile{
		Kind: randomness.KindUniform,
		Unit: randomness.UnitNumber,
		Min:  &minimum,
		Max:  &maximum,
	}
}

func floatSampler(t *testing.T, minimum, maximum string) *randomness.UniformFloatSampler {
	t.Helper()
	sampler, err := randomness.NewUniformFloatSampler(floatProfile(minimum, maximum))
	if err != nil {
		t.Fatal(err)
	}
	return sampler
}

// [2,6) includes 2 and 4, but even the largest random fraction must stay below 6.
func TestUniformFloatUsesHalfOpenBounds(t *testing.T) {
	sampler := floatSampler(t, "2", "6")
	source := &floatDraws{values: []uint64{0, 1 << 63, math.MaxUint64}}
	if got := sampler.Sample(source); got != 2 {
		t.Fatalf("first value = %g, want 2", got)
	}
	if got := sampler.Sample(source); got != 4 {
		t.Fatalf("midpoint = %g, want 4", got)
	}
	if got := sampler.Sample(source); got < 2 || got >= 6 {
		t.Fatalf("last value = %g, outside [2,6)", got)
	}
}

// Near 2^53 the last interpolation can round up; return the last value below max.
func TestUniformFloatExcludesUpperBoundAfterRounding(t *testing.T) {
	sampler := floatSampler(t, "9007199254740992", "9007199254740996")
	source := &floatDraws{values: []uint64{math.MaxUint64}}
	if got := sampler.Sample(source); got != 9007199254740994 {
		t.Fatalf("rounded sample = %.0f, want 9007199254740994", got)
	}
}

// 0.1 and 0.2 are not exact binary fractions. Compare returned values to exact decimals.
func TestUniformFloatStaysInsideExactDecimalBounds(t *testing.T) {
	sampler := floatSampler(t, "0.1", "0.2")
	minimum, _ := new(big.Rat).SetString("0.1")
	maximum, _ := new(big.Rat).SetString("0.2")
	source := &floatDraws{values: []uint64{0, 1 << 63, math.MaxUint64}}
	for range source.values {
		value := sampler.Sample(source)
		exact := new(big.Rat).SetFloat64(value)
		if exact.Cmp(minimum) < 0 || exact.Cmp(maximum) >= 0 {
			t.Fatalf("%.18g lies outside exact [0.1,0.2)", value)
		}
	}
}

// Both decimals lie between adjacent float64 values; fail preparation before any draw.
func TestUniformFloatRejectsIntervalWithoutRepresentableValue(t *testing.T) {
	profile := floatProfile("9007199254740992.1", "9007199254740992.2")
	if err := randomness.ValidateProfile(profile); err != nil {
		t.Fatalf("definition itself should be valid: %v", err)
	}
	if sampler, err := randomness.NewUniformFloatSampler(profile); err == nil || sampler != nil {
		t.Fatal("an empty floating support passed preparation")
	}
}

// This interval contains exactly one float64, so sampling needs no random source.
func TestUniformFloatSingleResultUsesNoDraw(t *testing.T) {
	sampler := floatSampler(t, "9007199254740992", "9007199254740994")
	if got := sampler.Sample(nil); got != 9007199254740992 {
		t.Fatalf("single result = %.0f", got)
	}
}

// Changing the input strings after preparation must not change the compiled sampler.
func TestUniformFloatCopiesProfileBounds(t *testing.T) {
	profile := floatProfile("2", "6")
	sampler, err := randomness.NewUniformFloatSampler(profile)
	if err != nil {
		t.Fatal(err)
	}
	*profile.Min, *profile.Max = "20", "60"
	if got := sampler.Sample(&floatDraws{values: []uint64{0}}); got != 2 {
		t.Fatalf("input mutation changed the sampler to %g", got)
	}
}

// A fixed stream replays the same samples even if another resource draws between them.
func TestUniformFloatReplaysWithoutConsumingOtherStreams(t *testing.T) {
	sampler := floatSampler(t, "0", "10")
	first, second := randomness.NewRandomGenerators(2026), randomness.NewRandomGenerators(2026)
	bins := [10]int{}
	for range 10_000 {
		second.Stream("other", "travel").Uint64()
		value := sampler.Sample(first.Stream("client/a", "travel"))
		if replay := sampler.Sample(second.Stream("client/a", "travel")); replay != value {
			t.Fatal("another stream changed uniform samples")
		}
		bins[int(value)]++
	}
	// Ten equal-width bins should each receive about 1000 samples, with a broad tolerance.
	for bin, count := range bins {
		if count < 800 || count > 1200 {
			t.Fatalf("bin %d has %d samples, want about 1000", bin, count)
		}
	}
}

func TestUniformFloatRejectsInvalidOrNonUniformProfiles(t *testing.T) {
	constant := "1"
	for _, profile := range []randomness.Profile{
		{},
		floatProfile("2", "1"),
		floatProfile("0", "1e3"),
		{Kind: randomness.KindConstant, Unit: randomness.UnitNumber, Value: &constant},
	} {
		if sampler, err := randomness.NewUniformFloatSampler(profile); err == nil || sampler != nil {
			t.Fatalf("invalid or non-uniform profile accepted: %+v", profile)
		}
	}
}
