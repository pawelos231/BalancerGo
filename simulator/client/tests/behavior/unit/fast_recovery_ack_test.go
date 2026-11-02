package client_test

import (
	"testing"
	"time"

	"load-balancer/model"
)

// Wrong byte positions, payload, SYN/FIN and RST cannot manufacture a data-loss signal.
// Three valid ACK=1 packets after these rejected examples still trigger exactly one retry.
func TestTCPFastRetransmitRejectsUnrelatedACKs(t *testing.T) {
	h := newFastRecoveryHarness(t, 8, 16*model.SEGMENT_SIZE)
	packets := []model.Packet{
		{Flag: model.FlagACK, Seq: 2, Ack: 1},
		{Flag: model.FlagACK, Seq: 1, Ack: 0},
		{Flag: model.FlagACK, Seq: 1, Ack: 8194},
		{Flag: model.FlagPSH | model.FlagACK, Seq: 1, Ack: 1, Size: 1},
		{Flag: model.FlagSYN | model.FlagACK, Seq: 0, Ack: 1},
		{Flag: model.FlagFIN | model.FlagACK, Seq: 2, Ack: 1},
		{Flag: model.FlagRST | model.FlagACK, Seq: 2, Ack: 1},
	}
	for _, packet := range packets {
		packet.Key = h.client.Key()
		packet.ReceiveWindowBytes = model.DefaultReceiveBufferBytes
		h.client.HandlePacket(packet)
	}
	if state := congestionState(t, h); state.DuplicateACKs != 0 || state.FastRetransmissions != 0 {
		t.Fatalf("an unrelated packet counted toward recovery: %+v", state)
	}
	for count := 0; count < 3; count++ {
		h.receive(model.FlagACK, 1)
	}
	if state := congestionState(t, h); state.FastRetransmissions != 1 || state.DuplicateACKs != 3 {
		t.Fatalf("three later valid duplicates did not trigger recovery: %+v", state)
	}
}

// Two ACK=1 duplicates cannot combine with ACK=1025 duplicates after real progress.
func TestTCPFastRetransmitRestartsDuplicateCountOnProgress(t *testing.T) {
	h := newFastRecoveryHarness(t, 8, 32*model.SEGMENT_SIZE)
	h.receive(model.FlagACK, 1)
	h.receive(model.FlagACK, 1)
	h.receive(model.FlagACK, 1025)
	h.runUntil(h.engine.Now())
	if state := congestionState(t, h); state.DuplicateACKs != 0 {
		t.Fatalf("ACK progress retained the previous missing-byte count: %+v", state)
	}

	h.receive(model.FlagACK, 1025)
	if state := congestionState(t, h); state.DuplicateACKs != 1 || state.FastRetransmissions != 0 {
		t.Fatalf("one new duplicate reused the old count: %+v", state)
	}
	h.receive(model.FlagACK, 1025)
	h.receive(model.FlagACK, 1025)
	data := sentClientData(h.sent)
	if state := congestionState(t, h); state.FastRetransmissions != 1 || data[len(data)-1].Seq != 1025 {
		t.Fatalf("new recovery did not retry its own missing head: %+v", state)
	}
}

// Recovery's fifth duplicate creates credit at 800 ms, but the sender's next slot is 900 ms.
// Only the retry bypasses the pause; newly generated payload preserves the configured gap.
func TestTCPFastRecoveryKeepsNewSegmentPause(t *testing.T) {
	h := newRenoTransferHarness(t)
	useFixedCongestionTestTimers(h)
	config := model.DefaultCongestionConfig()
	config.InitialWindowSegments = 8
	h.client.SetCongestionConfig(config)
	h.client.SendCompleteStream(16*model.SEGMENT_SIZE, 100*time.Millisecond)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(800 * time.Millisecond)
	for count := 0; count < 5; count++ {
		h.receive(model.FlagACK, 1)
	}
	h.runUntil(900*time.Millisecond - time.Nanosecond)
	if len(sentClientData(h.sent)) != 9 {
		t.Fatal("recovery sent new data before its configured pause elapsed")
	}
	h.runUntil(900 * time.Millisecond)
	data := sentClientData(h.sent)
	if len(data) != 10 || data[9].Seq != 8193 {
		t.Fatalf("new recovery data missed its existing send slot: %+v", data)
	}
}

// One fast retry plus 29 timeout retries exhausts the existing 30-retry limit.
// Changing the detection path cannot silently grant an extra attempt.
func TestTCPFastRetransmitSharesTheDataRetryLimit(t *testing.T) {
	h := newRenoTransferHarness(t)
	useFixedCongestionTestTimers(h)
	h.client.SendCompleteStream(4096, 0)
	h.client.EstablishConnection()
	h.runUntil(0)
	h.receive(model.FlagSYN|model.FlagACK, 1)
	h.runUntil(0)
	for count := 0; count < 3; count++ {
		h.receive(model.FlagACK, 1)
	}
	h.runUntil(30 * time.Second)
	transfer := h.transfer()
	if transfer.Status != model.TransferFailed || transfer.FailureReason != model.TransferDataRetryLimit ||
		transfer.DataRetransmissions != 30 || congestionState(t, h).FastRetransmissions != 1 {
		t.Fatalf("fast retry escaped shared retransmission accounting: %+v", transfer)
	}
	if len(sentClientData(h.sent)) != 34 || h.engine.Pending() != 0 {
		t.Fatal("retry-limit cleanup emitted another packet or left a timer running")
	}
}
