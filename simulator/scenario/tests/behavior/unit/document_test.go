package scenario_test

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"load-balancer/scenario"
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

// TestNormalizedDocumentKeepsResolvedGroupsExact checks inherited values and compact exceptions.
func TestNormalizedDocumentKeepsResolvedGroupsExact(t *testing.T) {
	input := exampleScenario()
	input.Fixed.Execution.Duration = time.Duration(1<<53 + 1)
	input.Fixed.Execution.Seed = math.MinInt64
	input.Fixed.Execution.MaxEvents = math.MaxUint64
	input.Fixed.Network.Defaults.PacketTravelTime = time.Duration(1<<53 + 3)
	input.Fixed.Network.Profiles = []scenario.LinkProfile{{
		ID: "fast",
		Config: scenario.LinkPatch{
			BandwidthBitsPerSecond: pointer[int64](math.MaxInt64),
		},
	}}
	editInitialState(&input, func(state *scenario.StateConfig) {
		state.Resources.Backends[0].Network.Profile = "fast"
	})
	group := &input.States["0"].Workload.ClientGroups[0]
	group.Count = scenario.MaxClients
	group.StartAt = time.Duration(1<<53 + 5)
	group.ClientGap = time.Duration(1<<53 + 7)
	group.Network.Profile = "fast"
	group.Stream.Packets = pointer(20)
	group.Overrides = []scenario.ClientOverride{{
		Index: 2,
		Network: scenario.EndpointNetworkConfig{
			ToLoadBalancer: scenario.LinkPatch{QueueCapacity: pointer(0)},
		},
		Stream: scenario.StreamPatch{Packets: pointer(3)},
	}}
	resolved, err := scenario.NormalizeV2(input)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := scenario.EncodeNormalizedDocumentV2(resolved)
	if err != nil {
		t.Fatal(err)
	}

	object := documentObject(t, encoded)
	if object["version"] != json.Number("2") || object["totalClients"] != json.Number(strconv.Itoa(scenario.MaxClients)) {
		t.Fatal("the resolved view must expose its version and complete client total")
	}
	execution := object["fixed"].(map[string]any)["execution"].(map[string]any)
	if execution["durationNanoseconds"] != "9007199254740993" {
		t.Fatal("resolved durations must retain every nanosecond")
	}
	if execution["seed"] != "-9223372036854775808" || execution["maxEvents"] != "18446744073709551615" {
		t.Fatal("resolved native integer limits must remain decimal strings")
	}
	defaults := object["fixed"].(map[string]any)["networkDefaults"].(map[string]any)
	if defaults["packetTravelTimeNanoseconds"] != "9007199254740995" {
		t.Fatal("resolved default link settings lost duration precision")
	}
	backend := object["states"].([]any)[0].(map[string]any)["backends"].([]any)[0].(map[string]any)
	backendNetwork := backend["network"].(map[string]any)
	for _, direction := range []string{"toLoadBalancer", "fromLoadBalancer"} {
		link := backendNetwork[direction].(map[string]any)
		if link["bandwidthBitsPerSecond"] != "9223372036854775807" {
			t.Fatal("backend directions must contain the effective profile bandwidth")
		}
	}

	groups := object["states"].([]any)[0].(map[string]any)["clientGroups"].([]any)
	if len(groups) != 1 {
		t.Fatal("a large group must not expand into individual clients")
	}
	resolvedGroup := groups[0].(map[string]any)
	if resolvedGroup["startAtNanoseconds"] != "9007199254740997" || resolvedGroup["clientGapNanoseconds"] != "9007199254740999" {
		t.Fatal("resolved arrival settings must use exact nanosecond fields")
	}
	groupStream := resolvedGroup["stream"].(map[string]any)
	if groupStream["bytes"] != json.Number("20480") {
		t.Fatal("group stream bytes must be resolved after applying its packet override")
	}
	overrides := resolvedGroup["overrides"].([]any)
	if len(overrides) != 1 {
		t.Fatal("the resolved view must retain only explicit client exceptions")
	}
	override := overrides[0].(map[string]any)
	overrideStream := override["stream"].(map[string]any)
	if override["index"] != json.Number("2") || overrideStream["bytes"] != json.Number("3072") {
		t.Fatal("client stream bytes must resolve against its own packet override")
	}
	overrideNetwork := override["network"].(map[string]any)
	toLB := overrideNetwork["toLoadBalancer"].(map[string]any)
	if toLB["queueCapacity"] != json.Number("0") || toLB["bandwidthBitsPerSecond"] != "9223372036854775807" {
		t.Fatal("explicit zero must override the queue while retaining inherited bandwidth")
	}
	if _, err := scenario.DecodeDocumentV2(encoded); err == nil {
		t.Fatal("the resolved view must not be accepted as an importable scenario")
	}
}

// TestNormalizedDocumentEmptyCollections keeps optional-looking resolved lists predictable.
func TestNormalizedDocumentEmptyCollections(t *testing.T) {
	encoded, err := scenario.EncodeNormalizedDocumentV2(scenario.NormalizedV2{States: []scenario.NormalizedState{{ClientGroups: []scenario.NormalizedClientGroup{{ID: "empty"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	object := documentObject(t, encoded)
	backends, ok := object["states"].([]any)[0].(map[string]any)["backends"].([]any)
	if !ok || len(backends) != 0 {
		t.Fatal("nil resolved backends must encode as an empty array")
	}
	group := object["states"].([]any)[0].(map[string]any)["clientGroups"].([]any)[0].(map[string]any)
	overrides, ok := group["overrides"].([]any)
	if !ok || len(overrides) != 0 {
		t.Fatal("nil resolved overrides must encode as an empty array")
	}
}

// TestDocumentNativeIntegerRoundTrip includes values that normalization may reject.
func TestDocumentNativeIntegerRoundTrip(t *testing.T) {
	input := exampleScenario()
	input.Fixed.Execution.Duration = time.Duration(math.MaxInt64)
	input.Fixed.Execution.SnapshotInterval = time.Duration(1<<53 + 1)
	input.Fixed.Execution.Seed = math.MinInt64
	input.Fixed.Execution.MaxEvents = math.MaxUint64
	input.Fixed.Execution.Speed = 0.25
	input.Fixed.Network.Defaults.BandwidthBitsPerSecond = math.MaxInt64
	input.Fixed.Network.Defaults.PacketTravelTime = time.Duration(1<<53 + 3)
	input.Fixed.Network.Defaults.QueueCapacity = math.MaxInt
	editInitialState(&input, func(state *scenario.StateConfig) {
		state.Resources.LoadBalancer.ForwardingBitsPerSecond = math.MaxInt64
	})
	editInitialState(&input, func(state *scenario.StateConfig) {
		state.Resources.Backends[0].ID = "9007199254740993"
	})
	editInitialState(&input, func(state *scenario.StateConfig) {
		state.Resources.Backends[0].Port = math.MaxUint16
	})
	input.Fixed.Workload.Defaults.Packets = math.MaxInt
	input.Fixed.Workload.Defaults.Bytes = math.MinInt
	input.Fixed.Workload.Defaults.PacketGap = time.Duration(math.MinInt64)
	editInitialState(&input, func(state *scenario.StateConfig) {
		state.Workload.ClientGroups[0].StartAt = time.Duration(math.MaxInt64)
	})
	editInitialState(&input, func(state *scenario.StateConfig) {
		state.Workload.ClientGroups[0].ClientGap = time.Duration(math.MinInt64)
	})
	editInitialState(&input, func(state *scenario.StateConfig) {
		state.Workload.ClientGroups[0].Network.ToLoadBalancer = scenario.LinkPatch{
			BandwidthBitsPerSecond: pointer[int64](1<<53 + 5),
			PacketTravelTime:       pointer(time.Duration(math.MaxInt64)),
			QueueCapacity:          pointer(math.MinInt),
		}
	})
	editInitialState(&input, func(state *scenario.StateConfig) {
		state.Workload.ClientGroups[0].Stream = scenario.StreamPatch{
			Packets:   pointer(math.MaxInt),
			Bytes:     pointer(math.MinInt),
			PacketGap: pointer(time.Duration(math.MinInt64)),
		}
	})

	encoded, err := scenario.EncodeDocumentV2(input)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := scenario.DecodeDocumentV2(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, input) {
		t.Fatalf("integer values changed during transport:\n got: %+v\nwant: %+v", decoded, input)
	}

	object := documentObject(t, encoded)
	execution := object["fixed"].(map[string]any)["execution"].(map[string]any)
	if execution["durationNanoseconds"] != "9223372036854775807" {
		t.Fatal("long durations must use an exact decimal string")
	}
	if execution["seed"] != "-9223372036854775808" || execution["maxEvents"] != "18446744073709551615" {
		t.Fatal("signed and unsigned native limits must stay exact")
	}
	if execution["speed"] != json.Number("0.25") {
		t.Fatal("floating-point settings must remain JSON numbers")
	}
	resources := object["states"].(map[string]any)["0"].(map[string]any)["resources"].(map[string]any)
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
			input.Fixed.Execution.Seed = value
			encoded, err := scenario.EncodeDocumentV2(input)
			if err != nil {
				t.Fatal(err)
			}
			wire := documentObject(t, encoded)["fixed"].(map[string]any)["execution"].(map[string]any)["seed"]
			expected := strconv.FormatInt(value, 10)
			if value >= -safeInteger && value <= safeInteger {
				if wire != json.Number(expected) {
					t.Fatalf("safe integer should be a JSON number, got %#v", wire)
				}
			} else if wire != expected {
				t.Fatalf("unsafe integer should be a decimal string, got %#v", wire)
			}
			decoded, err := scenario.DecodeDocumentV2(encoded)
			if err != nil || decoded.Fixed.Execution.Seed != value {
				t.Fatalf("round trip changed %d: decoded=%d err=%v", value, decoded.Fixed.Execution.Seed, err)
			}
		})
	}

	for _, value := range []uint64{0, uint64(safeInteger), 1 << 53, math.MaxUint64} {
		t.Run("unsigned/"+strconv.FormatUint(value, 10), func(t *testing.T) {
			input := exampleScenario()
			input.Fixed.Execution.MaxEvents = value
			encoded, err := scenario.EncodeDocumentV2(input)
			if err != nil {
				t.Fatal(err)
			}
			wire := documentObject(t, encoded)["fixed"].(map[string]any)["execution"].(map[string]any)["maxEvents"]
			expected := strconv.FormatUint(value, 10)
			if value <= uint64(safeInteger) {
				if wire != json.Number(expected) {
					t.Fatalf("safe unsigned integer should be a number, got %#v", wire)
				}
			} else if wire != expected {
				t.Fatalf("unsafe unsigned integer should be a string, got %#v", wire)
			}
			decoded, err := scenario.DecodeDocumentV2(encoded)
			if err != nil || decoded.Fixed.Execution.MaxEvents != value {
				t.Fatalf("round trip changed %d: decoded=%d err=%v", value, decoded.Fixed.Execution.MaxEvents, err)
			}
		})
	}
}

// TestDocumentDecimalStrings accepts quoted integers without coercing identifiers.
func TestDocumentDecimalStrings(t *testing.T) {
	data := []byte(`{"version":2,"fixed":{"execution":{"durationNanoseconds":"9007199254740993","snapshotIntervalNanoseconds":"1","seed":"-9223372036854775808","speed":0.25,"maxEvents":"18446744073709551615"},"network":{"defaults":{"bandwidthBitsPerSecond":"9007199254740995","packetTravelTimeNanoseconds":"0","queueCapacity":"2","lossRate":0.125},"profiles":[{"id":"001","config":{"queueCapacity":"0"}}]},"loadBalancer":{"id":"0001"},"workload":{"defaults":{"packets":"3","bytes":"-1","packetGapNanoseconds":"0"}}},"states":{"0":{"resources":{"loadBalancer":{"algorithm":"random","forwardingBitsPerSecond":"1000","ingress":{"bandwidthBitsPerSecond":"1000","packetTravelTimeNanoseconds":"0","queueCapacity":"2","lossRate":0}},"backends":[{"id":"123","address":"10.0.0.1","port":"65535","network":{"profile":"001"}}]},"workload":{"clientGroups":[{"id":"456","count":"1","startAtNanoseconds":"0","clientGapNanoseconds":"0","stream":{"packets":"0","bytes":"0","packetGapNanoseconds":"0"},"overrides":[{"index":"0"}]}]}}}}`)
	input, err := scenario.DecodeDocumentV2(data)
	if err != nil {
		t.Fatal(err)
	}
	if input.Fixed.Execution.Duration != 1<<53+1 || input.Fixed.Network.Defaults.BandwidthBitsPerSecond != 1<<53+3 {
		t.Fatal("quoted integers lost precision before reaching Go fields")
	}
	if input.Fixed.LoadBalancer.ID != "0001" || input.Fixed.Network.Profiles[0].ID != "001" {
		t.Fatal("numeric-looking IDs must not be treated as integer tokens")
	}
	if input.States["0"].Resources.Backends[0].Port != math.MaxUint16 || input.Fixed.Workload.Defaults.Bytes != -1 {
		t.Fatal("quoted safe integers and signed sentinel values must be accepted")
	}
	stream := input.States["0"].Workload.ClientGroups[0].Stream
	if stream.Bytes == nil || *stream.Bytes != 0 || stream.Packets == nil || *stream.Packets != 0 {
		t.Fatal("quoted zero patches must remain explicit overrides")
	}
	if _, err := scenario.NormalizeV2(input); err != nil {
		t.Fatalf("the decoded document should remain usable by the existing normalizer: %v", err)
	}

	encoded, err := scenario.EncodeDocumentV2(input)
	if err != nil {
		t.Fatal(err)
	}
	if documentObject(t, encoded)["version"] != json.Number("2") {
		t.Fatal("canonical output should use numbers for safe quoted input integers")
	}
}

// TestDocumentZeroAndNilPatches distinguishes inheritance from explicit zero.
func TestDocumentZeroAndNilPatches(t *testing.T) {
	input := exampleScenario()
	input.Fixed.Network.Profiles = []scenario.LinkProfile{{
		ID: "zero",
		Config: scenario.LinkPatch{
			PacketTravelTime: pointer(time.Duration(0)),
			QueueCapacity:    pointer(0),
			LossRate:         pointer(0.0),
		},
	}}
	editInitialState(&input, func(state *scenario.StateConfig) {
		state.Workload.ClientGroups[0].Stream.Bytes = pointer(0)
	})
	encoded, err := scenario.EncodeDocumentV2(input)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := scenario.DecodeDocumentV2(encoded)
	if err != nil || !reflect.DeepEqual(decoded, input) {
		t.Fatalf("zero patches changed during transport: decoded=%+v err=%v", decoded, err)
	}
	object := documentObject(t, encoded)
	network := object["fixed"].(map[string]any)["network"].(map[string]any)
	profile := network["profiles"].([]any)[0].(map[string]any)
	patch := profile["config"].(map[string]any)
	if _, present := patch["bandwidthBitsPerSecond"]; present {
		t.Fatal("nil patch pointers must be omitted")
	}
	if patch["packetTravelTimeNanoseconds"] != json.Number("0") || patch["queueCapacity"] != json.Number("0") {
		t.Fatal("non-nil zero pointers must be encoded")
	}

	nullPatch, err := scenario.DecodeDocumentV2([]byte(`{"version":2,"fixed":{"network":{"profiles":[{"id":"p","config":{"queueCapacity":null,"lossRate":null}}]}},"states":{"0":{}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if patch := nullPatch.Fixed.Network.Profiles[0].Config; patch.QueueCapacity != nil || patch.LossRate != nil {
		t.Fatal("null patch values must mean inheritance, like omitted pointers")
	}

	// Required empty collections are arrays; optional empty collections are omitted.
	empty, err := scenario.EncodeDocumentV2(scenario.ScenarioV2{Version: 2, States: map[string]scenario.StateConfig{"0": {}}})
	if err != nil {
		t.Fatal(err)
	}
	emptyObject := documentObject(t, empty)
	resources := emptyObject["states"].(map[string]any)["0"].(map[string]any)["resources"].(map[string]any)
	workload := emptyObject["states"].(map[string]any)["0"].(map[string]any)["workload"].(map[string]any)
	if items, ok := resources["backends"].([]any); !ok || len(items) != 0 {
		t.Fatal("nil required backend collections must encode as []")
	}
	if items, ok := workload["clientGroups"].([]any); !ok || len(items) != 0 {
		t.Fatal("nil required client groups must encode as []")
	}
	if _, present := emptyObject["fixed"].(map[string]any)["network"].(map[string]any)["profiles"]; present {
		t.Fatal("empty optional profiles must be omitted")
	}
	if _, err := scenario.DecodeDocumentV2(empty); err != nil {
		t.Fatalf("canonical empty collections must decode: %v", err)
	}
	if omitted, err := scenario.DecodeDocumentV2([]byte("{\"version\":2} \n\t")); err != nil || !reflect.DeepEqual(omitted, scenario.ScenarioV2{Version: 2}) {
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
		{"unknown nested", `{"version":2,"fixed":{"execution":{"extra":1}},"states":{"0":{}}}`, "scenario.fixed.execution.extra: unknown field"},
		{"case-sensitive key", `{"Version":1}`, "scenario.Version: unknown field"},
		{"unknown patch", `{"version":2,"fixed":{"network":{"profiles":[{"config":{"extra":1}}]}},"states":{"0":{}}}`, "profiles[0].config.extra: unknown field"},
		{"duplicate root", `{"version":1,"version":2}`, "scenario.version: duplicate field"},
		{"duplicate escaped key", `{"version":1,"ver\u0073ion":2}`, "scenario.version: duplicate field"},
		{"duplicate nested", `{"version":2,"fixed":{"execution":{"seed":1,"seed":2}}}`, "scenario.fixed.execution.seed: duplicate field"},
		{"null document", `null`, "scenario: must not be null"},
		{"null scalar", `{"version":null}`, "scenario.version: must not be null"},
		{"null string", `{"version":2,"fixed":{"loadBalancer":{"id":null}},"states":{"0":{"resources":{"loadBalancer":{}}}}}`, "loadBalancer.id: must not be null"},
		{"null object", `{"version":2,"fixed":{"execution":null},"states":{"0":{}}}`, "scenario.fixed.execution: must not be null"},
		{"null array", `{"version":2,"fixed":{},"states":{"0":{"resources":{"backends":null}}}}`, "resources.backends: must not be null"},
		{"null optional array", `{"version":2,"fixed":{"network":{"profiles":null}},"states":{"0":{}}}`, "network.profiles: must not be null"},
		{"null patch object", `{"version":2,"fixed":{},"states":{"0":{"resources":{"backends":[{"network":{"toLoadBalancer":null}}]}}}}`, "toLoadBalancer: must not be null"},
		{"scalar document", `1`, "scenario: expected an object"},
		{"array document", `[]`, "scenario: expected an object"},
		{"array instead of object", `{"version":2,"fixed":{"execution":[]},"states":{"0":{}}}`, "execution: expected an object"},
		{"object instead of array", `{"version":2,"fixed":{},"states":{"0":{"workload":{"clientGroups":{}}}}}`, "clientGroups: expected an array"},
		{"null array element", `{"version":2,"fixed":{},"states":{"0":{"resources":{"backends":[null]}}}}`, "backends[0]: must not be null"},
		{"scalar array element", `{"version":2,"fixed":{},"states":{"0":{"resources":{"backends":[1]}}}}`, "backends[0]: expected an object"},
		{"numeric identifier", `{"version":2,"fixed":{"loadBalancer":{"id":123}},"states":{"0":{"resources":{"loadBalancer":{}}}}}`, "id: expected a string"},
		{"quoted speed", `{"version":2,"fixed":{"execution":{"speed":"1"}},"states":{"0":{}}}`, "speed: expected a number"},
		{"quoted loss", `{"version":2,"fixed":{"network":{"defaults":{"lossRate":"0.5"}}},"states":{"0":{}}}`, "lossRate: expected a number"},
		{"infinite speed", `{"version":2,"fixed":{"execution":{"speed":1e309}},"states":{"0":{}}}`, "speed: must be a finite number"},
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
		{"unsafe positive number", `{"version":2,"fixed":{"execution":{"seed":9007199254740992}},"states":{"0":{}}}`, "unsafe JSON integer"},
		{"unsafe negative number", `{"version":2,"fixed":{"execution":{"seed":-9007199254740992}},"states":{"0":{}}}`, "unsafe JSON integer"},
		{"unsafe unsigned number", `{"version":2,"fixed":{"execution":{"maxEvents":18446744073709552000}},"states":{"0":{}}}`, "unsafe JSON integer"},
		{"signed overflow", `{"version":2,"fixed":{"execution":{"seed":"9223372036854775808"}},"states":{"0":{}}}`, "integer outside int64 range"},
		{"signed underflow", `{"version":2,"fixed":{"execution":{"seed":"-9223372036854775809"}},"states":{"0":{}}}`, "integer outside int64 range"},
		{"unsigned overflow", `{"version":2,"fixed":{"execution":{"maxEvents":"18446744073709551616"}},"states":{"0":{}}}`, "integer outside uint64 range"},
		{"negative unsigned string", `{"version":2,"fixed":{"execution":{"maxEvents":"-1"}},"states":{"0":{}}}`, "integer outside uint64 range"},
		{"negative unsigned number", `{"version":2,"fixed":{"execution":{"maxEvents":-1}},"states":{"0":{}}}`, "integer outside uint64 range"},
		{"port overflow number", `{"version":2,"fixed":{},"states":{"0":{"resources":{"backends":[{"port":65536}]}}}}`, "integer outside uint16 range"},
		{"port overflow string", `{"version":2,"fixed":{},"states":{"0":{"resources":{"backends":[{"port":"65536"}]}}}}`, "integer outside uint16 range"},
		{"duration overflow", `{"version":2,"fixed":{"execution":{"durationNanoseconds":"9223372036854775808"}},"states":{"0":{}}}`, "integer outside time.Duration range"},
		{"native int overflow", `{"version":2,"fixed":{},"states":{"0":{"workload":{"clientGroups":[{"count":"9223372036854775808"}]}}}}`, "integer outside int range"},
		{"second object", `{} {}`, "trailing JSON"},
		{"trailing scalar", `{} true`, "trailing JSON"},
		{"trailing malformed", `{} x`, "trailing JSON"},
		{"missing value", `{"version":2,"fixed":{"execution":{"seed":}} }`, "scenario.fixed.execution.seed:"},
		{"truncated object", `{"version":2,"fixed":{"execution":{`, "scenario.fixed.execution:"},
		{"truncated array", ` {"version":2,"states":{"0":{"resources":{"backends":[}}`, `scenario.states["0"].resources.backends:`},
		{"empty document", ``, "scenario: EOF"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input, err := scenario.DecodeDocumentV2([]byte(test.data))
			if err == nil || !strings.Contains(err.Error(), test.fragment) {
				t.Fatalf("expected error containing %q, got %v", test.fragment, err)
			}
			if !reflect.DeepEqual(input, scenario.ScenarioV2{}) {
				t.Fatal("failed decoding must not return a partial scenario")
			}
		})
	}
}

// TestDocumentRejectsNonfiniteOutput keeps invalid floats out of HTTP responses.
func TestDocumentRejectsNonfiniteOutput(t *testing.T) {
	cases := []struct {
		name   string
		change func(*scenario.ScenarioV2)
		path   string
	}{
		{
			name:   "speed infinity",
			change: func(input *scenario.ScenarioV2) { input.Fixed.Execution.Speed = math.Inf(1) },
			path:   "scenario.fixed.execution.speed",
		},
		{
			name:   "loss NaN",
			change: func(input *scenario.ScenarioV2) { input.Fixed.Network.Defaults.LossRate = math.NaN() },
			path:   "scenario.fixed.network.defaults.lossRate",
		},
		{
			name: "patch infinity",
			change: func(input *scenario.ScenarioV2) {
				editInitialState(input, func(state *scenario.StateConfig) {
					state.Workload.ClientGroups[0].Network.ToLoadBalancer.LossRate = pointer(math.Inf(-1))
				})
			},
			path: `scenario.states["0"].workload.clientGroups[0].network.toLoadBalancer.lossRate`,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := exampleScenario()
			test.change(&input)
			encoded, err := scenario.EncodeDocumentV2(input)
			if err == nil || !strings.Contains(err.Error(), test.path+": must be a finite number") {
				t.Fatalf("expected a field-specific finite-number error, got %v", err)
			}
			if encoded != nil {
				t.Fatal("failed encoding must not return a partial document")
			}
		})
	}
}
