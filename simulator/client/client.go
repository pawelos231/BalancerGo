package client

import (
	"time"

	"load-balancer/engine"
	"load-balancer/model"
)

// Client sends one segment at a time using logical byte SEQ/ACK positions.
// All calls and callbacks belong to the virtual-time engine's owner.
type Client struct {
	key    model.ClientKey
	engine *engine.Engine
	sender func(model.Packet)

	terminated         bool
	closing            bool
	handshakeStarted   bool
	handshakeCompleted bool
	retrySyn           uint8
	lastSent           model.Packet
	waitingForAck      bool

	// Counts retransmissions of the current segment and resets after its ACK.
	currentSegmentRetransmissions uint8

	nextSendSeq      uint64
	oldestUnackedSeq uint64
	nextReceiveSeq   uint64
	peerInitialSeq   uint64

	// New data must stay below the byte position advertised by the backend.
	peerWindowEnd uint64

	// Advertised on our packets; this profile receives no backend payload.
	receiveWindowBytes uint16

	streamRequested                bool
	streamCompleted                bool
	remainingPayloadBytesToConfirm int
	gap                            time.Duration

	acknowledgedPayloadBytes uint64
	transfer                 transferMetrics

	synSendEvent engine.EventID
	synTimer     engine.EventID
	dataTimer    engine.EventID
	readTimer    engine.EventID
	sendEvent    engine.EventID
	floodEvent   engine.EventID

	OnClose          func()
	OnStreamComplete func()

	// Tells the simulation how many new payload bytes the client has ACK-confirmed.
	// Repeated ACKs for the same segment do not count those bytes again.
	OnPayloadAcknowledged func(int)
}

func NewClient(
	e *engine.Engine,
	srcIP string,
	srcPort uint16,
	dstIP string,
	dstPort uint16,
	proto model.Protocol,
) *Client {
	if e == nil {
		panic("client: nil engine")
	}
	if proto != model.TCP {
		panic("client: only the simplified TCP model is implemented")
	}
	return &Client{
		engine:             e,
		key:                GenerateClientKey(srcIP, srcPort, dstIP, dstPort, proto),
		receiveWindowBytes: model.DefaultReceiveBufferBytes,
		transfer:           transferMetrics{status: model.TransferInProgress},
	}
}

// SetReceiveBufferBytes sets the window advertised when this connection starts.
func (c *Client) SetReceiveBufferBytes(capacity int) {
	if capacity < 1 || capacity > model.DefaultReceiveBufferBytes {
		panic("client: receive buffer must be in 1..65535 bytes")
	}
	if c.handshakeStarted || c.terminated {
		panic("client: receive buffer must be set before the connection starts")
	}

	c.receiveWindowBytes = uint16(capacity)
}

func (c *Client) SetSender(sender func(model.Packet)) {
	c.sender = sender
}

func (c *Client) Key() model.ClientKey {
	return c.key
}

func (c *Client) IsHandshakeCompleted() bool {
	return c.handshakeCompleted
}

func (c *Client) IsTerminated() bool {
	return c.terminated
}

func (c *Client) StreamCompleted() bool {
	return c.streamCompleted
}

func (c *Client) EstablishConnection() {
	if c.terminated || c.closing || c.handshakeStarted {
		return
	}
	c.handshakeStarted = true
	// This profile starts at SEQ 0; SYN reserves the first position.
	c.nextSendSeq = 1
	c.transfer.startedAt = c.engine.Now()
	c.armReadTimer()
	c.synSendEvent = c.engine.ScheduleAfter(0, func() {
		c.synSendEvent = 0
		if !c.terminated && !c.closing && !c.handshakeCompleted {
			c.sendSyn()
		}
	})
}

func (c *Client) sendSyn() {
	c.cancelEvent(&c.synTimer)
	c.synTimer = c.engine.ScheduleAfter(model.SYN_TIMEOUT, c.retransmitSyn)
	c.emit(GeneratePacket(c.key, model.FlagSYN, 0, 0, 0))
}

func (c *Client) retransmitSyn() {
	c.synTimer = 0
	if c.terminated || c.closing || c.handshakeCompleted {
		return
	}
	if c.retrySyn >= model.MAX_RETRANSMISIONS {
		c.ShutdownWithReason(model.TransferSynRetryLimit)
		return
	}
	c.retrySyn++
	c.transfer.synRetransmissions++
	c.sendSyn()
}

func (c *Client) finishStream() {
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

// FloodSyn retains the previous deliberately limited attack generator: all
// repeated SYNs share one flow key. It does not model distributed SYN flooding.
func (c *Client) FloodSyn(count int, gap time.Duration) {
	if count < 0 || gap < 0 {
		panic("client: flood count and gap must be nonnegative")
	}
	c.cancelEvent(&c.floodEvent)
	if c.terminated || c.closing || count == 0 {
		return
	}
	var sendNext func(int)
	sendNext = func(index int) {
		c.floodEvent = 0
		if c.terminated || c.closing {
			return
		}
		c.emit(GeneratePacket(c.key, model.FlagSYN, 0, 0, 0))
		if !c.terminated && !c.closing && index+1 < count {
			c.floodEvent = c.engine.ScheduleAfter(gap, func() { sendNext(index + 1) })
		}
	}
	c.floodEvent = c.engine.ScheduleAfter(0, func() { sendNext(0) })
}

func (c *Client) armReadTimer() {
	c.cancelEvent(&c.readTimer)
	c.readTimer = c.engine.ScheduleAfter(model.READ_DEADLINE, func() {
		c.readTimer = 0
		if !c.terminated {
			c.ShutdownWithReason(model.TransferReadTimeout)
		}
	})
}

func (c *Client) Close() {
	if c.terminated || c.closing {
		return
	}
	if !c.handshakeCompleted {
		c.ShutdownWithReason(model.TransferClosedEarly)
		return
	}
	c.recordTransferFailure(model.TransferClosedEarly)
	c.closing = true
	c.cancelEvent(&c.synSendEvent)
	c.cancelEvent(&c.synTimer)
	c.cancelEvent(&c.dataTimer)
	c.cancelEvent(&c.sendEvent)
	c.cancelEvent(&c.floodEvent)
	c.waitingForAck = false
	c.armReadTimer()
	c.sendEvent = c.engine.ScheduleAfter(0, func() {
		c.sendEvent = 0
		if !c.terminated {
			packet := GeneratePacket(
				c.key,
				model.FlagFIN|model.FlagACK,
				0,
				c.nextSendSeq,
				c.nextReceiveSeq,
			)
			c.nextSendSeq++
			c.emit(packet)
		}
	})
}

func (c *Client) Shutdown() {
	c.ShutdownWithReason(model.TransferInterrupted)
}

// ShutdownWithReason annotates the existing shutdown without changing its timers or cleanup.
func (c *Client) ShutdownWithReason(reason model.TransferFailureReason) {
	if c.terminated {
		return
	}
	c.recordTransferFailure(reason)
	c.terminated = true
	c.cancelEvent(&c.synSendEvent)
	c.cancelEvent(&c.synTimer)
	c.cancelEvent(&c.dataTimer)
	c.cancelEvent(&c.readTimer)
	c.cancelEvent(&c.sendEvent)
	c.cancelEvent(&c.floodEvent)
	if c.OnClose != nil {
		c.OnClose()
	}
}

func (c *Client) emit(pkt model.Packet) {
	if !c.terminated && c.sender != nil {
		pkt.ReceiveWindowBytes = c.receiveWindowBytes
		c.sender(pkt)
	}
}

func (c *Client) cancelEvent(id *engine.EventID) {
	if *id != 0 {
		c.engine.Cancel(*id)
		*id = 0
	}
}

func (c *Client) GetState() model.ClientState {
	return model.ClientState{
		Key:                      c.key,
		RetrySyn:                 c.retrySyn,
		RetryData:                c.currentSegmentRetransmissions,
		AcknowledgedPayloadBytes: c.acknowledgedPayloadBytes,
		Transfer:                 c.GetTransferState(),
	}
}
