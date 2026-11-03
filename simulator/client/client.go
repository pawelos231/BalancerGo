package client

import (
	"time"

	"load-balancer/engine"
	"load-balancer/model"
	"load-balancer/protocols/tcp"
)

// Client owns one workload and delegates its connection to the TCP transport.
// It schedules traffic generation, while TCP owns packets, windows and retries.
type Client struct {
	engine     *engine.Engine
	transport  *tcp.Sender
	floodEvent engine.EventID

	OnClose               func()
	OnStreamComplete      func()
	OnPayloadAcknowledged func(int)
}

func NewClient(
	e *engine.Engine,
	srcIP string,
	srcPort uint16,
	dstIP string,
	dstPort uint16,
	protocol model.Protocol,
) *Client {
	key := GenerateClientKey(srcIP, srcPort, dstIP, dstPort, protocol)
	c := &Client{engine: e, transport: tcp.NewSender(e, key)}
	c.transport.OnClose = func() {
		c.stopFlood()
		if c.OnClose != nil {
			c.OnClose()
		}
	}
	c.transport.OnStreamComplete = func() {
		if c.OnStreamComplete != nil {
			c.OnStreamComplete()
		}
		c.stopFlood()
	}
	c.transport.OnPayloadAcknowledged = func(bytes int) {
		if c.OnPayloadAcknowledged != nil {
			c.OnPayloadAcknowledged(bytes)
		}
	}
	return c
}

// SendCompleteStream defines a finite workload; TCP pulls from it when ready.
// Its pause starts after each actual new send, including time spent waiting for ACKs.
func (c *Client) SendCompleteStream(bytes int, gap time.Duration) {
	source := newFiniteStream(bytes, gap, c.engine.EndTime())
	c.transport.SetSource(source)
}

// FloodSyn generates repeated SYNs for one flow without creating extra TCP connections.
func (c *Client) FloodSyn(count int, gap time.Duration) {
	if count < 0 || gap < 0 {
		panic("client: flood count and gap must be nonnegative")
	}
	c.stopFlood()
	if c.IsTerminated() || c.transport.CloseRequested() || count == 0 {
		return
	}
	var sendNext func(int)
	sendNext = func(index int) {
		c.floodEvent = 0
		if c.IsTerminated() || c.transport.CloseRequested() {
			return
		}
		c.transport.SendSYNProbe()
		if !c.IsTerminated() && !c.transport.CloseRequested() && index+1 < count {
			c.floodEvent = c.engine.ScheduleAfter(gap, func() { sendNext(index + 1) })
		}
	}
	c.floodEvent = c.engine.ScheduleAfter(0, func() { sendNext(0) })
}

// Close drains accepted payload before TCP starts its FIN exchange.
func (c *Client) Close() {
	c.stopFlood()
	c.transport.Close()
}

func (c *Client) Shutdown() { c.transport.Shutdown() }

func (c *Client) ShutdownWithReason(reason model.TransferFailureReason) {
	c.transport.ShutdownWithReason(reason)
}

// stopFlood cancels generation only; TCP cancels its own transport timers.
func (c *Client) stopFlood() {
	if c.floodEvent != 0 {
		c.engine.Cancel(c.floodEvent)
		c.floodEvent = 0
	}
}
