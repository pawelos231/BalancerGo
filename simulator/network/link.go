// Package network models directed links driven by a shared simulation engine.
// Links and their random sources belong to the same goroutine as the engine.
// They know nothing about endpoint types, transport protocols or packet payloads.
package network

import (
	"math"
	"math/bits"
	"math/rand"
	"time"

	"load-balancer/engine"
)

// Config describes a directed link's transmission, travel and buffering limits.
type Config struct {
	BandwidthBitsPerSecond      int64
	PacketTravelTime            time.Duration // Virtual travel time after transmission.
	QueueCapacity               int
	LossRate                    float64
	QueuePolicy                 string // Empty or tailDrop preserves ordinary FIFO tail drop.
	CongestionThresholdPercent  float64
	CongestionSignalProbability float64
}

// Stats holds packet and byte counters, plus the current state of the link.
type Stats struct {
	OfferedPackets           uint64
	OfferedBytes             uint64
	AcceptedPackets          uint64
	AcceptedBytes            uint64
	QueueDroppedPackets      uint64
	QueueDroppedBytes        uint64
	ECNMarkedPackets         uint64
	ECNMarkedBytes           uint64
	CongestionDroppedPackets uint64 // Early drops, also included in QueueDroppedPackets.
	CongestionDroppedBytes   uint64
	// Completed serialization includes packets later lost or still travelling.
	TransmittedBytes uint64
	// Randomly lost packets still consumed link bandwidth.
	LostPackets       uint64
	LostBytes         uint64
	DeliveredPackets  uint64
	DeliveredBytes    uint64
	QueuedPackets     int
	Transmitting      bool
	TravellingPackets int
}

// Link models a one-way network path, e.g. client -> balancer or balancer -> server.
// Multiple TCP connections share its bandwidth and packet queue.
type Link struct {
	engine        *engine.Engine
	config        Config
	rng           *rand.Rand
	congestionRNG *rand.Rand
	queue         []transmission
	queueHead     int
	transmitting  bool
	travelling    int
	stats         Stats
	onReady       func()
}

// NewLink creates an idle link and panics on invalid configuration.
func NewLink(e *engine.Engine, config Config, rng *rand.Rand) *Link {
	if e == nil {
		panic("network: engine must not be nil")
	}

	if config.BandwidthBitsPerSecond <= 0 {
		panic("network: bandwidth must be positive bits per second")
	}

	if config.PacketTravelTime < 0 {
		panic("network: packet travel time must not be negative")
	}

	if config.QueueCapacity < 0 {
		panic("network: queue capacity must not be negative")
	}

	if math.IsNaN(config.LossRate) || math.IsInf(config.LossRate, 0) || config.LossRate < 0 || config.LossRate > 1 {
		panic("network: loss rate must be in 0..1")
	}

	if config.LossRate > 0 && config.LossRate < 1 && rng == nil {
		panic("network: probabilistic loss requires a link-owned random source")
	}
	validateCongestionConfig(config)

	return &Link{engine: e, config: config, rng: rng}
}

// Transmit returns whether a packet was admitted, rather than whether it will
// arrive by the simulation's end time. A full waiting queue rejects new packets.
// sizeBytes must be nonnegative and onArrival must be nonnil. The caller owns
// freezing any packet state captured by onArrival at the moment of transmission.
// Even zero-byte, zero-delay packets are delivered through queued engine events.
func (l *Link) Transmit(sizeBytes int, onArrival func()) bool {
	if onArrival == nil {
		panic("network: arrival callback must not be nil")
	}
	return l.TransmitECN(sizeBytes, NotCapable, func(bool) { onArrival() })
}

// TransmitECN applies congestion signaling once, when a packet must wait.
// The callback reports its final CE state, including a mark received earlier.
func (l *Link) TransmitECN(sizeBytes int, state ECNState, onArrival func(marked bool)) bool {
	if l.onReady != nil {
		panic("network: managed link must be started through its external queue")
	}
	if sizeBytes < 0 {
		panic("network: packet size must not be negative")
	}
	if onArrival == nil {
		panic("network: arrival callback must not be nil")
	}
	validateECNState(state)

	packet := transmission{
		sizeBytes: sizeBytes,
		duration:  serializationDuration(sizeBytes, l.config.BandwidthBitsPerSecond),
	}
	queueFull := l.transmitting && l.queued() >= l.config.QueueCapacity
	selected := !queueFull && l.transmitting && l.shouldSignalCongestion(l.queued()+1)
	marked := state == AlreadyMarked || (selected && state == Capable)
	packet.onArrival = func() { onArrival(marked) }
	l.stats.OfferedPackets++
	l.stats.OfferedBytes += uint64(sizeBytes)

	if queueFull || (selected && state == NotCapable) {
		l.stats.QueueDroppedPackets++
		l.stats.QueueDroppedBytes += uint64(sizeBytes)
		if !queueFull {
			l.stats.CongestionDroppedPackets++
			l.stats.CongestionDroppedBytes += uint64(sizeBytes)
		}
		return false
	}
	if selected && state == Capable {
		l.stats.ECNMarkedPackets++
		l.stats.ECNMarkedBytes += uint64(sizeBytes)
	}

	l.stats.AcceptedPackets++
	l.stats.AcceptedBytes += uint64(sizeBytes)
	if l.transmitting {
		l.queue = append(l.queue, packet)
	} else {
		l.start(packet)
	}
	return true
}

// Ready reports a free transmitter; packets still travelling do not block it.
func (l *Link) Ready() bool {
	return !l.transmitting
}

// TryStart starts immediately or returns false without queueing or counting a
// busy attempt. Completion and arrival callbacks always run as engine events.
func (l *Link) TryStart(sizeBytes int, onArrival func()) bool {
	if sizeBytes < 0 {
		panic("network: packet size must not be negative")
	}
	if onArrival == nil {
		panic("network: arrival callback must not be nil")
	}
	if !l.Ready() {
		return false
	}

	packet := transmission{
		sizeBytes: sizeBytes,
		duration:  serializationDuration(sizeBytes, l.config.BandwidthBitsPerSecond),
		onArrival: onArrival,
	}

	l.stats.OfferedPackets++
	l.stats.OfferedBytes += uint64(sizeBytes)
	l.stats.AcceptedPackets++
	l.stats.AcceptedBytes += uint64(sizeBytes)
	l.start(packet)

	return true
}

// useExternalQueue gives a fresh link to one manager without notifying it yet.
func (l *Link) useExternalQueue(onReady func()) {
	if onReady == nil {
		panic("network: readiness callback must not be nil")
	}
	if l.onReady != nil {
		panic("network: link already has an external queue")
	}

	hasTraffic := l.stats.OfferedPackets != 0 || l.transmitting || l.queued() != 0 || l.travelling != 0
	if hasTraffic {
		panic("network: external queue requires a fresh link")
	}

	l.onReady = onReady
}

// Config returns a value copy of this directed link's own configuration.
func (l *Link) Config() Config {
	return l.config
}

// Stats returns copied counters and the current physical link state.
func (l *Link) Stats() Stats {
	stats := l.stats
	stats.QueuedPackets = l.queued()
	stats.Transmitting = l.transmitting
	stats.TravellingPackets = l.travelling
	return stats
}

// DiscardPending drops unfinished work after the whole engine queue is cleared.
func (l *Link) DiscardPending() {
	l.queue = nil
	l.queueHead = 0
	l.transmitting = false
	l.travelling = 0
}

// queued excludes consumed slots retained at the front of the slice.
func (l *Link) queued() int {
	return len(l.queue) - l.queueHead
}

// start releases the transmitter after serialization, before travel completes.
func (l *Link) start(packet transmission) {
	l.transmitting = true
	l.engine.ScheduleAfter(packet.duration, func() {
		// Transmission has finished, so the sender is available again.
		l.transmitting = false
		l.stats.TransmittedBytes += uint64(packet.sizeBytes)

		lossRate := l.config.LossRate
		packetLost := lossRate == 1
		if lossRate > 0 && lossRate < 1 {
			packetLost = l.rng.Float64() < lossRate
		}

		// A surviving packet still needs time to reach the receiver.
		if packetLost {
			l.stats.LostPackets++
			l.stats.LostBytes += uint64(packet.sizeBytes)
		} else {
			l.travelling++
			l.engine.ScheduleAfter(l.config.PacketTravelTime, func() {
				l.travelling--
				l.stats.DeliveredPackets++
				l.stats.DeliveredBytes += uint64(packet.sizeBytes)
				packet.onArrival()
			})
		}

		// A manager owns the waiting queue; autonomous links keep their own FIFO.
		if l.onReady != nil {
			l.onReady()
		} else if l.queued() > 0 {
			nextPacket := l.pop()
			l.start(nextPacket)
		}
	})
}

// pop releases one waiting packet and occasionally compacts the queue.
func (l *Link) pop() transmission {
	const minConsumedSlots = 1024

	// Take the first waiting packet and release its slot.
	packet := l.queue[l.queueHead]
	l.queue[l.queueHead] = transmission{}
	l.queueHead++

	remainingPacketCount := len(l.queue) - l.queueHead
	if remainingPacketCount == 0 {
		l.queue = l.queue[:0]
		l.queueHead = 0
		return packet
	}

	// Move waiting packets to the front when enough slots have been consumed.
	enoughConsumedSlots := l.queueHead >= minConsumedSlots
	atLeastHalfConsumed := l.queueHead >= remainingPacketCount
	if enoughConsumedSlots && atLeastHalfConsumed {
		copy(l.queue, l.queue[l.queueHead:])
		clear(l.queue[remainingPacketCount:])
		l.queue = l.queue[:remainingPacketCount]
		l.queueHead = 0
	}

	return packet
}

// transmission freezes one packet's cost and delivery callback while queued.
type transmission struct {
	sizeBytes int
	duration  time.Duration
	onArrival func()
}

// serializationDuration computes ceil(bytes * 8 * 1e9 / bitsPerSecond) without
// overflowing the intermediate product. A positive-size packet takes at least
// one nanosecond. Durations outside time.Duration's range panic explicitly.
func serializationDuration(sizeBytes int, bandwidthBitsPerSecond int64) time.Duration {
	hi, lo := bits.Mul64(uint64(sizeBytes), 8*uint64(time.Second))
	divisor := uint64(bandwidthBitsPerSecond)
	if hi >= divisor {
		panic("network: packet serialization duration overflows")
	}
	nanoseconds, remainder := bits.Div64(hi, lo, divisor)
	if remainder > 0 {
		if nanoseconds == math.MaxUint64 {
			panic("network: packet serialization duration overflows")
		}
		nanoseconds++
	}
	if nanoseconds > math.MaxInt64 {
		panic("network: packet serialization duration overflows")
	}
	return time.Duration(nanoseconds)
}
