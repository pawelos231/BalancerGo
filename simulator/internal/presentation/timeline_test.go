package presentation

import (
	"testing"
	"time"

	"load-balancer/internal/contract"
	"load-balancer/internal/simulation"
	"load-balancer/scenario"
)

// Both backend labels change at 250 ms while the same clients and links remain.
func timelineProjectionWorld(t *testing.T) *simulation.World {
	t.Helper()
	return timelineProjectionFrom(t, timelineProjectionDefinition())
}

// timelineProjectionDefinition provides independent state collections for each test.
func timelineProjectionDefinition() scenario.ScenarioV2 {
	base := projectionScenario()
	initial := scenario.StateConfig{
		Resources: scenario.StateResourcesConfig{
			LoadBalancer: scenario.LoadBalancerStateConfig{
				Algorithm:               base.Resources.LoadBalancer.Algorithm,
				Ingress:                 base.Resources.LoadBalancer.Ingress,
				ForwardingBitsPerSecond: base.Resources.LoadBalancer.ForwardingBitsPerSecond,
			},
			Backends: []scenario.BackendStateConfig{
				{ID: "one", Name: "Old one", Address: "10.0.0.1", Port: 80},
				{ID: "two", Name: "Old two", Address: "10.0.0.2", Port: 80},
			},
		},
		Workload: scenario.StateWorkloadConfig{ClientGroups: base.Workload.ClientGroups},
	}
	next := initial
	next.Resources.Backends = append([]scenario.BackendStateConfig(nil), initial.Resources.Backends...)
	next.Resources.Backends[0].Name = "New one"
	next.Resources.Backends[1].Name = "New two"

	return scenario.ScenarioV2{
		Version: scenario.VersionV2,
		Fixed: scenario.FixedConfig{
			Execution:    base.Execution,
			LoadBalancer: scenario.LoadBalancerIdentity{ID: base.Resources.LoadBalancer.ID},
			Network:      base.Network,
			Workload:     scenario.WorkloadDefaultsConfig{Defaults: base.Workload.Defaults},
		},
		States: map[string]scenario.StateConfig{
			"0": initial, "250000000": next,
		},
	}
}

func timelineProjectionFrom(t *testing.T, input scenario.ScenarioV2) *simulation.World {
	t.Helper()
	plan, err := scenario.CompileV2(input)
	if err != nil {
		t.Fatal(err)
	}
	world, err := simulation.NewTimeline(plan)
	if err != nil {
		t.Fatal(err)
	}
	return world
}

// Availability, admission and a resource cohort publish together without changing old graphs.
func TestTimelineAvailabilityPublishesIndependentPhysicalAdministrativeAndObservedState(t *testing.T) {
	input := timelineProjectionDefinition()
	next := input.States["250000000"]
	available := false
	drain := scenario.BackendAdminDrain
	next.Resources.Backends[0].PhysicalAvailable = &available
	next.Resources.Backends[0].AdministrativeState = &drain
	bandwidth := int64(5_000_000)
	next.Resources.Backends[0].Network.FromLoadBalancer.BandwidthBitsPerSecond = &bandwidth
	cores, cost, queue := 2, time.Microsecond, 4
	next.Resources.Backends[0].Host = scenario.HostConfig{
		Cores: &cores, PacketProcessingTime: &cost, QueueCapacity: &queue,
	}
	next.Resources.Backends[1] = scenario.BackendStateConfig{
		ID: "three", Name: "New three", Address: "10.0.0.3", Port: 80,
		PhysicalAvailable: &available, AdministrativeState: &drain,
	}
	input.States["250000000"] = next
	world := timelineProjectionFrom(t, input)
	advanceProjection(t, world, 0)
	projector := NewProjector("availability")
	first := project(t, projector, world)
	oldDocument := jsonProjection(t, first.Document)
	advanceProjection(t, world, 250*time.Millisecond)
	frame, document, err := BuildSnapshot(world, projector, RunState{Status: "running", RunID: "availability"})
	if err != nil || document == nil || document.Revision != "2" || frame.Topology.Revision != "2" ||
		world.Info().ConfigurationRevision != 2 {
		t.Fatalf("availability cohort did not publish one complete revision: %v", err)
	}
	seen := 0
	for _, node := range frame.Topology.Nodes {
		if node.ID != simulation.BackendNodeID("one") && node.ID != simulation.BackendNodeID("three") {
			continue
		}
		seen++
		if node.Active == nil || *node.Active || node.Metrics["physicalAvailable"] != 0 ||
			node.Metrics["administrativeDrain"] != 1 || node.Metrics["observedHealthy"] != 1 {
			t.Fatalf("backend status dimensions were mixed: %+v", node)
		}
	}
	if seen != 2 || document.Servers[0].Name != "New one" || document.Servers[1].ID != "three" {
		t.Fatal("status publication omitted a retained or inserted backend")
	}
	assertHostDefinition(t, document, simulation.BackendNodeID("one"), 1, 2, 1, 4)
	foundLink := false
	for _, link := range document.Links {
		if link.ID == "link/backend/one/from-lb" {
			foundLink = link.Config.BandwidthBitsPerSecond == 5_000_000
		}
	}
	if !foundLink || jsonProjection(t, first.Document) != oldDocument || project(t, projector, world).Document != nil {
		t.Fatal("compound commit lost its link edit, rewrote old definitions or invalidated the new cache")
	}
}

// Membership and retained-resource edits publish together without rewriting old graphs.
func TestTimelineMembershipPublishesCoherentTopology(t *testing.T) {
	input := timelineProjectionDefinition()
	next := input.States["250000000"]
	next.Resources.Backends[0] = scenario.BackendStateConfig{
		ID: "three", Name: "New three", Address: "10.0.0.3", Port: 80,
	}
	bandwidth := int64(5_000_000)
	next.Resources.Backends[1].Network.FromLoadBalancer = scenario.LinkPatch{
		BandwidthBitsPerSecond: &bandwidth,
	}
	cores, cost, queue := 2, time.Microsecond, 4
	next.Resources.Backends[1].Host = scenario.HostConfig{
		Cores: &cores, PacketProcessingTime: &cost, QueueCapacity: &queue,
	}
	next.Resources.LoadBalancer.Algorithm = "random"
	input.States["250000000"] = next
	world := timelineProjectionFrom(t, input)
	advanceProjection(t, world, 0)
	projector := NewProjector("membership")
	first := project(t, projector, world)
	oldDocument := jsonProjection(t, first.Document)
	advanceProjection(t, world, 250*time.Millisecond)
	frame, definition, err := BuildSnapshot(world, projector, RunState{
		Status: "running", RunID: "membership",
	})
	if err != nil || definition == nil || definition.Revision != "2" || frame.Topology.Revision != "2" {
		t.Fatalf("membership did not publish one complete new definition: %v", err)
	}
	if len(definition.Servers) != 2 || len(definition.Nodes) != len(first.Document.Nodes) ||
		len(definition.Links) != len(first.Document.Links) || world.Info().Algorithm != "random" {
		t.Fatal("membership or policy was only partially projected")
	}
	foundNew, foundRetained := false, false
	for _, node := range definition.Nodes {
		if node.ID == simulation.BackendNodeID("one") {
			t.Fatal("the removed backend remained in the graph")
		}
		foundNew = foundNew || node.ID == simulation.BackendNodeID("three")
		foundRetained = foundRetained || node.ID == simulation.BackendNodeID("two") && node.Label == "New two"
	}
	for _, link := range definition.Links {
		if link.Source == simulation.BackendNodeID("one") || link.Target == simulation.BackendNodeID("one") {
			t.Fatal("a historical link retained a graph endpoint that no longer exists")
		}
		if link.ID == "link/backend/two/from-lb" && link.Config.BandwidthBitsPerSecond != 5_000_000 {
			t.Fatal("the retained backend link missed its same-time edit")
		}
	}
	if !foundNew || !foundRetained || jsonProjection(t, first.Document) != oldDocument {
		t.Fatal("membership missed a backend or mutated the retained old definition")
	}
	if project(t, projector, world).Document != nil {
		t.Fatal("reading the committed graph created another definition revision")
	}
}

// Retained link IDs still need fresh definitions when their actual limits change.
func TestTimelineBackendLinkEditRefreshesTopologyConfiguration(t *testing.T) {
	input := timelineProjectionDefinition()
	next := input.States["250000000"]
	bandwidth := int64(1_000_000)
	travel := 5 * time.Millisecond
	next.Resources.Backends[0].Network.FromLoadBalancer = scenario.LinkPatch{
		BandwidthBitsPerSecond: &bandwidth,
		PacketTravelTime:       &travel,
	}
	input.States["250000000"] = next
	world := timelineProjectionFrom(t, input)
	advanceProjection(t, world, 0)
	projector := NewProjector("links")
	first := project(t, projector, world)
	oldDocument := jsonProjection(t, first.Document)

	advanceProjection(t, world, 250*time.Millisecond)
	frame, definition, err := BuildSnapshot(world, projector, RunState{Status: "running", RunID: "links"})
	if err != nil || definition == nil || definition.Revision != "2" || frame.Topology.Revision != "2" {
		t.Fatalf("the changed link did not produce a coherent new definition: %v", err)
	}
	found := false
	for _, link := range definition.Links {
		if link.ID == "link/backend/one/from-lb" {
			found = true
			if link.Config.BandwidthBitsPerSecond != bandwidth || link.Config.PacketTravelTimeMs != 5 {
				t.Fatalf("retained link definition still has its old settings: %+v", link.Config)
			}
		}
	}
	if !found || len(first.Document.Links) != len(definition.Links) {
		t.Fatal("a settings edit changed the graph's membership")
	}
	if jsonProjection(t, first.Document) != oldDocument {
		t.Fatal("the new link definition changed a retained old document")
	}
}

// Each shared-limit edit refreshes retained definitions before its first sample.
func TestTimelineSharedNetworkEditRefreshesTopologyConfiguration(t *testing.T) {
	for _, resource := range []string{"ingress", "forwarding"} {
		t.Run(resource, func(t *testing.T) {
			input := timelineProjectionDefinition()
			next := input.States["250000000"]
			next.Resources.Backends = input.States["0"].Resources.Backends
			if resource == "ingress" {
				next.Resources.LoadBalancer.Ingress = scenario.LinkConfig{
					BandwidthBitsPerSecond: 5_000_000, PacketTravelTime: 3 * time.Millisecond,
					QueueCapacity: 5, LossRate: 0.25, QueuePolicy: "threshold",
					CongestionThresholdPercent: 70, CongestionSignalProbability: 0.5,
				}
			} else {
				next.Resources.LoadBalancer.ForwardingBitsPerSecond = 5_000_000
			}
			input.States["250000000"] = next
			world := timelineProjectionFrom(t, input)
			advanceProjection(t, world, 0)
			projector := NewProjector("shared-limits")
			first := project(t, projector, world)
			oldDocument := jsonProjection(t, first.Document)

			advanceProjection(t, world, 250*time.Millisecond)
			frame, definition, err := BuildSnapshot(world, projector, RunState{
				Status: "running", RunID: "shared-limits",
			})
			if err != nil || definition == nil || definition.Revision != "2" || frame.Topology.Revision != "2" {
				t.Fatalf("the shared-limit edit did not publish a coherent definition: %v", err)
			}
			config := next.Resources.LoadBalancer.Ingress
			want := contract.TopologyLinkConfig{
				BandwidthBitsPerSecond: config.BandwidthBitsPerSecond,
				PacketTravelTimeMs:     float64(config.PacketTravelTime) / float64(time.Millisecond),
				QueueCapacity:          config.QueueCapacity, LossRate: config.LossRate,
				QueuePolicy: config.QueuePolicy, CongestionThresholdPercent: config.CongestionThresholdPercent,
				CongestionSignalProbability: config.CongestionSignalProbability,
			}
			if definition.Ingress.Config != want || len(definition.Ingress.Queues) != 2 {
				t.Fatalf("the shared ingress definition retained old limits: %+v", definition.Ingress)
			}
			for index, queue := range definition.Ingress.Queues {
				if queue.Capacity != config.QueueCapacity || queue.ID != first.Document.Ingress.Queues[index].ID {
					t.Fatal("the input definition lost its capacity or stable identity")
				}
			}
			found := false
			for _, metric := range definition.Nodes[0].Metrics {
				if metric.Key == "forwardingBitsPerSecond" {
					found = metric.Value != nil &&
						*metric.Value == float64(next.Resources.LoadBalancer.ForwardingBitsPerSecond)
				}
			}
			if !found || len(definition.Nodes) != len(first.Document.Nodes) ||
				len(definition.Links) != len(first.Document.Links) {
				t.Fatal("the forwarding definition is stale or shared limits changed graph membership")
			}
			for index, node := range definition.Nodes {
				if node.ID != first.Document.Nodes[index].ID {
					t.Fatal("the shared-limit edit changed a retained node ID")
				}
			}
			for index, link := range definition.Links {
				if link.ID != first.Document.Links[index].ID {
					t.Fatal("the shared-limit edit changed a retained link ID")
				}
			}
			if jsonProjection(t, first.Document) != oldDocument || project(t, projector, world).Document != nil {
				t.Fatal("publishing limits mutated the old document or invalidated the new cache")
			}
		})
	}
}

// Policy, network and CPU settings publish as one revision under stable resource IDs.
func TestTimelineCompoundPolicyEditPublishesCoherentConfiguration(t *testing.T) {
	input := timelineProjectionDefinition()
	next := input.States["250000000"]
	next.Resources.Backends = input.States["0"].Resources.Backends
	config := &next.Resources.LoadBalancer
	config.Algorithm = "random"
	halfOpen, established, closing, reset := 7*time.Second, time.Duration(0), 9*time.Second, 11*time.Second
	config.MappingTimeouts = scenario.LoadBalancerTimeoutConfig{
		HalfOpenIdleTimeout: &halfOpen, EstablishedIdleTimeout: &established,
		ClosingIdleTimeout: &closing, ResetIdleTimeout: &reset,
	}
	config.ForwardingBitsPerSecond = 5_000_000
	config.Ingress.BandwidthBitsPerSecond = 6_000_000
	cores, queue, cost := 3, 7, time.Millisecond
	config.Host = scenario.HostConfig{Cores: &cores, QueueCapacity: &queue, PacketProcessingTime: &cost}
	input.States["250000000"] = next
	world := timelineProjectionFrom(t, input)
	advanceProjection(t, world, 0)
	projector := NewProjector("policy")
	first := project(t, projector, world)
	oldDocument := jsonProjection(t, first.Document)

	advanceProjection(t, world, 250*time.Millisecond-time.Nanosecond)
	before, _ := world.CurrentConfiguration()
	if before.LoadBalancer.Algorithm != "roundRobin" || world.Info().Algorithm != "roundRobin" ||
		project(t, projector, world).Document != nil {
		t.Fatal("a policy edit became visible before its timestamp")
	}
	advanceProjection(t, world, 250*time.Millisecond)
	frame, document, err := BuildSnapshot(world, projector, RunState{Status: "running", RunID: "policy"})
	if err != nil || document == nil || document.Revision != "2" || frame.Topology.Revision != "2" {
		t.Fatalf("the policy edit did not publish one complete definition: %v", err)
	}
	current, ok := world.CurrentConfiguration()
	info := world.Info()
	if !ok || current.At != 250*time.Millisecond || info.ConfigurationRevision != 2 ||
		current.LoadBalancer.Algorithm != "random" || info.Algorithm != "random" ||
		frame.Algorithm != "Random" || frame.Simulation.Algorithm != "random" {
		t.Fatal("current configuration and snapshot metadata disagree about the committed policy")
	}
	if info.LBHalfOpenIdleTimeout != halfOpen || info.LBEstablishedIdleTimeout != established ||
		info.LBClosingIdleTimeout != closing || info.LBResetIdleTimeout != reset ||
		current.LoadBalancer.ResetIdleTimeout != reset {
		t.Fatal("current timeout configuration retained previous limits")
	}
	result := BuildResult(world, 0)
	if result.State.Algorithm != "Random" || result.LBIdleTimeouts != (contract.LBIdleTimeoutConfig{
		HalfOpenIdleTimeoutSeconds: 7, EstablishedIdleTimeoutSeconds: 0,
		ClosingIdleTimeoutSeconds: 9, ResetIdleTimeoutSeconds: 11,
	}) {
		t.Fatal("the result report did not use the current mapping policy")
	}
	assertHostDefinition(t, document, simulation.LoadBalancerNodeID, 1, 3, 1_000, 7)
	if document.Ingress.Config.BandwidthBitsPerSecond != 6_000_000 ||
		info.Forwarding.ForwardingBitsPerSecond != 5_000_000 ||
		len(document.Nodes) != len(first.Document.Nodes) || len(document.Links) != len(first.Document.Links) {
		t.Fatal("the compound policy commit published stale limits or changed graph membership")
	}
	for index, node := range document.Nodes {
		if node.ID != first.Document.Nodes[index].ID {
			t.Fatal("the compound policy edit changed a retained node ID")
		}
	}
	if jsonProjection(t, first.Document) != oldDocument || project(t, projector, world).Document != nil {
		t.Fatal("publishing policy mutated an old document or invalidated the new cache")
	}
}

// Host-only commits refresh cached settings and keep CPU history through bypass.
func TestTimelineHostEditRefreshesTopologyConfiguration(t *testing.T) {
	for _, resource := range []string{"loadBalancer", "backend"} {
		t.Run(resource, func(t *testing.T) {
			input := timelineProjectionDefinition()
			nodeID := simulation.LoadBalancerNodeID
			if resource == "backend" {
				nodeID = simulation.BackendNodeID("one")
			}
			withHost := func(cores, queue int, cost time.Duration) scenario.StateConfig {
				state := input.States["0"]
				state.Resources.Backends = append(
					[]scenario.BackendStateConfig(nil), state.Resources.Backends...,
				)
				host := scenario.HostConfig{
					Cores: &cores, QueueCapacity: &queue, PacketProcessingTime: &cost,
				}
				if resource == "loadBalancer" {
					state.Resources.LoadBalancer.Host = host
				} else {
					state.Resources.Backends[0].Host = host
				}
				return state
			}
			input.States["0"] = withHost(2, 4, 300*time.Millisecond)
			input.States["250000000"] = withHost(1, 1, 0)
			input.States["375000000"] = withHost(3, 7, 2*time.Millisecond)

			world := timelineProjectionFrom(t, input)
			advanceProjection(t, world, 0)
			projector := NewProjector("host-settings")
			first := project(t, projector, world)
			oldDocument := jsonProjection(t, first.Document)
			assertHostDefinition(t, first.Document, nodeID, 1, 2, 300_000, 4)

			advanceProjection(t, world, 250*time.Millisecond-time.Nanosecond)
			if before := project(t, projector, world); before.Document != nil ||
				before.Topology.Revision != "1" {
				t.Fatal("a future CPU change invalidated definitions before its boundary")
			}

			advanceProjection(t, world, 250*time.Millisecond)
			frame, definition, err := BuildSnapshot(world, projector, RunState{
				Status: "running", RunID: "host-settings",
			})
			if err != nil || definition == nil || definition.Revision != "2" ||
				frame.Topology.Revision != "2" {
				t.Fatalf("CPU bypass did not publish one coherent definition: %v", err)
			}
			assertHostDefinition(t, definition, nodeID, 0, 1, 0, 1)
			foundLive := false
			for _, node := range frame.Topology.Nodes {
				if node.ID != nodeID {
					continue
				}
				foundLive = true
				if node.Metrics["hostBusyCores"] == 0 ||
					node.Metrics["hostBusyTimeSeconds"] <= 0 ||
					node.Metrics["hostAvailableCoreTimeSeconds"] <= 0 ||
					node.Metrics["hostUtilization"] <= 0 {
					t.Fatalf("bypass hid retiring jobs or their CPU history: %+v", node.Metrics)
				}
			}
			if !foundLive {
				t.Fatalf("CPU sample is missing node %q", nodeID)
			}
			events := world.EventsExecuted()
			if repeated := project(t, projector, world); repeated.Document != nil ||
				repeated.Topology.Revision != "2" || world.EventsExecuted() != events {
				t.Fatal("reading the CPU commit changed its revision or executed work")
			}

			advanceProjection(t, world, 375*time.Millisecond)
			reenabled := project(t, projector, world)
			if reenabled.Document == nil || reenabled.Document.Revision != "3" ||
				reenabled.Topology.Revision != "3" {
				t.Fatal("re-enabling finite CPU did not refresh the cached definition")
			}
			assertHostDefinition(t, reenabled.Document, nodeID, 1, 3, 2_000, 7)
			if jsonProjection(t, first.Document) != oldDocument ||
				project(t, projector, world).Document != nil {
				t.Fatal("CPU commits mutated an old document or invalidated the new cache")
			}
		})
	}
}

func assertHostDefinition(
	t *testing.T,
	document *contract.TopologyDocument,
	nodeID string,
	enabled, cores, costUs, queue float64,
) {
	t.Helper()
	for _, node := range document.Nodes {
		if node.ID != nodeID {
			continue
		}
		values := make(map[string]float64)
		for _, metric := range node.Metrics {
			if metric.Value != nil {
				values[metric.Key] = *metric.Value
			}
		}
		for key, want := range map[string]float64{
			"hostCPUEnabled": enabled, "hostCores": cores,
			"hostPacketProcessingTimeUs": costUs, "hostQueueCapacity": queue,
		} {
			if got, exists := values[key]; !exists || got != want {
				t.Fatalf("CPU definition %s/%s = %v, want %v", nodeID, key, got, want)
			}
		}
		return
	}
	t.Fatalf("CPU definition is missing node %q", nodeID)
}

// Backend names and the frame revision become visible together after one configuration event.
func TestTimelineCommitPublishesOneCompleteDefinitionBeforeItsSnapshot(t *testing.T) {
	world := timelineProjectionWorld(t)
	advanceProjection(t, world, 0)
	projector := NewProjector("timeline")
	first := project(t, projector, world)
	if first.Document == nil || first.Document.Revision != "1" || world.Info().CreatedClients != 2 {
		t.Fatal("the initial world must expose both clients and its first definition")
	}
	oldDocument := jsonProjection(t, first.Document)

	// A sample before the boundary must retain the old complete definition.
	advanceProjection(t, world, 250*time.Millisecond-time.Nanosecond)
	before := project(t, projector, world)
	if before.Document != nil || before.Topology.Revision != "1" {
		t.Fatal("the pending transition changed the cached definition too early")
	}

	advanceProjection(t, world, 250*time.Millisecond)
	frame, definition, err := BuildSnapshot(world, projector, RunState{Status: "running", RunID: "timeline"})
	if err != nil {
		t.Fatal(err)
	}
	if definition == nil || definition.Revision != "2" || frame.Topology.Revision != definition.Revision {
		t.Fatal("the first post-commit snapshot must have its complete new definition")
	}
	if len(definition.Servers) != 2 || definition.Servers[0].Name != "New one" ||
		definition.Servers[1].Name != "New two" {
		t.Fatal("the document exposed a partially applied backend-name transition")
	}
	for _, server := range definition.Servers {
		found := false
		for _, node := range definition.Nodes {
			if node.ID == simulation.BackendNodeID(server.ID) {
				found = node.Label == server.Name
			}
		}
		if !found {
			t.Fatalf("backend %q has inconsistent label and server metadata", server.ID)
		}
	}
	if jsonProjection(t, first.Document) != oldDocument {
		t.Fatal("publishing the new definition mutated the retained old document")
	}

	// Client identities remain stable across the configuration commit.
	for _, oldNode := range first.Document.Nodes {
		if oldNode.Kind != contract.TopologyNodeClient {
			continue
		}
		found := false
		for _, node := range definition.Nodes {
			if node.ID != oldNode.ID {
				continue
			}
			found = true
			if node.ClientKey == nil || *node.ClientKey != *oldNode.ClientKey {
				t.Fatalf("the configuration commit changed client %q's identity", node.ID)
			}
		}
		if !found {
			t.Fatalf("the configuration commit removed client %q", oldNode.ID)
		}
	}
	if next := project(t, projector, world); next.Document != nil || next.Topology.Revision != "2" {
		t.Fatal("reading the committed configuration again created another revision")
	}
}
