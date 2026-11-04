package balancer

import (
	"math/rand"
	"testing"
	"time"

	"load-balancer/algorithms"
	"load-balancer/client"
	"load-balancer/engine"
	"load-balancer/model"
	"load-balancer/server"
)

func TestMappingObservesPacketShapesAndDirection(t *testing.T) {
	for _, test := range []struct {
		name       string
		start      connectionPhase
		fromServer bool
		packet     model.Packet
		want       connectionPhase
	}{
		{"client SYN", stateHalfOpen, false, model.Packet{Flag: model.FlagSYN}, stateHalfOpen},
		{"backend SYN ACK", stateHalfOpen, true, model.Packet{Flag: model.FlagSYN | model.FlagACK}, stateHalfOpen},
		{"backend ACK cannot complete handshake", stateHalfOpen, true, model.Packet{Flag: model.FlagACK}, stateHalfOpen},
		{"client ACK completes handshake", stateHalfOpen, false, model.Packet{Flag: model.FlagACK}, stateEstablished},
		{"first client data ACK completes handshake", stateHalfOpen, false, model.Packet{Flag: model.FlagACK | model.FlagPSH, Size: 100}, stateEstablished},
		{"client data without PSH", stateEstablished, false, model.Packet{Flag: model.FlagACK, Size: 100}, stateEstablished},
		{"backend data", stateEstablished, true, model.Packet{Flag: model.FlagACK | model.FlagPSH, Size: 100}, stateEstablished},
		{"empty PSH ACK", stateEstablished, false, model.Packet{Flag: model.FlagACK | model.FlagPSH}, stateEstablished},
		{"client FIN before handshake ACK", stateHalfOpen, false, model.Packet{Flag: model.FlagFIN | model.FlagACK}, stateClosing},
		{"backend FIN", stateEstablished, true, model.Packet{Flag: model.FlagFIN | model.FlagACK}, stateClosing},
		{"client reset", stateEstablished, false, model.Packet{Flag: model.FlagRST}, stateReset},
		{"backend reset ACK", stateEstablished, true, model.Packet{Flag: model.FlagRST | model.FlagACK}, stateReset},
		{"closing ignores new SYN", stateClosing, false, model.Packet{Flag: model.FlagSYN}, stateClosing},
		{"closing ignores backend SYN ACK", stateClosing, true, model.Packet{Flag: model.FlagSYN | model.FlagACK}, stateClosing},
		{"closing ignores ACK", stateClosing, false, model.Packet{Flag: model.FlagACK}, stateClosing},
		{"reset overrides closing", stateClosing, true, model.Packet{Flag: model.FlagRST}, stateReset},
		{"reset ignores FIN", stateReset, false, model.Packet{Flag: model.FlagFIN | model.FlagACK}, stateReset},
		{"reset ignores new SYN", stateReset, false, model.Packet{Flag: model.FlagSYN}, stateReset},
		{"reset ignores ACK", stateReset, true, model.Packet{Flag: model.FlagACK}, stateReset},
		// The LB classifies shape, including controls whose sequence numbers an endpoint rejects.
		{"reset does not validate sequence", stateEstablished, false, model.Packet{Flag: model.FlagRST | model.FlagACK, Seq: ^uint64(0)}, stateReset},
		{"ACK does not validate sequence range", stateHalfOpen, false, model.Packet{Flag: model.FlagACK, Seq: ^uint64(0), Size: 1}, stateEstablished},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := connectionState{phase: test.start, lastActivity: time.Second}
			test.packet.Key.Protocol = model.TCP
			state.observe(test.packet, test.fromServer, 2*time.Second)
			if state.phase != test.want || state.lastActivity != 2*time.Second {
				t.Fatalf("observed state = %+v, want phase %v and refreshed activity", state, test.want)
			}
		})
	}
}

func TestMappingIgnoresUnsupportedPacketShapes(t *testing.T) {
	for _, test := range []struct {
		name       string
		fromServer bool
		packet     model.Packet
	}{
		{"backend pure SYN", true, model.Packet{Flag: model.FlagSYN}},
		{"client SYN ACK", false, model.Packet{Flag: model.FlagSYN | model.FlagACK}},
		{"SYN with payload", false, model.Packet{Flag: model.FlagSYN, Size: 1}},
		{"SYN ACK with payload", true, model.Packet{Flag: model.FlagSYN | model.FlagACK, Size: 1}},
		{"FIN with payload", false, model.Packet{Flag: model.FlagFIN | model.FlagACK, Size: 1}},
		{"RST with payload", true, model.Packet{Flag: model.FlagRST | model.FlagACK, Size: 1}},
		{"SYN FIN", false, model.Packet{Flag: model.FlagSYN | model.FlagFIN}},
		{"FIN RST", true, model.Packet{Flag: model.FlagFIN | model.FlagRST | model.FlagACK}},
		{"SYN RST", false, model.Packet{Flag: model.FlagSYN | model.FlagRST}},
		{"SYN PSH", false, model.Packet{Flag: model.FlagSYN | model.FlagPSH}},
		{"FIN PSH", true, model.Packet{Flag: model.FlagFIN | model.FlagACK | model.FlagPSH}},
		{"RST PSH", false, model.Packet{Flag: model.FlagRST | model.FlagACK | model.FlagPSH}},
		{"bare FIN", true, model.Packet{Flag: model.FlagFIN}},
		{"bare payload", false, model.Packet{Size: 1}},
		{"bare PSH", true, model.Packet{Flag: model.FlagPSH, Size: 1}},
		{"negative payload", false, model.Packet{Flag: model.FlagACK, Size: -1}},
		{"unknown flag", true, model.Packet{Flag: model.FlagACK | model.Flag(1<<7)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.packet.Key.Protocol = model.TCP
			for _, phase := range []connectionPhase{stateHalfOpen, stateEstablished, stateClosing, stateReset} {
				state := connectionState{phase: phase, lastActivity: time.Second}
				before := state
				state.observe(test.packet, test.fromServer, 2*time.Second)
				if state != before {
					t.Fatalf("unsupported shape changed state from %+v to %+v", before, state)
				}
			}
		})
	}
}

func TestMappingTimeoutsAreIndependent(t *testing.T) {
	custom := TimeoutConfig{
		HalfOpen: 7 * time.Second, Established: 11 * time.Second,
		Closing: 16 * time.Second, Reset: 21 * time.Second,
	}
	for _, phase := range []struct {
		name         string
		packet       model.Packet
		defaultSweep time.Duration
		customSweep  time.Duration
		disabled     TimeoutConfig
	}{
		{"half open", model.Packet{Flag: model.FlagSYN}, 5 * time.Second, 10 * time.Second,
			TimeoutConfig{Established: time.Second, Closing: time.Second, Reset: time.Second}},
		{"established", model.Packet{Flag: model.FlagACK}, 50 * time.Second, 15 * time.Second,
			TimeoutConfig{HalfOpen: time.Second, Closing: time.Second, Reset: time.Second}},
		{"closing", model.Packet{Flag: model.FlagFIN | model.FlagACK}, 120 * time.Second, 20 * time.Second,
			TimeoutConfig{HalfOpen: time.Second, Established: time.Second, Reset: time.Second}},
		{"reset", model.Packet{Flag: model.FlagRST}, 10 * time.Second, 25 * time.Second,
			TimeoutConfig{HalfOpen: time.Second, Established: time.Second, Closing: time.Second}},
	} {
		t.Run(phase.name, func(t *testing.T) {
			for _, policy := range []struct {
				name    string
				config  *TimeoutConfig
				sweep   time.Duration
				expires bool
			}{
				{"default", nil, phase.defaultSweep, true},
				{"custom", &custom, phase.customSweep, true},
				{"disabled", &phase.disabled, 180 * time.Second, false},
			} {
				t.Run(policy.name, func(t *testing.T) {
					fixture := newMappingFixture(t, policy.config)
					fixture.sendClient(model.Packet{Flag: model.FlagSYN})
					fixture.sendClient(phase.packet)
					fixture.runUntil(t, policy.sweep-time.Nanosecond)
					if fixture.lb.byKey[fixture.endpoint.Key()] != fixture.backend {
						t.Fatal("route expired before its cleanup sweep")
					}
					fixture.runUntil(t, policy.sweep)
					if got := fixture.lb.byKey[fixture.endpoint.Key()] != nil; got == policy.expires {
						t.Fatalf("route present = %v, want expiry = %v", got, policy.expires)
					}
					if fixture.lb.GetClient(fixture.endpoint.Key()) != fixture.endpoint ||
						fixture.endpoint.IsTerminated() || fixture.backend.Killed {
						t.Fatal("mapping cleanup removed a registration or stopped an endpoint")
					}
				})
			}
		})
	}
}

// Traffic may refresh a mapping after its nominal deadline and before the next sweep.
// The observation belongs to LB input, even when chaos drops or delays its output.
func TestMappingRefreshesBothDirectionsBeforeChaosAndCleanup(t *testing.T) {
	for _, direction := range []string{"client", "backend"} {
		t.Run(direction, func(t *testing.T) {
			for _, chaos := range []struct {
				name   string
				config model.ChaosConfig
			}{
				{"dropped", model.ChaosConfig{PacketDropRate: 1}},
				{"delayed", model.ChaosConfig{Latency: 2 * time.Second}},
			} {
				t.Run(chaos.name, func(t *testing.T) {
					fixture := newMappingFixture(t, &TimeoutConfig{Established: 3 * time.Second})
					fixture.sendClient(model.Packet{Flag: model.FlagSYN})
					fixture.sendClient(model.Packet{Flag: model.FlagACK})
					fixture.runUntil(t, 4*time.Second)
					fixture.lb.SetChaosConfig(&chaos.config)
					packet := model.Packet{Key: fixture.endpoint.Key(), Flag: model.FlagACK}
					before := len(fixture.toBackend) + len(fixture.toClient)
					if direction == "client" {
						fixture.lb.HandleClientPacket(packet)
					} else {
						fixture.lb.HandleServerPacket(fixture.backend, packet)
					}
					if state := fixture.lb.connections[packet.Key]; state == nil || state.lastActivity != 4*time.Second {
						t.Fatalf("input did not refresh activity: %+v", state)
					}
					fixture.runUntil(t, 5*time.Second)
					if fixture.lb.byKey[packet.Key] != fixture.backend {
						t.Fatal("sweep ignored the packet that arrived after the nominal deadline")
					}
					if len(fixture.toBackend)+len(fixture.toClient) != before {
						t.Fatal("chaos did not withhold the output")
					}
					fixture.runUntil(t, 10*time.Second)
					if fixture.lb.byKey[packet.Key] != nil {
						t.Fatal("an idle mapping survived its later cleanup sweep")
					}
				})
			}
		})
	}
}

func TestMappingCleanupAndEqualTimePacketFollowEventOrder(t *testing.T) {
	for _, packetFirst := range []bool{true, false} {
		name := "cleanup first"
		if packetFirst {
			name = "packet first"
		}
		t.Run(name, func(t *testing.T) {
			fixture := newMappingFixture(t, &TimeoutConfig{Established: 10 * time.Second})
			fixture.sendClient(model.Packet{Flag: model.FlagSYN})
			fixture.sendClient(model.Packet{Flag: model.FlagACK})
			schedulePacket := func() {
				fixture.engine.ScheduleAt(10*time.Second, func() { fixture.sendClient(model.Packet{Flag: model.FlagACK}) })
			}
			// The sweep at 5 s schedules the sweep at 10 s with its next EventID.
			if packetFirst {
				schedulePacket()
			}
			fixture.runUntil(t, 5*time.Second)
			if !packetFirst {
				schedulePacket()
			}
			fixture.runUntil(t, 10*time.Second)
			if got := fixture.lb.byKey[fixture.endpoint.Key()] != nil; got != packetFirst {
				t.Fatalf("route present = %v, packet first = %v", got, packetFirst)
			}
		})
	}
}

func TestMappingAllocationRequiresPureClientSYN(t *testing.T) {
	for _, packet := range []model.Packet{
		{Flag: model.FlagSYN, Size: 1},
		{Flag: model.FlagSYN | model.FlagPSH},
		{Flag: model.FlagSYN | model.FlagFIN},
		{Flag: model.FlagSYN | model.FlagRST},
		{Flag: model.FlagSYN | model.FlagACK},
		{Flag: model.FlagACK},
		{Flag: model.FlagACK | model.FlagPSH, Size: 10},
		{Flag: model.FlagFIN | model.FlagACK},
		{Flag: model.FlagRST},
	} {
		fixture := newMappingFixture(t, nil)
		fixture.sendClient(packet)
		if len(fixture.lb.connections) != 0 || len(fixture.toBackend) != 0 {
			t.Fatalf("packet %+v allocated or forwarded without a route", packet)
		}
	}
}

func TestUnmatchedPacketsDoNotRefreshMapping(t *testing.T) {
	fixture := newMappingFixture(t, nil)
	fixture.sendClient(model.Packet{Flag: model.FlagSYN})
	fixture.sendClient(model.Packet{Flag: model.FlagACK})
	key := fixture.endpoint.Key()
	before := *fixture.lb.connections[key]
	fixture.runUntil(t, 2*time.Second)
	other := server.NewServer(fixture.engine, "other", "Other", "10.0.0.2", 80, 0, 1, 100)
	fixture.lb.HandleServerPacket(other, model.Packet{Key: key, Flag: model.FlagRST})
	unknown := client.NewClient(fixture.engine, "192.0.0.2", 40001, "lb.local", 80, model.TCP).Key()
	fixture.lb.HandleClientPacket(model.Packet{Key: unknown, Flag: model.FlagSYN})
	fixture.lb.HandleServerPacket(fixture.backend, model.Packet{Key: unknown, Flag: model.FlagRST})
	if got := *fixture.lb.connections[key]; got != before || len(fixture.lb.connections) != 1 {
		t.Fatalf("unmatched traffic changed the mapping: %+v", got)
	}
	if len(fixture.toBackend) != 2 || len(fixture.toClient) != 0 {
		t.Fatal("unmatched traffic was forwarded")
	}

	// Unsupported traffic on an existing route still reaches the endpoint for validation.
	fixture.sendClient(model.Packet{Flag: model.FlagRST | model.FlagACK, Size: 1})
	if got := *fixture.lb.connections[key]; got != before || len(fixture.toBackend) != 3 {
		t.Fatal("an unsupported shape changed LB state or bypassed endpoint forwarding")
	}
}

func TestSameKeySYNKeepsTerminalMappingPinned(t *testing.T) {
	for _, terminal := range []model.Flag{model.FlagFIN | model.FlagACK, model.FlagRST} {
		fixture := newMappingFixture(t, nil)
		other := server.NewServer(fixture.engine, "other", "Other", "10.0.0.2", 80, 0, 1, 100)
		otherPackets := 0
		if err := fixture.lb.AddServer(other, func(model.Packet) { otherPackets++ }); err != nil {
			t.Fatal(err)
		}
		fixture.sendClient(model.Packet{Flag: model.FlagSYN})
		fixture.sendClient(model.Packet{Flag: terminal})
		state := fixture.lb.connections[fixture.endpoint.Key()]
		phase := state.phase
		fixture.runUntil(t, time.Second)
		fixture.sendClient(model.Packet{Flag: model.FlagSYN})
		if fixture.lb.byKey[fixture.endpoint.Key()] != fixture.backend ||
			fixture.lb.connections[fixture.endpoint.Key()] != state || state.phase != phase ||
			state.lastActivity != time.Second || otherPackets != 0 || len(fixture.toBackend) != 3 {
			t.Fatal("same-key SYN replaced a terminal mapping or changed its pinned backend")
		}
	}
}

type mappingFixture struct {
	engine    *engine.Engine
	lb        *LoadBalancer
	endpoint  *client.Client
	backend   *server.Server
	toBackend []model.Packet
	toClient  []model.Packet
}

func newMappingFixture(t *testing.T, config *TimeoutConfig) *mappingFixture {
	t.Helper()
	fixture := &mappingFixture{engine: engine.New(200 * time.Second)}
	fixture.lb = NewLoadBalancer(fixture.engine, model.LBModeNAT, &algorithms.RoundRobin{},
		rand.New(rand.NewSource(1)))
	t.Cleanup(fixture.lb.Shutdown)
	if config != nil {
		fixture.lb.SetMappingTimeouts(*config)
	}
	fixture.backend = server.NewServer(fixture.engine, "one", "Backend", "10.0.0.1", 80, 0, 1, 100)
	if err := fixture.lb.AddServer(fixture.backend, func(packet model.Packet) {
		fixture.toBackend = append(fixture.toBackend, packet)
	}); err != nil {
		t.Fatal(err)
	}
	fixture.endpoint = client.NewClient(fixture.engine, "192.0.0.1", 40000, "lb.local", 80, model.TCP)
	if err := fixture.lb.AddClient(fixture.endpoint, func(packet model.Packet) {
		fixture.toClient = append(fixture.toClient, packet)
	}); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (fixture *mappingFixture) sendClient(packet model.Packet) {
	packet.Key = fixture.endpoint.Key()
	fixture.lb.HandleClientPacket(packet)
}

func (fixture *mappingFixture) runUntil(t *testing.T, until time.Duration) {
	t.Helper()
	if err := fixture.engine.RunUntil(until); err != nil {
		t.Fatal(err)
	}
}
