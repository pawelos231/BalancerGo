package client_test

import (
	"testing"
	"time"

	"load-balancer/model"
)

// Four sends at 200/400/600/800 ms must leave the first timeout at 1200 ms.
func TestTCPTimerFollowsOldestPendingData(t *testing.T) {
	h := newTransferHarness(t)
	h.client.SendCompleteStream(4096, 200*time.Millisecond)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receiveWindow(model.FlagSYN|model.FlagACK, 1, 4096)
	h.runUntil(1100 * time.Millisecond)
	if len(sentClientData(h.sent)) != 4 {
		t.Fatal("the window did not contain four original transmissions")
	}

	// Neither a duplicate ACK nor a changed window without ACK progress buys time.
	h.receiveWindow(model.FlagACK, 1, 4096)
	h.receiveWindow(model.FlagACK, 1, 5000)
	h.runUntil(1200*time.Millisecond - time.Nanosecond)
	if len(sentClientData(h.sent)) != 4 {
		t.Fatal("the oldest range was retransmitted before its timeout")
	}
	h.runUntil(1200 * time.Millisecond)
	data := sentClientData(h.sent)
	if len(data) != 5 || data[4] != data[0] {
		t.Fatalf("later sends or ACKs moved the original timeout: %+v", data)
	}

	// New progress at 1250 ms restarts the timer for the second pending segment.
	h.runUntil(1250 * time.Millisecond)
	h.receiveWindow(model.FlagACK, 1025, 4096)
	if h.client.GetState().RetryData != 0 {
		t.Fatal("ACK progress did not reset the current retry budget")
	}
	h.runUntil(2250*time.Millisecond - time.Nanosecond)
	if len(sentClientData(h.sent)) != 5 {
		t.Fatal("the next pending range kept the previous timeout")
	}
	h.runUntil(2250 * time.Millisecond)
	data = sentClientData(h.sent)
	if len(data) != 6 || data[5].Seq != 1025 || data[5].Size != 1024 {
		t.Fatalf("the new timer did not retry only the second range: %+v", data)
	}
	if state := h.transfer(); state.DataRetransmissions != 2 || h.client.GetState().RetryData != 1 {
		t.Fatalf("retry totals did not survive ACK progress: %+v", state)
	}
}

// ACK at 150 ms must not postpone a send due at 200 ms to 250 ms.
func TestTCPPacketGapUsesNewSendTimesInsteadOfACKTimes(t *testing.T) {
	h := newTransferHarness(t)
	h.client.SendCompleteStream(3072, 100*time.Millisecond)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receiveWindow(model.FlagSYN|model.FlagACK, 1, 2048)
	h.runUntil(150 * time.Millisecond)
	if len(sentClientData(h.sent)) != 1 {
		t.Fatal("the first segment did not wait for its initial 100 ms gap")
	}
	h.receiveWindow(model.FlagACK, 1025, 2048)

	h.runUntil(200*time.Millisecond - time.Nanosecond)
	if len(sentClientData(h.sent)) != 1 {
		t.Fatal("the next segment ignored the gap between new transmissions")
	}
	h.runUntil(200 * time.Millisecond)
	if len(sentClientData(h.sent)) != 2 {
		t.Fatal("ACK arrival restarted the segment gap")
	}

	// A closed window blocks the next send, but reopening after its due time resumes now.
	h.runUntil(250 * time.Millisecond)
	h.receiveWindow(model.FlagACK, 2049, 0)
	h.runUntil(350 * time.Millisecond)
	if len(sentClientData(h.sent)) != 2 {
		t.Fatal("the scheduled third segment ignored the closed window")
	}
	h.receiveWindow(model.FlagACK, 2049, 1024)
	h.runUntil(350 * time.Millisecond)
	data := sentClientData(h.sent)
	if len(data) != 3 || data[2].Seq != 2049 {
		t.Fatalf("reopening added an unnecessary fresh gap: %+v", data)
	}
}

// Handshake at 100 ms and a stream requested at 400 ms make its first send due at 500 ms.
func TestTCPFirstSendWaitsForStreamRequestedAfterHandshake(t *testing.T) {
	h := newTransferHarness(t)
	h.client.EstablishConnection()
	h.runUntil(100 * time.Millisecond)
	h.receiveWindow(model.FlagSYN|model.FlagACK, 1, 4096)

	// The connection exists already, but payload cannot start before it is requested.
	h.runUntil(400 * time.Millisecond)
	if len(sentClientData(h.sent)) != 0 {
		t.Fatal("the connection sent payload before a stream was requested")
	}
	h.client.SendCompleteStream(1024, 100*time.Millisecond)

	// Repeated control packets at 450 ms must not move the initial deadline to 550 ms.
	h.runUntil(450 * time.Millisecond)
	h.receiveWindow(model.FlagSYN|model.FlagACK, 1, 4096)
	h.receiveWindow(model.FlagACK, 1, 4096)
	h.runUntil(500*time.Millisecond - time.Nanosecond)
	if len(sentClientData(h.sent)) != 0 {
		t.Fatal("the initial gap did not start when the stream became ready")
	}

	h.runUntil(500 * time.Millisecond)
	data := sentClientData(h.sent)
	if len(data) != 1 || data[0].Seq != 1 || data[0].Size != 1024 {
		t.Fatalf("the ready stream did not send its first segment at 500 ms: %+v", data)
	}
}

// With zero gap, an already queued observer runs between the first and second sends.
func TestTCPZeroGapYieldsBetweenNewSegments(t *testing.T) {
	h := newTransferHarness(t)
	h.client.SendCompleteStream(4096, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receiveWindow(model.FlagSYN|model.FlagACK, 1, 4096)

	observedDataPackets := -1
	h.engine.ScheduleAt(0, func() {
		observedDataPackets = len(sentClientData(h.sent))
	})
	h.runUntil(0)

	if observedDataPackets != 1 || len(sentClientData(h.sent)) != 4 {
		t.Fatalf("zero-gap sends did not yield: observer=%d, final=%d",
			observedDataPackets, len(sentClientData(h.sent)))
	}
}

// Closing after one send cancels the next send and retransmission, even with queued data.
func TestTCPStopCancelsPendingDataAndFutureSends(t *testing.T) {
	for _, manualClose := range []bool{false, true} {
		name := "shutdown"
		if manualClose {
			name = "manual close"
		}
		t.Run(name, func(t *testing.T) {
			h := newTransferHarness(t)
			h.client.SendCompleteStream(4096, 100*time.Millisecond)
			h.client.EstablishConnection()
			h.runUntil(0)
			h.receive(model.FlagSYN|model.FlagACK, 1)
			h.runUntil(150 * time.Millisecond)

			if manualClose {
				h.client.Close()
			} else {
				h.client.Shutdown()
			}
			h.runUntil(5 * time.Second)

			if len(sentClientData(h.sent)) != 1 || h.client.StreamCompleted() {
				t.Fatalf("stopped transfer kept sending data: %+v", h.sent)
			}
			expectedFINs := 0
			if manualClose {
				expectedFINs = 1
			}
			if h.count(model.FlagFIN) != expectedFINs {
				t.Fatalf("stop sent %d FINs, want %d", h.count(model.FlagFIN), expectedFINs)
			}
		})
	}
}
