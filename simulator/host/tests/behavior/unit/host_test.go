package host_test

import (
	"math"
	"slices"
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/host"
)

// Two packets work from 0 to 10 us; the next two wait and finish at 20 us.
// A fifth simultaneous packet does not fit either a core or the two-slot FIFO.
func TestCoresAndFIFOHaveSeparateCapacity(t *testing.T) {
	e := engine.New(100 * time.Microsecond)
	cpu := host.New(e, host.Config{
		Cores:                2,
		PacketProcessingTime: 10 * time.Microsecond,
		QueueCapacity:        2,
	})
	var completed []int
	for packet := 0; packet < 5; packet++ {
		accepted := cpu.Submit(func() { completed = append(completed, packet) })
		if accepted != (packet < 4) {
			t.Fatalf("packet %d admission=%v", packet, accepted)
		}
	}

	initial := cpu.Stats()
	if initial.BusyCores != 2 || initial.QueuedPackets != 2 || initial.MaxQueuedPackets != 2 ||
		initial.AcceptedPackets != 4 || initial.QueueDroppedPackets != 1 {
		t.Fatalf("unexpected initial load: %+v", initial)
	}

	advance(t, e, 9*time.Microsecond)
	beforeCompletion := cpu.Stats()
	if len(completed) != 0 || beforeCompletion.BusyTime != 18*time.Microsecond ||
		beforeCompletion.MaxQueueWait != 9*time.Microsecond {
		t.Fatalf("partial work before completion: %+v", beforeCompletion)
	}

	// Reading measurements never adds or consumes simulation events.
	pending := e.Pending()
	if again := cpu.Stats(); again != beforeCompletion || e.Pending() != pending {
		t.Fatal("reading stats changed the resource or event queue")
	}

	advance(t, e, 10*time.Microsecond)
	if !slices.Equal(completed, []int{0, 1}) {
		t.Fatalf("first completions=%v", completed)
	}
	working := cpu.Stats()
	if working.BusyCores != 2 || working.QueuedPackets != 0 || working.StartedPackets != 4 ||
		working.QueueWaitTotal != 20*time.Microsecond {
		t.Fatalf("waiting packets did not take the freed cores: %+v", working)
	}

	advance(t, e, 20*time.Microsecond)
	finished := cpu.Stats()
	if !slices.Equal(completed, []int{0, 1, 2, 3}) || finished.BusyCores != 0 ||
		finished.ProcessedPackets != 4 || finished.BusyTime != 40*time.Microsecond ||
		finished.MaxQueueWait != 10*time.Microsecond {
		t.Fatalf("wrong final work or accounting: completed=%v stats=%+v", completed, finished)
	}

	// The same 40 us of work uses 20% of two cores observed for 100 us.
	advance(t, e, 100*time.Microsecond)
	if utilization := cpu.Stats().Utilization(); utilization != 0.2 {
		t.Fatalf("utilization=%v, want 20%% including the idle interval", utilization)
	}
}

// Cost zero calls protocol work inline, even with zero waiting slots.
// A second event at the same timestamp still runs after the inline callback.
func TestZeroCostPreservesImmediateOrdering(t *testing.T) {
	e := engine.New(time.Millisecond)
	cpu := host.New(e, host.Config{Cores: 1})
	var order []string
	e.ScheduleAt(0, func() {
		cpu.Submit(func() { order = append(order, "packet") })
		order = append(order, "submit returned")
	})
	e.ScheduleAt(0, func() { order = append(order, "next event") })
	advance(t, e, time.Millisecond)

	want := []string{"packet", "submit returned", "next event"}
	stats := cpu.Stats()
	if !slices.Equal(order, want) || e.EventsExecuted() != 2 {
		t.Fatalf("zero cost changed event order: %v, events=%d", order, e.EventsExecuted())
	}
	if stats.Enabled || stats.BusyTime != 0 || stats.ProcessedPackets != 1 || stats.QueuedPackets != 0 {
		t.Fatalf("zero cost occupied CPU resources: %+v", stats)
	}
	if stats.Utilization() != 0 {
		t.Fatal("disabled CPU reported modeled utilization")
	}
}

// No waiting slots means two cores accept two packets and reject the third.
// Once the first pair finishes, a new packet can use a freed core normally.
func TestZeroQueueStillAcceptsIdleCores(t *testing.T) {
	e := engine.New(time.Millisecond)
	cpu := host.New(e, host.Config{Cores: 2, PacketProcessingTime: 10 * time.Microsecond})
	noop := func() {}
	if !cpu.Submit(noop) || !cpu.Submit(noop) || cpu.Submit(noop) {
		t.Fatal("zero queue should admit exactly the two idle cores")
	}
	advance(t, e, 10*time.Microsecond)
	if !cpu.Submit(noop) || cpu.Stats().QueueDroppedPackets != 1 {
		t.Fatal("the finished cores did not become available")
	}
}

// Work started at 10 us would finish at 20 us, beyond this 15-us experiment.
// It remains active and contributes only five microseconds of partial CPU work.
func TestUnfinishedWorkRetainsPartialBusyTime(t *testing.T) {
	e := engine.New(15 * time.Microsecond)
	cpu := host.New(e, host.Config{
		Cores:                2,
		PacketProcessingTime: 10 * time.Microsecond,
		QueueCapacity:        1,
	})
	completed := 0
	for packet := 0; packet < 3; packet++ {
		cpu.Submit(func() { completed++ })
	}
	advance(t, e, e.EndTime())

	stats := cpu.Stats()
	if completed != 2 || stats.ProcessedPackets != 2 || stats.BusyCores != 1 ||
		stats.BusyTime != 25*time.Microsecond || stats.ObservedTime != 15*time.Microsecond ||
		e.Pending() != 0 {
		t.Fatalf("unfinished work was lost or completed early: %+v", stats)
	}
	if utilization := stats.Utilization(); math.Abs(utilization-5.0/6.0) > 1e-12 {
		t.Fatalf("partial-work utilization=%v, want 25/30 core-microseconds", utilization)
	}

	// A late arrival can take the other core; the following one fills the FIFO.
	// Neither may pretend to finish beyond the inclusive experiment horizon.
	late := func() { t.Fatal("late work completed beyond the horizon") }
	if !cpu.Submit(late) || !cpu.Submit(late) || cpu.Submit(late) {
		t.Fatal("late arrivals did not respect occupied cores and waiting capacity")
	}
	advance(t, e, e.EndTime())
	lateStats := cpu.Stats()
	if lateStats.BusyCores != 2 || lateStats.QueuedPackets != 1 ||
		lateStats.BusyTime != stats.BusyTime || lateStats.QueueDroppedPackets != 1 {
		t.Fatalf("late work changed completed CPU time: %+v", lateStats)
	}
	cpu.Stop()
	if discarded := cpu.Stats().DiscardedPackets; discarded != 3 || e.Pending() != 0 {
		t.Fatalf("late unfinished work was not released: discarded=%d", discarded)
	}
}

// Failure abandons the active packet and its waiter. A recovered host starts fresh work.
// Old completion callbacks must never appear after recovery.
func TestDiscardPendingAllowsRecoveryWithoutOldCallbacks(t *testing.T) {
	e := engine.New(100 * time.Microsecond)
	cpu := host.New(e, host.Config{
		Cores:                1,
		PacketProcessingTime: 10 * time.Microsecond,
		QueueCapacity:        1,
	})
	var completed []string
	cpu.Submit(func() { completed = append(completed, "old active") })
	cpu.Submit(func() { completed = append(completed, "old waiting") })
	advance(t, e, 5*time.Microsecond)
	cpu.DiscardPending()
	if e.Pending() != 0 || cpu.Stats().DiscardedPackets != 2 {
		t.Fatal("failure retained CPU work")
	}

	cpu.Submit(func() { completed = append(completed, "recovered") })
	advance(t, e, 15*time.Microsecond)
	stats := cpu.Stats()
	if !slices.Equal(completed, []string{"recovered"}) || stats.BusyTime != 15*time.Microsecond ||
		stats.ProcessedPackets != 1 || stats.DiscardedPackets != 2 {
		t.Fatalf("recovery revived old work: completed=%v stats=%+v", completed, stats)
	}
}

// A completion callback cannot jump ahead of a packet already waiting in the FIFO.
// It may also stop the host; that must cancel the next packet's reserved completion.
func TestCallbackReentrancyPreservesFIFOAndStop(t *testing.T) {
	e := engine.New(100 * time.Microsecond)
	cpu := host.New(e, host.Config{
		Cores:                1,
		PacketProcessingTime: 10 * time.Microsecond,
		QueueCapacity:        1,
	})
	var completed []string
	cpu.Submit(func() {
		completed = append(completed, "first")
		cpu.Submit(func() { completed = append(completed, "nested") })
	})
	cpu.Submit(func() {
		completed = append(completed, "waiting")
		cpu.Stop()
	})
	advance(t, e, e.EndTime())

	stats := cpu.Stats()
	if !slices.Equal(completed, []string{"first", "waiting"}) || e.Pending() != 0 ||
		stats.ProcessedPackets != 2 || stats.DiscardedPackets != 1 || !stats.Stopped {
		t.Fatalf("reentrant callbacks broke FIFO/stop: completed=%v stats=%+v", completed, stats)
	}
	if cpu.Submit(func() { t.Fatal("stopped work ran") }) || cpu.Stats() != stats {
		t.Fatal("stopped host accepted or counted new work")
	}

	// Server shutdown and the later global reset may both reach this resource.
	cpu.Stop()
	cpu.DiscardPending()
	if cpu.Stats() != stats || e.Pending() != 0 {
		t.Fatal("repeated cleanup changed stopped counters or revived work")
	}
}

func advance(t *testing.T, e *engine.Engine, until time.Duration) {
	t.Helper()
	if err := e.RunUntil(until); err != nil {
		t.Fatal(err)
	}
}
