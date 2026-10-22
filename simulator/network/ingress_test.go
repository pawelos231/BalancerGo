package network

import (
	"math"
	"math/rand"
	"reflect"
	"strconv"
	"testing"
	"time"

	"load-balancer/engine"
)

// TestIngressOldestHead checks one shared rate and admission order across FIFOs.
func TestIngressOldestHead(t *testing.T) {
	e := engine.New(10 * time.Millisecond)
	link := NewLink(e, Config{
		BandwidthBitsPerSecond: 8_000,
		QueueCapacity:          3,
	}, nil)
	ingress := NewIngress(link)
	clients := ingress.AddInput()
	backends := ingress.AddInput()

	var arrivals []string
	record := func(name string) func() {
		return func() {
			arrivals = append(arrivals, name+"@"+e.Now().String())
		}
	}

	// The backend head is older even though its input was registered second.
	clients.Transmit(1, record("client-1"))
	backends.Transmit(1, record("backend-1"))
	clients.Transmit(1, record("client-2"))
	backends.Transmit(1, record("backend-2"))
	clients.Transmit(1, record("client-3"))

	if !clients.Stats().Transmitting || backends.Stats().Transmitting {
		t.Fatal("only the first input should occupy the shared transmitter")
	}
	if ingress.Stats().QueuedPackets != 4 || link.Stats().QueuedPackets != 0 {
		t.Fatal("waiting packets must belong to input FIFOs, not a hidden link queue")
	}
	runLinkUntil(t, e, e.EndTime())

	expected := []string{
		"client-1@1ms",
		"backend-1@2ms",
		"client-2@3ms",
		"backend-2@4ms",
		"client-3@5ms",
	}
	if !reflect.DeepEqual(arrivals, expected) {
		t.Fatalf("arrivals = %v, want %v", arrivals, expected)
	}
	if stats := ingress.Stats(); stats != (Stats{
		OfferedPackets:   5,
		OfferedBytes:     5,
		AcceptedPackets:  5,
		AcceptedBytes:    5,
		DeliveredPackets: 5,
		DeliveredBytes:   5,
	}) {
		t.Fatalf("unexpected shared counters: %+v", stats)
	}
	if clients.Stats().DeliveredPackets != 3 || backends.Stats().DeliveredPackets != 2 {
		t.Fatal("deliveries must remain attributed to their source input")
	}
	if e.EventsExecuted() != 10 || e.Pending() != 0 {
		t.Fatal("ingress should add no events beyond link completion and arrival")
	}
}

// TestIngressIndependentCapacity checks tail drop and separate waiting limits.
func TestIngressIndependentCapacity(t *testing.T) {
	for _, capacity := range []int{0, 1} {
		t.Run(strconv.Itoa(capacity), func(t *testing.T) {
			e := engine.New(20 * time.Millisecond)
			ingress := NewIngress(NewLink(e, Config{
				BandwidthBitsPerSecond: 8_000,
				QueueCapacity:          capacity,
			}, nil))
			clients := ingress.AddInput()
			backends := ingress.AddInput()
			arrive := func() {}

			if !clients.Transmit(1, arrive) {
				t.Fatal("an idle transmitter must accept a packet without a waiting slot")
			}
			if accepted := backends.Transmit(2, arrive); accepted != (capacity == 1) {
				t.Fatal("backend admission did not follow its own waiting capacity")
			}
			if backends.Transmit(3, arrive) {
				t.Fatal("a full backend FIFO must drop its next packet")
			}
			if accepted := clients.Transmit(4, arrive); accepted != (capacity == 1) {
				t.Fatal("a full backend FIFO must not consume the client waiting slot")
			}
			if clients.Transmit(5, arrive) {
				t.Fatal("a full client FIFO must drop its next packet")
			}

			clientStats := clients.Stats()
			backendStats := backends.Stats()
			stats := ingress.Stats()
			if stats.OfferedPackets != 5 || stats.OfferedBytes != 15 {
				t.Fatalf("every offer must be counted once: %+v", stats)
			}
			if clientStats.OfferedPackets != 3 || clientStats.OfferedBytes != 10 {
				t.Fatalf("unexpected client offers: %+v", clientStats)
			}
			if backendStats.OfferedPackets != 2 || backendStats.OfferedBytes != 5 {
				t.Fatalf("unexpected backend offers: %+v", backendStats)
			}

			if capacity == 0 {
				if stats.AcceptedPackets != 1 || stats.AcceptedBytes != 1 || stats.QueuedPackets != 0 {
					t.Fatalf("zero capacity should retain only the active packet: %+v", stats)
				}
				if stats.QueueDroppedPackets != 4 || stats.QueueDroppedBytes != 14 {
					t.Fatalf("unexpected zero-capacity drops: %+v", stats)
				}
				if clientStats.QueueDroppedBytes != 9 || backendStats.QueueDroppedBytes != 5 {
					t.Fatal("drops must be attributed to their own input")
				}
			} else {
				if stats.AcceptedPackets != 3 || stats.AcceptedBytes != 7 || stats.QueuedPackets != 2 {
					t.Fatalf("each input should retain one waiting packet: %+v", stats)
				}
				if stats.QueueDroppedPackets != 2 || stats.QueueDroppedBytes != 8 {
					t.Fatalf("unexpected independent-FIFO drops: %+v", stats)
				}
				if clientStats.QueueDroppedBytes != 5 || backendStats.QueueDroppedBytes != 3 {
					t.Fatal("drops must be attributed to their own input")
				}
			}

			before := ingress.Stats()
			pending := e.Pending()
			if ingress.Stats() != before || e.Pending() != pending {
				t.Fatal("reading ingress statistics must not schedule work")
			}
			runLinkUntil(t, e, e.EndTime())
			if stats := ingress.Stats(); stats.DeliveredPackets != stats.AcceptedPackets || stats.QueuedPackets != 0 {
				t.Fatalf("accepted packets should drain without hidden queues: %+v", stats)
			}
		})
	}
}

// TestIngressTravelOverlap checks zero-capacity reuse before earlier arrivals.
func TestIngressTravelOverlap(t *testing.T) {
	e := engine.New(20 * time.Millisecond)
	ingress := NewIngress(NewLink(e, Config{
		BandwidthBitsPerSecond: 8_000,
		PacketTravelTime:       10 * time.Millisecond,
	}, nil))
	clients := ingress.AddInput()
	backends := ingress.AddInput()
	var arrivals []time.Duration
	record := func(input *Input) func() {
		return func() {
			stats := input.Stats()
			if stats.TravellingPackets != 0 || stats.DeliveredPackets != 1 {
				t.Errorf("arrival must update its own counters before calling the receiver: %+v", stats)
			}
			arrivals = append(arrivals, e.Now())
		}
	}

	clients.Transmit(1, record(clients))
	runLinkUntil(t, e, time.Millisecond)
	if !backends.Transmit(1, record(backends)) {
		t.Fatal("a travelling client packet must not block the backend transmission")
	}
	runLinkUntil(t, e, 2*time.Millisecond)
	if clients.Stats().TravellingPackets != 1 || backends.Stats().TravellingPackets != 1 {
		t.Fatal("each input should retain its own travelling packet")
	}
	if stats := ingress.Stats(); stats.TravellingPackets != 2 || stats.Transmitting || stats.QueuedPackets != 0 {
		t.Fatalf("shared transmitter should be idle while both packets travel: %+v", stats)
	}
	runLinkUntil(t, e, e.EndTime())
	if !reflect.DeepEqual(arrivals, []time.Duration{11 * time.Millisecond, 12 * time.Millisecond}) {
		t.Fatalf("travel or serialization was counted twice: %v", arrivals)
	}
}

// TestIngressLossAttribution checks that lost packets still occupy the shared wire.
func TestIngressLossAttribution(t *testing.T) {
	e := engine.New(10 * time.Millisecond)
	ingress := NewIngress(NewLink(e, Config{
		BandwidthBitsPerSecond: 8_000,
		QueueCapacity:          2,
		LossRate:               1,
	}, nil))
	clients := ingress.AddInput()
	backends := ingress.AddInput()
	arrive := func() { t.Error("a lost packet must not arrive") }
	clients.Transmit(2, arrive)
	backends.Transmit(3, arrive)
	clients.Transmit(1, arrive)

	runLinkUntil(t, e, 2*time.Millisecond)
	if clients.Stats().LostPackets != 1 || clients.Stats().LostBytes != 2 || !backends.Stats().Transmitting {
		t.Fatal("the first loss must finish before the backend starts")
	}
	runLinkUntil(t, e, 5*time.Millisecond)
	if backends.Stats().LostPackets != 1 || backends.Stats().LostBytes != 3 || !clients.Stats().Transmitting {
		t.Fatal("backend loss must be attributed before the next client transmission")
	}
	runLinkUntil(t, e, e.EndTime())
	if stats := clients.Stats(); stats.LostPackets != 2 || stats.LostBytes != 3 || stats.DeliveredPackets != 0 {
		t.Fatalf("unexpected client loss counters: %+v", stats)
	}
	if stats := ingress.Stats(); stats.LostPackets != 3 || stats.LostBytes != 6 || stats.TravellingPackets != 0 || stats.Transmitting {
		t.Fatalf("unexpected shared loss counters: %+v", stats)
	}
	if e.EventsExecuted() != 3 || e.Pending() != 0 {
		t.Fatal("loss should consume transmission time without adding arrival events")
	}
}

// TestIngressSeededLoss checks that source attribution does not consume extra RNG draws.
func TestIngressSeededLoss(t *testing.T) {
	type result struct {
		arrivals []int
		inputs   [2]Stats
		shared   Stats
	}
	run := func() result {
		e := engine.New(20 * time.Millisecond)
		ingress := NewIngress(NewLink(e, Config{
			BandwidthBitsPerSecond: 8_000,
			QueueCapacity:          16,
			LossRate:               0.5,
		}, rand.New(rand.NewSource(42))))
		inputs := [2]*Input{ingress.AddInput(), ingress.AddInput()}
		var arrivals []int
		for index := 0; index < 16; index++ {
			inputs[index%2].Transmit(1, func() { arrivals = append(arrivals, index) })
		}
		runLinkUntil(t, e, e.EndTime())
		return result{
			arrivals: arrivals,
			inputs:   [2]Stats{inputs[0].Stats(), inputs[1].Stats()},
			shared:   ingress.Stats(),
		}
	}

	first := run()
	second := run()
	if !reflect.DeepEqual(first, second) {
		t.Fatal("fresh ingress runs with the same seed must have identical outcomes")
	}
	if first.shared.DeliveredPackets == 0 || first.shared.LostPackets == 0 {
		t.Fatal("the seeded case should exercise both loss and delivery")
	}

	expectedRNG := rand.New(rand.NewSource(42))
	var expectedArrivals []int
	for index := 0; index < 16; index++ {
		if expectedRNG.Float64() >= 0.5 {
			expectedArrivals = append(expectedArrivals, index)
		}
	}
	if !reflect.DeepEqual(first.arrivals, expectedArrivals) {
		t.Fatal("dispatcher must preserve one link-owned RNG draw per transmission")
	}
	for index, stats := range first.inputs {
		if stats.DeliveredPackets+stats.LostPackets != 8 || stats.DeliveredBytes+stats.LostBytes != 8 {
			t.Fatalf("input %d lost physical outcome attribution: %+v", index, stats)
		}
	}
}

// TestIngressFrozenAtEnd preserves queued, transmitting and travelling packets.
func TestIngressFrozenAtEnd(t *testing.T) {
	e := engine.New(2 * time.Millisecond)
	ingress := NewIngress(NewLink(e, Config{
		BandwidthBitsPerSecond: 8_000,
		PacketTravelTime:       2 * time.Millisecond,
		QueueCapacity:          2,
	}, nil))
	clients := ingress.AddInput()
	backends := ingress.AddInput()
	arrive := func() { t.Error("no packet should arrive before the end time") }
	clients.Transmit(1, arrive)
	backends.Transmit(2, arrive)
	clients.Transmit(3, arrive)
	backends.Transmit(4, arrive)
	runLinkUntil(t, e, e.EndTime())

	stats := ingress.Stats()
	if stats.QueuedPackets != 2 || !stats.Transmitting || stats.TravellingPackets != 1 {
		t.Fatalf("unfinished packets must remain frozen: %+v", stats)
	}
	if clients.Stats().QueuedPackets != 1 || clients.Stats().TravellingPackets != 1 || clients.Stats().Transmitting {
		t.Fatalf("unexpected frozen client state: %+v", clients.Stats())
	}
	if backends.Stats().QueuedPackets != 1 || !backends.Stats().Transmitting || backends.Stats().TravellingPackets != 0 {
		t.Fatalf("unexpected frozen backend state: %+v", backends.Stats())
	}
	if stats.LostPackets != 0 || stats.DeliveredPackets != 0 || e.Pending() != 0 {
		t.Fatal("the end time must not turn unfinished work into losses or late events")
	}
}

// TestIngressZeroBytes checks equal-time completion and arrival bookkeeping.
func TestIngressZeroBytes(t *testing.T) {
	e := engine.New(0)
	ingress := NewIngress(NewLink(e, Config{
		BandwidthBitsPerSecond: 8_000,
		QueueCapacity:          2,
	}, nil))
	clients := ingress.AddInput()
	backends := ingress.AddInput()
	var arrivals []int
	for index, input := range []*Input{clients, backends, clients, backends} {
		if !input.Transmit(0, func() {
			if input.Stats().TravellingPackets < 0 {
				t.Error("equal-time delivery must follow travel attribution")
			}
			arrivals = append(arrivals, index)
		}) {
			t.Fatal("zero-byte packets should use the same FIFO admission rules")
		}
	}
	if len(arrivals) != 0 {
		t.Fatal("zero-byte delivery must remain an engine event")
	}
	runLinkUntil(t, e, 0)
	if !reflect.DeepEqual(arrivals, []int{0, 1, 2, 3}) {
		t.Fatalf("equal-time packet order changed: %v", arrivals)
	}
	for _, input := range []*Input{clients, backends} {
		if stats := input.Stats(); stats.DeliveredPackets != 2 || stats.DeliveredBytes != 0 || stats.TravellingPackets != 0 || stats.Transmitting {
			t.Fatalf("unexpected zero-byte input counters: %+v", stats)
		}
	}
	if e.EventsExecuted() != 8 || e.Pending() != 0 {
		t.Fatal("zero-byte dispatch must not add recursion or extra events")
	}
}

// TestIngressValidation checks ownership and validation before counter changes.
func TestIngressValidation(t *testing.T) {
	expectLinkPanic(t, func() { NewIngress(nil) })
	e := engine.New(time.Second)
	link := NewLink(e, Config{BandwidthBitsPerSecond: 8_000}, nil)
	ingress := NewIngress(link)
	input := ingress.AddInput()
	if ingress.Config() != link.Config() {
		t.Fatal("ingress must expose the physical link configuration")
	}
	expectLinkPanic(t, func() { NewIngress(link) })
	expectLinkPanic(t, func() { link.Transmit(1, func() {}) })

	for _, state := range []string{"idle", "busy"} {
		t.Run(state, func(t *testing.T) {
			if state == "busy" {
				input.Transmit(1, func() {})
			}
			before := input.Stats()
			sharedBefore := ingress.Stats()
			pending := e.Pending()
			expectLinkPanic(t, func() { input.Transmit(-1, func() {}) })
			expectLinkPanic(t, func() { input.Transmit(1, nil) })
			expectLinkPanic(t, func() { input.Transmit(math.MaxInt, func() {}) })
			if input.Stats() != before || ingress.Stats() != sharedBefore || e.Pending() != pending {
				t.Fatal("invalid packets must not change counters or scheduled work")
			}
		})
	}

	used := NewLink(e, Config{BandwidthBitsPerSecond: 8_000}, nil)
	used.Transmit(1, func() {})
	expectLinkPanic(t, func() { NewIngress(used) })
}
