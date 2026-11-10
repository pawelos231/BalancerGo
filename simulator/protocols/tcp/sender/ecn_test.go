package sender

import (
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
)

// One marked packet can produce many ECE ACKs. They reduce one flight once,
// report delivered bytes normally, and never manufacture a retransmission.
func TestECNFeedbackReducesOnceAndKeepsRetriesClean(t *testing.T) {
	e, sender, packets := ecnTestFlight(t, 8, 16, false)
	defer sender.Shutdown()
	ack := ecnTestACK(sender, 1025, model.FlagECE)
	sender.HandlePacket(ack)
	sender.HandlePacket(ack)
	if state := sender.GetState(); state.Congestion.CongestionWindowBytes != 4096 ||
		state.ECN.ECNResponseCount != 1 || state.Transfer.DataRetransmissions != 0 ||
		state.Congestion.Phase == "fastRecovery" {
		t.Fatalf("ECE duplicated a response or fabricated loss: %+v", state)
	}

	// ACK the original flight; only the first newly emitted segment carries CWR.
	sender.HandlePacket(ecnTestACK(sender, 8193, 0))
	if err := e.RunUntil(0); err != nil {
		t.Fatal(err)
	}
	newPackets := (*packets)[9:] // SYN and the original eight data segments.
	if len(newPackets) < 2 || newPackets[0].Flag&model.FlagCWR == 0 {
		t.Fatalf("the first new payload did not carry CWR: %+v", newPackets)
	}
	for _, pkt := range newPackets[1:] {
		if pkt.Flag&model.FlagCWR != 0 {
			t.Fatal("CWR leaked into a later new data segment")
		}
	}
	retry, ok := sender.prepareDataRetry()
	if !ok || retry.ECN.Codepoint != model.NotECT || retry.Flag&model.FlagCWR != 0 {
		t.Fatalf("a buffered retry replayed ECT/CWR: %+v", retry)
	}
	sender.retransmitSACKHole(model.SACKBlock{Start: retry.Seq, End: retry.Seq + 100}, false)
	if pkt := (*packets)[len(*packets)-1]; pkt.ECN.Codepoint != model.NotECT || pkt.Flag&model.FlagCWR != 0 {
		t.Fatalf("a rebuilt SACK retry replayed ECT/CWR: %+v", pkt)
	}
}

// ECN's flight guard suppresses only reductions: a real SACK hole still enters
// fast recovery and is repaired immediately, even on the same ECE ACK.
func TestECNAndSACKLossShareReductionButStillRepair(t *testing.T) {
	e, sender, packets := ecnTestFlight(t, 8, 16, true)
	defer sender.Shutdown()
	ack := ecnTestACK(sender, 1, model.FlagECE)
	ack.SACKBlocks = []model.SACKBlock{{Start: 2049, End: 5121}}
	sender.HandlePacket(ack)
	state := sender.GetState()
	if state.ECN.ECNResponseCount != 1 || state.Congestion.SlowStartThresholdBytes != 4096 ||
		state.Congestion.Phase != "fastRecovery" || state.Congestion.FastRetransmissions != 1 {
		t.Fatalf("same-flight ECE prevented SACK repair or reduced twice: %+v", state)
	}
	if retry := (*packets)[len(*packets)-1]; retry.Seq != 1 || retry.ECN.Codepoint != model.NotECT {
		t.Fatalf("unexpected ECN SACK repair: %+v", retry)
	}
	// Dropping that real repair still allows the ordinary RTO response.
	if err := e.RunUntil(sender.rto.state.CurrentRTO); err != nil {
		t.Fatal(err)
	}
	if state := sender.GetCongestionState(); state.CongestionWindowBytes != model.SEGMENT_SIZE || state.Phase != "slowStart" {
		t.Fatalf("RTO after ECN did not act as loss: %+v", state)
	}
}

// At one MSS, successful CE delivery delays NEW payload by the retransmission
// interval; persistent CE doubles the delay without a fake retry or loss count.
func TestECNAtOneMSSBacksOffNewPayload(t *testing.T) {
	e, sender, packets := ecnTestFlight(t, 1, 4, false)
	defer sender.Shutdown()
	sender.HandlePacket(ecnTestACK(sender, 1025, model.FlagECE))
	if err := e.RunUntil(999 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if len(*packets) != 2 { // SYN and one original payload.
		t.Fatalf("one-MSS ECE sent before RTO elapsed: %+v", *packets)
	}
	if err := e.RunUntil(time.Second); err != nil {
		t.Fatal(err)
	}
	if len(*packets) != 3 || (*packets)[2].Seq != 1025 || (*packets)[2].Flag&model.FlagCWR == 0 {
		t.Fatalf("RTO hold did not release new CWR payload: %+v", *packets)
	}
	sender.HandlePacket(ecnTestACK(sender, 2049, model.FlagECE))
	// A clean ACK reordered from the previous flight cannot reset CE backoff.
	sender.HandlePacket(ecnTestACK(sender, 1025, 0))
	if sender.ecn.holdRTO != 2*time.Second {
		t.Fatal("stale clean feedback reset persistent-congestion backoff")
	}
	if err := e.RunUntil(2999 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if len(*packets) != 3 {
		t.Fatal("persistent one-MSS CE did not back off to two seconds")
	}
	if err := e.RunUntil(3 * time.Second); err != nil {
		t.Fatal(err)
	}
	if len(*packets) != 4 || sender.GetTransferState().DataRetransmissions != 0 {
		t.Fatalf("ECN delay fabricated a retry: packets=%+v state=%+v", *packets, sender.GetState())
	}
	sender.HandlePacket(ecnTestACK(sender, 3073, model.FlagECE))
	if sender.ecn.holdRTO != 4*time.Second {
		t.Fatal("persistent congestion did not retain exponential backoff after stale feedback")
	}
}

// A repeated valid ECE resets the minimum-window timer even if its first copy
// already confirmed the whole flight; only unsent payload needs this hold.
func TestECNOneMSSDuplicateFeedbackRefreshesDrainedFlightHold(t *testing.T) {
	e, sender, packets := ecnTestFlight(t, 1, 2, false)
	defer sender.Shutdown()
	ack := ecnTestACK(sender, 1025, model.FlagECE)
	sender.HandlePacket(ack)
	if err := e.RunUntil(900 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	sender.HandlePacket(ack)
	if err := e.RunUntil(1899 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if len(*packets) != 2 {
		t.Fatal("drained-flight ECE did not refresh the one-MSS hold")
	}
	if err := e.RunUntil(1900 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if len(*packets) != 3 || sender.GetECNState().ECNResponseCount != 1 {
		t.Fatalf("a duplicate ECE reduced again or failed to release data: %+v", sender.GetECNState())
	}
	// Final delivery has no new payload to hold, so graceful close remains immediate.
	sender.HandlePacket(ecnTestACK(sender, 2049, model.FlagECE))
	if sender.ecn.holdEvent != 0 || !sender.connection.finSent {
		t.Fatal("final ECE delivery retained a useless new-data hold")
	}
}

// Once the repair itself is lost, RTO is a new congestion event. A smaller
// outstanding flight must supply a fresh threshold, despite earlier ECN.
func TestECNLostFastRetransmissionUsesFreshTimeoutThreshold(t *testing.T) {
	e, sender, _ := ecnTestFlight(t, 8, 8, false)
	defer sender.Shutdown()
	ack := ecnTestACK(sender, 6145, model.FlagECE)
	sender.HandlePacket(ack) // Eight-MSS pre-ACK flight gives ssthresh4; two MSS remain.
	if sender.GetCongestionState().SlowStartThresholdBytes != 4*model.SEGMENT_SIZE {
		t.Fatal("expected an initial four-MSS ECN threshold")
	}
	for count := 0; count < 3; count++ {
		sender.HandlePacket(ack)
	}
	if sender.GetCongestionState().FastRetransmissions != 1 {
		t.Fatal("the real hole was not repaired after ECN")
	}
	if err := e.RunUntil(time.Second); err != nil {
		t.Fatal(err)
	}
	if state := sender.GetCongestionState(); state.SlowStartThresholdBytes != 2*model.SEGMENT_SIZE ||
		state.CongestionWindowBytes != model.SEGMENT_SIZE {
		t.Fatalf("loss of the repair reused the ECN threshold: %+v", state)
	}
}

func ecnTestFlight(t *testing.T, initialSegments, totalSegments int, sack bool) (*engine.Engine, *Sender, *[]model.Packet) {
	t.Helper()
	e := engine.New(10 * time.Second)
	sender := newTestSender(e)
	config := model.DefaultCongestionConfig()
	config.InitialWindowSegments = initialSegments
	sender.SetCongestionConfig(config)
	sender.SetECNEnabled(true)
	sender.SetSACKEnabled(sack)
	sender.SetIdleTimeout(0)
	rto := model.DefaultRTOConfig()
	rto.Mode = model.RTOFixed
	rto.InitialTimeout = time.Second
	rto.MaximumTimeout = 8 * time.Second
	sender.SetRTOConfig(rto)
	var packets []model.Packet
	sender.SetSender(func(pkt model.Packet) { packets = append(packets, pkt) })
	sender.SetSource(newTestPayloadSource(e, totalSegments*model.SEGMENT_SIZE, 0))
	sender.EstablishConnection()
	if err := e.RunUntil(0); err != nil {
		t.Fatal(err)
	}
	sender.HandlePacket(model.Packet{
		Key: sender.Key(), Flag: model.FlagSYN | model.FlagACK | model.FlagECE,
		Ack: 1, ReceiveWindowBytes: 65535, SACKPermitted: sack,
	})
	// Omit the final standalone ACK from packet observations below.
	packets = packets[:1]
	if err := e.RunUntil(0); err != nil {
		t.Fatal(err)
	}
	return e, sender, &packets
}

func ecnTestACK(sender *Sender, ack uint64, flags model.Flag) model.Packet {
	return model.Packet{
		Key: sender.Key(), Flag: model.FlagACK | flags, Seq: 1,
		Ack: ack, ReceiveWindowBytes: 65535,
	}
}
