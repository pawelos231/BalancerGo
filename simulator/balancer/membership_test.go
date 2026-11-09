package balancer

import (
	"testing"
	"time"

	"load-balancer/host"
	"load-balancer/model"
	"load-balancer/server"
)

func TestRetireServerPreservesMappingsUntilOrdinaryCleanup(t *testing.T) {
	fixture := newMappingFixture(t, &TimeoutConfig{Established: 3 * time.Second})
	fixture.sendClient(model.Packet{Flag: model.FlagSYN})
	fixture.sendClient(model.Packet{Flag: model.FlagACK})
	key := fixture.endpoint.Key()
	connection := fixture.lb.connections[key]
	before := *connection
	fixture.runUntil(t, time.Second)
	fixture.lb.RetireServer(fixture.backend)
	fixture.lb.RetireServer(fixture.backend)

	if !fixture.backend.Killed || len(fixture.lb.GetServers()) != 0 || fixture.lb.health[fixture.backend] != nil {
		t.Fatal("retirement did not stop and unregister the backend")
	}
	if fixture.endpoint.IsTerminated() || !fixture.lb.HasClient(fixture.endpoint) ||
		fixture.lb.byKey[key] != fixture.backend || fixture.lb.connections[key] != connection || *connection != before {
		t.Fatal("retirement changed the remote client or its mapping")
	}
	if clients := fixture.lb.GetConnectionTable()[fixture.backend]; len(clients) != 1 || clients[0] != fixture.endpoint {
		t.Fatal("retirement discarded the backend's mapping tombstone")
	}
	fixture.lb.VisitRoutes(func(string, model.ClientKey) {
		t.Fatal("retired routes are still presented as active")
	})

	other := server.NewServer(fixture.engine, "two", "Other", "10.0.0.2", 80, 0, 1, 100)
	otherPackets := 0
	if err := fixture.lb.AddServer(other, func(model.Packet) { otherPackets++ }); err != nil {
		t.Fatal(err)
	}
	if selected, err := fixture.lb.Route(routingClient(t, fixture.lb, 40001)); err != nil || selected != other {
		t.Fatalf("a new mapping selected %v, err=%v", selected, err)
	}
	if selected, err := fixture.lb.Route(fixture.endpoint); err != nil || selected != fixture.backend {
		t.Fatal("retirement moved an existing mapping to another backend")
	}
	fixture.sendClient(model.Packet{Flag: model.FlagSYN})
	if len(fixture.toBackend) != 3 || otherPackets != 0 || fixture.lb.byKey[key] != fixture.backend {
		t.Fatal("traffic on a retired route lost its original sender or reached another backend")
	}
	fixture.runUntil(t, 5*time.Second-time.Nanosecond)
	if fixture.lb.connections[key] != connection {
		t.Fatal("retired mapping disappeared before the normal cleanup sweep")
	}
	fixture.runUntil(t, 5*time.Second)
	if fixture.lb.byKey[key] != nil || fixture.lb.connections[key] != nil ||
		fixture.lb.connTable[fixture.backend] != nil || fixture.lb.toServer[fixture.backend] != nil {
		t.Fatal("normal cleanup retained expired retirement tombstones")
	}
	if fixture.endpoint.IsTerminated() || !fixture.lb.HasClient(fixture.endpoint) {
		t.Fatal("normal mapping cleanup closed or unregistered the remote client")
	}
}

func TestRetireServerCancelsHealthAndQueuedServerReplies(t *testing.T) {
	fixture := newMappingFixture(t, nil)
	fixture.sendClient(model.Packet{Flag: model.FlagSYN})
	fixture.sendClient(model.Packet{Flag: model.FlagACK})
	key := fixture.endpoint.Key()
	before := *fixture.lb.connections[key]
	fixture.backend.MarkFailed()
	fixture.runUntil(t, time.Second)
	check := fixture.lb.health[fixture.backend]
	fixture.lb.PrepareHostConfig(host.Config{
		Cores: 1, PacketProcessingTime: time.Second, QueueCapacity: 2,
	}).Apply()
	fixture.lb.HandleServerPacket(fixture.backend, model.Packet{Key: key, Flag: model.FlagRST})
	fixture.lb.RetireServer(fixture.backend)
	if fixture.engine.Cancel(check.tick) || fixture.engine.Cancel(check.timeout) {
		t.Fatal("retirement left a health tick or timeout pending")
	}
	fixture.runUntil(t, 3*time.Second)
	fixture.lb.HandleServerPacket(fixture.backend, model.Packet{Key: key, Flag: model.FlagRST})
	if got := *fixture.lb.connections[key]; got != before || len(fixture.toClient) != 0 {
		t.Fatalf("a late backend reply changed mapping or reached the remote client: %+v", got)
	}
	if len(fixture.lb.health) != 0 || len(fixture.lb.GetServers()) != 0 {
		t.Fatal("a late probe restored retired backend membership")
	}
}

func TestRetireServerWithoutMappingsReleasesSender(t *testing.T) {
	lb, backends := routingBackends(t, 0)
	lb.RetireServer(backends[0])
	if len(lb.toServer) != 0 || len(lb.connTable) != 0 {
		t.Fatal("retirement retained a sender with no mappings")
	}
	if _, err := lb.Route(routingClient(t, lb, 40000)); err == nil {
		t.Fatal("a new mapping selected a retired backend")
	}
}
