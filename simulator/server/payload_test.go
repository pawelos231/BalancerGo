package server_test

import (
	"slices"
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
	"load-balancer/server"
)

func TestReceivedPayloadCountsUniqueSegmentsAcrossFlows(t *testing.T) {
	e := engine.New(time.Second)
	s := server.NewServer(e, "srv", "backend", "10.0.0.2", 80, 1, 1, 10)
	flowA := model.ClientKey{SrcIP: "10.0.0.1", SrcPort: 1000, DstIP: "10.0.0.2", DstPort: 80, Protocol: model.TCP}
	flowB := flowA
	flowB.SrcPort++
	var replies []model.Packet
	s.SetSender(func(pkt model.Packet) { replies = append(replies, pkt) })
	var increments []int
	var observedTotals []uint64
	s.OnPayloadReceived = func(bytes int) {
		increments = append(increments, bytes)
		observedTotals = append(observedTotals, s.GetState().ReceivedPayloadBytes)
	}
	assertTotal := func(want uint64) {
		t.Helper()
		if got := s.GetState().ReceivedPayloadBytes; got != want {
			t.Fatalf("received payload = %d, want %d", got, want)
		}
	}
	data := func(key model.ClientKey, index uint32, size int) {
		s.HandlePacket(model.Packet{Key: key, Flag: model.FlagPSH | model.FlagACK, Size: size, PlaceInStream: index})
	}

	for _, flow := range []model.ClientKey{flowA, flowB} {
		s.HandlePacket(model.Packet{Key: flow, Flag: model.FlagSYN})
		s.HandlePacket(model.Packet{Key: flow, Flag: model.FlagACK, PlaceInStream: 1})
	}
	assertTotal(0)
	if s.GetState().ActiveConnections != 2 {
		t.Fatal("fixture did not establish two independent flows")
	}
	unknown := flowA
	unknown.SrcPort += 2
	data(unknown, 0, 51)
	assertTotal(0)

	data(flowA, 0, model.SEGMENT_SIZE)
	assertTotal(model.SEGMENT_SIZE)
	data(flowA, 0, model.SEGMENT_SIZE)
	data(flowA, 0, 2*model.SEGMENT_SIZE)
	assertTotal(model.SEGMENT_SIZE)
	if got := replies[len(replies)-1]; got.Flag != model.FlagACK || got.PlaceInStream != 1 {
		t.Fatalf("duplicate data was not acknowledged at the existing position: %+v", got)
	}
	repliesBeforeFutureData := len(replies)
	data(flowA, 2, 99)
	assertTotal(model.SEGMENT_SIZE)
	if len(replies) != repliesBeforeFutureData {
		t.Fatal("future data unexpectedly received an ACK")
	}

	// Both flows begin at segment zero and contribute to the backend total.
	data(flowB, 0, 13)
	assertTotal(model.SEGMENT_SIZE + 13)
	data(flowA, 1, 37)
	const wantTotal = model.SEGMENT_SIZE + 13 + 37
	assertTotal(wantTotal)
	data(flowA, 0, model.SEGMENT_SIZE)
	data(flowB, 0, 13)
	assertTotal(wantTotal)
	if !slices.Equal(increments, []int{model.SEGMENT_SIZE, 13, 37}) {
		t.Fatalf("payload callbacks = %v", increments)
	}
	if !slices.Equal(observedTotals, []uint64{model.SEGMENT_SIZE, model.SEGMENT_SIZE + 13, wantTotal}) {
		t.Fatalf("payload totals visible in callbacks = %v", observedTotals)
	}

	s.HandlePacket(model.Packet{Key: flowA, Flag: model.FlagFIN, PlaceInStream: 2})
	assertTotal(wantTotal)
	s.ForgetConnection(flowB)
	s.ForgetConnection(flowB)
	if s.GetState().ActiveConnections != 0 {
		t.Fatal("closed flows remain active")
	}
	data(flowA, 2, 99)
	assertTotal(wantTotal)
	s.Shutdown()
	data(flowB, 1, 99)
	assertTotal(wantTotal)
	fresh := server.NewServer(e, "srv", "backend", "10.0.0.2", 80, 1, 1, 10)
	if got := fresh.GetState().ReceivedPayloadBytes; got != 0 {
		t.Fatalf("fresh server received payload = %d, want 0", got)
	}
}

func TestReceivedPayloadWithLostHandshakeACK(t *testing.T) {
	e := engine.New(time.Second)
	s := server.NewServer(e, "srv", "backend", "10.0.0.2", 80, 1, 1, 10)
	flow := model.ClientKey{SrcIP: "10.0.0.1", SrcPort: 1000, DstIP: "10.0.0.2", DstPort: 80, Protocol: model.TCP}
	s.HandlePacket(model.Packet{Key: flow, Flag: model.FlagSYN})
	first := model.Packet{Key: flow, Flag: model.FlagPSH | model.FlagACK, Size: 17}
	s.HandlePacket(first)
	s.HandlePacket(first)
	state := s.GetState()
	if state.ReceivedPayloadBytes != 17 || state.ActiveConnections != 1 || state.HalfOpenConnections != 0 {
		t.Fatalf("data completing the handshake was not counted exactly once: %+v", state)
	}

	// Failure cleanup removes live connections while preserving received history.
	s.Shutdown()
	state = s.GetState()
	if state.ReceivedPayloadBytes != 17 || state.ActiveConnections != 0 {
		t.Fatalf("shutdown discarded payload history or retained live connections: %+v", state)
	}
}
