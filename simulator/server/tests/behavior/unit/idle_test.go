package server_test

import (
	"testing"
	"time"

	"load-balancer/model"
)

// Repeating SYN at 9 ms cannot move the reservation's original 10 ms deadline.
func TestServerHalfOpenTimeoutKeepsTheFirstDeadline(t *testing.T) {
	h := newByteServerHarness(t)
	h.server.SetIdleTimeouts(10*time.Millisecond, 0)
	h.receive(model.FlagSYN, 0, 0, 0)

	h.runUntil(9 * time.Millisecond)
	h.receive(model.FlagSYN, 0, 0, 0)
	if got := h.server.GetState().HalfOpenConnections; got != 1 {
		t.Fatalf("half-open connections = %d, want 1", got)
	}

	h.runUntil(10 * time.Millisecond)
	h.assertConnection(model.TCPClosed, 0)
	if got := h.server.GetState().HalfOpenConnections; got != 0 {
		t.Fatalf("expired reservation still occupies a slot: %d", got)
	}
}

// A valid packet at 8 ms extends a 10 ms idle limit to 18 ms; a bad ACK does not.
func TestServerIdleTimeoutRefreshesOnlyAfterValidPeerTraffic(t *testing.T) {
	for _, test := range []struct {
		name string
		size int
	}{
		{name: "ACK"},
		{name: "payload", size: 10},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newByteServerHarness(t)
			h.server.SetIdleTimeouts(5*time.Millisecond, 10*time.Millisecond)
			h.receive(model.FlagSYN, 0, 0, 0)
			h.receive(model.FlagACK, 1, 1, 0)

			h.runUntil(8 * time.Millisecond)
			h.receive(model.FlagACK, 1, 1, test.size)
			h.runUntil(10 * time.Millisecond)
			h.assertConnection(model.TCPEstablished, 1)

			// This sequence is beyond the next expected byte and cannot refresh idle ACK activity.
			h.runUntil(17 * time.Millisecond)
			h.receive(model.FlagACK, 999, 1, 0)
			h.assertConnection(model.TCPEstablished, 1)

			h.runUntil(18 * time.Millisecond)
			h.assertConnection(model.TCPClosed, 0)
			if h.engine.Pending() != 0 {
				t.Fatal("expired connection left an idle event")
			}
		})
	}
}

// Completing SYN at 9 ms replaces the 10 ms handshake deadline with a 39 ms idle deadline.
func TestServerHandshakeSwitchesToItsEstablishedIdleLimit(t *testing.T) {
	h := newByteServerHarness(t)
	h.server.SetIdleTimeouts(10*time.Millisecond, 30*time.Millisecond)
	h.receive(model.FlagSYN, 0, 0, 0)

	h.runUntil(9 * time.Millisecond)
	h.receive(model.FlagACK, 1, 1, 0)
	h.runUntil(10 * time.Millisecond)
	h.assertConnection(model.TCPEstablished, 1)

	h.runUntil(39 * time.Millisecond)
	h.assertConnection(model.TCPClosed, 0)
}

// Explicit zero limits keep both a silent half-open reservation and an established flow alive.
func TestServerIdleCleanupCanBeDisabled(t *testing.T) {
	h := newByteServerHarness(t)
	h.server.SetIdleTimeouts(0, 0)
	h.receive(model.FlagSYN, 0, 0, 0)
	h.runUntil(500 * time.Millisecond)
	if got := h.server.GetConnectionState(h.flow).State; got != model.TCPSynReceived {
		t.Fatalf("disabled half-open cleanup changed state to %s", got)
	}

	h.receive(model.FlagACK, 1, 1, 0)
	h.runUntil(time.Second)
	h.assertConnection(model.TCPEstablished, 1)
	if h.engine.Pending() != 0 {
		t.Fatal("disabled idle cleanup scheduled an event")
	}
}

// A new flow using the same tuple at 8 ms survives the old flow's 10 ms deadline.
func TestServerOldIdleTimerCannotDeleteAReplacementConnection(t *testing.T) {
	h := newByteServerHarness(t)
	h.server.SetIdleTimeouts(10*time.Millisecond, 0)
	h.receive(model.FlagSYN, 0, 0, 0)
	h.runUntil(8 * time.Millisecond)
	h.server.ForgetConnection(h.flow)
	h.receive(model.FlagSYN, 1000, 0, 0)

	h.runUntil(10 * time.Millisecond)
	if got := h.server.GetConnectionState(h.flow).State; got != model.TCPSynReceived {
		t.Fatalf("old deadline removed replacement: %s", got)
	}

	h.runUntil(18 * time.Millisecond)
	h.assertConnection(model.TCPClosed, 0)
}

// A 5 ms generic idle limit must not interrupt the FIN retry waiting for its 10 ms RTO.
func TestServerClosingUsesFINTimersInsteadOfIdleCleanup(t *testing.T) {
	h := newByteServerHarness(t)
	h.server.SetIdleTimeouts(time.Millisecond, 5*time.Millisecond)
	h.server.SetRTOConfig(model.RTOConfig{
		Mode:           model.RTOFixed,
		InitialTimeout: 10 * time.Millisecond,
		MinimumTimeout: time.Millisecond,
		MaximumTimeout: time.Second,
	})
	h.receive(model.FlagSYN, 0, 0, 0)
	h.receive(model.FlagACK, 1, 1, 0)
	h.receive(model.FlagFIN|model.FlagACK, 1, 1, 0)

	h.runUntil(5 * time.Millisecond)
	h.assertConnection(model.TCPLastAck, 1)
	h.runUntil(10 * time.Millisecond)
	if state := h.server.GetConnectionState(h.flow); state.FINRetransmissions != 1 {
		t.Fatalf("idle cleanup interrupted FIN retry: %+v", state)
	}

	h.receive(model.FlagACK, 2, 2, 0)
	h.assertConnection(model.TCPClosed, 0)
	if h.engine.Pending() != 0 {
		t.Fatal("finished close left timers")
	}
}

// Shutting down a backend cancels its local cleanup without emitting FIN or RST.
func TestServerShutdownCancelsItsIdleTimer(t *testing.T) {
	h := newByteServerHarness(t)
	h.server.SetIdleTimeouts(time.Millisecond, 10*time.Millisecond)
	h.receive(model.FlagSYN, 0, 0, 0)
	h.receive(model.FlagACK, 1, 1, 0)
	before := len(h.replies)

	h.server.Shutdown()
	h.runUntil(time.Second)
	h.assertConnection(model.TCPClosed, 0)
	if len(h.replies) != before || h.engine.Pending() != 0 {
		t.Fatal("shutdown emitted a packet or retained an idle timer")
	}
}
