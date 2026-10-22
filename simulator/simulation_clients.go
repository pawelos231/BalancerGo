package main

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"load-balancer/client"
	httpserver "load-balancer/http"
	"load-balancer/model"
	"load-balancer/scenario"
)

// runtimeClientGroup reserves identities without constructing all its clients.
// NextIndex keeps later spawns separate from clients still waiting to arrive.
type runtimeClientGroup struct {
	Config      scenario.NormalizedClientGroup
	AddressBase int
	NextIndex   int
}

// runtimeClient ties a created TCP client to its scenario and topology identity.
// Closed clients remain here so their links retain historical counters.
type runtimeClient struct {
	ID      string
	GroupID string
	Index   int
	Config  scenario.ClientConfig
	Client  *client.Client
}

// initializeClients reserves address ranges and schedules one arrival per group.
// The remaining arrivals form a chain, rather than one upfront event per client.
func (s *simulation) initializeClients() {
	groups := s.scenario.ClientGroups
	s.clientGroups = make([]runtimeClientGroup, len(groups))
	s.scheduledClients = s.scenario.TotalClients
	for i, group := range groups {
		s.clientGroups[i] = runtimeClientGroup{
			Config:    group,
			NextIndex: group.Count,
		}
	}

	// Sort groups by ID before assigning IP ranges, so changing their order in the
	// config does not change client addresses. Keep the original group list as is.
	ordered := make([]*runtimeClientGroup, len(groups))
	for i := range s.clientGroups {
		ordered[i] = &s.clientGroups[i]
	}

	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Config.ID < ordered[j].Config.ID
	})

	nextAddress := 1
	for _, group := range ordered {
		group.AddressBase = nextAddress
		nextAddress += group.Config.Count
	}

	for i := range s.clientGroups {
		group := &s.clientGroups[i]
		s.scheduleClientBatch(
			group,
			0,
			group.Config.Count,
			group.AddressBase,
			group.Config.StartAt,
		)
	}
}

// scheduleClientBatch creates each client at its own virtual arrival time.
// The address range is reserved already, including clients beyond the end time.
func (s *simulation) scheduleClientBatch(
	group *runtimeClientGroup,
	firstIndex, count, firstAddress int,
	startAt time.Duration,
) {
	if count == 0 {
		return
	}

	nextIndex := firstIndex
	var arrival func()
	arrival = func() {
		// A temporary count allows spawned indices without modifying the scenario.
		config := group.Config
		config.Count = group.NextIndex
		settings, err := config.Client(nextIndex)
		if err != nil {
			panic(err)
		}

		address := firstAddress + nextIndex - firstIndex
		s.startClient(settings, group.Config.ID, nextIndex, address)
		nextIndex++
		if nextIndex-firstIndex < count {
			s.engine.ScheduleAfter(group.Config.ClientGap, arrival)
		}
	}
	s.engine.ScheduleAt(startAt, arrival)
}

// startClient creates two private links and starts the configured TCP stream.
func (s *simulation) startClient(
	config scenario.ClientConfig,
	groupID string,
	index, address int,
) {
	sourceIP := fmt.Sprintf(
		"192.%d.%d.%d",    // 1st octet: always 192.
		byte(address>>16), // 2nd octet.
		byte(address>>8),  // 3rd octet.
		byte(address),     // 4th octet.
	)
	cl := client.NewClient(
		s.engine,
		sourceIP,
		uint16(40000+address%20000),
		"lb.local",
		80,
		model.TCP,
	)

	toIngress := s.addLink(
		"link/"+config.ID+"/to-upstream",
		config.ID+" -> upstream",
		config.ID,
		upstreamNodeID,
		config.Network.ToLoadBalancer,
	)
	toClient := s.addOutput(
		"link/"+config.ID+"/from-lb",
		"LB -> "+config.ID,
		loadBalancerNodeID,
		config.ID,
		config.Network.FromLoadBalancer,
	)

	// Every SYN, data packet and retry crosses both access and shared ingress.
	cl.SetSender(func(pkt model.Packet) {
		toIngress.Transmit(pkt.WireSize(), func() {
			s.ingress.Transmit(pkt.WireSize(), func() {
				if s.lb.HasClient(cl) {
					s.lb.HandleClientPacket(pkt)
				}
			})
		})
	})
	cl.OnStreamComplete = func() {
		s.completedStreams++
		s.completedPayloadBytes += uint64(config.Stream.Bytes)
	}
	cl.OnClose = func() { s.closedClients++ }

	err := s.lb.AddClient(cl, func(pkt model.Packet) {
		toClient.Transmit(pkt.WireSize(), func() {
			if s.lb.HasClient(cl) {
				cl.HandlePacket(pkt)
			}
		})
	})
	if err != nil {
		panic(err)
	}

	s.clients = append(s.clients, runtimeClient{
		ID:      config.ID,
		GroupID: groupID,
		Index:   index,
		Config:  config,
		Client:  cl,
	})
	s.createdClients++
	cl.SendCompleteStream(config.Stream.Bytes, config.Stream.PacketGap)
	cl.EstablishConnection()
}

// spawnClients adds a batch to one group's settings, starting at the current time.
// Multiple groups require an explicit groupId so profiles are never guessed.
func (s *simulation) spawnClients(message httpserver.SpawnClientMsg) error {
	invalidCount := message.Count < 1 || message.Count > 10000
	addressRangeExceeded := s.scheduledClients > scenario.MaxClients-message.Count
	if invalidCount || addressRangeExceeded {
		return errors.New("spawn count must be 1..10000 and fit the client address range")
	}

	groupID := message.GroupID
	if groupID == "" {
		if len(s.clientGroups) != 1 {
			return errors.New("spawn groupId is required unless the scenario has exactly one client group")
		}
		groupID = s.clientGroups[0].Config.ID
	}

	for i := range s.clientGroups {
		group := &s.clientGroups[i]
		if group.Config.ID != groupID {
			continue
		}

		firstIndex := group.NextIndex
		firstAddress := s.scheduledClients + 1
		group.NextIndex += message.Count
		s.scheduledClients += message.Count
		s.scheduleClientBatch(group, firstIndex, message.Count, firstAddress, s.engine.Now())
		return nil
	}
	return fmt.Errorf("unknown client group: %s", groupID)
}
