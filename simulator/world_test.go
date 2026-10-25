package main

import (
	"context"
	"testing"
	"time"

	"load-balancer/internal/simulation"
	"load-balancer/scenario"
)

// Native fixtures are shared by application integration tests, without exposing
// the world's private endpoint pointers or network queues.
func scenarioRuntimeDefinition() scenario.Scenario {
	definition := legacyTestConfiguration().scenarioDefinition()
	definition.Execution.Duration = 500 * time.Millisecond
	definition.Network.Defaults = scenario.LinkConfig{
		BandwidthBitsPerSecond: 8_000_000,
		QueueCapacity:          32,
	}
	definition.Resources.LoadBalancer.Ingress = definition.Network.Defaults
	definition.Resources.LoadBalancer.ForwardingBitsPerSecond = 9_223_372_036_854_775_807
	definition.Workload.Defaults = scenario.StreamConfig{Bytes: 0}
	definition.Workload.ClientGroups = []scenario.ClientGroup{{ID: "clients", Count: 1}}
	return definition
}

func scenarioRuntimeValue[T any](value T) *T { return &value }

func normalizedRuntimeScenario(t *testing.T, input scenario.Scenario) scenario.Normalized {
	t.Helper()
	resolved, err := scenario.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func newScenarioSimulation(input scenario.Normalized) *simulation.World {
	return simulation.New(input)
}

func newSimulation(cfg configuration) *simulation.World {
	return simulation.New(mustNormalizeScenario(cfg.scenarioDefinition()))
}

func mustNormalizeScenario(input scenario.Scenario) scenario.Normalized {
	resolved, err := scenario.Normalize(input)
	if err != nil {
		panic(err)
	}
	return resolved
}

func runIngressUntil(t *testing.T, world *simulation.World, until time.Duration) {
	t.Helper()
	if err := world.Advance(context.Background(), until); err != nil {
		t.Fatal(err)
	}
}
