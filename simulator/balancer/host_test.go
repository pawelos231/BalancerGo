package balancer

import (
	"testing"
	"time"

	"load-balancer/client"
	"load-balancer/host"
	"load-balancer/model"
)

// One shared core handles SYN at 10 us, backend SYN+ACK at 20 us and client ACK at 30 us.
// Routing and mapping activity timestamps change only when each packet finishes CPU service.
func TestBalancerCPUIsSharedByBothPacketDirections(t *testing.T) {
	lb, backends := routingBackends(t, 0)
	lb.SetHostConfig(host.Config{
		Cores:                1,
		PacketProcessingTime: 10 * time.Microsecond,
		QueueCapacity:        2,
	})
	endpoint := routingClient(t, lb, 40000)
	key := endpoint.Key()
	var toBackend, toClient []model.Packet
	lb.clientTx[key] = func(packet model.Packet) { toClient = append(toClient, packet) }
	lb.toServer[backends[0]] = func(packet model.Packet) {
		toBackend = append(toBackend, packet)
		if packet.Flag == model.FlagSYN {
			lb.HandleServerPacket(backends[0], model.Packet{
				Key: key, Flag: model.FlagSYN | model.FlagACK, Ack: 1,
			})
		}
	}

	lb.HandleClientPacket(model.Packet{Key: key, Flag: model.FlagSYN})
	if len(lb.byKey) != 0 || len(toBackend) != 0 {
		t.Fatal("a SYN created a route before finishing CPU service")
	}
	runBalancerCPU(t, lb, 10*time.Microsecond)
	if lb.byKey[key] != backends[0] || len(toBackend) != 1 || len(toClient) != 0 {
		t.Fatal("SYN routing or backend reply skipped the CPU boundary")
	}

	// The final client ACK waits behind the backend's SYN+ACK on the same core.
	lb.HandleClientPacket(model.Packet{Key: key, Flag: model.FlagACK, Seq: 1, Ack: 1})
	runBalancerCPU(t, lb, 20*time.Microsecond)
	if len(toClient) != 1 || lb.connections[key].phase != stateHalfOpen ||
		lb.connections[key].lastActivity != 20*time.Microsecond {
		t.Fatal("opposite packet directions did not share the same core")
	}
	runBalancerCPU(t, lb, 30*time.Microsecond)
	stats := lb.HostStats()
	if len(toBackend) != 2 || lb.connections[key].phase != stateEstablished ||
		stats.ProcessedPackets != 3 || stats.BusyTime != 30*time.Microsecond {
		t.Fatalf("shared CPU did not finish the three handshake packets: %+v", stats)
	}
}

// CPU waiting adds another interval in which a tuple's client or mapping can change.
// Old deliveries must never create or update the replacement's routing state.
func TestBalancerCPURechecksClientAndMappingIdentity(t *testing.T) {
	lb, backends := routingBackends(t, 0)
	lb.SetHostConfig(host.Config{
		Cores:                1,
		PacketProcessingTime: 10 * time.Microsecond,
		QueueCapacity:        2,
	})
	original := routingClient(t, lb, 40000)
	key := original.Key()
	forwarded := 0
	lb.toServer[backends[0]] = func(model.Packet) { forwarded++ }
	lb.HandleClientPacket(model.Packet{Key: key, Flag: model.FlagSYN})
	runBalancerCPU(t, lb, 5*time.Microsecond)

	// The first SYN has not created a mapping, so the same tuple can be registered again.
	replacement := client.NewClient(
		lb.engine,
		key.SrcIP,
		key.SrcPort,
		key.DstIP,
		key.DstPort,
		model.TCP,
	)
	if err := lb.AddClient(replacement, func(model.Packet) {}); err != nil {
		t.Fatal(err)
	}
	runBalancerCPU(t, lb, 10*time.Microsecond)
	if len(lb.connections) != 0 || forwarded != 0 {
		t.Fatal("the old client's queued SYN reached its replacement")
	}

	lb.HandleClientPacket(model.Packet{Key: key, Flag: model.FlagSYN})
	runBalancerCPU(t, lb, 20*time.Microsecond)
	lb.HandleClientPacket(model.Packet{Key: key, Flag: model.FlagACK, Seq: 1, Ack: 1})
	runBalancerCPU(t, lb, 25*time.Microsecond)

	// Replace the mapping while that ACK is being processed.
	lb.ClearConnectionTable()
	if _, err := lb.Route(replacement); err != nil {
		t.Fatal(err)
	}
	current := lb.connections[key]
	runBalancerCPU(t, lb, 30*time.Microsecond)
	if lb.connections[key] != current || current.phase != stateHalfOpen ||
		current.lastActivity != 25*time.Microsecond || forwarded != 1 {
		t.Fatal("an old queued ACK updated the new mapping")
	}
}

func runBalancerCPU(t *testing.T, lb *LoadBalancer, until time.Duration) {
	t.Helper()
	if err := lb.engine.RunUntil(until); err != nil {
		t.Fatal(err)
	}
}
