package client_test

import (
	"slices"
	"testing"
	"time"

	"load-balancer/model"
)

func TestTCPFitsSegmentsInsideAdvertisedWindow(t *testing.T) {
	h := newTransferHarness(t)
	h.client.SetReceiveBufferBytes(13)
	h.client.SendCompleteStream(8, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receiveWindow(model.FlagSYN|model.FlagACK, 1, 3)
	h.runUntil(0)

	// A three-byte window produces 3 + 3 + 2 bytes, one segment at a time.
	for index, size := range []int{3, 3, 2} {
		data := sentClientData(h.sent)
		if len(data) != index+1 || data[index].Size != size {
			t.Fatalf("segment %d = %+v, want %d payload bytes", index, data, size)
		}
		nextByte := data[index].Seq + uint64(size)
		h.receiveWindow(model.FlagACK, nextByte, 3)
		h.runUntil(h.engine.Now())
	}
	if !h.client.StreamCompleted() || h.client.GetState().AcknowledgedPayloadBytes != 8 {
		t.Fatalf("small-window transfer did not finish: %+v", h.client.GetState())
	}

	for _, packet := range h.sent {
		if packet.ReceiveWindowBytes != 13 {
			t.Fatalf("client advertised %d bytes, want its configured 13", packet.ReceiveWindowBytes)
		}
	}
}

func TestTCPPartialACKRetransmitsOnlyUnconfirmedSuffix(t *testing.T) {
	h := newTransferHarness(t)
	var increments []int
	h.client.OnPayloadAcknowledged = func(bytes int) {
		increments = append(increments, bytes)
	}
	h.client.SendCompleteStream(20, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(0)

	// Five bytes arrived; the other fifteen remain the same outstanding segment.
	h.receive(model.FlagACK, 6)
	h.receive(model.FlagACK, 6)
	h.receive(model.FlagACK, 4)
	h.receive(model.FlagACK, 22)
	if got := h.client.GetState().AcknowledgedPayloadBytes; got != 5 {
		t.Fatalf("partial ACK confirmed %d bytes, want 5", got)
	}
	if len(sentClientData(h.sent)) != 1 || h.client.StreamCompleted() {
		t.Fatal("partial ACK released another segment or completed the transfer")
	}

	h.runUntil(model.SEGMENT_TIMEOUT)
	data := sentClientData(h.sent)
	if len(data) != 2 || data[1].Seq != 6 || data[1].Size != 15 {
		t.Fatalf("retry did not retain only [6, 21): %+v", data)
	}
	h.receive(model.FlagACK, 21)
	if !h.client.StreamCompleted() || h.client.GetState().AcknowledgedPayloadBytes != 20 {
		t.Fatalf("remaining suffix did not finish: %+v", h.client.GetState())
	}
	if !slices.Equal(increments, []int{5, 15}) {
		t.Fatalf("newly acknowledged increments = %v, want [5 15]", increments)
	}
}

func TestTCPZeroWindowWaitsForCurrentWindowUpdate(t *testing.T) {
	h := newTransferHarness(t)
	h.client.SendCompleteStream(8, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receiveWindow(model.FlagSYN|model.FlagACK, 1, 0)
	h.runUntil(250 * time.Millisecond)

	// A repeated SYN-ACK cannot replace the window learned from the first one.
	h.receiveWindow(model.FlagSYN|model.FlagACK, 1, 5)
	h.runUntil(h.engine.Now())
	if len(sentClientData(h.sent)) != 0 {
		t.Fatal("a zero window or duplicate handshake allowed new data")
	}
	h.receiveWindow(model.FlagACK, 1, 3)
	h.runUntil(h.engine.Now())
	data := sentClientData(h.sent)
	if len(data) != 1 || data[0].Size != 3 {
		t.Fatalf("window update did not release three bytes: %+v", data)
	}

	// After progress, an older ACK cannot reopen the newly closed window.
	h.receiveWindow(model.FlagACK, 4, 0)
	h.receiveWindow(model.FlagACK, 1, model.DefaultReceiveBufferBytes)
	h.runUntil(500 * time.Millisecond)
	if len(sentClientData(h.sent)) != 1 {
		t.Fatal("a stale ACK reopened the window")
	}
	h.receiveWindow(model.FlagACK, 4, 2)
	h.runUntil(h.engine.Now())
	data = sentClientData(h.sent)
	if len(data) != 2 || data[1].Seq != 4 || data[1].Size != 2 {
		t.Fatalf("current ACK did not reopen the window: %+v", data)
	}
	h.receiveWindow(model.FlagACK, 6, 3)
	h.runUntil(h.engine.Now())
	h.receiveWindow(model.FlagACK, 9, 3)
	if !h.client.StreamCompleted() {
		t.Fatal("transfer did not finish after the window reopened")
	}
}

func TestTCPRechecksWindowAfterSegmentGap(t *testing.T) {
	h := newTransferHarness(t)
	h.client.SendCompleteStream(4, 100*time.Millisecond)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receiveWindow(model.FlagSYN|model.FlagACK, 1, 4)
	h.receiveWindow(model.FlagACK, 1, 0)
	h.runUntil(200 * time.Millisecond)
	if len(sentClientData(h.sent)) != 0 {
		t.Fatal("the scheduled segment ignored a later closed window")
	}

	h.receiveWindow(model.FlagACK, 1, 4)
	h.runUntil(300 * time.Millisecond)
	data := sentClientData(h.sent)
	if len(data) != 1 || data[0].Size != 4 {
		t.Fatalf("reopened window did not resume the delayed segment: %+v", data)
	}
}
