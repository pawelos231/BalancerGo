package simulation

import "time"

// PayloadProgress counts unique payload bytes independently of connection state.
type PayloadProgress struct {
	ReceivedBytes     uint64
	AcknowledgedBytes uint64
}

// TrafficRate is a measurement over one completed virtual-time window.
type TrafficRate struct {
	TransmitBytesPerSecond float64
	ReceiveBytesPerSecond  float64
	SampleWindow           time.Duration
}

// trafficCounters keeps cumulative wire bytes, independent of snapshot delivery.
type trafficCounters struct {
	transmitted uint64
	received    uint64
}

// trafficSampler owns the last sampling boundary and its cached node rates.
// Reading topology never advances this window or changes the simulation.
type trafficSampler struct {
	initialized bool
	at          time.Duration
	counters    map[string]trafficCounters
	rates       map[string]TrafficRate
}

// sample closes one shared interval for all nodes, including newly created clients.
func (sampler *trafficSampler) sample(at time.Duration, counters map[string]trafficCounters) {
	if sampler.initialized && at <= sampler.at {
		return
	}
	if !sampler.initialized {
		sampler.initialized = true
		sampler.at = at
		sampler.counters = counters
		return
	}

	seconds := (at - sampler.at).Seconds()
	rates := make(map[string]TrafficRate, len(counters))
	for id, current := range counters {
		previous := sampler.counters[id]
		// A counter reset has no valid delta; do not display a fabricated rate.
		if current.transmitted < previous.transmitted || current.received < previous.received {
			continue
		}
		rates[id] = TrafficRate{
			TransmitBytesPerSecond: float64(current.transmitted-previous.transmitted) / seconds,
			ReceiveBytesPerSecond:  float64(current.received-previous.received) / seconds,
			SampleWindow:           at - sampler.at,
		}
	}
	sampler.at = at
	sampler.counters = counters
	sampler.rates = rates
}

// SampleTraffic runs after the whole virtual-time boundary has been processed.
// Incoming access hops stop before LB reception, so RX comes from shared ingress.
func (s *World) SampleTraffic() {
	counters := map[string]trafficCounters{LoadBalancerNodeID: {}}
	for _, link := range s.links {
		stats := link.Stats()
		source := counters[link.Source]
		source.transmitted += stats.TransmittedBytes
		counters[link.Source] = source

		if link.Target != LoadBalancerNodeID {
			target := counters[link.Target]
			target.received += stats.DeliveredBytes
			counters[link.Target] = target
		}
	}
	balancer := counters[LoadBalancerNodeID]
	balancer.received = s.ingress.Stats().DeliveredBytes
	counters[LoadBalancerNodeID] = balancer
	s.traffic.sample(s.engine.Now(), counters)
}

// NodeTraffic returns the last completed sampling window as a detached value.
func (s *World) NodeTraffic(id string) (TrafficRate, bool) {
	rate, known := s.traffic.rates[id]
	return rate, known
}
