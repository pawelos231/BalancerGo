package network

import (
	"load-balancer/engine"
	"testing"
	"time"
)

// TestLinkReadyBeforeArrival checks that travel does not occupy the transmitter.
func TestLinkReadyBeforeArrival(t *testing.T) {
	e := engine.New(20 * time.Millisecond)
	link := NewLink(e, Config{
		BandwidthBitsPerSecond: 8_000,
		PacketTravelTime:       10 * time.Millisecond,
	}, nil)
	var arrivals []time.Duration
	onArrival := func() { arrivals = append(arrivals, e.Now()) }

	if !link.Ready() || !link.TryStart(1, onArrival) || link.Ready() {
		t.Fatal("an idle transmitter should accept a packet and become busy")
	}
	runLinkUntil(t, e, time.Millisecond)
	if !link.Ready() || link.Stats().TravellingPackets != 1 || len(arrivals) != 0 {
		t.Fatalf("transmitter should be ready while the first packet travels: %+v", link.Stats())
	}
	if !link.TryStart(2, onArrival) {
		t.Fatal("a travelling packet must not block the next transmission")
	}
	runLinkUntil(t, e, 3*time.Millisecond)
	if !link.Ready() || link.Stats().TravellingPackets != 2 {
		t.Fatalf("both packets should be travelling: %+v", link.Stats())
	}
	runLinkUntil(t, e, 13*time.Millisecond)
	if len(arrivals) != 2 || arrivals[0] != 11*time.Millisecond || arrivals[1] != 13*time.Millisecond {
		t.Fatalf("unexpected arrival times: %v", arrivals)
	}
	stats := link.Stats()
	if stats.DeliveredPackets != 2 || stats.DeliveredBytes != 3 || stats.TravellingPackets != 0 {
		t.Fatalf("unexpected delivery counters: %+v", stats)
	}
}

// TestLinkTryStartBusyDoesNotQueue checks that retrying a busy output is harmless.
func TestLinkTryStartBusyDoesNotQueue(t *testing.T) {
	e := engine.New(10 * time.Millisecond)
	link := NewLink(e, Config{BandwidthBitsPerSecond: 8_000, QueueCapacity: 100}, nil)
	arrivals := 0
	if !link.TryStart(1, func() { arrivals++ }) {
		t.Fatal("first packet should start")
	}
	before := link.Stats()
	pending := e.Pending()
	if link.TryStart(99, func() { t.Error("busy attempt must never arrive") }) {
		t.Fatal("busy transmitter must reject an immediate start")
	}
	if link.Stats() != before || e.Pending() != pending {
		t.Fatalf("busy attempt changed counters or events: before=%+v after=%+v", before, link.Stats())
	}
	runLinkUntil(t, e, e.EndTime())
	if arrivals != 1 || link.Stats().QueuedPackets != 0 || !link.Ready() {
		t.Fatalf("busy attempt created hidden work: arrivals=%d stats=%+v", arrivals, link.Stats())
	}
}

// TestLinkTryStartValidatesBusyAttempts keeps invalid input independent of readiness.
func TestLinkTryStartValidatesBusyAttempts(t *testing.T) {
	for _, state := range []string{"idle", "busy"} {
		t.Run(state, func(t *testing.T) {
			e := engine.New(time.Second)
			link := NewLink(e, Config{BandwidthBitsPerSecond: 8_000}, nil)
			if state == "busy" {
				link.TryStart(1, func() {})
			}
			before := link.Stats()
			expectLinkPanic(t, func() { link.TryStart(-1, func() {}) })
			expectLinkPanic(t, func() { link.TryStart(1, nil) })
			if link.Stats() != before {
				t.Fatal("invalid input changed counters")
			}
		})
	}
}

// TestManagedLinkNotifiesAfterSerialization checks ownership and callback ordering.
func TestManagedLinkNotifiesAfterSerialization(t *testing.T) {
	e := engine.New(20 * time.Millisecond)
	link := NewLink(e, Config{
		BandwidthBitsPerSecond: 8_000,
		PacketTravelTime:       10 * time.Millisecond,
	}, nil)
	readyCalls := 0
	arrivals := 0
	link.useExternalQueue(func() {
		readyCalls++
		if !link.Ready() || link.Stats().TravellingPackets != readyCalls {
			t.Errorf("readiness must follow travel scheduling: %+v", link.Stats())
		}
		if readyCalls == 1 && !link.TryStart(1, func() { arrivals++ }) {
			t.Error("manager should be able to start the next packet immediately")
		}
	})
	if readyCalls != 0 {
		t.Fatal("binding a manager must not invoke it")
	}
	expectLinkPanic(t, func() { link.Transmit(1, func() {}) })
	expectLinkPanic(t, func() { link.useExternalQueue(func() {}) })
	if !link.TryStart(1, func() { arrivals++ }) || readyCalls != 0 || arrivals != 0 {
		t.Fatal("starting a packet must not invoke callbacks synchronously")
	}
	runLinkUntil(t, e, 2*time.Millisecond)
	if readyCalls != 2 || arrivals != 0 || link.Stats().QueuedPackets != 0 {
		t.Fatalf("manager should own all waiting work: ready=%d arrivals=%d stats=%+v", readyCalls, arrivals, link.Stats())
	}
	runLinkUntil(t, e, 12*time.Millisecond)
	if arrivals != 2 || link.Stats().OfferedPackets != 2 || link.Stats().AcceptedPackets != 2 {
		t.Fatalf("successful starts should be counted once: arrivals=%d stats=%+v", arrivals, link.Stats())
	}
}

// TestManagedLinkRequiresFreshOwnership rejects binding after any earlier traffic.
func TestManagedLinkRequiresFreshOwnership(t *testing.T) {
	for _, state := range []string{"transmitting", "queued", "travelling", "completed"} {
		t.Run(state, func(t *testing.T) {
			e := engine.New(10 * time.Millisecond)
			link := NewLink(e, Config{
				BandwidthBitsPerSecond: 8_000,
				PacketTravelTime:       time.Millisecond,
				QueueCapacity:          1,
			}, nil)
			link.Transmit(1, func() {})
			switch state {
			case "queued":
				link.Transmit(1, func() {})
			case "travelling":
				runLinkUntil(t, e, time.Millisecond)
			case "completed":
				runLinkUntil(t, e, 2*time.Millisecond)
			}
			expectLinkPanic(t, func() { link.useExternalQueue(func() {}) })
		})
	}
	e := engine.New(time.Second)
	link := NewLink(e, Config{BandwidthBitsPerSecond: 8_000}, nil)
	expectLinkPanic(t, func() { link.useExternalQueue(nil) })
	link.useExternalQueue(func() {})
}

// TestManagedLinkLossStillReleasesOutput checks that loss consumes transmission time.
func TestManagedLinkLossStillReleasesOutput(t *testing.T) {
	e := engine.New(time.Millisecond)
	link := NewLink(e, Config{BandwidthBitsPerSecond: 8_000, LossRate: 1}, nil)
	readyCalls := 0
	link.useExternalQueue(func() {
		readyCalls++
		if !link.Ready() || link.Stats().LostPackets != 1 {
			t.Errorf("loss must be accounted before notifying the manager: %+v", link.Stats())
		}
	})
	link.TryStart(1, func() { t.Error("lost packet must not arrive") })
	runLinkUntil(t, e, e.EndTime())
	stats := link.Stats()
	if readyCalls != 1 || stats.LostBytes != 1 || stats.DeliveredPackets != 0 || stats.TravellingPackets != 0 {
		t.Fatalf("unexpected loss lifecycle: ready=%d stats=%+v", readyCalls, stats)
	}
}

// TestLinkZeroAndHorizon keeps zero-time delivery and hard end-time behavior explicit.
func TestLinkZeroAndHorizon(t *testing.T) {
	cases := []struct {
		name       string
		end        time.Duration
		size       int
		travel     time.Duration
		readyCalls int
		arrivals   int
		ready      bool
		travelling int
	}{
		{name: "zero size at zero horizon", readyCalls: 1, arrivals: 1, ready: true},
		{name: "serialization beyond horizon", size: 1},
		{name: "arrival at horizon", end: time.Millisecond, size: 1, readyCalls: 1, arrivals: 1, ready: true},
		{name: "travel beyond horizon", end: time.Millisecond, size: 1, travel: time.Millisecond, readyCalls: 1, ready: true, travelling: 1},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			e := engine.New(test.end)
			link := NewLink(e, Config{BandwidthBitsPerSecond: 8_000, PacketTravelTime: test.travel}, nil)
			readyCalls, arrivals := 0, 0
			link.useExternalQueue(func() { readyCalls++ })
			if !link.TryStart(test.size, func() { arrivals++ }) {
				t.Fatal("a free transmitter should start even when completion exceeds the horizon")
			}
			if readyCalls != 0 || arrivals != 0 {
				t.Fatal("callbacks must wait for engine execution")
			}
			runLinkUntil(t, e, test.end)
			stats := link.Stats()
			if readyCalls != test.readyCalls || arrivals != test.arrivals || link.Ready() != test.ready || stats.TravellingPackets != test.travelling {
				t.Fatalf("unexpected horizon state: readyCalls=%d arrivals=%d ready=%v stats=%+v", readyCalls, arrivals, link.Ready(), stats)
			}
			if stats.AcceptedPackets != 1 || stats.QueueDroppedPackets != 0 {
				t.Fatalf("the horizon must not turn an accepted packet into a queue drop: %+v", stats)
			}
		})
	}
}

// TestAutonomousLinkKeepsFIFOAndTailDrop protects the existing ingress behavior.
func TestAutonomousLinkKeepsFIFOAndTailDrop(t *testing.T) {
	e := engine.New(5 * time.Millisecond)
	link := NewLink(e, Config{
		BandwidthBitsPerSecond: 8_000,
		PacketTravelTime:       time.Millisecond,
		QueueCapacity:          1,
	}, nil)
	var arrivals []time.Duration
	onArrival := func() { arrivals = append(arrivals, e.Now()) }
	if !link.Transmit(1, onArrival) || !link.Transmit(2, onArrival) {
		t.Fatal("one active and one waiting packet should be admitted")
	}
	if link.Transmit(1, func() { t.Error("tail-dropped packet must not arrive") }) {
		t.Fatal("full waiting queue should tail-drop the third packet")
	}
	stats := link.Stats()
	if stats.QueuedPackets != 1 || stats.OfferedPackets != 3 || stats.AcceptedPackets != 2 || stats.QueueDroppedPackets != 1 {
		t.Fatalf("unexpected autonomous admission counters: %+v", stats)
	}
	runLinkUntil(t, e, 4*time.Millisecond)
	if len(arrivals) != 2 || arrivals[0] != 2*time.Millisecond || arrivals[1] != 4*time.Millisecond {
		t.Fatalf("unexpected FIFO arrival times: %v", arrivals)
	}
	if !link.Ready() || link.Stats().QueuedPackets != 0 {
		t.Fatal("autonomous queue should drain without an external manager")
	}
}

// runLinkUntil advances a test engine and reports execution failures at its caller.
func runLinkUntil(t *testing.T, e *engine.Engine, until time.Duration) {
	t.Helper()
	if err := e.RunUntil(until); err != nil {
		t.Fatal(err)
	}
}

// expectLinkPanic checks API misuse without depending on panic message wording.
func expectLinkPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Error("expected a panic")
		}
	}()
	fn()
}
