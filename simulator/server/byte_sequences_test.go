package server_test

import (
	"math"
	"slices"
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
	"load-balancer/server"
)

// byteServerHarness observes replies directly, without adding network delays.
type byteServerHarness struct {
	t       *testing.T
	engine  *engine.Engine
	server  *server.Server
	flow    model.ClientKey
	replies []model.Packet
}

func newByteServerHarness(t *testing.T) *byteServerHarness {
	t.Helper()
	e := engine.New(time.Second)
	h := &byteServerHarness{
		t:      t,
		engine: e,
		server: server.NewServer(e, "srv", "backend", "10.0.0.2", 80, 1, 1, 10),
		flow: model.ClientKey{
			SrcIP: "10.0.0.1", SrcPort: 1000,
			DstIP: "10.0.0.2", DstPort: 80,
			Protocol: model.TCP,
		},
	}
	h.server.SetSender(func(packet model.Packet) {
		h.replies = append(h.replies, packet)
	})
	return h
}

func (h *byteServerHarness) receive(flag model.Flag, seq, ack uint64, size int) {
	h.t.Helper()
	h.server.HandlePacket(model.Packet{
		Key: h.flow, Flag: flag, Seq: seq, Ack: ack, Size: size,
	})
}

func (h *byteServerHarness) assertReply(flag model.Flag, seq, ack uint64) {
	h.assertReplyWindow(flag, seq, ack, model.DefaultReceiveBufferBytes)
}

func (h *byteServerHarness) assertReplyWindow(flag model.Flag, seq, ack uint64, window uint16) {
	h.t.Helper()
	if len(h.replies) == 0 {
		h.t.Fatal("server sent no reply")
	}
	got := h.replies[len(h.replies)-1]
	want := model.Packet{
		Key: h.flow, Flag: flag, Seq: seq, Ack: ack,
		ReceiveWindowBytes: window,
	}
	if got != want {
		h.t.Fatalf("last reply = %+v, want %+v", got, want)
	}
}

func TestServerBytePositionsForHandshakePayloadAndFIN(t *testing.T) {
	h := newByteServerHarness(t)
	var increments []int
	h.server.OnPayloadReceived = func(bytes int) {
		increments = append(increments, bytes)
	}

	// Each direction owns its sequence. The client's SYN uses position 1000.
	h.receive(model.FlagSYN, 1000, 0, 0)
	h.assertReply(model.FlagSYN|model.FlagACK, 0, 1001)
	beforeACK := len(h.replies)
	h.receive(model.FlagACK, 1001, 1, 0)
	if len(h.replies) != beforeACK {
		t.Fatal("a pure ACK caused another ACK")
	}

	// Data advances by payload length, including the short final segment.
	segments := []struct {
		seq  uint64
		size int
		ack  uint64
	}{
		{seq: 1001, size: 1024, ack: 2025},
		{seq: 2025, size: 1024, ack: 3049},
		{seq: 3049, size: 2, ack: 3051},
	}
	for _, segment := range segments {
		h.receive(model.FlagACK, segment.seq, 1, segment.size)
		h.assertReply(model.FlagACK, 1, segment.ack)
	}
	if got := h.server.GetState().ReceivedPayloadBytes; got != 2050 {
		t.Fatalf("received payload = %d, want 2050", got)
	}
	if !slices.Equal(increments, []int{1024, 1024, 2}) {
		t.Fatalf("payload increments = %v", increments)
	}

	// FIN occupies one position but adds no payload bytes.
	if err := h.engine.RunUntil(5 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	h.receive(model.FlagFIN|model.FlagACK, 3051, 1, 0)
	h.assertReply(model.FlagFIN|model.FlagACK, 1, 3052)
	state := h.server.GetState()
	if state.ActiveConnections != 0 || state.HalfOpenConnections != 0 ||
		state.ReceivedPayloadBytes != 2050 || state.LatencyP95Milli != 5 {
		t.Fatalf("FIN changed payload or retained the connection: %+v", state)
	}
}

func TestServerSYNRetryKeepsReservationAndBytePositions(t *testing.T) {
	h := newByteServerHarness(t)
	h.receive(model.FlagSYN, 91, 0, 0)
	h.receive(model.FlagSYN, 91, 0, 0)
	h.assertReply(model.FlagSYN|model.FlagACK, 0, 92)
	if state := h.server.GetState(); state.HalfOpenConnections != 1 || len(h.replies) != 2 {
		t.Fatalf("SYN retry duplicated its reservation: %+v", state)
	}

	// Another ISN and invalid handshake confirmations cannot reset or promote it.
	h.receive(model.FlagSYN, 92, 0, 0)
	h.receive(model.FlagACK, 92, 0, 0)
	h.receive(model.FlagACK, 92, 2, 0)
	h.receive(model.FlagACK, 91, 1, 0)
	if state := h.server.GetState(); state.HalfOpenConnections != 1 ||
		state.ActiveConnections != 0 || len(h.replies) != 2 {
		t.Fatalf("invalid handshake changed its reservation: %+v", state)
	}

	// The first data ACK can complete a handshake after its standalone ACK was lost.
	h.receive(model.FlagACK, 92, 1, 17)
	h.assertReply(model.FlagACK, 1, 109)
	if state := h.server.GetState(); state.HalfOpenConnections != 0 || state.ActiveConnections != 1 {
		t.Fatalf("data did not complete the handshake: %+v", state)
	}

	// Delayed SYN and data copies cannot move the receive cursor backwards.
	h.receive(model.FlagSYN, 91, 0, 0)
	h.assertReply(model.FlagSYN|model.FlagACK, 0, 92)
	h.receive(model.FlagACK, 92, 1, 17)
	h.assertReply(model.FlagACK, 1, 109)
	h.receive(model.FlagACK, 109, 1, 3)
	h.assertReply(model.FlagACK, 1, 112)
	if got := h.server.GetState().ReceivedPayloadBytes; got != 20 {
		t.Fatalf("retries changed received payload: %d", got)
	}
}

func TestServerRejectsInvalidByteRangesAndACKs(t *testing.T) {
	h := newByteServerHarness(t)
	h.receive(model.FlagSYN, 0, 0, 0)
	h.receive(model.FlagACK, 1, 1, 17)
	h.assertReply(model.FlagACK, 1, 18)
	before := len(h.replies)

	// Invalid controls and byte ranges cannot change payload or connection state.
	invalid := []struct {
		flag model.Flag
		seq  uint64
		ack  uint64
		size int
	}{
		{flag: model.FlagACK, seq: 18, ack: 0, size: 7},
		{flag: model.FlagACK, seq: 18, ack: 2, size: 7},
		{flag: model.FlagACK, seq: 18, ack: 1, size: -1},
		{flag: model.FlagACK, seq: math.MaxUint64, ack: 1, size: 2},
		{flag: model.FlagFIN | model.FlagACK, seq: 18, ack: 1, size: 1},
		{flag: model.FlagFIN | model.FlagACK, seq: 17, ack: 1},
		{flag: model.FlagFIN | model.FlagACK, seq: 18, ack: 2},
		{flag: model.FlagSYN | model.FlagFIN, seq: 18},
		{flag: model.FlagSYN, seq: 0, size: 1},
		{flag: model.FlagSYN | model.FlagACK, seq: 0, ack: 1},
		{flag: model.FlagRST | model.FlagACK, seq: 18, ack: 1},
	}
	for _, packet := range invalid {
		h.receive(packet.flag, packet.seq, packet.ack, packet.size)
		state := h.server.GetState()
		if len(h.replies) != before || state.ReceivedPayloadBytes != 17 ||
			state.ActiveConnections != 1 || state.HalfOpenConnections != 0 {
			t.Fatalf("invalid packet %+v changed state: %+v", packet, state)
		}
	}

	// A complete earlier copy is acknowledged, and the next byte remains usable.
	h.receive(model.FlagACK, 1, 1, 17)
	h.assertReply(model.FlagACK, 1, 18)
	h.receive(model.FlagACK, 18, 1, 1)
	h.assertReply(model.FlagACK, 1, 19)
	if got := h.server.GetState().ReceivedPayloadBytes; got != 18 {
		t.Fatalf("received payload after invalid packets = %d", got)
	}
}

func TestServerLogicalBytePositionsDoNotWrapAtUint32(t *testing.T) {
	h := newByteServerHarness(t)
	const initial uint64 = 1 << 40
	h.receive(model.FlagSYN, initial, 0, 0)
	h.assertReply(model.FlagSYN|model.FlagACK, 0, initial+1)
	h.receive(model.FlagACK, initial+1, 1, 1024)
	h.assertReply(model.FlagACK, 1, initial+1025)
	h.receive(model.FlagFIN|model.FlagACK, initial+1025, 1, 0)
	h.assertReply(model.FlagFIN|model.FlagACK, 1, initial+1026)
	if got := h.server.GetState(); got.ReceivedPayloadBytes != 1024 || got.ActiveConnections != 0 {
		t.Fatalf("large logical position changed transfer accounting: %+v", got)
	}
}

func TestServerRejectsMalformedOpeningAndUnknownFIN(t *testing.T) {
	h := newByteServerHarness(t)
	h.receive(model.FlagSYN, math.MaxUint64, 0, 0)
	h.receive(model.FlagSYN, 0, 0, -1)
	h.receive(model.FlagSYN|model.FlagFIN, 0, 0, 0)
	h.receive(model.FlagFIN|model.FlagACK, 1, 1, 0)
	if state := h.server.GetState(); len(h.replies) != 0 || state.HalfOpenConnections != 0 ||
		state.ActiveConnections != 0 {
		t.Fatalf("malformed opening or unknown FIN created state: %+v", state)
	}
}
