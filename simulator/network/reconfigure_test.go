package network

import (
	"math/rand"
	"reflect"
	"testing"
	"time"

	"load-balancer/engine"
)

// A queued packet can become slower than time.Duration allows after a bandwidth edit.
// It remains in service beyond the horizon, rather than failing during event execution.
func TestReconfigureQueuedTransmissionBeyondDurationRange(t *testing.T) {
	for _, queue := range []string{"link", "output", "input"} {
		t.Run(queue, func(t *testing.T) {
			e := engine.New(time.Second)
			config := Config{BandwidthBitsPerSecond: 8_000_000_000, QueueCapacity: 1}
			link := NewLink(e, config, nil)
			transmit := link.Transmit
			if queue == "output" {
				transmit = NewEgress(e, 8_000_000_000).AddOutput(link).Transmit
			} else if queue == "input" {
				transmit = NewIngress(link).AddInput().Transmit
			}
			arrivals := 0
			transmit(1, func() { arrivals++ })
			transmit(2_000_000_000, func() { t.Fatal("the oversized transmission cannot finish") })
			config.BandwidthBitsPerSecond = 1
			link.PrepareConfig(config, nil).Apply()

			runLinkUntil(t, e, e.EndTime())
			stats := link.Stats()
			if arrivals != 1 || !stats.Transmitting || stats.QueuedPackets != 0 || stats.AcceptedPackets != 2 {
				t.Fatalf("admitted unfinished work was lost or restarted: %+v", stats)
			}
		})
	}
}

// Packet 1 keeps its old 1 ms transmission and 10 ms travel; packet 2 starts with new limits.
func TestLinkReconfigureKeepsActiveSettingsAndUpdatesWaitingPackets(t *testing.T) {
	e := engine.New(30 * time.Millisecond)
	config := Config{
		BandwidthBitsPerSecond: 8_000,
		PacketTravelTime:       10 * time.Millisecond,
		QueueCapacity:          1,
	}
	link := NewLink(e, config, nil)
	var arrivals []time.Duration
	arrive := func() { arrivals = append(arrivals, e.Now()) }
	link.Transmit(1, arrive)
	link.Transmit(1, arrive)

	config.BandwidthBitsPerSecond = 16_000
	config.PacketTravelTime = 20 * time.Millisecond
	update := link.PrepareConfig(config, nil)
	e.ScheduleConfigurationAt(500*time.Microsecond, update.Apply)

	// Both packets are travelling by 2 ms; another edit must not move either arrival.
	config.PacketTravelTime = 0
	config.LossRate = 1
	later := link.PrepareConfig(config, nil)
	e.ScheduleConfigurationAt(2*time.Millisecond, later.Apply)
	runLinkUntil(t, e, e.EndTime())

	want := []time.Duration{11 * time.Millisecond, 21*time.Millisecond + 500*time.Microsecond}
	if !reflect.DeepEqual(arrivals, want) {
		t.Fatalf("arrival times = %v, want %v", arrivals, want)
	}
	if stats := link.Stats(); stats.AcceptedPackets != 2 || stats.DeliveredPackets != 2 || stats.LostPackets != 0 {
		t.Fatalf("editing travelling packets changed their outcomes: %+v", stats)
	}
}

// A smaller queue keeps admitted packets, then drops new arrivals until a slot opens.
func TestLinkReconfigureShrinksOrdinaryAndManagedQueuesWithoutDiscardingWork(t *testing.T) {
	for _, managed := range []bool{false, true} {
		name := "ordinary"
		if managed {
			name = "managed output"
		}
		t.Run(name, func(t *testing.T) {
			e := engine.New(5 * time.Millisecond)
			config := Config{BandwidthBitsPerSecond: 8_000, QueueCapacity: 2}
			link := NewLink(e, config, nil)
			transmit, stats := link.Transmit, link.Stats
			if managed {
				output := NewEgress(e, 8_000_000_000).AddOutput(link)
				transmit, stats = output.Transmit, output.Stats
			}
			arrivals := 0
			arrive := func() { arrivals++ }
			for index := 0; index < 3; index++ {
				if !transmit(1, arrive) {
					t.Fatal("one active packet and two waiting packets should fit")
				}
			}

			before := stats()
			config.QueueCapacity = 1
			link.PrepareConfig(config, nil).Apply()
			if stats() != before {
				t.Fatal("changing capacity discarded work or reset counters")
			}
			if transmit(1, arrive) {
				t.Fatal("a queue above its new capacity admitted another packet")
			}
			runLinkUntil(t, e, time.Millisecond)
			if transmit(1, arrive) {
				t.Fatal("the one remaining waiting packet still occupies the new capacity")
			}
			runLinkUntil(t, e, 2*time.Millisecond)
			if !transmit(1, arrive) {
				t.Fatal("the queue did not admit a packet after a waiting slot opened")
			}
			runLinkUntil(t, e, e.EndTime())
			if got := stats(); arrivals != 4 || got.DeliveredPackets != 4 || got.QueueDroppedPackets != 2 {
				t.Fatalf("queue shrink lost admitted work: arrivals=%d stats=%+v", arrivals, got)
			}
		})
	}
}

// Switching loss to 100% affects the waiting packet, not the packet already transmitting.
func TestLinkReconfigureCapturesLossWhenTransmissionStarts(t *testing.T) {
	e := engine.New(3 * time.Millisecond)
	config := Config{BandwidthBitsPerSecond: 8_000, QueueCapacity: 1}
	link := NewLink(e, config, nil)
	arrivals := 0
	link.Transmit(1, func() { arrivals++ })
	link.Transmit(1, func() { t.Fatal("the waiting packet should use the new loss rate") })

	config.LossRate = 1
	update := link.PrepareConfig(config, nil)
	e.ScheduleConfigurationAt(500*time.Microsecond, update.Apply)
	runLinkUntil(t, e, e.EndTime())
	if stats := link.Stats(); arrivals != 1 || stats.LostPackets != 1 || stats.TransmittedBytes != 2 {
		t.Fatalf("loss was not frozen separately for each transmission: arrivals=%d stats=%+v", arrivals, stats)
	}
}

// Preparation must reject an invalid edit without changing the live configuration or counters.
func TestLinkReconfigureValidatesBeforeChangingState(t *testing.T) {
	e := engine.New(time.Second)
	config := Config{BandwidthBitsPerSecond: 8_000, QueueCapacity: 1}
	link := NewLink(e, config, nil)
	link.Transmit(1, func() {})
	before := link.Stats()
	invalid := config
	invalid.QueueCapacity = -1
	expectLinkPanic(t, func() { link.PrepareConfig(invalid, nil) })
	invalid = config
	invalid.LossRate = 0.5
	expectLinkPanic(t, func() { link.PrepareConfig(invalid, nil) })
	if link.Config() != config || link.Stats() != before {
		t.Fatal("rejected preparation changed the link")
	}
}

// Disabling and enabling signaling must resume its random sequence rather than reseed it.
func TestLinkReconfigurePreservesCongestionRandomSequence(t *testing.T) {
	e := engine.New(time.Second)
	config := Config{BandwidthBitsPerSecond: 8_000, QueueCapacity: 10}
	link := NewLink(e, config, nil)
	config.QueuePolicy = "threshold"
	config.CongestionSignalProbability = 0.5
	stream := rand.New(rand.NewSource(7))
	expected := rand.New(rand.NewSource(7))
	link.PrepareConfig(config, stream).Apply()
	if link.shouldSignalCongestion(1) != (expected.Float64() < 0.5) {
		t.Fatal("the first signal did not use the prepared random stream")
	}

	paused := config
	paused.QueuePolicy = "tailDrop"
	link.PrepareConfig(paused, nil).Apply()
	link.PrepareConfig(config, rand.New(rand.NewSource(7))).Apply()
	if link.shouldSignalCongestion(1) != (expected.Float64() < 0.5) {
		t.Fatal("reenabling signaling replaced the existing random sequence")
	}
}
