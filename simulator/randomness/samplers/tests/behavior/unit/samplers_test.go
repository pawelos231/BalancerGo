package samplers_test

import (
	"encoding/json"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"load-balancer/randomness/internal/testutil"
	"load-balancer/randomness/profile"
	"load-balancer/randomness/samplers"
)

func TestFactoryPreparesEveryCanonicalFamily(t *testing.T) {
	one, zero, two := "1", "0", "2"
	t.Run("constant", func(t *testing.T) {
		checkFactoryReplay(t, profile.Profile{
			Kind: profile.KindConstant, Unit: profile.UnitNumber, Value: &one,
		})
	})
	t.Run("uniform", func(t *testing.T) {
		checkFactoryReplay(t, profile.Profile{
			Kind: profile.KindUniform, Unit: profile.UnitNumber, Min: &zero, Max: &two,
		})
	})

	// Reuse canonical definitions so new presets also exercise both factory paths.
	files, err := filepath.Glob("../../../../../../examples/randomness/families/*-cases.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("find canonical families: %v", err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var corpus struct {
			ExpectedProfiles map[string]json.RawMessage `json:"expectedProfiles"`
		}
		if err := json.Unmarshal(data, &corpus); err != nil {
			t.Fatal(err)
		}
		if len(corpus.ExpectedProfiles) == 0 {
			t.Fatalf("%s has no canonical profiles", file)
		}
		for name, document := range corpus.ExpectedProfiles {
			definition, err := profile.DecodeProfile(document)
			if err != nil {
				t.Fatal(err)
			}
			t.Run(string(definition.Kind)+"/"+name, func(t *testing.T) {
				checkFactoryReplay(t, definition)
			})
		}
	}
}

func checkFactoryReplay(t *testing.T, definition profile.Profile) {
	t.Helper()
	floating, err := samplers.NewFloat(definition)
	if err != nil {
		t.Fatal(err)
	}
	integer, err := samplers.NewInteger(definition)
	if err != nil {
		t.Fatal(err)
	}

	first, second := rand.New(rand.NewSource(31)), rand.New(rand.NewSource(31))
	for i := 0; i < 100; i++ {
		value := floating.Sample(first)
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value != floating.Sample(second) {
			t.Fatalf("invalid floating sample or replay: %g", value)
		}
		whole := integer.Sample(first)
		if whole < 0 || whole != integer.Sample(second) {
			t.Fatalf("invalid integer sample or replay: %d", whole)
		}
	}
}

func TestFactoryKeepsExactDiscreteIntegerPaths(t *testing.T) {
	one, large, upper := "1", "9007199254740993", "9007199254740994"
	cases := []struct {
		definition profile.Profile
		expected   int64
	}{
		{
			definition: profile.Profile{
				Kind: profile.KindBernoulli, Unit: profile.UnitNumber, Probability: &one,
			},
			expected: 1,
		},
		{
			definition: profile.Profile{
				Kind: profile.KindPoisson, Unit: profile.UnitNumber,
				Min: &large, Max: &upper, Mean: &large,
			},
			expected: 9007199254740993,
		},
		{
			definition: profile.Profile{
				Kind: profile.KindDiscreteWeighted, Unit: profile.UnitBytes,
				Entries: []profile.WeightedEntry{{Value: "9007199254740993.25", Weight: "1"}},
			},
			expected: 9007199254740993,
		},
		{
			definition: profile.Profile{
				Kind: profile.KindEmpirical, Unit: profile.UnitBytes,
				Observations: []string{"9007199254740993.25"},
			},
			expected: 9007199254740993,
		},
	}
	for _, example := range cases {
		t.Run(string(example.definition.Kind), func(t *testing.T) {
			sampler, err := samplers.NewInteger(example.definition)
			if err != nil {
				t.Fatal(err)
			}
			if got := sampler.Sample(nil); got != example.expected {
				t.Fatalf("integer path returned %d, want %d", got, example.expected)
			}
		})
	}
}

func TestFactoryFloorsContinuousSampleExactlyOnce(t *testing.T) {
	definition := testutil.Profile(t, `{
  "kind": "normal",
  "unit": "nanoseconds",
  "min": "1.25",
  "max": "9.5",
  "mean": "5",
  "stddev": "2"
}`)

	floating, err := samplers.NewFloat(definition)
	if err != nil {
		t.Fatal(err)
	}

	integer, err := samplers.NewInteger(definition)
	if err != nil {
		t.Fatal(err)
	}

	// Matching streams must produce the same value before whole-unit realization.
	first, second := rand.New(rand.NewSource(2)), rand.New(rand.NewSource(2))
	for i := 0; i < 1000; i++ {
		expected := int64(floating.Sample(first))
		if got := integer.Sample(second); got != expected {
			t.Fatalf("floor=%d want %d", got, expected)
		}
	}
}

func TestFactoryRetainsExactUniformIntegerPath(t *testing.T) {
	// Both bounds floor to the same integer above 2^53.
	definition := testutil.Profile(t, `{
  "kind": "uniform",
  "unit": "bytes",
  "min": "9007199254740993.1",
  "max": "9007199254740993.2"
}`)

	sampler, err := samplers.NewInteger(definition)
	if err != nil {
		t.Fatal(err)
	}

	if got := sampler.Sample(nil); got != 9007199254740993 {
		t.Fatalf("uniform value rounded to %d", got)
	}
}

func TestExtremeNumericalParametersAreRejectedOrFinite(t *testing.T) {
	tiny := "0." + strings.Repeat("0", 119) + "1"
	cases := []struct {
		name     string
		document string
	}{
		{
			name: "normal tiny stddev",
			document: `{
  "kind": "normal",
  "unit": "number",
  "min": "0",
  "max": "10",
  "mean": "5",
  "stddev": "` + tiny + `"
}`,
		},
		{
			name: "exponential tiny mean",
			document: `{
  "kind": "exponential",
  "unit": "number",
  "min": "0",
  "max": "10",
  "mean": "` + tiny + `"
}`,
		},
		{
			name: "gamma tiny shape",
			document: `{
  "kind": "gamma",
  "unit": "number",
  "min": "0",
  "max": "10",
  "shape": "` + tiny + `",
  "scale": "1"
}`,
		},
		{
			name: "gamma tiny scale",
			document: `{
  "kind": "gamma",
  "unit": "number",
  "min": "0",
  "max": "10",
  "shape": "2",
  "scale": "` + tiny + `"
}`,
		},
		{
			name: "gamma large shape",
			document: `{
  "kind": "gamma",
  "unit": "number",
  "min": "0",
  "max": "9223372036854775807",
  "shape": "9000000000000000000",
  "scale": "1"
}`,
		},
		{
			name: "student t tiny degrees of freedom",
			document: `{
  "kind": "studentT",
  "unit": "number",
  "min": "0",
  "max": "10",
  "location": "5",
  "scale": "1",
  "degreesOfFreedom": "` + tiny + `"
}`,
		},
		{
			name: "student t large degrees of freedom",
			document: `{
  "kind": "studentT",
  "unit": "number",
  "min": "0",
  "max": "10",
  "location": "5",
  "scale": "1",
  "degreesOfFreedom": "9000000000000000000"
}`,
		},
		{
			name: "beta tiny alpha",
			document: `{
  "kind": "beta",
  "unit": "number",
  "min": "0",
  "max": "1",
  "alpha": "` + tiny + `",
  "beta": "1"
}`,
		},
		{
			name: "beta large shapes",
			document: `{
  "kind": "beta",
  "unit": "number",
  "min": "0",
  "max": "1",
  "alpha": "9000000000000000000",
  "beta": "9000000000000000000"
}`,
		},
		{
			name: "lognormal tiny log stddev",
			document: `{
  "kind": "lognormal",
  "unit": "number",
  "min": "0",
  "max": "10",
  "median": "5",
  "logStddev": "` + tiny + `"
}`,
		},
		{
			name: "weibull tiny shape",
			document: `{
  "kind": "weibull",
  "unit": "number",
  "min": "0",
  "max": "10",
  "shape": "` + tiny + `",
  "scale": "1"
}`,
		},
	}

	for _, test := range cases {
		definition := testutil.Profile(t, test.document)
		t.Run(test.name, func(t *testing.T) {
			sampler, err := samplers.NewFloat(definition)
			if err != nil {
				return
			}

			rng := rand.New(rand.NewSource(4))
			for i := 0; i < 100; i++ {
				value := sampler.Sample(rng)
				if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
					t.Fatalf("invalid numerical sample %g", value)
				}
			}
		})
	}
}
