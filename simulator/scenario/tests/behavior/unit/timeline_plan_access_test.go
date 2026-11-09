package scenario_test

import (
	"math"
	"reflect"
	"testing"
	"time"

	"load-balancer/scenario"
)

// A state becomes visible exactly at its timestamp and remains visible through the run's end.
func TestCompiledSelectsStateAtExactTimeBoundaries(t *testing.T) {
	input := exampleTimeline()
	next := exampleTimeline().States["0"]
	next.Resources.LoadBalancer.ForwardingBitsPerSecond = 100
	input.States["2"] = next
	plan := compileTimeline(t, input)

	for _, query := range []struct {
		at      time.Duration
		stateAt time.Duration
	}{
		{0, 0}, {1, 0}, {2, 2}, {3, 2}, {input.Fixed.Execution.Duration, 2},
	} {
		state, err := plan.StateAt(query.at)
		if err != nil || state.At != query.stateAt {
			t.Fatalf("StateAt(%v) returned state=%v, error=%v", query.at, state.At, err)
		}
	}
	for _, at := range []time.Duration{-1, input.Fixed.Execution.Duration + 1} {
		if _, err := plan.StateAt(at); err == nil {
			t.Fatalf("StateAt(%v) accepted a time outside the run", at)
		}
	}

	// Large exact timestamps must not pass through floating-point arithmetic.
	input.Fixed.Execution.Duration = time.Duration(math.MaxInt64)
	delete(input.States, "2")
	input.States["9007199254740993"] = next
	plan = compileTimeline(t, input)
	state, err := plan.StateAt(9_007_199_254_740_993)
	if err != nil || state.At != 9_007_199_254_740_993 {
		t.Fatalf("large timestamp lost precision: state=%v, error=%v", state.At, err)
	}
	state, err = plan.StateAt(time.Duration(math.MaxInt64))
	if err != nil || state.At != 9_007_199_254_740_993 {
		t.Fatal("the inclusive terminal query failed at the time.Duration limit")
	}
}

// The plan owns its data, and each inspection result owns its nested slices and pointers.
func TestCompiledProtectsInputStatesAndTransitionsFromMutation(t *testing.T) {
	input := exampleTimeline()
	input.Fixed.TCP.MSSBytes = pointer(1024)
	next := exampleTimeline().States["0"]
	next.Resources.LoadBalancer.ForwardingBitsPerSecond = 100
	next.Resources.Backends[0].Name = "updated"
	next.Resources.Backends = append(next.Resources.Backends, scenario.BackendStateConfig{
		ID: "extra-backend", Address: "10.0.0.2", Port: 80,
	})
	next.Workload.ClientGroups[0].Overrides = []scenario.ClientOverride{
		{Index: 2, Stream: scenario.StreamPatch{Bytes: pointer(200)}},
	}
	next.Workload.ClientGroups = append(next.Workload.ClientGroups, scenario.ClientGroup{
		ID: "burst", Count: 1, StartAt: 2,
		Overrides: []scenario.ClientOverride{{Index: 0, Stream: scenario.StreamPatch{Bytes: pointer(300)}}},
	})
	input.States["2"] = next
	plan := compileTimeline(t, input)

	wantFixed := plan.Fixed()
	wantState, err := plan.StateAt(2)
	if err != nil {
		t.Fatal(err)
	}
	wantChanges := plan.Transitions()

	// Mutating raw patches after compilation must not reach the plan.
	*input.Fixed.TCP.MSSBytes = 1
	*input.States["2"].Workload.ClientGroups[0].Overrides[0].Stream.Bytes = 999
	input.States["2"].Resources.Backends[0].Name = "edited input"

	// Mutating a state view must not affect another view or transition configuration.
	state, err := plan.StateAt(2)
	if err != nil {
		t.Fatal(err)
	}
	state.Backends[0].Name = "edited state"
	state.ClientGroups[0].Overrides[0].Stream.Bytes = 1
	state.ClientGroups[1].Overrides[0].Stream.Bytes = 2

	// Exercise added and updated resource views, including both groups' nested overrides.
	changes := plan.Transitions()
	changes[0].At = 100
	changes[0].LoadBalancer.Config.ForwardingBitsPerSecond = 1
	changes[0].Backends.Updated[0].Config.Name = "edited update"
	changes[0].Backends.Added[0].Name = "edited addition"
	changes[0].ClientGroups.Updated[0].Config.Overrides[0].Stream.Bytes = 3
	changes[0].ClientGroups.Added[0].Overrides[0].Stream.Bytes = 4
	fixed := plan.Fixed()
	fixed.Execution.Duration = 0

	actualState, err := plan.StateAt(2)
	if err != nil || !reflect.DeepEqual(actualState, wantState) {
		t.Fatal("editing an input or inspection view changed the plan's full state")
	}
	if !reflect.DeepEqual(plan.Transitions(), wantChanges) || plan.Fixed() != wantFixed {
		t.Fatal("editing a returned transition or fixed settings changed the plan")
	}
}

// A maximal group stays one generator; compilation does not allocate millions of clients.
func TestCompiledKeepsMaximalClientGroupsCompact(t *testing.T) {
	input := exampleTimeline()
	input.States["0"].Workload.ClientGroups[0].Count = scenario.MaxClients
	plan := compileTimeline(t, input)
	state, err := plan.StateAt(0)
	if err != nil || len(state.ClientGroups) != 1 || state.ClientGroups[0].Count != scenario.MaxClients {
		t.Fatalf("maximal group was expanded or changed: %+v, %v", state, err)
	}
	if plan.TotalClients() != scenario.MaxClients || len(plan.Transitions()) != 0 {
		t.Fatal("initial-only compilation lost its reserved budget or invented a transition")
	}
}
