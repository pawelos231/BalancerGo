package simulation

import (
	"time"

	"load-balancer/host"
	"load-balancer/model"
	"load-balancer/network"
	"load-balancer/scenario"
)

// Graph namespaces preserve resource identities across observations.
const LoadBalancerNodeID = "infrastructure/lb"

func BackendNodeID(resourceID string) string { return "backend/" + resourceID }

// Info contains scalar run configuration and measurements at the current time.
// It owns no endpoint references, mutable collections or external representation.
type Info struct {
	LoadBalancerID                string
	Algorithm                     string
	ConfigurationRevision         uint64 // Committed world settings, independent of telemetry revisions.
	Mode                          model.LBMode
	Now                           time.Duration
	EndTime                       time.Duration
	SnapshotInterval              time.Duration
	Speed                         float64
	Seed                          int64
	ReceiveBufferBytes            int
	MSSBytes                      int
	SACKEnabled                   bool
	ECNEnabled                    bool
	DelayedACK                    model.DelayedACKConfig
	RTO                           model.RTOConfig
	Close                         model.CloseConfig
	CongestionControl             model.CongestionConfig
	ClientIdleTimeout             time.Duration
	BackendHalfOpenIdleTimeout    time.Duration
	BackendEstablishedIdleTimeout time.Duration
	LBHalfOpenIdleTimeout         time.Duration
	LBEstablishedIdleTimeout      time.Duration
	LBClosingIdleTimeout          time.Duration
	LBResetIdleTimeout            time.Duration
	EventsExecuted                uint64
	PendingEvents                 int
	ScheduledClients              int
	CreatedClients                int
	CompletedStreams              int
	ClosedClients                 int
	CompletedPayloadBytes         uint64
	Payload                       PayloadProgress
	Forwarding                    network.ForwardingStats
	LoadBalancerHost              host.Stats
}

type ClientObservation struct {
	ID, GroupID       string
	Index             int
	Config            scenario.ClientConfig
	State             model.ClientState
	Active, Completed bool
}

type BackendObservation struct{ State model.ServerState }

type RouteObservation struct {
	BackendID string
	Key       model.ClientKey
}

type LinkObservation struct {
	ID, Source, Target string
	Config             network.Config
	Stats              network.Stats
}

type IngressInputObservation struct {
	ID, Side string
	Capacity int
	Stats    network.Stats
}

type IngressObservation struct {
	Config network.Config
	Stats  network.Stats
	Inputs [2]IngressInputObservation
}

func (s *World) Info() Info {
	return Info{
		LoadBalancerID: s.configuration.LoadBalancer.ID, Algorithm: s.algorithm, Mode: s.lb.Mode,
		ConfigurationRevision: s.configurationRevision(),
		Now:                   s.engine.Now(), EndTime: s.engine.EndTime(),
		SnapshotInterval: s.fixed.Execution.SnapshotInterval,
		Speed:            s.fixed.Execution.Speed, Seed: s.fixed.Execution.Seed,
		ReceiveBufferBytes: s.fixed.TCP.ReceiveBufferBytes,
		MSSBytes:           s.fixed.TCP.MSSBytes,
		SACKEnabled:        s.fixed.TCP.SACKEnabled,
		ECNEnabled:         s.fixed.TCP.ECNEnabled,
		DelayedACK:         configuredDelayedACK(s.fixed.TCP.DelayedACK),
		CongestionControl: model.CongestionConfig{
			Mode:                           model.CongestionMode(s.fixed.TCP.CongestionControl.Mode),
			FastRetransmit:                 s.fixed.TCP.CongestionControl.FastRetransmit,
			CubicFastConvergence:           s.fixed.TCP.CongestionControl.CubicFastConvergence,
			InitialWindowSegments:          s.fixed.TCP.CongestionControl.InitialWindowSegments,
			MaximumWindowSegments:          s.fixed.TCP.CongestionControl.MaximumWindowSegments,
			InitialSlowStartThresholdBytes: s.fixed.TCP.CongestionControl.InitialSlowStartThresholdBytes,
		},
		RTO:                           configuredRTO(s.fixed.TCP.RTO),
		Close:                         configuredClose(s.fixed.TCP.Close),
		ClientIdleTimeout:             s.fixed.TCP.ClientIdleTimeout,
		BackendHalfOpenIdleTimeout:    s.fixed.TCP.BackendHalfOpenIdleTimeout,
		BackendEstablishedIdleTimeout: s.fixed.TCP.BackendEstablishedIdleTimeout,
		LBHalfOpenIdleTimeout:         s.configuration.LoadBalancer.HalfOpenIdleTimeout,
		LBEstablishedIdleTimeout:      s.configuration.LoadBalancer.EstablishedIdleTimeout,
		LBClosingIdleTimeout:          s.configuration.LoadBalancer.ClosingIdleTimeout,
		LBResetIdleTimeout:            s.configuration.LoadBalancer.ResetIdleTimeout,
		EventsExecuted:                s.engine.EventsExecuted(), PendingEvents: s.engine.Pending(),
		ScheduledClients: s.scheduledClients, CreatedClients: s.createdClients,
		CompletedStreams: s.completedStreams, ClosedClients: s.closedClients,
		CompletedPayloadBytes: s.completedPayloadBytes, Payload: s.payload,
		Forwarding:       s.egress.Stats(),
		LoadBalancerHost: s.lb.HostStats(),
	}
}

// VisitClients follows creation order and includes historical closed clients.
func (s *World) VisitClients(visit func(ClientObservation)) {
	for _, runtime := range s.clients {
		visit(ClientObservation{
			ID: runtime.ID, GroupID: runtime.GroupID, Index: runtime.Index, Config: runtime.Config,
			State: runtime.Client.GetState(), Active: !runtime.Client.IsTerminated(),
			Completed: runtime.Client.StreamCompleted(),
		})
	}
}

// VisitBackends follows active backend registration order.
func (s *World) VisitBackends(visit func(BackendObservation)) {
	s.lb.VisitBackends(func(state model.ServerState) { visit(BackendObservation{State: state}) })
}

// VisitRoutes follows backend registration, then connection table order.
func (s *World) VisitRoutes(visit func(RouteObservation)) {
	s.lb.VisitRoutes(func(backendID string, key model.ClientKey) {
		visit(RouteObservation{BackendID: backendID, Key: key})
	})
}

// VisitLinks includes historical links and follows their construction order.
func (s *World) VisitLinks(visit func(LinkObservation)) {
	for _, link := range s.links {
		visit(LinkObservation{
			ID: link.ID, Source: link.Source, Target: link.Target,
			Config: link.Link.Config(), Stats: link.Stats(),
		})
	}
}

func (s *World) Ingress() IngressObservation {
	config := s.ingress.Config()
	return IngressObservation{
		Config: config, Stats: s.ingress.Stats(),
		Inputs: [2]IngressInputObservation{
			{ID: "ingress/clients", Side: "clients", Capacity: config.QueueCapacity, Stats: s.clientIngress.Stats()},
			{ID: "ingress/backends", Side: "backends", Capacity: config.QueueCapacity, Stats: s.backendIngress.Stats()},
		},
	}
}
