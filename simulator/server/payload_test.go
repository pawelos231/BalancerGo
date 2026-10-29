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
	flowA := model.ClientKey{
		SrcIP: "10.0.0.1", SrcPort: 1000,
		DstIP: "10.0.0.2", DstPort: 80,
		Protocol: model.TCP,
	}
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
	data := func(key model.ClientKey, sequence uint64, size int) {
		s.HandlePacket(model.Packet{
			Key: key, Flag: model.FlagPSH | model.FlagACK,
			Size: size, Seq: sequence, Ack: 1,
		})
	}

	for _, flow := range []model.ClientKey{flowA, flowB} {
		s.HandlePacket(model.Packet{Key: flow, Flag: model.FlagSYN})
		s.HandlePacket(model.Packet{Key: flow, Flag: model.FlagACK, Seq: 1, Ack: 1})
	}
	assertTotal(0)
	if s.GetState().ActiveConnections != 2 {
		t.Fatal("fixture did not establish two independent flows")
	}
	unknown := flowA
	unknown.SrcPort += 2
	data(unknown, 1, 51)
	assertTotal(0)

	data(flowA, 1, model.SEGMENT_SIZE)
	assertTotal(model.SEGMENT_SIZE)
	data(flowA, 1, model.SEGMENT_SIZE)
	data(flowA, 1, 2*model.SEGMENT_SIZE)
	assertTotal(model.SEGMENT_SIZE)
	if got := replies[len(replies)-1]; got.Flag != model.FlagACK || got.Seq != 1 || got.Ack != 1025 {
		t.Fatalf("duplicate data was not acknowledged at the existing position: %+v", got)
	}
	repliesBeforeFutureData := len(replies)
	data(flowA, 2049, 99)
	assertTotal(model.SEGMENT_SIZE)
	if len(replies) != repliesBeforeFutureData {
		t.Fatal("future data unexpectedly received an ACK")
	}

	// Each flow starts after its SYN and contributes only new payload bytes.
	data(flowB, 1, 13)
	assertTotal(model.SEGMENT_SIZE + 13)
	data(flowA, 1025, 37)
	const wantTotal = model.SEGMENT_SIZE + 13 + 37
	assertTotal(wantTotal)
	data(flowA, 1, model.SEGMENT_SIZE)
	data(flowB, 1, 13)
	assertTotal(wantTotal)
	if !slices.Equal(increments, []int{model.SEGMENT_SIZE, 13, 37}) {
		t.Fatalf("payload callbacks = %v", increments)
	}
	wantObserved := []uint64{model.SEGMENT_SIZE, model.SEGMENT_SIZE + 13, wantTotal}
	if !slices.Equal(observedTotals, wantObserved) {
		t.Fatalf("payload totals visible in callbacks = %v", observedTotals)
	}

	s.HandlePacket(model.Packet{Key: flowA, Flag: model.FlagFIN | model.FlagACK, Seq: 1062, Ack: 1})
	assertTotal(wantTotal)
	s.ForgetConnection(flowB)
	s.ForgetConnection(flowB)
	if s.GetState().ActiveConnections != 0 {
		t.Fatal("closed flows remain active")
	}
	data(flowA, 1062, 99)
	assertTotal(wantTotal)
	s.Shutdown()
	data(flowB, 14, 99)
	assertTotal(wantTotal)
	fresh := server.NewServer(e, "srv", "backend", "10.0.0.2", 80, 1, 1, 10)
	if got := fresh.GetState().ReceivedPayloadBytes; got != 0 {
		t.Fatalf("fresh server received payload = %d, want 0", got)
	}
}

func TestReceivedPayloadWithLostHandshakeACK(t *testing.T) {
	e := engine.New(time.Second)
	s := server.NewServer(e, "srv", "backend", "10.0.0.2", 80, 1, 1, 10)
	flow := model.ClientKey{
		SrcIP: "10.0.0.1", SrcPort: 1000,
		DstIP: "10.0.0.2", DstPort: 80,
		Protocol: model.TCP,
	}
	s.HandlePacket(model.Packet{Key: flow, Flag: model.FlagSYN})
	first := model.Packet{Key: flow, Flag: model.FlagPSH | model.FlagACK, Size: 17, Seq: 1, Ack: 1}
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
