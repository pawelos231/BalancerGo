package receiver

import (
	"time"

	"load-balancer/engine"
	"load-balancer/model"
)

// receiveConnection owns one peer's handshake, receive buffer and closing timers.
type receiveConnection struct {
	startedAt      time.Duration
	established    bool
	state          model.TCPState
	peerInitialSeq uint64
	sendNextSeq    uint64
	receiver       receiveBuffer
	lastActivity   time.Duration
	idleEvent      engine.EventID

	// The first waiting segment starts one deadline; a second can cancel it.
	pendingACKSegmentCount int
	delayedACKEvent        engine.EventID

	// Keep the first SYN's SACK agreement and recently reported blocks.
	sackPermitted   bool
	sackRecent      [model.MaxSACKBlocks]model.SACKBlock
	sackRecentCount int
	ecnNegotiated   bool
	ecnEcho         bool // Repeat ECE until a valid CWR arrives, even after an ACK is lost.

	closeStarted   bool
	closeStartedAt time.Duration
	outcome        model.CloseOutcome
	finSent        bool
	finAcked       bool
	finSeq         uint64
	finRetries     uint64
	finTimeouts    int
	finRTO         time.Duration
	finEvent       engine.EventID
	waitEvent      engine.EventID
	peerFIN        bool
	peerFINSeq     uint64
	pendingFIN     bool
	pendingFINSeq  uint64
}

func (r *Receiver) HandlePacket(pkt model.Packet) {
	if r.stopped || pkt.Key.Protocol != model.TCP {
		return
	}
	r.trafficStarted = true
	sequenceEnd, valid := pkt.SequenceEnd()
	if !valid || !pkt.ValidSACKOptions() ||
		pkt.Flag&model.FlagSYN != 0 && pkt.Flag&model.FlagFIN != 0 {
		return
	}
	conn := r.connections[pkt.Key]
	if pkt.Flag&model.FlagRST != 0 {
		r.handleRST(pkt, conn)
		return
	}
	if pkt.Flag&model.FlagSYN != 0 {
		r.handleSYN(pkt, conn, sequenceEnd)
		return
	}
	if conn == nil || conn.state == model.TCPClosed || pkt.Flag&model.FlagACK == 0 ||
		!conn.validACK(pkt.Ack) {
		return
	}
	if pkt.Flag&model.FlagFIN != 0 {
		r.handleFIN(pkt, conn)
		return
	}

	// Data can complete the handshake when the standalone third ACK was lost.
	if !conn.established {
		validSequence := pkt.Seq == conn.receiver.nextExpectedSeq
		if pkt.Size > 0 {
			validSequence = conn.receiver.overlapsWindow(pkt.Seq, sequenceEnd)
		}
		if !validSequence || !r.establishConnection(pkt.Key, conn) {
			return
		}
	}
	if pkt.Size == 0 {
		if pkt.Seq == conn.receiver.nextExpectedSeq {
			if r.acknowledgeFIN(pkt.Key, conn, pkt.Ack) {
				r.observeECN(conn, pkt)
				r.recordConnectionActivity(pkt.Key, conn)
			}
		}
		return
	}
	if conn.peerFIN {
		return // The peer has already ended its byte stream.
	}

	// Only fresh data arriving in order can wait. Filling an old gap stays urgent.
	windowBefore := conn.receiver.windowBytes()
	mayDelayACK := pkt.Seq == conn.receiver.nextExpectedSeq && len(conn.receiver.ranges) == 0
	newECNFeedback := conn.ecnNegotiated && pkt.ECN.Codepoint == model.CE &&
		(!conn.ecnEcho || pkt.Flag&model.FlagCWR != 0)
	if conn.receiver.overlapsWindow(pkt.Seq, sequenceEnd) {
		if !r.acknowledgeFIN(pkt.Key, conn, pkt.Ack) {
			return
		}
		r.observeECN(conn, pkt)
		r.recordConnectionActivity(pkt.Key, conn)
	}
	if conn.pendingFIN {
		sequenceEnd = min(sequenceEnd, conn.pendingFINSeq)
	}
	newBytes := conn.receiver.accept(pkt.Seq, sequenceEnd)
	conn.recordSACKArrival(pkt.Seq, sequenceEnd)
	if newBytes > 0 {
		r.receivedPayloadBytes += uint64(newBytes)
		if r.callbacks.OnPayloadReceived != nil {
			r.callbacks.OnPayloadReceived(newBytes)
		}
	}
	if r.connections[pkt.Key] != conn {
		return
	}
	if conn.pendingFIN && conn.receiver.nextExpectedSeq == conn.pendingFINSeq {
		r.consumeFIN(pkt.Key, conn, conn.pendingFINSeq)
		return
	}

	// Partial/out-of-window copies, window changes and fresh CE feedback need an ACK now.
	mayDelayACK = mayDelayACK && newBytes == pkt.Size &&
		windowBefore == conn.receiver.windowBytes() && !newECNFeedback
	r.acknowledgeData(pkt.Key, conn, mayDelayACK)
}

// validACK permits an old ACK while our FIN is still in flight.
func (conn *receiveConnection) validACK(ack uint64) bool {
	return ack >= 1 && ack <= conn.sendNextSeq
}

// handleSYN reserves one flow; repeated SYNs keep its original byte positions.
func (r *Receiver) handleSYN(pkt model.Packet, conn *receiveConnection, sequenceEnd uint64) {
	if pkt.Size != 0 || pkt.Flag&model.FlagACK != 0 {
		return
	}
	if conn == nil {
		if r.callbacks.CanOpen != nil && !r.callbacks.CanOpen() {
			return
		}
		if r.stopped || r.connections[pkt.Key] != nil {
			return
		}
		conn = &receiveConnection{
			startedAt:      r.engine.Now(),
			state:          model.TCPSynReceived,
			peerInitialSeq: pkt.Seq,
			sendNextSeq:    1, // Fixed backend ISN zero; SYN occupies one position.
			sackPermitted:  r.sackEnabled && pkt.SACKPermitted,
			ecnNegotiated:  r.ecnState.Enabled && pkt.Flag&(model.FlagECE|model.FlagCWR) == model.FlagECE|model.FlagCWR,
			receiver: receiveBuffer{
				nextExpectedSeq: sequenceEnd,
				capacity:        r.receiveBufferBytes,
			},
		}
		r.connections[pkt.Key] = conn
		r.halfOpenConnections++

		// Retransmitted SYNs keep this first handshake deadline.
		conn.lastActivity = r.engine.Now()
		r.scheduleIdleTimeout(pkt.Key, conn, r.halfOpenIdleTimeout)
		r.notifyConnectionCounts()
		// A host callback can stop the receiver or remove this reservation.
		if r.stopped || r.connections[pkt.Key] != conn {
			return
		}
	}
	if pkt.Seq != conn.peerInitialSeq || conn.finSent || conn.state == model.TCPClosed {
		return
	}
	r.emit(model.Packet{
		Key:                pkt.Key,
		Flag:               synAckFlags(conn),
		Seq:                0,
		Ack:                conn.peerInitialSeq + 1,
		ReceiveWindowBytes: conn.receiver.windowBytes(),
		SACKPermitted:      conn.sackPermitted,
	})
}

// establishConnection moves one reservation from half-open to active once.
func (r *Receiver) establishConnection(key model.ClientKey, conn *receiveConnection) bool {
	if r.callbacks.CanEstablish != nil && !r.callbacks.CanEstablish() {
		r.finishConnection(key, conn, model.CloseAborted)
		return false
	}
	if r.stopped || r.connections[key] != conn {
		return false
	}
	conn.established = true
	conn.state = model.TCPEstablished
	r.cancelIdleTimer(conn)
	r.recordConnectionActivity(key, conn)
	if r.halfOpenConnections > 0 {
		r.halfOpenConnections--
	}
	r.activeConnections++
	r.notifyConnectionCounts()
	return !r.stopped && r.connections[key] == conn
}

// ForgetConnection releases local state and cancels every connection timer.
func (r *Receiver) ForgetConnection(key model.ClientKey) {
	if conn := r.connections[key]; conn != nil {
		r.finishConnection(key, conn, model.CloseAborted)
	}
}
