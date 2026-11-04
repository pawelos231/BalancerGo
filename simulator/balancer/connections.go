package balancer

import (
	"time"

	"load-balancer/model"
)

// TimeoutConfig chooses how long each kind of idle mapping stays in the LB.
// Zero disables cleanup for that phase; these limits are independent of endpoint TCP.
type TimeoutConfig struct {
	HalfOpen    time.Duration
	Established time.Duration
	Closing     time.Duration
	Reset       time.Duration
}

type connectionPhase uint8

const (
	stateHalfOpen    connectionPhase = iota // SYN observed; waiting for a client ACK.
	stateEstablished                        // Client ACK or DATA+ACK observed.
	stateClosing                            // FIN observed in either direction.
	stateReset                              // RST observed in either direction.
)

// connectionState holds only the LB's phase and last observed activity.
// It does not track the endpoint's sequence numbers or TCP timers.
type connectionState struct {
	lastActivity time.Duration
	phase        connectionPhase
}

// SetMappingTimeouts sets local cleanup limits before clients arrive.
func (lb *LoadBalancer) SetMappingTimeouts(config TimeoutConfig) {
	validLimits := config.HalfOpen >= 0 && config.Established >= 0 &&
		config.Closing >= 0 && config.Reset >= 0

	if !validLimits || len(lb.clients) != 0 {
		panic("balancer: nonnegative mapping timeouts must be set before clients arrive")
	}
	lb.timeouts = config
}

// observe refreshes a matched flow and follows flags, without checking TCP byte positions.
func (connection *connectionState) observe(pkt model.Packet, fromServer bool, now time.Duration) {
	if !supportedMappingPacket(pkt, fromServer) {
		return
	}
	connection.lastActivity = now

	if pkt.Flag&model.FlagRST != 0 {
		connection.phase = stateReset
		return
	}
	if connection.phase == stateReset {
		return
	}

	// ACKs and delayed SYNs can refresh closing traffic, but cannot reopen the mapping.
	if pkt.Flag&model.FlagFIN != 0 {
		connection.phase = stateClosing
		return
	}
	if connection.phase == stateClosing {
		return
	}

	// DATA+ACK also completes observation when the standalone handshake ACK was lost.
	if !fromServer && pkt.Flag&model.FlagSYN == 0 && pkt.Flag&model.FlagACK != 0 {
		connection.phase = stateEstablished
	}
}

// supportedMappingPacket defines the packet shapes this simplified L4 observer understands.
// TCP receivers independently decide whether the sequence numbers are acceptable.
func supportedMappingPacket(pkt model.Packet, fromServer bool) bool {
	if pkt.Key.Protocol != model.TCP || pkt.Size < 0 {
		return false
	}
	// ECE/CWR are endpoint signaling; they do not change this mapping's phase.
	baseFlags := pkt.Flag &^ (model.FlagECE | model.FlagCWR)
	switch baseFlags {
	case model.FlagSYN:
		return !fromServer && pkt.Size == 0
	case model.FlagSYN | model.FlagACK:
		return fromServer && pkt.Size == 0
	case model.FlagACK, model.FlagPSH | model.FlagACK:
		return true
	case model.FlagFIN | model.FlagACK, model.FlagRST, model.FlagRST | model.FlagACK:
		return pkt.Size == 0
	default:
		return false
	}
}

// expired uses LB-owned activity and state; it never asks whether the client is alive.
func (lb *LoadBalancer) expired(connection *connectionState) bool {
	timeout := lb.timeouts.HalfOpen
	switch connection.phase {
	case stateEstablished:
		timeout = lb.timeouts.Established
	case stateClosing:
		timeout = lb.timeouts.Closing
	case stateReset:
		timeout = lb.timeouts.Reset
	}
	return timeout > 0 && lb.engine.Now()-connection.lastActivity >= timeout
}
