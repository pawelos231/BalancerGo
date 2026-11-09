package simulation

import (
	"testing"
	"time"

	"load-balancer/model"
	"load-balancer/scenario"
)

// Failure and recovery are learned by probes; drain is independent LB policy.
func TestTimelineAvailabilityDoesNotRevealPhysicalHealthToSelection(t *testing.T) {
	definition := timelineDefinition()
	definition.Fixed.Execution.Duration = 3 * time.Second
	initial := definition.States["0"]
	initial.Resources.Backends = initial.Resources.Backends[:1]
	definition.States["0"] = initial
	failed := initial
	failed.Resources.Backends = append([]scenario.BackendStateConfig(nil), initial.Resources.Backends...)
	failed.Resources.Backends[0].PhysicalAvailable = scenarioRuntimeValue(false)
	definition.States["100000000"] = failed
	recovered := initial
	recovered.Resources.Backends = append([]scenario.BackendStateConfig(nil), initial.Resources.Backends...)
	recovered.Resources.Backends[0].AdministrativeState = scenarioRuntimeValue(scenario.BackendAdminDrain)
	definition.States["1600000000"] = recovered
	definition.States["2100000000"] = initial
	world := newTimelineWorld(t, definition)
	backend := world.lb.GetServers()[0]
	advanceTimelinePolicy(t, world, 100*time.Millisecond)
	beforeLearning := timelinePolicyClient(t, world, 0)
	if selected, err := world.lb.Route(beforeLearning); err != nil || selected != backend {
		t.Fatal("physical failure leaked into new selection before a probe timeout")
	}
	world.lb.HandleClientPacket(model.Packet{Key: beforeLearning.Key(), Flag: model.FlagSYN})
	advanceTimelinePolicy(t, world, 200*time.Millisecond)
	if backend.HostStats().OfferedPackets != 0 || backend.NumberOfHandledPackets != 0 {
		t.Fatal("a physically unavailable backend accepted local CPU or TCP work")
	}
	advanceTimelinePolicy(t, world, 1500*time.Millisecond)
	if _, err := world.lb.Route(timelinePolicyClient(t, world, 1)); err == nil {
		t.Fatal("the failed health probe did not exclude new mappings")
	}
	advanceTimelinePolicy(t, world, 1600*time.Millisecond)
	state := readWorld(world).Backends[0].State
	if !state.Active || !state.Draining || state.HealthTCP {
		t.Fatalf("recovery changed drain or instantly marked LB health: %+v", state)
	}
	advanceTimelinePolicy(t, world, 2*time.Second)
	state = readWorld(world).Backends[0].State
	if !state.HealthTCP || !state.Draining {
		t.Fatal("a reachable draining backend did not answer its next probe")
	}
	if _, err := world.lb.Route(timelinePolicyClient(t, world, 2)); err == nil {
		t.Fatal("healthy probe results bypassed administrative drain")
	}
	advanceTimelinePolicy(t, world, 2100*time.Millisecond)
	if selected, err := world.lb.Route(timelinePolicyClient(t, world, 3)); err != nil || selected != backend {
		t.Fatal("omitted full-state availability/admission did not restore true/active defaults")
	}
	if selected, err := world.lb.Route(beforeLearning); err != nil || selected != backend {
		t.Fatal("failure, recovery or drain moved an old mapping")
	}
}

// A first SYN already assigned by the LB still reaches TCP after drain starts.
func TestTimelineDrainPreservesFirstSYNRetryAndEstablishedTraffic(t *testing.T) {
	definition := timelineDefinition()
	next := timelineDefinition().States["0"]
	next.Resources.Backends[0].AdministrativeState = scenarioRuntimeValue(scenario.BackendAdminDrain)
	definition.States["500000"] = next
	world := newTimelineWorld(t, definition)
	endpoint := timelinePolicyClient(t, world, 0)
	backend, err := world.lb.Route(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	syn := model.Packet{Key: endpoint.Key(), Flag: model.FlagSYN}
	world.lb.HandleClientPacket(syn)
	advanceTimelinePolicy(t, world, 2*time.Millisecond)
	if !backend.Draining || backend.HalfOpenConnections != 1 {
		t.Fatal("drain blocked the first SYN on an already assigned mapping")
	}
	world.lb.HandleClientPacket(syn)
	advanceTimelinePolicy(t, world, 4*time.Millisecond)
	if backend.NumberOfHandledPackets != 2 || backend.HalfOpenConnections != 1 {
		t.Fatal("drain blocked a retransmitted SYN or reserved another TCP slot")
	}
	world.lb.HandleClientPacket(model.Packet{Key: endpoint.Key(), Flag: model.FlagACK, Seq: 1, Ack: 1})
	world.lb.HandleClientPacket(model.Packet{
		Key: endpoint.Key(), Flag: model.FlagACK | model.FlagPSH, Seq: 1, Ack: 1, Size: 7,
	})
	advanceTimelinePolicy(t, world, 8*time.Millisecond)
	if backend.ActiveConnections != 1 || backend.GetState().ReceivedPayloadBytes != 7 {
		t.Fatal("drain interrupted an existing handshake or payload flow")
	}
	if selected, err := world.lb.Route(timelinePolicyClient(t, world, 1)); err != nil || selected == backend {
		t.Fatal("drain did not exclude the backend from new selection")
	}
}

// Initial and inserted settings apply before ordinary arrivals at that timestamp.
func TestTimelineInitialAndInsertedUnavailableDrainingBackends(t *testing.T) {
	definition := timelineDefinition()
	initial := definition.States["0"]
	initial.Resources.Backends[0].PhysicalAvailable = scenarioRuntimeValue(false)
	initial.Resources.Backends[1].AdministrativeState = scenarioRuntimeValue(scenario.BackendAdminDrain)
	initial.Workload.ClientGroups[0].Count = 1
	definition.States["0"] = initial
	next := initial
	next.Resources.Backends = append([]scenario.BackendStateConfig(nil), initial.Resources.Backends...)
	next.Resources.Backends = append(next.Resources.Backends, scenario.BackendStateConfig{
		ID: "later", Address: "10.0.0.3", Port: 80,
		PhysicalAvailable:   scenarioRuntimeValue(false),
		AdministrativeState: scenarioRuntimeValue(scenario.BackendAdminDrain),
		Host:                timelineCPUConfig(2, time.Microsecond, 3),
	})
	definition.States["10000000"] = next
	world := newTimelineWorld(t, definition)
	advanceTimelinePolicy(t, world, 9*time.Millisecond)
	first := world.lb.GetServers()[0]
	if first.Active || first.HostStats().OfferedPackets != 0 || len(world.lb.GetConnectionTable()[first]) != 1 {
		t.Fatal("initial physical unavailability was applied after the first arrival")
	}
	advanceTimelinePolicy(t, world, 15*time.Millisecond)
	later := world.backends["later"].server
	state := readWorld(world).Backends[2].State
	if later.Active || !state.Draining || !state.HealthTCP || later.HostStats().ObservedTime != 5*time.Millisecond ||
		later.HostStats().Cores != 2 || later.HostStats().Stopped {
		t.Fatal("inserted backend lost its status, insertion-time CPU lifetime or recoverability")
	}
	if _, err := world.lb.Route(timelinePolicyClient(t, world, 1)); err != nil {
		t.Fatal("unknown physical health was prematurely excluded from new selection")
	}
}

// A recoverable crash clears only local state; an emitted reply remains on the wire.
func TestTimelineCrashKeepsTravellingReplyClientAndMapping(t *testing.T) {
	definition := timelineDefinition()
	initial := definition.States["0"]
	initial.Resources.Backends = initial.Resources.Backends[:1]
	initial.Resources.Backends[0].Host = timelineCPUConfig(1, 100*time.Microsecond, 3)
	initial.Workload.ClientGroups[0].Count = 1
	definition.Fixed.Workload.Defaults.PacketGap = time.Second
	definition.States["0"] = initial
	failed := initial
	failed.Resources.Backends = append([]scenario.BackendStateConfig(nil), initial.Resources.Backends...)
	failed.Resources.Backends[0].PhysicalAvailable = scenarioRuntimeValue(false)
	definition.States["2500000"] = failed
	definition.States["20000000"] = initial
	world := newTimelineWorld(t, definition)
	advanceTimelinePolicy(t, world, 2400*time.Microsecond)
	backend := world.lb.GetServers()[0]
	endpoint := world.clients[0].Client
	if backend.HalfOpenConnections != 1 || world.backends[backend.ID].returnLink.Stats().TravellingPackets != 1 {
		t.Fatal("fixture did not leave an emitted SYN+ACK travelling")
	}
	for range 3 {
		backend.HandlePacket(model.Packet{Key: endpoint.Key(), Flag: model.FlagACK, Seq: 1, Ack: 1})
	}
	before := backend.HostStats()
	advanceTimelinePolicy(t, world, 10*time.Millisecond)
	after := backend.HostStats()
	if backend.Active || backend.Killed || backend.ActiveConnections != 0 || backend.HalfOpenConnections != 0 ||
		after.BusyCores != 0 || after.QueuedPackets != 0 || after.DiscardedPackets != 3 {
		t.Fatal("crash retained local work or permanently stopped the endpoint")
	}
	if !endpoint.IsHandshakeCompleted() || endpoint.IsTerminated() || !world.lb.HasClient(endpoint) ||
		len(world.lb.GetConnectionTable()[backend]) != 1 {
		t.Fatal("crash erased an emitted reply or changed the remote endpoint/mapping")
	}
	advanceTimelinePolicy(t, world, 20*time.Millisecond)
	if !backend.Active || backend.HostStats().Stopped || backend.HostStats().Cores != before.Cores ||
		backend.HostStats().ProcessedPackets != after.ProcessedPackets || backend.HostStats().DiscardedPackets != 3 {
		t.Fatal("recovery reset CPU settings or historical packet counters")
	}
	backend.HandlePacket(model.Packet{Key: endpoint.Key(), Flag: model.FlagSYN})
	advanceTimelinePolicy(t, world, 21*time.Millisecond)
	if backend.HalfOpenConnections != 1 || backend.NumberOfHandledPackets != 2 {
		t.Fatal("the recovered backend could not process a new local handshake")
	}
}
