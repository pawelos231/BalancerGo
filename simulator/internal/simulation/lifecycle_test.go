package simulation

import (
	"testing"
	"time"

	"load-balancer/model"
	"load-balancer/scenario"
)

// lifecycleScenario completes the handshake, then waits 30 seconds before data.
// With client idle cleanup disabled, each test can isolate another owner's timer.
func lifecycleScenario() scenario.ScenarioV2 {
	definition := baseScenario()
	definition.Fixed.Execution.Duration = 40 * time.Second
	editInitialState(&definition, func(state *scenario.StateConfig) {
		state.Resources.Backends = state.Resources.Backends[:1]
		state.Workload.ClientGroups[0].Count = 1
	})
	definition.Fixed.Workload.Defaults.Bytes = 10 * model.SEGMENT_SIZE
	definition.Fixed.Workload.Defaults.PacketGap = 30 * time.Second

	disabled := time.Duration(0)
	definition.Fixed.TCP.ClientIdleTimeout = &disabled
	return definition
}

// A stopped client leaves two independent records: the backend expires at 1 s,
// while the LB's 6-second idle limit is noticed by its sweep at 10 s.
func TestClientStopLeavesBackendAndBalancerToTheirOwnTimers(t *testing.T) {
	definition := lifecycleScenario()
	backendIdle := time.Second
	balancerIdle := 6 * time.Second
	definition.Fixed.TCP.BackendEstablishedIdleTimeout = &backendIdle
	editInitialState(&definition, func(state *scenario.StateConfig) {
		state.Resources.LoadBalancer.MappingTimeouts.EstablishedIdleTimeout = &balancerIdle
	})

	world := newTimelineWorld(t, definition)
	runIngressUntil(t, world, 20*time.Millisecond)
	client := world.clients[0].Client
	backend := world.lb.GetServers()[0]
	if backend.GetConnectionState(client.Key()).State != model.TCPEstablished {
		t.Fatal("the fixture did not establish the backend connection")
	}

	if err := world.StopClient(world.clients[0].ID); err != nil {
		t.Fatal(err)
	}
	if !client.IsTerminated() || lifecycleRouteCount(world) != 1 ||
		backend.GetConnectionState(client.Key()).State != model.TCPEstablished {
		t.Fatal("stopping the client also removed another owner's state")
	}

	// The backend's idle deadline does not remove the LB mapping.
	runIngressUntil(t, world, 2*time.Second)
	if backend.GetConnectionState(client.Key()).State != model.TCPClosed ||
		lifecycleRouteCount(world) != 1 {
		t.Fatal("backend cleanup did not remain local")
	}

	// Only the LB's later cleanup removes its route.
	runIngressUntil(t, world, 10*time.Second)
	if lifecycleRouteCount(world) != 0 {
		t.Fatal("the LB retained a route beyond its own idle deadline")
	}
}

// The LB can forget a quiet flow while both TCP endpoints remain established.
func TestBalancerIdleTimeoutDoesNotStopEitherEndpoint(t *testing.T) {
	definition := lifecycleScenario()
	balancerIdle := time.Second
	editInitialState(&definition, func(state *scenario.StateConfig) {
		state.Resources.LoadBalancer.MappingTimeouts.EstablishedIdleTimeout = &balancerIdle
	})

	world := newTimelineWorld(t, definition)
	runIngressUntil(t, world, 20*time.Millisecond)
	client := world.clients[0].Client
	backend := world.lb.GetServers()[0]
	if lifecycleRouteCount(world) != 1 {
		t.Fatal("the fixture did not create an LB route")
	}

	// Cleanup runs every five seconds; neither endpoint has an idle deadline here.
	runIngressUntil(t, world, 6*time.Second)
	if lifecycleRouteCount(world) != 0 {
		t.Fatal("the LB's idle route did not expire")
	}
	if client.IsTerminated() || !client.IsHandshakeCompleted() ||
		backend.GetConnectionState(client.Key()).State != model.TCPEstablished {
		t.Fatal("the LB timeout terminated a remote TCP endpoint")
	}
}

// A stopped backend causes silence; only the client's own deadline ends its wait.
func TestBackendStopDoesNotImmediatelyTerminateItsClient(t *testing.T) {
	definition := lifecycleScenario()
	clientIdle := time.Second
	definition.Fixed.TCP.ClientIdleTimeout = &clientIdle

	world := newTimelineWorld(t, definition)
	runIngressUntil(t, world, 20*time.Millisecond)
	client := world.clients[0].Client
	backend := world.lb.GetServers()[0]
	if !client.IsHandshakeCompleted() {
		t.Fatal("the fixture did not complete its handshake")
	}

	if err := world.StopBackend(backend.ID); err != nil {
		t.Fatal(err)
	}
	if client.IsTerminated() || lifecycleRouteCount(world) != 1 {
		t.Fatal("backend shutdown immediately stopped the client or removed its route")
	}

	runIngressUntil(t, world, 2*time.Second)
	state := client.GetTransferState()
	if !client.IsTerminated() || state.FailureReason != model.TransferReadTimeout {
		t.Fatalf("client did not detect silence through its own timeout: %+v", state)
	}
	if lifecycleRouteCount(world) != 1 {
		t.Fatal("the client's timeout also removed the LB route")
	}
}

// Clearing the LB's table is a local operation, rather than a remote shutdown.
func TestClearingBalancerRoutesKeepsBothEndpointsAlive(t *testing.T) {
	world := newTimelineWorld(t, lifecycleScenario())
	runIngressUntil(t, world, 20*time.Millisecond)
	client := world.clients[0].Client
	backend := world.lb.GetServers()[0]

	world.ClearConnections()
	if lifecycleRouteCount(world) != 0 {
		t.Fatal("clearing the LB did not remove its route")
	}
	if client.IsTerminated() ||
		backend.GetConnectionState(client.Key()).State != model.TCPEstablished {
		t.Fatal("clearing LB routes also cleared an endpoint")
	}
}

// A SYN already travelling still reaches the backend after its sender stops.
func TestClientCrashDoesNotEraseItsTravellingSYN(t *testing.T) {
	definition := lifecycleScenario()
	handshakeTimeout := time.Second
	definition.Fixed.TCP.BackendHalfOpenIdleTimeout = &handshakeTimeout
	world := newTimelineWorld(t, definition)
	runIngressUntil(t, world, 0)
	client := world.clients[0].Client
	backend := world.lb.GetServers()[0]
	if backend.NumberOfHandledPackets != 0 {
		t.Fatal("the SYN arrived before traversing its links")
	}

	client.Shutdown()
	runIngressUntil(t, world, 10*time.Millisecond)
	if backend.GetConnectionState(client.Key()).State != model.TCPSynReceived ||
		backend.HalfOpenConnections != 1 {
		t.Fatal("the client's crash erased the SYN already in the network")
	}

	// The backend frees this incomplete handshake on its own configured deadline.
	runIngressUntil(t, world, 2*time.Second)
	if backend.HalfOpenConnections != 0 || lifecycleRouteCount(world) != 1 {
		t.Fatal("half-open cleanup did not remain local to the backend")
	}
}

// The backend's SYN+ACK leaves at about 2 ms and arrives at the client after 4 ms.
// Killing the backend between those times must not erase the already-sent reply.
func TestBackendCrashDoesNotEraseItsTravellingSYNACK(t *testing.T) {
	world := newTimelineWorld(t, lifecycleScenario())
	runIngressUntil(t, world, 2500*time.Microsecond)
	client := world.clients[0].Client
	backend := world.lb.GetServers()[0]
	if backend.HalfOpenConnections != 1 || client.IsHandshakeCompleted() {
		t.Fatal("the fixture did not stop while SYN+ACK was travelling")
	}

	backend.Shutdown()
	runIngressUntil(t, world, 10*time.Millisecond)
	if !client.IsHandshakeCompleted() || client.IsTerminated() {
		t.Fatal("the backend's crash erased its already-sent SYN+ACK")
	}
	if backend.ActiveConnections != 0 || backend.HalfOpenConnections != 0 {
		t.Fatal("the stopped backend accepted the client's later handshake ACK")
	}
}

// The LB initially still selects a crashed backend, then excludes it after a probe timeout.
func TestBalancerLearnsBackendFailureThroughItsProbe(t *testing.T) {
	definition := lifecycleScenario()
	editInitialState(&definition, func(state *scenario.StateConfig) {
		state.Workload.ClientGroups[0].Count = 2
		state.Workload.ClientGroups[0].ClientGap = 1600 * time.Millisecond
	})
	world := newTimelineWorld(t, definition)
	backend := world.lb.GetServers()[0]
	backend.Shutdown()

	// No probe has timed out yet, so physical failure alone cannot change selection.
	runIngressUntil(t, world, 10*time.Millisecond)
	if lifecycleRouteCount(world) != 1 {
		t.Fatal("the LB learned the backend's failure before observing a probe timeout")
	}

	// The first probe starts at 1 s and expires at 1.5 s; later clients get no route.
	runIngressUntil(t, world, 1600*time.Millisecond)
	runIngressUntil(t, world, 1610*time.Millisecond)
	if len(world.clients) != 2 || lifecycleRouteCount(world) != 1 {
		t.Fatal("the LB kept selecting the backend after its failed health probe")
	}
	if world.clients[0].Client.IsTerminated() || world.clients[1].Client.IsTerminated() {
		t.Fatal("the failed health probe terminated a client endpoint")
	}
}

// Unsupported control shapes cannot replace the established mapping's idle policy.
func TestBalancerIgnoresInvalidClosingControls(t *testing.T) {
	for _, test := range []struct {
		name   string
		packet model.Packet
	}{
		{
			name: "reset with payload",
			packet: model.Packet{
				Flag: model.FlagRST | model.FlagACK,
				Seq:  999,
				Ack:  1,
				Size: 1,
			},
		},
		{
			name: "FIN with payload",
			packet: model.Packet{
				Flag: model.FlagFIN | model.FlagACK,
				Seq:  1,
				Ack:  1,
				Size: 1,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition := lifecycleScenario()
			shortCloseLimit := time.Second
			editInitialState(&definition, func(state *scenario.StateConfig) {
				state.Resources.LoadBalancer.MappingTimeouts.ClosingIdleTimeout = &shortCloseLimit

				state.Resources.LoadBalancer.MappingTimeouts.ResetIdleTimeout = &shortCloseLimit
			})
			world := newTimelineWorld(t, definition)
			runIngressUntil(t, world, time.Second)
			client := world.clients[0].Client
			backend := world.lb.GetServers()[0]

			// The packet reaches LB processing, then the backend rejects it.
			packet := test.packet
			packet.Key = client.Key()
			world.lb.HandleClientPacket(packet)
			runIngressUntil(t, world, 5*time.Second)

			if lifecycleRouteCount(world) != 1 ||
				client.GetConnectionState().State != model.TCPEstablished ||
				backend.GetConnectionState(client.Key()).State != model.TCPEstablished {
				t.Fatal("an invalid control packet closed an endpoint or shortened LB retention")
			}
		})
	}
}

// The LB sees a valid RST shape; the TCP receiver rejects its wrong byte position.
// Their independent records can therefore disagree without terminating either endpoint.
func TestBalancerResetObservationDoesNotValidateEndpointSequence(t *testing.T) {
	definition := lifecycleScenario()
	resetIdle := time.Second
	editInitialState(&definition, func(state *scenario.StateConfig) {
		state.Resources.LoadBalancer.MappingTimeouts.ResetIdleTimeout = &resetIdle
	})
	world := newTimelineWorld(t, definition)
	runIngressUntil(t, world, time.Second)
	client := world.clients[0].Client
	backend := world.lb.GetServers()[0]
	world.lb.HandleClientPacket(model.Packet{
		Key: client.Key(), Flag: model.FlagRST | model.FlagACK, Seq: 999, Ack: 1,
	})
	runIngressUntil(t, world, 2*time.Second)
	if lifecycleRouteCount(world) != 1 ||
		client.GetConnectionState().State != model.TCPEstablished ||
		backend.GetConnectionState(client.Key()).State != model.TCPEstablished {
		t.Fatal("the wrong-sequence reset changed endpoint state or bypassed the cleanup sweep")
	}
	runIngressUntil(t, world, 5*time.Second)
	if lifecycleRouteCount(world) != 0 || client.IsTerminated() ||
		backend.GetConnectionState(client.Key()).State != model.TCPEstablished ||
		world.lb.GetClient(client.Key()) != client {
		t.Fatal("LB reset retention did not expire independently of endpoint validation")
	}
}

func TestBalancerClosingPoliciesIgnoreEndpointRTOAndMSL(t *testing.T) {
	for _, endpointLimit := range []time.Duration{time.Second, time.Minute} {
		t.Run(endpointLimit.String(), func(t *testing.T) {
			for _, flag := range []model.Flag{model.FlagFIN | model.FlagACK, model.FlagRST | model.FlagACK} {
				definition := lifecycleScenario()
				definition.Fixed.TCP.Close.MSL = &endpointLimit
				definition.Fixed.TCP.Close.FinWaitTimeout = &endpointLimit
				definition.Fixed.TCP.RTO.InitialTimeout = &endpointLimit
				definition.Fixed.TCP.RTO.MinimumTimeout = &endpointLimit
				definition.Fixed.TCP.RTO.MaximumTimeout = &endpointLimit
				mappingIdle := 6 * time.Second
				editInitialState(&definition, func(state *scenario.StateConfig) {
					state.Resources.LoadBalancer.MappingTimeouts.ClosingIdleTimeout = &mappingIdle

					state.Resources.LoadBalancer.MappingTimeouts.ResetIdleTimeout = &mappingIdle
				})
				world := newTimelineWorld(t, definition)
				runIngressUntil(t, world, time.Second)
				client := world.clients[0].Client
				backend := world.lb.GetServers()[0]
				// Shape is supported by the LB; the endpoint rejects this distant sequence.
				world.lb.HandleClientPacket(model.Packet{Key: client.Key(), Flag: flag, Seq: ^uint64(0), Ack: 1})
				runIngressUntil(t, world, 5*time.Second)
				if lifecycleRouteCount(world) != 1 {
					t.Fatal("endpoint timers shortened LB retention")
				}
				runIngressUntil(t, world, 10*time.Second)
				if lifecycleRouteCount(world) != 0 || client.IsTerminated() ||
					backend.GetConnectionState(client.Key()).State != model.TCPEstablished {
					t.Fatal("endpoint timers changed the LB's explicit cleanup deadline")
				}
			}
		})
	}
}

// A six-second FIN retry succeeds only if the explicitly configured LB policy retains it.
func TestBalancerClosingTimeoutControlsFINRetryRoute(t *testing.T) {
	for _, test := range []struct {
		name     string
		timeout  time.Duration
		recovers bool
	}{
		{"long closing timeout", 8 * time.Second, true},
		{"short closing timeout", time.Second, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition := lifecycleScenario()
			definition.Fixed.Execution.Duration = 8 * time.Second
			editInitialState(&definition, func(state *scenario.StateConfig) {
				state.Resources.Backends = baseScenario().States["0"].Resources.Backends

				state.Resources.LoadBalancer.MappingTimeouts.ClosingIdleTimeout = &test.timeout
			})
			definition.Fixed.Workload.Defaults.Bytes = 2050
			definition.Fixed.Workload.Defaults.PacketGap = 0
			finWait := time.Second
			definition.Fixed.TCP.Close.FinWaitTimeout = &finWait

			// Keep RTO at 6 s after the handshake and data RTT measurements.
			retryDelay := 6 * time.Second
			definition.Fixed.TCP.RTO.InitialTimeout = &retryDelay
			definition.Fixed.TCP.RTO.MinimumTimeout = &retryDelay
			definition.Fixed.TCP.RTO.MaximumTimeout = &retryDelay
			world := newTimelineWorld(t, definition)
			backend := world.lb.GetServers()[0]

			// Lose the first FIN after it crosses the LB and before reaching the backend.
			world.ScheduleAt(9500*time.Microsecond, func() { backend.Active = false })
			world.ScheduleAt(20*time.Millisecond, func() { backend.Active = true })
			runIngressUntil(t, world, 20*time.Millisecond)
			client := world.clients[0].Client
			if client.GetConnectionState().State != model.TCPFinWait1 ||
				backend.GetConnectionState(client.Key()).State != model.TCPEstablished {
				t.Fatal("the fixture did not lose the first FIN after it crossed the LB")
			}

			runIngressUntil(t, world, 5*time.Second)
			if got := lifecycleRouteCount(world) == 1; got != test.recovers {
				t.Fatalf("route retained at sweep = %v, want %v", got, test.recovers)
			}
			if client.IsTerminated() || backend.GetConnectionState(client.Key()).State != model.TCPEstablished {
				t.Fatal("LB expiry stopped an endpoint before its FIN retry")
			}

			runIngressUntil(t, world, 7*time.Second)
			state := client.GetConnectionState()
			if state.FINRetransmissions != 1 || world.lb.GetClient(client.Key()) != client ||
				world.lb.GetServers()[1].NumberOfHandledPackets != 0 {
				t.Fatal("the FIN retry lost endpoint registration or was rerouted to another backend")
			}
			if test.recovers {
				if state.State != model.TCPTimeWait || backend.GetConnectionState(client.Key()).State != model.TCPClosed {
					t.Fatalf("the retained route did not carry the FIN retry: %+v", state)
				}
			} else if state.State != model.TCPFinWait1 || client.IsTerminated() ||
				backend.GetConnectionState(client.Key()).State != model.TCPEstablished || lifecycleRouteCount(world) != 0 {
				t.Fatal("a FIN without a mapping changed an endpoint or recreated its route")
			}
		})
	}
}

// Stop is the deliberate exception: it stops every endpoint and all queued work.
func TestWorldStopCancelsEndpointTimersAndNetworkWork(t *testing.T) {
	definition := lifecycleScenario()
	editInitialState(&definition, func(state *scenario.StateConfig) { state.Workload.ClientGroups[0].Count = 3 })
	definition.Fixed.Network.Defaults.BandwidthBitsPerSecond = 8000
	definition.Fixed.Network.Defaults.PacketTravelTime = time.Second

	world := newTimelineWorld(t, definition)
	runIngressUntil(t, world, 0)
	if world.PendingEvents() == 0 {
		t.Fatal("the fixture did not create any scheduled work")
	}
	clients := append([]runtimeClient(nil), world.clients...)
	backends := world.lb.GetServers()
	links := append([]namedLink(nil), world.links...)
	ingress := world.ingress
	egress := world.egress

	world.Stop()
	if world.PendingEvents() != 0 || lifecycleRouteCount(world) != 0 {
		t.Fatal("Stop left scheduled work or LB routes behind")
	}
	for _, runtime := range clients {
		if !runtime.Client.IsTerminated() {
			t.Fatal("Stop left an active client")
		}
	}
	for _, backend := range backends {
		if !backend.Killed || backend.ActiveConnections != 0 || backend.HalfOpenConnections != 0 {
			t.Fatal("Stop left an active backend or its connection state")
		}
	}

	// Queues and in-flight counters must agree with the stopped engine.
	for _, link := range links {
		stats := link.Stats()
		if stats.QueuedPackets != 0 || stats.Transmitting || stats.TravellingPackets != 0 {
			t.Fatalf("Stop retained unfinished link work: %+v", stats)
		}
	}
	input := ingress.Stats()
	if input.QueuedPackets != 0 || input.Transmitting || input.TravellingPackets != 0 ||
		egress.Stats().QueuedPackets != 0 {
		t.Fatal("Stop retained shared ingress or output work")
	}
}

func lifecycleRouteCount(world *World) int {
	count := 0
	world.VisitRoutes(func(RouteObservation) { count++ })
	return count
}
