package main

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
	"load-balancer/scenario"
)

type configuration struct {
	clients                  int
	servers                  int
	packets                  int
	streamBytes              int
	receiveBufferBytes       int
	rto                      model.RTOConfig
	congestion               model.CongestionConfig
	clientIdleTimeout        *time.Duration
	lbHalfOpenIdleTimeout    *time.Duration
	lbEstablishedIdleTimeout *time.Duration

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

	headless        bool
	tracePath       string
	scenarioPath    string
	outputPath      string
	seeds           string
	outputDirectory string
}

func readConfiguration() (configuration, error) {
	var c configuration
	ingressDefaults := scenario.DefaultIngressConfig()
	c.registerTCPFlags()
	flag.IntVar(&c.clients, "clients", 200, "number of simulated clients")
	flag.IntVar(&c.servers, "servers", 10, "number of backend servers (zero simulates no backends)")
	flag.IntVar(&c.packets, "packets", 10000, "1024-byte payload units per client when -bytes is -1")
	flag.IntVar(&c.streamBytes, "bytes", -1, "payload bytes per client; -1 uses packets * segment size")
	flag.IntVar(
		&c.receiveBufferBytes,
		"tcp-receive-buffer",
		model.DefaultReceiveBufferBytes,
		"TCP receive-buffer capacity per connection in bytes (1..65535)",
	)
	flag.DurationVar(&c.clientGap, "client-gap", 0, "virtual interval between new clients")
	flag.DurationVar(&c.packetGap, "packet-gap", 0, "virtual sender pause before each data segment")
	flag.DurationVar(&c.duration, "duration", 2*time.Minute, "virtual experiment end time (inclusive)")
	flag.DurationVar(
		&c.snapshotInterval,
		"snapshot-interval",
		100*time.Millisecond,
		"virtual snapshot and runner step interval",
	)
	flag.Int64Var(&c.bandwidth, "bandwidth", 1_000_000_000, "bandwidth of each directed link in bits/s")
	flag.Int64Var(
		&c.forwardingBitsPerSecond,
		"lb-bandwidth",
		1_000_000_000,
		"shared load balancer forwarding budget in bits/s",
	)
	flag.DurationVar(
		&c.packetTravelTime,
		"packet-travel-time",
		time.Millisecond,
		"virtual packet travel time after transmission on each directed link",
	)
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
	flag.Uint64Var(
		&c.maxEvents,
		"max-events",
		engine.DefaultMaxEvents,
		"maximum callbacks executed in the experiment",
	)
	flag.BoolVar(&c.headless, "headless", false, "run without HTTP or pacing, print JSON result and exit")
	flag.StringVar(&c.tracePath, "trace", "", "optional Go runtime trace output path")
	flag.StringVar(
		&c.scenarioPath,
		"scenario",
		"",
		"run a full scenario JSON file without HTTP or pacing",
	)
	flag.StringVar(
		&c.outputPath,
		"output",
		"",
		"write the scenario run report to a new file instead of stdout",
	)
	flag.StringVar(
		&c.seeds,
		"seeds",
		"",
		"run a scenario series with comma-separated signed decimal seeds",
	)
	flag.StringVar(
		&c.outputDirectory,
		"output-dir",
		"",
		"write series reports and index.json in a new directory",
	)
	if err := flag.CommandLine.Parse(os.Args[1:]); err != nil {
		return c, err
	}
	if flag.NArg() != 0 {
		return c, fmt.Errorf("unexpected arguments: %v", flag.Args())
	}
	if err := c.checkFileOptions(); err != nil {
		return c, err
	}

	if c.scenarioPath != "" {
		c.headless = true
		return c, nil
	}
	if c.receiveBufferBytes == 0 {
		return c, errors.New("tcp.receiveBufferBytes: must be in 1..65535")
	}
	// Keep the raw -1 byte sentinel for frontend defaults. Normalization must
	// happen after the user has had a chance to change the packet count.
	_, err := c.normalized()
	return c, err
}

// checkFileOptions checks explicit paths and keeps JSON as the source of settings.
// An empty scenario argument must fail rather than start the interactive server.
func (c configuration) checkFileOptions() error {
	series := c.seeds != "" || c.outputDirectory != ""
	var invalidOption error
	flag.Visit(func(option *flag.Flag) {
		if invalidOption != nil {
			return
		}
		switch option.Name {
		case "scenario", "output", "output-dir":
			if option.Value.String() == "" {
				invalidOption = fmt.Errorf("-%s requires a nonempty path", option.Name)
			}
			if series && option.Name == "output" {
				invalidOption = errors.New("-output cannot be combined with -seeds or -output-dir")
			}
			return
		case "seeds":
			if option.Value.String() == "" {
				invalidOption = errors.New("-seeds requires a nonempty seed list")
			}
			return
		case "trace":
			if series {
				invalidOption = errors.New("-trace cannot be combined with -seeds or -output-dir")
			}
			return
		case "headless":
			return
		}
		if c.scenarioPath != "" {
			invalidOption = fmt.Errorf("-%s cannot be combined with -scenario", option.Name)
		}
	})
	if invalidOption != nil {
		return invalidOption
	}
	if series {
		if c.scenarioPath == "" {
			return errors.New("-seeds and -output-dir require -scenario")
		}
		if c.seeds == "" || c.outputDirectory == "" {
			return errors.New("-seeds and -output-dir must be provided together")
		}
		_, err := parseSeriesSeeds(c.seeds)
		return err
	}
	if c.outputPath != "" && c.scenarioPath == "" {
		return errors.New("-output requires -scenario")
	}
	return nil
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
	c.receiveBufferBytes = resolved.TCP.ReceiveBufferBytes
	return c, nil
}
