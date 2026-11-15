package scenario_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"load-balancer/randomness"
	"load-balancer/scenario"
)

// Keep raw profile JSON intact so duplicate keys and numeric parameters reach the codec.
func scenarioWithRawRandomProfiles(t *testing.T, registry string) []byte {
	t.Helper()
	document, err := scenario.EncodeDocument(exampleScenario())
	if err != nil {
		t.Fatal(err)
	}
	opening := []byte(`"fixed":{`)
	if !bytes.Contains(document, opening) {
		t.Fatal("scenario document has no fixed object")
	}
	return bytes.Replace(document, opening, []byte(`"fixed":{"randomProfiles":`+registry+`,`), 1)
}

func TestScenarioRandomProfilesSharedValidationCases(t *testing.T) {
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
			document := scenarioWithRawRandomProfiles(t, `{"definition":`+example.Document+`}`)
			decoded, err := scenario.DecodeDocument(document)
			if !example.Valid {
				if err == nil {
					t.Fatal("invalid shared profile was accepted inside a scenario")
				}
				if !reflect.DeepEqual(decoded, scenario.Scenario{}) {
					t.Fatal("failed decode returned a partial scenario")
				}
				return
			}
			if err != nil {
				t.Fatalf("valid shared profile was rejected: %v", err)
			}
			if _, err := scenario.Compile(decoded); err != nil {
				t.Fatalf("valid profile failed validation before Start: %v", err)
			}
			encoded, err := scenario.EncodeDocument(decoded)
			if err != nil {
				t.Fatal(err)
			}
			restored, err := scenario.DecodeDocument(encoded)
			if err != nil || !reflect.DeepEqual(decoded.Fixed.RandomProfiles, restored.Fixed.RandomProfiles) {
				t.Fatalf("profile definition changed during scenario round trip: %v", err)
			}
		})
	}
}

func TestScenarioRandomProfileRegistryRejectsMalformedJSON(t *testing.T) {
	const valid = `{"kind":"uniform","unit":"nanoseconds","min":"0.5","max":"1.5"}`
	cases := []struct {
		name     string
		registry string
		path     string
	}{
		{"null registry", `null`, `scenario.fixed.randomProfiles`},
		{"array registry", `[]`, `scenario.fixed.randomProfiles`},
		{"numeric registry", `1`, `scenario.fixed.randomProfiles`},
		{"duplicate name", `{"cpu":` + valid + `,"cpu":` + valid + `}`, `scenario.fixed.randomProfiles["cpu"]`},
		{"escaped duplicate name", `{"cpu":` + valid + `,"\u0063pu":` + valid + `}`, `scenario.fixed.randomProfiles["cpu"]`},
		{"null definition", `{"cpu":null}`, `scenario.fixed.randomProfiles["cpu"]`},
		{"missing kind", `{"cpu":{"unit":"number","value":"1"}}`, `scenario.fixed.randomProfiles["cpu"].kind`},
		{"missing unit", `{"cpu":{"kind":"constant","value":"1"}}`, `scenario.fixed.randomProfiles["cpu"].unit`},
		{"null forbidden min", `{"cpu":{"kind":"constant","unit":"number","value":"1","min":null}}`, `scenario.fixed.randomProfiles["cpu"].min`},
		{"null forbidden value", `{"cpu":{"kind":"uniform","unit":"number","min":"1","max":"2","value":null}}`, `scenario.fixed.randomProfiles["cpu"].value`},
		{"unknown parameter", `{"cpu":{"kind":"constant","unit":"number","value":"1","reference":"other"}}`, `scenario.fixed.randomProfiles["cpu"].reference`},
		{"duplicate parameter", `{"cpu":{"kind":"constant","unit":"number","value":"1","value":"2"}}`, `scenario.fixed.randomProfiles["cpu"].value`},
		{"truncated definition", `{"cpu":{"kind":"uniform"`, `scenario.fixed.randomProfiles["cpu"]`},
	}
	for _, example := range cases {
		t.Run(example.name, func(t *testing.T) {
			_, err := scenario.DecodeDocument(scenarioWithRawRandomProfiles(t, example.registry))
			if err == nil || !strings.Contains(err.Error(), example.path) {
				t.Fatalf("expected error at %s, got %v", example.path, err)
			}
		})
	}
}

func TestScenarioRandomProfilesValidateNamesAndTypedDefinitions(t *testing.T) {
	valid := randomness.Profile{
		Kind: randomness.KindUniform, Unit: randomness.UnitNanoseconds,
		Min: pointer("0.5"), Max: pointer("1.5"),
	}
	for _, name := range []string{"", "space name", "profilé", strings.Repeat("a", 129)} {
		t.Run("invalid name "+name, func(t *testing.T) {
			input := exampleScenario()
			input.Fixed.RandomProfiles = map[string]randomness.Profile{name: valid}
			if _, err := scenario.Normalize(input); err == nil || !strings.Contains(err.Error(), "fixed.randomProfiles[") {
				t.Fatalf("invalid profile name passed normalization: %v", err)
			}
		})
	}
	cases := []struct {
		name    string
		profile randomness.Profile
	}{
		{"missing kind", randomness.Profile{Unit: randomness.UnitNumber, Value: pointer("1")}},
		{"unknown unit", randomness.Profile{Kind: randomness.KindConstant, Unit: "seconds", Value: pointer("1")}},
		{"missing bound", randomness.Profile{Kind: randomness.KindUniform, Unit: randomness.UnitNumber, Min: pointer("0")}},
		{"forbidden parameter", randomness.Profile{Kind: randomness.KindConstant, Unit: randomness.UnitNumber, Value: pointer("1"), Min: pointer("0")}},
		{"equal bounds", randomness.Profile{Kind: randomness.KindUniform, Unit: randomness.UnitNumber, Min: pointer("1"), Max: pointer("1")}},
	}
	for _, example := range cases {
		t.Run(example.name, func(t *testing.T) {
			input := exampleScenario()
			input.Fixed.RandomProfiles = map[string]randomness.Profile{"unused": example.profile}
			if _, err := scenario.Normalize(input); err == nil || !strings.Contains(err.Error(), `fixed.randomProfiles["unused"]`) {
				t.Fatalf("invalid typed definition passed normalization: %v", err)
			}
			if _, err := scenario.EncodeDocument(input); err == nil || !strings.Contains(err.Error(), `scenario.fixed.randomProfiles["unused"]`) {
				t.Fatalf("invalid typed definition passed encoding: %v", err)
			}
		})
	}
}

func TestScenarioRandomProfilesRemainUnboundAndPreserveExactDefinitions(t *testing.T) {
	input := exampleScenario()
	baseline, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Fixed.RandomProfiles = map[string]randomness.Profile{
		"fraction": {Kind: randomness.KindUniform, Unit: randomness.UnitBytes,
			Min: pointer("1.0000000000000000001"), Max: pointer("1.0000000000000000002")},
		"unsafe": {Kind: randomness.KindUniform, Unit: randomness.UnitNanoseconds,
			Min: pointer("9007199254740992.1"), Max: pointer("9007199254740992.2")},
		"constant": {Kind: randomness.KindConstant, Unit: randomness.UnitBitsPerSecond,
			Value: pointer("9223372036854775807")},
	}
	before, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolved, baseline) {
		t.Fatal("unbound definitions changed normalized runtime settings")
	}
	if _, err := scenario.Compile(input); err != nil {
		t.Fatal(err)
	}
	after, err := scenario.EncodeDocument(input)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("validation changed original profile definitions: %v", err)
	}
	restored, err := scenario.DecodeDocument(after)
	if err != nil || !reflect.DeepEqual(restored.Fixed.RandomProfiles, input.Fixed.RandomProfiles) {
		t.Fatalf("scenario export changed exact bounds: %v", err)
	}
	*input.Fixed.RandomProfiles["fraction"].Min = "0"
	if *restored.Fixed.RandomProfiles["fraction"].Min != "1.0000000000000000001" {
		t.Fatal("decoded profiles share storage with the source definition")
	}
}

func TestScenarioRandomProfilesStayOptional(t *testing.T) {
	input := exampleScenario()
	for _, profiles := range []map[string]randomness.Profile{nil, {}} {
		input.Fixed.RandomProfiles = profiles
		document, err := scenario.EncodeDocument(input)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(document, []byte(`"randomProfiles"`)) {
			t.Fatal("an empty random profile registry should be omitted")
		}
		if _, err := scenario.DecodeDocument(document); err != nil {
			t.Fatal(err)
		}
	}
}

// This native fixture is generated from the same library source checked by the compiler.
func TestUniformScenarioFixturePassesNativeAdmissionWithoutBindingProfiles(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "..", "examples", "randomness", "uniform-scenario.json")
	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	input, err := scenario.DecodeDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	precise := input.Fixed.RandomProfiles["precise-bytes"]
	if precise.Min == nil || precise.Max == nil || *precise.Min != "9007199254740992.125" || *precise.Max != "9007199254740992.375" {
		t.Fatal("the shared fixture lost its exact fractional bounds above the safe integer range")
	}
	resolved, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := scenario.Compile(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.States) != 1 || len(resolved.States[0].Backends) != 1 || len(resolved.States[0].ClientGroups) != 1 {
		t.Fatal("the shared fixture must include one initial backend and client group")
	}
	state, err := plan.StateAt(0)
	if err != nil || !reflect.DeepEqual(state, resolved.States[0]) {
		t.Fatalf("compiled fixture changed the normalized initial state: %v", err)
	}
	if state.LoadBalancer.Host.PacketProcessingTime != 0 || state.Backends[0].Host.PacketProcessingTime != 0 {
		t.Fatal("the unbound CPU profile changed the scalar CPU defaults")
	}
	if resolved.Fixed.NetworkDefaults.PacketTravelTime != time.Millisecond {
		t.Fatal("the unbound travel profile changed the scalar link default")
	}
	for name, links := range map[string]scenario.DirectionalLinks{
		"backend": state.Backends[0].Network,
		"clients": state.ClientGroups[0].Network,
	} {
		if links.ToLoadBalancer.PacketTravelTime != time.Millisecond || links.FromLoadBalancer.PacketTravelTime != time.Millisecond {
			t.Fatalf("the unbound travel profile changed the %s scalar links", name)
		}
	}
	encoded, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := scenario.DecodeDocument(encoded)
	if err != nil || !reflect.DeepEqual(restored.Fixed.RandomProfiles, input.Fixed.RandomProfiles) {
		t.Fatalf("native fixture export changed exact profile definitions: %v", err)
	}
}
