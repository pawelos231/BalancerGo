package simulation

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"

	"load-balancer/model"
	"load-balancer/network"
	"load-balancer/scenario"
)

// scenarioRuntimeDefinition supplies a small, fast native scenario for these tests.
func scenarioRuntimeDefinition() scenario.Scenario {
	definition := baseScenario()
	definition.Execution.Duration = 500 * time.Millisecond
	definition.Network.Defaults = scenario.LinkConfig{
		BandwidthBitsPerSecond: 8_000_000,
		QueueCapacity:          32,
	}
	definition.Resources.LoadBalancer.Ingress = definition.Network.Defaults
	definition.Resources.LoadBalancer.ForwardingBitsPerSecond = math.MaxInt64
	definition.Workload.Defaults = scenario.StreamConfig{Bytes: 0}
	definition.Workload.ClientGroups = []scenario.ClientGroup{{ID: "clients", Count: 1}}
	return definition
}

// scenarioRuntimeValue creates a pointer for an explicit scenario override.
func scenarioRuntimeValue[T any](value T) *T { return &value }

// normalizedRuntimeScenario validates the document before creating its runtime.
func normalizedRuntimeScenario(t *testing.T, definition scenario.Scenario) scenario.Normalized {
	t.Helper()
	resolved, err := scenario.Normalize(definition)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// scenarioRuntimeLink locates a physical path by its actual graph endpoints.
func scenarioRuntimeLink(t *testing.T, s *World, source, target string) namedLink {
	t.Helper()
	for _, link := range s.links {
		if link.Source == source && link.Target == target {
			return link
		}
	}
	t.Fatalf("missing link from %q to %q", source, target)
	return namedLink{}
}

// scenarioRuntimeClient locates a created client by its stable scenario ID.
func scenarioRuntimeClient(t *testing.T, s *World, id string) runtimeClient {
	t.Helper()
	for _, cl := range s.clients {
		if cl.ID == id {
			return cl
		}
	}
	t.Fatalf("missing runtime client %q", id)
	return runtimeClient{}
}

// assertRuntimeLinkConfig checks that normalization reaches the physical model.
func assertRuntimeLinkConfig(t *testing.T, link namedLink, want scenario.LinkConfig) {
	t.Helper()
	expected := network.Config{
		BandwidthBitsPerSecond: want.BandwidthBitsPerSecond,
		PacketTravelTime:       want.PacketTravelTime,
		QueueCapacity:          want.QueueCapacity,
		LossRate:               want.LossRate,
	}
	if got := link.Link.Config(); got != expected {
		t.Fatalf("link %q config = %+v, want %+v", link.ID, got, expected)
	}
}

// TestScenarioRuntimeGroupsAreLazy checks compact groups without client expansion.
func TestScenarioRuntimeGroupsAreLazy(t *testing.T) {
	definition := scenarioRuntimeDefinition()
	definition.Workload.ClientGroups = []scenario.ClientGroup{{
		ID:      "future",
		Count:   1,
		StartAt: time.Second,
	}}
	small := New(normalizedRuntimeScenario(t, definition))
	definition.Workload.ClientGroups[0].Count = scenario.MaxClients
	large := New(normalizedRuntimeScenario(t, definition))

	// Changing only the planned count must not allocate clients, links or events.
	if len(large.clients) != 0 || large.createdClients != 0 {
		t.Fatal("future clients were constructed before their arrival time")
	}
	if large.scheduledClients != scenario.MaxClients {
		t.Fatal("planned client count was lost")
	}
	if len(large.links) != len(small.links) || large.engine.Pending() != small.engine.Pending() {
		t.Fatal("client count expanded into upfront links or individual events")
	}
	runIngressUntil(t, large, definition.Execution.Duration)
	if len(large.clients) != 0 {
		t.Fatal("clients scheduled beyond the horizon were created")
	}
}

// TestScenarioRuntimeUsesDirectionalProfilesAndArrivalTimes checks native settings.
func TestScenarioRuntimeUsesDirectionalProfilesAndArrivalTimes(t *testing.T) {
	definition := scenarioRuntimeDefinition()
	definition.Workload.Defaults.Bytes = 64
	definition.Network.Profiles = []scenario.LinkProfile{
		{
			ID: "slow-client",
			Config: scenario.LinkPatch{
				BandwidthBitsPerSecond: scenarioRuntimeValue[int64](16_000),
				PacketTravelTime:       scenarioRuntimeValue(2 * time.Millisecond),
				QueueCapacity:          scenarioRuntimeValue(9),
			},
		},
		{
			ID: "backend",
			Config: scenario.LinkPatch{
				BandwidthBitsPerSecond: scenarioRuntimeValue[int64](256_000),
				QueueCapacity:          scenarioRuntimeValue(1),
			},
		},
	}
	definition.Resources.Backends[0].Network = scenario.EndpointNetworkConfig{
		ToLoadBalancer: scenario.LinkPatch{
			BandwidthBitsPerSecond: scenarioRuntimeValue[int64](128_000),
			PacketTravelTime:       scenarioRuntimeValue(7 * time.Millisecond),
		},
		FromLoadBalancer: scenario.LinkPatch{
			BandwidthBitsPerSecond: scenarioRuntimeValue[int64](64_000),
			PacketTravelTime:       scenarioRuntimeValue(3 * time.Millisecond),
		},
	}
	definition.Resources.Backends[1].Network = scenario.EndpointNetworkConfig{
		Profile: "backend",
		ToLoadBalancer: scenario.LinkPatch{
			BandwidthBitsPerSecond: scenarioRuntimeValue[int64](32_000),
		},
	}
	definition.Workload.ClientGroups = []scenario.ClientGroup{
		{
			ID:        "slow",
			Count:     2,
			StartAt:   10 * time.Millisecond,
			ClientGap: 20 * time.Millisecond,
			Network:   scenario.EndpointNetworkConfig{Profile: "slow-client"},
			Stream: scenario.StreamPatch{
				PacketGap: scenarioRuntimeValue(2 * time.Millisecond),
			},
			Overrides: []scenario.ClientOverride{{
				Index: 1,
				Network: scenario.EndpointNetworkConfig{
					ToLoadBalancer: scenario.LinkPatch{
						BandwidthBitsPerSecond: scenarioRuntimeValue[int64](32_000),
					},
					FromLoadBalancer: scenario.LinkPatch{
						PacketTravelTime: scenarioRuntimeValue(time.Duration(0)),
					},
				},
				Stream: scenario.StreamPatch{
					Bytes:     scenarioRuntimeValue(128),
					PacketGap: scenarioRuntimeValue(time.Duration(0)),
				},
			}},
		},
		{
			ID:      "fast",
			Count:   1,
			StartAt: 5 * time.Millisecond,
			Stream:  scenario.StreamPatch{Bytes: scenarioRuntimeValue(0)},
		},
	}
	resolved := normalizedRuntimeScenario(t, definition)
	s := New(resolved)
	initialLinks := len(s.links)

	// No client or private link exists before its exact group arrival time.
	runIngressUntil(t, s, 5*time.Millisecond-time.Nanosecond)
	if len(s.clients) != 0 || len(s.links) != initialLinks {
		t.Fatal("group arrival created a client or links too early")
	}
	runIngressUntil(t, s, 5*time.Millisecond)
	if len(s.clients) != 1 || s.clients[0].ID != "client/fast/0" {
		t.Fatal("earlier group did not create its first client at 5 ms")
	}
	runIngressUntil(t, s, 10*time.Millisecond)
	if len(s.clients) != 2 || s.clients[1].ID != "client/slow/0" {
		t.Fatal("second group did not start at 10 ms")
	}
	runIngressUntil(t, s, 30*time.Millisecond-time.Nanosecond)
	if len(s.clients) != 2 {
		t.Fatal("client gap did not delay the second client")
	}
	runIngressUntil(t, s, 30*time.Millisecond)
	if len(s.clients) != 3 || s.clients[2].ID != "client/slow/1" {
		t.Fatal("second client did not arrive after its configured gap")
	}

	// Each endpoint direction receives its own resolved physical settings.
	for _, backend := range resolved.Backends {
		id := BackendNodeID(backend.ID)
		incoming := scenarioRuntimeLink(t, s, id, LoadBalancerNodeID)
		outgoing := scenarioRuntimeLink(t, s, LoadBalancerNodeID, id)
		assertRuntimeLinkConfig(t, incoming, backend.Network.ToLoadBalancer)
		assertRuntimeLinkConfig(t, outgoing, backend.Network.FromLoadBalancer)
	}
	for _, group := range resolved.ClientGroups {
		for index := 0; index < group.Count; index++ {
			want, err := group.Client(index)
			if err != nil {
				t.Fatal(err)
			}
			got := scenarioRuntimeClient(t, s, want.ID)
			if got.Config != want || got.GroupID != group.ID || got.Index != index {
				t.Fatalf("client %q lost its group or individual settings", want.ID)
			}
			incoming := scenarioRuntimeLink(t, s, want.ID, LoadBalancerNodeID)
			outgoing := scenarioRuntimeLink(t, s, LoadBalancerNodeID, want.ID)
			assertRuntimeLinkConfig(t, incoming, want.Network.ToLoadBalancer)
			assertRuntimeLinkConfig(t, outgoing, want.Network.FromLoadBalancer)
		}
	}
	if len(s.links) != initialLinks+2*len(s.clients) {
		t.Fatal("clients did not receive exactly two private links each")
	}

	// The first slow SYN needs 20 ms transmission plus 2 ms travel after 10 ms.
	slowAccess := scenarioRuntimeLink(t, s, "client/slow/0", LoadBalancerNodeID)
	overriddenAccess := scenarioRuntimeLink(t, s, "client/slow/1", LoadBalancerNodeID)
	runIngressUntil(t, s, 32*time.Millisecond-time.Nanosecond)
	if slowAccess.Stats().DeliveredPackets != 0 || overriddenAccess.Stats().DeliveredPackets != 0 {
		t.Fatal("a slow client SYN arrived before its private link finished")
	}
	runIngressUntil(t, s, 32*time.Millisecond)
	if slowAccess.Stats().DeliveredPackets != 1 || slowAccess.Stats().DeliveredBytes != 40 {
		t.Fatal("profile bandwidth did not deliver the first complete SYN at 32 ms")
	}

	// The override halves serialization to 10 ms for the client created at 30 ms.
	runIngressUntil(t, s, 42*time.Millisecond-time.Nanosecond)
	if overriddenAccess.Stats().DeliveredPackets != 0 {
		t.Fatal("individual bandwidth override completed before transmission and travel")
	}
	runIngressUntil(t, s, 42*time.Millisecond)
	if overriddenAccess.Stats().DeliveredPackets != 1 || overriddenAccess.Stats().DeliveredBytes != 40 {
		t.Fatal("individual bandwidth override did not deliver its SYN at 42 ms")
	}

	// Completion bytes must use each client's stream, including the explicit zero.
	if err := runWorld(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	report := readWorld(s)
	if report.CompletedStreams != 3 || report.ClosedClients != 3 {
		t.Fatalf("heterogeneous streams did not finish: %+v", report)
	}
	if report.CompletedPayloadBytes != 192 {
		t.Fatalf("completed payload bytes = %d, want 64 + 128 + 0", report.CompletedPayloadBytes)
	}
}

// TestScenarioRuntimeSharesBudgetAcrossClientAndBackendOutputs uses real TCP packets.
func TestScenarioRuntimeSharesBudgetAcrossClientAndBackendOutputs(t *testing.T) {
	definition := scenarioRuntimeDefinition()
	definition.Workload.ClientGroups[0].Count = 2
	definition.Resources.LoadBalancer.ForwardingBitsPerSecond = 320_000
	s := New(normalizedRuntimeScenario(t, definition))

	// The first SYN leaves the LB at 80 us. Its next grants are spaced by 1 ms.
	thirdGrant := 2080 * time.Microsecond
	runIngressUntil(t, s, thirdGrant-time.Nanosecond)
	if s.egress.Stats().ForwardedPackets != 2 {
		t.Fatal("independent outputs multiplied the shared forwarding budget")
	}
	runIngressUntil(t, s, thirdGrant)
	stats := s.egress.Stats()
	if stats.ForwardedPackets != 3 || stats.ForwardedBytes != 120 {
		t.Fatalf("shared grants did not count complete wire packets: %+v", stats)
	}

	// Two backend SYNs and one client SYN-ACK consume the very same budget.
	var backendStarts, clientStarts uint64
	for _, link := range s.links {
		if link.Output == nil {
			continue
		}
		physicalStarts := link.Link.Stats().AcceptedPackets
		for _, cl := range s.clients {
			if link.Target == cl.ID {
				clientStarts += physicalStarts
			}
		}
		for _, backend := range s.scenario.Backends {
			if link.Target == BackendNodeID(backend.ID) {
				backendStarts += physicalStarts
			}
		}
	}
	if backendStarts != 2 || clientStarts != 1 {
		t.Fatalf("shared output starts: backends=%d clients=%d", backendStarts, clientStarts)
	}
}

// TestScenarioRuntimeSpawnUsesExplicitGroupSettings keeps initial and spawned IDs apart.
func TestScenarioRuntimeSpawnUsesExplicitGroupSettings(t *testing.T) {
	definition := scenarioRuntimeDefinition()
	definition.Workload.ClientGroups = []scenario.ClientGroup{
		{
			ID:        "alpha",
			Count:     1,
			StartAt:   50 * time.Millisecond,
			ClientGap: 10 * time.Millisecond,
			Stream:    scenario.StreamPatch{Bytes: scenarioRuntimeValue(11)},
			Overrides: []scenario.ClientOverride{{
				Index:  0,
				Stream: scenario.StreamPatch{Bytes: scenarioRuntimeValue(7)},
			}},
		},
		{
			ID:      "beta",
			Count:   1,
			StartAt: 50 * time.Millisecond,
			Stream:  scenario.StreamPatch{Bytes: scenarioRuntimeValue(5)},
		},
	}
	s := New(normalizedRuntimeScenario(t, definition))
	initialEvents := s.engine.Pending()

	invalid := []struct {
		count   int
		groupID string
	}{{2, ""}, {2, "unknown"}, {0, "alpha"}, {10001, "alpha"}}
	// Rejected typed operations leave planned clients and arrival events unchanged.
	for _, request := range invalid {
		if err := s.SpawnClients(request.count, request.groupID); err == nil {
			t.Fatalf("invalid spawn accepted: %+v", request)
		}
		if s.scheduledClients != 2 || s.engine.Pending() != initialEvents {
			t.Fatal("rejected spawn changed the scenario schedule")
		}
	}
	if err := s.SpawnClients(2, "alpha"); err != nil {
		t.Fatal(err)
	}

	// Spawned clients use new indices immediately; initial index zero still waits.
	runIngressUntil(t, s, 0)
	if len(s.clients) != 1 || s.clients[0].ID != "client/alpha/1" {
		t.Fatal("spawn reused an initial identity or waited for the group's StartAt")
	}
	runIngressUntil(t, s, 10*time.Millisecond)
	if len(s.clients) != 2 || s.clients[1].ID != "client/alpha/2" {
		t.Fatal("spawned batch ignored the group's client gap")
	}
	runIngressUntil(t, s, 50*time.Millisecond)
	if len(s.clients) != 4 || s.scheduledClients != 4 {
		t.Fatal("initial and spawned batches did not remain independent")
	}
	if s.scenario.ClientGroups[0].Count != 1 {
		t.Fatal("spawn mutated the source scenario's original client count")
	}

	// Reserved initial addresses cannot collide with earlier-created spawns.
	keys := make(map[model.ClientKey]bool)
	for _, cl := range s.clients {
		key := cl.Client.Key()
		if keys[key] {
			t.Fatalf("spawn reused a reserved flow identity: %+v", key)
		}
		keys[key] = true
	}
	spawned := scenarioRuntimeClient(t, s, "client/alpha/1")
	if spawned.Config.Stream.Bytes != 11 {
		t.Fatal("spawn inherited an unrelated individual override")
	}
	if err := runWorld(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if readWorld(s).CompletedPayloadBytes != 34 {
		t.Fatal("completed payload did not use both initial and spawned stream settings")
	}
}

// TestScenarioRuntimeSpawnAllowsCountOnlyForOneGroup preserves the current UI command.
func TestScenarioRuntimeSpawnAllowsCountOnlyForOneGroup(t *testing.T) {

	definition := baseScenario()
	definition.Workload.ClientGroups[0].Count = 0
	definition.Resources.Backends = nil
	s := New(normalizedRuntimeScenario(t, definition))
	if err := s.SpawnClients(2, ""); err != nil {
		t.Fatal(err)
	}

	runIngressUntil(t, s, 0)
	if len(s.clients) != 2 || s.scheduledClients != 2 {
		t.Fatal("single empty group did not accept the existing count-only spawn command")
	}
	if s.clients[0].ID != "client/clients/0" || s.clients[1].ID != "client/clients/1" {
		t.Fatal("count-only spawn did not use stable group identities")
	}
	firstKey := s.clients[0].Client.Key()
	if firstKey.SrcIP != "192.0.0.1" || firstKey.SrcPort != 40001 {
		t.Fatal("the first legacy client lost its original address and source port")
	}
}

// TestScenarioRuntimeTopologyNamespaces avoids collisions with user resource names.
func TestScenarioRuntimeTopologyNamespaces(t *testing.T) {
	definition := scenarioRuntimeDefinition()
	definition.Resources.LoadBalancer.ID = "edge"
	definition.Resources.Backends[0].ID = "lb"
	definition.Resources.Backends[1].ID = "upstream"
	s := New(normalizedRuntimeScenario(t, definition))
	runIngressUntil(t, s, 0)

	ids := map[string]bool{LoadBalancerNodeID: true}
	addID := func(id string) {
		if ids[id] {
			t.Fatalf("duplicate world node id: %s", id)
		}
		ids[id] = true
	}
	s.VisitBackends(func(backend BackendObservation) { addID(BackendNodeID(backend.State.ID)) })
	s.VisitClients(func(client ClientObservation) { addID(client.ID) })

	expected := []string{
		LoadBalancerNodeID,
		"backend/lb",
		"backend/upstream",
		"client/clients/0",
	}
	if len(ids) != len(expected) {
		t.Fatalf("unexpected topology nodes: %v", ids)
	}
	for _, id := range expected {
		if !ids[id] {
			t.Fatalf("missing namespaced node %q", id)
		}
	}
	s.VisitLinks(func(link LinkObservation) {
		if !ids[link.Source] || !ids[link.Target] {
			t.Fatalf("link %q references a missing endpoint", link.ID)
		}
	})
	if s.scenario.LoadBalancer.ID != "edge" {
		t.Fatal("infrastructure namespacing replaced the configured LB resource id")
	}
}

// resourceObservations retains each client's loss sequence and reserved address.
type resourceObservations struct {
	stats map[string][]network.Stats
	keys  map[string]model.ClientKey
}

// observeScenarioResources exercises production client senders without backend replies.
func observeScenarioResources(
	t *testing.T,
	definition scenario.Scenario,
	inspectSnapshots bool,
) resourceObservations {
	t.Helper()
	s := New(normalizedRuntimeScenario(t, definition))
	for _, srv := range s.lb.GetServers() {
		srv.MarkFailed()
	}
	runIngressUntil(t, s, 0)
	observed := resourceObservations{
		stats: make(map[string][]network.Stats),
		keys:  make(map[string]model.ClientKey),
	}
	for _, cl := range s.clients {
		observed.keys[cl.ID] = cl.Client.Key()
		cl.Client.FloodSyn(32, time.Millisecond)
	}

	// Sampling must not advance any resource's private random sequence.
	for index := 0; index < 32; index++ {
		until := time.Duration(index)*time.Millisecond + 500*time.Microsecond
		runIngressUntil(t, s, until)
		if inspectSnapshots {
			readWorld(s)
		}
		for _, cl := range s.clients {
			link := scenarioRuntimeLink(t, s, cl.ID, LoadBalancerNodeID)
			observed.stats[cl.ID] = append(observed.stats[cl.ID], link.Stats())
		}
	}
	return observed
}

// TestScenarioRuntimeResourceRNGSurvivesDeclarationChanges checks stable IDs and seeds.
func TestScenarioRuntimeResourceRNGSurvivesDeclarationChanges(t *testing.T) {
	definition := scenarioRuntimeDefinition()
	definition.Network.Defaults.LossRate = 0.5
	definition.Workload.ClientGroups = []scenario.ClientGroup{
		{ID: "alpha", Count: 1},
		{ID: "beta", Count: 1},
	}
	first := observeScenarioResources(t, definition, false)

	// Move declarations and create an unrelated resource before the same clients.
	definition.Workload.ClientGroups[0], definition.Workload.ClientGroups[1] =
		definition.Workload.ClientGroups[1], definition.Workload.ClientGroups[0]
	definition.Resources.Backends[0], definition.Resources.Backends[1] =
		definition.Resources.Backends[1], definition.Resources.Backends[0]
	definition.Resources.Backends = append(definition.Resources.Backends, scenario.BackendConfig{
		ID:      "unused",
		Address: "10.0.0.3",
		Port:    80,
	})
	second := observeScenarioResources(t, definition, true)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("resource order or backend count changed client addresses or loss sequences")
	}
}
