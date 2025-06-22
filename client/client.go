package client

import (
	"context"
	"fmt"
	"load-balancer/model"
	"time"
)

type Client struct {
	key        model.ClientKey
	OutChannel chan model.Packet // packets to be sent to the LB
	InChannel  chan model.Packet // packets received from the LB

	lastSent      model.Packet  // last sent packet, used for retransmission if needed
	retrySyn      uint8         // SYN retrans counter
	retryData     uint8         // DATA retrans counter
	responseTimer *time.Timer   // timer for waiting for a response from the server
	waitAck       chan struct{} // unbuffered channel to sync sender with ACK
	handshakeDone chan struct{} // signals that 3-way handshake is complete
}

func NewClient(
	srcIP string,
	srcPort int16,
	dstIP string,
	dstPort int16,
	proto model.Protocol,
	out chan model.Packet,
	in chan model.Packet) *Client {

	return &Client{
		key:           GenerateClientKey(srcIP, srcPort, dstIP, dstPort, proto),
		OutChannel:    out,
		InChannel:     in,
		waitAck:       make(chan struct{}),
		handshakeDone: make(chan struct{}, 1),
	}
}

func (c *Client) Open() {
	// Initializes the connection by sending a SYN packet
	synPacket := GeneratePacket(c.key, model.FlagSYN, 0, 0, 0)
	c.lastSent = synPacket
	c.OutChannel <- synPacket
	c.startTimer(model.SYN_TIMEOUT, c.retransmitSyn) // wait for SYN-ACK
	<-c.handshakeDone                                // wait for SYN-ACK to arrive
}

func (c *Client) Close() {
	finPacket := GeneratePacket(c.key, model.FlagFIN, 0, 0, 2<<30) // 2<<30 is a complete SCANDAL XD but it nicely shows the FIN packet in the stream
	c.OutChannel <- finPacket                                      // send the FIN packet to the server
	if c.responseTimer != nil {
		c.responseTimer.Stop()
	}
}

func (c *Client) Key() model.ClientKey { return c.key }

// SendStream sends dataSize bytes in segSize chunks; each chunk waits for ACK.
// so it will send dataSize / SEGMENT_SIZE packets, each of size SEGMENT_SIZE.
func (c *Client) SendCompleteStream(dataSize int, delayBetween time.Duration) {
	packets := GenerateStream(c.key, dataSize, delayBetween)
	for _, pkt := range packets {
		time.Sleep(pkt.Delay)                                 // simulate network gap
		c.lastSent = pkt                                      // update last sent packet
		c.OutChannel <- pkt                                   // send packet to the LB
		c.startTimer(model.SEGMENT_TIMEOUT, c.retransmitData) // start timer for ACK
		<-c.waitAck                                           // block until ACK received
	}
	c.Close()

}

func (c *Client) retransmitSyn() {
	if c.retrySyn >= model.MAX_RETRANSMISIONS {
		return
	}
	c.retrySyn++
	retry := c.lastSent
	retry.Flag = model.FlagSYN
	c.OutChannel <- retry
	c.startTimer(model.SYN_TIMEOUT, c.retransmitSyn)
}

func (c *Client) retransmitData() {
	if c.retryData >= model.MAX_RETRANSMISIONS {
		return
	}
	c.retryData++
	retry := c.lastSent
	retry.Flag = model.FlagPSH
	c.OutChannel <- retry
	c.startTimer(model.SEGMENT_TIMEOUT, c.retransmitData)
}

func (c *Client) ExpectResponse() {
	go func() {
		var (
			ctx    context.Context
			cancel context.CancelFunc
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
			ctx, cancel = context.WithTimeout(context.Background(), model.READ_DEADLINE)
		}
		resetCtx() // initialize the context

		for {
			select {
			case pkt := <-c.InChannel:
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
					c.handshakeDone <- struct{}{} // unblock sender to start data stream

				// if the received packet is of type ACK, reset the retry counters
				case pkt.Flag&model.FlagACK != 0:
					// ACK of data
					if c.responseTimer != nil {
						c.responseTimer.Stop()
					}
					c.retryData = 0
					c.waitAck <- struct{}{} // let sender push next chunk

				case pkt.Flag&model.FlagFIN != 0:
					close(c.waitAck) // FIN packet received, close the wait channel
					return
				}

			// if the context has been cancelled, exit the loop
			case <-ctx.Done():
				fmt.Println("Client: response timeout, no packets received")
				return
			}
		}
	}()
}

// FloodSyn sends a specified number of SYN packets to the server with a defined gap between each packet.
func (c *Client) FloodSyn(count int, gap time.Duration) {
	go func() {
		for i := 0; i < count; i++ {
			synPacket := GeneratePacket(c.key, model.FlagSYN, 0, 0, uint32(i))
			c.OutChannel <- synPacket
			time.Sleep(gap)
		}
	}()
}

/* ---------------- timer utility ---------------- */

func (c *Client) startTimer(d time.Duration, fn func()) {
	if c.responseTimer != nil {
		c.responseTimer.Stop()
	}
	c.responseTimer = time.AfterFunc(d, fn)
}

func (c *Client) GetState() model.ClientState {
	return model.ClientState{
		Key:       c.key,
		RetrySyn:  c.retrySyn,
		RetryData: c.retryData,
	}
}
