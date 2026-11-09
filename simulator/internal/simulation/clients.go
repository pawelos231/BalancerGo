package simulation

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"load-balancer/client"
	"load-balancer/engine"
	"load-balancer/model"
	"load-balancer/network"
	"load-balancer/scenario"
)

// runtimeClientGroup reserves identities without constructing all its clients.
// Objects stay at stable addresses, even when future groups become present.
type runtimeClientGroup struct {
	Config         scenario.NormalizedClientGroup
	AddressBase    int
	ReservedCount  int
	NextIndex      int // v1 reserves ahead; v2 advances this index at birth.
	Born           int
	Present        bool
	LastBornAt     time.Duration
	ArrivalEventID engine.EventID
	ClientIndices  []int // Positions in World.clients, in this group's birth order.
}

type clientGroupReservation struct {
	ID    string
	Count int
}

// runtimeClient ties a created TCP client to its scenario and topology identity.
// Closed clients remain here so their links retain historical counters.
type runtimeClient struct {
	ID      string
	GroupID string
	Index   int
	Config  scenario.ClientConfig
	Client  *client.Client
	ToInput *network.Link
	Output  *network.Output
}

// initializeClients reserves address ranges and schedules one arrival per group.
// The remaining arrivals form a chain, rather than one upfront event per client.
func (s *World) initializeClients(reservations []clientGroupReservation) {
	groups := s.configuration.ClientGroups
	if reservations == nil {
		for _, group := range groups {
			reservations = append(reservations, clientGroupReservation{group.ID, group.Count})
		}
	}
	sort.Slice(reservations, func(i, j int) bool {
		return reservations[i].ID < reservations[j].ID
	})

	s.clientGroups = make(map[string]*runtimeClientGroup, len(reservations))
	nextAddress := 1
	for _, reservation := range reservations {
		group := &runtimeClientGroup{
			AddressBase: nextAddress, ReservedCount: reservation.Count,
		}
		s.clientGroups[reservation.ID] = group
		s.clientGroupOrder = append(s.clientGroupOrder, group)
		nextAddress += reservation.Count
	}

	for _, config := range groups {
		group := s.clientGroups[config.ID]
		group.Config = config
		group.Present = true
		s.scheduledClients += config.Count
		if s.timeline != nil {
			continue
		}
		// v1 reserves initial indices before independent manual spawn batches.
		group.NextIndex = config.Count
		s.scheduleClientBatch(
			group,
			0,
			group.Config.Count,
			group.AddressBase,
			group.Config.StartAt,
		)
	}
	if s.timeline != nil {
		for _, group := range s.clientGroupOrder {
			s.planClientArrival(group, 0)
		}
	}
}

// timelineClientReservations stays compact: one range per distinct group.
// Maximum budgets include future declarations and never release removed slots.
func timelineClientReservations(
	initial []scenario.NormalizedClientGroup,
	commits []configurationCommit,
) []clientGroupReservation {
	maximum := make(map[string]int)
	include := func(config scenario.NormalizedClientGroup) {
		maximum[config.ID] = max(maximum[config.ID], config.Count)
	}
	for _, group := range initial {
		include(group)
	}
	for _, commit := range commits {
		for _, group := range commit.groups.Added {
			include(group)
		}
		for _, update := range commit.groups.Updated {
			include(update.Config)
		}
	}
	reservations := make([]clientGroupReservation, 0, len(maximum))
	for id, count := range maximum {
		reservations = append(reservations, clientGroupReservation{id, count})
	}
	return reservations
}

// planClientArrival owns the sole pending birth for a timeline generator.
// Check the remaining horizon before adding durations, including MaxInt64 gaps.
func (s *World) planClientArrival(group *runtimeClientGroup, at time.Duration) {
	if !group.Present || group.NextIndex >= group.Config.Count {
		return
	}
	nextAt := max(at, group.Config.StartAt)
	if group.Born > 0 {
		if group.Config.ClientGap > s.EndTime()-group.LastBornAt {
			return
		}
		nextAt = max(at, group.LastBornAt+group.Config.ClientGap)
	}
	group.ArrivalEventID = s.engine.ScheduleAt(nextAt, func() {
		group.ArrivalEventID = 0
		index := group.NextIndex
		group.NextIndex++
		s.birthClient(group, index, group.AddressBase+index)
		s.planClientArrival(group, s.Now())
	})
}

// birthClient freezes the stream at birth; later edits only update its network.
func (s *World) birthClient(group *runtimeClientGroup, index, address int) {
	config := clientSettings(group.Config, index)
	group.ClientIndices = append(group.ClientIndices, len(s.clients))
	group.Born++
	group.LastBornAt = s.Now()
	s.startClient(config, group.Config.ID, index, address)
}

// clientSettings also handles v1 manual slots beyond the original birth budget.
// Callers already own the slot reservation; sparse overrides do not impose Count.
func clientSettings(group scenario.NormalizedClientGroup, index int) scenario.ClientConfig {
	config := scenario.ClientConfig{
		ID:      "client/" + group.ID + "/" + strconv.Itoa(index),
		Network: group.Network, Stream: group.Stream,
	}
	if override, ok := group.Override(index); ok {
		config.Network, config.Stream = override.Network, override.Stream
	}
	return config
}

// scheduleClientBatch creates each client at its own virtual arrival time.
// The address range is reserved already, including clients beyond the end time.
func (s *World) scheduleClientBatch(
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
		address := firstAddress + nextIndex - firstIndex
		s.birthClient(group, nextIndex, address)
		nextIndex++
		if nextIndex-firstIndex < count {
			s.engine.ScheduleAfter(group.Config.ClientGap, arrival)
		}
	}
	s.engine.ScheduleAt(startAt, arrival)
}

// startClient creates two private links and starts the configured TCP stream.
func (s *World) startClient(
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
	cl.SetReceiveBufferBytes(s.fixed.TCP.ReceiveBufferBytes)
	congestion := s.fixed.TCP.CongestionControl
	cl.SetCongestionConfigWithMSS(model.CongestionConfig{
		Mode:                           model.CongestionMode(congestion.Mode),
		FastRetransmit:                 congestion.FastRetransmit,
		CubicFastConvergence:           congestion.CubicFastConvergence,
		InitialWindowSegments:          congestion.InitialWindowSegments,
		MaximumWindowSegments:          congestion.MaximumWindowSegments,
		InitialSlowStartThresholdBytes: congestion.InitialSlowStartThresholdBytes,
	}, s.fixed.TCP.MSSBytes)
	cl.SetSACKEnabled(s.fixed.TCP.SACKEnabled)
	cl.SetECNEnabled(s.fixed.TCP.ECNEnabled)

	// Each connection estimates its own RTT using the scenario's timer policy.
	cl.SetRTOConfig(configuredRTO(s.fixed.TCP.RTO))
	cl.SetCloseConfig(configuredClose(s.fixed.TCP.Close))
	cl.SetIdleTimeout(s.fixed.TCP.ClientIdleTimeout)

	toInput := s.addLink(
		"link/"+config.ID+"/to-lb",
		config.ID,
		LoadBalancerNodeID,
		config.Network.ToLoadBalancer,
	)
	toClient := s.addOutput(
		"link/"+config.ID+"/from-lb",
		LoadBalancerNodeID,
		config.ID,
		config.Network.FromLoadBalancer,
	)

	// Every SYN, data packet and retry crosses both access and shared ingress.
	cl.SetSender(func(pkt model.Packet) {
		transmitPacket(toInput, pkt, func(pkt model.Packet) {
			transmitPacket(s.clientIngress, pkt, func(pkt model.Packet) {
				if s.lb.HasClient(cl) {
					s.lb.HandleClientPacket(pkt)
				}
			})
		})
	})
	cl.OnPayloadAcknowledged = func(bytes int) {
		s.payload.AcknowledgedBytes += uint64(bytes)
	}
	cl.OnStreamComplete = func() {
		s.completedStreams++
		s.completedPayloadBytes += uint64(config.Stream.Bytes)
	}
	cl.OnClose = func() { s.closedClients++ }

	err := s.lb.AddClient(cl, func(pkt model.Packet) {
		transmitPacket(toClient, pkt, func(pkt model.Packet) {
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
		ToInput: toInput,
		Output:  toClient,
	})
	s.createdClients++
	cl.SendCompleteStream(config.Stream.Bytes, config.Stream.PacketGap)
	cl.EstablishConnection()
}

// SpawnClients adds a batch to one group's settings, starting at the current time.
// Multiple groups require an explicit groupId so profiles are never guessed.
func (s *World) SpawnClients(count int, groupID string) error {
	if s.timeline != nil {
		return ErrTimelineManualMutation
	}

	invalidCount := count < 1 || count > 10000
	addressRangeExceeded := s.scheduledClients > scenario.MaxClients-count
	if invalidCount || addressRangeExceeded {
		return errors.New("spawn count must be 1..10000 and fit the client address range")
	}

	if groupID == "" {
		if len(s.clientGroups) != 1 {
			return errors.New("spawn groupId is required unless the scenario has exactly one client group")
		}
		groupID = s.clientGroupOrder[0].Config.ID
	}

	if group, ok := s.clientGroups[groupID]; ok {
		firstIndex := group.NextIndex
		firstAddress := s.scheduledClients + 1
		group.NextIndex += count
		s.scheduledClients += count
		s.scheduleClientBatch(group, firstIndex, count, firstAddress, s.engine.Now())
		return nil
	}
	return fmt.Errorf("unknown client group: %s", groupID)
}
