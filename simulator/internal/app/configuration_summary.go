package app

import (
	"load-balancer/internal/contract"
	"load-balancer/model"
	"time"
)

func (c configuration) publicConfig() contract.SimulationConfig {
	receiveBufferBytes := c.receiveBufferBytes
	if receiveBufferBytes == 0 {
		receiveBufferBytes = model.DefaultReceiveBufferBytes
	}
	rto := c.effectiveRTOConfig()
	closeConfig := c.effectiveCloseConfig()
	delayedACK := c.effectiveDelayedACKConfig()
	congestion := c.effectiveCongestionConfig()
	clientIdle, halfOpenIdle, establishedIdle := c.effectiveIdleTimeouts()
	closingIdle, resetIdle := c.effectiveLBFinalIdleTimeouts()
	backendHalfOpen, backendEstablished := c.effectiveBackendIdleTimeouts()
	lbHost := effectiveHostConfig(c.lbHost)
	backendHost := effectiveHostConfig(c.backendHost)

	return contract.SimulationConfig{
		Clients:                         c.clients,
		Servers:                         c.servers,
		Packets:                         c.packets,
		Bytes:                           c.streamBytes,
		MSSBytes:                        c.effectiveMSSBytes(),
		ReceiveBufferBytes:              receiveBufferBytes,
		SACKEnabled:                     c.sackEnabled,
		ECNEnabled:                      c.ecnEnabled,
		DelayedACKEnabled:               delayedACK.Enabled,
		DelayedACKSegments:              delayedACK.SegmentsPerACK,
		DelayedACKMaxDelayMs:            float64(delayedACK.MaxDelay) / float64(time.Millisecond),
		CongestionControl:               string(congestion.Mode),
		FastRetransmit:                  congestion.FastRetransmit,
		CubicFastConvergence:            congestion.CubicFastConvergence,
		InitialCongestionWindowSegments: congestion.InitialWindowSegments,
		MaximumCongestionWindowSegments: congestion.MaximumWindowSegments,
		InitialSlowStartThresholdBytes:  congestion.InitialSlowStartThresholdBytes,
		RTOMode:                         string(rto.Mode),
		RTOBackoff:                      rto.ExponentialBackoff,
		RTOInitialMs:                    float64(rto.InitialTimeout) / float64(time.Millisecond),
		RTOMinimumMs:                    float64(rto.MinimumTimeout) / float64(time.Millisecond),
		RTOMaximumMs:                    float64(rto.MaximumTimeout) / float64(time.Millisecond),
		TCPMSLMs:                        float64(closeConfig.MSL) / float64(time.Millisecond),
		TCPFinWaitTimeoutMs:             float64(closeConfig.FinWaitTimeout) / float64(time.Millisecond),
		TCPMaxFINRetries:                closeConfig.MaxFINRetries,
		ClientIdleTimeoutMs:             float64(clientIdle) / float64(time.Millisecond),
		BackendHalfOpenIdleTimeoutMs:    float64(backendHalfOpen) / float64(time.Millisecond),
		BackendEstablishedIdleTimeoutMs: float64(backendEstablished) / float64(time.Millisecond),
		LBHalfOpenIdleTimeoutMs:         float64(halfOpenIdle) / float64(time.Millisecond),
		LBEstablishedIdleTimeoutMs:      float64(establishedIdle) / float64(time.Millisecond),
		LBClosingIdleTimeoutMs:          float64(closingIdle) / float64(time.Millisecond),
		LBResetIdleTimeoutMs:            float64(resetIdle) / float64(time.Millisecond),
		LBHostCores:                     lbHost.Cores,
		LBPacketProcessingTimeUs:        float64(lbHost.PacketProcessingTime) / float64(time.Microsecond),
		LBCPUQueueCapacity:              lbHost.QueueCapacity,
		BackendHostCores:                backendHost.Cores,
		BackendPacketProcessingTimeUs:   float64(backendHost.PacketProcessingTime) / float64(time.Microsecond),
		BackendCPUQueueCapacity:         backendHost.QueueCapacity,
		DurationSeconds:                 float64(c.duration) / float64(time.Second),
		ClientGapMs:                     float64(c.clientGap) / float64(time.Millisecond),
		PacketGapMs:                     float64(c.packetGap) / float64(time.Millisecond),
		SnapshotIntervalMs:              float64(c.snapshotInterval) / float64(time.Millisecond),
		BandwidthBitsPerSecond:          c.bandwidth,
		ForwardingBitsPerSecond:         c.forwardingBitsPerSecond,
		PacketTravelTimeMs:              float64(c.packetTravelTime) / float64(time.Millisecond),
		LinkQueue:                       c.queue,
		Loss:                            c.loss,
		QueuePolicy:                     c.queuePolicy,
		CongestionThresholdPercent:      c.congestionThresholdPercent,
		CongestionSignalProbability:     c.congestionSignalProbability,

		IngressBandwidthBitsPerSecond:      c.ingress.BandwidthBitsPerSecond,
		IngressPacketTravelTimeMs:          float64(c.ingress.PacketTravelTime) / float64(time.Millisecond),
		IngressQueueCapacity:               c.ingress.QueueCapacity,
		IngressLoss:                        c.ingress.LossRate,
		IngressQueuePolicy:                 c.ingress.QueuePolicy,
		IngressCongestionThresholdPercent:  c.ingress.CongestionThresholdPercent,
		IngressCongestionSignalProbability: c.ingress.CongestionSignalProbability,

		Seed:      c.seed,
		Algorithm: c.algorithm,
		Speed:     c.speed,
		MaxEvents: c.maxEvents,
	}
}
