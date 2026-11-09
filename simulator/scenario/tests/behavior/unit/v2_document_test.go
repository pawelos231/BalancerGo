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

// TestDocumentV2EmptyShape keeps format parsing separate from timeline validation.
func TestDocumentV2EmptyShape(t *testing.T) {
	input, err := scenario.DecodeDocumentV2([]byte(`{"version":2,"fixed":{},"states":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if input.Version != scenario.VersionV2 || input.States == nil || len(input.States) != 0 {
		t.Fatalf("empty state object changed shape: %+v", input)
	}

	omitted, err := scenario.DecodeDocumentV2([]byte(`{"version":2}`))
	if err != nil || !reflect.DeepEqual(omitted, scenario.ScenarioV2{Version: scenario.VersionV2}) {
		t.Fatalf("ordinary missing fields should retain zero values: %+v, %v", omitted, err)
	}
	encoded, err := scenario.EncodeDocumentV2(omitted)
	if err != nil {
		t.Fatal(err)
	}
	object := documentObject(t, encoded)
	if states, ok := object["states"].(map[string]any); !ok || len(states) != 0 {
		t.Fatalf("nil states should encode as {}, got %s", encoded)
	}
	if _, err := scenario.DecodeDocumentV2(encoded); err != nil {
		t.Fatalf("canonical empty document should decode: %v", err)
	}
}

// TestDocumentV2Version requires an explicit v2 version in both directions.
func TestDocumentV2Version(t *testing.T) {
	for _, version := range []int{0, 1, scenario.VersionV2 + 1} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			data := []byte(`{"version":` + strconv.Itoa(version) + `}`)
			decoded, err := scenario.DecodeDocumentV2(data)
			if err == nil || !strings.Contains(err.Error(), "scenario.version: expected version 2") {
				t.Fatalf("expected a version error, got %v", err)
			}
			if !reflect.DeepEqual(decoded, scenario.ScenarioV2{}) {
				t.Fatal("failed decoding must not return a partial document")
			}
			encoded, err := scenario.EncodeDocumentV2(scenario.ScenarioV2{Version: version})
			if err == nil || encoded != nil {
				t.Fatalf("invalid version must not encode: %s, %v", encoded, err)
			}
		})
	}
	if _, err := scenario.DecodeDocumentV2([]byte(`{}`)); err == nil {
		t.Fatal("an omitted version must not imply v2")
	}
	if input, err := scenario.DecodeDocumentV2([]byte(`{"version":"2"}`)); err != nil || input.Version != scenario.VersionV2 {
		t.Fatalf("quoted exact versions should follow the existing integer rules: %+v, %v", input, err)
	}
}

// TestDocumentV2TimestampKeys retains exact keys beyond browser-safe integers.
func TestDocumentV2TimestampKeys(t *testing.T) {
	data := []byte(`{"version":2,"states":{"0":{},"9007199254740993":{},"9223372036854775807":{}}}`)
	input, err := scenario.DecodeDocumentV2(data)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := scenario.EncodeDocumentV2(input)
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
	if _, err := scenario.DecodeDocumentV2([]byte(`{"version":2,"states":{"1":{}}}`)); err != nil {
		t.Fatalf("the document reader must not enforce timeline relationships: %v", err)
	}
}

// TestDocumentV2RejectsNoncanonicalTimestamps checks syntax in Go maps and JSON.
func TestDocumentV2RejectsNoncanonicalTimestamps(t *testing.T) {
	keys := []string{"", "-1", "-0", "+0", "00", "01", " 1", "1 ", "1.0", "1e3", "١", "9223372036854775808"}
	for _, key := range keys {
		t.Run(strconv.Quote(key), func(t *testing.T) {
			quoted, err := json.Marshal(key)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := scenario.DecodeDocumentV2([]byte(`{"version":2,"states":{` + string(quoted) + `:{}}}`))
			if err == nil || !strings.Contains(err.Error(), "canonical nonnegative int64 nanosecond timestamp") {
				t.Fatalf("expected a timestamp error for %q, got %v", key, err)
			}
			if !reflect.DeepEqual(decoded, scenario.ScenarioV2{}) {
				t.Fatal("failed decoding must not return partial states")
			}
			input := scenario.ScenarioV2{Version: scenario.VersionV2, States: map[string]scenario.StateConfig{key: {}}}
			encoded, err := scenario.EncodeDocumentV2(input)
			if err == nil || encoded != nil {
				t.Fatalf("invalid Go-map key must not encode: %s, %v", encoded, err)
			}
		})
	}
}

// TestDocumentV2DuplicateKeys rejects aliases before reading an invalid replacement.
func TestDocumentV2DuplicateKeys(t *testing.T) {
	cases := []struct {
		name string
		data string
		path string
	}{
		{"state", `{"version":2,"states":{"0":{},"0":null}}`, `scenario.states["0"]: duplicate key`},
		{"escaped state", `{"version":2,"states":{"0":{},"\u0030":null}}`, `scenario.states["0"]: duplicate key`},
		{"root", `{"version":2,"ver\u0073ion":null}`, "scenario.version: duplicate field"},
		{"state field", `{"version":2,"states":{"0":{"resources":{},"resources":null}}}`, `scenario.states["0"].resources: duplicate field`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := scenario.DecodeDocumentV2([]byte(test.data))
			if err == nil || !strings.Contains(err.Error(), test.path) {
				t.Fatalf("expected %q, got %v", test.path, err)
			}
		})
	}
}

// TestDocumentV2StrictShape applies the existing object and collection rules to maps.
func TestDocumentV2StrictShape(t *testing.T) {
	cases := []struct {
		name string
		data string
		path string
	}{
		{"null root", `null`, "scenario: must not be null"},
		{"null fixed", `{"version":2,"fixed":null}`, "scenario.fixed: must not be null"},
		{"null states", `{"version":2,"states":null}`, "scenario.states: must not be null"},
		{"states array", `{"version":2,"states":[]}`, "scenario.states: expected an object"},
		{"null state", `{"version":2,"states":{"0":null}}`, `scenario.states["0"]: must not be null`},
		{"scalar state", `{"version":2,"states":{"0":1}}`, `scenario.states["0"]: expected an object`},
		{"unknown root", `{"version":2,"execution":{}}`, "scenario.execution: unknown field"},
		{"unknown fixed", `{"version":2,"fixed":{"extra":1}}`, "scenario.fixed.extra: unknown field"},
		{"unknown state", `{"version":2,"states":{"0":{"extra":1}}}`, `scenario.states["0"].extra: unknown field`},
		{"truncated map", `{"version":2,"states":{"0":{}`, "scenario.states:"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input, err := scenario.DecodeDocumentV2([]byte(test.data))
			if err == nil || !strings.Contains(err.Error(), test.path) {
				t.Fatalf("expected %q, got %v", test.path, err)
			}
			if !reflect.DeepEqual(input, scenario.ScenarioV2{}) {
				t.Fatal("failed decoding must return a zero document")
			}
		})
	}
}

// TestDocumentV2TrailingTokens accepts whitespace after exactly one JSON document.
func TestDocumentV2TrailingTokens(t *testing.T) {
	for _, suffix := range []string{` {}`, ` true`, ` x`} {
		_, err := scenario.DecodeDocumentV2([]byte(`{"version":2}` + suffix))
		if err == nil || !strings.Contains(err.Error(), "trailing JSON") {
			t.Fatalf("expected a trailing-token error for %q, got %v", suffix, err)
		}
	}
	if _, err := scenario.DecodeDocumentV2([]byte("{\"version\":2} \n\t")); err != nil {
		t.Fatalf("trailing whitespace should decode: %v", err)
	}
}

// TestDocumentV2ExactValuesRoundTrip covers fixed values and nested state patches.
func TestDocumentV2ExactValuesRoundTrip(t *testing.T) {
	data := []byte(`{
		"version": 2,
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
	input, err := scenario.DecodeDocumentV2(data)
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

	encoded, err := scenario.EncodeDocumentV2(input)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := scenario.DecodeDocumentV2(encoded)
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

// TestDocumentV2IntegerRules rejects precision loss before any semantic checks.
func TestDocumentV2IntegerRules(t *testing.T) {
	cases := []struct {
		data string
		want string
	}{
		{`{"version":2,"fixed":{"execution":{"seed":9007199254740992}}}`, "unsafe JSON integer"},
		{`{"version":2,"fixed":{"execution":{"maxEvents":"18446744073709551616"}}}`, "integer outside uint64 range"},
		{`{"version":2,"states":{"0":{"workload":{"clientGroups":[{"count":1e0}]}}}}`, "without fractions or exponents"},
		{`{"version":2,"states":{"0":{"resources":{"backends":[{"port":65536}]}}}}`, "integer outside uint16 range"},
		{`{"version":2,"fixed":{"execution":{"speed":"0.25"}}}`, "speed: expected a number"},
	}
	for _, test := range cases {
		_, err := scenario.DecodeDocumentV2([]byte(test.data))
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("expected %q for %s, got %v", test.want, test.data, err)
		}
	}
}

// TestDocumentV2RejectsNonfiniteOutput covers fixed and mapped state values.
func TestDocumentV2RejectsNonfiniteOutput(t *testing.T) {
	input := scenario.ScenarioV2{Version: scenario.VersionV2}
	input.Fixed.Execution.Speed = math.NaN()
	encoded, err := scenario.EncodeDocumentV2(input)
	if err == nil || encoded != nil || !strings.Contains(err.Error(), "scenario.fixed.execution.speed: must be a finite number") {
		t.Fatalf("fixed nonfinite value should fail encoding: %s, %v", encoded, err)
	}

	input.Fixed.Execution.Speed = 0
	state := scenario.StateConfig{}
	state.Resources.LoadBalancer.Ingress.LossRate = math.Inf(1)
	input.States = map[string]scenario.StateConfig{"0": state}
	encoded, err = scenario.EncodeDocumentV2(input)
	want := `scenario.states["0"].resources.loadBalancer.ingress.lossRate: must be a finite number`
	if err == nil || encoded != nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("mapped nonfinite value should fail encoding: %s, %v", encoded, err)
	}
}
