package balancer

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"load-balancer/algorithms"
	"load-balancer/client"
	"load-balancer/engine"
	"load-balancer/model"
	"load-balancer/server"
)

// routingBackends registers real hosts without exchanging TCP packets.
// Each limit is the capacity initially advertised to this LB.
func routingBackends(t *testing.T, limits ...uint32) (*LoadBalancer, []*server.Server) {
	t.Helper()
	e := engine.New(time.Second)
	lb := NewLoadBalancer(e, model.LBModeNAT, &algorithms.RoundRobin{}, rand.New(rand.NewSource(1)))
	t.Cleanup(lb.Shutdown)
	backends := make([]*server.Server, 0, len(limits))
	for index, limit := range limits {
		id := fmt.Sprintf("backend-%d", index)
		address := fmt.Sprintf("10.0.0.%d", index+1)
		backend := server.NewServer(e, id, id, address, 80, 0, 1, 100)
		backend.MaxConnections = limit
		if err := lb.AddServer(backend, func(model.Packet) {}); err != nil {
			t.Fatal(err)
		}
		backends = append(backends, backend)
	}
	return lb, backends
}

func routingClient(t *testing.T, lb *LoadBalancer, port uint16) *client.Client {
	t.Helper()
	endpoint := client.NewClient(lb.engine, "192.0.0.1", port, "lb.local", 80, model.TCP)
	if err := lb.AddClient(endpoint, func(model.Packet) {}); err != nil {
		t.Fatal(err)
	}
	return endpoint
}

func TestPreparedAlgorithmDoesNotSelectOrChangeRoutes(t *testing.T) {
	lb, backends := routingBackends(t, 0, 0)
	first := routingClient(t, lb, 40000)
	if _, err := lb.Route(first); err != nil {
		t.Fatal(err)
	}
	previous := lb.GetAlgorithm()
	rng := rand.New(rand.NewSource(23))
	reference := rand.New(rand.NewSource(23))
	selection := &algorithms.Random{RNG: rng}
	prepared := lb.PrepareAlgorithm(selection)
	if lb.GetAlgorithm() != previous {
		t.Fatal("preparing an algorithm installed it before the commit")
	}
	prepared.Apply()
	if lb.GetAlgorithm() != selection || lb.byKey[first.Key()] != backends[0] {
		t.Fatal("applying a prepared algorithm changed an existing route or installed another selector")
	}
	if selected, err := lb.Route(routingClient(t, lb, 40001)); err != nil || selected != backends[reference.Intn(len(backends))] {
		t.Fatalf("the prepared selector did not use the original RNG sequence: %v, %v", selected, err)
	}
	if got, want := rng.Int63(), reference.Int63(); got != want {
		t.Fatal("algorithm preparation or application consumed a random draw")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("nil algorithm preparation did not fail before application")
			}
		}()
		lb.PrepareAlgorithm(nil)
	}()
	if lb.GetAlgorithm() != selection {
		t.Fatal("failed algorithm preparation changed the selector")
	}
}

// A one-slot backend has room for one LB mapping, regardless of the host's
// independent established-TCP counter. A half-open mapping occupies that slot too.
func TestRouteUsesLBMappingsInsteadOfBackendTCPCount(t *testing.T) {
	lb, backends := routingBackends(t, 1)
	backend := backends[0]
	backend.ActiveConnections = 100
	first := routingClient(t, lb, 40000)

	selected, err := lb.Route(first)
	if err != nil || selected != backend {
		t.Fatalf("first mapping selected %v, err=%v", selected, err)
	}

	// Clearing the host counter does not release a mapping owned by the LB.
	backend.ActiveConnections = 0
	second := routingClient(t, lb, 40001)
	if _, err := lb.Route(second); err == nil {
		t.Fatal("the second client bypassed the LB's one-mapping limit")
	}
}

// Changing a host field does not instantly update the LB's registered settings.
func TestRouteRetainsRegistrationTimeCapacity(t *testing.T) {
	lb, backends := routingBackends(t, 1)
	backends[0].MaxConnections = 0
	if _, err := lb.Route(routingClient(t, lb, 40000)); err != nil {
		t.Fatal(err)
	}
	if _, err := lb.Route(routingClient(t, lb, 40001)); err == nil {
		t.Fatal("routing read the host's new unlimited setting instead of the registered limit")
	}
}

// fixedCandidate exposes what the LB passes to an algorithm and returns one index.
type fixedCandidate struct {
	index      int
	candidates []algorithms.Candidate
}

func (selection *fixedCandidate) Pick(candidates []algorithms.Candidate) (int, error) {
	selection.candidates = append([]algorithms.Candidate(nil), candidates...)
	return selection.index, nil
}

// With the middle backend unhealthy, candidate 1 must identify backend 2.
func TestRouteResolvesIndexWithinHealthyCandidates(t *testing.T) {
	lb, backends := routingBackends(t, 1, 2, 3)
	lb.health[backends[1]].healthy = false
	selection := &fixedCandidate{index: 1}
	lb.algo = selection

	selected, err := lb.Route(routingClient(t, lb, 40000))
	if err != nil || selected != backends[2] {
		t.Fatalf("filtered index 1 selected %v, err=%v", selected, err)
	}
	if len(selection.candidates) != 2 || selection.candidates[0].MaxConnections != 1 ||
		selection.candidates[1].MaxConnections != 3 {
		t.Fatalf("candidates do not follow healthy backend order: %+v", selection.candidates)
	}
}

// A bad algorithm result must fail before any mapping is stored.
func TestRouteRejectsUnknownCandidateIndex(t *testing.T) {
	for _, index := range []int{-1, 2, 100} {
		t.Run(fmt.Sprintf("index_%d", index), func(t *testing.T) {
			lb, backends := routingBackends(t, 1, 1)
			lb.algo = &fixedCandidate{index: index}
			if _, err := lb.Route(routingClient(t, lb, 40000)); err == nil {
				t.Fatal("invalid algorithm result was accepted")
			}
			if len(lb.byKey) != 0 || len(lb.connections) != 0 ||
				len(lb.connTable[backends[0]]) != 0 || len(lb.connTable[backends[1]]) != 0 {
				t.Fatal("invalid selection left a partial mapping")
			}
		})
	}
}
