package scenario

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// documentObject inspects JSON token types without rounding its numbers.
func documentObject(t *testing.T, data []byte) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		t.Fatal(err)
	}
	return object
}

// TestDocumentNativeIntegerRoundTrip includes values that normalization may reject.
func TestDocumentNativeIntegerRoundTrip(t *testing.T) {
	input := exampleScenario()
	input.Execution.Duration = time.Duration(math.MaxInt64)
	input.Execution.SnapshotInterval = time.Duration(1<<53 + 1)
	input.Execution.Seed = math.MinInt64
	input.Execution.MaxEvents = math.MaxUint64
	input.Execution.Speed = 0.25
	input.Network.Defaults.BandwidthBitsPerSecond = math.MaxInt64
	input.Network.Defaults.PacketTravelTime = time.Duration(1<<53 + 3)
	input.Network.Defaults.QueueCapacity = math.MaxInt
	input.Resources.LoadBalancer.ForwardingBitsPerSecond = math.MaxInt64
	input.Resources.Backends[0].ID = "9007199254740993"
	input.Resources.Backends[0].Port = math.MaxUint16
	input.Workload.Defaults.Packets = math.MaxInt
	input.Workload.Defaults.Bytes = math.MinInt
	input.Workload.Defaults.PacketGap = time.Duration(math.MinInt64)
	input.Workload.ClientGroups[0].StartAt = time.Duration(math.MaxInt64)
	input.Workload.ClientGroups[0].ClientGap = time.Duration(math.MinInt64)
	input.Workload.ClientGroups[0].Network.ToLoadBalancer = LinkPatch{
		BandwidthBitsPerSecond: pointer[int64](1<<53 + 5),
		PacketTravelTime:       pointer(time.Duration(math.MaxInt64)),
		QueueCapacity:          pointer(math.MinInt),
	}
	input.Workload.ClientGroups[0].Stream = StreamPatch{
		Packets:   pointer(math.MaxInt),
		Bytes:     pointer(math.MinInt),
		PacketGap: pointer(time.Duration(math.MinInt64)),
	}

	encoded, err := EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeDocument(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, input) {
		t.Fatalf("integer values changed during transport:\n got: %+v\nwant: %+v", decoded, input)
	}

	object := documentObject(t, encoded)
	execution := object["execution"].(map[string]any)
	if execution["durationNanoseconds"] != "9223372036854775807" {
		t.Fatal("long durations must use an exact decimal string")
	}
	if execution["seed"] != "-9223372036854775808" || execution["maxEvents"] != "18446744073709551615" {
		t.Fatal("signed and unsigned native limits must stay exact")
	}
	if execution["speed"] != json.Number("0.25") {
		t.Fatal("floating-point settings must remain JSON numbers")
	}
	resources := object["resources"].(map[string]any)
	backend := resources["backends"].([]any)[0].(map[string]any)
	if backend["id"] != "9007199254740993" || backend["port"] != json.Number("65535") {
		t.Fatal("IDs must stay strings while safe ports stay numbers")
	}
}

// TestDocumentSafeIntegerBoundaries checks both sides of the browser-safe range.
func TestDocumentSafeIntegerBoundaries(t *testing.T) {
	const safeInteger int64 = 9_007_199_254_740_991
	signed := []int64{
		0,
		safeInteger,
		-safeInteger,
		safeInteger + 1,
		-safeInteger - 1,
		math.MaxInt64,
		math.MinInt64,
	}
	for _, value := range signed {
		t.Run("signed/"+strconv.FormatInt(value, 10), func(t *testing.T) {
			input := exampleScenario()
			input.Execution.Seed = value
			encoded, err := EncodeDocument(input)
			if err != nil {
				t.Fatal(err)
			}
			wire := documentObject(t, encoded)["execution"].(map[string]any)["seed"]
			expected := strconv.FormatInt(value, 10)
			if value >= -safeInteger && value <= safeInteger {
				if wire != json.Number(expected) {
					t.Fatalf("safe integer should be a JSON number, got %#v", wire)
				}
			} else if wire != expected {
				t.Fatalf("unsafe integer should be a decimal string, got %#v", wire)
			}
			decoded, err := DecodeDocument(encoded)
			if err != nil || decoded.Execution.Seed != value {
				t.Fatalf("round trip changed %d: decoded=%d err=%v", value, decoded.Execution.Seed, err)
			}
		})
	}

	for _, value := range []uint64{0, uint64(safeInteger), 1 << 53, math.MaxUint64} {
		t.Run("unsigned/"+strconv.FormatUint(value, 10), func(t *testing.T) {
			input := exampleScenario()
			input.Execution.MaxEvents = value
			encoded, err := EncodeDocument(input)
			if err != nil {
				t.Fatal(err)
			}
			wire := documentObject(t, encoded)["execution"].(map[string]any)["maxEvents"]
			expected := strconv.FormatUint(value, 10)
			if value <= uint64(safeInteger) {
				if wire != json.Number(expected) {
					t.Fatalf("safe unsigned integer should be a number, got %#v", wire)
				}
			} else if wire != expected {
				t.Fatalf("unsafe unsigned integer should be a string, got %#v", wire)
			}
			decoded, err := DecodeDocument(encoded)
			if err != nil || decoded.Execution.MaxEvents != value {
				t.Fatalf("round trip changed %d: decoded=%d err=%v", value, decoded.Execution.MaxEvents, err)
			}
		})
	}
}

// TestDocumentDecimalStrings accepts quoted integers without coercing identifiers.
func TestDocumentDecimalStrings(t *testing.T) {
	data := []byte(`{
		"version": "1",
		"execution": {
			"durationNanoseconds": "9007199254740993",
			"snapshotIntervalNanoseconds": "1",
			"seed": "-9223372036854775808",
			"speed": 0.25,
			"maxEvents": "18446744073709551615"
		},
		"network": {
			"defaults": {
				"bandwidthBitsPerSecond": "9007199254740995",
				"packetTravelTimeNanoseconds": "0",
				"queueCapacity": "2",
				"lossRate": 0.125
			},
			"profiles": [{"id": "001", "config": {"queueCapacity": "0"}}]
		},
		"resources": {
			"loadBalancer": {
				"id": "0001",
				"algorithm": "random",
				"forwardingBitsPerSecond": "1000",
				"ingress": {
					"bandwidthBitsPerSecond": "1000",
					"packetTravelTimeNanoseconds": "0",
					"queueCapacity": "2",
					"lossRate": 0
				}
			},
			"backends": [{
				"id": "123", "address": "10.0.0.1", "port": "65535",
				"network": {"profile": "001"}
			}]
		},
		"workload": {
			"defaults": {"packets": "3", "bytes": "-1", "packetGapNanoseconds": "0"},
			"clientGroups": [{
				"id": "456", "count": "1", "startAtNanoseconds": "0", "clientGapNanoseconds": "0",
				"stream": {"packets": "0", "bytes": "0", "packetGapNanoseconds": "0"},
				"overrides": [{"index": "0"}]
			}]
		}
	}`)
	input, err := DecodeDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	if input.Execution.Duration != 1<<53+1 || input.Network.Defaults.BandwidthBitsPerSecond != 1<<53+3 {
		t.Fatal("quoted integers lost precision before reaching Go fields")
	}
	if input.Resources.LoadBalancer.ID != "0001" || input.Network.Profiles[0].ID != "001" {
		t.Fatal("numeric-looking IDs must not be treated as integer tokens")
	}
	if input.Resources.Backends[0].Port != math.MaxUint16 || input.Workload.Defaults.Bytes != -1 {
		t.Fatal("quoted safe integers and signed sentinel values must be accepted")
	}
	stream := input.Workload.ClientGroups[0].Stream
	if stream.Bytes == nil || *stream.Bytes != 0 || stream.Packets == nil || *stream.Packets != 0 {
		t.Fatal("quoted zero patches must remain explicit overrides")
	}
	if _, err := Normalize(input); err != nil {
		t.Fatalf("the decoded document should remain usable by the existing normalizer: %v", err)
	}

	encoded, err := EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	if documentObject(t, encoded)["version"] != json.Number("1") {
		t.Fatal("canonical output should use numbers for safe quoted input integers")
	}
}

// TestDocumentZeroAndNilPatches distinguishes inheritance from explicit zero.
func TestDocumentZeroAndNilPatches(t *testing.T) {
	input := exampleScenario()
	input.Network.Profiles = []LinkProfile{{
		ID: "zero",
		Config: LinkPatch{
			PacketTravelTime: pointer(time.Duration(0)),
			QueueCapacity:    pointer(0),
			LossRate:         pointer(0.0),
		},
	}}
	input.Workload.ClientGroups[0].Stream.Bytes = pointer(0)
	encoded, err := EncodeDocument(input)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeDocument(encoded)
	if err != nil || !reflect.DeepEqual(decoded, input) {
		t.Fatalf("zero patches changed during transport: decoded=%+v err=%v", decoded, err)
	}
	object := documentObject(t, encoded)
	network := object["network"].(map[string]any)
	profile := network["profiles"].([]any)[0].(map[string]any)
	patch := profile["config"].(map[string]any)
	if _, present := patch["bandwidthBitsPerSecond"]; present {
		t.Fatal("nil patch pointers must be omitted")
	}
	if patch["packetTravelTimeNanoseconds"] != json.Number("0") || patch["queueCapacity"] != json.Number("0") {
		t.Fatal("non-nil zero pointers must be encoded")
	}

	nullPatch, err := DecodeDocument([]byte(`{"network":{"profiles":[{"id":"p","config":{"queueCapacity":null,"lossRate":null}}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if patch := nullPatch.Network.Profiles[0].Config; patch.QueueCapacity != nil || patch.LossRate != nil {
		t.Fatal("null patch values must mean inheritance, like omitted pointers")
	}

	// Required empty collections are arrays; optional empty collections are omitted.
	empty, err := EncodeDocument(Scenario{})
	if err != nil {
		t.Fatal(err)
	}
	emptyObject := documentObject(t, empty)
	resources := emptyObject["resources"].(map[string]any)
	workload := emptyObject["workload"].(map[string]any)
	if items, ok := resources["backends"].([]any); !ok || len(items) != 0 {
		t.Fatal("nil required backend collections must encode as []")
	}
	if items, ok := workload["clientGroups"].([]any); !ok || len(items) != 0 {
		t.Fatal("nil required client groups must encode as []")
	}
	if _, present := emptyObject["network"].(map[string]any)["profiles"]; present {
		t.Fatal("empty optional profiles must be omitted")
	}
	if _, err := DecodeDocument(empty); err != nil {
		t.Fatalf("canonical empty collections must decode: %v", err)
	}
	if omitted, err := DecodeDocument([]byte("{} \n\t")); err != nil || !reflect.DeepEqual(omitted, Scenario{}) {
		t.Fatal("omitted values must retain the existing zero-value behavior")
	}
}

// TestDocumentRejectsInvalidInput checks shape, precision and strict field names.
func TestDocumentRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name     string
		data     string
		fragment string
	}{
		{"unknown root", `{"extra":1}`, "scenario.extra: unknown field"},
		{"unknown nested", `{"execution":{"extra":1}}`, "scenario.execution.extra: unknown field"},
		{"case-sensitive key", `{"Version":1}`, "scenario.Version: unknown field"},
		{"unknown patch", `{"network":{"profiles":[{"config":{"extra":1}}]}}`, "profiles[0].config.extra: unknown field"},
		{"duplicate root", `{"version":1,"version":2}`, "scenario.version: duplicate field"},
		{"duplicate escaped key", `{"version":1,"ver\u0073ion":2}`, "scenario.version: duplicate field"},
		{"duplicate nested", `{"execution":{"seed":1,"seed":2}}`, "scenario.execution.seed: duplicate field"},
		{"null document", `null`, "scenario: must not be null"},
		{"null scalar", `{"version":null}`, "scenario.version: must not be null"},
		{"null string", `{"resources":{"loadBalancer":{"id":null}}}`, "loadBalancer.id: must not be null"},
		{"null object", `{"execution":null}`, "scenario.execution: must not be null"},
		{"null array", `{"resources":{"backends":null}}`, "resources.backends: must not be null"},
		{"null optional array", `{"network":{"profiles":null}}`, "network.profiles: must not be null"},
		{"null patch object", `{"resources":{"backends":[{"network":{"toLoadBalancer":null}}]}}`, "toLoadBalancer: must not be null"},
		{"scalar document", `1`, "scenario: expected an object"},
		{"array document", `[]`, "scenario: expected an object"},
		{"array instead of object", `{"execution":[]}`, "execution: expected an object"},
		{"object instead of array", `{"workload":{"clientGroups":{}}}`, "clientGroups: expected an array"},
		{"null array element", `{"resources":{"backends":[null]}}`, "backends[0]: must not be null"},
		{"scalar array element", `{"resources":{"backends":[1]}}`, "backends[0]: expected an object"},
		{"numeric identifier", `{"resources":{"loadBalancer":{"id":123}}}`, "id: expected a string"},
		{"quoted speed", `{"execution":{"speed":"1"}}`, "speed: expected a number"},
		{"quoted loss", `{"network":{"defaults":{"lossRate":"0.5"}}}`, "lossRate: expected a number"},
		{"infinite speed", `{"execution":{"speed":1e309}}`, "speed: must be a finite number"},
		{"boolean integer", `{"version":true}`, "expected an integer number or decimal string"},
		{"fractional integer", `{"version":1.5}`, "without fractions or exponents"},
		{"decimal integer token", `{"version":1.0}`, "without fractions or exponents"},
		{"exponent integer token", `{"version":1e0}`, "without fractions or exponents"},
		{"quoted fraction", `{"version":"1.5"}`, "without fractions or exponents"},
		{"quoted exponent", `{"version":"1e0"}`, "without fractions or exponents"},
		{"quoted plus", `{"version":"+1"}`, "without fractions or exponents"},
		{"quoted leading zero", `{"version":"01"}`, "without fractions or exponents"},
		{"quoted whitespace", `{"version":" 1"}`, "without fractions or exponents"},
		{"empty integer", `{"version":""}`, "without fractions or exponents"},
		{"unsafe positive number", `{"execution":{"seed":9007199254740992}}`, "unsafe JSON integer"},
		{"unsafe negative number", `{"execution":{"seed":-9007199254740992}}`, "unsafe JSON integer"},
		{"unsafe unsigned number", `{"execution":{"maxEvents":18446744073709551615}}`, "unsafe JSON integer"},
		{"signed overflow", `{"execution":{"seed":"9223372036854775808"}}`, "integer outside int64 range"},
		{"signed underflow", `{"execution":{"seed":"-9223372036854775809"}}`, "integer outside int64 range"},
		{"unsigned overflow", `{"execution":{"maxEvents":"18446744073709551616"}}`, "integer outside uint64 range"},
		{"negative unsigned string", `{"execution":{"maxEvents":"-1"}}`, "integer outside uint64 range"},
		{"negative unsigned number", `{"execution":{"maxEvents":-1}}`, "integer outside uint64 range"},
		{"port overflow number", `{"resources":{"backends":[{"port":65536}]}}`, "integer outside uint16 range"},
		{"port overflow string", `{"resources":{"backends":[{"port":"65536"}]}}`, "integer outside uint16 range"},
		{"duration overflow", `{"execution":{"durationNanoseconds":"9223372036854775808"}}`, "integer outside time.Duration range"},
		{"native int overflow", `{"workload":{"clientGroups":[{"count":"9223372036854775808"}]}}`, "integer outside int range"},
		{"second object", `{} {}`, "trailing JSON"},
		{"trailing scalar", `{} true`, "trailing JSON"},
		{"trailing malformed", `{} x`, "trailing JSON"},
		{"missing value", `{"execution":{"seed":}}`, "scenario.execution.seed:"},
		{"truncated object", `{"execution":{`, "scenario.execution:"},
		{"truncated array", `{"resources":{"backends":[`, "scenario.resources.backends:"},
		{"empty document", ``, "scenario: EOF"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input, err := DecodeDocument([]byte(test.data))
			if err == nil || !strings.Contains(err.Error(), test.fragment) {
				t.Fatalf("expected error containing %q, got %v", test.fragment, err)
			}
			if !reflect.DeepEqual(input, Scenario{}) {
				t.Fatal("failed decoding must not return a partial scenario")
			}
		})
	}
}

// TestDocumentRejectsNonfiniteOutput keeps invalid floats out of HTTP responses.
func TestDocumentRejectsNonfiniteOutput(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Scenario)
		path   string
	}{
		{
			name:   "speed infinity",
			change: func(input *Scenario) { input.Execution.Speed = math.Inf(1) },
			path:   "scenario.execution.speed",
		},
		{
			name:   "loss NaN",
			change: func(input *Scenario) { input.Network.Defaults.LossRate = math.NaN() },
			path:   "scenario.network.defaults.lossRate",
		},
		{
			name: "patch infinity",
			change: func(input *Scenario) {
				input.Workload.ClientGroups[0].Network.ToLoadBalancer.LossRate = pointer(math.Inf(-1))
			},
			path: "scenario.workload.clientGroups[0].network.toLoadBalancer.lossRate",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := exampleScenario()
			test.change(&input)
			encoded, err := EncodeDocument(input)
			if err == nil || !strings.Contains(err.Error(), test.path+": must be a finite number") {
				t.Fatalf("expected a field-specific finite-number error, got %v", err)
			}
			if encoded != nil {
				t.Fatal("failed encoding must not return a partial document")
			}
		})
	}
}
