package client_test

import (
	"slices"
	"testing"

	"load-balancer/model"
)

// A 2500-byte window sends 1024 + 1024 + 452 bytes before waiting for ACK.
func TestTCPSlidingWindowFillsAvailableCredit(t *testing.T) {
	h := newTransferHarness(t)
	h.client.SendCompleteStream(5000, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receiveWindow(model.FlagSYN|model.FlagACK, 1, 2500)
	h.runUntil(0)

	firstWindow := sentClientData(h.sent)
	if len(firstWindow) != 3 {
		t.Fatalf("first window contains %d segments, want 3", len(firstWindow))
	}
	for index, expectedSize := range []int{1024, 1024, 452} {
		if firstWindow[index].Size != expectedSize {
			t.Fatalf("segment %d has %d bytes, want %d", index, firstWindow[index].Size, expectedSize)
		}
	}
	if h.client.StreamCompleted() || h.count(model.FlagFIN) != 0 {
		t.Fatal("sending one window completed an unconfirmed stream")
	}

	// ACK=2501 frees the first window; the next 2500 bytes may leave immediately.
	h.receiveWindow(model.FlagACK, 2501, 2500)
	h.runUntil(0)
	allData := sentClientData(h.sent)
	if len(allData) != 6 || allData[3].Seq != 2501 {
		t.Fatalf("ACK did not release the next window: %+v", allData)
	}
	expectedSequence := uint64(1)
	for _, packet := range allData {
		if packet.Seq != expectedSequence {
			t.Fatalf("gap or repeated new data at Seq=%d, want %d", packet.Seq, expectedSequence)
		}
		expectedSequence += uint64(packet.Size)
	}
	if expectedSequence != 5001 {
		t.Fatalf("sent payload ends at %d, want 5001", expectedSequence)
	}
	if h.client.GetState().AcknowledgedPayloadBytes != 2500 || h.count(model.FlagFIN) != 0 {
		t.Fatal("the unconfirmed second window was counted as complete")
	}

	// Confirming the last byte completes exactly 5000 bytes and starts one FIN.
	h.receiveWindow(model.FlagACK, 5001, 2500)
	h.runUntil(0)
	if !h.client.StreamCompleted() || h.client.GetState().AcknowledgedPayloadBytes != 5000 {
		t.Fatalf("final window did not complete the stream: %+v", h.client.GetState())
	}
	if h.count(model.FlagFIN) != 1 {
		t.Fatalf("completed stream sent %d FINs, want 1", h.count(model.FlagFIN))
	}
}

// ACK=2501 removes two full segments and leaves [2501,3073) at the queue head.
func TestTCPCumulativeACKTrimsTheFirstRemainingSegment(t *testing.T) {
	h := newTransferHarness(t)
	var confirmedIncrements []int
	h.client.OnPayloadAcknowledged = func(newBytes int) {
		confirmedIncrements = append(confirmedIncrements, newBytes)
	}

	h.client.SendCompleteStream(4096, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receiveWindow(model.FlagSYN|model.FlagACK, 1, 4096)
	h.runUntil(0)
	if len(sentClientData(h.sent)) != 4 {
		t.Fatal("the initial four segments did not fit into the advertised window")
	}

	// Repeated, old and impossible ACKs cannot count the same progress again.
	h.receiveWindow(model.FlagACK, 2501, 4096)
	h.receiveWindow(model.FlagACK, 2501, 4096)
	h.receiveWindow(model.FlagACK, 2049, 4096)
	h.receiveWindow(model.FlagACK, 4098, 4096)
	if got := h.client.GetState().AcknowledgedPayloadBytes; got != 2500 {
		t.Fatalf("cumulative ACK confirmed %d bytes, want 2500", got)
	}

	// The timer retries just the missing 572-byte suffix, not the queued tail.
	h.runUntil(model.SEGMENT_TIMEOUT)
	data := sentClientData(h.sent)
	if len(data) != 5 || data[4].Seq != 2501 || data[4].Size != 572 {
		t.Fatalf("timeout did not retry the trimmed head: %+v", data)
	}

	h.receiveWindow(model.FlagACK, 4097, 4096)
	if !h.client.StreamCompleted() || !slices.Equal(confirmedIncrements, []int{2500, 1596}) {
		t.Fatalf("remaining ranges did not complete once: increments=%v state=%+v",
			confirmedIncrements, h.client.GetState())
	}
}

// An immediate ACK inside the send callback cannot duplicate or skip stream bytes.
func TestTCPSynchronousACKKeepsTheWholeStreamExact(t *testing.T) {
	h := newTransferHarness(t)
	h.client.SetSender(func(packet model.Packet) {
		h.sent = append(h.sent, packet)
		if packet.Size > 0 {
			// The sender must record this range before the callback confirms it.
			nextMissingByte := packet.Seq + uint64(packet.Size)
			h.receive(model.FlagACK, nextMissingByte)
		}
	})

	h.client.SendCompleteStream(2050, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(2 * model.SEGMENT_TIMEOUT)

	data := sentClientData(h.sent)
	if len(data) != 3 || data[0].Seq != 1 || data[1].Seq != 1025 ||
		data[2].Seq != 2049 || data[2].Size != 2 {
		t.Fatalf("synchronous ACK changed segment boundaries: %+v", data)
	}
	state := h.client.GetState()
	if !h.client.StreamCompleted() || state.AcknowledgedPayloadBytes != 2050 ||
		state.Transfer.DataRetransmissions != 0 || h.count(model.FlagFIN) != 1 {
		t.Fatalf("synchronous transfer did not finish exactly once: %+v", state)
	}
}
