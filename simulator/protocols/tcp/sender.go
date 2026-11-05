package tcp

import (
	"time"

	"load-balancer/engine"
	"load-balancer/model"
)

// Sender owns one TCP connection, including windows, retries and orderly shutdown.
// All calls and callbacks belong to the virtual-time engine's owner.
type Sender struct {
	key    model.ClientKey
	engine *engine.Engine
	sender func(model.Packet)

	terminated         bool
	connection         connectionClose
	handshakeStarted   bool
	handshakeCompleted bool
	retrySyn           uint8

	// Sent segments remain here until their bytes are cumulatively acknowledged.
	pendingSegments sendBuffer

	// Counts retries of the oldest pending fragment, resetting on ACK progress.
	oldestSegmentRetransmissions uint8

	rto         rtoEstimator
	rttProbe    roundTripProbe
	idleTimeout time.Duration
	congestion  congestionControl
	sack        sackSender
	ecn         senderECN

	nextSendSeq      uint64
	oldestUnackedSeq uint64
	nextReceiveSeq   uint64
	peerInitialSeq   uint64

	// New data must stay below the byte position advertised by the backend.
	peerWindowEnd uint64

	// Keep the raw window too: a window update is not evidence of missing data.
	peerReceiveWindow uint16

	// Advertised on our packets; this profile receives no backend payload.
	receiveWindowBytes uint16
	mssBytes           int // Maximum payload per segment; shared with congestion control and SACK.

	streamCompleted                bool
	remainingPayloadBytesToConfirm int
	source                         PayloadSource

	acknowledgedPayloadBytes uint64
	transfer                 transferMetrics

	synSendEvent engine.EventID
	synTimer     engine.EventID
	dataTimer    engine.EventID
	readTimer    engine.EventID
	sendEvent    engine.EventID
	sendEventAt  time.Duration

	OnClose          func()
	OnStreamComplete func()

	// Tells the simulation how many new payload bytes the client has ACK-confirmed.
	// Repeated ACKs for the same segment do not count those bytes again.
	OnPayloadAcknowledged func(int)
}

func NewSender(
	e *engine.Engine,
	key model.ClientKey,
) *Sender {
	if e == nil {
		panic("tcp: nil engine")
	}
	if key.Protocol != model.TCP {
		panic("tcp: only the simplified TCP model is implemented")
	}
	return &Sender{
		engine:             e,
		key:                key,
		receiveWindowBytes: model.DefaultReceiveBufferBytes,
		mssBytes:           model.DefaultMSSBytes,
		rto:                newRTOEstimator(model.DefaultRTOConfig()),
		idleTimeout:        model.READ_DEADLINE,
		congestion:         newCongestionControl(model.DefaultCongestionConfig()),
		connection:         connectionClose{config: model.DefaultCloseConfig(), state: model.TCPClosed},
		transfer:           transferMetrics{status: model.TransferInProgress},
	}
}

// SetReceiveBufferBytes sets the window advertised when this connection starts.
func (c *Sender) SetReceiveBufferBytes(capacity int) {
	if capacity < 1 || capacity > model.DefaultReceiveBufferBytes {
		panic("tcp: receive buffer must be in 1..65535 bytes")
	}
	if c.handshakeStarted || c.terminated {
		panic("tcp: receive buffer must be set before the connection starts")
	}

	c.receiveWindowBytes = uint16(capacity)
}

// SetMSSBytes selects a fixed payload limit before the connection starts.
// For MSS 1460, sending 3000 bytes can produce 1460 + 1460 + 80 bytes.
func (c *Sender) SetMSSBytes(bytes int) {
	c.SetCongestionConfigWithMSS(c.congestion.config, bytes)
}

func (c *Sender) SetSender(sender func(model.Packet)) {
	c.sender = sender
}

func (c *Sender) Key() model.ClientKey {
	return c.key
}

func (c *Sender) IsHandshakeCompleted() bool {
	return c.handshakeCompleted
}

func (c *Sender) IsTerminated() bool {
	return c.terminated
}

func (c *Sender) StreamCompleted() bool {
	return c.streamCompleted
}

func (c *Sender) EstablishConnection() {
	if c.terminated || c.connection.requested || c.handshakeStarted {
		return
	}
	c.validateSACKProfile()
	c.validateECNProfile()
	c.handshakeStarted = true
	c.connection.state = model.TCPSynSent
	// This profile starts at SEQ 0; SYN reserves the first position.
	c.nextSendSeq = 1
	c.transfer.startedAt = c.engine.Now()
	c.armReadTimer()
	c.synSendEvent = c.engine.ScheduleAfter(0, func() {
		c.synSendEvent = 0
		if !c.terminated && !c.connection.finSent && !c.handshakeCompleted {
			c.sendSyn()
		}
	})
}

func (c *Sender) finishStream() {
	if c.streamCompleted {
		return
	}
	c.streamCompleted = true
	c.recordTransferSuccess()
	if c.OnStreamComplete != nil {
		c.OnStreamComplete()
	}
	c.Close()
}

// CloseRequested also covers draining accepted data before FIN can be sent.
func (c *Sender) CloseRequested() bool { return c.connection.requested }

// SendSYNProbe emits one SYN without creating handshake state or retry timers.
// A workload can repeat this to generate the existing single-flow SYN flood.
func (c *Sender) SendSYNProbe() {
	c.validateECNProfile()
	c.emit(GeneratePacket(c.key, c.synFlags(), 0, 0, 0))
}

func (c *Sender) Close() {
	if c.terminated {
		return
	}
	if !c.handshakeCompleted {
		c.ShutdownWithReason(model.TransferClosedEarly)
		return
	}
	c.beginClosing()
	c.connection.requested = true
	c.startFINWhenDrained()
	c.queueSegment()
}

func (c *Sender) Shutdown() {
	c.ShutdownWithReason(model.TransferInterrupted)
}

// ShutdownWithReason annotates the existing shutdown without changing its timers or cleanup.
func (c *Sender) ShutdownWithReason(reason model.TransferFailureReason) {
	if c.terminated {
		return
	}
	c.recordTransferFailure(reason)
	if c.connection.outcome == "" {
		c.connection.outcome = model.CloseAborted
	}
	c.connection.state = model.TCPClosed
	if c.connection.started {
		c.connection.finishedAt = c.engine.Now()
	}
	c.terminated = true
	c.cancelEvent(&c.connection.sendEvent)
	c.cancelEvent(&c.connection.finTimer)
	c.cancelEvent(&c.connection.waitTimer)
	c.cancelEvent(&c.synSendEvent)
	c.cancelEvent(&c.synTimer)
	c.cancelEvent(&c.dataTimer)
	c.cancelEvent(&c.readTimer)
	c.cancelEvent(&c.sendEvent)
	c.cancelEvent(&c.ecn.holdEvent)
	c.pendingSegments.reset()
	c.sack.board.Reset()
	c.sack.active = false
	c.ecn.retryRanges = nil
	c.oldestSegmentRetransmissions = 0
	c.rttProbe = roundTripProbe{}
	c.congestion.clearLossTracking()
	if c.OnClose != nil {
		c.OnClose()
	}
}

func (c *Sender) emit(pkt model.Packet) {
	if !c.terminated && c.sender != nil {
		pkt.ReceiveWindowBytes = c.receiveWindowBytes
		c.sender(pkt)
	}
}

func (c *Sender) cancelEvent(id *engine.EventID) {
	if *id != 0 {
		c.engine.Cancel(*id)
		*id = 0
	}
}

func (c *Sender) GetState() model.ClientState {
	return model.ClientState{
		Connection:               c.GetConnectionState(),
		Key:                      c.key,
		RetrySyn:                 c.retrySyn,
		RetryData:                c.oldestSegmentRetransmissions,
		AcknowledgedPayloadBytes: c.acknowledgedPayloadBytes,
		Transfer:                 c.GetTransferState(),
		RTO:                      c.GetRTOState(),
		Congestion:               c.GetCongestionState(),
		ECN:                      c.GetECNState(),
	}
}
