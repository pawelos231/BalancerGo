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

type Config struct {
	BandwidthBitsPerSecond int64
	PacketTravelTime       time.Duration // Virtual travel time after transmission.
	QueueCapacity          int
	LossRate               float64
}

// Stats holds packet and byte counters, plus the current state of the link.
type Stats struct {
	OfferedPackets      uint64
	OfferedBytes        uint64
	AcceptedPackets     uint64
	AcceptedBytes       uint64
	QueueDroppedPackets uint64
	QueueDroppedBytes   uint64
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
	engine       *engine.Engine
	config       Config
	rng          *rand.Rand
	queue        []transmission
	queueHead    int
	transmitting bool
	travelling   int
	stats        Stats
}

// NewLink panics on invalid configuration
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

	return &Link{engine: e, config: config, rng: rng}
}

// Transmit returns whether a packet was admitted, rather than whether it will
// arrive by the simulation's end time. A full waiting queue rejects new packets.
// sizeBytes must be nonnegative and onArrival must be nonnil. The caller owns
// freezing any packet state captured by onArrival at the moment of transmission.
// Even zero-byte, zero-delay packets are delivered through queued engine events.
func (l *Link) Transmit(sizeBytes int, onArrival func()) bool {
	if sizeBytes < 0 {
		panic("network: packet size must not be negative")
	}
	if onArrival == nil {
		panic("network: arrival callback must not be nil")
	}
	packet := transmission{sizeBytes: sizeBytes, duration: serializationDuration(sizeBytes, l.config.BandwidthBitsPerSecond), onArrival: onArrival}
	l.stats.OfferedPackets++
	l.stats.OfferedBytes += uint64(sizeBytes)
	if l.transmitting && l.queued() >= l.config.QueueCapacity {
		l.stats.QueueDroppedPackets++
		l.stats.QueueDroppedBytes += uint64(sizeBytes)
		return false
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

// Config returns a value copy of this directed link's own configuration.
func (l *Link) Config() Config {
	return l.config
}

func (l *Link) Stats() Stats {
	stats := l.stats
	stats.QueuedPackets = l.queued()
	stats.Transmitting = l.transmitting
	stats.TravellingPackets = l.travelling
	return stats
}

func (l *Link) queued() int {
	return len(l.queue) - l.queueHead
}

func (l *Link) start(packet transmission) {
	l.transmitting = true
	l.engine.ScheduleAfter(packet.duration, func() {
		// Transmission has finished, so the sender is available again.
		l.transmitting = false

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

		// Start the next transmission while earlier packets may still be in flight.
		if l.queued() > 0 {
			nextPacket := l.pop()
			l.start(nextPacket)
		}
	})
}

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
