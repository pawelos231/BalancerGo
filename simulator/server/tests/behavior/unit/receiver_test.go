package server_test

import (
	"slices"
	"testing"

	"load-balancer/model"
)

func TestServerCumulativeACKAfterReorderedData(t *testing.T) {
	h := newByteServerHarness(t)
	h.server.SetReceiveBufferBytes(8)
	var received []int
	h.server.OnPayloadReceived = func(bytes int) { received = append(received, bytes) }

	h.receive(model.FlagSYN, 0, 0, 0)
	h.assertReplyWindow(model.FlagSYN|model.FlagACK, 0, 1, 8)
	h.receive(model.FlagACK, 1, 1, 0)

	// The later half is accepted, but its duplicate cannot advance ACK or payload.
	h.receive(model.FlagACK, 5, 1, 4)
	h.assertReplyWindow(model.FlagACK, 1, 1, 8)
	h.receive(model.FlagACK, 5, 1, 4)
	h.assertReplyWindow(model.FlagACK, 1, 1, 8)
	if got := h.server.GetState().ReceivedPayloadBytes; got != 4 {
		t.Fatalf("payload before filling gap = %d, want 4", got)
	}

	// Filling the gap acknowledges both halves, without counting the later one again.
	h.receive(model.FlagACK, 1, 1, 4)
	h.assertReplyWindow(model.FlagACK, 1, 9, 8)
	h.receive(model.FlagACK, 1, 1, 8)
	h.assertReplyWindow(model.FlagACK, 1, 9, 8)
	if got := h.server.GetState().ReceivedPayloadBytes; got != 8 || !slices.Equal(received, []int{4, 4}) {
		t.Fatalf("payload after filling gap = %d, increments=%v", got, received)
	}

	// Data outside the current window repeats ACK without consuming capacity.
	h.receive(model.FlagACK, 20, 1, 3)
	h.assertReplyWindow(model.FlagACK, 1, 9, 8)
}

func TestServerBuffersDataWhenHandshakeACKWasLost(t *testing.T) {
	h := newByteServerHarness(t)
	h.server.SetReceiveBufferBytes(16)
	h.receive(model.FlagSYN, 0, 0, 0)

	// The data ACK covers the backend SYN even though the first payload is missing.
	h.receive(model.FlagACK, 5, 1, 4)
	h.assertReplyWindow(model.FlagACK, 1, 1, 16)
	state := h.server.GetState()
	if state.ActiveConnections != 1 || state.HalfOpenConnections != 0 || state.ReceivedPayloadBytes != 4 {
		t.Fatalf("out-of-order data did not finish the handshake: %+v", state)
	}

	h.receive(model.FlagACK, 1, 1, 4)
	h.assertReplyWindow(model.FlagACK, 1, 9, 16)

	// A delayed SYN cannot discard the existing receiver state.
	h.receive(model.FlagSYN, 0, 0, 0)
	h.assertReplyWindow(model.FlagSYN|model.FlagACK, 0, 1, 16)
	h.receive(model.FlagACK, 9, 1, 2)
	h.assertReplyWindow(model.FlagACK, 1, 11, 16)
	if got := h.server.GetState().ReceivedPayloadBytes; got != 10 {
		t.Fatalf("payload after repeated SYN = %d, want 10", got)
	}
}

func TestServerReceiveBuffersBelongToIndividualConnections(t *testing.T) {
	h := newByteServerHarness(t)
	h.server.SetReceiveBufferBytes(8)
	firstFlow := h.flow
	secondFlow := firstFlow
	secondFlow.SrcPort++

	for _, flow := range []model.ClientKey{firstFlow, secondFlow} {
		h.flow = flow
		h.receive(model.FlagSYN, 0, 0, 0)
		h.receive(model.FlagACK, 1, 1, 0)
	}

	h.flow = firstFlow
	h.receive(model.FlagACK, 5, 1, 4)
	h.assertReplyWindow(model.FlagACK, 1, 1, 8)

	h.flow = secondFlow
	h.receive(model.FlagACK, 1, 1, 4)
	h.assertReplyWindow(model.FlagACK, 1, 5, 8)

	h.flow = firstFlow
	h.receive(model.FlagACK, 1, 1, 4)
	h.assertReplyWindow(model.FlagACK, 1, 9, 8)
	if got := h.server.GetState().ReceivedPayloadBytes; got != 12 {
		t.Fatalf("independent receive buffers changed total payload: %d", got)
	}
}
