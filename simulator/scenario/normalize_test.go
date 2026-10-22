package scenario

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"load-balancer/model"
)

// exampleScenario provides valid settings shared by the normalization tests.
func exampleScenario() Scenario {
	return Scenario{
		Version: Version,

		Execution: ExecutionConfig{
			Duration:         time.Minute,
			SnapshotInterval: time.Millisecond,
			Seed:             1,
			MaxEvents:        1000,
		},

		Network: NetworkConfig{
			Defaults: LinkConfig{
				BandwidthBitsPerSecond: 1_000_000_000,
				PacketTravelTime:       time.Millisecond,
				QueueCapacity:          1024,
				LossRate:               0.2,
			},
		},

		Resources: ResourcesConfig{
			LoadBalancer: LoadBalancerConfig{
				ID:                      "lb",
				Algorithm:               "roundRobin",
				Ingress:                 DefaultIngressConfig(),
				ForwardingBitsPerSecond: 1_000_000_000,
			},

			Backends: []BackendConfig{
				{
					ID:      "backend",
					Address: "10.0.0.1",
					Port:    80,
				},
			},
		},

		Workload: WorkloadConfig{
			Defaults: StreamConfig{
				Packets:   10,
				Bytes:     -1,
				PacketGap: time.Millisecond,
			},

			ClientGroups: []ClientGroup{
				{
					ID:    "clients",
					Count: 10,
				},
			},
		},
	}
}

func pointer[T any](value T) *T {
	return &value
}

func TestGroupAndClientPatchesPreserveZeroAndProfileInheritance(t *testing.T) {
	// Give the group and one client different bandwidth profiles.
	input := exampleScenario()
	input.Network.Profiles = []LinkProfile{
		{
			ID: "group",
			Config: LinkPatch{
				BandwidthBitsPerSecond: pointer[int64](100),
				LossRate:               pointer(0.4),
			},
		},
		{
			ID: "client",
			Config: LinkPatch{
				BandwidthBitsPerSecond: pointer[int64](200),
			},
		},
	}

	group := &input.Workload.ClientGroups[0]
	group.Network = EndpointNetworkConfig{
		Profile: "group",

		ToLoadBalancer: LinkPatch{
			QueueCapacity: pointer(7),
		},

		FromLoadBalancer: LinkPatch{
			PacketTravelTime: pointer(7 * time.Nanosecond),
		},
	}

	// Explicit zeros must replace inherited values for this client.
	group.Overrides = []ClientOverride{
		{
			Index: 2,
			Network: EndpointNetworkConfig{
				Profile: "client",

				ToLoadBalancer: LinkPatch{
					PacketTravelTime: pointer(time.Duration(0)),
					QueueCapacity:    pointer(0),
					LossRate:         pointer(0.0),
				},
			},
		},
	}

	resolved, err := Normalize(input)
	if err != nil {
		t.Fatal(err)
	}

	resolvedGroup := resolved.ClientGroups[0]

	defaultClient, err := resolvedGroup.Client(0)
	if err != nil {
		t.Fatal(err)
	}

	customClient, err := resolvedGroup.Client(2)
	if err != nil {
		t.Fatal(err)
	}

	// An ordinary client keeps the group's settings.
	defaultToLB := defaultClient.Network.ToLoadBalancer
	if defaultToLB.BandwidthBitsPerSecond != 100 || defaultToLB.QueueCapacity != 7 {
		t.Fatalf("group settings not applied: %+v", defaultClient.Network)
	}

	// The custom profile changes both directions.
	toLB := customClient.Network.ToLoadBalancer
	fromLB := customClient.Network.FromLoadBalancer

	if toLB.BandwidthBitsPerSecond != 200 || fromLB.BandwidthBitsPerSecond != 200 {
		t.Fatalf("client profile not applied in both directions: %+v", customClient.Network)
	}

	zeroValuesPreserved := toLB.PacketTravelTime == 0 &&
		toLB.QueueCapacity == 0 &&
		toLB.LossRate == 0

	if !zeroValuesPreserved {
		t.Fatalf("explicit zero settings lost: %+v", toLB)
	}

	// Unspecified fields still inherit the group's values.
	if fromLB.PacketTravelTime != 7*time.Nanosecond || fromLB.LossRate != 0.4 {
		t.Fatalf("sparse client profile reset inherited settings: %+v", fromLB)
	}

	if defaultClient.ID != "client/clients/0" || customClient.ID != "client/clients/2" {
		t.Fatalf("unexpected stable ids: %q, %q", defaultClient.ID, customClient.ID)
	}
}

func TestClientPacketOverrideResolvesByteSentinelLast(t *testing.T) {
	// Each layer changes a stream whose byte count is still derived from packets.
	input := exampleScenario()
	group := &input.Workload.ClientGroups[0]
	group.Stream.Packets = pointer(20)

	group.Overrides = []ClientOverride{
		{
			Index: 2,
			Stream: StreamPatch{
				Packets: pointer(3),
			},
		},
		{
			Index: 4,
			Stream: StreamPatch{
				Bytes:     pointer(0),
				PacketGap: pointer(time.Duration(0)),
			},
		},
	}

	resolved, err := Normalize(input)
	if err != nil {
		t.Fatal(err)
	}

	// Defaults and group overrides resolve their byte counts independently.
	resolvedGroup := resolved.ClientGroups[0]
	expectedDefaultBytes := 10 * model.SEGMENT_SIZE
	expectedGroupBytes := 20 * model.SEGMENT_SIZE

	if resolved.DefaultStream.Bytes != expectedDefaultBytes {
		t.Fatalf("default stream bytes = %d, want %d", resolved.DefaultStream.Bytes, expectedDefaultBytes)
	}

	if resolvedGroup.Stream.Bytes != expectedGroupBytes {
		t.Fatalf("group stream bytes = %d, want %d", resolvedGroup.Stream.Bytes, expectedGroupBytes)
	}

	// A client packet override must be applied before resolving bytes=-1.
	client, err := resolvedGroup.Client(2)
	if err != nil {
		t.Fatal(err)
	}

	if client.Stream.Bytes != 3*model.SEGMENT_SIZE {
		t.Fatalf("client packets override ignored: %+v", client.Stream)
	}

	empty, err := resolvedGroup.Client(4)
	if err != nil {
		t.Fatal(err)
	}

	if empty.Stream.Bytes != 0 || empty.Stream.PacketGap != 0 {
		t.Fatalf("explicit zero stream settings ignored: %+v", empty.Stream)
	}

	// Resolving the output must leave the original input untouched.
	if input.Workload.Defaults.Bytes != -1 {
		t.Fatal("normalization mutated the input byte sentinel")
	}
}

func TestHeterogeneousBackendsResolveTheirOwnDirections(t *testing.T) {
	// One backend uses a profile; another overrides only one direction.
	input := exampleScenario()
	input.Network.Profiles = []LinkProfile{
		{
			ID: "slow",
			Config: LinkPatch{
				BandwidthBitsPerSecond: pointer[int64](100),
			},
		},
	}

	input.Resources.Backends[0].Network.Profile = "slow"
	input.Resources.Backends = append(input.Resources.Backends, BackendConfig{
		ID:      "fast",
		Address: "10.0.0.2",
		Port:    80,

		Network: EndpointNetworkConfig{
			FromLoadBalancer: LinkPatch{
				BandwidthBitsPerSecond: pointer[int64](200),
			},
		},
	})

	resolved, err := Normalize(input)
	if err != nil {
		t.Fatal(err)
	}

	slow := resolved.Backends[0].Network
	if slow.ToLoadBalancer.BandwidthBitsPerSecond != 100 {
		t.Fatal("backend profile ignored")
	}

	// The second backend keeps defaults in its untouched direction.
	fast := resolved.Backends[1].Network
	if fast.FromLoadBalancer.BandwidthBitsPerSecond != 200 {
		t.Fatalf("backend directional override ignored: %+v", fast)
	}

	if fast.ToLoadBalancer.BandwidthBitsPerSecond != 1_000_000_000 {
		t.Fatalf("backend directional settings leaked: %+v", fast)
	}
}

func TestMaximumClientGroupStaysCompactAndDetached(t *testing.T) {
	// A huge group needs storage only for its shared settings and exceptions.
	input := exampleScenario()
	group := &input.Workload.ClientGroups[0]
	group.Count = MaxClients

	group.Overrides = []ClientOverride{
		{
			Index: 9,
			Stream: StreamPatch{
				Bytes: pointer(9),
			},
		},
		{
			Index: 2,
			Stream: StreamPatch{
				Bytes: pointer(2),
			},
		},
	}

	first, err := Normalize(input)
	if err != nil {
		t.Fatal(err)
	}

	second, err := Normalize(input)
	if err != nil {
		t.Fatal(err)
	}

	// Repeated normalization must produce the same values.
	if !reflect.DeepEqual(first, second) {
		t.Fatal("the same scenario produced different normalized values")
	}

	if first.TotalClients != MaxClients || len(first.ClientGroups) != 1 {
		t.Fatal("client group was expanded or lost")
	}

	firstGroup := first.ClientGroups[0]
	if len(firstGroup.Overrides) != 2 {
		t.Fatal("client overrides were expanded or lost")
	}

	// Sorting the result must not reorder the original input.
	if firstGroup.Overrides[0].Index != 2 {
		t.Fatal("normalized overrides were not sorted")
	}

	if group.Overrides[0].Index != 9 {
		t.Fatal("sorting normalized overrides reordered the input")
	}

	client, err := firstGroup.Client(MaxClients - 1)
	if err != nil {
		t.Fatal(err)
	}

	if client.ID != "client/clients/16777214" {
		t.Fatalf("unexpected last client id: %q", client.ID)
	}

	// Later changes to input pointers must not affect the resolved values.
	*group.Overrides[1].Stream.Bytes = 100
	input.Resources.Backends[0].Name = "changed"

	if firstGroup.Overrides[0].Stream.Bytes != 2 {
		t.Fatal("normalized stream aliases mutable input")
	}

	if first.Backends[0].Name == "changed" {
		t.Fatal("normalized backend aliases mutable input")
	}
}

func TestZeroAndLargeNativeValuesRemainValid(t *testing.T) {
	// Go values keep their native ranges, including durations and event limits.
	input := exampleScenario()
	input.Execution = ExecutionConfig{
		Duration:         time.Duration(math.MaxInt64),
		SnapshotInterval: 1,
		Seed:             math.MinInt64,
		MaxEvents:        math.MaxUint64,
	}

	input.Network.Defaults.BandwidthBitsPerSecond = math.MaxInt64
	input.Network.Defaults.PacketTravelTime = 0
	input.Network.Defaults.QueueCapacity = 0
	input.Resources.LoadBalancer.ForwardingBitsPerSecond = math.MaxInt64
	input.Resources.LoadBalancer.Ingress = LinkConfig{BandwidthBitsPerSecond: math.MaxInt64}

	input.Workload.Defaults = StreamConfig{
		Packets: math.MaxInt,
		Bytes:   0,
	}

	group := &input.Workload.ClientGroups[0]
	group.Count = 0
	group.StartAt = time.Duration(math.MaxInt64)
	group.ClientGap = time.Duration(math.MaxInt64)

	resolved, err := Normalize(input)
	if err != nil {
		t.Fatal(err)
	}

	// An empty group stays in the result but cannot return a client.
	if len(resolved.ClientGroups) != 1 || resolved.TotalClients != 0 {
		t.Fatal("empty group must be retained")
	}

	if _, err := resolved.ClientGroups[0].Client(0); err == nil {
		t.Fatal("empty group accepted a client index")
	}

	input.Execution.Duration = 0
	if _, err := Normalize(input); err != nil {
		t.Fatalf("zero duration should be valid: %v", err)
	}

	// Far-future arrivals need no overflowing count * gap calculation.
	group.Count = MaxClients
	if _, err := Normalize(input); err != nil {
		t.Fatalf("far-future arrivals should not require overflowing count * gap: %v", err)
	}
}

func TestSimplifiedTransportByteBoundary(t *testing.T) {
	input := exampleScenario()

	// Use the stricter of the segment counter and native byte limits.
	maximum := uint64(math.MaxUint32)*uint64(model.SEGMENT_SIZE) - 1
	nativeByteLimit := uint64(math.MaxInt - 40)

	if nativeByteLimit < maximum {
		maximum = nativeByteLimit
	}

	input.Workload.Defaults.Bytes = int(maximum)
	if _, err := Normalize(input); err != nil {
		t.Fatalf("last supported stream byte rejected: %v", err)
	}

	// One extra byte must exceed the supported transport range.
	input.Workload.Defaults.Bytes++
	if _, err := Normalize(input); err == nil {
		t.Fatal("stream beyond transport range accepted")
	}

	// Deriving bytes from a huge packet count must detect multiplication overflow.
	input.Workload.Defaults.Bytes = -1
	input.Workload.Defaults.Packets = math.MaxInt

	if _, err := Normalize(input); err == nil {
		t.Fatal("packet count multiplication overflow accepted")
	}
}

func TestNormalizeRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name          string
		change        func(*Scenario)
		errorFragment string
	}{
		// Execution settings.
		{
			name: "version",
			change: func(input *Scenario) {
				input.Version++
			},
			errorFragment: "version",
		},
		{
			name: "duration",
			change: func(input *Scenario) {
				input.Execution.Duration = -1
			},
			errorFragment: "execution.durationNanoseconds",
		},
		{
			name: "snapshot",
			change: func(input *Scenario) {
				input.Execution.SnapshotInterval = 0
			},
			errorFragment: "execution.snapshotIntervalNanoseconds",
		},
		{
			name: "negative speed",
			change: func(input *Scenario) {
				input.Execution.Speed = -1
			},
			errorFragment: "execution.speed",
		},
		{
			name: "nan speed",
			change: func(input *Scenario) {
				input.Execution.Speed = math.NaN()
			},
			errorFragment: "execution.speed",
		},
		{
			name: "infinite speed",
			change: func(input *Scenario) {
				input.Execution.Speed = math.Inf(1)
			},
			errorFragment: "execution.speed",
		},
		{
			name: "paced overflow",
			change: func(input *Scenario) {
				input.Execution.Speed = math.SmallestNonzeroFloat64
			},
			errorFragment: "paced duration",
		},
		{
			name: "events",
			change: func(input *Scenario) {
				input.Execution.MaxEvents = 0
			},
			errorFragment: "execution.maxEvents",
		},

		// Link defaults.
		{
			name: "bandwidth",
			change: func(input *Scenario) {
				input.Network.Defaults.BandwidthBitsPerSecond = 0
			},
			errorFragment: "network.defaults.bandwidthBitsPerSecond",
		},
		{
			name: "travel",
			change: func(input *Scenario) {
				input.Network.Defaults.PacketTravelTime = -1
			},
			errorFragment: "network.defaults.packetTravelTimeNanoseconds",
		},
		{
			name: "queue",
			change: func(input *Scenario) {
				input.Network.Defaults.QueueCapacity = -1
			},
			errorFragment: "network.defaults.queueCapacity",
		},
		{
			name: "negative loss",
			change: func(input *Scenario) {
				input.Network.Defaults.LossRate = -0.1
			},
			errorFragment: "network.defaults.lossRate",
		},
		{
			name: "high loss",
			change: func(input *Scenario) {
				input.Network.Defaults.LossRate = 1.1
			},
			errorFragment: "network.defaults.lossRate",
		},
		{
			name: "nan loss",
			change: func(input *Scenario) {
				input.Network.Defaults.LossRate = math.NaN()
			},
			errorFragment: "network.defaults.lossRate",
		},
		{
			name: "infinite loss",
			change: func(input *Scenario) {
				input.Network.Defaults.LossRate = math.Inf(1)
			},
			errorFragment: "network.defaults.lossRate",
		},

		// Load balancer settings.
		{
			name: "budget",
			change: func(input *Scenario) {
				input.Resources.LoadBalancer.ForwardingBitsPerSecond = 0
			},
			errorFragment: "resources.loadBalancer.forwardingBitsPerSecond",
		},
		{
			name: "algorithm",
			change: func(input *Scenario) {
				input.Resources.LoadBalancer.Algorithm = "unknown"
			},
			errorFragment: "resources.loadBalancer.algorithm",
		},

		// Client arrival and stream settings.
		{
			name: "negative clients",
			change: func(input *Scenario) {
				input.Workload.ClientGroups[0].Count = -1
			},
			errorFragment: "clientGroups[0].count",
		},
		{
			name: "too many clients",
			change: func(input *Scenario) {
				input.Workload.ClientGroups[0].Count = MaxClients + 1
			},
			errorFragment: "clientGroups[0].count",
		},
		{
			name: "client start",
			change: func(input *Scenario) {
				input.Workload.ClientGroups[0].StartAt = -1
			},
			errorFragment: "clientGroups[0].startAtNanoseconds",
		},
		{
			name: "client gap",
			change: func(input *Scenario) {
				input.Workload.ClientGroups[0].ClientGap = -1
			},
			errorFragment: "clientGroups[0].clientGapNanoseconds",
		},
		{
			name: "packets despite explicit bytes",
			change: func(input *Scenario) {
				input.Workload.Defaults.Packets = -1
				input.Workload.Defaults.Bytes = 0
			},
			errorFragment: "workload.defaults.packets",
		},
		{
			name: "bytes",
			change: func(input *Scenario) {
				input.Workload.Defaults.Bytes = -2
			},
			errorFragment: "workload.defaults.bytes",
		},
		{
			name: "packet gap",
			change: func(input *Scenario) {
				input.Workload.Defaults.PacketGap = -1
			},
			errorFragment: "workload.defaults.packetGapNanoseconds",
		},

		// Identifiers and backend endpoints.
		{
			name: "lb id",
			change: func(input *Scenario) {
				input.Resources.LoadBalancer.ID = "bad/id"
			},
			errorFragment: "resources.loadBalancer.id",
		},
		{
			name: "backend id collision",
			change: func(input *Scenario) {
				input.Resources.Backends[0].ID = "lb"
			},
			errorFragment: "already used",
		},
		{
			name: "group id collision",
			change: func(input *Scenario) {
				input.Workload.ClientGroups[0].ID = "backend"
			},
			errorFragment: "already used",
		},
		{
			name: "empty address",
			change: func(input *Scenario) {
				input.Resources.Backends[0].Address = ""
			},
			errorFragment: "backends[0].address",
		},
		{
			name: "port",
			change: func(input *Scenario) {
				input.Resources.Backends[0].Port = 0
			},
			errorFragment: "backends[0].port",
		},

		// Endpoint profiles and client-specific overrides.
		{
			name: "unknown backend profile",
			change: func(input *Scenario) {
				input.Resources.Backends[0].Network.Profile = "missing"
			},
			errorFragment: "backends[0].network.profile",
		},
		{
			name: "unknown group profile",
			change: func(input *Scenario) {
				input.Workload.ClientGroups[0].Network.Profile = "missing"
			},
			errorFragment: "clientGroups[0].network.profile",
		},
		{
			name: "directional bandwidth",
			change: func(input *Scenario) {
				group := &input.Workload.ClientGroups[0]
				group.Network.ToLoadBalancer.BandwidthBitsPerSecond = pointer[int64](0)
			},
			errorFragment: "toLoadBalancer.bandwidthBitsPerSecond",
		},
		{
			name: "invalid override index",
			change: func(input *Scenario) {
				group := &input.Workload.ClientGroups[0]
				group.Overrides = []ClientOverride{
					{Index: 10},
				}
			},
			errorFragment: "overrides[0].index",
		},
		{
			name: "duplicate override",
			change: func(input *Scenario) {
				group := &input.Workload.ClientGroups[0]
				group.Overrides = []ClientOverride{
					{Index: 2},
					{Index: 2},
				}
			},
			errorFragment: "overrides[1].index",
		},
		{
			name: "invalid client stream",
			change: func(input *Scenario) {
				group := &input.Workload.ClientGroups[0]
				group.Overrides = []ClientOverride{
					{
						Index: 2,
						Stream: StreamPatch{
							Packets: pointer(-1),
						},
					},
				}
			},
			errorFragment: "overrides[0].stream.packets",
		},
		{
			name: "unknown client profile",
			change: func(input *Scenario) {
				group := &input.Workload.ClientGroups[0]
				group.Overrides = []ClientOverride{
					{
						Index: 2,
						Network: EndpointNetworkConfig{
							Profile: "missing",
						},
					},
				}
			},
			errorFragment: "overrides[0].network.profile",
		},
		{
			name: "unused invalid profile",
			change: func(input *Scenario) {
				input.Network.Profiles = []LinkProfile{
					{
						ID: "unused",
						Config: LinkPatch{
							QueueCapacity: pointer(-1),
						},
					},
				}
			},
			errorFragment: "profiles[0].config.queueCapacity",
		},
		{
			name: "duplicate profile",
			change: func(input *Scenario) {
				input.Network.Profiles = []LinkProfile{
					{ID: "same"},
					{ID: "same"},
				}
			},
			errorFragment: "duplicate profile",
		},
		{
			name: "too many backends",
			change: func(input *Scenario) {
				input.Resources.Backends = make([]BackendConfig, MaxBackends+1)
			},
			errorFragment: "resources.backends: count",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Change one setting in an otherwise valid scenario.
			input := exampleScenario()
			test.change(&input)

			_, err := Normalize(input)
			if err == nil {
				t.Fatal("invalid scenario accepted")
			}

			// The error should identify the setting that failed validation.
			if !strings.Contains(err.Error(), test.errorFragment) {
				t.Fatalf("expected error containing %q, got %v", test.errorFragment, err)
			}
		})
	}
}

func TestDefaultsValidatedEvenWithoutClientsOrBackends(t *testing.T) {
	// Invalid stream defaults remain invalid even when no client will use them.
	input := exampleScenario()
	input.Resources.Backends = nil
	input.Workload.ClientGroups = []ClientGroup{
		{
			ID:    "empty",
			Count: 0,
		},
	}

	input.Workload.Defaults.Packets = -1
	if _, err := Normalize(input); err == nil {
		t.Fatal("invalid default stream accepted for an empty group")
	}

	// Fix the stream, then check unused link defaults independently.
	input.Workload.Defaults.Packets = 0
	input.Network.Defaults.BandwidthBitsPerSecond = 0

	if _, err := Normalize(input); err == nil {
		t.Fatal("invalid default link accepted without endpoints")
	}
}

func TestTotalClientCountAcrossGroups(t *testing.T) {
	// Each group fits on its own, but their sum exceeds the client limit.
	input := exampleScenario()
	input.Workload.ClientGroups = []ClientGroup{
		{
			ID:    "a",
			Count: MaxClients,
		},
		{
			ID:    "b",
			Count: 1,
		},
	}

	if _, err := Normalize(input); err == nil {
		t.Fatal("sum of client counts exceeded the address space")
	}
}

func TestNativeJSONRoundTripRetainsNanosecondsAndFullWidthIntegers(t *testing.T) {
	// Use a duration beyond JavaScript's exact integer range.
	input := exampleScenario()
	input.Execution.Duration = time.Duration(1<<53 + 1)
	input.Execution.Seed = math.MinInt64
	input.Execution.MaxEvents = math.MaxUint64
	input.Network.Defaults.PacketTravelTime = 123456789
	input.Resources.LoadBalancer.Ingress.PacketTravelTime = time.Duration(1<<53 + 3)

	group := &input.Workload.ClientGroups[0]
	group.Network.ToLoadBalancer.QueueCapacity = pointer(0)

	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}

	// Native JSON encoding must keep the original integer, without rounding.
	expectedDuration := "\"durationNanoseconds\":9007199254740993"
	if !strings.Contains(string(data), expectedDuration) {
		t.Fatalf("duration not encoded as exact integer nanoseconds: %s", data)
	}

	var restored Scenario
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(input, restored) {
		t.Fatal("native JSON round trip changed the scenario")
	}

	// The restored document must still satisfy all configuration rules.
	if _, err := Normalize(restored); err != nil {
		t.Fatal(err)
	}
}

// TestDefaultIngressConfigReturnsIndependentValues fixes the shared Go defaults.
func TestDefaultIngressConfigReturnsIndependentValues(t *testing.T) {
	want := LinkConfig{
		BandwidthBitsPerSecond: 1_000_000_000,
		QueueCapacity:          1024,
	}
	first := DefaultIngressConfig()
	if first != want {
		t.Fatalf("unexpected shared ingress defaults: %+v", first)
	}
	first.QueueCapacity = 0
	if DefaultIngressConfig() != want {
		t.Fatal("changing one default configuration changed later callers")
	}
}

// TestIngressDoesNotInheritEndpointProfiles checks the complete shared input config.
func TestIngressDoesNotInheritEndpointProfiles(t *testing.T) {
	input := exampleScenario()
	input.Resources.LoadBalancer.Ingress = LinkConfig{
		BandwidthBitsPerSecond: 300_000_000,
		PacketTravelTime:       13 * time.Nanosecond,
		QueueCapacity:          0,
		LossRate:               1,
	}
	input.Network.Profiles = []LinkProfile{
		{
			ID: "endpoint",
			Config: LinkPatch{
				BandwidthBitsPerSecond: pointer[int64](200_000_000),
				PacketTravelTime:       pointer(7 * time.Nanosecond),
			},
		},
	}
	input.Resources.Backends[0].Network.Profile = "endpoint"
	input.Workload.ClientGroups[0].Network.Profile = "endpoint"
	resolved, err := Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.LoadBalancer.Ingress != input.Resources.LoadBalancer.Ingress {
		t.Fatal("endpoint defaults or profiles changed the shared input")
	}
	if resolved.Backends[0].Network.ToLoadBalancer.BandwidthBitsPerSecond != 200_000_000 {
		t.Fatal("the independent endpoint profile was not applied")
	}
	input.Resources.LoadBalancer.Ingress.LossRate = 0
	if resolved.LoadBalancer.Ingress.LossRate != 1 {
		t.Fatal("normalized ingress aliases the input configuration")
	}
}

// TestIngressValidationRequiresACompleteLink also validates scenarios with no endpoints.
func TestIngressValidationRequiresACompleteLink(t *testing.T) {
	cases := []struct {
		name   string
		change func(*LinkConfig)
		field  string
	}{
		{
			name:   "missing ingress",
			change: func(config *LinkConfig) { *config = LinkConfig{} },
			field:  "bandwidthBitsPerSecond",
		},
		{
			name:   "negative bandwidth",
			change: func(config *LinkConfig) { config.BandwidthBitsPerSecond = -1 },
			field:  "bandwidthBitsPerSecond",
		},
		{
			name:   "negative travel",
			change: func(config *LinkConfig) { config.PacketTravelTime = -1 },
			field:  "packetTravelTimeNanoseconds",
		},
		{
			name:   "negative queue",
			change: func(config *LinkConfig) { config.QueueCapacity = -1 },
			field:  "queueCapacity",
		},
		{
			name:   "negative loss",
			change: func(config *LinkConfig) { config.LossRate = -0.1 },
			field:  "lossRate",
		},
		{
			name:   "high loss",
			change: func(config *LinkConfig) { config.LossRate = 1.1 },
			field:  "lossRate",
		},
		{
			name:   "NaN loss",
			change: func(config *LinkConfig) { config.LossRate = math.NaN() },
			field:  "lossRate",
		},
		{
			name:   "infinite loss",
			change: func(config *LinkConfig) { config.LossRate = math.Inf(1) },
			field:  "lossRate",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := exampleScenario()
			input.Resources.Backends = nil
			input.Workload.ClientGroups = nil
			test.change(&input.Resources.LoadBalancer.Ingress)
			_, err := Normalize(input)
			path := "resources.loadBalancer.ingress." + test.field
			if err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("expected ingress validation error for %s, got %v", path, err)
			}
		})
	}
}
