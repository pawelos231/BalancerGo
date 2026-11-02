package server_test

import (
	"testing"
	"time"

	"load-balancer/model"
)

func newClosingServer(t *testing.T, retries int) *byteServerHarness {
	t.Helper()
	h := newByteServerHarness(t)
	h.server.SetCloseConfig(model.CloseConfig{
		MSL: 10 * time.Millisecond, FinWaitTimeout: 50 * time.Millisecond,
		MaxFINRetries: retries,
	})
	h.server.SetRTOConfig(model.RTOConfig{
		Mode: model.RTOFixed, ExponentialBackoff: true,
		InitialTimeout: 10 * time.Millisecond, MinimumTimeout: time.Millisecond,
		MaximumTimeout: 40 * time.Millisecond,
	})
	h.receive(model.FlagSYN, 0, 0, 0)
	h.receive(model.FlagACK, 1, 1, 0)
	return h
}

func (h *byteServerHarness) runUntil(at time.Duration) {
	h.t.Helper()
	if err := h.engine.RunUntil(at); err != nil {
		h.t.Fatal(err)
	}
}

func (h *byteServerHarness) assertConnection(state model.TCPState, active uint32) {
	h.t.Helper()
	if got := h.server.GetConnectionState(h.flow); got.State != state {
		h.t.Fatalf("connection state = %+v, want %s", got, state)
	}
	if got := h.server.GetState().ActiveConnections; got != active {
		h.t.Fatalf("active connections = %d, want %d", got, active)
	}
}

func TestServerLostFinalACKKeepsFINSequenceAndReservation(t *testing.T) {
	h := newClosingServer(t, 2)
	h.receive(model.FlagFIN|model.FlagACK, 1, 1, 0)
	h.assertReply(model.FlagFIN|model.FlagACK, 1, 2)
	h.assertConnection(model.TCPLastAck, 1)
	h.runUntil(10 * time.Millisecond)
	h.assertReply(model.FlagFIN|model.FlagACK, 1, 2)
	if state := h.server.GetConnectionState(h.flow); state.FINRetransmissions != 1 {
		t.Fatalf("lost ACK did not retry FIN: %+v", state)
	}

	h.receive(model.FlagACK, 2, 2, 0)
	h.assertConnection(model.TCPClosed, 0)
	if h.engine.Pending() != 0 {
		t.Fatal("final ACK left a retry timer")
	}
	before := len(h.replies)
	h.runUntil(time.Second)
	if len(h.replies) != before {
		t.Fatal("FIN retried after its final ACK")
	}
}

func TestServerRepeatedFINDoesNotPostponeRetryLimit(t *testing.T) {
	h := newClosingServer(t, 1)
	h.receive(model.FlagFIN|model.FlagACK, 1, 1, 0)
	h.runUntil(9 * time.Millisecond)
	h.receive(model.FlagFIN|model.FlagACK, 1, 1, 0)
	h.assertReply(model.FlagFIN|model.FlagACK, 1, 2)
	h.runUntil(10 * time.Millisecond)
	if state := h.server.GetConnectionState(h.flow); state.FINRetransmissions != 2 {
		t.Fatalf("repeated FIN moved the original retry: %+v", state)
	}
	h.runUntil(29 * time.Millisecond)
	h.receive(model.FlagFIN|model.FlagACK, 1, 1, 0)
	h.runUntil(30 * time.Millisecond)
	h.assertConnection(model.TCPClosed, 0)
	if h.engine.Pending() != 0 {
		t.Fatal("retry limit left closing events")
	}
}

func TestServerFINBackoffReachesConfiguredMaximum(t *testing.T) {
	h := newClosingServer(t, 3)
	h.receive(model.FlagFIN|model.FlagACK, 1, 1, 0)
	for index, at := range []time.Duration{10, 30, 70} {
		h.runUntil(at * time.Millisecond)
		state := h.server.GetConnectionState(h.flow)
		if state.FINRetransmissions != uint64(index+1) {
			t.Fatalf("retry at %s: %+v", at*time.Millisecond, state)
		}
		h.assertReply(model.FlagFIN|model.FlagACK, 1, 2)
	}
	h.runUntil(109 * time.Millisecond)
	h.assertConnection(model.TCPLastAck, 1)
	h.runUntil(110 * time.Millisecond)
	h.assertConnection(model.TCPClosed, 0)
}

func TestServerActiveCloseReceivesDataAndWaitsForPeerFIN(t *testing.T) {
	h := newClosingServer(t, 2)
	h.server.CloseConnection(h.flow)
	h.assertReply(model.FlagFIN|model.FlagACK, 1, 1)
	h.assertConnection(model.TCPFinWait1, 1)
	// The peer can send data before receiving or acknowledging our FIN.
	h.receive(model.FlagACK, 1, 1, 4)
	h.assertReply(model.FlagACK, 2, 5)
	h.assertConnection(model.TCPFinWait1, 1)
	h.receive(model.FlagACK, 5, 2, 0)
	h.assertConnection(model.TCPFinWait2, 1)
	h.receive(model.FlagACK, 5, 2, 3)
	h.assertReply(model.FlagACK, 2, 8)
	if got := h.server.GetState().ReceivedPayloadBytes; got != 7 {
		t.Fatalf("half-close received %d bytes, want 7", got)
	}
	h.receive(model.FlagFIN|model.FlagACK, 8, 2, 0)
	h.assertReply(model.FlagACK, 2, 9)
	h.assertConnection(model.TCPTimeWait, 1)
	h.runUntil(19 * time.Millisecond)
	h.assertConnection(model.TCPTimeWait, 1)
	h.runUntil(20 * time.Millisecond)
	h.assertConnection(model.TCPClosed, 0)
}

func TestServerTimeWaitRestartsOnDuplicatePeerFIN(t *testing.T) {
	h := newClosingServer(t, 2)
	h.server.CloseConnection(h.flow)
	h.receive(model.FlagFIN|model.FlagACK, 1, 2, 0)
	h.assertConnection(model.TCPTimeWait, 1)
	h.runUntil(15 * time.Millisecond)
	h.receive(model.FlagFIN|model.FlagACK, 1, 2, 0)
	h.assertReply(model.FlagACK, 2, 2)
	h.runUntil(20 * time.Millisecond)
	h.assertConnection(model.TCPTimeWait, 1)
	h.runUntil(35 * time.Millisecond)
	h.assertConnection(model.TCPClosed, 0)
}

func TestServerSimultaneousCloseAcknowledgesFINIndependently(t *testing.T) {
	h := newClosingServer(t, 2)
	h.server.CloseConnection(h.flow)
	h.receive(model.FlagFIN|model.FlagACK, 1, 1, 0)
	h.assertReply(model.FlagACK, 2, 2)
	h.assertConnection(model.TCPClosing, 1)
	h.receive(model.FlagACK, 2, 2, 0)
	h.assertConnection(model.TCPTimeWait, 1)
	h.runUntil(20 * time.Millisecond)
	h.assertConnection(model.TCPClosed, 0)
}

func TestServerFinWait2HasBoundedLifetime(t *testing.T) {
	h := newClosingServer(t, 2)
	h.server.CloseConnection(h.flow)
	h.receive(model.FlagACK, 1, 2, 0)
	h.assertConnection(model.TCPFinWait2, 1)
	h.runUntil(49 * time.Millisecond)
	h.assertConnection(model.TCPFinWait2, 1)
	h.runUntil(50 * time.Millisecond)
	h.assertConnection(model.TCPClosed, 0)
	if h.engine.Pending() != 0 {
		t.Fatal("FIN_WAIT_2 timeout left events")
	}
}

func TestServerFINWaitsForMissingPayload(t *testing.T) {
	h := newClosingServer(t, 2)
	h.receive(model.FlagACK, 5, 1, 4)
	h.receive(model.FlagFIN|model.FlagACK, 9, 1, 0)
	h.assertReply(model.FlagACK, 1, 1)
	h.assertConnection(model.TCPEstablished, 1)
	h.receive(model.FlagACK, 1, 1, 4)
	h.assertReply(model.FlagFIN|model.FlagACK, 1, 10)
	h.assertConnection(model.TCPLastAck, 1)
	if got := h.server.GetState().ReceivedPayloadBytes; got != 8 {
		t.Fatalf("reordered FIN changed unique payload: %d", got)
	}
	h.receive(model.FlagACK, 10, 2, 0)
	h.assertConnection(model.TCPClosed, 0)
}

func TestServerInvalidFINCannotAcknowledgeOurFIN(t *testing.T) {
	h := newClosingServer(t, 2)
	h.server.CloseConnection(h.flow)
	before := len(h.replies)
	h.receive(model.FlagFIN|model.FlagACK, 1, 2, 1)
	h.receive(model.FlagFIN|model.FlagACK, 0, 2, 0)
	h.receive(model.FlagACK, 2, 2, 0)
	h.assertConnection(model.TCPFinWait1, 1)
	if len(h.replies) != before || h.engine.Pending() != 1 {
		t.Fatal("invalid closing controls changed replies or retry timer")
	}
	h.runUntil(10 * time.Millisecond)
	h.assertReply(model.FlagFIN|model.FlagACK, 1, 1)
}

func TestServerFINCannotPrecedeBufferedPayload(t *testing.T) {
	h := newClosingServer(t, 2)
	h.receive(model.FlagACK, 5, 1, 4)
	before := len(h.replies)
	h.receive(model.FlagFIN|model.FlagACK, 5, 1, 0)
	h.assertConnection(model.TCPEstablished, 1)
	if len(h.replies) != before {
		t.Fatal("FIN before accepted data caused a reply")
	}
	h.receive(model.FlagFIN|model.FlagACK, 9, 1, 0)
	h.receive(model.FlagACK, 1, 1, 4)
	h.assertReply(model.FlagFIN|model.FlagACK, 1, 10)
	h.assertConnection(model.TCPLastAck, 1)
}

func TestServerRSTValidationAndNoResponse(t *testing.T) {
	h := newClosingServer(t, 2)
	h.server.CloseConnection(h.flow)
	before := len(h.replies)
	invalid := []model.Packet{
		{Flag: model.FlagRST, Seq: 0},
		{Flag: model.FlagRST, Seq: 2},
		{Flag: model.FlagRST | model.FlagACK, Seq: 1, Ack: 3},
		{Flag: model.FlagRST | model.FlagACK, Seq: 1, Ack: 0},
		{Flag: model.FlagRST | model.FlagSYN, Seq: 1},
		{Flag: model.FlagRST | model.FlagFIN, Seq: 1},
		{Flag: model.FlagRST | model.FlagPSH, Seq: 1},
		{Flag: model.FlagRST, Seq: 1, Size: 1},
	}
	for _, packet := range invalid {
		packet.Key = h.flow
		h.server.HandlePacket(packet)
		h.assertConnection(model.TCPFinWait1, 1)
		if len(h.replies) != before {
			t.Fatalf("invalid RST caused a response: %+v", packet)
		}
	}
	h.receive(model.FlagRST, 1, 0, 0)
	h.assertConnection(model.TCPClosed, 0)
	if len(h.replies) != before || h.engine.Pending() != 0 {
		t.Fatal("valid RST caused a reply or left timers")
	}
}

func TestServerHalfOpenRSTMustAcknowledgeSYN(t *testing.T) {
	h := newByteServerHarness(t)
	h.receive(model.FlagSYN, 9, 0, 0)
	before := len(h.replies)
	h.receive(model.FlagRST, 10, 0, 0)
	h.receive(model.FlagRST|model.FlagACK, 10, 0, 0)
	h.receive(model.FlagRST|model.FlagACK, 9, 1, 0)
	if state := h.server.GetState(); state.HalfOpenConnections != 1 || len(h.replies) != before {
		t.Fatalf("invalid opening RST changed reservation: %+v", state)
	}
	h.receive(model.FlagRST|model.FlagACK, 10, 1, 0)
	if state := h.server.GetState(); state.HalfOpenConnections != 0 || len(h.replies) != before {
		t.Fatalf("valid opening RST retained state or caused reply: %+v", state)
	}
}

func TestServerAbortSendsCurrentResetAndCancelsTimers(t *testing.T) {
	h := newClosingServer(t, 2)
	h.receive(model.FlagACK, 1, 1, 4)
	h.server.CloseConnection(h.flow)
	h.server.AbortConnection(h.flow)
	packet := h.replies[len(h.replies)-1]
	if packet.Flag != model.FlagRST|model.FlagACK || packet.Seq != 2 || packet.Ack != 5 {
		t.Fatalf("abort reset = %+v", packet)
	}
	h.assertConnection(model.TCPClosed, 0)
	if h.engine.Pending() != 0 {
		t.Fatal("abort left a FIN timer")
	}
	h.server.AbortConnection(h.flow)
	h.server.ForgetConnection(h.flow)
	h.assertConnection(model.TCPClosed, 0)
}

func TestServerDirectFinalACKCancelsPreparedTimer(t *testing.T) {
	h := newClosingServer(t, 2)
	h.server.SetSender(func(packet model.Packet) {
		if packet.Flag&model.FlagFIN != 0 {
			h.receive(model.FlagACK, packet.Ack, packet.Seq+1, 0)
		}
	})
	h.receive(model.FlagFIN|model.FlagACK, 1, 1, 0)
	h.assertConnection(model.TCPClosed, 0)
	if h.engine.Pending() != 0 {
		t.Fatal("synchronous final ACK left its prepared timer")
	}
}

func TestServerShutdownAndForgetCancelClosingEvents(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		h := newClosingServer(t, 2)
		h.server.CloseConnection(h.flow)
		if shutdown {
			h.server.Shutdown()
		} else {
			h.server.ForgetConnection(h.flow)
		}
		h.assertConnection(model.TCPClosed, 0)
		if h.engine.Pending() != 0 {
			t.Fatalf("shutdown=%t left closing events", shutdown)
		}
	}
}
