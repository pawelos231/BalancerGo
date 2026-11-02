package client_test

import (
	"testing"
	"time"

	"load-balancer/model"
)

// ACK=1025 confirms segment 1; segments 2 and 4 are missing from the old window.
// Repairing segment 2 advances ACK to 3073, which immediately retries segment 4.
func TestTCPNewRenoRepairsSecondHoleBeforeTimeout(t *testing.T) {
	h := newNewRenoHarness(t, 8, 8*model.SEGMENT_SIZE)
	h.receive(model.FlagACK, 1025)
	h.runUntil(100 * time.Millisecond)
	receiveThreeDuplicateACKs(h)

	h.runUntil(200 * time.Millisecond)
	h.receive(model.FlagACK, 3073)
	data := sentClientData(h.sent)
	state := congestionState(t, h)
	if len(data) != 10 || data[8].Seq != 1025 || data[9].Seq != 3073 {
		t.Fatalf("expected retries of segments 2 and 4, got %+v", data)
	}
	if state.Phase != "fastRecovery" || state.FastRetransmissions != 2 ||
		state.SlowStartThresholdBytes != 3584 || state.CongestionWindowBytes != 5632 {
		t.Fatalf("partial ACK left the wrong recovery state: %+v", state)
	}

	// Duplicate and stale replies cannot confirm the same bytes twice.
	h.receive(model.FlagACK, 3073)
	h.receive(model.FlagACK, 1025)
	h.receive(model.FlagACK, 8193)
	h.runUntil(h.engine.Now())
	h.receive(model.FlagFIN|model.FlagACK, 8194)
	h.runUntil(3 * time.Second)
	state = congestionState(t, h)
	if !h.client.StreamCompleted() || h.transfer().DataRetransmissions != 2 ||
		h.client.GetState().AcknowledgedPayloadBytes != 8192 || state.BytesInFlight != 0 {
		t.Fatalf("repair did not complete unique payload exactly once: %+v", h.client.GetState())
	}
	if h.engine.Pending() != 0 || h.client.GetRTOState().CurrentRTO != time.Second {
		t.Fatal("fast recovery left a timer running or backed off RTO")
	}
}

// A partial-byte ACK trims [1025,2049) to [1793,2049); only those 256 bytes are retried.
func TestTCPNewRenoRetriesOnlyTheUnacknowledgedFragment(t *testing.T) {
	h := newNewRenoHarness(t, 8, 8*model.SEGMENT_SIZE)
	h.receive(model.FlagACK, 1025)
	samplesBeforeRetry := h.client.GetRTOState().Samples
	receiveThreeDuplicateACKs(h)
	h.receive(model.FlagACK, 1793)

	data := sentClientData(h.sent)
	retry := data[len(data)-1]
	state := congestionState(t, h)
	if retry.Seq != 1793 || retry.Size != 256 || state.FastRetransmissions != 2 ||
		state.Phase != "fastRecovery" || state.CongestionWindowBytes != 5888 {
		t.Fatalf("partial-byte ACK retried the wrong fragment: packet=%+v, state=%+v", retry, state)
	}
	if h.client.GetRTOState().Samples != samplesBeforeRetry {
		t.Fatal("Karn's rule accepted an ambiguous retransmission RTT")
	}
}

// A fourth duplicate creates room for new bytes, but the old recovery still ends at 8193.
// ACK=8193 completes that recovery even though a newer segment remains unconfirmed.
func TestTCPNewRenoKeepsRecoveryBoundaryWhileNewDataIsSent(t *testing.T) {
	h := newNewRenoHarness(t, 8, 32*model.SEGMENT_SIZE)
	h.receive(model.FlagACK, 1025)
	receiveThreeDuplicateACKs(h)
	h.receive(model.FlagACK, 1025)
	h.runUntil(h.engine.Now())
	data := sentClientData(h.sent)
	newTail := data[len(data)-1]
	if newTail.Seq != 8193 || newTail.Size != 512 {
		t.Fatalf("recovery did not send the expected new tail: %+v", newTail)
	}

	h.receive(model.FlagACK, 8193)
	state := congestionState(t, h)
	if state.Phase == "fastRecovery" || state.FastRetransmissions != 1 ||
		state.BytesInFlight != 512 || state.CongestionWindowBytes != 2048 {
		t.Fatalf("new data moved the fixed recovery boundary: %+v", state)
	}

	// ACK at the boundary does not permit a new loss episode from late duplicates.
	receiveThreeDuplicateACKs(h)
	if state := congestionState(t, h); state.FastRetransmissions != 1 {
		t.Fatalf("late duplicates reopened the old recovery: %+v", state)
	}

	// Progress beyond the boundary permits an independent loss in a newer flight.
	h.receive(model.FlagACK, 8705)
	h.runUntil(h.engine.Now())
	receiveThreeDuplicateACKs(h)
	if state := congestionState(t, h); state.FastRetransmissions != 2 || state.Phase != "fastRecovery" {
		t.Fatalf("a later flight could not enter its own recovery: %+v", state)
	}
}

// Retry callbacks can synchronously ACK the next hole and then the whole old window.
// Both recursive replies must see finished counters, trimmed buffers and valid timers.
func TestTCPNewRenoSupportsSynchronousPartialAndFullACKs(t *testing.T) {
	h := newNewRenoHarness(t, 8, 8*model.SEGMENT_SIZE)
	h.receive(model.FlagACK, 1025)
	h.client.SetSender(func(packet model.Packet) {
		h.sent = append(h.sent, packet)
		if packet.Flag&model.FlagFIN != 0 {
			h.receive(model.FlagFIN|model.FlagACK, packet.Seq+1)
		}
		switch packet.Seq {
		case 1025:
			h.receive(model.FlagACK, 3073)
		case 3073:
			h.receive(model.FlagACK, 8193)
		}
	})
	receiveThreeDuplicateACKs(h)
	h.runUntil(3 * time.Second)

	state := congestionState(t, h)
	if !h.client.StreamCompleted() || state.FastRetransmissions != 2 ||
		h.transfer().DataRetransmissions != 2 || state.BytesInFlight != 0 || h.engine.Pending() != 0 {
		t.Fatalf("synchronous recovery retained work or lost counters: %+v", h.client.GetState())
	}
}

// A payload observer may deliver a newer ACK before the earlier ACK's retry is emitted.
// That newer reply closes the old gap; the captured retry must not resend the new tail.
func TestTCPNewRenoPayloadCallbackCannotRetryAnAlreadyConfirmedGap(t *testing.T) {
	h := newNewRenoHarness(t, 8, 32*model.SEGMENT_SIZE)
	h.receive(model.FlagACK, 1025)
	receiveThreeDuplicateACKs(h)
	h.receive(model.FlagACK, 1025)
	h.runUntil(h.engine.Now()) // A new 512-byte tail follows the fixed recovery boundary.
	sendsBeforeACK := len(sentClientData(h.sent))

	advanced := false
	h.client.OnPayloadAcknowledged = func(int) {
		if !advanced {
			advanced = true
			h.receive(model.FlagACK, 8193)
		}
	}
	h.receive(model.FlagACK, 3073)
	state := congestionState(t, h)
	if !advanced || state.FastRetransmissions != 1 || state.BytesInFlight != 512 ||
		state.Phase == "fastRecovery" || len(sentClientData(h.sent)) != sendsBeforeACK {
		t.Fatalf("a stale partial-ACK action retried newer data: %+v", h.client.GetState())
	}
}

// Disabling fast retransmit keeps NewReno on the ordinary one-second RTO path.
func TestTCPNewRenoCanDisableFastRetransmit(t *testing.T) {
	h := startNewRenoHarness(t, 8, 8*model.SEGMENT_SIZE, false)
	h.receive(model.FlagACK, 1025)
	receiveThreeDuplicateACKs(h)
	h.runUntil(time.Second - time.Nanosecond)
	if h.transfer().DataRetransmissions != 0 {
		t.Fatal("disabled fast retransmit still sent an early retry")
	}
	h.runUntil(time.Second)
	state := congestionState(t, h)
	if h.transfer().DataRetransmissions != 1 || state.FastRetransmissions != 0 ||
		state.CongestionWindowBytes != model.SEGMENT_SIZE {
		t.Fatalf("NewReno changed the RTO fallback: %+v", state)
	}
}

// Only the first partial ACK restarts the one-second RTO.
func TestTCPNewRenoOnlyFirstPartialACKRestartsRTO(t *testing.T) {
	h := newNewRenoHarness(t, 8, 8*model.SEGMENT_SIZE)
	h.receive(model.FlagACK, 1025)
	h.runUntil(100 * time.Millisecond)
	receiveThreeDuplicateACKs(h)

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

	// Late duplicates must not undo the timeout response.
	receiveThreeDuplicateACKs(h)
	if state := congestionState(t, h); state.FastRetransmissions != 3 ||
		state.CongestionWindowBytes != model.SEGMENT_SIZE {
		t.Fatalf("old duplicate ACKs restarted fast recovery after RTO: %+v", state)
	}
}

// Losing the first payload byte waits for RTO until ACKs pass the initial boundary.
func TestTCPNewRenoInitialRecoveryGuardUsesRTO(t *testing.T) {
	h := newNewRenoHarness(t, 8, 8*model.SEGMENT_SIZE)
	receiveThreeDuplicateACKs(h)
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

func newNewRenoHarness(t *testing.T, initialSegments, payloadBytes int) *transferHarness {
	t.Helper()
	return startNewRenoHarness(t, initialSegments, payloadBytes, true)
}

func startNewRenoHarness(
	t *testing.T,
	initialSegments, payloadBytes int,
	fastRetransmit bool,
) *transferHarness {
	t.Helper()
	h := newRenoTransferHarness(t)
	config := model.DefaultCongestionConfig()
	config.Mode = model.CongestionNewReno
	config.InitialWindowSegments = initialSegments
	config.FastRetransmit = fastRetransmit
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

func receiveThreeDuplicateACKs(h *transferHarness) {
	ack := uint64(1) + h.client.GetState().AcknowledgedPayloadBytes
	for count := 0; count < 3; count++ {
		h.receive(model.FlagACK, ack)
	}
}
