package network

import (
	"testing"
	"time"

	"load-balancer/engine"
)

func TestLinkRetireCancelsQueuedTransmittingAndTravellingPackets(t *testing.T) {
	e := engine.New(30 * time.Millisecond)
	link := NewLink(e, Config{
		BandwidthBitsPerSecond: 8_000,
		PacketTravelTime:       10 * time.Millisecond,
		QueueCapacity:          2,
	}, nil)
	arrivals := 0
	for range 3 {
		if !link.Transmit(1, func() { arrivals++ }) {
			t.Fatal("packet should be admitted")
		}
	}
	runLinkUntil(t, e, time.Millisecond)
	before := link.Stats()
	if !before.Transmitting || before.TravellingPackets != 1 || before.QueuedPackets != 1 {
		t.Fatalf("expected all three forms of unfinished work: %+v", before)
	}

	link.Retire()
	link.Retire()
	before.Transmitting = false
	before.TravellingPackets = 0
	before.QueuedPackets = 0
	if link.Ready() || link.Stats() != before || e.Pending() != 0 {
		t.Fatalf("retirement must clear work and preserve history: %+v, pending=%d", link.Stats(), e.Pending())
	}
	if link.Transmit(1, func() { arrivals++ }) || link.TryStart(1, func() { arrivals++ }) {
		t.Fatal("retired link admitted new work")
	}
	runLinkUntil(t, e, e.EndTime())
	if arrivals != 0 || link.Stats() != before {
		t.Fatalf("retired callbacks changed history: arrivals=%d stats=%+v", arrivals, link.Stats())
	}
}

func TestLinkRetireAtSerializationBoundary(t *testing.T) {
	e := engine.New(2 * time.Millisecond)
	link := NewLink(e, Config{BandwidthBitsPerSecond: 8_000}, nil)
	link.Transmit(1, func() { t.Error("retired packet arrived") })
	e.ScheduleConfigurationAt(time.Millisecond, link.Retire)
	runLinkUntil(t, e, e.EndTime())
	stats := link.Stats()
	if stats.AcceptedPackets != 1 || stats.TransmittedBytes != 0 || stats.DeliveredPackets != 0 {
		t.Fatalf("configuration must cancel ordinary completion at the same time: %+v", stats)
	}
}

func TestOutputRetirePreservesBudgetAndOtherOutputs(t *testing.T) {
	e := engine.New(30 * time.Nanosecond)
	egress := NewEgress(e, 8_000_000_000)
	retiring := egress.AddOutput(NewLink(e, Config{
		BandwidthBitsPerSecond: 8_000_000_000,
		PacketTravelTime:       10 * time.Nanosecond,
		QueueCapacity:          3,
	}, nil))
	waiting := egress.AddOutput(egressLink(e, 1))
	surviving := egress.AddOutput(egressLink(e, 1))
	onRetiredArrival := func() { t.Error("retired output delivered a packet") }
	retiring.Transmit(1, onRetiredArrival)
	retiring.Transmit(1, onRetiredArrival)
	retiring.Transmit(1, onRetiredArrival)
	waiting.Transmit(2, onRetiredArrival)
	var survivorArrival time.Duration
	surviving.Transmit(1, func() { survivorArrival = e.Now() })
	if waiting.readyIndex < 0 {
		t.Fatal("waiting output should be present in the ready heap")
	}
	waitingBefore := waiting.Stats()
	waiting.Retire()
	waiting.Retire()
	waitingBefore.QueuedPackets = 0
	if waiting.readyIndex != -1 || waiting.Stats() != waitingBefore || egress.Stats().QueuedPackets != 3 {
		t.Fatalf("queued retirement corrupted dispatcher state: %+v", egress.Stats())
	}

	runEgress(t, e, time.Nanosecond)
	before := retiring.Stats()
	if !before.Transmitting || before.TravellingPackets != 1 || before.QueuedPackets != 1 {
		t.Fatalf("expected active, travelling and queued packets: %+v", before)
	}
	retiring.Retire()
	retiring.Retire()
	before.Transmitting = false
	before.TravellingPackets = 0
	before.QueuedPackets = 0
	if retiring.Transmit(1, onRetiredArrival) || waiting.Transmit(1, onRetiredArrival) {
		t.Fatal("retired output admitted work")
	}
	if egress.nextGrantAt != 2*time.Nanosecond || egress.Stats().QueuedPackets != 1 {
		t.Fatalf("retirement refunded a charged grant or lost another queue: %+v", egress.Stats())
	}

	runEgress(t, e, e.EndTime())
	stats := egress.Stats()
	if survivorArrival != 3*time.Nanosecond || stats.ForwardedPackets != 3 || stats.ForwardedBytes != 3 {
		t.Fatalf("surviving output did not preserve the charged grant: arrival=%s stats=%+v", survivorArrival, stats)
	}
	if stats.QueuedPackets != 0 || stats.BudgetBlocked || stats.BudgetWait != 2*time.Nanosecond {
		t.Fatalf("retirement left invalid queue or budget accounting: %+v", stats)
	}
	if retiring.Stats() != before || waiting.Stats() != waitingBefore || egress.readyOutputs.Len() != 0 {
		t.Fatal("retired work reappeared or changed historical counters")
	}
}

func TestOutputRetireDoesNotDispatchWithinConfiguration(t *testing.T) {
	e := engine.New(3 * time.Nanosecond)
	egress := NewEgress(e, 8_000_000_000)
	first := egress.AddOutput(egressLink(e, 1))
	second := egress.AddOutput(egressLink(e, 1))
	third := egress.AddOutput(egressLink(e, 1))
	first.Transmit(1, func() {})
	second.Transmit(1, func() { t.Error("second retired output arrived") })
	third.Transmit(1, func() { t.Error("third retired output arrived") })
	e.ScheduleConfigurationAt(time.Nanosecond, func() {
		second.Retire()
		third.Retire()
	})
	runEgress(t, e, e.EndTime())
	stats := egress.Stats()
	if stats.ForwardedPackets != 1 || stats.ForwardedBytes != 1 || stats.QueuedPackets != 0 {
		t.Fatalf("retirement started another output before the configuration finished: %+v", stats)
	}
}
