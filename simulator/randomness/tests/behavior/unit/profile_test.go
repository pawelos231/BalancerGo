package randomness_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"load-balancer/randomness"
)

// Go and TypeScript consume the same raw documents, preserving duplicate field cases.
func TestProfileSharedValidationCases(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "..", "examples", "randomness", "profile-cases.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Name     string `json:"name"`
			Valid    bool   `json:"valid"`
			Document string `json:"document"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) == 0 {
		t.Fatal("shared profile corpus is empty")
	}
	for _, example := range corpus.Cases {
		t.Run(example.Name, func(t *testing.T) {
			decoded, err := randomness.DecodeProfile([]byte(example.Document))
			if !example.Valid {
				if err == nil {
					t.Fatal("invalid shared profile was accepted")
				}
				if !reflect.DeepEqual(decoded, randomness.Profile{}) {
					t.Fatalf("failed decode leaked a partial definition: %+v", decoded)
				}
				return
			}
			if err != nil {
				t.Fatalf("valid shared profile was rejected: %v", err)
			}
			if err := randomness.ValidateProfile(decoded); err != nil {
				t.Fatalf("decoded definition failed validation: %v", err)
			}
			encoded, err := randomness.EncodeProfile(decoded)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := randomness.DecodeProfile(encoded)
			if err != nil || !reflect.DeepEqual(restored, decoded) {
				t.Fatalf("profile changed during an exact JSON round trip: %+v, %v", restored, err)
			}
		})
	}
}

func TestProfileValidationAndEncodingRejectInvalidTypedDefinitions(t *testing.T) {
	text := func(value string) *string { return &value }
	cases := []struct {
		name    string
		profile randomness.Profile
	}{
		{"missing kind", randomness.Profile{Unit: randomness.UnitNumber, Value: text("1")}},
		{"unknown kind", randomness.Profile{Kind: "normal", Unit: randomness.UnitNumber, Value: text("1")}},
		{"missing unit", randomness.Profile{Kind: randomness.KindConstant, Value: text("1")}},
		{"unknown unit", randomness.Profile{Kind: randomness.KindConstant, Unit: "seconds", Value: text("1")}},
		{"missing value", randomness.Profile{Kind: randomness.KindConstant, Unit: randomness.UnitNumber}},
		{"constant min", randomness.Profile{Kind: randomness.KindConstant, Unit: randomness.UnitNumber, Value: text("0"), Min: text("0")}},
		{"constant max", randomness.Profile{Kind: randomness.KindConstant, Unit: randomness.UnitNumber, Value: text("0"), Max: text("1")}},
		{"uniform value", randomness.Profile{Kind: randomness.KindUniform, Unit: randomness.UnitNumber, Value: text("0"), Min: text("0"), Max: text("1")}},
		{"missing min", randomness.Profile{Kind: randomness.KindUniform, Unit: randomness.UnitNumber, Max: text("1")}},
		{"missing max", randomness.Profile{Kind: randomness.KindUniform, Unit: randomness.UnitNumber, Min: text("0")}},
		{"empty decimal", randomness.Profile{Kind: randomness.KindConstant, Unit: randomness.UnitNumber, Value: text("")}},
	}
	for _, example := range cases {
		t.Run(example.name, func(t *testing.T) {
			if err := randomness.ValidateProfile(example.profile); err == nil {
				t.Fatal("invalid typed profile was accepted")
			}
			if encoded, err := randomness.EncodeProfile(example.profile); err == nil || encoded != nil {
				t.Fatalf("invalid typed profile was encoded: %s, %v", encoded, err)
			}
		})
	}
}

func TestProfileRejectsNoncanonicalDecimalSpellings(t *testing.T) {
	for _, value := range []string{
		"+1", "-0", "00", "00.1", "0.0", "1.", ".1", "1..1", "1/2", "1E3",
		"NaN", "Infinity", " 1", "1 ", "1\n", "\u0661", "1_000",
	} {
		t.Run(value, func(t *testing.T) {
			profile := randomness.Profile{Kind: randomness.KindConstant, Unit: randomness.UnitNumber, Value: &value}
			if err := randomness.ValidateProfile(profile); err == nil {
				t.Fatalf("noncanonical decimal %q was accepted", value)
			}
		})
	}
}

func TestProfileStrictDecoderRejectsMalformedAndAmbiguousJSON(t *testing.T) {
	for _, document := range []string{
		``, `null`, `true`, `"profile"`, `{}`, `{`,
		`{"kind":"constant","unit":"number","value":"1"`,
		`{"kind":"constant","unit":"number","value":"1",}`,
		`{"kind":"constant","unit":"number","value":"1"} null`,
		`{"kind":"constant","unit":"number","value":"1"} x`,
		`{"kind":null,"unit":"number","value":"1"}`,
		`{"kind":1,"unit":"number","value":"1"}`,
		`{"kind":"constant","unit":null,"value":"1"}`,
		`{"kind":"uniform","unit":"number","min":null,"max":"1"}`,
		`{"kind":"uniform","unit":"number","min":"0","max":{}}`,
		`{"kind":"constant","unit":"number","value":"1","\u0076alue":"2"}`,
		`{"kind":"constant","kind":"uniform","unit":"number","min":"0","max":"1"}`,
	} {
		t.Run(document, func(t *testing.T) {
			if decoded, err := randomness.DecodeProfile([]byte(document)); err == nil || !reflect.DeepEqual(decoded, randomness.Profile{}) {
				t.Fatalf("invalid JSON escaped strict decoding: %+v, %v", decoded, err)
			}
		})
	}
}

func TestProfileDefinitionsKeepFractionsUntilTargetConversion(t *testing.T) {
	for _, document := range []string{
		`{"kind":"constant","unit":"nanoseconds","value":"0.125"}`,
		`{"kind":"uniform","unit":"bytes","min":"1.0000000000000000001","max":"1.0000000000000000002"}`,
		`{"kind":"uniform","unit":"nanoseconds","min":"9223372036854775806.999999999999999999","max":"9223372036854775807"}`,
	} {
		decoded, err := randomness.DecodeProfile([]byte(document))
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := randomness.EncodeProfile(decoded)
		if err != nil || string(encoded) != document {
			t.Fatalf("exact fractional definition changed: %s, %v", encoded, err)
		}
	}
}

func TestProfileDomainsApplyToEveryCanonicalUnit(t *testing.T) {
	for _, unit := range []randomness.Unit{
		randomness.UnitNanoseconds, randomness.UnitBytes, randomness.UnitBitsPerSecond, randomness.UnitNumber, randomness.UnitPercentage,
	} {
		t.Run(string(unit), func(t *testing.T) {
			maximum := "9223372036854775807"
			overflow := "9223372036854775808"
			if unit == randomness.UnitPercentage {
				maximum, overflow = "100", "100.000000000000000001"
			}
			valid := randomness.Profile{Kind: randomness.KindConstant, Unit: unit, Value: &maximum}
			if err := randomness.ValidateProfile(valid); err != nil {
				t.Fatalf("inclusive domain maximum was rejected: %v", err)
			}
			invalid := randomness.Profile{Kind: randomness.KindConstant, Unit: unit, Value: &overflow}
			if err := randomness.ValidateProfile(invalid); err == nil || !strings.Contains(err.Error(), "profile.value") {
				t.Fatalf("domain overflow was not identified: %v", err)
			}
		})
	}
}
