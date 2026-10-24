package main

import (
	"time"

	"load-balancer/model"
)

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
	rates       map[string]model.TopologyTraffic
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
	rates := make(map[string]model.TopologyTraffic, len(counters))
	for id, current := range counters {
		previous := sampler.counters[id]
		// A counter reset has no valid delta; do not display a fabricated rate.
		if current.transmitted < previous.transmitted || current.received < previous.received {
			continue
		}
		rates[id] = model.TopologyTraffic{
			TransmitBytesPerSecond: float64(current.transmitted-previous.transmitted) / seconds,
			ReceiveBytesPerSecond:  float64(current.received-previous.received) / seconds,
			SampleWindowSeconds:    seconds,
		}
	}
	sampler.at = at
	sampler.counters = counters
	sampler.rates = rates
}

// sampleTraffic runs after the whole virtual-time boundary has been processed.
// Incoming access hops stop before LB reception, so RX comes from shared ingress.
func (s *simulation) sampleTraffic() {
	counters := map[string]trafficCounters{loadBalancerNodeID: {}}
	for _, link := range s.links {
		stats := link.Stats()
		source := counters[link.Source]
		source.transmitted += stats.TransmittedBytes
		counters[link.Source] = source

		if link.Target != loadBalancerNodeID {
			target := counters[link.Target]
			target.received += stats.DeliveredBytes
			counters[link.Target] = target
		}
	}
	balancer := counters[loadBalancerNodeID]
	balancer.received = s.ingress.Stats().DeliveredBytes
	counters[loadBalancerNodeID] = balancer
	s.traffic.sample(s.engine.Now(), counters)
}

// nodeTraffic returns a detached copy; no measurement exists before positive time passes.
func (s *simulation) nodeTraffic(id string) *model.TopologyTraffic {
	rate, known := s.traffic.rates[id]
	if !known {
		return nil
	}
	return &rate
}
