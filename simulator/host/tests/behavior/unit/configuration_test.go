package host_test

import (
	"math"
	"slices"
	"testing"
	"time"

	"load-balancer/engine"
	"load-balancer/host"
)

// Three old jobs finish at 10 ns after a shrink to one core at 2 ns.
// Only the last retiring job releases capacity for the retained FIFO.
func TestShrinkRetiresActiveCoresAndRetainsAdmittedWork(t *testing.T) {
	e := engine.New(20 * time.Nanosecond)
	cpu := host.New(e, cpuConfig(3, 10, 3))
	var completed []time.Duration
	for range 5 {
		cpu.Submit(func() { completed = append(completed, e.Now()) })
	}
	changeCPU(e, cpu, 2, cpuConfig(1, 4, 1))
	advance(t, e, 2*time.Nanosecond)
	if cpu.Submit(func() {}) || cpu.Stats().QueuedPackets != 2 {
		t.Fatal("shrinking discarded admitted work or accepted an overfull waiter")
	}
	advance(t, e, 10*time.Nanosecond)
	stats := cpu.Stats()
	if stats.BusyCores != 1 || stats.QueuedPackets != 1 ||
		stats.BusyTime != 30*time.Nanosecond || stats.AvailableCoreTime != 30*time.Nanosecond {
		t.Fatalf("retiring cores used the wrong capacity or started early: %+v", stats)
	}
	advance(t, e, e.EndTime())
	want := []time.Duration{10, 10, 10, 14, 18}
	stats = cpu.Stats()
	if !slices.Equal(completed, want) || stats.BusyTime != 38*time.Nanosecond ||
		stats.AvailableCoreTime != 40*time.Nanosecond || stats.Utilization() != 0.95 {
		t.Fatalf("wrong shrink history: completed=%v stats=%+v", completed, stats)
	}
}

// A pre-existing arrival at the growth timestamp runs before the resume event.
// It must give old waiters the new core, even though the pump has not run yet.
func TestGrowDoesNotLetSameTimeArrivalPassWaiters(t *testing.T) {
	e := engine.New(12 * time.Nanosecond)
	cpu := host.New(e, cpuConfig(1, 10, 2))
	var completed []time.Duration
	for range 3 {
		cpu.Submit(func() { completed = append(completed, e.Now()) })
	}
	e.ScheduleAt(2, func() {
		if cpu.Submit(func() { t.Error("new arrival passed an old waiter") }) {
			t.Error("a full retained FIFO accepted another waiter")
		}
	})
	changeCPU(e, cpu, 2, cpuConfig(2, 3, 1))
	advance(t, e, e.EndTime())
	want := []time.Duration{5, 8, 10}
	stats := cpu.Stats()
	if !slices.Equal(completed, want) || stats.AvailableCoreTime != 22*time.Nanosecond ||
		stats.BusyTime != 16*time.Nanosecond || stats.QueueDroppedPackets != 1 {
		t.Fatalf("growth changed old service or FIFO order: completed=%v stats=%+v", completed, stats)
	}
}

// Cost zero flushes waiters in ordinary work, including reentrant arrivals.
// An old active job keeps its deadline and blocks finite service after re-enabling.
func TestZeroCostToggleKeepsOldDeadlineAndQueuedFIFO(t *testing.T) {
	e := engine.New(14 * time.Nanosecond)
	cpu := host.New(e, cpuConfig(1, 10, 2))
	var completed []string
	cpu.Submit(func() { completed = append(completed, "old active") })
	cpu.Submit(func() {
		completed = append(completed, "first waiter")
		cpu.Submit(func() { completed = append(completed, "nested arrival") })
	})
	cpu.Submit(func() { completed = append(completed, "second waiter") })
	e.ScheduleAt(2, func() {
		cpu.Submit(func() { completed = append(completed, "same-time arrival") })
	})
	changeCPU(e, cpu, 2, cpuConfig(1, 0, 0))
	changeCPU(e, cpu, 3, cpuConfig(1, 4, 1))
	e.ScheduleAt(3, func() {
		cpu.Submit(func() { completed = append(completed, "finite arrival") })
	})
	advance(t, e, 2*time.Nanosecond)
	want := []string{"first waiter", "second waiter", "nested arrival", "same-time arrival"}
	if !slices.Equal(completed, want) || cpu.Stats().BusyCores != 1 ||
		cpu.Stats().Enabled || cpu.Stats().Utilization() != 1 {
		t.Fatalf("bypass reordered waiting work or hid old work: %v %+v", completed, cpu.Stats())
	}
	advance(t, e, 10*time.Nanosecond)
	want = append(want, "old active")
	if !slices.Equal(completed, want) || cpu.Stats().BusyCores != 1 {
		t.Fatal("the old job changed its deadline or finite work bypassed the CPU")
	}
	advance(t, e, e.EndTime())
	want = append(want, "finite arrival")
	stats := cpu.Stats()
	if !slices.Equal(completed, want) || stats.ProcessedPackets != 6 ||
		stats.BusyTime != 14*time.Nanosecond || stats.AvailableCoreTime != 14*time.Nanosecond {
		t.Fatalf("wrong cost-toggle history: completed=%v stats=%+v", completed, stats)
	}
}

func TestPreparedConfigDefersCallbacksAndCleanupCancelsPump(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(map[bool]string{false: "discard", true: "stop"}[stop], func(t *testing.T) {
			e := engine.New(20)
			cpu := host.New(e, cpuConfig(1, 10, 1))
			for range 2 {
				cpu.Submit(func() { t.Error("discarded callback ran") })
			}
			advance(t, e, 2)
			before := cpu.Stats()
			update := cpu.PrepareConfig(cpuConfig(1, 0, 0))
			if cpu.Stats() != before {
				t.Fatal("preparing an update changed live host state")
			}
			update.Apply()
			if cpu.Stats().ProcessedPackets != 0 || e.Pending() != 1 {
				t.Fatal("Apply processed work or scheduled a callback")
			}
			update.Resume()
			pending, stats := e.Pending(), cpu.Stats()
			if pending != 2 || cpu.Stats() != stats || e.Pending() != pending {
				t.Fatal("a read consumed the pending pump or changed counters")
			}
			if stop {
				cpu.Stop()
			} else {
				cpu.DiscardPending()
			}
			if e.Pending() != 0 || cpu.Stats().DiscardedPackets != 2 {
				t.Fatal("cleanup retained a pump or completion event")
			}
			advance(t, e, e.EndTime())
		})
	}
}

func TestPreparedConfigValidatesWholeLifetimeEvenAtZeroCost(t *testing.T) {
	e := engine.New(time.Duration(math.MaxInt64 / 2))
	cpu := host.New(e, cpuConfig(1, 0, 0))
	advance(t, e, e.EndTime()-1)
	defer func() {
		if recover() == nil {
			t.Fatal("late update validated only the remaining interval")
		}
	}()
	cpu.PrepareConfig(cpuConfig(3, 0, 0))
}

// A queued bypass callback may stop the host before a same-time arrival is admitted.
// That arrival follows the same counter policy as other submissions to a stopped host.
func TestBypassWaiterCanStopHostBeforeNewArrivalIsCounted(t *testing.T) {
	e := engine.New(20)
	cpu := host.New(e, cpuConfig(1, 10, 1))
	cpu.Submit(func() { t.Error("the discarded active packet completed") })
	cpu.Submit(cpu.Stop)
	advance(t, e, 2)
	cpu.PrepareConfig(cpuConfig(1, 0, 0)).Apply()
	if cpu.Submit(func() { t.Error("the stopped host accepted another packet") }) {
		t.Fatal("the arrival was admitted after a waiter stopped the host")
	}
	stats := cpu.Stats()
	if !stats.Stopped || stats.OfferedPackets != 2 || stats.AcceptedPackets != 2 ||
		stats.QueueDroppedPackets != 0 || stats.ProcessedPackets != 1 ||
		stats.DiscardedPackets != 1 || e.Pending() != 0 {
		t.Fatalf("the interrupted arrival changed admission counters: %+v", stats)
	}
}

func cpuConfig(cores int, cost time.Duration, queueCapacity int) host.Config {
	return host.Config{Cores: cores, PacketProcessingTime: cost, QueueCapacity: queueCapacity}
}

func changeCPU(e *engine.Engine, cpu *host.Host, at time.Duration, config host.Config) {
	update := cpu.PrepareConfig(config)
	e.ScheduleConfigurationAt(at, update.Apply)
	e.ScheduleAt(at, update.Resume)
}
