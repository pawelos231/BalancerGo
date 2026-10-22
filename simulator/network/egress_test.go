package network

import (
	"math"
	"reflect"
	"strconv"
	"testing"
	"time"

	"load-balancer/engine"
)

// egressLink creates a link with short, exact transmission times for these tests.
func egressLink(e *engine.Engine, capacity int) *Link {
	config := Config{
		BandwidthBitsPerSecond: 8_000_000_000,
		QueueCapacity:          capacity,
	}
	return NewLink(e, config, nil)
}

// runEgress advances virtual time and fails the test on an engine error.
func runEgress(t *testing.T, e *engine.Engine, until time.Duration) {
	t.Helper()
	if err := e.RunUntil(until); err != nil {
		t.Fatal(err)
	}
}

// expectEgressPanic checks that invalid arguments fail immediately.
func expectEgressPanic(t *testing.T, callback func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic")
		}
	}()
	callback()
}

// TestEgressIndependentBranches checks FIFO without blocking an unrelated link.
func TestEgressIndependentBranches(t *testing.T) {
	// Give each branch its own transmitter; the fast branch also waits for a grant.
	e := engine.New(4 * time.Second)
	dispatcher := NewEgress(e, 8_000_000_000)
	slow := dispatcher.AddOutput(NewLink(e, Config{
		BandwidthBitsPerSecond: 8,
		QueueCapacity:          1,
	}, nil))
	fast := dispatcher.AddOutput(NewLink(e, Config{
		BandwidthBitsPerSecond: 8,
		QueueCapacity:          2,
	}, nil))

	var arrivals []string
	record := func(name string) func() {
		return func() {
			arrivals = append(arrivals, name+"@"+e.Now().String())
		}
	}

	// The slow branch starts a longer packet before the fast branch sends.
	slow.Transmit(2, record("slow-1"))
	slow.Transmit(1, record("slow-2"))
	fast.Transmit(1, record("fast-1"))
	fast.Transmit(1, record("fast-2"))
	runEgress(t, e, 2*time.Nanosecond)

	if dispatcher.readyOutputs.Len() != 0 {
		t.Fatal("busy branches must stay outside the ready heap")
	}
	bothHeadsWaiting := slow.Stats().QueuedPackets == 1 && fast.Stats().QueuedPackets == 1
	if !bothHeadsWaiting {
		t.Fatal("busy branches should retain FIFO heads outside the ready heap")
	}

	runEgress(t, e, 4*time.Second)

	// Each branch keeps FIFO order while the fast branch progresses independently.
	expected := []string{
		"fast-1@1.000000002s",
		"slow-1@2s",
		"fast-2@2.000000002s",
		"slow-2@3s",
	}
	if !reflect.DeepEqual(arrivals, expected) {
		t.Fatalf("arrivals = %v, want %v", arrivals, expected)
	}
	if slow.link.Stats().QueuedPackets != 0 || fast.link.Stats().QueuedPackets != 0 {
		t.Fatal("managed links must not retain a second waiting queue")
	}
}

// TestEgressQueueCapacity checks that active transmissions do not use waiting slots.
func TestEgressQueueCapacity(t *testing.T) {
	for _, capacity := range []int{0, 1} {
		t.Run(strconv.Itoa(capacity), func(t *testing.T) {
			// Fill the transmitter, then offer more packets than the FIFO can hold.
			e := engine.New(10 * time.Nanosecond)
			output := NewEgress(e, 8_000_000_000).AddOutput(egressLink(e, capacity))
			arrivals := 0
			arrive := func() { arrivals++ }

			if !output.Transmit(2, arrive) {
				t.Fatal("an idle output should start even with zero waiting capacity")
			}
			secondAccepted := output.Transmit(3, arrive)
			if secondAccepted != (capacity == 1) {
				t.Fatal("second packet admission did not follow waiting capacity")
			}
			if output.Transmit(4, arrive) {
				t.Fatal("packet admitted beyond waiting capacity")
			}

			// Admission counters include waiting packets; physical counters do not.
			stats := output.Stats()
			if stats.OfferedPackets != 3 || stats.OfferedBytes != 9 {
				t.Fatalf("unexpected offered counters: %+v", stats)
			}
			if stats.QueuedPackets != capacity || !stats.Transmitting {
				t.Fatalf("unexpected queue state: %+v", stats)
			}

			acceptedPackets, acceptedBytes := uint64(1), uint64(2)
			if capacity == 1 {
				acceptedPackets, acceptedBytes = 2, 5
			}
			if stats.AcceptedPackets != acceptedPackets || stats.AcceptedBytes != acceptedBytes {
				t.Fatalf("unexpected accepted counters: %+v", stats)
			}
			droppedPackets := uint64(3) - acceptedPackets
			droppedBytes := uint64(9) - acceptedBytes
			if stats.QueueDroppedPackets != droppedPackets || stats.QueueDroppedBytes != droppedBytes {
				t.Fatalf("unexpected queue drop counters: %+v", stats)
			}

			physical := output.link.Stats()
			if physical.AcceptedPackets != 1 || physical.QueuedPackets != 0 {
				t.Fatalf("link counted a waiting packet as physical transmission: %+v", physical)
			}

			// Drain admitted packets and verify the output can accept again.
			runEgress(t, e, 5*time.Nanosecond)
			stats = output.Stats()
			if stats.DeliveredPackets != acceptedPackets || stats.DeliveredBytes != acceptedBytes {
				t.Fatalf("unexpected final delivery counters: %+v", stats)
			}
			if stats.QueuedPackets != 0 || stats.Transmitting {
				t.Fatalf("output did not become idle: %+v", stats)
			}
			if arrivals != int(acceptedPackets) {
				t.Fatalf("arrival callbacks = %d, want %d", arrivals, acceptedPackets)
			}
			if !output.Transmit(1, arrive) {
				t.Fatal("output did not accept a packet after becoming idle")
			}
		})
	}
}

// TestEgressPhysicalStatsRemainSingleCounts checks loss and delivery overlays.
func TestEgressPhysicalStatsRemainSingleCounts(t *testing.T) {
	for _, loss := range []float64{0, 1} {
		t.Run(strconv.Itoa(int(loss)), func(t *testing.T) {
			// Two packets fit; a third must be dropped by the output FIFO.
			e := engine.New(10 * time.Nanosecond)
			link := NewLink(e, Config{
				BandwidthBitsPerSecond: 8_000_000_000,
				PacketTravelTime:       2 * time.Nanosecond,
				QueueCapacity:          1,
				LossRate:               loss,
			}, nil)
			output := NewEgress(e, 8_000_000_000).AddOutput(link)
			arrivals := 0
			arrive := func() { arrivals++ }

			output.Transmit(2, arrive)
			output.Transmit(3, arrive)
			output.Transmit(4, arrive)
			runEgress(t, e, 10*time.Nanosecond)

			// The stats overlay replaces admissions and retains physical progress.
			stats := output.Stats()
			physical := link.Stats()
			expectedAdmissions := stats.OfferedPackets == 3 && stats.AcceptedPackets == 2
			if !expectedAdmissions || stats.QueueDroppedPackets != 1 {
				t.Fatalf("admissions were double counted: %+v", stats)
			}

			lossCountsMatch := stats.LostPackets == physical.LostPackets && stats.LostBytes == physical.LostBytes
			deliveryCountsMatch := stats.DeliveredPackets == physical.DeliveredPackets && stats.DeliveredBytes == physical.DeliveredBytes
			if !lossCountsMatch || !deliveryCountsMatch {
				t.Fatal("physical progress was changed by the output stats overlay")
			}

			allAdmittedPacketsLost := stats.LostPackets == 2 && stats.LostBytes == 5
			if loss == 1 && (!allAdmittedPacketsLost || arrivals != 0) {
				t.Fatalf("unexpected random-loss counters: %+v", stats)
			}
			allAdmittedPacketsDelivered := stats.DeliveredPackets == 2 && stats.DeliveredBytes == 5
			if loss == 0 && (!allAdmittedPacketsDelivered || arrivals != 2) {
				t.Fatalf("unexpected delivery counters: %+v", stats)
			}
		})
	}
}

// TestEgressTravelDoesNotBlockReadiness separates serialization from travel.
func TestEgressTravelDoesNotBlockReadiness(t *testing.T) {
	// Travel lasts longer than both packet transmissions combined.
	e := engine.New(20 * time.Nanosecond)
	link := NewLink(e, Config{
		BandwidthBitsPerSecond: 8_000_000_000,
		PacketTravelTime:       10 * time.Nanosecond,
		QueueCapacity:          1,
	}, nil)
	output := NewEgress(e, 8_000_000_000).AddOutput(link)

	output.Transmit(1, func() {})
	output.Transmit(1, func() {})
	runEgress(t, e, time.Nanosecond)

	// The second packet starts while the first is still travelling.
	stats := output.Stats()
	secondPacketStarted := stats.QueuedPackets == 0 && stats.Transmitting
	if !secondPacketStarted || stats.TravellingPackets != 1 {
		t.Fatalf("second packet did not start when the transmitter became free: %+v", stats)
	}

	runEgress(t, e, 2*time.Nanosecond)
	stats = output.Stats()
	transmitterReady := !stats.Transmitting && link.Ready()
	if !transmitterReady || stats.TravellingPackets != 2 {
		t.Fatalf("travel blocked transmitter readiness: %+v", stats)
	}
}

// TestEgressRetainsUnfinishedPacketsAtHorizon checks the existing frozen-end rule.
func TestEgressRetainsUnfinishedPacketsAtHorizon(t *testing.T) {
	// The first packet cannot finish before the configured horizon.
	e := engine.New(time.Nanosecond)
	output := NewEgress(e, 8_000_000_000).AddOutput(egressLink(e, 1))
	if !output.Transmit(2, func() {}) {
		t.Fatal("first packet should be admitted even when completion is beyond the horizon")
	}
	if !output.Transmit(1, func() {}) {
		t.Fatal("waiting packet should be admitted even when completion is beyond the horizon")
	}

	runEgress(t, e, time.Nanosecond)

	// The horizon freezes unfinished work without converting it into drops.
	stats := output.Stats()
	workRetained := stats.Transmitting && stats.QueuedPackets == 1
	admissionsRetained := stats.AcceptedPackets == 2 && stats.QueueDroppedPackets == 0
	if !workRetained || !admissionsRetained || stats.DeliveredPackets != 0 {
		t.Fatalf("unfinished work was silently removed or dropped: %+v", stats)
	}
}

// TestEgressZeroTimePreservesCompletionOrder checks stable simultaneous events.
func TestEgressZeroTimePreservesCompletionOrder(t *testing.T) {
	// Registration alone must leave the engine event queue empty.
	e := engine.New(0)
	dispatcher := NewEgress(e, 8_000_000_000)
	first := dispatcher.AddOutput(egressLink(e, 1))
	second := dispatcher.AddOutput(egressLink(e, 1))
	if e.Pending() != 0 {
		t.Fatal("attaching outputs must not add engine events")
	}

	var arrivals []string
	record := func(name string) func() {
		return func() {
			arrivals = append(arrivals, name)
		}
	}

	// Link completion order determines which branch becomes ready first.
	first.Transmit(0, record("a1"))
	second.Transmit(0, record("b1"))
	second.Transmit(0, record("b2"))
	first.Transmit(0, record("a2"))
	runEgress(t, e, 0)

	if !reflect.DeepEqual(arrivals, []string{"a1", "b1", "a2", "b2"}) {
		t.Fatalf("simultaneous readiness order changed: %v", arrivals)
	}
	if e.EventsExecuted() != 8 || dispatcher.readyOutputs.Len() != 0 {
		t.Fatal("dispatch should add no wake events or stale heap entries")
	}
}

// TestEgressReadyHeapUsesAdmissionOrder checks ordering independently of registration.
func TestEgressReadyHeapUsesAdmissionOrder(t *testing.T) {
	e := engine.New(time.Nanosecond)
	dispatcher := NewEgress(e, 8_000_000_000)
	first := dispatcher.AddOutput(egressLink(e, 2))
	second := dispatcher.AddOutput(egressLink(e, 2))
	var arrivals []string

	// Stage ready heads while dispatch is already active, as a nested wake would.
	dispatcher.dispatching = true
	second.Transmit(0, func() {
		arrivals = append(arrivals, "second")
	})
	runEgress(t, e, time.Nanosecond)
	first.Transmit(0, func() {
		arrivals = append(arrivals, "first")
	})
	dispatcher.markReady(second)
	dispatcher.markReady(first)
	if dispatcher.readyOutputs.Len() != 2 {
		t.Fatal("an output was duplicated in the ready heap")
	}

	// Release dispatch and compare delivery order with admission order.
	dispatcher.dispatching = false
	dispatcher.dispatch()
	runEgress(t, e, time.Nanosecond)

	if !reflect.DeepEqual(arrivals, []string{"second", "first"}) {
		t.Fatalf("ready output heads did not follow admission order: %v", arrivals)
	}
	if first.readyIndex != -1 || second.readyIndex != -1 {
		t.Fatal("dispatched outputs remained registered in the heap")
	}
}

// TestEgressRejectsInvalidPacketsBeforeCounting checks argument validation order.
func TestEgressRejectsInvalidPacketsBeforeCounting(t *testing.T) {
	tests := []struct {
		name      string
		sizeBytes int
		arrival   func()
	}{
		{
			name:      "negative size",
			sizeBytes: -1,
			arrival:   func() {},
		},
		{
			name:      "nil arrival",
			sizeBytes: 1,
		},
		{
			name:      "serialization overflow",
			sizeBytes: math.MaxInt,
			arrival:   func() {},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := engine.New(time.Second)
			link := NewLink(e, Config{BandwidthBitsPerSecond: 1}, nil)
			output := NewEgress(e, 8_000_000_000).AddOutput(link)

			expectEgressPanic(t, func() {
				output.Transmit(test.sizeBytes, test.arrival)
			})

			if output.Stats() != (Stats{}) || e.Pending() != 0 {
				t.Fatal("invalid input changed counters or scheduled work")
			}
		})
	}
}

// TestEgressRejectsInvalidAttachment checks exclusive ownership and engine identity.
func TestEgressRejectsInvalidAttachment(t *testing.T) {
	// Reject missing engines, missing links and links owned by another engine.
	expectEgressPanic(t, func() { NewEgress(nil, 8_000_000_000) })
	e := engine.New(0)
	dispatcher := NewEgress(e, 8_000_000_000)
	expectEgressPanic(t, func() { dispatcher.AddOutput(nil) })
	foreignLink := egressLink(engine.New(0), 0)
	expectEgressPanic(t, func() { dispatcher.AddOutput(foreignLink) })

	// A managed link cannot be attached again or bypass its output FIFO.
	link := egressLink(e, 0)
	dispatcher.AddOutput(link)
	expectEgressPanic(t, func() { dispatcher.AddOutput(link) })
	expectEgressPanic(t, func() { NewEgress(e, 8_000_000_000).AddOutput(link) })
	expectEgressPanic(t, func() { link.Transmit(0, func() {}) })

	// A completed transmission still makes a link unsuitable for attachment.
	used := egressLink(e, 0)
	used.Transmit(0, func() {})
	runEgress(t, e, 0)
	expectEgressPanic(t, func() { dispatcher.AddOutput(used) })
}

// TestEgressSequenceOverflowFailsExplicitly checks the stable order counter guard.
func TestEgressSequenceOverflowFailsExplicitly(t *testing.T) {
	e := engine.New(0)
	dispatcher := NewEgress(e, 8_000_000_000)
	output := dispatcher.AddOutput(egressLink(e, 0))
	dispatcher.nextSequence = ^uint64(0)

	expectEgressPanic(t, func() { output.Transmit(0, func() {}) })

	if output.Stats() != (Stats{}) || e.Pending() != 0 {
		t.Fatal("sequence overflow changed counters or scheduled work")
	}
}

// TestEgressReleasesConsumedCallbacks checks queue compaction and reference release.
func TestEgressReleasesConsumedCallbacks(t *testing.T) {
	// Enough waiting packets force the FIFO to compact its consumed prefix.
	e := engine.New(0)
	output := NewEgress(e, 8_000_000_000).AddOutput(egressLink(e, 3000))
	for index := 0; index < 3000; index++ {
		if !output.Transmit(0, func() {}) {
			t.Fatal("unexpected admission failure")
		}
	}

	runEgress(t, e, 0)

	// Draining resets the queue and releases callbacks in retained slice storage.
	queueReset := output.queued() == 0 && output.queueHead == 0
	if !queueReset || output.Stats().DeliveredPackets != 3000 {
		t.Fatal("FIFO did not drain completely")
	}
	for _, packet := range output.queue[:cap(output.queue)] {
		if packet.onArrival != nil {
			t.Fatal("consumed callback retained in queue storage")
		}
	}
}
