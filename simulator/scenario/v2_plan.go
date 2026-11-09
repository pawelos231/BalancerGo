package scenario

import (
	"fmt"
	"slices"
	"sort"
	"time"
)

// CompileV2 validates the whole document, then prepares changes between full states.
// It does not create clients, schedule events or modify a running simulation.
func CompileV2(input ScenarioV2) (*CompiledV2, error) {
	resolved, err := NormalizeV2(input)
	if err != nil {
		return nil, err
	}

	plan := &CompiledV2{
		scenario:    resolved,
		transitions: make([]Transition, 0, len(resolved.States)-1),
	}
	for index := 1; index < len(resolved.States); index++ {
		previous := resolved.States[index-1]
		next := resolved.States[index]
		transition := compileTransition(previous, next)
		if hasChanges(transition) {
			plan.transitions = append(plan.transitions, transition)
		}
	}
	return plan, nil
}

// Fixed returns run-wide settings, such as TCP and execution limits, by value.
func (plan *CompiledV2) Fixed() NormalizedFixedConfig {
	return plan.scenario.Fixed
}

// TotalClients returns reserved client slots across the entire run, not active clients.
func (plan *CompiledV2) TotalClients() int {
	return plan.scenario.TotalClients
}

// StateAt returns the desired configuration, including declarations with no changes.
// At 15 s, states at 0, 10 and 20 s select 10 s. The terminal time is also allowed.
func (plan *CompiledV2) StateAt(at time.Duration) (NormalizedState, error) {
	if plan == nil || len(plan.scenario.States) == 0 {
		return NormalizedState{}, fmt.Errorf("scenario: timeline has not been compiled")
	}
	endTime := plan.scenario.Fixed.Execution.Duration
	if at < 0 || at > endTime {
		return NormalizedState{}, fmt.Errorf("scenario: query time must be in 0..%s", endTime)
	}

	// Find the first later state; the preceding one is active at the requested time.
	states := plan.scenario.States
	nextIndex := sort.Search(len(states), func(index int) bool {
		return states[index].At > at
	})
	return cloneState(states[nextIndex-1]), nil
}

// Transitions returns independent instructions for all nonempty changes after time zero.
// Empty declarations remain available through StateAt without scheduling needless work.
func (plan *CompiledV2) Transitions() []Transition {
	result := make([]Transition, len(plan.transitions))
	for index, transition := range plan.transitions {
		result[index] = cloneTransition(transition)
	}
	return result
}

// cloneState copies collection storage, including the individual client overrides.
func cloneState(state NormalizedState) NormalizedState {
	state.Backends = slices.Clone(state.Backends)
	state.ClientGroups = cloneGroups(state.ClientGroups)
	return state
}

// cloneGroups keeps the compact group representation while detaching nested overrides.
func cloneGroups(groups []NormalizedClientGroup) []NormalizedClientGroup {
	result := slices.Clone(groups)
	for index := range result {
		result[index].Overrides = slices.Clone(result[index].Overrides)
	}
	return result
}

// cloneTransition prevents edits to returned instructions from changing the stored plan.
func cloneTransition(transition Transition) Transition {
	if transition.LoadBalancer != nil {
		update := *transition.LoadBalancer
		transition.LoadBalancer = &update
	}

	transition.Backends.Added = slices.Clone(transition.Backends.Added)
	transition.Backends.Updated = slices.Clone(transition.Backends.Updated)
	transition.Backends.Removed = slices.Clone(transition.Backends.Removed)

	transition.ClientGroups.Added = cloneGroups(transition.ClientGroups.Added)
	transition.ClientGroups.Updated = slices.Clone(transition.ClientGroups.Updated)
	for index := range transition.ClientGroups.Updated {
		config := &transition.ClientGroups.Updated[index].Config
		config.Overrides = slices.Clone(config.Overrides)
	}
	transition.ClientGroups.Stopped = slices.Clone(transition.ClientGroups.Stopped)
	return transition
}
