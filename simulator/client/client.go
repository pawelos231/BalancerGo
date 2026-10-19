package client

import (
	"load-balancer/engine"
	"load-balancer/model"
	"time"
)

// Client is the existing stop-and-wait TCP-like model driven by virtual-time
// events. PlaceInStream identifies segments; it is not a TCP byte sequence.
// All calls and callbacks belong to the engine's single execution thread.
type Client struct {
	key    model.ClientKey
	engine *engine.Engine
	sender func(model.Packet)

	closed             bool
	closing            bool
	handshakeStarted   bool
	handshakeCompleted bool
	retrySyn           uint8
	retryData          uint8
	lastSent           model.Packet
	waitingForAck      bool

	streamRequested bool
	streamCompleted bool
	remainingBytes  int
	segmentIndex    uint32
	gap             time.Duration

	synSendEvent engine.EventID
	synTimer     engine.EventID
	dataTimer    engine.EventID
	readTimer    engine.EventID
	sendEvent    engine.EventID
	floodEvent   engine.EventID

	OnClose          func()
	OnStreamComplete func()
}

func NewClient(e *engine.Engine, srcIP string, srcPort uint16, dstIP string, dstPort uint16, proto model.Protocol) *Client {
	if e == nil {
		panic("client: nil engine")
	}
	if proto != model.TCP {
		panic("client: only the simplified TCP model is implemented")
	}
	return &Client{engine: e, key: GenerateClientKey(srcIP, srcPort, dstIP, dstPort, proto)}
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

func (c *Client) IsClosed() bool {
	return c.closed
}

func (c *Client) StreamCompleted() bool {
	return c.streamCompleted
}

func (c *Client) EstablishConnection() {
	if c.closed || c.closing || c.handshakeStarted {
		return
	}
	c.handshakeStarted = true
	c.armReadTimer()
	c.synSendEvent = c.engine.ScheduleAfter(0, func() {
		c.synSendEvent = 0
		if !c.closed && !c.closing && !c.handshakeCompleted {
			c.sendSyn()
		}
	})
}

func (c *Client) sendSyn() {
	c.cancelEvent(&c.synTimer)
	c.synTimer = c.engine.ScheduleAfter(model.SYN_TIMEOUT, c.retransmitSyn)
	c.emit(GeneratePacket(c.key, model.FlagSYN, 0, 0))
}

func (c *Client) retransmitSyn() {
	c.synTimer = 0
	if c.closed || c.closing || c.handshakeCompleted {
		return
	}
	if c.retrySyn >= model.MAX_RETRANSMISIONS {
		c.Shutdown()
		return
	}
	c.retrySyn++
	c.sendSyn()
}

// SendCompleteStream may be called before EstablishConnection. A client owns
// one finite stream; data starts only after the handshake has completed.
func (c *Client) SendCompleteStream(dataSize int, gap time.Duration) {
	if dataSize < 0 || gap < 0 {
		panic("client: stream size and gap must be nonnegative")
	}
	if c.closed || c.closing || c.streamRequested {
		return
	}
	c.streamRequested = true
	c.remainingBytes = dataSize
	c.gap = gap
	if c.handshakeCompleted {
		c.queueSegment()
	}
}

func (c *Client) queueSegment() {
	if c.closed || c.closing || !c.handshakeCompleted || !c.streamRequested || c.waitingForAck || c.sendEvent != 0 {
		return
	}
	if c.remainingBytes == 0 {
		c.finishStream()
		return
	}
	c.sendEvent = c.engine.ScheduleAfter(c.gap, func() {
		c.sendEvent = 0
		if c.closed || c.closing {
			return
		}
		payloadBytes := min(c.remainingBytes, model.SEGMENT_SIZE)
		c.lastSent = GeneratePacket(c.key, model.FlagPSH, payloadBytes, c.segmentIndex)
		c.waitingForAck = true
		c.retryData = 0
		c.armDataTimer()
		c.emit(c.lastSent)
	})
}

func (c *Client) armDataTimer() {
	c.cancelEvent(&c.dataTimer)
	c.dataTimer = c.engine.ScheduleAfter(model.SEGMENT_TIMEOUT, c.retransmitData)
}

func (c *Client) retransmitData() {
	c.dataTimer = 0
	if c.closed || c.closing || !c.waitingForAck {
		return
	}
	if c.retryData >= model.MAX_RETRANSMISIONS {
		c.Shutdown()
		return
	}
	c.retryData++
	c.armDataTimer()
	c.emit(c.lastSent)
}

func (c *Client) HandlePacket(pkt model.Packet) {
	if c.closed || pkt.Key != c.key {
		return
	}
	c.armReadTimer()
	// A FIN|ACK must close the client rather than acknowledge another segment.
	if pkt.Flag&model.FlagFIN != 0 {
		c.Shutdown()
		return
	}
	if c.closing {
		return
	}
	if pkt.Flag&model.FlagSYN != 0 && pkt.Flag&model.FlagACK != 0 {
		if !c.handshakeStarted {
			return // A same-flow SYN flood deliberately never completes a handshake.
		}
		c.cancelEvent(&c.synTimer)
		c.retrySyn = 0
		c.handshakeCompleted = true
		c.emit(GeneratePacket(c.key, model.FlagACK, 0, 1))
		c.queueSegment()
		return
	}
	if pkt.Flag&model.FlagACK != 0 && c.waitingForAck && pkt.PlaceInStream == c.lastSent.PlaceInStream+1 {
		c.cancelEvent(&c.dataTimer)
		c.waitingForAck = false
		c.retryData = 0
		c.remainingBytes -= c.lastSent.Size
		c.segmentIndex++
		c.queueSegment()
	}
}

func (c *Client) finishStream() {
	if c.streamCompleted {
		return
	}
	c.streamCompleted = true
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
	if c.closed || c.closing || count == 0 {
		return
	}
	var sendNext func(int)
	sendNext = func(index int) {
		c.floodEvent = 0
		if c.closed || c.closing {
			return
		}
		c.emit(GeneratePacket(c.key, model.FlagSYN, 0, uint32(index)))
		if !c.closed && !c.closing && index+1 < count {
			c.floodEvent = c.engine.ScheduleAfter(gap, func() { sendNext(index + 1) })
		}
	}
	c.floodEvent = c.engine.ScheduleAfter(0, func() { sendNext(0) })
}

func (c *Client) armReadTimer() {
	c.cancelEvent(&c.readTimer)
	c.readTimer = c.engine.ScheduleAfter(model.READ_DEADLINE, func() {
		c.readTimer = 0
		if !c.closed {
			c.Shutdown()
		}
	})
}

func (c *Client) Close() {
	if c.closed || c.closing {
		return
	}
	if !c.handshakeCompleted {
		c.Shutdown()
		return
	}
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
		if !c.closed {
			c.emit(GeneratePacket(c.key, model.FlagFIN, 0, c.segmentIndex))
		}
	})
}

func (c *Client) Shutdown() {
	if c.closed {
		return
	}
	c.closed = true
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
	if !c.closed && c.sender != nil {
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
	return model.ClientState{Key: c.key, RetrySyn: c.retrySyn, RetryData: c.retryData}
}
