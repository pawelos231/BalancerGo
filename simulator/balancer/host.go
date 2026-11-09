package balancer

import "load-balancer/host"

// SetHostConfig gives both directions one shared CPU pool before packets arrive.
func (lb *LoadBalancer) SetHostConfig(config host.Config) {
	if lb.closed || lb.processor.Stats().OfferedPackets != 0 {
		panic("balancer: host configuration must be set before traffic starts")
	}
	processor := host.New(lb.engine, config)
	lb.processor.Stop()
	lb.processor = processor
}

func (lb *LoadBalancer) HostStats() host.Stats { return lb.processor.Stats() }

// PrepareHostConfig edits CPU limits while preserving packet work and counters.
func (lb *LoadBalancer) PrepareHostConfig(config host.Config) host.PreparedConfig {
	return lb.processor.PrepareConfig(config)
}
