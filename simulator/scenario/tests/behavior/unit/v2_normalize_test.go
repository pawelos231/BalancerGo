package scenario_test

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"load-balancer/scenario"
)

// exampleTimeline keeps the same valid defaults as the static scenario fixture.
// Each call owns its slices, so editing one state cannot change another state.
func exampleTimeline() scenario.ScenarioV2 { return exampleScenario() }

// Times 2 and 10 must follow numeric order, not JSON key order or string sorting.
func TestNormalizeV2OrdersFullStatesNumerically(t *testing.T) {
	input := exampleTimeline()
	input.States["10"] = exampleTimeline().States["0"]
	input.States["2"] = exampleTimeline().States["0"]

	resolved, err := scenario.NormalizeV2(input)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Version != scenario.VersionV2 || len(resolved.States) != 3 {
		t.Fatalf("unexpected normalized document: %+v", resolved)
	}
	for index, expected := range []time.Duration{0, 2, 10} {
		if resolved.States[index].At != expected {
			t.Fatalf("state %d is at %v, want %v", index, resolved.States[index].At, expected)
		}
	}
	if resolved.TotalClients != 10 || resolved.States[2].TotalClients != 10 {
		t.Fatal("repeating the same group must not reserve its client IDs three times")
	}
}

// A missing patch in state 2 uses fixed defaults, not state 1's overrides.
func TestNormalizeV2ResolvesEveryStateIndependently(t *testing.T) {
	input := exampleTimeline()
	input.Fixed.Network.Profiles = []scenario.LinkProfile{
		{ID: "slow", Config: scenario.LinkPatch{BandwidthBitsPerSecond: pointer[int64](100)}},
	}

	first := input.States["0"]
	first.Resources.Backends[0].Network.Profile = "slow"
	first.Resources.Backends[0].PhysicalAvailable = pointer(false)
	first.Resources.Backends[0].AdministrativeState = pointer(scenario.BackendAdminDrain)
	first.Resources.LoadBalancer.MappingTimeouts.HalfOpenIdleTimeout = pointer(time.Duration(0))
	first.Workload.ClientGroups[0].Stream.Packets = pointer(2)
	first.Workload.ClientGroups[0].Overrides = []scenario.ClientOverride{
		{
			Index: 1,
			Network: scenario.EndpointNetworkConfig{
				FromLoadBalancer: scenario.LinkPatch{QueueCapacity: pointer(0)},
			},
			Stream: scenario.StreamPatch{Packets: pointer(3)},
		},
	}
	input.States["0"] = first
	input.States["2"] = exampleTimeline().States["0"]

	resolved, err := scenario.NormalizeV2(input)
	if err != nil {
		t.Fatal(err)
	}
	before, after := resolved.States[0], resolved.States[1]
	if before.Backends[0].PhysicalAvailable || before.Backends[0].AdministrativeState != scenario.BackendAdminDrain {
		t.Fatal("explicit false/drain was lost")
	}
	if !after.Backends[0].PhysicalAvailable || after.Backends[0].AdministrativeState != scenario.BackendAdminActive {
		t.Fatal("omitted availability must reset to true/active in the next full state")
	}
	if before.Backends[0].Network.ToLoadBalancer.BandwidthBitsPerSecond != 100 {
		t.Fatal("the backend profile was not resolved")
	}
	if after.Backends[0].Network.ToLoadBalancer != input.Fixed.Network.Defaults {
		t.Fatal("the next backend inherited an earlier state's profile")
	}
	if before.LoadBalancer.HalfOpenIdleTimeout != 0 || after.LoadBalancer.HalfOpenIdleTimeout <= 0 {
		t.Fatal("mapping timeout defaults must also resolve separately in each state")
	}

	// bytes=-1 is resolved after each packet override: 2 KiB for the group, 3 KiB for client 1.
	group := before.ClientGroups[0]
	client, err := group.Client(1)
	if err != nil {
		t.Fatal(err)
	}
	if group.Stream.Bytes != 2048 || client.Stream.Bytes != 3072 {
		t.Fatalf("stream overrides were resolved too early: group=%+v client=%+v", group.Stream, client.Stream)
	}
	if client.Network.FromLoadBalancer.QueueCapacity != 0 || after.ClientGroups[0].Stream.Bytes != 10*1024 {
		t.Fatal("explicit zero or fixed stream inheritance was lost")
	}
}

// A bad final state rejects the whole document; callers never receive a valid prefix.
func TestNormalizeV2RejectsInvalidSettingsAnywhere(t *testing.T) {
	tests := []struct {
		name  string
		edit  func(*scenario.ScenarioV2)
		field string
	}{
		{
			name:  "fixed execution",
			edit:  func(input *scenario.ScenarioV2) { input.Fixed.Execution.MaxEvents = 0 },
			field: "fixed.execution.maxEvents",
		},
		{
			name:  "fixed TCP",
			edit:  func(input *scenario.ScenarioV2) { input.Fixed.TCP.MSSBytes = pointer(0) },
			field: "fixed.tcp.mssBytes",
		},
		{
			name:  "fixed link defaults",
			edit:  func(input *scenario.ScenarioV2) { input.Fixed.Network.Defaults.LossRate = math.NaN() },
			field: "fixed.network.defaults.lossRate",
		},
		{
			name:  "fixed stream defaults",
			edit:  func(input *scenario.ScenarioV2) { input.Fixed.Workload.Defaults.Bytes = -2 },
			field: "fixed.workload.defaults.bytes",
		},
		{
			name: "late ingress",
			edit: func(input *scenario.ScenarioV2) {
				state := input.States["2"]
				state.Resources.LoadBalancer.Ingress.BandwidthBitsPerSecond = 0
				input.States["2"] = state
			},
			field: "ingress.bandwidthBitsPerSecond",
		},
		{
			name: "late mapping timeout",
			edit: func(input *scenario.ScenarioV2) {
				state := input.States["2"]
				state.Resources.LoadBalancer.MappingTimeouts.ClosingIdleTimeout = pointer(-time.Second)
				input.States["2"] = state
			},
			field: "mappingTimeouts.closingIdleTimeoutNanoseconds",
		},
		{
			name:  "late profile reference",
			edit:  func(input *scenario.ScenarioV2) { input.States["2"].Resources.Backends[0].Network.Profile = "missing" },
			field: "network.profile",
		},
		{
			name:  "late CPU",
			edit:  func(input *scenario.ScenarioV2) { input.States["2"].Resources.Backends[0].Host.Cores = pointer(0) },
			field: "host.cores",
		},
		{
			name: "late administrative state",
			edit: func(input *scenario.ScenarioV2) {
				input.States["2"].Resources.Backends[0].AdministrativeState = pointer(scenario.BackendAdministrativeState("dead"))
			},
			field: "administrativeState",
		},
		{
			name:  "late duplicate ID",
			edit:  func(input *scenario.ScenarioV2) { input.States["2"].Resources.Backends[0].ID = "clients" },
			field: "id",
		},
		{
			name: "late duplicate endpoint",
			edit: func(input *scenario.ScenarioV2) {
				state := input.States["2"]
				other := state.Resources.Backends[0]
				other.ID = "other-backend"
				state.Resources.Backends = append(state.Resources.Backends, other)
				input.States["2"] = state
			},
			field: "address",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := exampleTimeline()
			input.States["2"] = exampleTimeline().States["0"]
			test.edit(&input)

			resolved, err := scenario.NormalizeV2(input)
			if err == nil || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("want an error for %s, got %v", test.field, err)
			}
			if !reflect.DeepEqual(resolved, scenario.NormalizedV2{}) {
				t.Fatal("validation returned a partially resolved timeline")
			}
		})
	}
}

// Normalizing must not sort input overrides in place or retain their patch pointers.
func TestNormalizeV2OwnsResolvedData(t *testing.T) {
	input := exampleTimeline()
	first := input.States["0"]
	first.Workload.ClientGroups[0].Overrides = []scenario.ClientOverride{
		{Index: 4, Stream: scenario.StreamPatch{Bytes: pointer(400)}},
		{Index: 1, Stream: scenario.StreamPatch{Bytes: pointer(100)}},
	}
	input.States["0"] = first
	input.States["2"] = exampleTimeline().States["0"]

	resolved, err := scenario.NormalizeV2(input)
	if err != nil {
		t.Fatal(err)
	}
	if input.States["0"].Workload.ClientGroups[0].Overrides[0].Index != 4 {
		t.Fatal("normalization reordered the original overrides")
	}
	if resolved.States[0].ClientGroups[0].Overrides[0].Index != 1 {
		t.Fatal("resolved overrides must be sorted for client lookup")
	}

	// Changes on either side must not leak into the other state or the original input.
	*first.Workload.ClientGroups[0].Overrides[1].Stream.Bytes = 999
	resolved.States[0].Backends[0].Name = "edited result"
	resolved.States[0].ClientGroups[0].Overrides[0].Index = 8
	if resolved.States[0].ClientGroups[0].Overrides[0].Stream.Bytes != 100 {
		t.Fatal("resolved stream retained an input pointer")
	}
	if input.States["0"].Resources.Backends[0].Name != "" || resolved.States[1].Backends[0].Name != "" {
		t.Fatal("resolved backend slices share mutable storage")
	}
	if first.Workload.ClientGroups[0].Overrides[1].Index != 1 {
		t.Fatal("resolved overrides share the input slice")
	}
}
