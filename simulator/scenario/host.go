package scenario

import (
	"fmt"
	"time"

	"load-balancer/host"
)

const (
	MaxHostCores         = 1024
	MaxHostQueueCapacity = 1_000_000
)

// DefaultHostConfig retains instant packet handling until CPU cost is enabled.
func DefaultHostConfig() NormalizedHostConfig {
	defaults := host.DefaultConfig()
	return NormalizedHostConfig{
		Cores:                defaults.Cores,
		PacketProcessingTime: defaults.PacketProcessingTime,
		QueueCapacity:        defaults.QueueCapacity,
	}
}

// normalizeHost keeps an explicit zero queue or disabled CPU cost.
func normalizeHost(config HostConfig, duration time.Duration, path string) (NormalizedHostConfig, error) {
	resolved := DefaultHostConfig()
	if config.Cores != nil {
		resolved.Cores = *config.Cores
	}
	if config.PacketProcessingTime != nil {
		resolved.PacketProcessingTime = *config.PacketProcessingTime
	}
	if config.QueueCapacity != nil {
		resolved.QueueCapacity = *config.QueueCapacity
	}

	if resolved.Cores < 1 || resolved.Cores > MaxHostCores {
		return NormalizedHostConfig{}, fmt.Errorf("%s.cores: must be in 1..%d", path, MaxHostCores)
	}
	if resolved.PacketProcessingTime < 0 {
		return NormalizedHostConfig{}, fmt.Errorf("%s.packetProcessingTimeNanoseconds: must be nonnegative", path)
	}
	if resolved.QueueCapacity < 0 || resolved.QueueCapacity > MaxHostQueueCapacity {
		return NormalizedHostConfig{}, fmt.Errorf("%s.queueCapacity: must be in 0..%d", path, MaxHostQueueCapacity)
	}
	resource := host.Config{
		Cores:                resolved.Cores,
		PacketProcessingTime: resolved.PacketProcessingTime,
		QueueCapacity:        resolved.QueueCapacity,
	}
	if err := resource.Validate(duration); err != nil {
		return NormalizedHostConfig{}, fmt.Errorf("%s: %w", path, err)
	}
	return resolved, nil
}
