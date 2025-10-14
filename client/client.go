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
	OutChannel chan model.Packet // packets to be sent to the LB
	InChannel  chan model.Packet // packets received from the LB

	lastSent           model.Packet       // last sent packet, used for retransmission if needed
	retrySyn           atomic.Uint32      // SYN retrans counter
	retryData          atomic.Uint32      // DATA retrans counter
	responseTimer      *time.Timer        // timer for waiting for a response from the server
	waitAck            chan struct{}      // unbuffered channel to sync sender with ACK
	handshakeDone      chan struct{}      // signals that 3-way handshake is complete
	ctx                context.Context    // context for managing timeouts
	cancel             context.CancelFunc // Function to cancel the context and signal shutdown
	closeOnce          sync.Once          // Ensures cleanup logic
	wg                 sync.WaitGroup
	handshakeCompleted atomic.Bool // to check if handshake is complete
	mu                 sync.Mutex  // mutex to protect shared state

}

func NewClient(
	srcIP string,
	srcPort int16,
	dstIP string,
	dstPort int16,
	proto model.Protocol,
	out chan model.Packet,
	in chan model.Packet) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		key:           GenerateClientKey(srcIP, srcPort, dstIP, dstPort, proto),
		OutChannel:    out,
		InChannel:     in,
		waitAck:       make(chan struct{}),
		handshakeDone: make(chan struct{}, 1),
		ctx:           ctx,
		cancel:        cancel,
		closeOnce:     sync.Once{},
		wg:            sync.WaitGroup{},
		mu:            sync.Mutex{},
	}
}

func (c *Client) Open() {
	synPacket := GeneratePacket(c.key, model.FlagSYN, 0, 0, 0)
	c.lastSent = synPacket
	select {
	case c.OutChannel <- synPacket:
	case <-c.ctx.Done():
		return
	}
	c.startTimer(model.SYN_TIMEOUT, c.retransmitSyn)
	select {
	case <-c.handshakeDone:
	case <-c.ctx.Done():
	}
}

func (c *Client) Key() model.ClientKey { return c.key }

func (c *Client) IsHandshakeCompleted() bool {
	return c.handshakeCompleted.Load()
}

func (c *Client) SendCompleteStream(dataSize int, delayBetween time.Duration) {
	c.wg.Add(1)
	defer c.wg.Done()

	packets := GeneratePackets(c.key, dataSize, delayBetween)
	for _, pkt := range packets {
		select {
		case <-c.ctx.Done():
			fmt.Printf("Client %v: Shutting down sender.\n", c.key)
			return
		default:
			// continue sending packets
		}

		time.Sleep(pkt.Delay)
		c.lastSent = pkt
		select {
		case c.OutChannel <- pkt:
		case <-c.ctx.Done():
			fmt.Printf("Client %v: Shutting down sender during send.\n", c.key)
			return
		}
		c.startTimer(model.SEGMENT_TIMEOUT, c.retransmitData)

		select {
		case <-c.waitAck:
			// ACK received, continue to next packet
		case <-c.ctx.Done():
			fmt.Printf("Client %v: Shutdown while waiting for ACK.\n", c.key)
			return
		}
	}
	c.Close()

}

func (c *Client) ExpectResponse() {
	c.wg.Add(1)
	go func() {
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

				mask := model.FlagSYN | model.FlagACK
				switch {
				case pkt.Flag&mask == (model.FlagSYN | model.FlagACK):
					c.mu.Lock()
					if c.responseTimer != nil {
						c.responseTimer.Stop() // got SYN-ACK
					}
					c.mu.Unlock()
					c.handshakeCompleted.Store(true)
					select {
					case c.handshakeDone <- struct{}{}:
					default:
					}

				case pkt.Flag&model.FlagACK != 0:
					c.mu.Lock()
					if c.responseTimer != nil {
						c.responseTimer.Stop()
					}
					c.mu.Unlock()

					c.retryData.Store(0) // reset data retransmission counter

					select {
					case c.waitAck <- struct{}{}:
					case <-c.ctx.Done():
					default:
					}

				case pkt.Flag&model.FlagFIN != 0:
					// Received FIN, initiate shutdown sequence.
					c.Shutdown()
					return
				}

			case <-timeoutContext.Done():
				fmt.Println("Client: response timeout, no packets received", c.key.SrcIP)
				c.Shutdown()
				return

			case <-c.ctx.Done():
				fmt.Printf("Client %v: Shutting down response handler.\n", c.key)
				return
			}
		}
	}()
}

func (c *Client) FloodSyn(count int, gap time.Duration) {
	go func() {
		for i := 0; i < count; i++ {
			select {
			case <-c.ctx.Done():
				return
			default:
				synPacket := GeneratePacket(c.key, model.FlagSYN, 0, 0, uint32(i))
				select {
				case c.OutChannel <- synPacket:
				case <-c.ctx.Done():
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
	case <-c.ctx.Done():
		return
	default:
	}

	if c.retrySyn.Load() >= model.MAX_RETRANSMISIONS {
		c.Shutdown()
		return
	}
	c.retrySyn.Add(1)
	retry := c.lastSent
	retry.Flag = model.FlagSYN

	select {
	case c.OutChannel <- retry:
	case <-c.ctx.Done():
		return
	}
	c.startTimer(model.SYN_TIMEOUT, c.retransmitSyn)
}

func (c *Client) retransmitData() {
	select {
	case <-c.ctx.Done():
		return
	default:
	}

	if c.retryData.Load() >= model.MAX_RETRANSMISIONS {
		c.Shutdown()
		return
	}

	c.retryData.Add(1)
	retry := c.lastSent
	retry.Flag = model.FlagPSH

	select {
	case c.OutChannel <- retry:
	case <-c.ctx.Done():
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
		case <-c.ctx.Done():
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
		case <-c.ctx.Done():
		}
	})
}

func (c *Client) Shutdown() {
	c.cancel()

	c.wg.Wait()

	c.closeOnce.Do(func() {
		if c.responseTimer != nil {
			c.responseTimer.Stop()
		}
		close(c.waitAck)
		close(c.handshakeDone)
	})
}
