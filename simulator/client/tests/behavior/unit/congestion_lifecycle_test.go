package client_test

import (
	"testing"
	"time"

	"load-balancer/model"
)

// A 1 s gap equals RTO and preserves cwnd=5120; a 1.05 s gap restarts at IW=4096.
// Restart happens only when the next send is due, using time since the last send.
func TestTCPRenoRestartsAfterIdleExceedingRTO(t *testing.T) {
	for _, gap := range []time.Duration{time.Second, 1050 * time.Millisecond} {
		t.Run(gap.String(), func(t *testing.T) {
			h := newRenoTransferHarness(t)
			useFixedCongestionTestTimers(h)
			h.client.SendCompleteStream(8192, gap)
			h.client.EstablishConnection()
			h.runUntil(0)
			h.receive(model.FlagSYN|model.FlagACK, 1)
			h.runUntil(gap + 100*time.Millisecond)
			h.receive(model.FlagACK, 1025)
			if state := congestionState(t, h); state.CongestionWindowBytes != 5120 {
				t.Fatalf("first data ACK did not grow the initial window: %+v", state)
			}

			// Even with a future send already scheduled, idle cannot reduce cwnd early.
			h.runUntil(2*gap - time.Nanosecond)
			if state := congestionState(t, h); state.CongestionWindowBytes != 5120 {
				t.Fatalf("a future send restarted the window before its deadline: %+v", state)
			}
			h.runUntil(2 * gap)
			wantWindow := uint64(5120)
			if gap > time.Second {
				wantWindow = 4096
			}
			state := congestionState(t, h)
			if state.CongestionWindowBytes != wantWindow || state.BytesInFlight != 1024 {
				t.Fatalf("idle gap=%s gave %+v, want window=%d", gap, state, wantWindow)
			}
		})
	}
}

// After a SYN retry, this connection starts and restarts with one MSS.
// Its first data ACK grows cwnd to two MSS, but a later 1.05 s gap returns it to one.
func TestTCPRenoIdleRestartKeepsReducedSYNWindow(t *testing.T) {
	h := newRenoTransferHarness(t)
	useFixedCongestionTestTimers(h)
	const gap = 1050 * time.Millisecond
	h.client.SendCompleteStream(8192, gap)
	h.client.EstablishConnection()

	// The first SYN times out at 1 s; the retry is acknowledged at 1.25 s.
	h.runUntil(1250 * time.Millisecond)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	firstSendAt := h.engine.Now() + gap
	h.runUntil(firstSendAt + 100*time.Millisecond)
	h.receive(model.FlagACK, 1025)
	if state := congestionState(t, h); state.CongestionWindowBytes != 2048 {
		t.Fatalf("first data ACK did not grow the reduced initial window: %+v", state)
	}

	// The configured four-MSS window cannot replace the effective one-MSS initial window.
	h.runUntil(firstSendAt + gap)
	state := congestionState(t, h)
	if state.CongestionWindowBytes != 1024 || state.BytesInFlight != 1024 {
		t.Fatalf("idle restart forgot the SYN retry's reduced initial window: %+v", state)
	}
}

// Eight synchronous data ACKs grow cwnd from 4 MSS to 12 MSS without oversending.
// Each callback sees its segment already included in flight before acknowledging it.
func TestTCPRenoSynchronousACKSeesUpdatedFlight(t *testing.T) {
	h := newRenoTransferHarness(t)
	h.client.SetIdleTimeout(0)
	h.client.SetSender(func(packet model.Packet) {
		h.sent = append(h.sent, packet)
		if packet.Flag == model.FlagSYN {
			h.receive(model.FlagSYN|model.FlagACK, 1)
		}
		if packet.Size > 0 {
			state := congestionState(t, h)
			if state.BytesInFlight != uint64(packet.Size) || state.BytesInFlight > state.CongestionWindowBytes {
				t.Fatalf("send callback saw stale flight or exceeded cwnd: %+v", state)
			}
			h.receive(model.FlagACK, packet.Seq+uint64(packet.Size))
		}
	})
	h.client.SendCompleteStream(8192, 0)
	h.client.EstablishConnection()
	h.runUntil(5 * time.Second)

	state := congestionState(t, h)
	if !h.client.StreamCompleted() || len(sentClientData(h.sent)) != 8 ||
		state.CongestionWindowBytes != 12288 || state.BytesInFlight != 0 {
		t.Fatalf("synchronous Reno stream = %+v, congestion = %+v", h.client.GetState(), state)
	}
	if pending := h.engine.Pending(); pending != 0 {
		t.Fatalf("completed synchronous stream left %d scheduled events", pending)
	}
}

// Closing with 4096 unconfirmed bytes clears flight and stops retries without growing cwnd.
// A stale ACK delivered after either close path cannot revive the pending stream.
func TestTCPRenoCleanupClearsFlight(t *testing.T) {
	for _, closeWithFIN := range []bool{false, true} {
		name := "shutdown"
		if closeWithFIN {
			name = "close with FIN"
		}
		t.Run(name, func(t *testing.T) {
			h := newRenoTransferHarness(t)
			h.client.SetIdleTimeout(0)
			h.client.SendCompleteStream(8192, 0)
			h.client.EstablishConnection()
			h.runUntil(0)
			h.receive(model.FlagSYN|model.FlagACK, 1)
			h.runUntil(0)
			before := congestionState(t, h)
			if before.BytesInFlight != 4096 {
				t.Fatalf("cleanup fixture did not fill its initial window: %+v", before)
			}

			if closeWithFIN {
				h.client.Close()
			} else {
				h.client.Shutdown()
			}
			h.receive(model.FlagACK, 4097)
			h.runUntil(5 * time.Second)
			before.BytesInFlight = 0
			if state := congestionState(t, h); state != before {
				t.Fatalf("cleanup retained flight or a stale ACK grew the window: %+v", state)
			}
			if len(sentClientData(h.sent)) != 4 || h.engine.Pending() != 0 {
				t.Fatalf("cleanup left payload work running: sent=%+v pending=%d", h.sent, h.engine.Pending())
			}
		})
	}
}

// With congestion disabled, an 8192-byte stream fills rwnd beyond the former four-MSS cap.
// Retrying its head keeps all 8192 bytes in flight and reports no congestion window.
func TestTCPCongestionNonePreservesReceiveWindowBaseline(t *testing.T) {
	h := newAdaptiveTransferHarness(t)
	h.client.SendCompleteStream(8192, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(200 * time.Millisecond)

	want := model.CongestionState{
		Mode:          model.CongestionNone,
		BytesInFlight: 8192,
		Phase:         "disabled",
	}
	if state := congestionState(t, h); state != want {
		t.Fatalf("disabled congestion state = %+v, want %+v", state, want)
	}
	data := sentClientData(h.sent)
	if len(data) != 9 || data[8] != data[0] {
		t.Fatalf("receive-window baseline did not send eight segments and retry its head: %+v", data)
	}
	h.receive(model.FlagACK, 8193)
	want.BytesInFlight = 0
	if state := congestionState(t, h); state != want || !h.client.StreamCompleted() {
		t.Fatalf("disabled congestion did not complete normally: %+v", state)
	}
}
