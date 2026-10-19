package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"load-balancer/engine"
	httpserver "load-balancer/http"
	"load-balancer/model"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/trace"
	"sync/atomic"
	"syscall"
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
	if c.clients < 0 || c.clients >= 1<<24 || c.servers < 0 || c.packets < 0 || c.streamBytes < -1 {
		return c, errors.New("invalid client, server or stream size")
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

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := readConfiguration()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.tracePath != "" {
		file, err := os.Create(cfg.tracePath)
		if err != nil {
			return err
		}
		if err := trace.Start(file); err != nil {
			file.Close()
			return err
		}
		defer file.Close()
		defer trace.Stop()
	}

	// These channels and goroutines are application I/O, not modelled transport.
	commands := make(chan httpserver.Envelope, 64)
	var accepting atomic.Bool
	accepting.Store(true)
	var hub *httpserver.Hub
	if !cfg.headless {
		hub = httpserver.NewHub(func(command httpserver.Envelope) error {
			if !accepting.Load() {
				return errors.New("experiment has finished; restart to run another")
			}
			select {
			case commands <- command:
				return nil
			default:
				return errors.New("simulation command queue is full")
			}
		})
		go hub.Run(ctx)
		srv := httpserver.NewServer(hub, os.Getenv("LB_HTTP_ADDR"))
		// Bind synchronously so startup fails before spending time on a simulation.
		listener, err := net.Listen("tcp", srv.Addr)
		if err != nil {
			return err
		}
		go func() {
			if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("HTTP server: %v", err)
				stop()
			}
		}()
		defer func() {
			shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			srv.Shutdown(shutdown)
		}()
		log.Printf("WebSocket server listening on %s/ws", srv.Addr)
	}

	sim := newSimulation(cfg, hub)
	started := time.Now()

	err = sim.run(ctx, commands)
	accepting.Store(false)

	for n := len(commands); n > 0; n-- {
		command := <-commands
		log.Printf("command %q rejected: experiment has finished", command.Type)
	}

	result := sim.result(time.Since(started))
	if encodeErr := json.NewEncoder(os.Stdout).Encode(result); encodeErr != nil {
		return encodeErr
	}

	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}

	if !cfg.headless {
		log.Print("Experiment finished; final snapshot remains available until shutdown")
		<-ctx.Done()
	}

	return nil
}
