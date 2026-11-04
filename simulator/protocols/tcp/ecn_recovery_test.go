package tcp

import (
	"testing"
	"time"

	"load-balancer/model"
)

// SACK can repair a later hole before an ACK advances it to the flight's head.
// The ordinary head retry count resets on that ACK, but losing the prior repair
// still supplies a new threshold on RTO rather than reusing the ECN reduction.
func TestECNSACKRepairTimeoutRetainsHistoryAcrossACKProgress(t *testing.T) {
	e, sender, packets := ecnTestFlight(t, 8, 16, true)
	defer sender.Shutdown()
	// The source's next burst is after this RTO, keeping the outstanding flight fixed.
	sender.source.(*testPayloadSource).readyAt = 2 * time.Second
	ack := ecnTestACK(sender, 1025, model.FlagECE)
	ack.SACKBlocks = []model.SACKBlock{{Start: 2049, End: 3073}, {Start: 4097, End: 8193}}
	sender.HandlePacket(ack)
	if retry := (*packets)[len(*packets)-1]; retry.Seq != 3073 {
		t.Fatalf("later hole was not repaired ahead of cumulative ACK progress: %+v", retry)
	}
	ack.Ack, ack.SACKBlocks = 3073, []model.SACKBlock{{Start: 4097, End: 8193}}
	sender.HandlePacket(ack)
	if sender.oldestSegmentRetransmissions != 0 {
		t.Fatal("the example did not reset the ordinary head retry count")
	}
	if err := e.RunUntil(time.Second); err != nil {
		t.Fatal(err)
	}
	// Five MSS remain cumulatively unconfirmed, so new RTO ssthresh is 2.5 MSS.
	if state := sender.GetCongestionState(); state.SlowStartThresholdBytes != 2560 ||
		state.CongestionWindowBytes != model.SEGMENT_SIZE {
		t.Fatalf("ACK progress hid the lost SACK repair's new congestion event: %+v", state)
	}
}
