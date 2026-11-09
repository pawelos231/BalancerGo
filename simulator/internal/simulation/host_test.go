package simulation

import (
	"context"
	"testing"
	"time"

	"load-balancer/client"
	"load-balancer/model"
	"load-balancer/scenario"
)

// Fast links isolate the host queue; both variants send the same workload.
// At 10 us, one core can process at most 500 packets in five virtual milliseconds.
func TestBackendCPUBackpressureAndDrops(t *testing.T) {
	var confirmed [2]uint64
	for index, cost := range []time.Duration{0, 10 * time.Microsecond} {
		definition := hostTestScenario()
		for i := range definition.States["0"].Resources.Backends {
			editInitialState(&definition, func(state *scenario.StateConfig) { state.Resources.Backends[i].Host = hostTestConfig(cost) })
		}
		world := newTimelineWorld(t, definition)
		if err := world.Advance(context.Background(), world.EndTime()); err != nil {
			t.Fatal(err)
		}
		confirmed[index] = world.Info().Payload.AcknowledgedBytes
		world.VisitBackends(func(backend BackendObservation) {
			state := backend.State.Host
			if cost == 0 {
				if state.Enabled || state.QueueDroppedPackets != 0 || state.BusyTime != 0 {
					t.Fatalf("disabled CPU constrained packets: %+v", state)
				}
				return
			}
			if state.Utilization() < 0.99 || state.QueueDroppedPackets == 0 {
				t.Fatalf("traffic did not expose the backend CPU bottleneck: %+v", state)
			}
			if state.ProcessedPackets > 500 || state.MaxQueuedPackets != 128 {
				t.Fatalf("CPU exceeded its capacity or never filled its FIFO: %+v", state)
			}
		})
	}
	if confirmed[0] <= confirmed[1] || confirmed[1] == 0 {
		t.Fatalf("expected slower but progressing TCP: ideal=%d limited=%d", confirmed[0], confirmed[1])
	}
}

// Both client packets and returning ACKs consume the same LB CPU budget.
func TestLoadBalancerCPUUsesOneSharedPool(t *testing.T) {
	definition := hostTestScenario()
	editInitialState(&definition, func(state *scenario.StateConfig) {
		state.Resources.LoadBalancer.Host = hostTestConfig(10 * time.Microsecond)
	})
	world := newTimelineWorld(t, definition)
	if err := world.Advance(context.Background(), world.EndTime()); err != nil {
		t.Fatal(err)
	}
	state := world.Info().LoadBalancerHost
	if state.Utilization() < 0.99 || state.QueueDroppedPackets == 0 || state.ProcessedPackets > 500 {
		t.Fatalf("LB did not respect one core shared by both directions: %+v", state)
	}
	world.VisitBackends(func(backend BackendObservation) {
		if backend.State.Host.Enabled {
			t.Fatal("LB-only scenario unexpectedly enabled backend CPU")
		}
	})
}

// The original client's SYN is waiting for backend CPU when its tuple is reused.
// Completion must recheck the original instance, rather than opening a new TCP state.
func TestBackendCPUDoesNotDeliverToReplacementClient(t *testing.T) {
	definition := baseScenario()
	editInitialState(&definition, func(state *scenario.StateConfig) { state.Resources.Backends[0].Host = hostTestConfig(time.Millisecond) })
	world := newTimelineWorld(t, definition)
	if err := world.Advance(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	original := world.clients[0].Client
	key := original.Key()
	backend := world.lb.GetServers()[0]
	backend.HandlePacketIf(model.Packet{Key: key, Flag: model.FlagSYN}, func() bool {
		return world.lb.HasClient(original)
	})

	replacement := client.NewClient(
		world.engine, key.SrcIP, key.SrcPort, key.DstIP, key.DstPort, key.Protocol,
	)
	if err := world.lb.AddClient(replacement, func(model.Packet) {}); err != nil {
		t.Fatal(err)
	}
	if err := world.Advance(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if backend.HalfOpenConnections != 0 || backend.HostStats().ProcessedPackets != 1 {
		t.Fatalf("stale CPU work reached the replacement connection: %+v", backend.GetState())
	}
}

func hostTestConfig(cost time.Duration) scenario.HostConfig {
	cores, queue := 1, 128
	return scenario.HostConfig{Cores: &cores, PacketProcessingTime: &cost, QueueCapacity: &queue}
}

func hostTestScenario() scenario.Scenario {
	definition := baseScenario()
	definition.Fixed.Execution.Duration = 5 * time.Millisecond
	definition.Fixed.Execution.MaxEvents = 5_000_000
	definition.Fixed.Network.Defaults.BandwidthBitsPerSecond = 100_000_000_000
	definition.Fixed.Network.Defaults.PacketTravelTime = 0
	definition.Fixed.Network.Defaults.QueueCapacity = 65536
	editInitialState(&definition, func(state *scenario.StateConfig) {
		state.Resources.LoadBalancer.Ingress = definition.Fixed.Network.Defaults

		state.Resources.LoadBalancer.ForwardingBitsPerSecond = 100_000_000_000

		state.Workload.ClientGroups[0].Count = 64
	})
	definition.Fixed.Workload.Defaults.Bytes = 64 * 1024 * 1024
	definition.Fixed.Workload.Defaults.PacketGap = 0
	// Loss recovery stays active while idle cleanup cannot end a stalled transfer.
	mss := 1460
	mode := "newReno"
	sack := true
	noIdle := time.Duration(0)
	definition.Fixed.TCP.MSSBytes = &mss
	definition.Fixed.TCP.CongestionControl.Mode = &mode
	definition.Fixed.TCP.SACKEnabled = &sack
	definition.Fixed.TCP.ClientIdleTimeout = &noIdle
	return definition
}
