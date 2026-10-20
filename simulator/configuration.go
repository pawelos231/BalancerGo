package main

import (
	"errors"
	"flag"
	"fmt"
	"load-balancer/engine"
	"load-balancer/model"
	"math"
	"time"
)

type configuration struct {
	clients, servers, packets, streamBytes                    int
	clientGap, packetGap, duration, snapshotInterval, latency time.Duration
	bandwidth                                                 int64
	queue                                                     int
	loss, speed                                               float64
	seed                                                      int64
	algorithm                                                 string
	maxEvents                                                 uint64
	headless                                                  bool
	tracePath                                                 string
}

func readConfiguration() (configuration, error) {
	var c configuration
	flag.IntVar(&c.clients, "clients", 200, "number of simulated clients")
	flag.IntVar(&c.servers, "servers", 10, "number of backend servers (zero simulates no backends)")
	flag.IntVar(&c.packets, "packets", 10000, "full payload segments per client")
	flag.IntVar(&c.streamBytes, "bytes", -1, "payload bytes per client; -1 uses packets * segment size")
	flag.DurationVar(&c.clientGap, "client-gap", 0, "virtual interval between new clients")
	flag.DurationVar(&c.packetGap, "packet-gap", 0, "virtual sender pause before each data segment")
	flag.DurationVar(&c.duration, "duration", 2*time.Minute, "virtual experiment end time (inclusive)")
	flag.DurationVar(&c.snapshotInterval, "snapshot-interval", 100*time.Millisecond, "virtual snapshot and runner step interval")
	flag.Int64Var(&c.bandwidth, "bandwidth", 1_000_000_000, "bandwidth of each directed link in bits/s")
	flag.DurationVar(&c.latency, "link-delay", time.Millisecond, "propagation delay of each directed link")
	flag.IntVar(&c.queue, "link-queue", 1024, "waiting packets per link, excluding its active transmission")
	flag.Float64Var(&c.loss, "loss", 0, "independent packet loss probability on each link")
	flag.Int64Var(&c.seed, "seed", 1, "reproducible experiment seed")
	flag.StringVar(&c.algorithm, "algorithm", "roundRobin", "roundRobin or random")
	flag.Float64Var(&c.speed, "speed", 1, "virtual seconds per real second; 0 runs without pacing")
	flag.Uint64Var(&c.maxEvents, "max-events", engine.DefaultMaxEvents, "maximum callbacks executed in the experiment")
	flag.BoolVar(&c.headless, "headless", false, "run without HTTP or pacing, print JSON result and exit")
	flag.StringVar(&c.tracePath, "trace", "", "optional Go runtime trace output path")
	flag.Parse()
	if flag.NArg() != 0 {
		return c, fmt.Errorf("unexpected arguments: %v", flag.Args())
	}
	// Keep the raw -1 byte sentinel for frontend defaults. Normalization must
	// happen after the user has had a chance to change the packet count.
	_, err := c.normalized()
	return c, err
}

// normalized validates a value copy and resolves options used by the model.
// The caller's CLI/default configuration remains unchanged.
func (c configuration) normalized() (configuration, error) {
	if c.clients < 0 || c.clients >= 1<<24 || c.servers < 0 || c.packets < 0 || c.streamBytes < -1 {
		return c, errors.New("invalid client, server or stream size")
	}
	if c.servers > 10000 {
		return c, errors.New("server count must not exceed 10000")
	}
	if c.clientGap < 0 || c.packetGap < 0 || c.duration < 0 || c.latency < 0 || c.snapshotInterval <= 0 {
		return c, errors.New("durations must be nonnegative and snapshot interval positive")
	}
	if c.bandwidth <= 0 || c.queue < 0 || !(c.loss >= 0 && c.loss <= 1) {
		return c, errors.New("bandwidth must be positive, queue nonnegative and loss in 0..1")
	}
	if c.algorithm != "roundRobin" && c.algorithm != "random" {
		return c, errors.New("unknown algorithm")
	}
	if c.speed < 0 || math.IsNaN(c.speed) || math.IsInf(c.speed, 0) || c.maxEvents == 0 {
		return c, errors.New("speed must be finite and nonnegative; max-events must be positive")
	}
	if c.headless {
		c.speed = 0
	}
	if c.speed > 0 && float64(c.duration)/c.speed >= float64(math.MaxInt64) {
		return c, errors.New("paced experiment duration exceeds time.Duration range")
	}
	if c.streamBytes == -1 {
		if c.packets > math.MaxInt/model.SEGMENT_SIZE {
			return c, errors.New("stream size overflows int")
		}
		c.streamBytes = c.packets * model.SEGMENT_SIZE
	}
	// PlaceInStream remains a uint32 segment counter in this migration.
	if uint64(c.streamBytes/model.SEGMENT_SIZE) >= math.MaxUint32 || c.streamBytes > math.MaxInt-40 {
		return c, errors.New("stream exceeds the simplified transport's range")
	}
	return c, nil
}

// simulationConfig is the frontend-visible experiment configuration. Process
// options such as tracing and headless mode are deliberately absent.
type simulationConfig struct {
	Clients                int     `json:"clients"`
	Servers                int     `json:"servers"`
	Packets                int     `json:"packets"`
	Bytes                  int     `json:"bytes"`
	DurationSeconds        float64 `json:"durationSeconds"`
	ClientGapMs            float64 `json:"clientGapMs"`
	PacketGapMs            float64 `json:"packetGapMs"`
	SnapshotIntervalMs     float64 `json:"snapshotIntervalMs"`
	BandwidthBitsPerSecond int64   `json:"bandwidthBitsPerSecond"`
	LinkDelayMs            float64 `json:"linkDelayMs"`
	LinkQueue              int     `json:"linkQueue"`
	Loss                   float64 `json:"loss"`
	Seed                   int64   `json:"seed"`
	Algorithm              string  `json:"algorithm"`
	Speed                  float64 `json:"speed"`
	MaxEvents              uint64  `json:"maxEvents"`
}

func (c configuration) publicConfig() simulationConfig {
	return simulationConfig{
		Clients: c.clients, Servers: c.servers, Packets: c.packets, Bytes: c.streamBytes,
		DurationSeconds:        float64(c.duration) / float64(time.Second),
		ClientGapMs:            float64(c.clientGap) / float64(time.Millisecond),
		PacketGapMs:            float64(c.packetGap) / float64(time.Millisecond),
		SnapshotIntervalMs:     float64(c.snapshotInterval) / float64(time.Millisecond),
		BandwidthBitsPerSecond: c.bandwidth,
		LinkDelayMs:            float64(c.latency) / float64(time.Millisecond),
		LinkQueue:              c.queue, Loss: c.loss, Seed: c.seed, Algorithm: c.algorithm,
		Speed: c.speed, MaxEvents: c.maxEvents,
	}
}

// configuration overlays the experiment fields while retaining base's process
// options. Callers can decode omitted JSON fields into base.publicConfig().
func (c simulationConfig) configuration(base configuration) (configuration, error) {
	base.clients, base.servers = c.Clients, c.Servers
	base.packets, base.streamBytes = c.Packets, c.Bytes
	base.bandwidth, base.queue = c.BandwidthBitsPerSecond, c.LinkQueue
	base.loss, base.seed = c.Loss, c.Seed
	base.algorithm, base.speed, base.maxEvents = c.Algorithm, c.Speed, c.MaxEvents
	for _, field := range []struct {
		name  string
		value float64
		unit  time.Duration
		out   *time.Duration
	}{
		{"durationSeconds", c.DurationSeconds, time.Second, &base.duration},
		{"clientGapMs", c.ClientGapMs, time.Millisecond, &base.clientGap},
		{"packetGapMs", c.PacketGapMs, time.Millisecond, &base.packetGap},
		{"snapshotIntervalMs", c.SnapshotIntervalMs, time.Millisecond, &base.snapshotInterval},
		{"linkDelayMs", c.LinkDelayMs, time.Millisecond, &base.latency},
	} {
		duration, err := configDuration(field.name, field.value, field.unit)
		if err != nil {
			return configuration{}, err
		}
		*field.out = duration
	}
	return base.normalized()
}

// Convert only after checking the floating-point input and scaled value.
// float64(MaxInt64) rounds to 2^63, so equality is already outside the safe
// conversion range. Positive fractions of a nanosecond truncate to zero.
func configDuration(name string, value float64, unit time.Duration) (time.Duration, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, fmt.Errorf("%s must be finite and nonnegative", name)
	}
	nanoseconds := value * float64(unit)
	if math.IsInf(nanoseconds, 0) || nanoseconds >= float64(math.MaxInt64) {
		return 0, fmt.Errorf("%s exceeds time.Duration range", name)
	}
	return time.Duration(nanoseconds), nil
}
