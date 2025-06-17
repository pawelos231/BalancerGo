package server

import (
	"math"
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
}

func NewServer(id, name, address string, port, distance, weight int16) *Server {
	return &Server{
		ID:       id,
		Name:     name,
		Address:  address,
		Port:     port,
		Distance: distance,
		Weight:   weight,
		Active:   true, // default to active
		Tags:     []string{},
		Metadata: make(map[string]string),
	}
}

func (s *Server) StartDrain() { s.Draining = true }
func (s *Server) StopDrain()  { s.Draining = false }

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

func (s *Server) AcceptConnection() bool {
	if !s.IsHealthy() {
		return false // cannot accept connections if unhealthy
	}
	if s.MaxConnections > 0 && s.MaxConnections <= s.ActiveConnections {
		return false // max connections reached
	}
	return true
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
