package model

import "time"

// ChaosConfig holds settings for chaos engineering hooks.
type ChaosConfig struct {
	// Latency adds a fixed delay to packet processing.
	Latency time.Duration
	// PacketDropRate is the probability (0.0 to 1.0) of dropping a packet.
	PacketDropRate float64
} 