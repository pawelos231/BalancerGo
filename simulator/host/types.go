// Package host models a finite FIFO and a pool of virtual packet-processing cores.
// The resource knows only when work finishes; protocol and routing callbacks stay outside.
package host

import (
	"fmt"
	"math"
	"time"
)

// Config sets how many packets can be processed at once and how long each takes.
// QueueCapacity counts waiting packets only; zero still permits work on an idle core.
type Config struct {
	Cores                int
	PacketProcessingTime time.Duration
	QueueCapacity        int
}

// DefaultConfig preserves immediate packet handling until a positive CPU cost is chosen.
func DefaultConfig() Config {
	return Config{Cores: 1, QueueCapacity: 1024}
}

// Validate also keeps cumulative core-time and waiting-time counters within time.Duration.
func (config Config) Validate(duration time.Duration) error {
	if config.Cores < 1 {
		return fmt.Errorf("cores must be positive")
	}
	if config.PacketProcessingTime < 0 {
		return fmt.Errorf("packet processing time must not be negative")
	}
	if config.QueueCapacity < 0 {
		return fmt.Errorf("queue capacity must not be negative")
	}
	if duration < 0 {
		return fmt.Errorf("observation duration must not be negative")
	}

	// Zero cost bypasses cores and queues, so their time counters stay at zero.
	if config.PacketProcessingTime == 0 || duration == 0 {
		return nil
	}
	maximumParallelWork := max(config.Cores, config.QueueCapacity)
	if int64(maximumParallelWork) > math.MaxInt64/int64(duration) {
		return fmt.Errorf("cores or queue capacity are too large for this duration")
	}
	return nil
}

// Stats describes virtual host work, separately from link losses and real simulator CPU use.
// QueueWaitTotal counts waits that ended when service started; divide by StartedPackets.
type Stats struct {
	Enabled              bool
	Stopped              bool
	Cores                int
	PacketProcessingTime time.Duration
	BusyCores            int
	QueueCapacity        int
	QueuedPackets        int
	MaxQueuedPackets     int

	OfferedPackets      uint64
	AcceptedPackets     uint64
	StartedPackets      uint64
	ProcessedPackets    uint64
	QueueDroppedPackets uint64
	DiscardedPackets    uint64 // Accepted work abandoned by failure or stop, rather than queue overflow.

	ObservedTime   time.Duration
	BusyTime       time.Duration // Sum of time spent on all cores, including unfinished packets.
	QueueWaitTotal time.Duration
	MaxQueueWait   time.Duration // Includes the oldest packet still waiting in the FIFO.
}

// Utilization is the share of configured core-time used during ObservedTime.
// Two cores busy for 10 us each during a 20-us observation give 50% utilization.
func (stats Stats) Utilization() float64 {
	if !stats.Enabled || stats.Cores < 1 || stats.ObservedTime <= 0 {
		return 0
	}
	capacity := float64(stats.Cores) * float64(stats.ObservedTime)
	return min(1, max(0, float64(stats.BusyTime)/capacity))
}
