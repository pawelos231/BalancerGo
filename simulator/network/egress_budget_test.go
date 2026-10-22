package network

import (
	"math"
	"reflect"
	"testing"
	"time"

	"load-balancer/engine"
)

// TestEgressSharedBudgetPacesAllDirections charges backend and client outputs together.
func TestEgressSharedBudgetPacesAllDirections(t *testing.T) {
	e := engine.New(10 * time.Nanosecond)
	dispatcher := NewEgress(e, 8_000_000_000)
	backend := dispatcher.AddOutput(NewLink(e, Config{
		BandwidthBitsPerSecond: 64_000_000_000,
		QueueCapacity:          2,
	}, nil))
	client := dispatcher.AddOutput(NewLink(e, Config{
		BandwidthBitsPerSecond: 64_000_000_000,
		QueueCapacity:          2,
	}, nil))
	var arrivals []string
	record := func(name string) func() {
		return func() { arrivals = append(arrivals, name+"@"+e.Now().String()) }
	}

	backend.Transmit(2, record("backend-1"))
	client.Transmit(2, record("client"))
	backend.Transmit(2, record("backend-2"))
	initial := dispatcher.Stats()
	if initial.ForwardedPackets != 1 || initial.QueuedPackets != 2 || !initial.BudgetBlocked {
		t.Fatalf("outputs did not share one initial grant: %+v", initial)
	}

	runEgress(t, e, e.EndTime())
	expected := []string{"backend-1@1ns", "client@3ns", "backend-2@5ns"}
	if !reflect.DeepEqual(arrivals, expected) {
		t.Fatalf("arrivals = %v, want %v", arrivals, expected)
	}
	stats := dispatcher.Stats()
	if stats.ForwardedPackets != 3 || stats.ForwardedBytes != 6 || stats.BudgetWait != 4*time.Nanosecond {
		t.Fatalf("unexpected shared forwarding counters: %+v", stats)
	}
	if stats.QueuedPackets != 0 || stats.BudgetBlocked || e.Pending() != 0 {
		t.Fatalf("drained outputs left pending work: %+v", stats)
	}
}

// TestEgressBudgetAndLinkRunInParallel excludes a second serial transmission cost.
func TestEgressBudgetAndLinkRunInParallel(t *testing.T) {
	e := engine.New(20 * time.Nanosecond)
	dispatcher := NewEgress(e, 8_000_000_000)
	output := dispatcher.AddOutput(NewLink(e, Config{
		BandwidthBitsPerSecond: 4_000_000_000,
		QueueCapacity:          1,
	}, nil))
	var arrivals []time.Duration
	record := func() { arrivals = append(arrivals, e.Now()) }
	output.Transmit(4, record)
	output.Transmit(4, record)

	if e.Pending() != 1 || dispatcher.wakeEvent != 0 {
		t.Fatal("a busy link must wait for completion instead of adding a budget tick")
	}
	runEgress(t, e, 6*time.Nanosecond)
	if dispatcher.Stats().BudgetBlocked || dispatcher.Stats().BudgetWait != 0 {
		t.Fatal("waiting only for the link must not count as budget wait")
	}
	runEgress(t, e, e.EndTime())
	expected := []time.Duration{8 * time.Nanosecond, 16 * time.Nanosecond}
	if !reflect.DeepEqual(arrivals, expected) {
		t.Fatalf("budget and link costs were added in series: %v", arrivals)
	}
	if dispatcher.Stats().BudgetWait != 0 || e.EventsExecuted() != 4 {
		t.Fatal("link-limited output created budget waits or idle wakeups")
	}
}

// TestEgressBudgetSelectsOldestReadyHead skips a busy branch and keeps admission ties stable.
func TestEgressBudgetSelectsOldestReadyHead(t *testing.T) {
	e := engine.New(30 * time.Nanosecond)
	dispatcher := NewEgress(e, 8_000_000_000)
	slow := dispatcher.AddOutput(NewLink(e, Config{
		BandwidthBitsPerSecond: 4_000_000_000,
		QueueCapacity:          1,
	}, nil))
	second := dispatcher.AddOutput(egressLink(e, 1))
	first := dispatcher.AddOutput(egressLink(e, 1))
	third := dispatcher.AddOutput(egressLink(e, 1))
	var arrivals []string
	record := func(name string) func() {
		return func() { arrivals = append(arrivals, name+"@"+e.Now().String()) }
	}

	slow.Transmit(8, record("slow-1"))
	slow.Transmit(1, record("slow-2"))
	first.Transmit(1, record("first"))
	second.Transmit(1, record("second"))
	runEgress(t, e, time.Nanosecond)
	third.Transmit(1, record("third"))
	runEgress(t, e, e.EndTime())

	expected := []string{
		"first@9ns",
		"second@10ns",
		"third@11ns",
		"slow-1@16ns",
		"slow-2@18ns",
	}
	if !reflect.DeepEqual(arrivals, expected) {
		t.Fatalf("ready heads did not follow admission order: %v", arrivals)
	}
}

// TestEgressZeroQueueRequiresBudget drops immediately when only the shared limit is busy.
func TestEgressZeroQueueRequiresBudget(t *testing.T) {
	e := engine.New(5 * time.Nanosecond)
	dispatcher := NewEgress(e, 8_000_000_000)
	starter := dispatcher.AddOutput(egressLink(e, 0))
	unbuffered := dispatcher.AddOutput(egressLink(e, 0))
	starter.Transmit(2, func() {})
	if unbuffered.Transmit(1, func() { t.Error("dropped packet arrived") }) {
		t.Fatal("a free link cannot bypass an unavailable budget")
	}
	if unbuffered.link.Stats().AcceptedPackets != 0 || dispatcher.wakeEvent != 0 {
		t.Fatal("a dropped packet must not start or create a budget wake")
	}
	runEgress(t, e, 2*time.Nanosecond)
	if !unbuffered.Transmit(1, func() {}) {
		t.Fatal("a drop must not postpone the next grant")
	}
	stats := dispatcher.Stats()
	if stats.ForwardedPackets != 2 || stats.ForwardedBytes != 3 || stats.QueueDroppedBytes != 1 {
		t.Fatalf("unexpected start/drop counters: %+v", stats)
	}
	if stats.QueuedPackets != 0 || stats.BudgetWait != 0 {
		t.Fatalf("an unbuffered drop must not create waiting traffic: %+v", stats)
	}
}

// TestEgressZeroQueueCannotPassOlderHead checks arrivals ordered before the grant event.
func TestEgressZeroQueueCannotPassOlderHead(t *testing.T) {
	e := engine.New(4 * time.Nanosecond)
	dispatcher := NewEgress(e, 8_000_000_000)
	starter := dispatcher.AddOutput(egressLink(e, 0))
	older := dispatcher.AddOutput(egressLink(e, 1))
	newer := dispatcher.AddOutput(egressLink(e, 0))
	arrivalRan := false

	// Register this arrival first, so it precedes the wake at the same timestamp.
	e.ScheduleAt(2*time.Nanosecond, func() {
		arrivalRan = true
		if newer.Transmit(1, func() {}) {
			t.Error("a zero-capacity output accepted a packet behind an older ready head")
		}
		if dispatcher.Stats().ForwardedPackets != 1 {
			t.Error("the new packet used the older head's pending grant")
		}
	})
	starter.Transmit(2, func() {})
	older.Transmit(1, func() {})
	runEgress(t, e, 2*time.Nanosecond)

	if !arrivalRan || older.link.Stats().AcceptedPackets != 1 || newer.link.Stats().AcceptedPackets != 0 {
		t.Fatal("the older head did not retain its grant")
	}
	if dispatcher.Stats().QueueDroppedPackets != 1 {
		t.Fatal("the zero-capacity rejection was not counted once")
	}
}

// TestEgressBudgetWaitIsSharedAndReadOnly counts the union of ready-output waits.
func TestEgressBudgetWaitIsSharedAndReadOnly(t *testing.T) {
	e := engine.New(12 * time.Nanosecond)
	dispatcher := NewEgress(e, 8_000_000_000)
	slow := dispatcher.AddOutput(NewLink(e, Config{
		BandwidthBitsPerSecond: 4_000_000_000,
		QueueCapacity:          1,
	}, nil))
	first := dispatcher.AddOutput(egressLink(e, 1))
	second := dispatcher.AddOutput(egressLink(e, 1))
	slow.Transmit(4, func() {})
	slow.Transmit(1, func() {})
	runEgress(t, e, time.Nanosecond)
	if dispatcher.Stats().BudgetWait != 0 || dispatcher.Stats().BudgetBlocked {
		t.Fatal("a busy output alone must not start a budget-wait interval")
	}
	first.Transmit(2, func() {})
	runEgress(t, e, 2*time.Nanosecond)
	second.Transmit(1, func() {})
	runEgress(t, e, 3*time.Nanosecond)

	pending := e.Pending()
	firstRead := dispatcher.Stats()
	secondRead := dispatcher.Stats()
	if firstRead != secondRead || e.Pending() != pending {
		t.Fatal("reading forwarding stats changed counters or scheduled events")
	}
	if firstRead.BudgetWait != 2*time.Nanosecond || !firstRead.BudgetBlocked {
		t.Fatalf("overlapping output waits were added or omitted: %+v", firstRead)
	}
	runEgress(t, e, 6*time.Nanosecond)
	stats := dispatcher.Stats()
	if stats.BudgetWait != 5*time.Nanosecond || stats.BudgetBlocked || stats.QueuedPackets != 1 {
		t.Fatalf("budget wait should stop when only the busy branch remains: %+v", stats)
	}
	runEgress(t, e, e.EndTime())
	if dispatcher.Stats().BudgetWait != 5*time.Nanosecond {
		t.Fatal("later busy-link waiting was counted as budget wait")
	}
}

// TestEgressLossAndRetriesConsumeGrants separates starts from pre-start queue drops.
func TestEgressLossAndRetriesConsumeGrants(t *testing.T) {
	e := engine.New(8 * time.Nanosecond)
	dispatcher := NewEgress(e, 8_000_000_000)
	output := dispatcher.AddOutput(NewLink(e, Config{
		BandwidthBitsPerSecond: 8_000_000_000,
		QueueCapacity:          1,
		LossRate:               1,
	}, nil))
	onArrival := func() { t.Error("lost packet arrived") }
	output.Transmit(2, onArrival)
	output.Transmit(3, onArrival)
	if output.Transmit(4, onArrival) {
		t.Fatal("full output should drop before reserving a grant")
	}
	runEgress(t, e, 5*time.Nanosecond)
	if !output.Transmit(2, onArrival) {
		t.Fatal("a retry should use the next available grant")
	}
	runEgress(t, e, e.EndTime())

	forwarding := dispatcher.Stats()
	physical := output.Stats()
	if forwarding.ForwardedPackets != 3 || forwarding.ForwardedBytes != 7 {
		t.Fatalf("loss or retry was excluded from the shared budget: %+v", forwarding)
	}
	if forwarding.QueueDroppedPackets != 1 || forwarding.QueueDroppedBytes != 4 {
		t.Fatalf("pre-start drop counters changed: %+v", forwarding)
	}
	if physical.LostPackets != 3 || physical.LostBytes != 7 || physical.AcceptedPackets != 3 {
		t.Fatalf("physical losses were counted incorrectly: %+v", physical)
	}
}

// TestEgressHasOneWakeAndNoIdleTicks keeps work proportional to real transmissions.
func TestEgressHasOneWakeAndNoIdleTicks(t *testing.T) {
	e := engine.New(100 * time.Nanosecond)
	dispatcher := NewEgress(e, 8_000_000_000)
	starter := dispatcher.AddOutput(NewLink(e, Config{BandwidthBitsPerSecond: math.MaxInt64}, nil))
	starter.Transmit(2, func() {})
	const waitingOutputs = 20
	var wake engine.EventID
	for index := 0; index < waitingOutputs; index++ {
		output := dispatcher.AddOutput(egressLink(e, 1))
		output.Transmit(1, func() {})
		if index == 0 {
			wake = dispatcher.wakeEvent
		}
		if wake == 0 || dispatcher.wakeEvent != wake || e.Pending() != 2 {
			t.Fatal("each waiting output created another wake or polling event")
		}
	}
	runEgress(t, e, e.EndTime())
	// Later link completions occur at grant times, so only the first wake executes.
	expectedEvents := uint64(2*(waitingOutputs+1) + 1)
	if e.EventsExecuted() != expectedEvents || dispatcher.wakeEvent != 0 || e.Pending() != 0 {
		t.Fatalf("unexpected wake lifecycle: events=%d pending=%d", e.EventsExecuted(), e.Pending())
	}
}

// TestEgressIdleTimeDoesNotAccumulateCredit restarts pacing from each actual grant.
func TestEgressIdleTimeDoesNotAccumulateCredit(t *testing.T) {
	e := engine.New(20 * time.Nanosecond)
	dispatcher := NewEgress(e, 8_000_000_000)
	first := dispatcher.AddOutput(egressLink(e, 1))
	second := dispatcher.AddOutput(egressLink(e, 1))
	first.Transmit(1, func() {})
	runEgress(t, e, 10*time.Nanosecond)
	first.Transmit(2, func() {})
	second.Transmit(1, func() {})
	if dispatcher.Stats().ForwardedPackets != 2 || dispatcher.nextGrantAt != 12*time.Nanosecond {
		t.Fatal("idle time created credit for a burst of grants")
	}
	runEgress(t, e, 11*time.Nanosecond)
	if second.link.Stats().AcceptedPackets != 0 {
		t.Fatal("second output started before the new grant interval elapsed")
	}
	runEgress(t, e, 12*time.Nanosecond)
	if second.link.Stats().AcceptedPackets != 1 {
		t.Fatal("second output did not start at the next grant")
	}
}

// TestEgressBudgetHorizonRetainsQueues includes a grant exactly at the boundary.
func TestEgressBudgetHorizonRetainsQueues(t *testing.T) {
	for _, end := range []time.Duration{0, 2 * time.Nanosecond, 3 * time.Nanosecond} {
		t.Run(end.String(), func(t *testing.T) {
			e := engine.New(end)
			dispatcher := NewEgress(e, 8_000_000_000)
			first := dispatcher.AddOutput(egressLink(e, 1))
			second := dispatcher.AddOutput(egressLink(e, 1))
			first.Transmit(3, func() {})
			second.Transmit(1, func() {})
			runEgress(t, e, end)

			stats := dispatcher.Stats()
			if end < 3*time.Nanosecond {
				if stats.ForwardedPackets != 1 || stats.QueuedPackets != 1 || !stats.BudgetBlocked {
					t.Fatalf("unfinished queue was lost at the horizon: %+v", stats)
				}
			} else if stats.ForwardedPackets != 2 || stats.QueuedPackets != 0 || stats.BudgetBlocked {
				t.Fatalf("a grant at the inclusive boundary was not used: %+v", stats)
			}
			if stats.BudgetWait != end || stats.QueueDroppedPackets != 0 {
				t.Fatalf("horizon wait/drop counters are incorrect: %+v", stats)
			}
			if dispatcher.wakeEvent != 0 || e.Pending() != 0 {
				t.Fatal("the horizon left a wake loop or an event outside the experiment")
			}
		})
	}
}

// TestEgressNearMaxEndTimeDoesNotWrap freezes a grant beyond the representable end.
func TestEgressNearMaxEndTimeDoesNotWrap(t *testing.T) {
	end := time.Duration(math.MaxInt64)
	e := engine.New(end)
	dispatcher := NewEgress(e, 8_000_000_000)
	first := dispatcher.AddOutput(egressLink(e, 1))
	second := dispatcher.AddOutput(egressLink(e, 1))
	runEgress(t, e, end-time.Nanosecond)
	first.Transmit(2, func() { t.Error("packet cannot arrive before the horizon") })
	second.Transmit(1, func() { t.Error("queued packet cannot start before the horizon") })
	runEgress(t, e, end)

	stats := dispatcher.Stats()
	if stats.ForwardedPackets != 1 || stats.QueuedPackets != 1 || stats.BudgetWait != time.Nanosecond {
		t.Fatalf("overflowing grant changed starts or wait accounting: %+v", stats)
	}
	if !stats.BudgetBlocked || dispatcher.wakeEvent != 0 || e.Pending() != 0 || e.EventsExecuted() != 0 {
		t.Fatal("overflowing grant wrapped into an executable event")
	}
}

// TestEgressRejectsInvalidBudgetBeforeCounting checks the new rate and cost boundaries.
func TestEgressRejectsInvalidBudgetBeforeCounting(t *testing.T) {
	e := engine.New(time.Second)
	expectEgressPanic(t, func() { NewEgress(e, 0) })
	expectEgressPanic(t, func() { NewEgress(e, -1) })
	dispatcher := NewEgress(e, 1)
	output := dispatcher.AddOutput(NewLink(e, Config{BandwidthBitsPerSecond: math.MaxInt64}, nil))
	expectEgressPanic(t, func() { output.Transmit(math.MaxInt, func() {}) })
	if output.Stats() != (Stats{}) || dispatcher.Stats().ForwardedPackets != 0 || e.Pending() != 0 {
		t.Fatal("an overflowing forwarding cost changed counters or scheduled work")
	}
}
