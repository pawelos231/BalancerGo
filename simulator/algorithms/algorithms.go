package algorithms

// Candidate contains settings and mapping counts known to the load balancer.
// MappedConnections counts LB mappings, independently of backend TCP connections.
type Candidate struct {
	MaxConnections    uint32 // Zero means unlimited.
	MappedConnections uint32
}

type Algorithm interface {
	// Pick returns the selected candidate's index in the input slice.
	// On error, it returns -1.
	Pick([]Candidate) (int, error)
}
