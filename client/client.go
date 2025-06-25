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
	retrySyn           uint8              // SYN retrans counter
	retryData          uint8              // DATA retrans counter
	responseTimer      *time.Timer        // timer for waiting for a response from the server
	waitAck            chan struct{}      // unbuffered channel to sync sender with ACK
	handshakeDone      chan struct{}      // signals that 3-way handshake is complete
	ctx                context.Context    // context for managing timeouts
	cancel             context.CancelFunc // Function to cancel the context and signal shutdown
	closeOnce          sync.Once          // Ensures cleanup logic
	wg                 sync.WaitGroup
	handshakeCompleted atomic.Bool // to check if handshake is complete

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
	}
}

func (c *Client) Open() {
	// Initializes the connection by sending a SYN packet
	synPacket := GeneratePacket(c.key, model.FlagSYN, 0, 0, 0)
	c.lastSent = synPacket
	select {
	case c.OutChannel <- synPacket:
	case <-c.ctx.Done():
		return
	}
	c.startTimer(model.SYN_TIMEOUT, c.retransmitSyn) // wait for SYN-ACK
	select {
	case <-c.handshakeDone: // wait for SYN-ACK to arrive
	case <-c.ctx.Done():
	}
}

func (c *Client) Key() model.ClientKey { return c.key }

// IsHandshakeCompleted returns true if the 3-way handshake has been completed.
func (c *Client) IsHandshakeCompleted() bool {
	return c.handshakeCompleted.Load()
}

// SendStream sends dataSize bytes in segSize chunks; each chunk waits for ACK.
// so it will send dataSize / SEGMENT_SIZE packets, each of size SEGMENT_SIZE.
func (c *Client) SendCompleteStream(dataSize int, delayBetween time.Duration) {
	c.wg.Add(1)
	defer c.wg.Done()

	packets := GenerateStream(c.key, dataSize, delayBetween)
	for _, pkt := range packets {
		select {
		case <-c.ctx.Done():
			fmt.Printf("Client %v: Shutting down sender.\n", c.key)
			return // exit if the context is done
		default:
			// continue sending packets
		}

		time.Sleep(pkt.Delay) // simulate network gap
		c.lastSent = pkt      // update last sent packet
		select {
		case c.OutChannel <- pkt: // send packet to the LB
		case <-c.ctx.Done():
			fmt.Printf("Client %v: Shutting down sender during send.\n", c.key)
			return
		}
		c.startTimer(model.SEGMENT_TIMEOUT, c.retransmitData) // start timer for ACK

		// Wait for ACK or shutdown
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
	c.wg.Add(1) // increment the wait group counter to track the response handler goroutine
	go func() {
		defer c.wg.Done()
		var (
			timeoutContext context.Context
			cancel         context.CancelFunc
		)
		defer func() {
			if cancel != nil {
				cancel() // ensure the context is cancelled to avoid leaks
			}
		}()

		resetCtx := func() {
			if cancel != nil {
				cancel() // cancel the previous context if it exists
			}
			timeoutContext, cancel = context.WithTimeout(context.Background(), model.READ_DEADLINE)
		}
		resetCtx() // initialize the context

		for {
			select {
			case pkt, ok := <-c.InChannel:
				if !ok {
					// InChannel was closed by its owner.
					return
				}

				resetCtx() // reset context for each new packet
				if pkt.Key != c.key {
					continue
				}

				mask := model.FlagSYN | model.FlagACK
				switch {
				// Checks if both SYN and ACK bits are set in pkt.Flag
				// (ex: packet is a SYN-ACK during the 3-way handshake).
				case pkt.Flag&mask == (model.FlagSYN | model.FlagACK):
					if c.responseTimer != nil {
						c.responseTimer.Stop() // got SYN-ACK
					}
					c.handshakeCompleted.Store(true)
					select {
					case c.handshakeDone <- struct{}{}:
					default:
						// ensure that we don't block if the channel is already full
					}

				// if the received packet is of type ACK, reset the retry counters
				case pkt.Flag&model.FlagACK != 0:
					// ACK of data
					if c.responseTimer != nil {
						c.responseTimer.Stop()
					}
					c.retryData = 0
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

			// if the (main) context is done, exit the loop
			case <-c.ctx.Done():
				fmt.Printf("Client %v: Shutting down response handler.\n", c.key)
				return
			}
		}
	}()
}

// FloodSyn sends a specified number of SYN packets to the server with a defined gap between each packet.
func (c *Client) FloodSyn(count int, gap time.Duration) {
	go func() {
		for i := 0; i < count; i++ {
			select {
			case <-c.ctx.Done():
				return // exit if the context is done
			default:
				synPacket := GeneratePacket(c.key, model.FlagSYN, 0, 0, uint32(i)) // generate a SYN packet with the current index
				select {
				case c.OutChannel <- synPacket: // send SYN packet to the LB
				case <-c.ctx.Done():
					return
				}
				time.Sleep(gap) // wait for the specified gap before sending the next packet
			}
		}
	}()
}

// It is a graceful
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		finPacket := GeneratePacket(c.key, model.FlagFIN, 0, 0, 2<<30)
		select {
		case c.OutChannel <- finPacket:
		case <-c.ctx.Done():
		}
	})
	// After sending FIN, we let the Shutdown sequence handle the rest.
}

// Shutdown stops the client by closing the channels and stopping the response timer. It is abdrupt, almost as if power was cut off.
func (c *Client) Shutdown() {
	// Signal all goroutines to stop.
	c.cancel()

	// Wait for all tracked goroutines (SendCompleteStream, ExpectResponse) to finish.
	c.wg.Wait()

	// Once all goroutines are done, safely clean up shared resources.
	c.closeOnce.Do(func() {
		if c.responseTimer != nil {
			c.responseTimer.Stop()
		}
		// Closing internal channels now is safe, as no goroutine will use them.
		close(c.waitAck)
		close(c.handshakeDone)
	})
}

func (c *Client) GetState() model.ClientState {
	return model.ClientState{
		Key:       c.key,
		RetrySyn:  c.retrySyn,
		RetryData: c.retryData,
	}
}

func (c *Client) retransmitSyn() {
	select {
	case <-c.ctx.Done():
		return
	default:
	}

	if c.retrySyn >= model.MAX_RETRANSMISIONS {
		c.Shutdown()
		return
	}
	c.retrySyn++
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

	if c.retryData >= model.MAX_RETRANSMISIONS {
		c.Shutdown()
		return
	}
	c.retryData++
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
