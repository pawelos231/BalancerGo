package simulation

import (
	"testing"
	"time"

	"load-balancer/model"
)

// Drop segments 2 and 4 once, retaining all access, ingress and backend links.
// NewReno repairs both gaps before RTO; received and confirmed payload remain unique.
func TestWorldNewRenoRepairsTwoLossesWithoutTimeout(t *testing.T) {
	definition := congestionScenario()
	mode := "newReno"
	initialSegments := 8
	definition.TCP.CongestionControl.Mode = &mode
	definition.TCP.CongestionControl.InitialWindowSegments = &initialSegments
	world := New(normalizedRuntimeScenario(t, definition))

	dropped := map[uint64]bool{1025: false, 3073: false}
	runtime := interceptCongestionTraffic(t, world, func(packet model.Packet) bool {
		alreadyDropped, target := dropped[packet.Seq]
		if packet.Size > 0 && target && !alreadyDropped {
			dropped[packet.Seq] = true
			return true
		}
		return false
	})

	// This ends before the minimum 200 ms RTO, so every retry was driven by ACKs.
	runIngressUntil(t, world, 100*time.Millisecond)
	state := runtime.Client.GetState()
	if !dropped[1025] || !dropped[3073] || state.Transfer.Status != model.TransferSucceeded ||
		state.Transfer.DataRetransmissions != 2 || state.Congestion.FastRetransmissions != 2 {
		t.Fatalf("two-loss NewReno transfer failed: %+v", state)
	}
	assertNativePayloadProgress(t, world, 12*model.SEGMENT_SIZE, 12*model.SEGMENT_SIZE)
}
