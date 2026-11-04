package tcp

import (
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
)

// ECE at the old deadline must postpone the next send; an expired hold is not
// permission to ignore new feedback while the workload still has data waiting.
func TestECNOneMSSFeedbackAtExpiredHold(t *testing.T) {
	for _, sourceGap := range []bool{false, true} {
		name := "at deadline"
		if sourceGap {
			name = "after deadline before source release"
		}
		t.Run(name, func(t *testing.T) {
			e, sender, packets := ecnTestFlight(t, 1, 2, false)
			defer sender.Shutdown()
			ack := ecnTestACK(sender, 1025, model.FlagECE)
			feedbackAt := time.Second
			if sourceGap {
				// Only the workload gap prevents sending after the first hold expires.
				sender.source.(*testPayloadSource).readyAt = 5 * time.Second
				feedbackAt = 4500 * time.Millisecond
			}

			// Schedule feedback first so it runs before the same-time hold callback.
			e.ScheduleAt(feedbackAt, func() { sender.HandlePacket(ack) })
			sender.HandlePacket(ack)
			newDeadline := feedbackAt + time.Second
			if err := e.RunUntil(newDeadline - time.Nanosecond); err != nil {
				t.Fatal(err)
			}
			if len(*packets) != 2 {
				t.Fatal("ECE failed to postpone the next new payload")
			}

			if err := e.RunUntil(newDeadline); err != nil {
				t.Fatal(err)
			}
			if len(*packets) != 3 || sender.GetECNState().ECNResponseCount != 1 {
				t.Fatalf("new deadline did not release data exactly once: %+v", sender.GetECNState())
			}
		})
	}
}

// Near the largest virtual timestamp, a hold beyond the run cannot wrap around
// or be clamped into permission to send one last packet at the horizon.
func TestECNHoldBeyondMaximumTimeDoesNotSendAtHorizon(t *testing.T) {
	end := time.Duration(1<<63 - 1)
	e := engine.New(end)
	sender := newTestSender(e)
	defer sender.Shutdown()
	sender.SetECNEnabled(true)
	sender.SetIdleTimeout(0)
	config := model.DefaultCongestionConfig()
	config.InitialWindowSegments = 1
	sender.SetCongestionConfig(config)
	rto := model.DefaultRTOConfig()
	rto.Mode = model.RTOFixed
	sender.SetRTOConfig(rto)
	var payloadPackets int
	sender.SetSender(func(pkt model.Packet) {
		if pkt.Size > 0 {
			payloadPackets++
		}
	})
	sender.EstablishConnection()
	if err := e.RunUntil(0); err != nil {
		t.Fatal(err)
	}
	sender.cancelEvent(&sender.synTimer)
	if err := e.RunUntil(end - time.Millisecond); err != nil {
		t.Fatal(err)
	}
	sender.HandlePacket(model.Packet{Key: sender.Key(), Flag: model.FlagSYN | model.FlagACK | model.FlagECE,
		Ack: 1, ReceiveWindowBytes: 65535})
	sender.SetSource(newTestPayloadSource(e, 2*model.SEGMENT_SIZE, 0))
	if err := e.RunUntil(e.Now()); err != nil {
		t.Fatal(err)
	}
	sender.HandlePacket(ecnTestACK(sender, 1025, model.FlagECE))
	if err := e.RunUntil(end); err != nil {
		t.Fatal(err)
	}
	if payloadPackets != 1 || !sender.ecn.holdBeyondRun {
		t.Fatalf("out-of-run ECN delay released new data: count=%d ECN=%+v", payloadPackets, sender.ecn)
	}
}
