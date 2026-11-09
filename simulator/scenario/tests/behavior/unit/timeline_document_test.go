package scenario_test

import (
	"encoding/json"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"load-balancer/scenario"
)

// TestDocumentEmptyShape keeps format parsing separate from timeline validation.
func TestDocumentEmptyShape(t *testing.T) {
	input, err := scenario.DecodeDocument([]byte(`{"scenarioVersion":2,"fixed":{},"states":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if input.ScenarioVersion != scenario.CurrentScenarioVersion || input.States == nil || len(input.States) != 0 {
		t.Fatalf("empty state object changed shape: %+v", input)
	}

	omitted, err := scenario.DecodeDocument([]byte(`{"scenarioVersion":2}`))
	if err != nil || !reflect.DeepEqual(omitted, scenario.Scenario{ScenarioVersion: scenario.CurrentScenarioVersion}) {
		t.Fatalf("ordinary missing fields should retain zero values: %+v, %v", omitted, err)
	}
	encoded, err := scenario.EncodeDocument(omitted)
	if err != nil {
		t.Fatal(err)
	}
	object := documentObject(t, encoded)
	if states, ok := object["states"].(map[string]any); !ok || len(states) != 0 {
		t.Fatalf("nil states should encode as {}, got %s", encoded)
	}
	if _, err := scenario.DecodeDocument(encoded); err != nil {
		t.Fatalf("canonical empty document should decode: %v", err)
	}
}

// TestDocumentVersion requires the current scenario version in both directions.
func TestDocumentVersion(t *testing.T) {
	for _, version := range []int{0, 1, scenario.CurrentScenarioVersion + 1} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			data := []byte(`{"scenarioVersion":` + strconv.Itoa(version) + `}`)
			decoded, err := scenario.DecodeDocument(data)
			if err == nil || !strings.Contains(err.Error(), "scenario.scenarioVersion: expected version 2") {
				t.Fatalf("expected a version error, got %v", err)
			}
			if !reflect.DeepEqual(decoded, scenario.Scenario{}) {
				t.Fatal("failed decoding must not return a partial document")
			}
			encoded, err := scenario.EncodeDocument(scenario.Scenario{ScenarioVersion: version})
			if err == nil || encoded != nil {
				t.Fatalf("invalid version must not encode: %s, %v", encoded, err)
			}
		})
	}
	if _, err := scenario.DecodeDocument([]byte(`{}`)); err == nil {
		t.Fatal("an omitted scenarioVersion must not imply the current format")
	}
	if input, err := scenario.DecodeDocument([]byte(`{"scenarioVersion":"2"}`)); err != nil || input.ScenarioVersion != scenario.CurrentScenarioVersion {
		t.Fatalf("quoted exact versions should follow the existing integer rules: %+v, %v", input, err)
	}
}

func TestDocumentRejectsLegacyVersionField(t *testing.T) {
	for _, document := range []string{
		`{"version":1}`,
		`{"version":2}`,
		`{"scenarioVersion":2,"version":2}`,
	} {
		decoded, err := scenario.DecodeDocument([]byte(document))
		if err == nil || !strings.Contains(err.Error(), "scenario.version: unknown field") {
			t.Fatalf("legacy version field accepted in %s: %v", document, err)
		}
		if !reflect.DeepEqual(decoded, scenario.Scenario{}) {
			t.Fatal("rejected legacy input returned a partial document")
		}
	}
}

// TestDocumentTimestampKeys retains exact keys beyond browser-safe integers.
func TestDocumentTimestampKeys(t *testing.T) {
	data := []byte(`{"scenarioVersion":2,"states":{"0":{},"9007199254740993":{},"9223372036854775807":{}}}`)
	input, err := scenario.DecodeDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	states := documentObject(t, encoded)["states"].(map[string]any)
	for _, key := range []string{"0", "9007199254740993", "9223372036854775807"} {
		if _, present := input.States[key]; !present {
			t.Fatalf("decoded timestamp %q lost precision", key)
		}
		if _, present := states[key]; !present {
			t.Fatalf("encoded timestamp %q lost precision", key)
		}
	}

	// A missing t=0 and a timestamp beyond the zero duration need later validation.
	if _, err := scenario.DecodeDocument([]byte(`{"scenarioVersion":2,"states":{"1":{}}}`)); err != nil {
		t.Fatalf("the document reader must not enforce timeline relationships: %v", err)
	}
}

// TestDocumentRejectsNoncanonicalTimestamps checks syntax in Go maps and JSON.
func TestDocumentRejectsNoncanonicalTimestamps(t *testing.T) {
	keys := []string{"", "-1", "-0", "+0", "00", "01", " 1", "1 ", "1.0", "1e3", "١", "9223372036854775808"}
	for _, key := range keys {
		t.Run(strconv.Quote(key), func(t *testing.T) {
			quoted, err := json.Marshal(key)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := scenario.DecodeDocument([]byte(`{"scenarioVersion":2,"states":{` + string(quoted) + `:{}}}`))
			if err == nil || !strings.Contains(err.Error(), "canonical nonnegative int64 nanosecond timestamp") {
				t.Fatalf("expected a timestamp error for %q, got %v", key, err)
			}
			if !reflect.DeepEqual(decoded, scenario.Scenario{}) {
				t.Fatal("failed decoding must not return partial states")
			}
			input := scenario.Scenario{ScenarioVersion: scenario.CurrentScenarioVersion, States: map[string]scenario.StateConfig{key: {}}}
			encoded, err := scenario.EncodeDocument(input)
			if err == nil || encoded != nil {
				t.Fatalf("invalid Go-map key must not encode: %s, %v", encoded, err)
			}
		})
	}
}

// TestDocumentDuplicateKeys rejects aliases before reading an invalid replacement.
func TestDocumentDuplicateKeys(t *testing.T) {
	cases := []struct {
		name string
		data string
		path string
	}{
		{"state", `{"scenarioVersion":2,"states":{"0":{},"0":null}}`, `scenario.states["0"]: duplicate key`},
		{"escaped state", `{"scenarioVersion":2,"states":{"0":{},"\u0030":null}}`, `scenario.states["0"]: duplicate key`},
		{"root", `{"scenarioVersion":2,"scenarioVer\u0073ion":null}`, "scenario.scenarioVersion: duplicate field"},
		{"state field", `{"scenarioVersion":2,"states":{"0":{"resources":{},"resources":null}}}`, `scenario.states["0"].resources: duplicate field`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := scenario.DecodeDocument([]byte(test.data))
			if err == nil || !strings.Contains(err.Error(), test.path) {
				t.Fatalf("expected %q, got %v", test.path, err)
			}
		})
	}
}

// TestDocumentStrictShape applies the existing object and collection rules to maps.
func TestDocumentStrictShape(t *testing.T) {
	cases := []struct {
		name string
		data string
		path string
	}{
		{"null root", `null`, "scenario: must not be null"},
		{"null fixed", `{"scenarioVersion":2,"fixed":null}`, "scenario.fixed: must not be null"},
		{"null states", `{"scenarioVersion":2,"states":null}`, "scenario.states: must not be null"},
		{"states array", `{"scenarioVersion":2,"states":[]}`, "scenario.states: expected an object"},
		{"null state", `{"scenarioVersion":2,"states":{"0":null}}`, `scenario.states["0"]: must not be null`},
		{"scalar state", `{"scenarioVersion":2,"states":{"0":1}}`, `scenario.states["0"]: expected an object`},
		{"unknown root", `{"scenarioVersion":2,"execution":{}}`, "scenario.execution: unknown field"},
		{"unknown fixed", `{"scenarioVersion":2,"fixed":{"extra":1}}`, "scenario.fixed.extra: unknown field"},
		{"unknown state", `{"scenarioVersion":2,"states":{"0":{"extra":1}}}`, `scenario.states["0"].extra: unknown field`},
		{"truncated map", `{"scenarioVersion":2,"states":{"0":{}`, "scenario.states:"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input, err := scenario.DecodeDocument([]byte(test.data))
			if err == nil || !strings.Contains(err.Error(), test.path) {
				t.Fatalf("expected %q, got %v", test.path, err)
			}
			if !reflect.DeepEqual(input, scenario.Scenario{}) {
				t.Fatal("failed decoding must return a zero document")
			}
		})
	}
}

// TestDocumentTrailingTokens accepts whitespace after exactly one JSON document.
func TestDocumentTrailingTokens(t *testing.T) {
	for _, suffix := range []string{` {}`, ` true`, ` x`} {
		_, err := scenario.DecodeDocument([]byte(`{"scenarioVersion":2}` + suffix))
		if err == nil || !strings.Contains(err.Error(), "trailing JSON") {
			t.Fatalf("expected a trailing-token error for %q, got %v", suffix, err)
		}
	}
	if _, err := scenario.DecodeDocument([]byte("{\"scenarioVersion\":2} \n\t")); err != nil {
		t.Fatalf("trailing whitespace should decode: %v", err)
	}
}

// TestDocumentExactValuesRoundTrip covers fixed values and nested state patches.
func TestDocumentExactValuesRoundTrip(t *testing.T) {
	data := []byte(`{
		"scenarioVersion": 2,
		"fixed": {
			"execution": {
				"durationNanoseconds": "9223372036854775807",
				"snapshotIntervalNanoseconds": "9007199254740993",
				"seed": "-9223372036854775808",
				"maxEvents": "18446744073709551615",
				"speed": 0.25
			},
			"tcp": {"receiveBufferBytes": 0, "ECNenabled": false},
			"loadBalancer": {"id": "001"},
			"network": {"profiles": [{"id": "001", "config": {"queueCapacity": 0}}]},
			"workload": {"defaults": {"bytes": -1, "packetGapNanoseconds": "-9223372036854775808"}}
		},
		"states": {
			"0": {
				"resources": {
					"loadBalancer": {
						"forwardingBitsPerSecond": "9007199254740995",
						"mappingTimeouts": {"halfOpenIdleTimeoutNanoseconds": 0}
					},
					"backends": [{
						"id": "001", "port": "65535",
						"physicalAvailable": false, "administrativeState": "drain",
						"network": {"profile": "missing", "toLoadBalancer": {"queueCapacity": null}},
						"host": {"packetProcessingTimeNanoseconds": "9223372036854775807"}
					}]
				},
				"workload": {"clientGroups": [{
					"id": "001", "count": "1", "stream": {"bytes": 0}
				}]}
			}
		}
	}`)
	input, err := scenario.DecodeDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	if input.Fixed.Execution.Duration != time.Duration(math.MaxInt64) || input.Fixed.Execution.MaxEvents != math.MaxUint64 {
		t.Fatal("fixed native integer limits lost precision")
	}
	if input.Fixed.Execution.Seed != math.MinInt64 || input.Fixed.Execution.SnapshotInterval != 1<<53+1 {
		t.Fatal("signed and browser-unsafe values changed")
	}
	state := input.States["0"]
	backend := state.Resources.Backends[0]
	if backend.ID != "001" || backend.Port != math.MaxUint16 || input.Fixed.LoadBalancer.ID != "001" {
		t.Fatal("identifiers and quoted safe ports changed")
	}
	if backend.PhysicalAvailable == nil || *backend.PhysicalAvailable {
		t.Fatal("explicit physical unavailability must retain its false pointer")
	}
	if backend.AdministrativeState == nil || *backend.AdministrativeState != scenario.BackendAdminDrain {
		t.Fatal("administrative state must retain its pointer value")
	}
	if backend.Network.ToLoadBalancer.QueueCapacity != nil {
		t.Fatal("null patch values must keep inheritance")
	}
	stream := state.Workload.ClientGroups[0].Stream
	if stream.Bytes == nil || *stream.Bytes != 0 {
		t.Fatal("explicit zero patches must remain distinct from inheritance")
	}

	encoded, err := scenario.EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := scenario.DecodeDocument(encoded)
	if err != nil || !reflect.DeepEqual(decoded, input) {
		t.Fatalf("nested values changed during transport: %+v, %v", decoded, err)
	}
	execution := documentObject(t, encoded)["fixed"].(map[string]any)["execution"].(map[string]any)
	if execution["durationNanoseconds"] != "9223372036854775807" || execution["maxEvents"] != "18446744073709551615" {
		t.Fatal("unsafe integer output must use decimal strings")
	}
	if execution["speed"] != json.Number("0.25") {
		t.Fatal("floating-point settings must stay JSON numbers")
	}
}

// TestDocumentIntegerRules rejects precision loss before any semantic checks.
func TestDocumentIntegerRules(t *testing.T) {
	cases := []struct {
		data string
		want string
	}{
		{`{"scenarioVersion":2,"fixed":{"execution":{"seed":9007199254740992}}}`, "unsafe JSON integer"},
		{`{"scenarioVersion":2,"fixed":{"execution":{"maxEvents":"18446744073709551616"}}}`, "integer outside uint64 range"},
		{`{"scenarioVersion":2,"states":{"0":{"workload":{"clientGroups":[{"count":1e0}]}}}}`, "without fractions or exponents"},
		{`{"scenarioVersion":2,"states":{"0":{"resources":{"backends":[{"port":65536}]}}}}`, "integer outside uint16 range"},
		{`{"scenarioVersion":2,"fixed":{"execution":{"speed":"0.25"}}}`, "speed: expected a number"},
	}
	for _, test := range cases {
		_, err := scenario.DecodeDocument([]byte(test.data))
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("expected %q for %s, got %v", test.want, test.data, err)
		}
	}
}

// TestTimelineDocumentRejectsNonfiniteOutput covers fixed and mapped state values.
func TestTimelineDocumentRejectsNonfiniteOutput(t *testing.T) {
	input := scenario.Scenario{ScenarioVersion: scenario.CurrentScenarioVersion}
	input.Fixed.Execution.Speed = math.NaN()
	encoded, err := scenario.EncodeDocument(input)
	if err == nil || encoded != nil || !strings.Contains(err.Error(), "scenario.fixed.execution.speed: must be a finite number") {
		t.Fatalf("fixed nonfinite value should fail encoding: %s, %v", encoded, err)
	}

	input.Fixed.Execution.Speed = 0
	state := scenario.StateConfig{}
	state.Resources.LoadBalancer.Ingress.LossRate = math.Inf(1)
	input.States = map[string]scenario.StateConfig{"0": state}
	encoded, err = scenario.EncodeDocument(input)
	want := `scenario.states["0"].resources.loadBalancer.ingress.lossRate: must be a finite number`
	if err == nil || encoded != nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("mapped nonfinite value should fail encoding: %s, %v", encoded, err)
	}
}
