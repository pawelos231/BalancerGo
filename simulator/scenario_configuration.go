package main

import (
	"fmt"
	"load-balancer/model"
	"load-balancer/scenario"
)

// scenarioDefinition maps CLI and browser settings to the scenario model.
// Shared ingress, endpoint links and forwarding keep their independent settings.
func (c configuration) scenarioDefinition() scenario.Scenario {
	receiveBufferBytes := c.receiveBufferBytes
	if receiveBufferBytes == 0 {
		receiveBufferBytes = model.DefaultReceiveBufferBytes
	}
	rto := c.effectiveRTOConfig()
	closeConfig := c.effectiveCloseConfig()
	mode := string(rto.Mode)
	congestion := c.effectiveCongestionConfig()
	congestionMode := string(congestion.Mode)
	clientIdle, halfOpenIdle, establishedIdle := c.effectiveIdleTimeouts()
	closingIdle, resetIdle := c.effectiveLBFinalIdleTimeouts()
	backendHalfOpen, backendEstablished := c.effectiveBackendIdleTimeouts()

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
		TCP: scenario.TCPConfig{
			ReceiveBufferBytes: &receiveBufferBytes,
			SACKEnabled:        &c.sackEnabled,
			ECNEnabled:         &c.ecnEnabled,
			Close: scenario.CloseConfig{
				MSL: &closeConfig.MSL, FinWaitTimeout: &closeConfig.FinWaitTimeout,
				MaxFINRetries: &closeConfig.MaxFINRetries,
			},
			RTO: scenario.RTOConfig{
				Mode:               &mode,
				ExponentialBackoff: &rto.ExponentialBackoff,
				InitialTimeout:     &rto.InitialTimeout,
				MinimumTimeout:     &rto.MinimumTimeout,
				MaximumTimeout:     &rto.MaximumTimeout,
			},
			ClientIdleTimeout:             &clientIdle,
			BackendHalfOpenIdleTimeout:    &backendHalfOpen,
			BackendEstablishedIdleTimeout: &backendEstablished,
			CongestionControl: scenario.CongestionConfig{
				Mode:                           &congestionMode,
				FastRetransmit:                 &congestion.FastRetransmit,
				CubicFastConvergence:           &congestion.CubicFastConvergence,
				InitialWindowSegments:          &congestion.InitialWindowSegments,
				InitialSlowStartThresholdBytes: &congestion.InitialSlowStartThresholdBytes,
			},
		},
		Execution: scenario.ExecutionConfig{
			Duration:         c.duration,
			SnapshotInterval: c.snapshotInterval,
			Seed:             c.seed,
			Speed:            c.speed,
			MaxEvents:        c.maxEvents,
		},
		Network: scenario.NetworkConfig{
			Defaults: scenario.LinkConfig{
				BandwidthBitsPerSecond:      c.bandwidth,
				PacketTravelTime:            c.packetTravelTime,
				QueueCapacity:               c.queue,
				LossRate:                    c.loss,
				QueuePolicy:                 c.queuePolicy,
				CongestionThresholdPercent:  c.congestionThresholdPercent,
				CongestionSignalProbability: c.congestionSignalProbability,
			},
		},
		Resources: scenario.ResourcesConfig{
			LoadBalancer: scenario.LoadBalancerConfig{
				ID:                      "lb",
				Algorithm:               c.algorithm,
				Ingress:                 c.ingress,
				ForwardingBitsPerSecond: c.forwardingBitsPerSecond,
				HalfOpenIdleTimeout:     &halfOpenIdle,
				EstablishedIdleTimeout:  &establishedIdle,
				ClosingIdleTimeout:      &closingIdle,
				ResetIdleTimeout:        &resetIdle,
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
