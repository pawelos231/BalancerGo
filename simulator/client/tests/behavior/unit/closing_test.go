package client_test

import (
	"testing"
	"time"

	"load-balancer/model"
)

func TestTCPCloseDrainsAcceptedStreamAfterPayloadLoss(t *testing.T) {
	h := newTransferHarness(t)
	closed := 0
	h.client.OnClose = func() { closed++ }
	h.client.SendCompleteStream(2050, 100*time.Millisecond)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(150 * time.Millisecond)
	h.client.Close()
	h.client.Close()
	h.client.SendCompleteStream(99, 0)

	h.runUntil(400 * time.Millisecond)
	data := sentClientData(h.sent)
	if len(data) != 3 || data[2].Seq != 2049 || data[2].Size != 2 || h.count(model.FlagFIN) != 0 {
		t.Fatalf("Close failed to drain accepted data before FIN: %+v", h.sent)
	}
	h.runUntil(1100 * time.Millisecond)
	data = sentClientData(h.sent)
	if len(data) != 4 || data[3] != data[0] || h.transfer().DataRetransmissions != 1 {
		t.Fatalf("Close lost the pending payload retry: %+v", data)
	}
	h.receive(model.FlagACK, 1025)
	h.runUntil(h.engine.Now())
	if h.count(model.FlagFIN) != 0 || h.client.StreamCompleted() {
		t.Fatal("Close sent FIN before all accepted payload was confirmed")
	}
	h.receive(model.FlagACK, 2051)
	h.runUntil(h.engine.Now())
	fin := h.sent[len(h.sent)-1]
	if fin.Flag != model.FlagFIN|model.FlagACK || fin.Seq != 2051 || !h.client.IsClosing() {
		t.Fatalf("drained stream ended at the wrong FIN: %+v", fin)
	}
	h.receive(model.FlagFIN|model.FlagACK, 2052)
	ack := h.sent[len(h.sent)-1]
	if ack.Flag != model.FlagACK || ack.Seq != 2052 || ack.Ack != 2 || h.client.IsTerminated() {
		t.Fatalf("FIN reply skipped the final ACK or TIME-WAIT: %+v", ack)
	}
	h.runUntil(h.engine.Now() + 20*time.Millisecond)
	state := h.client.GetState()
	if state.Transfer.Status != model.TransferSucceeded || state.AcknowledgedPayloadBytes != 2050 ||
		state.Connection.Outcome != model.CloseGraceful || state.Connection.FINRetransmissions != 0 ||
		!h.client.IsTerminated() || h.engine.Pending() != 0 || closed != 1 {
		t.Fatalf("graceful draining left work or lost payload progress: %+v, closes=%d", state, closed)
	}
}

func TestTCPPeerFINClosesOnlyItsSendingHalf(t *testing.T) {
	h := newTransferHarness(t)
	h.client.SendCompleteStream(2050, 100*time.Millisecond)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(150 * time.Millisecond)
	h.receive(model.FlagFIN|model.FlagACK, 1025)
	if state := h.client.GetConnectionState(); state.State != model.TCPCloseWait ||
		h.client.IsTerminated() || h.client.GetState().AcknowledgedPayloadBytes != 1024 {
		t.Fatalf("peer FIN discarded the unfinished stream: %+v", h.client.GetState())
	}
	ack := h.sent[len(h.sent)-1]
	if ack.Flag != model.FlagACK || ack.Ack != 2 {
		t.Fatalf("peer FIN did not consume exactly one sequence position: %+v", ack)
	}
	h.runUntil(300 * time.Millisecond)
	if len(sentClientData(h.sent)) != 3 || h.count(model.FlagFIN) != 0 {
		t.Fatalf("peer FIN stopped accepted payload or sent FIN too early: %+v", h.sent)
	}
	h.client.HandlePacket(model.Packet{
		Key: h.client.Key(), Flag: model.FlagACK, Seq: 2, Ack: 2051,
		ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
	})
	h.runUntil(h.engine.Now())
	fin := h.sent[len(h.sent)-1]
	if state := h.client.GetConnectionState(); state.State != model.TCPLastAck ||
		fin.Flag != model.FlagFIN|model.FlagACK || fin.Seq != 2051 || fin.Ack != 2 {
		t.Fatalf("passive close did not wait for the FIN ACK: state=%+v, packet=%+v", state, fin)
	}
	h.client.HandlePacket(model.Packet{
		Key: h.client.Key(), Flag: model.FlagACK, Seq: 2, Ack: 2052,
		ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
	})
	if state := h.client.GetState(); !h.client.IsTerminated() || h.engine.Pending() != 0 ||
		state.Connection.Outcome != model.CloseGraceful || state.Transfer.Status != model.TransferSucceeded ||
		state.AcknowledgedPayloadBytes != 2050 {
		t.Fatalf("passive close lost the successful stream: %+v", state)
	}
}

// Two duplicate data ACKs plus the peer's FIN are still only two loss signals.
// A FIN may repeat ACK=1 without indicating that another data segment arrived.
func TestTCPPeerFINDoesNotCountAsDuplicateDataACK(t *testing.T) {
	h := newFastRecoveryHarness(t, 8, 8*model.SEGMENT_SIZE)
	h.receive(model.FlagACK, 1)
	h.receive(model.FlagACK, 1)
	h.receive(model.FlagFIN|model.FlagACK, 1)
	state := h.client.GetState()
	if state.Connection.State != model.TCPCloseWait || state.Congestion.DuplicateACKs != 2 ||
		state.Congestion.FastRetransmissions != 0 || state.Transfer.DataRetransmissions != 0 {
		t.Fatalf("FIN manufactured a third duplicate data ACK: %+v", state)
	}
}

func TestTCPFINAcknowledgmentCanPrecedePeerFIN(t *testing.T) {
	h := newTransferHarness(t)
	h.client.SendCompleteStream(0, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(0)
	h.receive(model.FlagACK, 2)
	if state := h.client.GetConnectionState(); state.State != model.TCPFinWait2 || h.engine.Pending() != 1 {
		t.Fatalf("pure FIN ACK failed to enter FIN-WAIT-2: %+v", state)
	}
	h.runUntil(1500 * time.Millisecond)
	if h.count(model.FlagFIN) != 1 || h.client.IsTerminated() {
		t.Fatal("acknowledged FIN was retried or terminated before the peer FIN")
	}
	h.receive(model.FlagFIN|model.FlagACK, 2)
	if state := h.client.GetConnectionState(); state.State != model.TCPTimeWait || !h.client.IsClosing() {
		t.Fatalf("later peer FIN failed to enter TIME-WAIT: %+v", state)
	}
	h.runUntil(1520 * time.Millisecond)
	if state := h.client.GetConnectionState(); !h.client.IsTerminated() || state.Outcome != model.CloseGraceful ||
		h.engine.Pending() != 0 {
		t.Fatalf("separate FIN exchange did not finish: %+v", state)
	}
}

// A malformed FIN cannot acknowledge our FIN or cancel its retry timer.
func TestTCPInvalidFINCannotAcknowledgeLocalFIN(t *testing.T) {
	h := newTransferHarness(t)
	h.client.SendCompleteStream(0, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(0)

	for _, packet := range []model.Packet{
		{Flag: model.FlagFIN | model.FlagACK | model.FlagPSH, Seq: 1, Ack: 2},
		{Flag: model.FlagFIN | model.FlagACK, Seq: 1, Ack: 2, Size: 1},
		{Flag: model.FlagFIN | model.FlagACK, Seq: 2, Ack: 2},
		{Flag: model.FlagFIN | model.FlagACK, Seq: 1, Ack: 0},
	} {
		packet.Key = h.client.Key()
		h.client.HandlePacket(packet)
		if state := h.client.GetConnectionState(); state.State != model.TCPFinWait1 {
			t.Fatalf("malformed FIN changed our closing state: packet=%+v state=%+v", packet, state)
		}
	}
	h.runUntil(time.Second)
	if h.count(model.FlagFIN) != 2 || h.client.GetConnectionState().FINRetransmissions != 1 {
		t.Fatal("a malformed FIN cancelled the valid FIN retry")
	}
}

func TestTCPFINRetriesKeepSequenceAndUseSeparateBackoff(t *testing.T) {
	h := newAdaptiveTransferHarness(t)
	config := model.DefaultCloseConfig()
	config.MaxFINRetries = 2
	h.client.SetCloseConfig(config)
	rto := model.DefaultRTOConfig()
	rto.Mode = model.RTOFixed
	rto.InitialTimeout = 100 * time.Millisecond
	rto.MinimumTimeout = 50 * time.Millisecond
	rto.MaximumTimeout = 250 * time.Millisecond
	h.client.SetRTOConfig(rto)
	h.client.SendCompleteStream(0, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(0)
	payloadRTO := h.client.GetRTOState()
	fin := h.sent[len(h.sent)-1]
	for index, deadline := range []time.Duration{100 * time.Millisecond, 300 * time.Millisecond} {
		h.runUntil(deadline - time.Nanosecond)
		if h.count(model.FlagFIN) != index+1 {
			t.Fatalf("FIN retry fired before %s", deadline)
		}
		h.runUntil(deadline)
		if retry := h.sent[len(h.sent)-1]; retry != fin || h.count(model.FlagFIN) != index+2 ||
			h.client.GetConnectionState().FINRetransmissions != uint64(index+1) || h.engine.Pending() != 1 {
			t.Fatalf("FIN retry changed sequence or timer count: %+v, pending=%d", retry, h.engine.Pending())
		}
	}
	h.runUntil(550*time.Millisecond - time.Nanosecond)
	if h.client.IsTerminated() {
		t.Fatal("FIN retry budget expired before the capped timeout")
	}
	h.runUntil(550 * time.Millisecond)
	state := h.client.GetState()
	if !h.client.IsTerminated() || state.Connection.Outcome != model.CloseFINRetryLimit ||
		state.Transfer.Status != model.TransferSucceeded || state.Transfer.DataRetransmissions != 0 ||
		h.count(model.FlagFIN) != 3 || h.engine.Pending() != 0 || h.client.GetRTOState() != payloadRTO {
		t.Fatalf("FIN exhaustion changed payload timing or left work: %+v", state)
	}
}

func TestTCPZeroFINRetryBudgetStillSendsTheOriginalFIN(t *testing.T) {
	h := newTransferHarness(t)
	config := model.DefaultCloseConfig()
	config.MaxFINRetries = 0
	h.client.SetCloseConfig(config)
	h.client.SendCompleteStream(0, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(time.Second - time.Nanosecond)
	if h.count(model.FlagFIN) != 1 || h.client.IsTerminated() {
		t.Fatal("zero retry budget suppressed the original FIN or its timeout")
	}
	h.runUntil(time.Second)
	if state := h.client.GetState(); !h.client.IsTerminated() || h.count(model.FlagFIN) != 1 ||
		state.Connection.FINRetransmissions != 0 || state.Connection.Outcome != model.CloseFINRetryLimit ||
		state.Transfer.Status != model.TransferSucceeded || h.engine.Pending() != 0 {
		t.Fatalf("zero retry budget retried FIN or lost payload success: %+v", state)
	}
}

// An odd 5 ns cap must allow doubling 2 ns to 4 ns before saturating at 5 ns.
func TestTCPFINBackoffPreservesOddNanosecondLimit(t *testing.T) {
	h := newTransferHarness(t)
	rto := model.DefaultRTOConfig()
	rto.Mode = model.RTOFixed
	rto.InitialTimeout = 2 * time.Nanosecond
	rto.MinimumTimeout = time.Nanosecond
	rto.MaximumTimeout = 5 * time.Nanosecond
	h.client.SetRTOConfig(rto)
	h.client.SendCompleteStream(0, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(0)
	h.runUntil(6 * time.Nanosecond)
	if h.count(model.FlagFIN) != 3 {
		t.Fatal("FIN retries did not occur at 2 ns and 6 ns under the 5 ns cap")
	}
}

func TestTCPFINWait2HasAFixedDeadline(t *testing.T) {
	h := newTransferHarness(t)
	config := model.DefaultCloseConfig()
	config.FinWaitTimeout = 50 * time.Millisecond
	h.client.SetCloseConfig(config)
	h.client.SendCompleteStream(0, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(0)
	h.receive(model.FlagACK, 2)
	h.runUntil(40 * time.Millisecond)
	h.receive(model.FlagACK, 2)
	h.runUntil(50*time.Millisecond - time.Nanosecond)
	if h.client.IsTerminated() {
		t.Fatal("FIN-WAIT-2 expired before its deadline")
	}
	h.runUntil(50 * time.Millisecond)
	if state := h.client.GetState(); !h.client.IsTerminated() || h.engine.Pending() != 0 ||
		state.Connection.Outcome != model.CloseWaitTimeout || state.Transfer.Status != model.TransferSucceeded {
		t.Fatalf("FIN-WAIT-2 failed to end without changing payload success: %+v", state)
	}
}

func TestTCPDuplicateFINRepeatsFinalACKAndRestartsTimeWait(t *testing.T) {
	h := newTransferHarness(t)
	config := model.DefaultCloseConfig()
	config.MSL = 50 * time.Millisecond
	h.client.SetCloseConfig(config)
	h.client.SendCompleteStream(0, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(0)
	h.receive(model.FlagFIN|model.FlagACK, 2)
	firstACK := h.sent[len(h.sent)-1]
	h.runUntil(60 * time.Millisecond)
	before := len(h.sent)
	h.receive(model.FlagFIN|model.FlagACK, 2)
	if len(h.sent) != before+1 || h.sent[len(h.sent)-1] != firstACK || h.engine.Pending() != 1 {
		t.Fatalf("duplicate FIN changed the final ACK or added a timer: %+v", h.sent)
	}
	h.runUntil(120 * time.Millisecond)
	h.client.HandlePacket(model.Packet{
		Key: h.client.Key(), Flag: model.FlagFIN | model.FlagACK, Seq: 99, Ack: 2,
		ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
	})
	h.runUntil(160*time.Millisecond - time.Nanosecond)
	if h.client.IsTerminated() {
		t.Fatal("duplicate FIN did not restart the full two-MSL wait")
	}
	h.runUntil(160 * time.Millisecond)
	state := h.client.GetConnectionState()
	if !h.client.IsTerminated() || state.Outcome != model.CloseGraceful || h.engine.Pending() != 0 {
		t.Fatalf("invalid FIN extended TIME-WAIT or left work: %+v", state)
	}
	h.receive(model.FlagFIN|model.FlagACK, 2)
	h.runUntil(time.Second)
	if h.client.GetConnectionState() != state || len(h.sent) != before+1 {
		t.Fatal("terminal connection responded again or changed its close result")
	}
}

func TestTCPDefaultTimeWaitLastsSixtySeconds(t *testing.T) {
	h := newTransferHarness(t)
	h.client.SetCloseConfig(model.DefaultCloseConfig())
	h.client.SendCompleteStream(0, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(0)
	h.receive(model.FlagFIN|model.FlagACK, 2)
	h.runUntil(time.Minute - time.Nanosecond)
	if h.client.IsTerminated() || h.client.GetConnectionState().State != model.TCPTimeWait {
		t.Fatalf("default TIME-WAIT ended early: %+v", h.client.GetConnectionState())
	}
	h.runUntil(time.Minute)
	if state := h.client.GetConnectionState(); !h.client.IsTerminated() || state.Outcome != model.CloseGraceful ||
		h.engine.Pending() != 0 {
		t.Fatalf("default TIME-WAIT did not end after two MSLs: %+v", state)
	}
}

func TestTCPResetValidatesFlagsSequenceAndACK(t *testing.T) {
	for _, test := range []struct {
		name  string
		reset model.Packet
	}{
		{"without ACK", model.Packet{Flag: model.FlagRST, Seq: 1}},
		{"old ACK", model.Packet{Flag: model.FlagRST | model.FlagACK, Seq: 1, Ack: 1}},
		{"newest ACK", model.Packet{Flag: model.FlagRST | model.FlagACK, Seq: 1, Ack: 4097}},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newTransferHarness(t)
			h.client.SendCompleteStream(4096, 0)
			h.client.EstablishConnection()
			h.runUntil(0)
			h.receive(model.FlagSYN|model.FlagACK, 1)
			h.runUntil(0)
			h.receive(model.FlagACK, 1025)
			invalid := []model.Packet{
				{Flag: model.FlagRST | model.FlagACK, Seq: 2, Ack: 1025},
				{Flag: model.FlagRST | model.FlagACK, Seq: 1, Ack: 0},
				{Flag: model.FlagRST | model.FlagACK, Seq: 1, Ack: 4098},
				{Flag: model.FlagRST | model.FlagSYN | model.FlagACK, Seq: 1, Ack: 1025},
				{Flag: model.FlagRST | model.FlagFIN | model.FlagACK, Seq: 1, Ack: 1025},
				{Flag: model.FlagRST | model.FlagPSH | model.FlagACK, Seq: 1, Ack: 1025},
				{Flag: model.FlagRST | model.FlagACK, Seq: 1, Ack: 1025, Size: 1},
			}
			for _, packet := range invalid {
				packet.Key = h.client.Key()
				h.client.HandlePacket(packet)
				if h.client.IsTerminated() || h.client.GetConnectionState().State != model.TCPEstablished {
					t.Fatalf("invalid reset closed the connection: %+v", packet)
				}
			}
			test.reset.Key = h.client.Key()
			h.client.HandlePacket(test.reset)
			state := h.client.GetState()
			if !h.client.IsTerminated() || state.Connection.Outcome != model.CloseReset ||
				state.Transfer.Status != model.TransferFailed || state.Transfer.FailureReason != model.TransferReset ||
				state.AcknowledgedPayloadBytes != 1024 || state.Congestion.BytesInFlight != 0 || h.engine.Pending() != 0 {
				t.Fatalf("valid reset lost partial progress or retained work: %+v", state)
			}
		})
	}
}

func TestTCPSynSentResetRequiresAnACKForTheSYN(t *testing.T) {
	h := newTransferHarness(t)
	h.client.SendCompleteStream(1, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	for _, packet := range []model.Packet{
		{Flag: model.FlagRST},
		{Flag: model.FlagRST | model.FlagACK, Ack: 0},
		{Flag: model.FlagRST | model.FlagACK, Ack: 2},
	} {
		packet.Key = h.client.Key()
		h.client.HandlePacket(packet)
		if h.client.IsTerminated() || h.client.GetConnectionState().State != model.TCPSynSent {
			t.Fatalf("unconfirmed SYN accepted a reset: %+v", packet)
		}
	}
	h.client.HandlePacket(model.Packet{Key: h.client.Key(), Flag: model.FlagRST | model.FlagACK, Ack: 1})
	if state := h.client.GetState(); !h.client.IsTerminated() || state.Connection.Outcome != model.CloseReset ||
		state.Transfer.FailureReason != model.TransferReset || h.engine.Pending() != 0 {
		t.Fatalf("valid SYN reset retained handshake work: %+v", state)
	}
}

// A backend may close as soon as it sees the third handshake ACK of an empty stream.
// Its FIN must be consumed before automatic local close chooses LAST_ACK.
func TestTCPEmptyStreamHandlesSynchronousPeerFINBeforeLocalClose(t *testing.T) {
	h := newTransferHarness(t)
	peerClosed := false
	h.client.SetSender(func(packet model.Packet) {
		h.sent = append(h.sent, packet)
		if packet.Flag == model.FlagSYN {
			h.receive(model.FlagSYN|model.FlagACK, 1)
		} else if packet.Flag == model.FlagACK && packet.Ack == 1 && !peerClosed {
			peerClosed = true
			h.receive(model.FlagFIN|model.FlagACK, 1)
		}
	})
	h.client.SendCompleteStream(0, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	state := h.client.GetState()
	if !peerClosed || state.Connection.State != model.TCPLastAck ||
		state.Transfer.Status != model.TransferSucceeded || h.count(model.FlagFIN) != 1 {
		t.Fatalf("empty-stream FIN chose the wrong close order: %+v", state)
	}
}

func TestTCPSynchronousClosingRepliesSeePreparedState(t *testing.T) {
	h := newTransferHarness(t)
	closed := 0
	h.client.OnClose = func() { closed++ }
	h.client.SetSender(func(packet model.Packet) {
		h.sent = append(h.sent, packet)
		switch {
		case packet.Flag == model.FlagSYN:
			h.receive(model.FlagSYN|model.FlagACK, 1)
		case packet.Size > 0:
			h.client.Close()
			h.receive(model.FlagACK, packet.Seq+uint64(packet.Size))
		case packet.Flag&model.FlagFIN != 0:
			if state := h.client.GetConnectionState(); state.State != model.TCPFinWait1 || h.engine.Pending() != 1 {
				t.Fatalf("FIN callback saw unfinished state: %+v, pending=%d", state, h.engine.Pending())
			}
			h.receive(model.FlagFIN|model.FlagACK, packet.Seq+1)
		}
	})
	h.client.SendCompleteStream(2050, 0)
	h.client.EstablishConnection()
	h.runUntil(20 * time.Millisecond)
	state := h.client.GetState()
	if !h.client.IsTerminated() || state.Transfer.Status != model.TransferSucceeded ||
		state.AcknowledgedPayloadBytes != 2050 || state.Connection.Outcome != model.CloseGraceful ||
		state.Connection.FINRetransmissions != 0 || len(sentClientData(h.sent)) != 3 ||
		h.count(model.FlagFIN) != 1 || h.engine.Pending() != 0 || closed != 1 {
		t.Fatalf("synchronous close left work or duplicated callbacks: %+v, closes=%d", state, closed)
	}
}

func TestTCPFINRetrySupportsSynchronousSimultaneousClose(t *testing.T) {
	h := newTransferHarness(t)
	fins := 0
	h.client.SetSender(func(packet model.Packet) {
		h.sent = append(h.sent, packet)
		if packet.Flag&model.FlagFIN == 0 {
			return
		}
		fins++
		if fins == 1 {
			return // Drop the original FIN.
		}
		if packet.Seq != 1 || h.client.GetConnectionState().FINRetransmissions != 1 {
			t.Fatalf("FIN retry callback saw stale sequence or counters: %+v", h.client.GetConnectionState())
		}
		h.receive(model.FlagFIN|model.FlagACK, 1)
		if state := h.client.GetConnectionState(); state.State != model.TCPClosing {
			t.Fatalf("peer FIN acknowledged our unconfirmed FIN too early: %+v", state)
		}
		h.client.HandlePacket(model.Packet{
			Key: h.client.Key(), Flag: model.FlagACK, Seq: 2, Ack: 2,
			ReceiveWindowBytes: model.DefaultReceiveBufferBytes,
		})
	})
	h.client.SendCompleteStream(0, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(time.Second)
	if state := h.client.GetConnectionState(); state.State != model.TCPTimeWait || h.engine.Pending() != 1 {
		t.Fatalf("synchronous FIN retry replies retained a retransmission timer: %+v", state)
	}
	h.runUntil(1020 * time.Millisecond)
	if state := h.client.GetState(); !h.client.IsTerminated() || state.Connection.Outcome != model.CloseGraceful ||
		state.Connection.FINRetransmissions != 1 || state.Transfer.Status != model.TransferSucceeded ||
		fins != 2 || h.engine.Pending() != 0 {
		t.Fatalf("lost FIN did not recover cleanly: %+v", state)
	}
}
