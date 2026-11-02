package client_test

import (
	"testing"
	"time"

	"load-balancer/model"
)

// Eight outstanding segments give ssthresh=4 MSS and a temporary recovery window of 7 MSS.
// The fifth duplicate ACK permits one new segment; repairing the gap leaves cwnd at 4 MSS.
func TestTCPFastRetransmitAndRenoRecovery(t *testing.T) {
	h := newFastRecoveryHarness(t, 8, 32*model.SEGMENT_SIZE)
	h.runUntil(100 * time.Millisecond)

	// Two duplicate ACKs leave the original eight segments and their RTO untouched.
	for count := 0; count < 2; count++ {
		h.receive(model.FlagACK, 1)
	}
	if len(sentClientData(h.sent)) != 8 {
		t.Fatal("fewer than three duplicate ACKs retried a segment")
	}

	h.receive(model.FlagACK, 1)
	data := sentClientData(h.sent)
	state := congestionState(t, h)
	if len(data) != 9 || data[8] != data[0] {
		t.Fatalf("third duplicate did not retry the oldest segment: %+v", data)
	}
	if state.Phase != "fastRecovery" || state.CongestionWindowBytes != 7168 ||
		state.SlowStartThresholdBytes != 4096 || state.BytesInFlight != 8192 ||
		state.DuplicateACKs != 3 || state.FastRetransmissions != 1 {
		t.Fatalf("incorrect recovery entry: %+v", state)
	}
	if h.transfer().DataRetransmissions != 1 || h.client.GetRTOState().CurrentRTO != time.Second {
		t.Fatal("fast retry was not counted, or incorrectly backed off RTO")
	}

	// The fourth duplicate reaches cwnd=8 MSS; the fifth creates one MSS of credit.
	h.receive(model.FlagACK, 1)
	h.runUntil(h.engine.Now())
	if len(sentClientData(h.sent)) != 9 {
		t.Fatal("sender transmitted while flight still filled the temporary window")
	}
	h.receive(model.FlagACK, 1)
	h.runUntil(h.engine.Now())
	data = sentClientData(h.sent)
	if len(data) != 10 || data[9].Seq != 8193 || data[9].Size != model.SEGMENT_SIZE {
		t.Fatalf("recovery credit did not release exactly one new segment: %+v", data)
	}

	// ACK=8193 confirms the repaired old flight, leaving the new segment outstanding.
	// It exits recovery without also performing a slow-start or avoidance increment.
	h.receive(model.FlagACK, 8193)
	state = congestionState(t, h)
	if state.Phase != "congestionAvoidance" || state.CongestionWindowBytes != 4096 ||
		state.BytesInFlight != 1024 || state.DuplicateACKs != 0 || state.FastRetransmissions != 1 {
		t.Fatalf("incorrect recovery exit: %+v", state)
	}
	if h.client.GetState().AcknowledgedPayloadBytes != 8192 {
		t.Fatal("retransmission changed the unique acknowledged payload count")
	}
	if h.client.GetRTOState().Samples != 1 {
		t.Fatal("Karn's rule accepted the ambiguous recovery ACK as an RTT sample")
	}
}

// A window-only ACK changes rwnd but does not count toward the three-duplicate threshold.
// Duplicate, window update, duplicate, duplicate contains exactly three qualifying replies.
func TestTCPFastRetransmitIgnoresWindowUpdates(t *testing.T) {
	h := newFastRecoveryHarness(t, 8, 16*model.SEGMENT_SIZE)
	h.receive(model.FlagACK, 1)
	h.receiveWindow(model.FlagACK, 1, 60000)
	if state := congestionState(t, h); state.DuplicateACKs != 1 {
		t.Fatalf("window update counted as a duplicate: %+v", state)
	}

	h.receiveWindow(model.FlagACK, 1, 60000)
	if len(sentClientData(h.sent)) != 8 {
		t.Fatal("window update caused recovery before the third duplicate")
	}
	h.receiveWindow(model.FlagACK, 1, 60000)
	if state := congestionState(t, h); state.FastRetransmissions != 1 || state.DuplicateACKs != 3 {
		t.Fatalf("third qualifying duplicate did not enter recovery: %+v", state)
	}
}

// A receiver-window limit still wins over a temporarily inflated congestion window.
// Eight old segments fill rwnd=8192, so additional duplicate ACKs cannot send beyond it.
func TestTCPFastRecoveryStillRespectsReceiveWindowAndInflationCap(t *testing.T) {
	h := newFastRecoveryHarness(t, 8, 32*model.SEGMENT_SIZE)
	h.receiveWindow(model.FlagACK, 1, 8192)
	for count := 0; count < 100; count++ {
		h.receiveWindow(model.FlagACK, 1, 8192)
		h.runUntil(h.engine.Now())
	}

	state := congestionState(t, h)
	if len(sentClientData(h.sent)) != 9 || state.BytesInFlight != 8192 {
		t.Fatalf("recovery exceeded the receiver's full window: %+v", state)
	}
	if state.CongestionWindowBytes != 12*model.SEGMENT_SIZE || state.FastRetransmissions != 1 {
		t.Fatalf("duplicate flood inflated the window beyond the original flight: %+v", state)
	}
}

// The retransmission callback may immediately ACK the entire stream and close it.
// All recovery accounting and the timer must already be ready before that callback.
func TestTCPFastRetransmitSupportsSynchronousRecoveryACK(t *testing.T) {
	h := newFastRecoveryHarness(t, 4, 4*model.SEGMENT_SIZE)
	h.client.SetSender(func(packet model.Packet) {
		h.sent = append(h.sent, packet)
		if packet.Flag&model.FlagFIN != 0 {
			h.receive(model.FlagFIN|model.FlagACK, packet.Seq+1)
		}
		if packet.Size > 0 {
			state := congestionState(t, h)
			if state.FastRetransmissions != 1 || state.Phase != "fastRecovery" {
				t.Fatalf("retry callback saw unfinished recovery state: %+v", state)
			}
			h.receive(model.FlagACK, 4097)
		}
	})
	for count := 0; count < 3; count++ {
		h.receive(model.FlagACK, 1)
	}
	h.runUntil(3 * time.Second)

	state := congestionState(t, h)
	if !h.client.StreamCompleted() || len(sentClientData(h.sent)) != 5 || h.engine.Pending() != 0 {
		t.Fatalf("synchronous recovery left work running: %+v, pending=%d", state, h.engine.Pending())
	}
	if state.FastRetransmissions != 1 || state.DuplicateACKs != 0 || state.BytesInFlight != 0 {
		t.Fatalf("completion lost the retained fast retry count or kept flight: %+v", state)
	}
}

// Keep examples on a fixed 1 s RTO; backoff remains enabled for fallback tests.
func newFastRecoveryHarness(t *testing.T, initialSegments, payloadBytes int) *transferHarness {
	t.Helper()
	h := newRenoTransferHarness(t)
	config := model.DefaultCongestionConfig()
	config.InitialWindowSegments = initialSegments
	h.client.SetCongestionConfig(config)
	rto := model.DefaultRTOConfig()
	rto.Mode = model.RTOFixed
	h.client.SetRTOConfig(rto)
	h.client.SetIdleTimeout(0)
	h.client.SendCompleteStream(payloadBytes, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(0)
	return h
}
