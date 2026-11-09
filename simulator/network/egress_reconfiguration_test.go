package network

import (
	"math"
	"reflect"
	"testing"
	"time"

	"load-balancer/engine"
)

// An edit preserves the active grant; queued heads use the rate at dispatch.
func TestEgressReconfigurePreservesGrantAndAdmissionOrder(t *testing.T) {
	for _, test := range []struct {
		name   string
		rate   int64
		editAt time.Duration
		lastAt time.Duration
		wait   time.Duration
	}{
		{"faster mid-grant", 16_000_000_000, time.Nanosecond, 4 * time.Nanosecond, 3 * time.Nanosecond},
		{"slower mid-grant", 4_000_000_000, time.Nanosecond, 7 * time.Nanosecond, 6 * time.Nanosecond},
		{"faster at boundary", 16_000_000_000, 2 * time.Nanosecond, 4 * time.Nanosecond, 3 * time.Nanosecond},
		{"slower at boundary", 4_000_000_000, 2 * time.Nanosecond, 7 * time.Nanosecond, 6 * time.Nanosecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := engine.New(12 * time.Nanosecond)
			dispatcher := NewEgress(e, 8_000_000_000)
			config := Config{BandwidthBitsPerSecond: 64_000_000_000, QueueCapacity: 1}
			first := dispatcher.AddOutput(NewLink(e, config, nil))
			second := dispatcher.AddOutput(NewLink(e, config, nil))
			var arrivals []string
			record := func(name string) func() {
				return func() { arrivals = append(arrivals, name+"@"+e.Now().String()) }
			}
			first.Transmit(2, record("first"))
			second.Transmit(2, record("second"))
			first.Transmit(2, record("first-next"))

			before, pending := dispatcher.Stats(), e.Pending()
			update := dispatcher.PrepareForwardingConfig(test.rate)
			if dispatcher.Stats() != before || e.Pending() != pending {
				t.Fatal("preparing the rate changed live work")
			}
			// Configuration also wins when the edit coincides with the grant wake.
			e.ScheduleConfigurationAt(test.editAt, func() {
				grant, wake := dispatcher.nextGrantAt, dispatcher.wakeEvent
				update.Apply()
				if dispatcher.nextGrantAt != grant || dispatcher.wakeEvent != wake ||
					dispatcher.Stats().ForwardedPackets != 1 {
					t.Fatal("applying the edit dispatched work or replaced the active grant")
				}
			})
			runEgress(t, e, e.EndTime())

			want := []string{"first@1ns", "second@3ns", "first-next@" + test.lastAt.String()}
			if !reflect.DeepEqual(arrivals, want) {
				t.Fatalf("arrivals = %v, want %v", arrivals, want)
			}
			stats := dispatcher.Stats()
			if stats.ForwardingBitsPerSecond != test.rate || stats.ForwardedPackets != 3 ||
				stats.ForwardedBytes != 6 || stats.BudgetWait != test.wait || stats.QueuedPackets != 0 {
				t.Fatalf("the edit changed shared accounting: %+v", stats)
			}
		})
	}
}

// A rate cut can reserve the rest of the run without wrapping a duration or wake.
func TestEgressReconfigureQueuedGrantBeyondHorizon(t *testing.T) {
	for _, test := range []struct {
		name string
		size int
	}{
		{"past horizon", 1},
		{"duration overflow", 2_000_000_000},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := engine.New(10 * time.Nanosecond)
			dispatcher := NewEgress(e, math.MaxInt64)
			first := dispatcher.AddOutput(egressLink(e, 1))
			second := dispatcher.AddOutput(NewLink(e, Config{
				BandwidthBitsPerSecond: math.MaxInt64, QueueCapacity: 1,
			}, nil))
			arrivals := 0
			first.Transmit(1, func() { arrivals++ })
			second.Transmit(test.size, func() { arrivals++ })
			first.Transmit(1, func() { t.Error("the reserved budget cannot become free") })
			cut := dispatcher.PrepareForwardingConfig(1)
			restore := dispatcher.PrepareForwardingConfig(math.MaxInt64)
			e.ScheduleConfigurationAt(time.Nanosecond, cut.Apply)
			e.ScheduleConfigurationAt(2*time.Nanosecond, restore.Apply)
			runEgress(t, e, e.EndTime())

			stats := dispatcher.Stats()
			if arrivals != 2 || stats.ForwardedPackets != 2 || stats.QueuedPackets != 1 ||
				!stats.BudgetBlocked || stats.BudgetWait != e.EndTime() {
				t.Fatalf("the rate cut lost work or changed its reservation: %+v", stats)
			}
			if dispatcher.wakeEvent != 0 || e.Pending() != 0 {
				t.Fatal("an unfinishable grant created a wrapped wake event")
			}
		})
	}
}

func TestEgressReconfigureRejectsInvalidRateBeforeMutation(t *testing.T) {
	e := engine.New(time.Second)
	dispatcher := NewEgress(e, 8_000_000_000)
	output := dispatcher.AddOutput(egressLink(e, 1))
	output.Transmit(2, func() {})
	output.Transmit(2, func() {})
	before, pending := dispatcher.Stats(), e.Pending()
	for _, rate := range []int64{0, -1} {
		expectEgressPanic(t, func() { dispatcher.PrepareForwardingConfig(rate) })
	}
	if dispatcher.Stats() != before || e.Pending() != pending {
		t.Fatal("an invalid edit changed counters or pending work")
	}
}
