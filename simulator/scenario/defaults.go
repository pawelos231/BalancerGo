package scenario

// DefaultIngressConfig provides the shared LB input's initial link settings.
// They are independent from endpoint links and the output forwarding limit.
func DefaultIngressConfig() LinkConfig {
	return LinkConfig{
		BandwidthBitsPerSecond: 1_000_000_000,
		PacketTravelTime:       0,
		QueueCapacity:          1024,
		LossRate:               0,
		// These values take effect only after choosing the threshold queue policy.
		CongestionThresholdPercent:  80,
		CongestionSignalProbability: 1,
	}
}
