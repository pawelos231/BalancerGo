package app

import (
	"fmt"
	"load-balancer/model"
	"load-balancer/scenario"
)

// scenarioDefinition maps CLI settings directly to a one-state scenario document.
// Shared ingress, endpoint links and forwarding keep their independent settings.
func (c configuration) scenarioDefinition() scenario.Scenario {
	mssBytes := c.effectiveMSSBytes()
	receiveBufferBytes := c.receiveBufferBytes
	if receiveBufferBytes == 0 {
		receiveBufferBytes = model.DefaultReceiveBufferBytes
	}
	rto := c.effectiveRTOConfig()
	closeConfig := c.effectiveCloseConfig()
	delayedACK := c.effectiveDelayedACKConfig()
	mode := string(rto.Mode)
	congestion := c.effectiveCongestionConfig()
	congestionMode := string(congestion.Mode)
	clientIdle, halfOpenIdle, establishedIdle := c.effectiveIdleTimeouts()
	closingIdle, resetIdle := c.effectiveLBFinalIdleTimeouts()
	backendHalfOpen, backendEstablished := c.effectiveBackendIdleTimeouts()

	backends := make([]scenario.BackendStateConfig, c.servers)
	for i := range backends {
		backends[i] = scenario.BackendStateConfig{
			ID:      fmt.Sprintf("srv-%02d", i),
			Name:    fmt.Sprintf("backend-%02d", i),
			Address: fmt.Sprintf("10.0.%d.%d", i/254, i%254+1),
			Port:    80,
			Host:    *hostInput(effectiveHostConfig(c.backendHost)),
		}
	}

	fixed := scenario.FixedConfig{
		TCP: scenario.TCPConfig{
			MSSBytes:           &mssBytes,
			ReceiveBufferBytes: &receiveBufferBytes,
			SACKEnabled:        &c.sackEnabled,
			ECNEnabled:         &c.ecnEnabled,
			DelayedACK: scenario.DelayedACKConfig{
				Enabled:        &delayedACK.Enabled,
				SegmentsPerACK: &delayedACK.SegmentsPerACK,
				MaxDelay:       &delayedACK.MaxDelay,
			},
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
				MaximumWindowSegments:          &congestion.MaximumWindowSegments,
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
		LoadBalancer: scenario.LoadBalancerIdentity{ID: "lb"},
		Workload: scenario.WorkloadDefaultsConfig{
			Defaults: scenario.StreamConfig{
				Packets:   c.packets,
				Bytes:     c.streamBytes,
				PacketGap: c.packetGap,
			},
		},
	}
	initial := scenario.StateConfig{
		Resources: scenario.StateResourcesConfig{
			LoadBalancer: scenario.LoadBalancerStateConfig{
				Algorithm:               c.algorithm,
				Host:                    *hostInput(effectiveHostConfig(c.lbHost)),
				Ingress:                 c.ingress,
				ForwardingBitsPerSecond: c.forwardingBitsPerSecond,
				MappingTimeouts: scenario.LoadBalancerTimeoutConfig{
					HalfOpenIdleTimeout:    &halfOpenIdle,
					EstablishedIdleTimeout: &establishedIdle,
					ClosingIdleTimeout:     &closingIdle,
					ResetIdleTimeout:       &resetIdle,
				},
			},
			Backends: backends,
		},
		Workload: scenario.StateWorkloadConfig{
			ClientGroups: []scenario.ClientGroup{{
				ID:        "clients",
				Count:     c.clients,
				ClientGap: c.clientGap,
			}},
		},
	}
	return scenario.Scenario{
		ScenarioVersion: scenario.CurrentScenarioVersion,
		Fixed:           fixed,
		States:          map[string]scenario.StateConfig{"0": initial},
	}
}
