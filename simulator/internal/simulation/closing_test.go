package simulation

import (
	"context"
	"testing"
	"time"

	"load-balancer/model"
)

// Each closing message is dropped once after crossing its access and ingress links.
// Separate 6-second TIME_WAIT and LB closing limits leave time for the backend FIN retry.
func TestWorldClosingRecoversLostControls(t *testing.T) {
	for _, lost := range []string{"client FIN", "backend FIN", "final ACK"} {
		t.Run(lost, func(t *testing.T) {
			definition := baseScenario()
			definition.Execution.Duration = 15 * time.Second
			definition.Resources.Backends = definition.Resources.Backends[:1]
			definition.Workload.ClientGroups[0].Count = 1
			definition.Workload.Defaults.Bytes = 2050
			msl := 3 * time.Second
			definition.TCP.Close.MSL = &msl
			closingIdle := 6 * time.Second
			definition.Resources.LoadBalancer.ClosingIdleTimeout = &closingIdle
			// Observing FIN selects the closing limit instead of this short established limit.
			idle := 10 * time.Millisecond
			definition.Resources.LoadBalancer.EstablishedIdleTimeout = &idle
			world := New(normalizedRuntimeScenario(t, definition))
			discarded := false
			backendFINs := 0
			finalACKs := 0
			interceptClosingTraffic(t, world, func(fromClient bool, packet model.Packet) bool {
				fin := packet.Flag&model.FlagFIN != 0
				finalACK := fromClient && packet.Flag == model.FlagACK && packet.Ack == 2
				if !fromClient && fin {
					backendFINs++
				}
				if finalACK {
					finalACKs++
				}
				target := (lost == "client FIN" && fromClient && fin) ||
					(lost == "backend FIN" && !fromClient && fin) ||
					(lost == "final ACK" && finalACK)
				if target && !discarded {
					discarded = true
					return true
				}
				return false
			})

			if err := runWorld(context.Background(), world); err != nil {
				t.Fatal(err)
			}
			client := world.clients[0].Client
			state := client.GetState()
			if !discarded || !client.IsTerminated() || state.Connection.Outcome != model.CloseGraceful ||
				state.Transfer.Status != model.TransferSucceeded || state.Transfer.DataRetransmissions != 0 {
				t.Fatalf("lost %s was not recovered gracefully: %+v", lost, state)
			}
			// The LB expires its own route at a later sweep; endpoint history remains.
			backend := world.lb.GetServers()[0]
			if world.Info().ClosedClients != 1 || len(world.lb.GetConnectionTable()[backend]) != 0 ||
				backend.ActiveConnections != 0 || world.lb.GetClient(client.Key()) != client {
				t.Fatal("closing lost endpoint history or left active route/connection state behind")
			}
			if lost == "final ACK" && (backendFINs != 2 || finalACKs != 2) {
				t.Fatalf("TIME_WAIT did not answer the retried FIN: FINs=%d ACKs=%d", backendFINs, finalACKs)
			}
			assertNativePayloadProgress(t, world, 2050, 2050)
		})
	}
}

// A backend sends a real RST through its return link, shared ingress and LB output.
// Reset is a connection outcome and only fails the still-unfinished payload attempt.
func TestWorldResetTravelsThroughBalancer(t *testing.T) {
	definition := baseScenario()
	definition.Resources.Backends = definition.Resources.Backends[:1]
	definition.Workload.ClientGroups[0].Count = 1
	definition.Workload.Defaults.Bytes = 10000
	definition.Workload.Defaults.PacketGap = 100 * time.Millisecond
	world := New(normalizedRuntimeScenario(t, definition))
	runIngressUntil(t, world, 20*time.Millisecond)
	client := world.clients[0].Client
	if !client.IsHandshakeCompleted() {
		t.Fatal("reset fixture did not complete its handshake")
	}
	world.lb.GetServers()[0].AbortConnection(client.Key())
	if client.IsTerminated() {
		t.Fatal("backend reset reached the client without traversing the network")
	}
	runIngressUntil(t, world, 50*time.Millisecond)
	state := client.GetState()
	if !client.IsTerminated() || state.Connection.Outcome != model.CloseReset ||
		state.Transfer.FailureReason != model.TransferReset {
		t.Fatalf("routed reset did not terminate the unfinished stream: %+v", state)
	}
}

// Packet interception preserves all configured physical hops; only delivery at LB is discarded.
func interceptClosingTraffic(
	t *testing.T,
	world *World,
	discard func(fromClient bool, packet model.Packet) bool,
) {
	t.Helper()
	runIngressUntil(t, world, 0)
	client := world.clients[0].Client
	backend := world.lb.GetServers()[0]
	clientLink := scenarioRuntimeLink(t, world, world.clients[0].ID, LoadBalancerNodeID)
	backendLink := scenarioRuntimeLink(t, world, BackendNodeID(backend.ID), LoadBalancerNodeID)
	client.SetSender(func(packet model.Packet) {
		drop := discard(true, packet)
		clientLink.Link.Transmit(packet.WireSize(), func() {
			world.clientIngress.Transmit(packet.WireSize(), func() {
				if !drop && world.lb.HasClient(client) {
					world.lb.HandleClientPacket(packet)
				}
			})
		})
	})
	backend.SetSender(func(packet model.Packet) {
		drop := discard(false, packet)
		backendLink.Link.Transmit(packet.WireSize(), func() {
			world.backendIngress.Transmit(packet.WireSize(), func() {
				if !drop && world.lb.HasClient(client) {
					world.lb.HandleServerPacket(backend, packet)
				}
			})
		})
	})
}
