package client

import (
	"context"
	"fmt"
	"load-balancer/model"
	"sync"
	"sync/atomic"
	"time"
)

type Client struct {
	key        model.ClientKey
	OutChannel chan *model.Packet // packets to be sent to the LB
	InChannel  chan *model.Packet // packets received from the LB

	lastSent           *model.Packet
	retrySyn           atomic.Uint32
	retryData          atomic.Uint32
	responseTimer      *time.Timer
	waitAck            chan struct{}
	closeSignal        chan struct{} // used in load balancer to signal client closure, cleans up resources on the LB side
	Ctx                context.Context
	cancel             context.CancelFunc
	closeOnce          sync.Once
	wg                 sync.WaitGroup
	handshakeCompleted atomic.Bool
	mu                 sync.Mutex
}

func NewClient(
	srcIP string,
	srcPort int16,
	dstIP string,
	dstPort int16,
	proto model.Protocol,
	out chan *model.Packet,
	in chan *model.Packet) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		key:         GenerateClientKey(srcIP, srcPort, dstIP, dstPort, proto),
		OutChannel:  out,
		InChannel:   in,
		Ctx:         ctx,
		cancel:      cancel,
		closeOnce:   sync.Once{},
		wg:          sync.WaitGroup{},
		mu:          sync.Mutex{},
		waitAck:     make(chan struct{}),
		closeSignal: make(chan struct{}),
	}
}

func (c *Client) EstablishConnection() {
	synPacket := GeneratePacket(c.key, model.FlagSYN, 0, 0, 0)
	c.lastSent = synPacket
	timer := time.NewTimer(model.READ_DEADLINE)

	select {
	case c.OutChannel <- synPacket:
	case <-c.Ctx.Done():
		return
	}

	// we wait for SYN-ACK or timeout
	select {
	case pkt, ok := <-c.InChannel:
		if !ok {
			return
		}
		if pkt.Key != c.key {
			return
		}

		if !(pkt.Flag&model.FlagSYN != 0 && pkt.Flag&model.FlagACK != 0) {
			fmt.Println("initialization went wrong")
			return
		}
	case <-timer.C:
		fmt.Println("Client: SYN-ACK timeout, shutting down", c.key.SrcIP)
		c.Shutdown()
		return
	}

	ackPacket := GeneratePacket(c.key, model.FlagACK, 0, 0, 1)
	c.lastSent = ackPacket

	select {
	case c.OutChannel <- ackPacket:
	case <-c.Ctx.Done():
		return
	}
	fmt.Println(c.key, "INITIALIZED THREEWAY HANDSHAKE")
	c.handshakeCompleted.Store(true)
}

func (c *Client) Key() model.ClientKey { return c.key }

func (c *Client) IsHandshakeCompleted() bool {
	return c.handshakeCompleted.Load()
}

// normally we would want implement sliding window, but for simplicity, we send one packet at a time
// also normally we would want to randomize delay a bit to simulate real network conditions
// also normally we would want to generate the packets in a streaming manner rather than all at once
// all of this concerns are (FOR NOW) ignored for simplicity
func (c *Client) SendCompleteStream(dataSize int, delay time.Duration) {
	c.wg.Add(1)
	defer c.wg.Done()
	var counter uint32
	for {
		pkt := GeneratePacket(c.key, model.FlagPSH, dataSize, delay, counter)
		counter++
		if counter > uint32(dataSize/model.SEGMENT_SIZE) {
			fmt.Println("Client: completed sending data stream", c.key.SrcIP)
			return
		}

		time.Sleep(pkt.Delay)
		c.lastSent = pkt

		select {
		case c.OutChannel <- pkt:
		case <-c.Ctx.Done():
			return
		}

		c.startTimer(model.SEGMENT_TIMEOUT, c.retransmitData)

		select {
		case <-c.waitAck: // ACK = generate next
			continue
		case <-c.Ctx.Done():
			return
		}
	}

}

func (c *Client) ListenForResponse() {
	c.wg.Add(1)
	defer c.wg.Done()

	var (
		timeoutContext context.Context
		cancel         context.CancelFunc
	)
	defer func() {
		if cancel != nil {
			cancel()
		}
	}()

	resetCtx := func() {
		if cancel != nil {
			cancel()
		}
		timeoutContext, cancel = context.WithTimeout(context.Background(), model.READ_DEADLINE)
	}
	resetCtx()

	for {
		select {
		case pkt, ok := <-c.InChannel:
			if !ok {
				return
			}

			resetCtx()
			if pkt.Key != c.key {
				continue
			}

			switch {

			case pkt.Flag&model.FlagACK != 0:
				c.mu.Lock()
				if c.responseTimer != nil {
					c.responseTimer.Stop()
				}
				c.mu.Unlock()

				c.retryData.Store(0) // reset data retransmission counter

				select {
				case c.waitAck <- struct{}{}:
				case <-c.Ctx.Done():
				default:
				}

			case pkt.Flag&model.FlagFIN != 0:
				// Received FIN, initiate shutdown sequence.
				fmt.Println("Client: received FIN, shutting down", c.key.SrcIP)
				c.Shutdown()
				return
			}

		case <-timeoutContext.Done():
			fmt.Println("Client: response timeout, no packets received", c.key.SrcIP)
			c.Shutdown()
			return

		case <-c.Ctx.Done():
			fmt.Printf("Client %v: Shutting down response handler.\n", c.key)
			return
		}
	}

}

func (c *Client) FloodSyn(count int, gap time.Duration) {
	go func() {
		for i := 0; i < count; i++ {
			select {
			case <-c.Ctx.Done():
				return
			default:
				synPacket := GeneratePacket(c.key, model.FlagSYN, 0, 0, uint32(i))
				select {
				case c.OutChannel <- synPacket:
				case <-c.Ctx.Done():
					return
				}
				time.Sleep(gap)
			}
		}
	}()
}

func (c *Client) GetState() model.ClientState {
	return model.ClientState{
		Key:       c.key,
		RetrySyn:  uint8(c.retrySyn.Load()),
		RetryData: uint8(c.retryData.Load()),
	}
}

func (c *Client) retransmitSyn() {
	select {
	case <-c.Ctx.Done():
		return
	default:
	}

	if c.retrySyn.Load() >= model.MAX_RETRANSMISIONS {
		fmt.Println("Client: Max SYN retransmissions reached, shutting down", c.key.SrcIP)
		c.Shutdown()
		return
	}
	c.retrySyn.Add(1)

	retry := new(model.Packet)
	retry.Key = c.lastSent.Key
	retry.Size = c.lastSent.Size
	retry.PlaceInStream = c.lastSent.PlaceInStream
	retry.Delay = c.lastSent.Delay
	retry.Flag = model.FlagSYN

	select {
	case c.OutChannel <- retry:
	case <-c.Ctx.Done():
		return
	}
	c.startTimer(model.SYN_TIMEOUT, c.retransmitSyn)
}

func (c *Client) retransmitData() {
	select {
	case <-c.Ctx.Done():
		return
	default:
	}

	if c.retryData.Load() >= model.MAX_RETRANSMISIONS {
		fmt.Println("Client: Max data retransmissions reached, shutting down", c.key.SrcIP)
		c.Shutdown()
		return
	}

	c.retryData.Add(1)

	retry := c.lastSent

	select {
	case c.OutChannel <- retry:
	case <-c.Ctx.Done():
		return
	}
	c.startTimer(model.SEGMENT_TIMEOUT, c.retransmitData)
}

func (c *Client) startTimer(d time.Duration, fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.responseTimer != nil {
		c.responseTimer.Stop()
	}

	c.responseTimer = time.AfterFunc(d, func() {
		select {
		case <-c.Ctx.Done():
			// Timer is irrelevant if client is already shutting down.
			return
		default:
			fn() // call the retransmission function
		}
	})
}

func (c *Client) Close() {
	c.closeOnce.Do(func() {
		finPacket := GeneratePacket(c.key, model.FlagFIN, 0, 0, 2<<30)
		select {
		case c.OutChannel <- finPacket:
		case <-c.Ctx.Done():
		}
	})
}

func (c *Client) Shutdown() {
	// fmt.Println("SHUTTING DOWN - cleaning resources")
	c.cancel()
	c.wg.Wait()

	c.closeOnce.Do(func() {
		if c.responseTimer != nil {
			c.responseTimer.Stop()
		}
		close(c.waitAck)
	})
}
