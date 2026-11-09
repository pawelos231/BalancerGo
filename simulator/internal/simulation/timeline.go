package simulation

import (
	"errors"
	"fmt"
	"math/rand"
	"time"

	"load-balancer/algorithms"
	"load-balancer/balancer"
	"load-balancer/host"
	"load-balancer/network"
	"load-balancer/scenario"
)

var ErrTimelineManualMutation = errors.New(
	"simulation: timeline configuration must be changed through scenario states",
)

// timelineRuntime tracks only committed settings, not future desired states.
type timelineRuntime struct {
	plan        *scenario.CompiledV2
	committedAt time.Duration
	revision    uint64
}

// configurationCommit contains checked edits, addressed by stable resource ID.
// Backend resources are bound only after their lifetime has started.
type configurationCommit struct {
	at                time.Duration
	configuration     scenario.NormalizedState
	added             []scenario.NormalizedBackendState
	removed           []string
	backendStates     []backendStateChange
	groups            scenario.ClientGroupChanges
	names             []backendNameChange
	links             []linkConfigurationChange
	forwardingChanged bool
	forwarding        network.PreparedForwardingConfig
	hosts             []hostConfigurationChange
	algorithmChanged  bool
	algorithm         balancer.PreparedAlgorithm
	timeoutsChanged   bool
	timeouts          balancer.PreparedTimeoutConfig
}

type backendNameChange struct {
	id, name string
}

type backendStateChange struct {
	id        string
	available bool
	draining  bool
}

type hostConfigurationChange struct {
	backendID string // Empty selects the shared LB CPU.
	config    host.Config
	prepared  host.PreparedConfig
}

// Link settings and random streams are checked before a future backend is created.
type linkConfigurationChange struct {
	id            string
	backendID     string // Empty selects the shared LB ingress.
	toBackend     bool
	config        network.Config
	congestionRNG *rand.Rand
}

// NewTimeline prepares a world from a completely checked configuration timeline.
func NewTimeline(plan *scenario.CompiledV2) (*World, error) {
	if plan == nil {
		return nil, fmt.Errorf("simulation: compiled timeline is required")
	}
	initial, err := plan.StateAt(0)
	if err != nil {
		return nil, err
	}
	commits, err := prepareConfigurationCommits(plan)
	if err != nil {
		return nil, err
	}

	timeline := &timelineRuntime{plan: plan, revision: 1}
	reservations := timelineClientReservations(initial.ClientGroups, commits)
	world := newWorld(plan.Fixed(), initial, timeline, reservations)
	for _, backend := range initial.Backends {
		world.applyBackendState(backendStateChange{
			id: backend.ID, available: backend.PhysicalAvailable,
			draining: backend.AdministrativeState == scenario.BackendAdminDrain,
		})
	}
	world.prepareResourceCommits(commits)

	// Register all configuration events before any packet or arrival is executed.
	for index := range commits {
		commit := &commits[index]
		world.engine.ScheduleConfigurationAt(commit.at, func() {
			world.commitConfiguration(commit)
		})
		if len(commit.hosts) > 0 {
			world.engine.ScheduleAt(commit.at, func() {
				for _, change := range commit.hosts {
					change.prepared.Resume()
				}
			})
		}
	}
	return world, nil
}

// prepareConfigurationCommits describes all edits before creating runtime resources.
// Compiled plans have already checked the entire document, including late states.
func prepareConfigurationCommits(plan *scenario.CompiledV2) ([]configurationCommit, error) {
	transitions := plan.Transitions()
	commits := make([]configurationCommit, 0, len(transitions))
	for _, transition := range transitions {
		state, err := plan.StateAt(transition.At)
		if err != nil {
			return nil, err
		}
		commit := configurationCommit{
			at:            transition.At,
			configuration: state,
			removed:       transition.Backends.Removed,
			groups:        transition.ClientGroups,
			names:         make([]backendNameChange, 0, len(transition.Backends.Updated)),
		}
		for _, backend := range transition.Backends.Added {
			commit.added = append(commit.added, backend)
		}
		if update := transition.LoadBalancer; update != nil {
			if update.HostChanged {
				commit.hosts = append(commit.hosts, hostConfigurationChange{
					config: configuredHost(update.Config.Host),
				})
			}
			if update.IngressChanged {
				commit.links = append(commit.links, linkConfigurationChange{
					id: "link/infrastructure/ingress", config: configuredLink(update.Config.Ingress),
				})
			}
			commit.forwardingChanged = update.ForwardingChanged
			commit.algorithmChanged = update.AlgorithmChanged
			commit.timeoutsChanged = update.TimeoutsChanged
		}
		for _, update := range transition.Backends.Updated {
			if update.AvailabilityChanged || update.AdmissionChanged {
				commit.backendStates = append(commit.backendStates, backendStateChange{
					id: update.Config.ID, available: update.Config.PhysicalAvailable,
					draining: update.Config.AdministrativeState == scenario.BackendAdminDrain,
				})
			}
			if update.HostChanged {
				commit.hosts = append(commit.hosts, hostConfigurationChange{
					backendID: update.Config.ID, config: configuredHost(update.Config.Host),
				})
			}
			if update.NameChanged {
				commit.names = append(commit.names, backendNameChange{
					id: update.Config.ID, name: update.Config.Name,
				})
			}
			if update.LinksChanged {
				endpoint := BackendNodeID(update.Config.ID)
				commit.links = append(commit.links,
					linkConfigurationChange{
						id:        "link/" + endpoint + "/to-lb",
						backendID: update.Config.ID,
						config:    configuredLink(update.Config.Network.ToLoadBalancer),
					},
					linkConfigurationChange{
						id:        "link/" + endpoint + "/from-lb",
						backendID: update.Config.ID,
						toBackend: true,
						config:    configuredLink(update.Config.Network.FromLoadBalancer),
					},
				)
			}
		}
		commits = append(commits, commit)
	}
	return commits, nil
}

// Prepare shared policies and random streams without creating future backend resources.
// The compiled scenario already checked every resource limit and identity.
func (s *World) prepareResourceCommits(commits []configurationCommit) {
	for commitIndex := range commits {
		commit := &commits[commitIndex]
		lb := commit.configuration.LoadBalancer
		if commit.algorithmChanged {
			var selection algorithms.Algorithm = &algorithms.RoundRobin{}
			if lb.Algorithm == "random" {
				selection = &algorithms.Random{RNG: s.algorithmRNG}
			}
			commit.algorithm = s.lb.PrepareAlgorithm(selection)
		}
		if commit.timeoutsChanged {
			commit.timeouts = s.lb.PrepareMappingTimeouts(configuredMappingTimeouts(lb))
		}
		if commit.forwardingChanged {
			commit.forwarding = s.egress.PrepareForwardingConfig(
				commit.configuration.LoadBalancer.ForwardingBitsPerSecond,
			)
		}
		for changeIndex := range commit.links {
			change := &commit.links[changeIndex]
			config := change.config
			if config.QueuePolicy == "threshold" &&
				config.CongestionSignalProbability > 0 && config.CongestionSignalProbability < 1 {
				change.congestionRNG = s.resourceRNG(change.id + "/congestion")
			}
		}
	}
}

// commitConfiguration runs to completion as one configuration-priority event.
// Publish the new configuration and its generation only after every edit is applied.
func (s *World) commitConfiguration(commit *configurationCommit) {
	for _, id := range commit.removed {
		s.removeBackend(id)
	}
	for _, backend := range commit.added {
		s.addBackend(backend.NormalizedBackend)
		s.applyBackendState(backendStateChange{
			id: backend.ID, available: backend.PhysicalAvailable,
			draining: backend.AdministrativeState == scenario.BackendAdminDrain,
		})
	}
	for index := range commit.hosts {
		change := &commit.hosts[index]
		if change.backendID == "" {
			change.prepared = s.lb.PrepareHostConfig(change.config)
		} else {
			change.prepared = s.backends[change.backendID].server.PrepareHostConfig(change.config)
		}
		change.prepared.Apply()
	}
	for _, change := range commit.links {
		if change.backendID == "" {
			s.ingress.PrepareConfig(change.config, change.congestionRNG).Apply()
			continue
		}
		backend := s.backends[change.backendID]
		if change.toBackend {
			backend.output.PrepareConfig(change.config, change.congestionRNG).Apply()
		} else {
			backend.returnLink.PrepareConfig(change.config, change.congestionRNG).Apply()
		}
	}
	if commit.forwardingChanged {
		commit.forwarding.Apply()
	}
	if commit.algorithmChanged {
		commit.algorithm.Apply()
		s.algorithm = commit.configuration.LoadBalancer.Algorithm
	}
	if commit.timeoutsChanged {
		commit.timeouts.Apply()
	}
	for _, change := range commit.names {
		s.backends[change.id].server.Name = change.name
	}
	for _, change := range commit.backendStates {
		s.applyBackendState(change)
	}
	s.commitClientGroups(commit.groups, commit.at)
	s.configuration = commit.configuration
	s.timeline.committedAt = commit.at
	s.timeline.revision++
}

func (s *World) applyBackendState(change backendStateChange) {
	backend := s.backends[change.id].server
	backend.SetPhysicalAvailable(change.available)
	s.lb.SetBackendDraining(backend, change.draining)
}

// CurrentConfiguration returns the last committed v2 state, never a future declaration.
// Like other world observations, it belongs to the engine's owner goroutine.
func (s *World) CurrentConfiguration() (scenario.NormalizedState, bool) {
	state, err := s.timeline.plan.StateAt(s.timeline.committedAt)
	return state, err == nil
}

func (s *World) configurationRevision() uint64 {
	return s.timeline.revision
}
