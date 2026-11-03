package client_test

import (
	"reflect"
	"testing"
	"time"

	"load-balancer/model"
)

// Eight outstanding segments become a one-MSS window after the third duplicate ACK.
// Further duplicates leave Tahoe there; either a partial or full new ACK grows it to two.
func TestTCPTahoeFastLossRestartsSlowStart(t *testing.T) {
	for _, test := range []struct {
		name string
		ack  uint64
	}{
		{name: "partial ACK", ack: 1025},
		{name: "full ACK", ack: 8193},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newTahoeHarness(t, 8, 16*model.SEGMENT_SIZE)
			for count := 0; count < 3; count++ {
				h.receive(model.FlagACK, 1)
			}
			state := congestionState(t, h)
			if state.CongestionWindowBytes != 1024 || state.SlowStartThresholdBytes != 4096 ||
				state.Phase != "slowStart" || state.FastRetransmissions != 1 || state.BytesInFlight != 8192 {
				t.Fatalf("third duplicate did not restart Tahoe: %+v", state)
			}
			data := sentClientData(h.sent)
			if len(data) != 9 || !reflect.DeepEqual(data[8], data[0]) || h.transfer().DataRetransmissions != 1 {
				t.Fatalf("fast loss did not retry the original head exactly once: %+v", data)
			}

			// Unlike Reno, more duplicates cannot inflate Tahoe or emit new payload.
			for count := 0; count < 8; count++ {
				h.receive(model.FlagACK, 1)
			}
			h.runUntil(0)
			if current := congestionState(t, h); current.CongestionWindowBytes != 1024 ||
				current.FastRetransmissions != 1 || len(sentClientData(h.sent)) != 9 {
				t.Fatalf("later duplicates changed Tahoe's restart: %+v", current)
			}

			h.receive(model.FlagACK, test.ack)
			state = congestionState(t, h)
			if state.CongestionWindowBytes != 2048 || state.Phase != "slowStart" || state.DuplicateACKs != 0 {
				t.Fatalf("new ACK did not grow Tahoe slow start: %+v", state)
			}
		})
	}
}

// A partial ACK does not clear the old-flight guard; ACK=8193 clears it for later data.
func TestTCPTahoeProtectsOldFlightAndAllowsNextFastLoss(t *testing.T) {
	h := newTahoeHarness(t, 8, 16*model.SEGMENT_SIZE)
	for count := 0; count < 3; count++ {
		h.receive(model.FlagACK, 1)
	}
	h.receive(model.FlagACK, 1025)
	for count := 0; count < 3; count++ {
		h.receive(model.FlagACK, 1025)
	}
	if state := congestionState(t, h); state.FastRetransmissions != 1 ||
		state.SlowStartThresholdBytes != 4096 || state.CongestionWindowBytes != 2048 {
		t.Fatalf("partial progress retriggered the same loss: %+v", state)
	}

	// Confirming the entire old flight permits a later, independent fast retransmission.
	h.receive(model.FlagACK, 8193)
	h.runUntil(0)
	for count := 0; count < 3; count++ {
		h.receive(model.FlagACK, 8193)
	}
	state := congestionState(t, h)
	data := sentClientData(h.sent)
	if state.FastRetransmissions != 2 || state.CongestionWindowBytes != 1024 ||
		data[len(data)-1].Seq != 8193 {
		t.Fatalf("next flight could not report its own loss: %+v", state)
	}
}

// Fast retransmit does not consume the first RTO's threshold reduction.
// A later partial ACK trims [513,1025) again, but the second RTO keeps that threshold.
func TestTCPTahoeTimeoutAfterFastLossKeepsOriginalSegmentIdentity(t *testing.T) {
	h := newTahoeHarness(t, 8, 8*model.SEGMENT_SIZE)
	for count := 0; count < 3; count++ {
		h.receive(model.FlagACK, 1)
	}
	h.runUntil(100 * time.Millisecond)
	h.receive(model.FlagACK, 513)
	h.runUntil(1100 * time.Millisecond)
	if state := congestionState(t, h); state.CongestionWindowBytes != 1024 ||
		state.SlowStartThresholdBytes != 3840 || state.FastRetransmissions != 1 {
		t.Fatalf("first RTO did not use the current 7680-byte flight: %+v", state)
	}

	h.runUntil(1200 * time.Millisecond)
	h.receive(model.FlagACK, 769)
	h.runUntil(3200 * time.Millisecond)
	state := congestionState(t, h)
	if state.SlowStartThresholdBytes != 3840 || state.CongestionWindowBytes != 1024 ||
		h.transfer().DataRetransmissions != 3 {
		t.Fatalf("repeated RTO cut the original segment's threshold again: %+v", state)
	}
}

// newTahoeHarness starts a fixed-RTO connection without changing the shared transport harness.
func newTahoeHarness(t *testing.T, initialSegments, payloadBytes int) *transferHarness {
	t.Helper()
	h := newRenoTransferHarness(t)
	config := model.DefaultCongestionConfig()
	config.Mode = model.CongestionTahoe
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
