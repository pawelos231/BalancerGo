package tcp

import (
	"math"
	"time"

	"load-balancer/engine"
	"load-balancer/model"
)

// ReceiverCallbacks connect TCP to its host without making TCP know about a backend.
// Admission reads current host limits; notifications update host observations immediately.
// Callbacks run synchronously and may stop the receiver or remove a connection.
type ReceiverCallbacks struct {
	Send               func(model.Packet)
	CanOpen            func() bool
	CanEstablish       func() bool
	OnConnectionCounts func(active, halfOpen uint32)
	OnPayloadReceived  func(int)
	OnPeerFIN          func(lifetime time.Duration) // Time since SYN, measured at the first peer FIN.
}

// Receiver owns accepted TCP connections, their byte ranges and virtual timers.
// The host decides whether a packet reaches it and whether a reply can leave.
type Receiver struct {
	engine         *engine.Engine
	callbacks      ReceiverCallbacks
	connections    map[model.ClientKey]*receiveConnection
	stopped        bool
	trafficStarted bool

	activeConnections    uint32
	halfOpenConnections  uint32
	receivedPayloadBytes uint64
	receiveBufferBytes   uint16
	sackEnabled          bool
	ecnState             model.ECNState
	delayedACKConfig     model.DelayedACKConfig
	ackState             model.ACKState

	halfOpenIdleTimeout    time.Duration
	establishedIdleTimeout time.Duration
	closeConfig            model.CloseConfig
	rtoConfig              model.RTOConfig
}

func NewReceiver(e *engine.Engine, callbacks ReceiverCallbacks) *Receiver {
	if e == nil {
		panic("tcp: receiver requires an engine")
	}
	return &Receiver{
		engine:                 e,
		callbacks:              callbacks,
		connections:            make(map[model.ClientKey]*receiveConnection),
		receiveBufferBytes:     model.DefaultReceiveBufferBytes,
		closeConfig:            model.DefaultCloseConfig(),
		rtoConfig:              model.DefaultRTOConfig(),
		delayedACKConfig:       model.DefaultDelayedACKConfig(),
		halfOpenIdleTimeout:    model.DefaultBackendHalfOpenIdleTimeout,
		establishedIdleTimeout: model.DefaultBackendEstablishedIdleTimeout,
	}
}

// SetReceiveBufferBytes sets each connection's capacity before any packet arrives.
func (r *Receiver) SetReceiveBufferBytes(capacity int) {
	if capacity < 1 || capacity > math.MaxUint16 {
		panic("tcp: receive buffer capacity must be in 1..65535")
	}
	if r.trafficStarted || len(r.connections) != 0 {
		panic("tcp: receive buffer capacity must be set before traffic")
	}
	r.receiveBufferBytes = uint16(capacity)
}

// Shutdown releases only this endpoint's state, without notifying remote peers.
func (r *Receiver) Shutdown() {
	if r.stopped {
		return
	}
	r.stopped = true
	r.ResetConnections()
}

// ResetConnections discards local flows and timers while preserving configuration and counters.
func (r *Receiver) ResetConnections() {
	for key := range r.connections {
		r.ForgetConnection(key)
	}
}

func (r *Receiver) ReceivedPayloadBytes() uint64 {
	return r.receivedPayloadBytes
}

// notifyConnectionCounts runs at the same moment TCP reserves or releases a slot.
func (r *Receiver) notifyConnectionCounts() {
	if r.callbacks.OnConnectionCounts != nil {
		r.callbacks.OnConnectionCounts(r.activeConnections, r.halfOpenConnections)
	}
}

func (r *Receiver) recordPeerFIN(elapsed time.Duration) {
	if r.callbacks.OnPeerFIN != nil {
		r.callbacks.OnPeerFIN(elapsed)
	}
}

func (r *Receiver) emit(packet model.Packet) {
	if !r.stopped && r.callbacks.Send != nil {
		r.callbacks.Send(packet)
	}
}
