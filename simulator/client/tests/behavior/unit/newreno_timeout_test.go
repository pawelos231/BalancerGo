package client_test

import (
	"testing"
	"time"

	"load-balancer/model"
)

// The first partial ACK at 200 ms sets RTO to 1.2 s; another at 400 ms keeps it there.
// Waiting for each later hole must not keep pushing the timeout further into the future.
func TestTCPNewRenoOnlyFirstPartialACKRestartsRTO(t *testing.T) {
	h := newNewRenoHarness(t, 8, 8*model.SEGMENT_SIZE)
	h.receive(model.FlagACK, 1025)
	h.runUntil(100 * time.Millisecond)
	triggerNewRenoFastRetry(h)

	h.runUntil(200 * time.Millisecond)
	h.receive(model.FlagACK, 3073)
	h.runUntil(400 * time.Millisecond)
	h.receive(model.FlagACK, 5121)
	if state := congestionState(t, h); state.FastRetransmissions != 3 {
		t.Fatalf("partial ACKs did not repair successive holes: %+v", state)
	}

	h.runUntil(1200*time.Millisecond - time.Nanosecond)
	if h.transfer().DataRetransmissions != 3 {
		t.Fatal("timeout fired before the first partial ACK's deadline")
	}
	h.runUntil(1200 * time.Millisecond)
	state := congestionState(t, h)
	if h.transfer().DataRetransmissions != 4 || state.FastRetransmissions != 3 ||
		state.CongestionWindowBytes != model.SEGMENT_SIZE ||
		h.client.GetRTOState().CurrentRTO != 2*time.Second {
		t.Fatalf("RTO failed to end recovery at its original deadline: %+v", state)
	}

	// Late duplicates from that old window must not undo the timeout response.
	triggerNewRenoFastRetry(h)
	if state := congestionState(t, h); state.FastRetransmissions != 3 ||
		state.CongestionWindowBytes != model.SEGMENT_SIZE {
		t.Fatalf("old duplicate ACKs restarted fast recovery after RTO: %+v", state)
	}
}

// Base RFC 6582 starts recover at the SYN's sequence: ACK=1 has not passed it yet.
// Without the optional ACK heuristic, losing the very first payload byte waits for RTO.
func TestTCPNewRenoInitialRecoveryGuardUsesRTO(t *testing.T) {
	h := newNewRenoHarness(t, 8, 8*model.SEGMENT_SIZE)
	triggerNewRenoFastRetry(h)
	if state := congestionState(t, h); state.FastRetransmissions != 0 {
		t.Fatalf("initial guard allowed a fast retry before ACK progress: %+v", state)
	}
	h.runUntil(time.Second)
	state := congestionState(t, h)
	if h.transfer().DataRetransmissions != 1 || state.FastRetransmissions != 0 ||
		state.CongestionWindowBytes != model.SEGMENT_SIZE {
		t.Fatalf("initial recovery guard prevented timeout fallback: %+v", state)
	}
}
