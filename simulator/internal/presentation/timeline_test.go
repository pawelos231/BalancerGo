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

	input := scenario.ScenarioV2{
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
