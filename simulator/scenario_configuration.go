package main

import (
	"fmt"
	"load-balancer/scenario"
)

// scenarioDefinition maps CLI and browser settings to the scenario model.
// Shared ingress, endpoint links and forwarding keep their independent settings.
func (c configuration) scenarioDefinition() scenario.Scenario {
	backends := make([]scenario.BackendConfig, c.servers)
	for i := range backends {
		backends[i] = scenario.BackendConfig{
			ID:      fmt.Sprintf("srv-%02d", i),
			Name:    fmt.Sprintf("backend-%02d", i),
			Address: fmt.Sprintf("10.0.%d.%d", i/254, i%254+1),
			Port:    80,
		}
	}

	return scenario.Scenario{
		Version: scenario.Version,
		Execution: scenario.ExecutionConfig{
			Duration:         c.duration,
			SnapshotInterval: c.snapshotInterval,
			Seed:             c.seed,
			Speed:            c.speed,
			MaxEvents:        c.maxEvents,
		},
		Network: scenario.NetworkConfig{
			Defaults: scenario.LinkConfig{
				BandwidthBitsPerSecond: c.bandwidth,
				PacketTravelTime:       c.packetTravelTime,
				QueueCapacity:          c.queue,
				LossRate:               c.loss,
			},
		},
		Resources: scenario.ResourcesConfig{
			LoadBalancer: scenario.LoadBalancerConfig{
				ID:                      "lb",
				Algorithm:               c.algorithm,
				Ingress:                 c.ingress,
				ForwardingBitsPerSecond: c.forwardingBitsPerSecond,
				QueueCapacity:           c.queue,
			},
			Backends: backends,
		},
		Workload: scenario.WorkloadConfig{
			Defaults: scenario.StreamConfig{
				Packets:   c.packets,
				Bytes:     c.streamBytes,
				PacketGap: c.packetGap,
			},
			ClientGroups: []scenario.ClientGroup{{
				ID:        "clients",
				Count:     c.clients,
				ClientGap: c.clientGap,
			}},
		},
	}
}
