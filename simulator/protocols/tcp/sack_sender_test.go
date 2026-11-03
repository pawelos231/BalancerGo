package tcp

import (
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
)

type sackHarness struct {
	t      *testing.T
	engine *engine.Engine
	client *Sender
	sent   []model.Packet
}

// Keep a fixed one-second RTO so ACK processing can be checked before any timeout.
func newSACKHarness(t *testing.T, offer, accept bool, payload int) *sackHarness {
	t.Helper()
	e := engine.New(5 * time.Second)
	c := newTestSender(e)
	c.SetSACKEnabled(offer)
	congestion := model.DefaultCongestionConfig()
	congestion.InitialWindowSegments = 8
	c.SetCongestionConfig(congestion)
	rto := model.DefaultRTOConfig()
	rto.Mode = model.RTOFixed
	c.SetRTOConfig(rto)
	h := &sackHarness{t: t, engine: e, client: c}
	c.SetSender(func(packet model.Packet) { h.sent = append(h.sent, packet) })
	t.Cleanup(c.Shutdown)

	c.SetSource(newTestPayloadSource(e, payload, 0))
	c.EstablishConnection()
	h.runUntil(0)
	c.HandlePacket(model.Packet{
		Key: c.key, Flag: model.FlagSYN | model.FlagACK, Ack: 1,
		ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
		SACKPermitted:      accept,
	})
	h.runUntil(0)
	return h
}

func (h *sackHarness) runUntil(until time.Duration) {
	h.t.Helper()
	if err := h.engine.RunUntil(until); err != nil {
		h.t.Fatal(err)
	}
}

// Reply with the cumulative ACK and any extra ranges retained by the backend.
func (h *sackHarness) acknowledge(ack uint64, blocks ...model.SACKBlock) {
	h.t.Helper()
	h.client.HandlePacket(model.Packet{
		Key: h.client.key, Flag: model.FlagACK, Seq: 1, Ack: ack,
		ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
		SACKBlocks:         blocks,
	})
}

// Both handshake advertisements are needed; a later SYN-ACK cannot change the agreement.
func TestSACKSenderNegotiationIsSticky(t *testing.T) {
	for _, offer := range []bool{false, true} {
		for _, accept := range []bool{false, true} {
			h := newSACKHarness(t, offer, accept, 8192)
			want := offer && accept
			if h.sent[0].SACKPermitted != offer || h.client.sack.negotiated != want {
				t.Fatalf("offer=%v accept=%v produced wrong negotiation", offer, accept)
			}
			h.client.HandlePacket(model.Packet{
				Key: h.client.key, Flag: model.FlagSYN | model.FlagACK, Ack: 1,
				ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
				SACKPermitted:      !accept,
			})
			if h.client.sack.negotiated != want {
				t.Fatal("a duplicate SYN-ACK changed the negotiated profile")
			}
		}
	}
}

// Repeating one report is not three new arrivals, and SACK alone confirms no payload.
func TestSACKRepeatedReportsDoNotInventProgressOrLoss(t *testing.T) {
	h := newSACKHarness(t, true, true, 8192)
	for count := 0; count < 4; count++ {
		h.acknowledge(1, model.SACKBlock{Start: 1025, End: 2049})
	}
	if h.client.sack.active || h.client.transfer.dataRetransmissions != 0 {
		t.Fatal("one report repeated four times triggered recovery")
	}
	if h.client.acknowledgedPayloadBytes != 0 || len(h.client.pendingSegments.segments) != 8 {
		t.Fatal("SACK removed data before its cumulative ACK")
	}
	if h.client.sack.duplicateACKs != 1 {
		t.Fatalf("new SACK feedback count = %d, want 1", h.client.sack.duplicateACKs)
	}
}

// Three ACKs each add a byte to one block; the total is below the byte-loss threshold.
// They still represent three new reports and can trigger RFC 6675 fast retransmit.
func TestSACKThreeSmallReportsCanIdentifyLoss(t *testing.T) {
	h := newSACKHarness(t, true, true, 8192)
	for end := uint64(1026); end <= 1028; end++ {
		h.acknowledge(1, model.SACKBlock{Start: 1025, End: end})
	}
	if !h.client.sack.active || h.client.congestion.loss.fastRetransmissions == 0 {
		t.Fatal("three new small SACK reports did not trigger recovery")
	}
}

// After repairing 2 and 4, recovery can send new data only within its reduced cwnd.
// A 512-byte remainder of that window does not open another RFC 6675 send slot.
func TestSACKNewDataSharesTheRecoveryPipeBudget(t *testing.T) {
	h := newSACKHarness(t, true, true, 16*1024)
	h.acknowledge(1025,
		model.SACKBlock{Start: 2049, End: 3073},
		model.SACKBlock{Start: 4097, End: 8193},
	)
	h.runUntil(0)

	window := h.client.congestion.policy.State().WindowBytes
	if h.client.sack.pipeBytes > window || h.client.nextSendSeq != 9217 {
		t.Fatalf("new data exceeded its recovery budget: pipe=%d, cwnd=%d, next=%d",
			h.client.sack.pipeBytes, window, h.client.nextSendSeq)
	}
	if h.client.nextPayloadSize() != 0 {
		t.Fatal("less than one MSS of room opened another recovery send slot")
	}
}

// A peer rejecting SACK keeps classic Reno, even if later ACKs carry unsolicited reports.
func TestSACKUnnegotiatedReportsAreIgnored(t *testing.T) {
	h := newSACKHarness(t, true, false, 8192)
	h.acknowledge(1, model.SACKBlock{Start: 1025, End: 8193})
	if h.client.sack.board.RangeCount() != 0 || h.client.transfer.dataRetransmissions != 0 {
		t.Fatal("unnegotiated blocks influenced loss recovery")
	}
}

// Segment 2 is only half missing; the retransmission must not resend its reported suffix.
func TestSACKRetriesOnlyTheMissingPartOfASegment(t *testing.T) {
	h := newSACKHarness(t, true, true, 8192)
	h.acknowledge(1025, model.SACKBlock{Start: 1537, End: 8193})
	retry := h.sent[len(h.sent)-1]
	if retry.Seq != 1025 || retry.Size != 512 || !h.client.sack.active {
		t.Fatalf("expected only [1025,1537), got %+v", retry)
	}
	if h.client.acknowledgedPayloadBytes != 1024 || h.client.rttProbe.active {
		t.Fatal("selective retry corrupted payload accounting or Karn's rule")
	}

	// ACK=8193 finally confirms everything, including the earlier SACK reports.
	h.acknowledge(8193)
	if h.client.sack.active || h.client.sack.board.RangeCount() != 0 ||
		h.client.acknowledgedPayloadBytes != 8192 {
		t.Fatal("full ACK did not finish recovery exactly once")
	}
}

// Reports may be unaligned or beyond sent data; invalid bounds must not create retries.
func TestSACKSenderIgnoresUnsentRanges(t *testing.T) {
	h := newSACKHarness(t, true, true, 8192)
	h.acknowledge(1, model.SACKBlock{Start: 1025, End: 10000})
	if h.client.sack.active || h.client.sack.board.RangeCount() != 0 {
		t.Fatal("a report beyond sent data was accepted")
	}
}

// A lost retransmission reaches RTO; old reports are cleared before the next retry.
// Fresh feedback cannot start another fast episode inside that timed-out flight.
func TestSACKTimeoutClearsReportsAndKeepsTheRecoveryBoundary(t *testing.T) {
	h := newSACKHarness(t, true, true, 8192)
	h.acknowledge(1, model.SACKBlock{Start: 1025, End: 8193})
	h.runUntil(time.Second)
	if h.client.sack.active || h.client.sack.board.RangeCount() != 0 ||
		h.client.sack.recoveryEndSeq != 8193 {
		t.Fatal("timeout retained old SACK recovery state")
	}
	if h.client.rto.state.CurrentRTO != 2*time.Second ||
		h.client.GetCongestionState().CongestionWindowBytes != 1024 {
		t.Fatal("SACK bypassed timeout backoff or slow start")
	}
	retries := h.client.transfer.dataRetransmissions
	h.acknowledge(1, model.SACKBlock{Start: 1025, End: 8193})
	if h.client.sack.active || h.client.transfer.dataRetransmissions != retries {
		t.Fatal("fresh feedback reopened the timed-out flight")
	}
}

// Repairing segment 2 can synchronously ACK through 3 and request repair of 4.
// Both callbacks must see ready counters, updated sequence positions and one recovery.
func TestSACKRecoveryAcceptsSynchronousACKs(t *testing.T) {
	h := newSACKHarness(t, true, true, 8192)
	h.client.SetSender(func(packet model.Packet) {
		h.sent = append(h.sent, packet)
		switch packet.Seq {
		case 1025:
			h.acknowledge(3073, model.SACKBlock{Start: 4097, End: 8193})
		case 3073:
			h.acknowledge(8193)
		}
	})
	h.acknowledge(1025,
		model.SACKBlock{Start: 2049, End: 3073},
		model.SACKBlock{Start: 4097, End: 8193},
	)
	if h.client.sack.active || h.client.acknowledgedPayloadBytes != 8192 ||
		h.client.transfer.dataRetransmissions != 2 || h.client.dataTimer != 0 {
		t.Fatalf("synchronous recovery left wrong state: %+v", h.client.GetState())
	}
}

// The ACK for segment 1 starts a repair whose reply confirms the entire stream.
// Completion observers must already have seen every payload-progress callback.
func TestSACKPublishesPayloadProgressBeforeSynchronousCompletion(t *testing.T) {
	h := newSACKHarness(t, true, true, 8192)
	confirmed := 0
	confirmedAtCompletion := 0
	h.client.OnPayloadAcknowledged = func(bytes int) { confirmed += bytes }
	h.client.OnStreamComplete = func() { confirmedAtCompletion = confirmed }
	h.client.SetSender(func(packet model.Packet) {
		if packet.Size > 0 && packet.Seq == 1025 {
			h.acknowledge(8193)
		}
	})

	h.acknowledge(1025, model.SACKBlock{Start: 2049, End: 8193})
	if confirmed != 8192 || confirmedAtCompletion != 8192 {
		t.Fatalf("completion saw %d payload bytes; final callback total=%d",
			confirmedAtCompletion, confirmed)
	}
}

// A progress callback can stop the client; this ACK must not start a repair afterward.
func TestSACKProgressCallbackCanStopPendingRecovery(t *testing.T) {
	h := newSACKHarness(t, true, true, 8192)
	h.client.OnPayloadAcknowledged = func(int) { h.client.Shutdown() }
	h.acknowledge(1025, model.SACKBlock{Start: 2049, End: 8193})
	if !h.client.terminated || h.client.transfer.dataRetransmissions != 0 {
		t.Fatal("SACK emitted a repair before the progress callback could stop the client")
	}
}

// A newer ACK omitted segment 3 from its limited report; an older ACK still reports it.
// Retain both ranges without moving the current cumulative ACK or receive window back.
func TestSACKReorderedACKCanAddUsefulRanges(t *testing.T) {
	h := newSACKHarness(t, true, true, 8192)
	h.acknowledge(1025, model.SACKBlock{Start: 4097, End: 5121})
	windowEnd := h.client.peerWindowEnd
	h.acknowledge(1, model.SACKBlock{Start: 2049, End: 3073})
	if h.client.sack.board.RangeCount() != 2 || h.client.oldestUnackedSeq != 1025 ||
		h.client.acknowledgedPayloadBytes != 1024 || h.client.peerWindowEnd != windowEnd {
		t.Fatalf("reordered feedback lost ranges or regressed progress: %+v", h.client.GetState())
	}
}

// FIN occupies the backend's next sequence position before a repair can get a reply.
// Otherwise that reply's Seq=2 would be rejected while the client still expects Seq=1.
func TestSACKConsumesPeerFINBeforeSynchronousRepair(t *testing.T) {
	h := newSACKHarness(t, true, true, 8192)
	h.client.SetSender(func(packet model.Packet) {
		if packet.Size > 0 && packet.Seq == 1025 {
			if packet.Ack != 2 {
				t.Fatalf("repair did not acknowledge peer FIN: %+v", packet)
			}
			h.client.HandlePacket(model.Packet{
				Key: h.client.key, Flag: model.FlagACK, Seq: 2, Ack: 8193,
				ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
			})
		}
	})
	h.client.HandlePacket(model.Packet{
		Key: h.client.key, Flag: model.FlagFIN | model.FlagACK, Seq: 1, Ack: 1025,
		ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
		SACKBlocks:         []model.SACKBlock{{Start: 2049, End: 8193}},
	})
	if h.client.acknowledgedPayloadBytes != 8192 || h.client.nextReceiveSeq != 2 {
		t.Fatalf("peer FIN caused a valid repair ACK to be discarded: %+v", h.client.GetState())
	}
}

// The backend already closed its sending half, then retries FIN with a newer ACK/SACK.
// Its FIN sequence stays old, while its reports about our outgoing data can advance.
func TestSACKDuplicateFINCanCarryNewPayloadFeedback(t *testing.T) {
	h := newSACKHarness(t, true, true, 8192)
	fin := model.Packet{
		Key: h.client.key, Flag: model.FlagFIN | model.FlagACK, Seq: 1, Ack: 1,
		ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
	}
	h.client.HandlePacket(fin)

	h.client.SetSender(func(packet model.Packet) {
		if packet.Size > 0 && packet.Seq == 1025 {
			h.client.HandlePacket(model.Packet{
				Key: h.client.key, Flag: model.FlagACK, Seq: 2, Ack: 8193,
				ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
			})
		}
	})
	fin.Ack = 1025
	fin.SACKBlocks = []model.SACKBlock{{Start: 2049, End: 8193}}
	h.client.HandlePacket(fin)
	if h.client.acknowledgedPayloadBytes != 8192 || h.client.nextReceiveSeq != 2 {
		t.Fatalf("a repeated FIN lost newer ACK/SACK feedback: %+v", h.client.GetState())
	}
}

// Even without SACK, a repeated FIN may confirm the last outgoing payload bytes.
// That progress must finish the stream and start our own FIN, rather than leave it idle.
func TestDuplicateFINCanFinishPayloadWithSACKDisabled(t *testing.T) {
	h := newSACKHarness(t, false, false, 8192)
	fin := model.Packet{
		Key: h.client.key, Flag: model.FlagFIN | model.FlagACK, Seq: 1, Ack: 1,
		ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
	}
	h.client.HandlePacket(fin)
	fin.Ack = 8193
	h.client.HandlePacket(fin)
	if !h.client.streamCompleted || !h.client.connection.finSent ||
		h.client.acknowledgedPayloadBytes != 8192 || h.client.nextReceiveSeq != 2 {
		t.Fatalf("a repeated FIN confirmed data without finishing the stream: %+v", h.client.GetState())
	}
}
