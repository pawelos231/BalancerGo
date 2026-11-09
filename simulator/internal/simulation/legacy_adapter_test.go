package simulation

import (
	"context"
	"testing"

	"load-balancer/scenario"
)

// The temporary v1 boundary preserves its historical zero-duration horizon.
func TestLegacyBoundaryPreservesZeroDuration(t *testing.T) {
	definition := baseScenario()
	definition.Execution.Duration = 0
	definition.Workload.ClientGroups[0].Count = 0
	resolved, err := scenario.Normalize(definition)
	if err != nil {
		t.Fatal(err)
	}
	world := New(resolved)
	if world.EndTime() != 0 || world.fixed.Execution.Duration != 0 {
		t.Fatal("the legacy boundary changed the admitted zero horizon")
	}
	if err := world.Advance(context.Background(), world.EndTime()); err != nil {
		t.Fatal(err)
	}
	if world.Now() != 0 || world.EventsExecuted() != 0 {
		t.Fatal("the zero-duration boundary invented simulation time or work")
	}
}
