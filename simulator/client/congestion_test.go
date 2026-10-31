package client_test

import (
	"testing"
	"time"

	"load-balancer/model"
)

// Default Reno sends four 1024-byte segments before waiting for a data ACK.
// SYN-ACK and window-only updates do not increase that initial 4096-byte window.
func TestTCPRenoStartsWithFourSegments(t *testing.T) {
	h := newRenoTransferHarness(t)
	wantInitial := model.CongestionState{
		Mode:                    model.CongestionReno,
		CongestionWindowBytes:   4096,
		SlowStartThresholdBytes: 65535,
		Phase:                   "slowStart",
	}
	if state := congestionState(t, h); state != wantInitial {
		t.Fatalf("initial Reno state = %+v, want %+v", state, wantInitial)
	}

	h.client.SendCompleteStream(8192, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	if state := congestionState(t, h); state != wantInitial {
		t.Fatalf("SYN-ACK grew the data window: %+v", state)
	}
	h.runUntil(0)
	if got := len(sentClientData(h.sent)); got != 4 {
		t.Fatalf("initial window sent %d segments, want 4", got)
	}

	// Duplicate, stale, future and window-only ACKs confirm no new payload.
	h.receive(model.FlagACK, 1)
	h.receive(model.FlagACK, 0)
	h.receive(model.FlagACK, 4098)
	h.receiveWindow(model.FlagACK, 1, 5000)
	h.runUntil(0)
	wantInitial.BytesInFlight = 4096
	if state := congestionState(t, h); state != wantInitial {
		t.Fatalf("ACK without progress grew the window: %+v", state)
	}
}

// One lost SYN reduces the starting window from four segments to one.
// The delayed SYN-ACK confirms only SYN, so payload still starts with 1024 bytes.
func TestTCPRenoSYNRetryStartsWithOneSegment(t *testing.T) {
	h := newRenoTransferHarness(t)
	h.client.SendCompleteStream(8192, 0)
	h.client.EstablishConnection()
	h.runUntil(1250 * time.Millisecond)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(h.engine.Now())

	state := congestionState(t, h)
	if h.count(model.FlagSYN) != 2 || len(sentClientData(h.sent)) != 1 {
		t.Fatalf("payload after a SYN retry = %+v", h.sent)
	}
	if state.CongestionWindowBytes != 1024 || state.BytesInFlight != 1024 ||
		state.SlowStartThresholdBytes != 65535 || state.Phase != "slowStart" {
		t.Fatalf("SYN retry did not keep a one-segment initial window: %+v", state)
	}
}

// A 2500-byte rwnd clips the third segment to 452 bytes; a large rwnd hits cwnd=4096.
// ACKing 100 bytes grants only 100 rwnd bytes, or 200 cwnd bytes after slow-start growth.
func TestTCPRenoUsesBothWindowsAndPartialCredit(t *testing.T) {
	for _, test := range []struct {
		name            string
		receiveWindow   uint16
		initialSizes    []int
		nextSize        int
		finalFlightSize uint64
	}{
		{"receive window limits", 2500, []int{1024, 1024, 452}, 100, 2500},
		{"congestion window limits", 5000, []int{1024, 1024, 1024, 1024}, 200, 4196},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newRenoTransferHarness(t)
			h.client.SendCompleteStream(10000, 0)
			h.client.EstablishConnection()
			h.runUntil(0)
			h.receiveWindow(model.FlagSYN|model.FlagACK, 1, test.receiveWindow)
			h.runUntil(0)

			data := sentClientData(h.sent)
			if len(data) != len(test.initialSizes) {
				t.Fatalf("initial payload = %+v, want sizes %v", data, test.initialSizes)
			}
			for index, size := range test.initialSizes {
				if data[index].Size != size {
					t.Fatalf("segment %d size = %d, want %d", index, data[index].Size, size)
				}
			}

			// Partial ACK=101 confirms 100 bytes and grows cwnd by those same 100 bytes.
			h.receiveWindow(model.FlagACK, 101, test.receiveWindow)
			h.runUntil(0)
			data = sentClientData(h.sent)
			if len(data) != len(test.initialSizes)+1 || data[len(data)-1].Size != test.nextSize {
				t.Fatalf("partial ACK released incorrect credit: %+v", data)
			}
			state := congestionState(t, h)
			if state.CongestionWindowBytes != 4196 || state.BytesInFlight != test.finalFlightSize {
				t.Fatalf("partial ACK window = %+v, want flight=%d", state, test.finalFlightSize)
			}
		})
	}
}

// A cumulative ACK of 2048 bytes grows slow start by only one MSS, from 4096 to 5120.
// A later ACK of 10 bytes adds only 10, rather than another full segment.
func TestTCPRenoSlowStartCountsNewBytesOnce(t *testing.T) {
	h := newRenoTransferHarness(t)
	h.client.SendCompleteStream(10000, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(0)

	h.receive(model.FlagACK, 2049)
	if state := congestionState(t, h); state.CongestionWindowBytes != 5120 {
		t.Fatalf("cumulative ACK grew slow start by more than one MSS: %+v", state)
	}
	h.runUntil(0)
	h.receive(model.FlagACK, 2059)
	want := congestionState(t, h)
	if want.CongestionWindowBytes != 5130 {
		t.Fatalf("10-byte ACK did not add exactly 10 bytes: %+v", want)
	}

	// Repeating either ACK or merely changing rwnd cannot count its bytes again.
	h.receive(model.FlagACK, 2059)
	h.receive(model.FlagACK, 2049)
	h.receiveWindow(model.FlagACK, 2059, 60000)
	if state := congestionState(t, h); state != want {
		t.Fatalf("repeated progress changed the congestion state: %+v", state)
	}
}

// cwnd=ssthresh=2048 starts in congestion avoidance and needs 2048 ACKed bytes to grow.
// A later cumulative ACK of 3072 bytes adds only one MSS, reaching 4096.
func TestTCPRenoCongestionAvoidanceUsesByteCounter(t *testing.T) {
	h := newRenoTransferHarness(t)
	config := model.DefaultCongestionConfig()
	config.InitialWindowSegments = 2
	config.InitialSlowStartThresholdBytes = 2048
	h.client.SetCongestionConfig(config)
	h.client.SendCompleteStream(10000, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(0)

	for _, step := range []struct {
		ack    uint64
		window uint64
	}{
		{ack: 1025, window: 2048},
		{ack: 2049, window: 3072},
		{ack: 5121, window: 4096},
	} {
		h.receive(model.FlagACK, step.ack)
		state := congestionState(t, h)
		if state.CongestionWindowBytes != step.window || state.Phase != "congestionAvoidance" {
			t.Fatalf("ACK=%d gave %+v, want CA window=%d", step.ack, state, step.window)
		}
		h.runUntil(0)
	}
}

// Slow start crosses ssthresh=2500 by growing from 2048 to 3072 on ACK=1025.
// That ACK cannot also count toward the next 3072-byte congestion-avoidance round.
func TestTCPRenoThresholdCrossingDoesNotCountAnACKTwice(t *testing.T) {
	h := newRenoTransferHarness(t)
	config := model.DefaultCongestionConfig()
	config.InitialWindowSegments = 2
	config.InitialSlowStartThresholdBytes = 2500
	h.client.SetCongestionConfig(config)
	h.client.SendCompleteStream(10000, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(0)

	for _, ack := range []uint64{1025, 2049, 3073, 4097} {
		h.receive(model.FlagACK, ack)
		wantWindow := uint64(3072)
		if ack == 4097 {
			wantWindow = 4096
		}
		state := congestionState(t, h)
		if state.CongestionWindowBytes != wantWindow || state.Phase != "congestionAvoidance" {
			t.Fatalf("ACK=%d counted the crossing twice: %+v, want window=%d", ack, state, wantWindow)
		}
		h.runUntil(0)
	}
}

// Both public views must report the same live window and unconfirmed payload bytes.
func congestionState(t *testing.T, h *transferHarness) model.CongestionState {
	t.Helper()
	state := h.client.GetCongestionState()
	if snapshot := h.client.GetState().Congestion; snapshot != state {
		t.Fatalf("congestion snapshot = %+v, getter = %+v", snapshot, state)
	}
	return state
}
