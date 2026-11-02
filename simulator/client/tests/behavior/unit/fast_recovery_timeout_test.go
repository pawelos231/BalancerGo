package client_test

import (
	"testing"
	"time"

	"load-balancer/model"
)

// A fast retry at 900 ms moves the 1 s RTO deadline from 1 s to 1.9 s.
// Only that later timeout doubles RTO and returns cwnd to one segment.
func TestTCPFastRetransmitRestartsRTOWithoutBackoff(t *testing.T) {
	h := newFastRecoveryHarness(t, 4, 8192)
	h.runUntil(900 * time.Millisecond)
	for count := 0; count < 3; count++ {
		h.receive(model.FlagACK, 1)
	}
	h.runUntil(1900*time.Millisecond - time.Nanosecond)
	if h.transfer().DataRetransmissions != 1 || h.client.GetRTOState().CurrentRTO != time.Second {
		t.Fatal("the old deadline fired, or the fast retry backed off the timer")
	}

	h.runUntil(1900 * time.Millisecond)
	state := congestionState(t, h)
	if h.transfer().DataRetransmissions != 2 || h.client.GetRTOState().CurrentRTO != 2*time.Second ||
		state.Phase != "slowStart" || state.CongestionWindowBytes != 1024 || state.DuplicateACKs != 0 {
		t.Fatalf("RTO did not replace fast recovery: %+v", state)
	}
}

// Recovery sends one extra segment, making flight=9216 before the first actual RTO.
// That timeout uses current flight/2=4608, then late duplicates cannot reinflate cwnd=1024.
func TestTCPTimeoutUsesCurrentFlightAfterFastRecovery(t *testing.T) {
	h := newFastRecoveryHarness(t, 8, 16*model.SEGMENT_SIZE)
	h.runUntil(100 * time.Millisecond)
	for count := 0; count < 5; count++ {
		h.receive(model.FlagACK, 1)
		h.runUntil(h.engine.Now())
	}
	h.runUntil(1100 * time.Millisecond)
	state := congestionState(t, h)
	if state.SlowStartThresholdBytes != 4608 || state.CongestionWindowBytes != 1024 ||
		state.BytesInFlight != 9216 || state.FastRetransmissions != 1 {
		t.Fatalf("first actual timeout reused the fast-recovery threshold: %+v", state)
	}

	for count := 0; count < 10; count++ {
		h.receive(model.FlagACK, 1)
	}
	state = congestionState(t, h)
	if state.Phase != "slowStart" || state.CongestionWindowBytes != 1024 ||
		state.FastRetransmissions != 1 || h.transfer().DataRetransmissions != 2 {
		t.Fatalf("late duplicates undid timeout recovery: %+v", state)
	}
}

// ACK=1025 fills only the first hole; classic Reno exits recovery immediately.
// Duplicate ACKs for the second hole cannot repeatedly cut this same flight's threshold.
func TestTCPPartialACKLeavesSecondHoleForRTO(t *testing.T) {
	h := newFastRecoveryHarness(t, 8, 8*model.SEGMENT_SIZE)
	for count := 0; count < 3; count++ {
		h.receive(model.FlagACK, 1)
	}
	h.runUntil(100 * time.Millisecond)
	h.receive(model.FlagACK, 1025)
	state := congestionState(t, h)
	if state.Phase != "congestionAvoidance" || state.CongestionWindowBytes != 4096 ||
		state.BytesInFlight != 7168 || state.DuplicateACKs != 0 {
		t.Fatalf("partial ACK did not exit classic Reno cleanly: %+v", state)
	}

	for count := 0; count < 3; count++ {
		h.receive(model.FlagACK, 1025)
	}
	if state := congestionState(t, h); state.FastRetransmissions != 1 || state.SlowStartThresholdBytes != 4096 {
		t.Fatalf("old flight entered recovery and reduced the threshold again: %+v", state)
	}
	h.runUntil(1100 * time.Millisecond)
	data := sentClientData(h.sent)
	if len(data) != 10 || data[9].Seq != 1025 {
		t.Fatalf("RTO did not repair the remaining oldest hole: %+v", data)
	}
	h.receive(model.FlagACK, 8193)
	if !h.client.StreamCompleted() || h.client.GetState().AcknowledgedPayloadBytes != 8192 {
		t.Fatal("repairing the second gap failed to confirm unique payload exactly once")
	}
}

// With fast recovery disabled, three duplicate ACKs still wait for the 1 s RTO.
func TestTCPFastRetransmitCanBeDisabled(t *testing.T) {
	h := newRenoTransferHarness(t)
	useFixedCongestionTestTimers(h)
	config := model.DefaultCongestionConfig()
	config.FastRetransmit = false
	h.client.SetCongestionConfig(config)
	h.client.SendCompleteStream(8192, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(0)
	for count := 0; count < 3; count++ {
		h.receive(model.FlagACK, 1)
	}
	if len(sentClientData(h.sent)) != 4 || congestionState(t, h).FastRetransmissions != 0 {
		t.Fatal("disabled fast retransmit still retried early")
	}
	h.runUntil(time.Second)
	if len(sentClientData(h.sent)) != 5 || congestionState(t, h).CongestionWindowBytes != 1024 {
		t.Fatal("disabled fast retransmit changed the ordinary RTO fallback")
	}
}

// Graceful draining or forced shutdown retains the completed fast retry count.
func TestTCPFastRecoveryCleanupRetainsRetryTotal(t *testing.T) {
	for _, closeWithFIN := range []bool{false, true} {
		name := "shutdown"
		if closeWithFIN {
			name = "close"
		}
		t.Run(name, func(t *testing.T) {
			h := newFastRecoveryHarness(t, 4, 8192)
			for count := 0; count < 3; count++ {
				h.receive(model.FlagACK, 1)
			}
			if closeWithFIN {
				h.client.Close()
				if state := congestionState(t, h); state.Phase != "fastRecovery" || state.BytesInFlight != 4096 {
					t.Fatalf("Close abandoned active recovery: %+v", state)
				}
				for _, ack := range []uint64{4097, 6145, 8193} {
					h.receive(model.FlagACK, ack)
					h.runUntil(h.engine.Now())
				}
				h.receive(model.FlagFIN|model.FlagACK, 8194)
			} else {
				h.client.Shutdown()
			}
			h.runUntil(3 * time.Second)
			state := congestionState(t, h)
			wantData := 5
			if closeWithFIN {
				wantData = 9
			}
			if state.Phase == "fastRecovery" || state.DuplicateACKs != 0 ||
				state.BytesInFlight != 0 || state.FastRetransmissions != 1 ||
				len(sentClientData(h.sent)) != wantData || h.engine.Pending() != 0 {
				t.Fatalf("cleanup retained recovery work or lost history: %+v", state)
			}
		})
	}
}

// Once ACK passes the protected old flight, a later independent loss can recover again.
// This applies after both fast recovery and an ordinary retransmission timeout.
func TestTCPFastRecoveryAllowsALaterFlight(t *testing.T) {
	for _, afterTimeout := range []bool{false, true} {
		name := "after fast recovery"
		if afterTimeout {
			name = "after timeout"
		}
		t.Run(name, func(t *testing.T) {
			h := newFastRecoveryHarness(t, 8, 32*model.SEGMENT_SIZE)
			wantFastRetries := uint64(2)
			if afterTimeout {
				h.runUntil(time.Second)
				wantFastRetries = 1
			} else {
				for count := 0; count < 3; count++ {
					h.receive(model.FlagACK, 1)
				}
			}

			// Confirm all original eight segments, then build a new four-segment flight.
			h.receive(model.FlagACK, 8193)
			h.runUntil(h.engine.Now())
			missingByte := uint64(8193)
			if afterTimeout {
				// Slow start grows the reduced timeout window from two to four segments.
				for _, ack := range []uint64{9217, 10241} {
					h.receive(model.FlagACK, ack)
					h.runUntil(h.engine.Now())
					missingByte = ack
				}
			}
			for count := 0; count < 3; count++ {
				h.receive(model.FlagACK, missingByte)
			}
			data := sentClientData(h.sent)
			state := congestionState(t, h)
			if state.FastRetransmissions != wantFastRetries || state.Phase != "fastRecovery" ||
				data[len(data)-1].Seq != missingByte {
				t.Fatalf("old recovery guard blocked an independent later loss: %+v", state)
			}
		})
	}
}
