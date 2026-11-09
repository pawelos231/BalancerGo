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
