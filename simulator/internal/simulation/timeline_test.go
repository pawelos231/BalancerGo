package simulation

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/scenario"
)

// timelineDefinition keeps both real backends but schedules no client arrivals.
// This makes one configuration event easy to distinguish from ordinary network work.
func timelineDefinition() scenario.ScenarioV2 {
	definition := baseScenario()
	definition.States["0"].Workload.ClientGroups[0].Count = 0
	return definition
}

func newTimelineWorld(t *testing.T, definition scenario.ScenarioV2) *World {
	t.Helper()
	plan, err := scenario.CompileV2(definition)
	if err != nil {
		t.Fatal(err)
	}
	world, err := NewTimeline(plan)
	if err != nil {
		t.Fatal(err)
	}
	return world
}

// Both names change at 100 ms before an ordinary observer at that same timestamp.
func TestTimelineCommitsConfigurationAsOneEvent(t *testing.T) {
	definition := timelineDefinition()
	next := timelineDefinition().States["0"]
	next.Resources.Backends[0].Name = "first updated"
	next.Resources.Backends[1].Name = "second updated"
	definition.States["100000000"] = next
	world := newTimelineWorld(t, definition)

	// Merely constructing the world must not expose the future state.
	before, ok := world.CurrentConfiguration()
	if !ok || before.At != 0 || before.Backends[0].Name != "backend-00" {
		t.Fatalf("unexpected initial configuration: %+v", before)
	}
	if world.Info().ConfigurationRevision != 1 {
		t.Fatal("the initial configuration must establish revision one")
	}

	observed := false
	world.ScheduleAt(100*time.Millisecond, func() {
		observed = true
		current, ok := world.CurrentConfiguration()
		if !ok || current.At != 100*time.Millisecond || world.Info().ConfigurationRevision != 2 {
			t.Fatal("an ordinary event did not see the committed configuration")
		}
		servers := world.lb.GetServers()
		if servers[0].Name != "first updated" || servers[1].Name != "second updated" {
			t.Fatal("an ordinary event observed only part of the rename")
		}
	})

	if err := world.Advance(context.Background(), 100*time.Millisecond-time.Nanosecond); err != nil {
		t.Fatal(err)
	}
	before, _ = world.CurrentConfiguration()
	if before.At != 0 {
		t.Fatal("the future configuration became active before its timestamp")
	}
	if err := world.Advance(context.Background(), 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if !observed || world.Now() != 100*time.Millisecond {
		t.Fatal("configuration application consumed virtual time or skipped the observer")
	}

	// Returned configuration collections are views the caller may edit safely.
	current, _ := world.CurrentConfiguration()
	current.Backends[0].Name = "edited inspection"
	again, _ := world.CurrentConfiguration()
	if again.Backends[0].Name != "first updated" {
		t.Fatal("editing an inspection view changed committed configuration")
	}
}

// A one-event budget may pause after the whole commit, never between its two renames.
func TestTimelineEventBudgetDoesNotSplitACommit(t *testing.T) {
	definition := timelineDefinition()
	next := timelineDefinition().States["0"]
	next.Resources.Backends[0].Name = "first updated"
	next.Resources.Backends[1].Name = "second updated"
	definition.States["100000000"] = next
	world := newTimelineWorld(t, definition)
	world.engine.MaxEvents = 1
	world.ScheduleAt(100*time.Millisecond, func() { t.Fatal("the second event exceeded the budget") })

	err := world.Advance(context.Background(), 100*time.Millisecond)
	if !errors.Is(err, engine.ErrEventLimit) || world.EventsExecuted() != 1 {
		t.Fatalf("expected a pause after one event, got %v", err)
	}
	servers := world.lb.GetServers()
	if servers[0].Name != "first updated" || servers[1].Name != "second updated" ||
		world.Info().ConfigurationRevision != 2 {
		t.Fatal("the event budget interrupted a configuration commit")
	}
}

// No runtime resources are created without a valid compiled timeline.
func TestTimelineRejectsMissingPlanBeforeConstruction(t *testing.T) {
	for _, plan := range []*scenario.CompiledV2{nil, {}} {
		world, err := NewTimeline(plan)
		if world != nil || err == nil {
			t.Fatalf("missing plan was admitted: world=%v error=%v", world, err)
		}
	}
}

// Stop cancels future changes; manual algorithm/spawn commands cannot bypass the timeline.
func TestTimelineStopsFutureCommitsAndRejectsManualMutations(t *testing.T) {
	definition := timelineDefinition()
	next := timelineDefinition().States["0"]
	next.Resources.Backends[0].Name = "future name"
	definition.States["100000000"] = next
	world := newTimelineWorld(t, definition)
	before := world.Info()
	if err := world.SetAlgorithm("random"); err == nil {
		t.Fatal("a manual algorithm command bypassed the timeline")
	}
	if err := world.SpawnClients(1, "clients"); err == nil {
		t.Fatal("a manual spawn command bypassed the timeline")
	}
	if !reflect.DeepEqual(world.Info(), before) {
		t.Fatal("a rejected command changed the world")
	}

	world.Stop()
	if world.PendingEvents() != 0 {
		t.Fatal("Stop retained a pending configuration event")
	}
	if err := world.Advance(context.Background(), world.EndTime()); err != nil {
		t.Fatal(err)
	}
	current, ok := world.CurrentConfiguration()
	if !ok || current.At != 0 || world.Info().ConfigurationRevision != 1 {
		t.Fatal("a stopped world applied a future configuration")
	}
}
