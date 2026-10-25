package main

import (
	"errors"
	"flag"
	"fmt"
	"load-balancer/engine"

	"load-balancer/scenario"
	"math"
	"time"
)

type configuration struct {
	clients     int
	servers     int
	packets     int
	streamBytes int

	clientGap        time.Duration
	packetGap        time.Duration
	duration         time.Duration
	snapshotInterval time.Duration
	packetTravelTime time.Duration

	bandwidth               int64
	forwardingBitsPerSecond int64
	queue                   int
	loss                    float64
	ingress                 scenario.LinkConfig

	speed     float64
	seed      int64
	algorithm string
	maxEvents uint64
	headless  bool
	tracePath string
}

func readConfiguration() (configuration, error) {
	var c configuration
	ingressDefaults := scenario.DefaultIngressConfig()
	flag.IntVar(&c.clients, "clients", 200, "number of simulated clients")
	flag.IntVar(&c.servers, "servers", 10, "number of backend servers (zero simulates no backends)")
	flag.IntVar(&c.packets, "packets", 10000, "full payload segments per client")
	flag.IntVar(&c.streamBytes, "bytes", -1, "payload bytes per client; -1 uses packets * segment size")
	flag.DurationVar(&c.clientGap, "client-gap", 0, "virtual interval between new clients")
	flag.DurationVar(&c.packetGap, "packet-gap", 0, "virtual sender pause before each data segment")
	flag.DurationVar(&c.duration, "duration", 2*time.Minute, "virtual experiment end time (inclusive)")
	flag.DurationVar(&c.snapshotInterval, "snapshot-interval", 100*time.Millisecond, "virtual snapshot and runner step interval")
	flag.Int64Var(&c.bandwidth, "bandwidth", 1_000_000_000, "bandwidth of each directed link in bits/s")
	flag.Int64Var(
		&c.forwardingBitsPerSecond,
		"lb-bandwidth",
		1_000_000_000,
		"shared load balancer forwarding budget in bits/s",
	)
	flag.DurationVar(&c.packetTravelTime, "packet-travel-time", time.Millisecond, "virtual packet travel time after transmission on each directed link")
	flag.IntVar(&c.queue, "link-queue", 1024, "waiting packets per link, excluding its active transmission")
	flag.Float64Var(&c.loss, "loss", 0, "independent packet loss probability on each link")
	flag.Int64Var(
		&c.ingress.BandwidthBitsPerSecond,
		"lb-ingress-bandwidth",
		ingressDefaults.BandwidthBitsPerSecond,
		"shared load balancer ingress bandwidth in bits/s",
	)
	flag.DurationVar(
		&c.ingress.PacketTravelTime,
		"lb-ingress-packet-travel-time",
		ingressDefaults.PacketTravelTime,
		"virtual packet travel time after shared ingress transmission",
	)
	flag.IntVar(
		&c.ingress.QueueCapacity,
		"lb-ingress-queue",
		ingressDefaults.QueueCapacity,
		"waiting packets per load balancer input queue (clients and backends)",
	)
	flag.Float64Var(
		&c.ingress.LossRate,
		"lb-ingress-loss",
		ingressDefaults.LossRate,
		"independent packet loss probability at shared ingress",
	)
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
	if c.clients < 0 || c.clients > scenario.MaxClients || c.servers < 0 {
		return c, errors.New("invalid client or server count")
	}
	if c.servers > scenario.MaxBackends {
		return c, fmt.Errorf("server count must not exceed %d", scenario.MaxBackends)
	}
	if c.speed < 0 || math.IsNaN(c.speed) || math.IsInf(c.speed, 0) {
		return c, errors.New("speed must be finite and nonnegative")
	}
	if c.headless {
		c.speed = 0
	}

	resolved, err := scenario.Normalize(c.scenarioDefinition())
	if err != nil {
		return c, err
	}
	c.streamBytes = resolved.DefaultStream.Bytes
	return c, nil
}
