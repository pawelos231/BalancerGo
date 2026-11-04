package network

import (
	"math"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"load-balancer/engine"
)

type congestionQueue interface {
	Transmit(int, func()) bool
	TransmitECN(int, ECNState, func(bool)) bool
	Stats() Stats
}

var congestionQueueOwners = []struct {
	name string
	make func(*engine.Engine, *Link) congestionQueue
}{
	{"link", func(_ *engine.Engine, link *Link) congestionQueue { return link }},
	{"input", func(_ *engine.Engine, link *Link) congestionQueue { return NewIngress(link).AddInput() }},
	{"output", func(e *engine.Engine, link *Link) congestionQueue {
		return NewEgress(e, math.MaxInt64).AddOutput(link)
	}},
}

func TestCongestionThresholdUsesArrivingWaitingPacket(t *testing.T) {
	for _, owner := range congestionQueueOwners {
		t.Run(owner.name, func(t *testing.T) {
			e := engine.New(100 * time.Millisecond)
			link := NewLink(e, Config{
				BandwidthBitsPerSecond: 8_000, QueueCapacity: 3,
				QueuePolicy: "threshold", CongestionThresholdPercent: 50, CongestionSignalProbability: 1,
			}, nil)
			queue := owner.make(e, link)
			var marks []bool
			arrive := func(marked bool) { marks = append(marks, marked) }
			// ceil(3 * 50 / 100) is two waiting packets; the active packet is excluded.
			for size := 1; size <= 4; size++ {
				state := Capable
				if size == 4 {
					state = AlreadyMarked
				}
				if !queue.TransmitECN(size, state, arrive) {
					t.Fatal("one active and three waiting packets should be admitted")
				}
			}
			if queue.TransmitECN(5, Capable, arrive) || queue.TransmitECN(6, NotCapable, arrive) {
				t.Fatal("a full queue must reject packets regardless of ECN capability")
			}
			stats := queue.Stats()
			if stats.ECNMarkedPackets != 1 || stats.ECNMarkedBytes != 3 {
				t.Fatalf("marks must count only the new admission-time transition: %+v", stats)
			}
			if stats.CongestionDroppedPackets != 0 || stats.QueueDroppedPackets != 2 || stats.QueueDroppedBytes != 11 {
				t.Fatalf("only the two full-queue arrivals should drop: %+v", stats)
			}
			if stats.OfferedPackets != stats.AcceptedPackets+stats.QueueDroppedPackets || stats.OfferedBytes != stats.AcceptedBytes+stats.QueueDroppedBytes {
				t.Fatalf("admission totals do not balance: %+v", stats)
			}
			runLinkUntil(t, e, e.EndTime())
			if !reflect.DeepEqual(marks, []bool{false, false, true, true}) {
				t.Fatalf("arrival marks = %v", marks)
			}
			if after := queue.Stats(); after.ECNMarkedPackets != 1 || after.ECNMarkedBytes != 3 {
				t.Fatalf("dispatch or delivery repeated marking: %+v", after)
			}
		})
	}
}

func TestCongestionEarlyDropAndExistingMark(t *testing.T) {
	for _, owner := range congestionQueueOwners {
		t.Run(owner.name, func(t *testing.T) {
			e := engine.New(100 * time.Millisecond)
			link := NewLink(e, Config{
				BandwidthBitsPerSecond: 8_000, QueueCapacity: 2,
				QueuePolicy: "threshold", CongestionSignalProbability: 1,
			}, nil)
			queue := owner.make(e, link)
			var marks []bool
			arrive := func(marked bool) { marks = append(marks, marked) }
			if !queue.Transmit(1, func() { marks = append(marks, false) }) {
				t.Fatal("an idle legacy packet should start without an early signal")
			}
			if queue.Transmit(2, func() { t.Error("early-dropped packet arrived") }) {
				t.Fatal("legacy Transmit should behave as NotCapable")
			}
			if !queue.TransmitECN(3, Capable, arrive) || !queue.TransmitECN(4, AlreadyMarked, arrive) {
				t.Fatal("two ECN-capable waiting packets should be admitted")
			}
			if queue.TransmitECN(5, AlreadyMarked, arrive) {
				t.Fatal("ECN admission did not respect the hard waiting limit")
			}
			stats := queue.Stats()
			if stats.CongestionDroppedPackets != 1 || stats.CongestionDroppedBytes != 2 {
				t.Fatalf("only the non-capable waiting arrival should drop early: %+v", stats)
			}
			if stats.QueueDroppedPackets != 2 || stats.QueueDroppedBytes != 7 {
				t.Fatalf("total queue drops must include early and full-queue drops: %+v", stats)
			}
			if stats.ECNMarkedPackets != 1 || stats.ECNMarkedBytes != 3 {
				t.Fatalf("only the newly marked packet should increase CE counters: %+v", stats)
			}
			switch queue := queue.(type) {
			case *Input:
				shared := queue.ingress.Stats()
				if shared.ECNMarkedPackets != 1 || shared.CongestionDroppedBytes != 2 || shared.QueueDroppedPackets != 2 {
					t.Fatalf("ingress lost queue-owner counters: %+v", shared)
				}
			case *Output:
				shared := queue.egress.Stats()
				if shared.ECNMarkedPackets != 1 || shared.CongestionDroppedBytes != 2 || shared.QueueDroppedPackets != 2 {
					t.Fatalf("egress lost queue-owner counters: %+v", shared)
				}
			}
			runLinkUntil(t, e, e.EndTime())
			if !reflect.DeepEqual(marks, []bool{false, true, true}) {
				t.Fatalf("arrival marks = %v", marks)
			}
		})
	}
}

func TestCongestionZeroCapacityAndTravellingPackets(t *testing.T) {
	for _, owner := range congestionQueueOwners {
		t.Run(owner.name, func(t *testing.T) {
			e := engine.New(30 * time.Millisecond)
			link := NewLink(e, Config{
				BandwidthBitsPerSecond: 8_000, PacketTravelTime: 10 * time.Millisecond,
				QueuePolicy: "threshold", CongestionSignalProbability: 1,
			}, nil)
			queue := owner.make(e, link)
			var marks []bool
			arrive := func(marked bool) { marks = append(marks, marked) }
			if !queue.TransmitECN(1, Capable, arrive) || queue.TransmitECN(2, Capable, arrive) {
				t.Fatal("zero capacity should admit only an immediate transmission")
			}
			runLinkUntil(t, e, time.Millisecond)
			if !queue.TransmitECN(1, AlreadyMarked, arrive) {
				t.Fatal("a travelling packet must not consume waiting capacity")
			}
			runLinkUntil(t, e, e.EndTime())
			stats := queue.Stats()
			if stats.ECNMarkedPackets != 0 || stats.CongestionDroppedPackets != 0 || stats.QueueDroppedPackets != 1 || !reflect.DeepEqual(marks, []bool{false, true}) {
				t.Fatalf("zero-capacity signaling or existing CE changed: marks=%v stats=%+v", marks, stats)
			}
		})
	}
}

type congestionCountingSource struct {
	rand.Source
	draws int
}

func (source *congestionCountingSource) Int63() int64 {
	source.draws++
	return source.Source.Int63()
}

func TestCongestionSkipsUnnecessaryRandomDraws(t *testing.T) {
	for _, owner := range congestionQueueOwners {
		for _, test := range []struct {
			name        string
			policy      string
			probability float64
			capacity    int
			threshold   float64
		}{
			{"default", "", 0.5, 1, 0},
			{"tailDrop", "tailDrop", 0.5, 1, 0},
			{"probabilityZero", "threshold", 0, 1, 0},
			{"probabilityOne", "threshold", 1, 1, 0},
			{"zeroCapacity", "threshold", 0.5, 0, 0},
			{"belowThreshold", "threshold", 0.5, 2, 100},
		} {
			t.Run(owner.name+"/"+test.name, func(t *testing.T) {
				e := engine.New(time.Second)
				link := NewLink(e, Config{
					BandwidthBitsPerSecond: 8_000, QueueCapacity: test.capacity,
					QueuePolicy: test.policy, CongestionSignalProbability: test.probability, CongestionThresholdPercent: test.threshold,
				}, nil)
				source := &congestionCountingSource{Source: rand.NewSource(7)}
				link.SetCongestionRNG(rand.New(source))
				queue := owner.make(e, link)
				queue.TransmitECN(1, Capable, func(bool) {})
				queue.TransmitECN(1, Capable, func(bool) {})
				if source.draws != 0 {
					t.Fatalf("unnecessary congestion draws: %d", source.draws)
				}
				wantMarks := uint64(0)
				if test.policy == "threshold" && test.probability == 1 && test.capacity > 0 {
					wantMarks = 1
				}
				if stats := queue.Stats(); stats.ECNMarkedPackets != wantMarks || stats.CongestionDroppedPackets != 0 {
					t.Fatalf("inactive and certain signaling counters changed: %+v", stats)
				}
				if test.capacity <= 1 {
					if queue.TransmitECN(1, Capable, func(bool) {}) || source.draws != 0 {
						t.Fatal("hard queue rejection must not draw or admit")
					}
				}
			})
		}
	}
}

func TestCongestionRNGIsIndependentFromLossAndDispatch(t *testing.T) {
	for _, owner := range congestionQueueOwners {
		t.Run(owner.name, func(t *testing.T) {
			e := engine.New(100 * time.Millisecond)
			lossRNG := rand.New(rand.NewSource(9))
			lossControl := rand.New(rand.NewSource(9))
			aqmRNG := rand.New(rand.NewSource(11))
			aqmControl := rand.New(rand.NewSource(11))
			link := NewLink(e, Config{
				BandwidthBitsPerSecond: 8_000, QueueCapacity: 20, LossRate: 0.5,
				QueuePolicy: "threshold", CongestionSignalProbability: 0.5,
			}, lossRNG)
			link.SetCongestionRNG(aqmRNG)
			queue := owner.make(e, link)
			var expectedMarks [21]bool
			markedPackets := uint64(0)
			for packet := 0; packet < len(expectedMarks); packet++ {
				if packet > 0 {
					expectedMarks[packet] = aqmControl.Float64() < 0.5
					if expectedMarks[packet] {
						markedPackets++
					}
				}
				packet := packet
				if !queue.TransmitECN(1, Capable, func(marked bool) {
					if marked != expectedMarks[packet] {
						t.Errorf("packet %d mark = %v, want %v", packet, marked, expectedMarks[packet])
					}
				}) {
					t.Fatal("the configured waiting capacity should admit all packets")
				}
			}
			// No serialization has completed; admission must leave the loss stream alone.
			if lossRNG.Int63() != lossControl.Int63() {
				t.Fatal("AQM consumed the loss random stream")
			}
			beforeHardDrop := aqmRNG.Int63()
			if beforeHardDrop != aqmControl.Int63() {
				t.Fatal("AQM did not use exactly one draw per waiting admission")
			}
			if queue.TransmitECN(1, Capable, func(bool) {}) {
				t.Fatal("a full queue admitted a packet")
			}
			runLinkUntil(t, e, e.EndTime())
			lostPackets := uint64(0)
			for packet := 0; packet < len(expectedMarks); packet++ {
				if lossControl.Float64() < 0.5 {
					lostPackets++
				}
			}
			stats := queue.Stats()
			if stats.LostPackets != lostPackets || stats.ECNMarkedPackets != markedPackets || stats.ECNMarkedBytes != markedPackets {
				t.Fatalf("loss changed admission marks or independent loss decisions: %+v", stats)
			}
			if aqmRNG.Int63() != aqmControl.Int63() {
				t.Fatal("hard drop or dispatch consumed another congestion draw")
			}
		})
	}
}

func TestCongestionEgressBudgetWaitingCounts(t *testing.T) {
	e := engine.New(100 * time.Millisecond)
	egress := NewEgress(e, 8_000)
	config := Config{BandwidthBitsPerSecond: 8_000_000, QueueCapacity: 1, QueuePolicy: "threshold", CongestionThresholdPercent: 100, CongestionSignalProbability: 1}
	first := egress.AddOutput(NewLink(e, config, nil))
	second := egress.AddOutput(NewLink(e, config, nil))
	first.Transmit(10, func() {})
	markedAtArrival := false
	if !second.TransmitECN(1, Capable, func(marked bool) { markedAtArrival = marked }) || !second.link.Ready() {
		t.Fatal("the free second link should wait for the shared forwarding budget")
	}
	if second.Stats().ECNMarkedPackets != 1 || second.Stats().QueuedPackets != 1 || second.TransmitECN(1, Capable, func(bool) {}) {
		t.Fatal("budget waiting must trigger marking and consume the hard waiting slot")
	}
	runLinkUntil(t, e, e.EndTime())
	if !markedAtArrival || egress.Stats().ECNMarkedPackets != 1 {
		t.Fatal("budget-waiting mark was lost or repeated")
	}
}

func TestCongestionConfigurationAndStateValidation(t *testing.T) {
	for _, policy := range []string{"", "tailDrop", "threshold"} {
		for _, invalid := range []float64{-1, 101, math.NaN(), math.Inf(1)} {
			expectLinkPanic(t, func() {
				NewLink(engine.New(time.Second), Config{BandwidthBitsPerSecond: 8_000, QueuePolicy: policy, CongestionThresholdPercent: invalid}, nil)
			})
		}
		for _, invalid := range []float64{-1, 1.1, math.NaN(), math.Inf(-1)} {
			expectLinkPanic(t, func() {
				NewLink(engine.New(time.Second), Config{BandwidthBitsPerSecond: 8_000, QueuePolicy: policy, CongestionSignalProbability: invalid}, nil)
			})
		}
	}
	expectLinkPanic(t, func() {
		NewLink(engine.New(time.Second), Config{BandwidthBitsPerSecond: 8_000, QueuePolicy: "unknown"}, nil)
	})
	for _, owner := range congestionQueueOwners {
		t.Run(owner.name, func(t *testing.T) {
			e := engine.New(time.Second)
			link := NewLink(e, Config{BandwidthBitsPerSecond: 8_000}, nil)
			queue := owner.make(e, link)
			expectLinkPanic(t, func() { queue.TransmitECN(1, ECNState(99), func(bool) {}) })
			expectLinkPanic(t, func() { queue.TransmitECN(1, Capable, nil) })
			if queue.Stats() != (Stats{}) || e.Pending() != 0 {
				t.Fatal("invalid ECN admission changed state")
			}
		})
	}
}
