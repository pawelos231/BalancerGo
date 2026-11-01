package client_test

import (
	"testing"
	"time"

	"load-balancer/model"
)

// A lost SYN makes Tahoe start with one MSS and retain that initial window after idle.
func TestTCPTahoeSYNRetryAlsoControlsIdleRestart(t *testing.T) {
	h := newRenoTransferHarness(t)
	config := model.DefaultCongestionConfig()
	config.Mode = model.CongestionTahoe
	h.client.SetCongestionConfig(config)
	useFixedCongestionTestTimers(h)
	const gap = 1050 * time.Millisecond
	h.client.SendCompleteStream(8192, gap)
	h.client.EstablishConnection()

	// The original SYN times out at 1 s; acknowledging its retry starts a paced workload.
	h.runUntil(1250 * time.Millisecond)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	firstSendAt := h.engine.Now() + gap
	h.runUntil(firstSendAt + 100*time.Millisecond)
	h.receive(model.FlagACK, 1025)
	if state := congestionState(t, h); state.CongestionWindowBytes != 2048 {
		t.Fatalf("Tahoe did not grow from its reduced initial window: %+v", state)
	}

	h.runUntil(firstSendAt + gap)
	if state := congestionState(t, h); state.CongestionWindowBytes != 1024 || state.BytesInFlight != 1024 {
		t.Fatalf("idle restart forgot Tahoe's SYN loss: %+v", state)
	}
}

// A synchronous recovery ACK sees the fast retry count already updated and cancels its timer.
func TestTCPTahoeSynchronousRetryACKCompletesAndRetainsMetrics(t *testing.T) {
	h := newTahoeHarness(t, 4, 4*model.SEGMENT_SIZE)
	h.client.SetSender(func(packet model.Packet) {
		h.sent = append(h.sent, packet)
		if packet.Size == 0 {
			return
		}
		state := congestionState(t, h)
		if state.FastRetransmissions != 1 || state.CongestionWindowBytes != 1024 || state.Phase != "slowStart" {
			t.Fatalf("retry callback saw unfinished Tahoe state: %+v", state)
		}
		h.receive(model.FlagACK, 4097)
	})
	for count := 0; count < 3; count++ {
		h.receive(model.FlagACK, 1)
	}
	h.runUntil(3 * time.Second)
	state := congestionState(t, h)
	if !h.client.StreamCompleted() || state.FastRetransmissions != 1 || state.DuplicateACKs != 0 ||
		state.BytesInFlight != 0 || h.engine.Pending() != 0 || len(sentClientData(h.sent)) != 5 {
		t.Fatalf("synchronous Tahoe completion retained work or lost metrics: %+v", state)
	}
}

// Closing or shutting down after a fast retry clears the active flight and duplicate run.
func TestTCPTahoeCleanupRetainsFastRetryTotal(t *testing.T) {
	for _, closeWithFIN := range []bool{false, true} {
		name := "shutdown"
		if closeWithFIN {
			name = "close"
		}
		t.Run(name, func(t *testing.T) {
			h := newTahoeHarness(t, 8, 16*model.SEGMENT_SIZE)
			for count := 0; count < 3; count++ {
				h.receive(model.FlagACK, 1)
			}
			if closeWithFIN {
				h.client.Close()
			} else {
				h.client.Shutdown()
			}
			h.receive(model.FlagACK, 8193)
			h.runUntil(3 * time.Second)
			state := congestionState(t, h)
			if state.FastRetransmissions != 1 || state.DuplicateACKs != 0 || state.BytesInFlight != 0 ||
				state.CongestionWindowBytes != 1024 || h.engine.Pending() != 0 {
				t.Fatalf("cleanup changed Tahoe's retained state: %+v", state)
			}
		})
	}
}
