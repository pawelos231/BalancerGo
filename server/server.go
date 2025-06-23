package server

import (
	"fmt"
	"load-balancer/model"
	"math"
	"sort"
	"sync/atomic"
	"time"
)

// server is an abstract representation of real-world server entities.
type Server struct {
	// --- Core attributes ---
	ID       string            // unique identifier -> (SrcIP,SrcPort,DstIP,DstPort,Protocol)
	Name     string            // human‑readable instance name
	Address  string            // IP address or hostname that the LB forwards traffic to
	Port     int16             // port the server listens on
	Distance int16             // distance in km from the LB (used by nearest‑node algorithms)
	Weight   int16             // load‑balancer weight; higher = more preferred
	Active   bool              // true = accepts traffic; false = drained/disabled
	Killed   bool              // true = server is killed and should not be used
	Tags     []string          // free‑form labels (e.g. "prod", "gpu", "arm")
	Metadata map[string]string // arbitrary key‑value metadata

	// --- High Availability / Scaling ---
	Region         string // cloud/colo region (multi‑region routing)
	Zone           string // availability zone (multi‑AZ fail‑over)
	AutoscaleGroup string // auto‑scaling group ID/name for correlation with scaling events
	Draining       bool   // true -> instance is in graceful‑shutdown/connection‑draining mode

	// --- Health & Observability ---
	HealthTCP       bool  // last result of L4 probe (SYN/ACK)
	LatencyP95Milli int32 // rolling p95 latency in milliseconds (used for SLOs and decisions)

	// --- Security ---
	TLSEnabled   bool // whether the server expects TLS (termination on the instance)
	MTLSRequired bool // whether mutual TLS authentication is required

	RateLimitRPS        int32         // soft RPS limit communicated by the LB (rate‑limiter/DDoS shield)
	MaxConnections      int32         // hard cap on concurrent connections (conntrack)
	ActiveConnections   int32         // current number of active connections (conntrack)
	HalfOpenConnections int32         // number of half-open connections (TCP SYNs in flight)
	MaxHalfOpen         int32         // maximum allowed half‑open connections (to prevent SYN flood)
	DrainTimeout        time.Duration // how long to wait for graceful shutdown before force‑closing connections
	DrainStartedAt      time.Time     // when the draining started (used to determine if we should exit the pool)

	// --- DevOps / Rollout ---
	Version       string    // semantic version/image tag (blue‑green/canary tracking)
	ConfigVersion string    // hash/version of the current runtime configuration
	LastUpdated   time.Time // timestamp of the last configuration change

	// internal
	DataIn                 chan model.Packet // channel for incoming packets from the LB
	DataOut                chan model.Packet // channel for outgoing packets to the LB
	Probe                  chan struct{}     // channel for health probes (e.g. TCP SYNs)
	ProbeAck               chan struct{}     // channel for health probe ACKs
	Done                   chan struct{}     // channel to signal that the server is done processing
	NumberOfHandledPackets int32             // total number of packets handled by this server
}

func NewServer(id, name, address string, port, distance, weight int16,
	out chan model.Packet,
	in chan model.Packet, maxHalfOpen int32) *Server {

	return &Server{
		ID:                     id,
		Name:                   name,
		Address:                address,
		Port:                   port,
		Distance:               distance,
		Weight:                 weight,
		MaxHalfOpen:            maxHalfOpen,
		HealthTCP:              true,  // default to healthy
		LatencyP95Milli:        0,     // default to 0 latency
		TLSEnabled:             false, // default to no TLS
		MTLSRequired:           false, // default to no mTLS
		Active:                 true,  // default to active
		Tags:                   []string{},
		Metadata:               make(map[string]string),
		DrainTimeout:           120 * time.Second,      // default drain timeout
		DataIn:                 in,                     // channel for incoming packets from the load balancer
		DataOut:                out,                    // channel for outgoing packets to the load balancer
		Probe:                  make(chan struct{}, 1), // buffered channel for health probes
		ProbeAck:               make(chan struct{}, 1), // buffered channel for health probe ACKs
		Done:                   make(chan struct{}, 1), // channel to signal that the server is done processing
		NumberOfHandledPackets: 0,                      // initialize handled packets count
	}
}

// Abruptly stops the server and closes all channel, just if the power goes off
func (s *Server) Kill() {
	s.Killed = true // mark the server as killed
	s.Active = false
	s.HealthTCP = false
	close(s.Done)     // close done channel to stop processing packets
	close(s.DataIn)   // close incoming channel to stop receiving packets
	close(s.DataOut)  // close outgoing channel to stop sending packets
	close(s.Probe)    // close probe channel to stop health checks
	close(s.ProbeAck) // close probe ACK channel
}

func (s *Server) MarkFailed() {
	fmt.Println("Marking server as FAILED:", s.ID, s.Name)
	s.Active = false
	s.HealthTCP = false
	atomic.StoreInt32(&s.LatencyP95Milli, math.MaxInt32) // mark latency as unknown
	s.Draining = true
	s.DrainStartedAt = time.Now() // start the draining process
}

func (s *Server) MarkHealthy() {
	if s.Killed {
		fmt.Println("Cannot mark server as HEALTHY, it is KILLED:", s.ID, s.Name)
		return
	}
	fmt.Println("Marking server as HEALTHY:", s.ID, s.Name)
	s.Active = true
	s.HealthTCP = true
	atomic.StoreInt32(&s.LatencyP95Milli, 0) // reset latency to 0 (healthy)
	s.Draining = false
	s.DrainStartedAt = time.Time{} // reset drain start time
}

func (s *Server) StartDrain() {
	s.Draining = true
	s.DrainStartedAt = time.Now() // start the draining process
}
func (s *Server) StopDrain() {
	s.Draining = false
	s.DrainStartedAt = time.Time{} // reset drain start time
}

func (s *Server) HandleHealthChecking() {
	if s.Killed {
		return // do not handle health checks if the server is killed
	}

	for {
		select {
		case <-s.Done:
			return // exit if the server is killed or done
		case <-s.Probe:
			s.ProbeAck <- struct{}{} // acknowledge the health probe
		}
	}
}

func (s *Server) HandlePacketStream(stream <-chan model.Packet) {
	connStart := make(map[model.ClientKey]time.Time)
	const bucket = 100
	var latSamples [bucket]int64 // latency samples for p95 calculation
	var sampleIdx int

	updateP95 := func(ms int64) {
		latSamples[sampleIdx%bucket] = ms
		sampleIdx++

		n := sampleIdx
		if n > bucket {
			n = bucket
		}

		tmp := make([]int64, n)
		copy(tmp, latSamples[:n])
		sort.Slice(tmp, func(i, j int) bool { return tmp[i] < tmp[j] })

		idx := int(math.Ceil(0.95*float64(n))) - 1
		if idx < 0 {
			idx = 0
		}

		p95 := tmp[idx]
		atomic.StoreInt32(&s.LatencyP95Milli, int32(p95))
	}

	// var (
	// 	ctx    context.Context
	// 	cancel context.CancelFunc
	// )
	// defer func() {
	// 	if cancel != nil {
	// 		cancel() // ensure the context is cancelled to avoid leaks
	// 	}
	// }()

	// resetCtx := func() {
	// 	if cancel != nil {
	// 		cancel() // cancel the previous context if it exists
	// 	}
	// 	ctx, cancel = context.WithTimeout(context.Background(), model.READ_DEADLINE)
	// }
	// resetCtx() // initialize the context

	for pkt := range stream {
		s.NumberOfHandledPackets++ // increment the number of handled packets

		// fmt.Println("Server received packet:", pkt.Key.SrcIP, pkt.Flag, s.ID, pkt.PlaceInStream)
		switch {
		// new SYN packet, we are starting a new connection
		case pkt.Flag&model.FlagSYN != 0 && pkt.Flag&model.FlagACK == 0:
			key := pkt.Key                             // extract client key from packet
			atomic.AddInt32(&s.HalfOpenConnections, 1) // increment active connections
			connStart[key] = time.Now()                // record the start time for this connection

			resp := model.Packet{
				Key:           key,
				Flag:          model.FlagSYN | model.FlagACK, // respond with SYN-ACK
				PlaceInStream: pkt.PlaceInStream + 1,         // increment place in stream
				Size:          pkt.Size,                      // echo the size of the SYN packet
			}

			s.DataOut <- resp // send SYN-ACK back to the client

		// if ACK from client, we are in the middle of a handshake
		// so we can drop the half-open slot
		// ACK packet, we are completing the handshake, and the connection is now active
		case pkt.Flag&model.FlagACK != 0 && pkt.Flag&model.FlagSYN == 0 && connStart[pkt.Key] != (time.Time{}):
			atomic.AddInt32(&s.ActiveConnections, 1)    // increment active connections
			atomic.AddInt32(&s.HalfOpenConnections, -1) // decrement half-open connections

		case pkt.Flag&model.FlagPSH != 0:
			pkt := model.Packet{
				Key:           pkt.Key,
				Flag:          model.FlagACK,
				PlaceInStream: pkt.PlaceInStream + 1, // increment place in stream
				Size:          1024,
			}
			s.DataOut <- pkt

		case pkt.Flag&model.FlagFIN != 0:
			atomic.AddInt32(&s.ActiveConnections, -1)

			if _, ok := connStart[pkt.Key]; ok {
				t0 := connStart[pkt.Key]
				updateP95(time.Since(t0).Milliseconds())
				delete(connStart, pkt.Key)
			}

			s.DataOut <- model.Packet{
				Key:           pkt.Key,
				Flag:          model.FlagFIN | model.FlagACK, // respond with FIN-ACK
				PlaceInStream: pkt.PlaceInStream + 1,         // increment place in stream
				Size:          0,
			}

		default:
			// if we receive a packet that is not SYN, ACK, PSH or FIN, we assume it is a FIN packet
			atomic.AddInt32(&s.ActiveConnections, -1)

			if _, ok := connStart[pkt.Key]; ok {
				t0 := connStart[pkt.Key]
				updateP95(time.Since(t0).Milliseconds())
				delete(connStart, pkt.Key)
			}

			s.DataOut <- model.Packet{
				Key:           pkt.Key,
				Flag:          model.FlagFIN | model.FlagACK, // respond with FIN-ACK
				PlaceInStream: pkt.PlaceInStream + 1,         // increment place in stream
				Size:          0,
			}

		}

		//fmt.Println(s.HalfOpenConnections, s.MaxHalfOpen, pkt.PlaceInStream, pkt.Flag, s.Name, pkt.Key.SrcIP, s.ActiveConnections)

	}
}

func (s *Server) generateLatencyHistogram() []int32 {
	return []int32{}
}

func (s *Server) CanAcceptNewConnection() bool {
	if !s.Active || s.Draining && s.IsHealthy() {
		return false // cannot accept new connections if inactive or draining
	}
	if s.MaxConnections > 0 && s.ActiveConnections >= s.MaxConnections {
		return false // max connections reached
	}
	return true
}

func (s *Server) UpdateHealth(healthTCP bool, latencyP95Milli int32) {
	s.HealthTCP = healthTCP
	atomic.StoreInt32(&s.LatencyP95Milli, latencyP95Milli)
}

func (s *Server) ShouldBeTerminated() bool {
	return s.Draining && s.ActiveConnections == 0 && s.HalfOpenConnections == 0
}

func (s *Server) IsHealthy() bool {
	if s.Draining &&
		time.Since(s.DrainStartedAt) > s.DrainTimeout &&
		atomic.LoadInt32(&s.ActiveConnections) == 0 {
		return false // if draining and timeout exceeded, consider unhealthy
	}
	return s.HealthTCP && s.Active && !s.Draining
}

func (s *Server) Score() float64 {
	if !s.IsHealthy() {
		return math.Inf(1) // unhealthy servers should be at the end of the list
	}

	drainingPenalty := 0.0
	if s.Draining {
		drainingPenalty = 10.0 // arbitrary penalty for draining servers
	}

	return float64(s.LatencyP95Milli)*(1.0/float64(max(1, int(s.Weight)))) + drainingPenalty
}

func (s *Server) GetState() model.ServerState {
	return model.ServerState{
		ID:                     s.ID,
		Name:                   s.Name,
		Address:                s.Address,
		Port:                   s.Port,
		Active:                 s.Active,
		ActiveConnections:      atomic.LoadInt32(&s.ActiveConnections),
		HalfOpenConnections:    atomic.LoadInt32(&s.HalfOpenConnections),
		LatencyP95Milli:        atomic.LoadInt32(&s.LatencyP95Milli),
		NumberOfHandledPackets: s.NumberOfHandledPackets,
	}
}
