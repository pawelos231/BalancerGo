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
func scenarioRuntimeDefinition() scenario.ScenarioV2 {
	definition := testConfiguration().scenarioDefinitionV2()
	definition.Fixed.Execution.Duration = 500 * time.Millisecond
	definition.Fixed.Network.Defaults = scenario.LinkConfig{
		BandwidthBitsPerSecond: 8_000_000,
		QueueCapacity:          32,
	}
	initial := definition.States["0"]
	initial.Resources.LoadBalancer.Ingress = definition.Fixed.Network.Defaults
	initial.Resources.LoadBalancer.ForwardingBitsPerSecond = 9_223_372_036_854_775_807
	definition.Fixed.Workload.Defaults = scenario.StreamConfig{Bytes: 0}
	initial.Workload.ClientGroups = []scenario.ClientGroup{{ID: "clients", Count: 1}}
	definition.States["0"] = initial
	return definition
}

func scenarioRuntimeValue[T any](value T) *T { return &value }

func initialScenarioState(t *testing.T, plan *scenario.CompiledV2) scenario.NormalizedState {
	t.Helper()
	state, err := plan.StateAt(0)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func compiledRuntimeScenario(t *testing.T, input scenario.ScenarioV2) *scenario.CompiledV2 {
	t.Helper()
	resolved, err := scenario.CompileV2(input)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func newScenarioSimulation(input *scenario.CompiledV2) *simulation.World {
	world, err := simulation.NewTimeline(input)
	if err != nil {
		panic(err)
	}
	return world
}

func newSimulation(cfg configuration) *simulation.World {
	return newScenarioSimulation(mustCompileScenario(cfg.scenarioDefinitionV2()))
}

func mustCompileScenario(input scenario.ScenarioV2) *scenario.CompiledV2 {
	resolved, err := scenario.CompileV2(input)
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
