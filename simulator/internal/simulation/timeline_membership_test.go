package simulation

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
	"load-balancer/network"
	"load-balancer/scenario"
)

// A later resource edit binds to the inserted backend, with no time-zero CPU history.
func TestTimelineCreatesBackendAtInsertionBeforeLaterResourceEdits(t *testing.T) {
	definition := timelineDefinition()
	initial := definition.States["0"]
	initial.Resources.Backends = initial.Resources.Backends[:1]
	definition.States["0"] = initial
	inserted := timelineDefinition().States["0"]
	inserted.Resources.Backends[1].Host = timelineCPUConfig(1, 15*time.Nanosecond, 2)
	definition.States["10"] = inserted
	edited := timelineDefinition().States["0"]
	edited.Resources.Backends[1].Name = "inserted then edited"
	edited.Resources.Backends[1].Host = timelineCPUConfig(2, 3*time.Nanosecond, 2)
	edited.Resources.Backends[1].Network.FromLoadBalancer = scenario.LinkPatch{
		BandwidthBitsPerSecond: scenarioRuntimeValue[int64](4_000_000_000),
	}
	edited.Resources.Backends[1].Network.ToLoadBalancer = scenario.LinkPatch{
		PacketTravelTime: scenarioRuntimeValue(7 * time.Nanosecond),
	}
	definition.States["20"] = edited
	world := newTimelineWorld(t, definition)
	backendID := inserted.Resources.Backends[1].ID
	if len(world.backends) != 1 || len(world.links) != 2 {
		t.Fatal("a future backend allocated resources at time zero")
	}
	advanceTimelinePolicy(t, world, 10*time.Nanosecond)
	backend := world.backends[backendID]
	if backend == nil || !backend.present || backend.server.HostStats().ObservedTime != 0 {
		t.Fatal("backend CPU observation started before insertion")
	}
	var completed []time.Duration
	for range 3 {
		backend.server.HandlePacketIf(model.Packet{Key: model.ClientKey{Protocol: model.TCP}}, func() bool {
			completed = append(completed, world.Now())
			return false
		})
	}
	advanceTimelinePolicy(t, world, 20*time.Nanosecond)
	if world.backends[backendID] != backend || backend.server.Name != "inserted then edited" ||
		backend.server.HostStats().ObservedTime != 10*time.Nanosecond ||
		backend.returnLink.Config().PacketTravelTime != 7*time.Nanosecond ||
		scenarioRuntimeLink(t, world, LoadBalancerNodeID, BackendNodeID(backendID)).Link.Config().
			BandwidthBitsPerSecond != 4_000_000_000 {
		t.Fatal("later edits replaced or missed the inserted backend's resources")
	}
	advanceTimelinePolicy(t, world, 30*time.Nanosecond)
	stats := backend.server.HostStats()
	if len(completed) != 3 || completed[0] != 23*time.Nanosecond ||
		completed[1] != 25*time.Nanosecond || completed[2] != 26*time.Nanosecond ||
		stats.ObservedTime != 20*time.Nanosecond || stats.AvailableCoreTime != 30*time.Nanosecond {
		t.Fatalf("late CPU lifespan or preserved work changed: completions=%v stats=%+v", completed, stats)
	}
}

// Removal wins over same-time delivery and discards only the removed resource's work.
func TestTimelineRemovalRetiresBusyCPUAndPhysicalPaths(t *testing.T) {
	definition := timelineDefinition()
	definition.Fixed.Network.Defaults = scenario.LinkConfig{
		BandwidthBitsPerSecond: 8_000_000_000, PacketTravelTime: 100 * time.Nanosecond,
		QueueCapacity: 3,
	}
	initial := definition.States["0"]
	initial.Resources.Backends[0].Host = timelineCPUConfig(1, 5*time.Nanosecond, 3)
	initial.Resources.LoadBalancer.ForwardingBitsPerSecond = 8_000_000_000
	definition.States["0"] = initial
	removed := timelineDefinition().States["0"]
	removed.Resources.Backends = removed.Resources.Backends[1:]
	removed.Resources.LoadBalancer.ForwardingBitsPerSecond = 8_000_000_000
	definition.States["5"] = removed
	world := newTimelineWorld(t, definition)
	backend := world.backends[initial.Resources.Backends[0].ID]
	output := scenarioRuntimeLink(t, world, LoadBalancerNodeID, BackendNodeID(backend.server.ID))
	completed := 0
	for range 3 {
		backend.server.HandlePacketIf(model.Packet{Key: model.ClientKey{Protocol: model.TCP}}, func() bool {
			completed++
			return false
		})
	}
	for _, size := range []int{1, 20, 1} {
		backend.output.Transmit(size, func() { completed++ })
		backend.returnLink.Transmit(size, func() { completed++ })
	}
	advanceTimelinePolicy(t, world, 4*time.Nanosecond)
	world.ScheduleAt(5*time.Nanosecond, func() {})
	beforeOutput, beforeReturn := output.Stats(), backend.returnLink.Stats()
	if !beforeOutput.Transmitting || beforeOutput.TravellingPackets != 1 || beforeOutput.QueuedPackets != 1 {
		t.Fatal("fixture did not leave serialization, travel and waiting work together")
	}
	world.engine.MaxEvents = world.EventsExecuted() + 1
	err := world.Advance(context.Background(), 5*time.Nanosecond)
	if !errors.Is(err, engine.ErrEventLimit) || backend.present || !backend.server.Killed ||
		len(world.lb.GetServers()) != 1 || world.Info().ConfigurationRevision != 2 {
		t.Fatalf("the removal was split or not applied before ordinary work: %v", err)
	}
	afterOutput, afterReturn := output.Stats(), backend.returnLink.Stats()
	if afterOutput.QueuedPackets != 0 || afterOutput.Transmitting || afterOutput.TravellingPackets != 0 ||
		afterReturn.QueuedPackets != 0 || afterReturn.Transmitting || afterReturn.TravellingPackets != 0 ||
		world.egress.Stats().QueuedPackets != 0 || backend.server.HostStats().DiscardedPackets != 3 {
		t.Fatal("removal retained physical or CPU work")
	}
	if afterOutput.AcceptedBytes != beforeOutput.AcceptedBytes ||
		afterOutput.TransmittedBytes != beforeOutput.TransmittedBytes ||
		afterReturn.AcceptedBytes != beforeReturn.AcceptedBytes ||
		afterReturn.TransmittedBytes != beforeReturn.TransmittedBytes {
		t.Fatal("removal erased historical network counters")
	}
	world.engine.MaxEvents = 100_000
	advanceTimelinePolicy(t, world, time.Millisecond)
	world.SampleTraffic()
	retiredTraffic, known := world.NodeTraffic(BackendNodeID(backend.server.ID))
	if completed != 0 || output.Stats() != afterOutput || backend.returnLink.Stats() != afterReturn ||
		world.egress.Stats().QueuedPackets != 0 || len(readWorld(world).Links) != 4 || !known ||
		retiredTraffic.TransmitBytesPerSecond != float64(afterReturn.TransmittedBytes)/time.Millisecond.Seconds() {
		t.Fatal("a stale callback revived retired work or historical links disappeared")
	}
}

// The old mapping survives removal and cannot send its packets to another backend.
func TestTimelineRemovalKeepsClientsAndMappingsUntilTheirOwnTimeouts(t *testing.T) {
	definition := timelineDefinition()
	definition.Fixed.Execution.Duration = 10 * time.Second
	definition.Fixed.Workload.Defaults.PacketGap = 8 * time.Second
	definition.Fixed.TCP.ClientIdleTimeout = scenarioRuntimeValue(time.Second)
	initial := definition.States["0"]
	initial.Workload.ClientGroups[0].Count = 1
	initial.Resources.LoadBalancer.MappingTimeouts.EstablishedIdleTimeout = scenarioRuntimeValue(50 * time.Millisecond)
	definition.States["0"] = initial
	removed := timelineDefinition().States["0"]
	removed.Workload.ClientGroups[0].Count = 1
	removed.Resources.LoadBalancer.MappingTimeouts = initial.Resources.LoadBalancer.MappingTimeouts
	removed.Resources.Backends = removed.Resources.Backends[1:]
	definition.States["20000000"] = removed
	world := newTimelineWorld(t, definition)
	advanceTimelinePolicy(t, world, 19*time.Millisecond)
	endpoint := world.clients[0].Client
	oldBackend := world.backends[initial.Resources.Backends[0].ID].server
	if !endpoint.IsHandshakeCompleted() || len(world.lb.GetConnectionTable()[oldBackend]) != 1 {
		t.Fatal("fixture did not establish the original mapping")
	}
	advanceTimelinePolicy(t, world, 20*time.Millisecond)
	if endpoint.IsTerminated() || world.lb.GetClient(endpoint.Key()) != endpoint ||
		len(world.lb.GetConnectionTable()[oldBackend]) != 1 || lifecycleRouteCount(world) != 0 {
		t.Fatal("removal closed the client or discarded its retained mapping")
	}
	if selected, err := world.lb.Route(endpoint); err != nil || selected != oldBackend {
		t.Fatal("removal moved the existing mapping")
	}
	fresh := timelinePolicyClient(t, world, 1)
	remaining := world.lb.GetServers()[0]
	if selected, err := world.lb.Route(fresh); err != nil || selected != remaining {
		t.Fatal("new selection included the removed backend")
	}
	world.lb.HandleClientPacket(model.Packet{Key: endpoint.Key(), Flag: model.FlagSYN})
	advanceTimelinePolicy(t, world, 30*time.Millisecond)
	if remaining.NumberOfHandledPackets != 0 || endpoint.IsTerminated() {
		t.Fatal("old traffic was rerouted or removal stopped the remote endpoint")
	}
	advanceTimelinePolicy(t, world, 2*time.Second)
	if !endpoint.IsTerminated() || endpoint.GetTransferState().FailureReason != model.TransferReadTimeout ||
		len(world.lb.GetConnectionTable()[oldBackend]) != 1 {
		t.Fatal("client silence detection did not remain independent of mapping cleanup")
	}
	advanceTimelinePolicy(t, world, 5*time.Second)
	if len(world.lb.GetConnectionTable()[oldBackend]) != 0 {
		t.Fatal("the ordinary LB timeout retained the retired mapping")
	}
}

// A reply already inside shared infrastructure may finish travel but cannot reach LB CPU.
func TestTimelineRemovalRejectsRepliesAlreadyInSharedIngress(t *testing.T) {
	definition := timelineDefinition()
	definition.Fixed.Network.Defaults = scenario.LinkConfig{
		BandwidthBitsPerSecond: 1_000_000_000_000, QueueCapacity: 3,
	}
	initial := definition.States["0"]
	initial.Resources.LoadBalancer.Ingress = scenario.LinkConfig{
		BandwidthBitsPerSecond: 1_000_000_000_000, QueueCapacity: 3,
		PacketTravelTime: 20 * time.Nanosecond,
	}
	definition.States["0"] = initial
	removed := initial
	removed.Resources.Backends = initial.Resources.Backends[1:]
	definition.States["10"] = removed
	world := newTimelineWorld(t, definition)
	endpoint := timelinePolicyClient(t, world, 0)
	backend, err := world.lb.Route(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	backend.HandlePacket(model.Packet{Key: endpoint.Key(), Flag: model.FlagSYN})
	advanceTimelinePolicy(t, world, 5*time.Nanosecond)
	if world.ingress.Stats().TravellingPackets != 1 || world.lb.HostStats().OfferedPackets != 0 {
		t.Fatal("fixture did not leave the backend reply travelling inside shared ingress")
	}
	advanceTimelinePolicy(t, world, 40*time.Nanosecond)
	if world.ingress.Stats().DeliveredPackets != 1 || world.lb.HostStats().OfferedPackets != 0 ||
		endpoint.IsTerminated() || !world.lb.HasClient(endpoint) {
		t.Fatal("a retired reply reached LB processing or changed the independent client")
	}
}

// Unrelated retirement cannot consume either a retained or a newly enabled link's RNG.
func TestTimelineMembershipPreservesResourceRandomStreams(t *testing.T) {
	type arrival struct {
		at     time.Duration
		marked bool
	}
	type trace struct {
		arrivals [2][]arrival
		stats    [2]network.Stats
	}
	var baseline trace
	for _, removeUnrelated := range []bool{false, true} {
		definition := timelineDefinition()
		definition.Fixed.Network.Defaults = scenario.LinkConfig{
			BandwidthBitsPerSecond: 8_000_000_000, QueueCapacity: 64,
		}
		stochastic := scenario.LinkPatch{
			LossRate:                    scenarioRuntimeValue(0.4),
			QueuePolicy:                 scenarioRuntimeValue("threshold"),
			CongestionThresholdPercent:  scenarioRuntimeValue(1.0),
			CongestionSignalProbability: scenarioRuntimeValue(0.5),
		}
		initial := definition.States["0"]
		initial.Resources.Backends[0].Network.ToLoadBalancer = stochastic
		definition.States["0"] = initial
		inserted := initial
		inserted.Resources.Backends = append([]scenario.BackendStateConfig(nil), initial.Resources.Backends...)
		inserted.Resources.Backends = append(inserted.Resources.Backends, scenario.BackendStateConfig{
			ID: "later", Address: "10.0.0.3", Port: 80,
		})
		definition.States["10"] = inserted
		edited := inserted
		edited.Resources.Backends = append([]scenario.BackendStateConfig(nil), inserted.Resources.Backends...)
		edited.Resources.Backends[2].Network.ToLoadBalancer = stochastic
		if removeUnrelated {
			edited.Resources.Backends = []scenario.BackendStateConfig{
				edited.Resources.Backends[0], edited.Resources.Backends[2],
			}
		}
		definition.States["20"] = edited
		world := newTimelineWorld(t, definition)
		var observed trace
		sendBurst := func(link *network.Link, index int) {
			for range 32 {
				link.TransmitECN(1, network.Capable, func(marked bool) {
					observed.arrivals[index] = append(observed.arrivals[index], arrival{world.Now(), marked})
				})
			}
		}
		retained := world.backends[initial.Resources.Backends[0].ID].returnLink
		sendBurst(retained, 0)
		advanceTimelinePolicy(t, world, 25*time.Nanosecond)
		late := world.backends["later"].returnLink
		sendBurst(retained, 0)
		sendBurst(late, 1)
		advanceTimelinePolicy(t, world, 100*time.Nanosecond)
		observed.stats = [2]network.Stats{retained.Stats(), late.Stats()}
		for _, stats := range observed.stats {
			if stats.LostPackets == 0 || stats.ECNMarkedPackets == 0 {
				t.Fatal("fixture did not exercise both loss and congestion random streams")
			}
		}
		if !removeUnrelated {
			baseline = observed
		} else if !reflect.DeepEqual(observed, baseline) {
			t.Fatal("unrelated backend retirement changed loss or congestion sequences")
		}
	}
}
