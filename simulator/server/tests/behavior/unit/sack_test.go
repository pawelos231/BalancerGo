package server_test

import (
	"slices"
	"testing"

	"load-balancer/model"
)

// Both endpoint settings must allow SACK; a later SYN cannot renegotiate it.
func TestServerSACKNegotiation(t *testing.T) {
	cases := []struct {
		name    string
		enabled bool
		offered bool
	}{
		{"both disabled", false, false},
		{"backend disabled", false, true},
		{"client did not offer", true, false},
		{"both enabled", true, true},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			h := newByteServerHarness(t)
			h.server.SetSACKEnabled(test.enabled)
			wantSACK := test.enabled && test.offered

			syn := model.Packet{Key: h.flow, Flag: model.FlagSYN, SACKPermitted: test.offered}
			h.server.HandlePacket(syn)
			if got := h.replies[len(h.replies)-1].SACKPermitted; got != wantSACK {
				t.Fatalf("SYN-ACK permission = %v, want %v", got, wantSACK)
			}

			// A retransmitted SYN must retain the first SYN's agreement.
			syn.SACKPermitted = !syn.SACKPermitted
			h.server.HandlePacket(syn)
			if got := h.replies[len(h.replies)-1].SACKPermitted; got != wantSACK {
				t.Fatalf("repeated SYN changed SACK permission to %v", got)
			}

			h.receive(model.FlagACK, 1, 1, 0)
			h.receive(model.FlagACK, 5, 1, 4)
			got := h.replies[len(h.replies)-1]
			if got.Ack != 1 || (len(got.SACKBlocks) != 0) != wantSACK {
				t.Fatalf("out-of-order report = %+v, negotiated=%v", got, wantSACK)
			}
		})
	}
}

// Filling a gap merges buffered ranges; old in-flight ACKs retain their original report.
func TestServerSACKReportsMergedRanges(t *testing.T) {
	h := newSACKServerHarness(t)

	h.receive(model.FlagACK, 5, 1, 4)
	assertSACKReport(t, h, 1, []model.SACKBlock{{Start: 5, End: 9}})
	firstACK := h.replies[len(h.replies)-1]

	// The most recently touched range comes first, even though its sequence is larger.
	h.receive(model.FlagACK, 13, 1, 4)
	assertSACKReport(t, h, 1, []model.SACKBlock{
		{Start: 13, End: 17}, {Start: 5, End: 9},
	})

	h.receive(model.FlagACK, 9, 1, 4)
	assertSACKReport(t, h, 1, []model.SACKBlock{{Start: 5, End: 17}})
	if !slices.Equal(firstACK.SACKBlocks, []model.SACKBlock{{Start: 5, End: 9}}) {
		t.Fatalf("an earlier ACK was mutated: %+v", firstACK)
	}

	// Duplicate bytes repeat the report without increasing received payload.
	h.receive(model.FlagACK, 5, 1, 4)
	assertSACKReport(t, h, 1, []model.SACKBlock{{Start: 5, End: 17}})
	if got := h.server.GetState().ReceivedPayloadBytes; got != 12 {
		t.Fatalf("payload with a duplicate = %d, want 12", got)
	}

	// Completing the prefix consumes all buffered data and removes the SACK blocks.
	h.receive(model.FlagACK, 1, 1, 4)
	assertSACKReport(t, h, 17, nil)
	if got := h.server.GetState().ReceivedPayloadBytes; got != 16 {
		t.Fatalf("payload after gap repair = %d, want 16", got)
	}
}

// A report fits four blocks and repeats the most recent ones when more gaps exist.
func TestServerSACKBlockLimitAndRecentOrder(t *testing.T) {
	h := newSACKServerHarness(t)
	for _, sequence := range []uint64{5, 9, 13, 17, 21} {
		h.receive(model.FlagACK, sequence, 1, 1)
	}
	assertSACKReport(t, h, 1, []model.SACKBlock{
		{Start: 21, End: 22}, {Start: 17, End: 18},
		{Start: 13, End: 14}, {Start: 9, End: 10},
	})

	// Even a duplicate must put its own existing buffered block first.
	h.receive(model.FlagACK, 5, 1, 1)
	assertSACKReport(t, h, 1, []model.SACKBlock{
		{Start: 5, End: 6}, {Start: 21, End: 22},
		{Start: 17, End: 18}, {Start: 13, End: 14},
	})

	// An out-of-window segment adds no block and preserves the recent reports.
	h.receive(model.FlagACK, 1000, 1, 1)
	assertSACKReport(t, h, 1, []model.SACKBlock{
		{Start: 5, End: 6}, {Start: 21, End: 22},
		{Start: 17, End: 18}, {Start: 13, End: 14},
	})
}

func newSACKServerHarness(t *testing.T) *byteServerHarness {
	t.Helper()
	h := newByteServerHarness(t)
	h.server.SetReceiveBufferBytes(64)
	h.server.SetSACKEnabled(true)
	h.server.HandlePacket(model.Packet{
		Key: h.flow, Flag: model.FlagSYN, SACKPermitted: true,
	})
	h.receive(model.FlagACK, 1, 1, 0)
	return h
}

func assertSACKReport(t *testing.T, h *byteServerHarness, ack uint64, blocks []model.SACKBlock) {
	t.Helper()
	got := h.replies[len(h.replies)-1]
	if got.Ack != ack || !slices.Equal(got.SACKBlocks, blocks) {
		t.Fatalf("ACK=%d blocks=%v; want ACK=%d blocks=%v", got.Ack, got.SACKBlocks, ack, blocks)
	}
}
