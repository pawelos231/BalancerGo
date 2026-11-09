package simulation

import (
	"math/rand"
	"time"

	"load-balancer/network"
	"load-balancer/scenario"
)

// commitClientGroups binds checked settings only to clients that actually exist.
// Future clients have reservations and descriptors, but no links or TCP objects.
func (s *World) commitClientGroups(changes scenario.ClientGroupChanges, at time.Duration) {
	for _, id := range changes.Stopped {
		group := s.clientGroups[id]
		s.cancelClientArrival(group)
		group.Present = false
	}
	for _, config := range changes.Added {
		group := s.clientGroups[config.ID]
		group.Config, group.Present = config, true
		s.planClientArrival(group, at)
	}
	for _, update := range changes.Updated {
		group := s.clientGroups[update.Config.ID]
		group.Config = update.Config
		if update.LinksChanged {
			s.updateClientNetworks(group)
		}
		if update.ReplanArrivals {
			s.cancelClientArrival(group)
			s.planClientArrival(group, at)
		}
	}

	// Reservations include never-born slots; this metric describes current plans.
	// Removed groups retain their births, so scheduled is always at least created.
	s.scheduledClients = 0
	for _, group := range s.clientGroupOrder {
		planned := group.Born
		if group.Present {
			planned = max(planned, group.Config.Count)
		}
		s.scheduledClients += planned
	}
}

func (s *World) cancelClientArrival(group *runtimeClientGroup) {
	s.engine.Cancel(group.ArrivalEventID)
	group.ArrivalEventID = 0
}

// Both lists follow client-index order. Resolve sparse overrides in one pass,
// including retained clients beyond a newly reduced birth budget.
func (s *World) updateClientNetworks(group *runtimeClientGroup) {
	overrides := group.Config.Overrides
	position := 0
	for _, clientIndex := range group.ClientIndices {
		runtime := &s.clients[clientIndex]
		for position < len(overrides) && overrides[position].Index < runtime.Index {
			position++
		}
		config := group.Config.Network
		if position < len(overrides) && overrides[position].Index == runtime.Index {
			config = overrides[position].Network
		}
		if runtime.Config.Network == config {
			continue
		}
		toConfig, toRNG := s.clientLinkSettings("link/"+runtime.ID+"/to-lb", config.ToLoadBalancer)
		fromConfig, fromRNG := s.clientLinkSettings("link/"+runtime.ID+"/from-lb", config.FromLoadBalancer)
		runtime.ToInput.PrepareConfig(toConfig, toRNG).Apply()
		runtime.Output.PrepareConfig(fromConfig, fromRNG).Apply()
		runtime.Config.Network = config
	}
}

// A new signaling stream is bound on first use; Link preserves an existing one.
func (s *World) clientLinkSettings(id string, config scenario.LinkConfig) (network.Config, *rand.Rand) {
	var congestionRNG *rand.Rand
	if config.QueuePolicy == "threshold" &&
		config.CongestionSignalProbability > 0 && config.CongestionSignalProbability < 1 {
		congestionRNG = s.resourceRNG(id + "/congestion")
	}
	return configuredLink(config), congestionRNG
}
