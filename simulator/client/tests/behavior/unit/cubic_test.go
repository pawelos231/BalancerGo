package client_test

import (
	"testing"
	"time"

	"load-balancer/model"
)

// A lost head in an eight-MSS flight uses CUBIC's 70% threshold and classic fast recovery.
// The retry keeps the original bytes/SEQ; a later new ACK resumes normal window growth.
func TestTCPCubicFastLossUsesSeventyPercentThreshold(t *testing.T) {
	h := newCubicHarness(t, 8, 16*model.SEGMENT_SIZE, 65535, 0)
	for count := 0; count < 3; count++ {
		h.receive(model.FlagACK, 1)
	}
	state := congestionState(t, h)
	if state.Mode != model.CongestionCubic || state.SlowStartThresholdBytes != 5734 ||
		state.CongestionWindowBytes != 8806 || state.Phase != "fastRecovery" ||
		state.BytesInFlight != 8192 || state.FastRetransmissions != 1 {
		t.Fatalf("CUBIC did not prepare the expected fast-retry state: %+v", state)
	}
	data := sentClientData(h.sent)
	if len(data) != 9 || data[8] != data[0] || h.transfer().DataRetransmissions != 1 {
		t.Fatalf("fast retransmit did not retain the original head: %+v", data)
	}

	// A fourth duplicate can inflate the classic recovery window by one more MSS.
	h.receive(model.FlagACK, 1)
	if current := congestionState(t, h); current.CongestionWindowBytes != 9830 ||
		current.FastRetransmissions != 1 {
		t.Fatalf("the next duplicate retried again instead of inflating recovery: %+v", current)
	}
	h.receive(model.FlagACK, 8193)
	if current := congestionState(t, h); current.CongestionWindowBytes != 5734 ||
		current.Phase != "slowStart" {
		t.Fatalf("advancing ACK did not deflate to the CUBIC threshold: %+v", current)
	}

	// The selected RFC profile includes equality with ssthresh in slow start.
	h.runUntil(0)
	h.receive(model.FlagACK, 9217)
	if current := congestionState(t, h); current.CongestionWindowBytes != 6758 ||
		current.Phase != "congestionAvoidance" {
		t.Fatalf("new data ACK did not leave slow start normally: %+v", current)
	}
}

// A fast retry does not consume the first RTO reduction; trimming the same head does not repeat it.
func TestTCPCubicTimeoutRetainsOriginalSegmentThreshold(t *testing.T) {
	h := newCubicHarness(t, 8, 8*model.SEGMENT_SIZE, 65535, 0)
	for count := 0; count < 3; count++ {
		h.receive(model.FlagACK, 1)
	}
	h.runUntil(100 * time.Millisecond)
	h.receive(model.FlagACK, 513)
	h.runUntil(1100 * time.Millisecond)
	state := congestionState(t, h)
	if state.CongestionWindowBytes != 1024 || state.SlowStartThresholdBytes != 5376 ||
		state.FastRetransmissions != 1 {
		t.Fatalf("first RTO did not reduce the current 7680-byte flight by beta=0.7: %+v", state)
	}

	h.runUntil(1200 * time.Millisecond)
	h.receive(model.FlagACK, 769)
	h.runUntil(3200 * time.Millisecond)
	if current := congestionState(t, h); current.SlowStartThresholdBytes != 5376 ||
		current.CongestionWindowBytes != 1024 || h.transfer().DataRetransmissions != 3 {
		t.Fatalf("a partial ACK made a repeated timeout cut the threshold again: %+v", current)
	}
}

// ACKs received during a 100 ms workload pause do not grow CUBIC's unused window.
func TestTCPCubicDoesNotGrowDuringWorkloadGap(t *testing.T) {
	h := newCubicHarness(t, 4, 64*model.SEGMENT_SIZE, 2048, 100*time.Millisecond)
	h.runUntil(150 * time.Millisecond)
	h.receive(model.FlagACK, 1025)
	if current := congestionState(t, h); current.CongestionWindowBytes != 4096 {
		t.Fatalf("a locally limited ACK grew CUBIC: %+v", current)
	}
	h.runUntil(250 * time.Millisecond)
	h.receive(model.FlagACK, 2049)
	if current := congestionState(t, h); current.CongestionWindowBytes != 4096 {
		t.Fatalf("the next workload pause grew CUBIC: %+v", current)
	}
}

func newCubicHarness(
	t *testing.T,
	initialSegments, payloadBytes int,
	threshold uint64,
	gap time.Duration,
) *transferHarness {
	t.Helper()
	h := newRenoTransferHarness(t)
	config := model.DefaultCongestionConfig()
	config.Mode = model.CongestionCubic
	config.InitialWindowSegments = initialSegments
	config.InitialSlowStartThresholdBytes = threshold
	h.client.SetCongestionConfig(config)
	rto := model.DefaultRTOConfig()
	rto.Mode = model.RTOFixed
	h.client.SetRTOConfig(rto)
	h.client.SetIdleTimeout(0)
	h.client.SendCompleteStream(payloadBytes, gap)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(0)
	return h
}
