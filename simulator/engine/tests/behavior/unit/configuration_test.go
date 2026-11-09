package engine_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"load-balancer/engine"
)

// Configuration registered after packets still runs first, without advancing virtual time.
func TestConfigurationRunsBeforeOrdinaryWorkAtTheSameTime(t *testing.T) {
	e := engine.New(20 * time.Nanosecond)
	var order []string
	e.ScheduleAt(10*time.Nanosecond, func() {
		order = append(order, "packet 1")
		e.ScheduleAfter(0, func() { order = append(order, "packet spawned by packet 1") })
	})
	e.ScheduleConfigurationAt(10*time.Nanosecond, func() {
		order = append(order, "configuration 1")
		if e.Now() != 10*time.Nanosecond {
			t.Fatal("configuration did not run at its scheduled virtual time")
		}
	})
	e.ScheduleAt(10*time.Nanosecond, func() { order = append(order, "packet 2") })
	e.ScheduleConfigurationAt(10*time.Nanosecond, func() {
		order = append(order, "configuration 2")
	})
	e.ScheduleAt(9*time.Nanosecond, func() { order = append(order, "earlier packet") })

	run(t, e, 10*time.Nanosecond)
	want := []string{
		"earlier packet", "configuration 1", "configuration 2",
		"packet 1", "packet 2", "packet spawned by packet 1",
	}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("callback order = %v, want %v", order, want)
	}
}

// A configuration callback may queue work, but cannot execute the event loop recursively.
func TestConfigurationCannotInterleaveCallbacksThroughNestedRun(t *testing.T) {
	e := engine.New(time.Second)
	var order []string
	e.ScheduleAt(0, func() { order = append(order, "packet") })
	e.ScheduleConfigurationAt(0, func() {
		order = append(order, "configuration 1")
		if err := e.RunUntil(0); !errors.Is(err, engine.ErrReentrantRun) {
			t.Fatalf("nested RunUntil returned %v", err)
		}

		e.ScheduleAt(0, func() { order = append(order, "queued packet") })
		e.ScheduleConfigurationAt(0, func() { order = append(order, "configuration 3") })
	})
	e.ScheduleConfigurationAt(0, func() { order = append(order, "configuration 2") })

	run(t, e, 0)
	want := []string{"configuration 1", "configuration 2", "configuration 3", "packet", "queued packet"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("callback order = %v, want %v", order, want)
	}
}

// Once a packet at 10 ns starts, a configuration for 10 ns would be retroactive.
func TestConfigurationRejectsInsertionAfterOrdinaryWorkStarts(t *testing.T) {
	t.Run("inside an ordinary callback", func(t *testing.T) {
		e := engine.New(20 * time.Nanosecond)
		futureRan := false
		e.ScheduleAt(10*time.Nanosecond, func() {
			requirePanic(t, "before ordinary work", func() {
				e.ScheduleConfigurationAt(e.Now(), func() {})
			})
			e.ScheduleConfigurationAt(11*time.Nanosecond, func() { futureRan = true })
		})

		run(t, e, 20*time.Nanosecond)
		if !futureRan {
			t.Fatal("rejecting a retroactive change also prevented a valid future change")
		}
	})

	t.Run("after returning from RunUntil", func(t *testing.T) {
		e := engine.New(20 * time.Nanosecond)
		e.ScheduleAt(10*time.Nanosecond, func() {})
		run(t, e, 10*time.Nanosecond)

		requirePanic(t, "before ordinary work", func() {
			e.ScheduleConfigurationAt(e.Now(), func() {})
		})
	})

	t.Run("after an event-limit pause", func(t *testing.T) {
		e := engine.New(20 * time.Nanosecond)
		e.MaxEvents = 1
		e.ScheduleAt(10*time.Nanosecond, func() {})
		e.ScheduleAt(10*time.Nanosecond, func() {})
		if err := e.RunUntil(10 * time.Nanosecond); !errors.Is(err, engine.ErrEventLimit) {
			t.Fatalf("expected an event-limit pause, got %v", err)
		}
		requirePanic(t, "before ordinary work", func() {
			e.ScheduleConfigurationAt(e.Now(), func() {})
		})

		// The pending packet remains queued after the rejected insertion.
		e.MaxEvents = 2
		run(t, e, 10*time.Nanosecond)
		if e.EventsExecuted() != 2 || e.Pending() != 0 {
			t.Fatal("the rejected insertion changed queued work or event accounting")
		}
	})
}

// Advancing an empty clock does not mean a packet at that timestamp has already run.
func TestConfigurationCanUseCurrentTimeBeforeOrdinaryWork(t *testing.T) {
	e := engine.New(time.Second)
	run(t, e, 10*time.Nanosecond)
	var order []string
	e.ScheduleAt(e.Now(), func() { order = append(order, "packet") })
	e.ScheduleConfigurationAt(e.Now(), func() { order = append(order, "configuration") })
	run(t, e, e.Now())

	if !reflect.DeepEqual(order, []string{"configuration", "packet"}) {
		t.Fatalf("current-time callbacks ran in the wrong order: %v", order)
	}
}

// Both priorities share cancellation, the inclusive end time and callback accounting.
func TestConfigurationUsesExistingQueueLifecycle(t *testing.T) {
	e := engine.New(time.Second)
	if e.MaxEvents != engine.DefaultMaxEvents {
		t.Fatal("configuration priority changed the default event budget")
	}
	never := func() { t.Fatal("a canceled or cleared event executed") }
	id := e.ScheduleConfigurationAt(0, never)
	if !e.Cancel(id) || e.Cancel(id) {
		t.Fatal("configuration cancellation did not remove exactly one event")
	}
	e.ScheduleConfigurationAt(time.Second, never)
	e.ScheduleAt(time.Second, never)
	e.Clear()
	if e.Pending() != 0 {
		t.Fatal("Clear retained configuration events")
	}
	if _, pending := e.NextEventTime(); pending {
		t.Fatal("the empty queue reports a next event")
	}
	if e.ScheduleConfigurationAt(time.Second+1, never) != 0 {
		t.Fatal("configuration beyond the horizon was scheduled")
	}

	// A configuration is one callback, even if its body applies multiple settings.
	e.MaxEvents = 1
	settings := 0
	e.ScheduleAt(time.Second, func() { settings++ })
	e.ScheduleConfigurationAt(time.Second, func() { settings += 2 })
	if err := e.RunUntil(time.Second); !errors.Is(err, engine.ErrEventLimit) {
		t.Fatalf("expected the ordinary callback to wait for more budget, got %v", err)
	}
	if settings != 2 || e.EventsExecuted() != 1 || e.Pending() != 1 {
		t.Fatal("configuration did not execute as one prioritized callback")
	}
	e.MaxEvents = 2
	run(t, e, time.Second)
	if settings != 3 {
		t.Fatal("the ordinary callback was lost after the event-limit pause")
	}
}

// The configuration scheduler retains the ordinary scheduler's input checks.
func TestConfigurationRejectsInvalidSchedulingArguments(t *testing.T) {
	e := engine.New(time.Second)
	requirePanic(t, "must not be nil", func() { e.ScheduleConfigurationAt(0, nil) })
	requirePanic(t, "in the past", func() { e.ScheduleConfigurationAt(-1, func() {}) })
	run(t, e, time.Nanosecond)
	requirePanic(t, "in the past", func() { e.ScheduleConfigurationAt(0, func() {}) })
}

func run(t *testing.T, e *engine.Engine, until time.Duration) {
	t.Helper()
	if err := e.RunUntil(until); err != nil {
		t.Fatal(err)
	}
}

func requirePanic(t *testing.T, fragment string, callback func()) {
	t.Helper()
	defer func() {
		message, ok := recover().(string)
		if !ok || !strings.Contains(message, fragment) {
			t.Fatalf("want panic containing %q, got %q", fragment, message)
		}
	}()
	callback()
}
