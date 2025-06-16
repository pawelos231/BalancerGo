package server

import "time"

// server is an abstract representation of real-world server entities.
type Server struct {
	// --- Core attributes ---
	ID       string            // unique identifier, e.g. UUID or CMDB slug
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
	Draining       bool   // true ➜ instance is in graceful‑shutdown/connection‑draining mode

	// --- Health & Observability ---
	HealthTCP       bool  // last result of L4 probe (SYN/ACK)
	LatencyP95Milli int32 // rolling p95 latency in milliseconds (used for SLOs and decisions)

	// --- Security ---
	TLSEnabled   bool // whether the server expects TLS (termination on the instance)
	MTLSRequired bool // whether mutual TLS authentication is required

	RateLimitRPS   int32 // soft RPS limit communicated by the LB (rate‑limiter/DDoS shield)
	MaxConnections int32 // hard cap on concurrent connections (conntrack)

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
