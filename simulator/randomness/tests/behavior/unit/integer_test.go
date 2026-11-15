package randomness_test

import (
	"encoding/binary"
	"math"
	"math/big"
	"reflect"
	"testing"

	"load-balancer/randomness"
)

// integerDraws replays scripted words and counts every draw.
type integerDraws struct {
	values []uint64
	calls  int
}

// Uint64 exposes unexpected draw consumption immediately.
func (draws *integerDraws) Uint64() uint64 {
	if draws.calls >= len(draws.values) {
		panic("integer sampler consumed an unexpected RNG draw")
	}
	value := draws.values[draws.calls]
	draws.calls++
	return value
}

// integerSampler compiles a standalone JSON profile for a test.
func integerSampler(t *testing.T, document string) *randomness.IntegerSampler {
	t.Helper()
	profile, err := randomness.DecodeProfile([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	sampler, err := randomness.NewIntegerSampler(profile)
	if err != nil {
		t.Fatal(err)
	}
	return sampler
}

// Uniform [10, 20) rejects excess words before taking a remainder.
func TestIntegerSamplerWholeBoundsAndModuloRejection(t *testing.T) {
	sampler := integerSampler(t, `{"kind":"uniform","unit":"number","min":"10","max":"20"}`)
	// Width ten leaves six excess uint64 values; these must be rejected.
	draws := &integerDraws{values: []uint64{0, 5, 6, 10, 19}}
	for _, want := range []int64{16, 10, 19} {
		if got := sampler.Sample(draws); got != want {
			t.Fatalf("sample = %d, want %d", got, want)
		}
	}
	if draws.calls != 5 {
		t.Fatalf("consumed %d draws, want 5 including two rejections", draws.calls)
	}
}

// Uniform [0.5, 2.5) floors to 0, 1, 1, 2 for four equally likely subintervals.
func TestIntegerSamplerPreservesExactFractionalBucketWeights(t *testing.T) {
	cases := []struct {
		name     string
		document string
		draws    []uint64
		want     []int64
	}{
		{
			name:     "quarter half quarter",
			document: `{"kind":"uniform","unit":"number","min":"0.5","max":"2.5"}`,
			draws:    []uint64{0, 1, 2, 3},
			want:     []int64{0, 1, 1, 2},
		},
		{
			name:     "unequal endpoint widths",
			document: `{"kind":"uniform","unit":"number","min":"0.25","max":"2.5"}`,
			draws:    []uint64{0, 1, 2, 3, 4, 5, 6, 7, 8},
			want:     []int64{0, 0, 0, 1, 1, 1, 1, 2, 2},
		},
		{
			name: "fraction above safe integers",
			document: `{
				"kind": "uniform",
				"unit": "bytes",
				"min": "9007199254740992.5",
				"max": "9007199254740994.5"
			}`,
			draws: []uint64{0, 1, 2, 3},
			want: []int64{
				9007199254740992,
				9007199254740993,
				9007199254740993,
				9007199254740994,
			},
		},
		{
			name: "fraction near int64 maximum",
			document: `{
				"kind": "uniform",
				"unit": "nanoseconds",
				"min": "9223372036854775805.5",
				"max": "9223372036854775807"
			}`,
			draws: []uint64{0, 1, 2},
			want:  []int64{math.MaxInt64 - 2, math.MaxInt64 - 1, math.MaxInt64 - 1},
		},
	}
	for _, example := range cases {
		t.Run(example.name, func(t *testing.T) {
			sampler := integerSampler(t, example.document)
			draws := &integerDraws{values: example.draws}
			for _, want := range example.want {
				if got := sampler.Sample(draws); got != want {
					t.Fatalf("sample = %d, want %d", got, want)
				}
			}
		})
	}
}

// Nine fractional subintervals reject candidates 9 through 15.
func TestIntegerSamplerFractionalRejection(t *testing.T) {
	sampler := integerSampler(t, `{"kind":"uniform","unit":"number","min":"0.25","max":"2.5"}`)
	// Nine exact subintervals need four bits; candidates nine through fifteen reject.
	draws := &integerDraws{values: []uint64{15, 9, 0, 8}}
	if got := sampler.Sample(draws); got != 0 || draws.calls != 3 {
		t.Fatalf("first sample = %d after %d draws, want 0 after 3", got, draws.calls)
	}
	if got := sampler.Sample(draws); got != 2 || draws.calls != 4 {
		t.Fatalf("last sample = %d after %d draws, want 2 after 4", got, draws.calls)
	}
}

// A tiny interval above 2 survives exact sampling across three uint64 chunks.
func TestIntegerSamplerUsesMultipleFixedUint64ChunksForPreciseFractions(t *testing.T) {
	sampler := integerSampler(t, `{
		"kind": "uniform",
		"unit": "number",
		"min": "0.000000000000000000000000000000000000001",
		"max": "2.000000000000000000000000000000000000001"
	}`)
	// The final exact subinterval alone floors to 2; float64 would erase it.
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(39), nil)
	width := new(big.Int).Mul(scale, big.NewInt(2))
	last := new(big.Int).Sub(width, big.NewInt(1))
	buffer := make([]byte, 24)
	last.FillBytes(buffer)
	draws := &integerDraws{values: []uint64{
		0, 0, 0,
		binary.BigEndian.Uint64(buffer[0:8]),
		binary.BigEndian.Uint64(buffer[8:16]),
		binary.BigEndian.Uint64(buffer[16:24]),
	}}
	if got := sampler.Sample(draws); got != 0 {
		t.Fatalf("first exact subinterval produced %d, want 0", got)
	}
	if got := sampler.Sample(draws); got != 2 {
		t.Fatalf("last exact subinterval produced %d, want 2", got)
	}
	if draws.calls != 6 {
		t.Fatalf("consumed %d uint64 chunks, want 6", draws.calls)
	}
}

// Adjacent large integers stay distinct, and the int64 upper bound stays excluded.
func TestIntegerSamplerKeepsWholeBoundsExactAtNativeLimits(t *testing.T) {
	cases := []struct {
		name     string
		document string
		draws    []uint64
		want     []int64
	}{
		{
			name: "above safe integer",
			document: `{
				"kind": "uniform",
				"unit": "bytes",
				"min": "9007199254740992",
				"max": "9007199254740994"
			}`,
			draws: []uint64{0, 1},
			want:  []int64{9007199254740992, 9007199254740993},
		},
		{
			name: "full nonnegative range",
			document: `{
				"kind": "uniform",
				"unit": "nanoseconds",
				"min": "0",
				"max": "9223372036854775807"
			}`,
			draws: []uint64{0, 1, math.MaxInt64 - 1},
			want:  []int64{math.MaxInt64 - 1},
		},
		{
			name: "near int64 maximum",
			document: `{
				"kind": "uniform",
				"unit": "bitsPerSecond",
				"min": "9223372036854775804",
				"max": "9223372036854775807"
			}`,
			draws: []uint64{0, 1, 2},
			want:  []int64{math.MaxInt64 - 2, math.MaxInt64 - 1},
		},
	}
	for _, example := range cases {
		t.Run(example.name, func(t *testing.T) {
			sampler := integerSampler(t, example.document)
			draws := &integerDraws{values: example.draws}
			for _, want := range example.want {
				if got := sampler.Sample(draws); got != want {
					t.Fatalf("sample from %s = %d, want %d", example.document, got, want)
				}
			}
			if draws.calls != len(example.draws) {
				t.Fatalf("consumed %d draws, want %d", draws.calls, len(example.draws))
			}
		})
	}
}

// Constants and ranges such as [0.125, 1) return their sole integer without RNG.
func TestIntegerSamplerDeterministicResultsConsumeNoRandomness(t *testing.T) {
	cases := []struct {
		document string
		want     int64
	}{
		{
			document: `{"kind":"constant","unit":"number","value":"0"}`,
			want:     0,
		},
		{
			document: `{"kind":"constant","unit":"percentage","value":"9.99"}`,
			want:     9,
		},
		{
			document: `{
				"kind": "constant",
				"unit": "bytes",
				"value": "9223372036854775807"
			}`,
			want: math.MaxInt64,
		},
		{
			document: `{"kind":"uniform","unit":"bytes","min":"0.125","max":"1"}`,
			want:     0,
		},
		{
			document: `{
				"kind": "uniform",
				"unit": "number",
				"min": "1.0000000000000000001",
				"max": "1.0000000000000000002"
			}`,
			want: 1,
		},
		{
			document: `{
				"kind": "uniform",
				"unit": "number",
				"min": "9007199254740992",
				"max": "9007199254740993"
			}`,
			want: 9007199254740992,
		},
	}
	for _, example := range cases {
		sampler := integerSampler(t, example.document)
		// Nil is sufficient because these definitions have only one integer realization.
		if got := sampler.Sample(nil); got != example.want {
			t.Fatalf("deterministic sample = %d, want %d", got, example.want)
		}
	}
}

// Invalid constants and uniform ranges fail before a sampler can draw anything.
func TestIntegerSamplerRejectsInvalidDefinitionsAtConstruction(t *testing.T) {
	text := func(value string) *string { return &value }
	for _, profile := range []randomness.Profile{
		{},
		{Kind: "normal", Unit: randomness.UnitNumber, Min: text("0"), Max: text("1")},
		{Kind: randomness.KindConstant, Unit: randomness.UnitNumber, Value: text("1.0")},
		{Kind: randomness.KindUniform, Unit: randomness.UnitNumber, Min: text("1"), Max: text("1")},
		{Kind: randomness.KindUniform, Unit: randomness.UnitNumber, Min: text("1")},
		{Kind: randomness.KindUniform, Unit: randomness.UnitNumber, Min: text("0"), Max: text("9223372036854775808")},
		{Kind: randomness.KindUniform, Unit: randomness.UnitPercentage, Min: text("0"), Max: text("100.1")},
	} {
		if sampler, err := randomness.NewIntegerSampler(profile); err == nil || sampler != nil {
			t.Fatalf("invalid definition produced sampler %+v, %v", sampler, err)
		}
	}
}

// Editing the source from [0.5, 2.5) to [100, 200) cannot change a compiled sampler.
func TestIntegerSamplerSnapshotsDefinitionAndDoesNotMutateIt(t *testing.T) {
	profile, err := randomness.DecodeProfile([]byte(`{"kind":"uniform","unit":"number","min":"0.5","max":"2.5"}`))
	if err != nil {
		t.Fatal(err)
	}
	original, _ := randomness.EncodeProfile(profile)
	sampler, err := randomness.NewIntegerSampler(profile)
	if err != nil {
		t.Fatal(err)
	}
	draws := &integerDraws{values: []uint64{0, 1, 2, 3}}
	if got := sampler.Sample(draws); got != 0 {
		t.Fatalf("first sample = %d, want 0", got)
	}
	if after, _ := randomness.EncodeProfile(profile); string(after) != string(original) {
		t.Fatalf("constructing or sampling changed the source profile: %s", after)
	}
	*profile.Min, *profile.Max = "100", "200"
	profile.Kind = randomness.KindConstant
	for _, want := range []int64{1, 1, 2} {
		if got := sampler.Sample(draws); got != want {
			t.Fatalf("source mutation changed compiled sample to %d, want %d", got, want)
		}
	}
}

// Drawing for backend b2 leaves backend b1's fixed-seed sample sequence unchanged.
func TestIntegerSamplerRepeatabilityAndResourceStreamIsolation(t *testing.T) {
	sampler := integerSampler(t, `{"kind":"uniform","unit":"nanoseconds","min":"0.5","max":"100.5"}`)
	first, second := randomness.NewRandomGenerators(-123), randomness.NewRandomGenerators(-123)
	firstStream := first.Stream("backend/b1", "processing")
	secondStream := second.Stream("backend/b1", "processing")
	unrelated := first.Stream("backend/b2", "processing")
	var actual, reference []int64
	for index := 0; index < 100; index++ {
		for draw := 0; draw < index; draw++ {
			unrelated.Uint64()
		}
		actual = append(actual, sampler.Sample(firstStream))
		reference = append(reference, sampler.Sample(secondStream))
	}
	if !reflect.DeepEqual(actual, reference) {
		t.Fatal("unrelated resource draws changed a fixed-seed sample sequence")
	}
	otherSeed := randomness.NewRandomGenerators(-124).Stream("backend/b1", "processing")
	var different []int64
	for range actual {
		different = append(different, sampler.Sample(otherSeed))
	}
	if reflect.DeepEqual(actual, different) {
		t.Fatal("different seeds produced the same entire sample sequence")
	}
}

// Whole intervals have equal buckets; [0.5, 2.5) has twice as much mass at 1.
func TestIntegerSamplerWholeAndFractionalFrequencies(t *testing.T) {
	cases := []struct {
		name      string
		document  string
		samples   int
		want      []int
		tolerance int
	}{
		{
			name:      "whole",
			document:  `{"kind":"uniform","unit":"number","min":"0","max":"10"}`,
			samples:   30000,
			want:      []int{3000, 3000, 3000, 3000, 3000, 3000, 3000, 3000, 3000, 3000},
			tolerance: 300,
		},
		{
			name:      "fractional",
			document:  `{"kind":"uniform","unit":"number","min":"0.5","max":"2.5"}`,
			samples:   40000,
			want:      []int{10000, 20000, 10000},
			tolerance: 700,
		},
	}
	for _, example := range cases {
		t.Run(example.name, func(t *testing.T) {
			sampler := integerSampler(t, example.document)
			stream := randomness.NewRandomGenerators(17).Stream("frequency-check", example.name)
			counts := make([]int, len(example.want))
			for draw := 0; draw < example.samples; draw++ {
				value := sampler.Sample(stream)
				if value < 0 || value >= int64(len(counts)) {
					t.Fatalf("sample %d escaped the admitted support", value)
				}
				counts[value]++
			}
			for value, want := range example.want {
				if got := counts[value]; got < want-example.tolerance || got > want+example.tolerance {
					t.Fatalf("count[%d] = %d, want %d within %d; counts = %v", value, got, want, example.tolerance, counts)
				}
			}
		})
	}
}
