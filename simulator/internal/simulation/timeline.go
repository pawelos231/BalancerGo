package simulation

import (
	"errors"
	"fmt"
	"time"

	"load-balancer/scenario"
	"load-balancer/server"
)

var (
	ErrUnsupportedTimelineChange = errors.New("simulation: timeline change is not supported yet")
	ErrTimelineManualMutation    = errors.New("simulation: timeline configuration must be changed through scenario states")
)

// timelineRuntime tracks only committed settings, not future desired states.
// The backend index binds prepared name edits to existing endpoints before execution.
type timelineRuntime struct {
	plan        *scenario.CompiledV2
	committedAt time.Duration
	revision    uint64
	backends    map[string]*server.Server
}

// configurationCommit holds data prepared before the engine starts.
// Applying it performs assignments only, without validation or observer callbacks.
type configurationCommit struct {
	at            time.Duration
	configuration scenario.Normalized
	names         []backendNameChange
}

type backendNameChange struct {
	id, name string
}

// NewTimeline prepares a world only after every transition has a supported executor.
// Initially only backend names can change; resource handlers arrive in later stages.
func NewTimeline(plan *scenario.CompiledV2) (*World, error) {
	if plan == nil {
		return nil, fmt.Errorf("simulation: compiled timeline is required")
	}
	initial, err := plan.StateAt(0)
	if err != nil {
		return nil, err
	}
	commits, err := prepareConfigurationCommits(plan, initial)
	if err != nil {
		return nil, err
	}

	world := New(timelineConfiguration(plan, initial))
	world.timeline = &timelineRuntime{
		plan:     plan,
		revision: 1,
		backends: make(map[string]*server.Server, len(initial.Backends)),
	}
	for _, backend := range world.lb.GetServers() {
		world.timeline.backends[backend.ID] = backend
	}

	// Register all configuration events before any packet or arrival is executed.
	for _, commit := range commits {
		world.engine.ScheduleConfigurationAt(commit.at, func() {
			world.commitConfiguration(commit)
		})
	}
	return world, nil
}

// prepareConfigurationCommits rejects unsupported late edits before creating a world.
// No transition is accepted merely because its initial state could run.
func prepareConfigurationCommits(
	plan *scenario.CompiledV2,
	initial scenario.NormalizedState,
) ([]configurationCommit, error) {
	for _, backend := range initial.Backends {
		if !backend.PhysicalAvailable || backend.AdministrativeState != scenario.BackendAdminActive {
			return nil, fmt.Errorf("%w at 0s: backend availability and admission policy", ErrUnsupportedTimelineChange)
		}
	}

	transitions := plan.Transitions()
	commits := make([]configurationCommit, 0, len(transitions))
	for _, transition := range transitions {
		if err := checkSupportedTransition(transition); err != nil {
			return nil, err
		}
		state, err := plan.StateAt(transition.At)
		if err != nil {
			return nil, err
		}
		commit := configurationCommit{
			at:            transition.At,
			configuration: timelineConfiguration(plan, state),
			names:         make([]backendNameChange, 0, len(transition.Backends.Updated)),
		}
		for _, update := range transition.Backends.Updated {
			commit.names = append(commit.names, backendNameChange{
				id: update.Config.ID, name: update.Config.Name,
			})
		}
		commits = append(commits, commit)
	}
	return commits, nil
}

// checkSupportedTransition is extended alongside each concrete resource handler.
func checkSupportedTransition(transition scenario.Transition) error {
	unsupported := func(feature string) error {
		return fmt.Errorf("%w at %s: %s", ErrUnsupportedTimelineChange, transition.At, feature)
	}
	if transition.LoadBalancer != nil {
		return unsupported("load balancer settings")
	}
	if len(transition.Backends.Added) != 0 || len(transition.Backends.Removed) != 0 {
		return unsupported("backend membership")
	}
	groups := transition.ClientGroups
	if len(groups.Added) != 0 || len(groups.Updated) != 0 || len(groups.Stopped) != 0 {
		return unsupported("client generators")
	}
	for _, update := range transition.Backends.Updated {
		if update.LinksChanged || update.HostChanged || update.AvailabilityChanged || update.AdmissionChanged {
			return unsupported("backend links, CPU, availability or admission policy")
		}
	}
	return nil
}

// timelineConfiguration adapts checked state data to the existing world constructor.
// Availability and admission are checked before these extra v2 fields are omitted.
func timelineConfiguration(plan *scenario.CompiledV2, state scenario.NormalizedState) scenario.Normalized {
	fixed := plan.Fixed()
	backends := make([]scenario.NormalizedBackend, 0, len(state.Backends))
	for _, backend := range state.Backends {
		backends = append(backends, backend.NormalizedBackend)
	}
	return scenario.Normalized{
		Version:         scenario.VersionV2,
		Execution:       fixed.Execution,
		TCP:             fixed.TCP,
		NetworkDefaults: fixed.NetworkDefaults,
		DefaultStream:   fixed.DefaultStream,
		LoadBalancer:    state.LoadBalancer,
		Backends:        backends,
		ClientGroups:    state.ClientGroups,
		TotalClients:    plan.TotalClients(),
	}
}

// commitConfiguration runs to completion as one configuration-priority event.
// Publish the new configuration and its generation only after every edit is applied.
func (s *World) commitConfiguration(commit configurationCommit) {
	for _, change := range commit.names {
		s.timeline.backends[change.id].Name = change.name
	}
	s.scenario = commit.configuration
	s.timeline.committedAt = commit.at
	s.timeline.revision++
}

// CurrentConfiguration returns the last committed v2 state, never a future declaration.
// Like other world observations, it belongs to the engine's owner goroutine.
func (s *World) CurrentConfiguration() (scenario.NormalizedState, bool) {
	if s.timeline == nil {
		return scenario.NormalizedState{}, false
	}
	state, err := s.timeline.plan.StateAt(s.timeline.committedAt)
	return state, err == nil
}

func (s *World) configurationRevision() uint64 {
	if s.timeline == nil {
		return 0
	}
	return s.timeline.revision
}
