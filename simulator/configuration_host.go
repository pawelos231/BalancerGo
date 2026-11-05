package main

import (
	"flag"

	"load-balancer/scenario"
)

// hostInput keeps explicit zero cost and queue capacity in the native document.
func hostInput(config scenario.NormalizedHostConfig) *scenario.HostConfig {
	return &scenario.HostConfig{
		Cores:                &config.Cores,
		PacketProcessingTime: &config.PacketProcessingTime,
		QueueCapacity:        &config.QueueCapacity,
	}
}

// effectiveHostConfig supplies defaults for older callers that omit host settings.
func effectiveHostConfig(input *scenario.HostConfig) scenario.NormalizedHostConfig {
	resolved := scenario.DefaultHostConfig()
	if input == nil {
		return resolved
	}
	if input.Cores != nil {
		resolved.Cores = *input.Cores
	}
	if input.PacketProcessingTime != nil {
		resolved.PacketProcessingTime = *input.PacketProcessingTime
	}
	if input.QueueCapacity != nil {
		resolved.QueueCapacity = *input.QueueCapacity
	}
	return resolved
}

// registerHostFlags exposes separate CPU limits for the LB and all generated backends.
func (c *configuration) registerHostFlags() {
	c.lbHost = hostInput(scenario.DefaultHostConfig())
	c.backendHost = hostInput(scenario.DefaultHostConfig())
	for _, entry := range []struct {
		prefix string
		config *scenario.HostConfig
	}{
		{prefix: "lb", config: c.lbHost},
		{prefix: "backend", config: c.backendHost},
	} {
		flag.IntVar(entry.config.Cores, entry.prefix+"-cpu-cores", *entry.config.Cores,
			"virtual cores available for packet processing (1..1024)")
		flag.DurationVar(entry.config.PacketProcessingTime,
			entry.prefix+"-packet-processing-time", *entry.config.PacketProcessingTime,
			"virtual CPU time per packet; zero disables the CPU limit")
		flag.IntVar(entry.config.QueueCapacity, entry.prefix+"-cpu-queue", *entry.config.QueueCapacity,
			"waiting packets in the CPU queue, excluding packets on active cores")
	}
}
