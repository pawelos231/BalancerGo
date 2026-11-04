package network

import (
	"math"
	"math/rand"
)

// ECNState describes only the capability and CE state needed at a queue.
type ECNState uint8

const (
	NotCapable ECNState = iota
	Capable
	AlreadyMarked
)

func validateECNState(state ECNState) {
	if state > AlreadyMarked {
		panic("network: invalid ECN state")
	}
}

func validateCongestionConfig(config Config) {
	if config.QueuePolicy != "" && config.QueuePolicy != "tailDrop" && config.QueuePolicy != "threshold" {
		panic("network: queue policy must be tailDrop or threshold")
	}
	threshold := config.CongestionThresholdPercent
	if math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold < 0 || threshold > 100 {
		panic("network: congestion threshold must be in 0..100")
	}
	probability := config.CongestionSignalProbability
	if math.IsNaN(probability) || math.IsInf(probability, 0) || probability < 0 || probability > 1 {
		panic("network: congestion signal probability must be in 0..1")
	}
}

// SetCongestionRNG assigns an independent random stream before the first packet.
// Only threshold signaling with a probability strictly between zero and one needs it.
func (l *Link) SetCongestionRNG(rng *rand.Rand) {
	if l.stats.OfferedPackets != 0 || l.transmitting || l.queued() != 0 || l.travelling != 0 {
		panic("network: congestion random source requires a fresh link")
	}
	if l.config.QueuePolicy == "threshold" && l.config.CongestionSignalProbability > 0 && l.config.CongestionSignalProbability < 1 && rng == nil {
		panic("network: probabilistic congestion signaling requires a random source")
	}
	l.congestionRNG = rng
}

// shouldSignalCongestion checks whether this waiting arrival should be marked or dropped early.
// Waiting occupancy includes this packet; callers check full queues and immediate starts first.
func (l *Link) shouldSignalCongestion(waiting int) bool {
	config := l.config
	if config.QueuePolicy != "threshold" || config.QueueCapacity == 0 || config.CongestionSignalProbability == 0 {
		return false
	}
	threshold := math.Ceil(float64(config.QueueCapacity) * config.CongestionThresholdPercent / 100)
	if float64(waiting) < threshold {
		return false
	}
	if config.CongestionSignalProbability == 1 {
		return true
	}
	if l.congestionRNG == nil {
		panic("network: probabilistic congestion signaling requires a random source")
	}
	return l.congestionRNG.Float64() < config.CongestionSignalProbability
}
