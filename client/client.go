package client

import (
	"context"
	"load-balancer/model"
	"time"
)

const (
	SYN_TIMEOUT        = 500 * time.Millisecond // max wait for SYN-ACK
	SEGMENT_TIMEOUT    = 700 * time.Millisecond // max wait for DATA-ACK
	MAX_RETRANSMISIONS = 3                      // how many times to retransmit a segment
	READ_DEADLINE      = 5 * time.Second        // read deadline for receiving packets
	SEGMENT_SIZE       = 1024                   // bytes per PSH segment
)

type Client struct {
	key        model.ClientKey
	outChannel chan<- model.Packet // packets to be sent to the server
	inChannel  <-chan model.Packet // packets received from the server

	lastSent      model.Packet  // last sent packet, used for retransmission if needed
	retrySyn      uint8         // SYN retrans counter
	retryData     uint8         // DATA retrans counter
	responseTimer *time.Timer   // timer for waiting for a response from the server
	waitAck       chan struct{} // unbuffered channel to sync sender with ACK
}

func NewClient(
	srcIP string, srcPort int16,
	dstIP string, dstPort int16,
	proto model.Protocol,
	out chan<- model.Packet, in <-chan model.Packet) *Client {

	return &Client{
		key:        GenerateClientKey(srcIP, srcPort, dstIP, dstPort, proto),
		outChannel: out,
		inChannel:  in,
		waitAck:    make(chan struct{}),
	}
}

func (c *Client) Open() {
	// Initializes the connection by sending a SYN packet
	synPacket := GeneratePacket(c.key, model.FlagSYN, 0, 0)
	c.lastSent = synPacket
	c.outChannel <- synPacket
	c.startTimer(SYN_TIMEOUT, c.retransmitSyn) // wait for SYN-ACK
}

func (c *Client) Close() {
	finPacket := GeneratePacket(c.key, model.FlagFIN, 0, 0)
	c.outChannel <- finPacket // send the FIN packet to the server
	if c.responseTimer != nil {
		c.responseTimer.Stop()
	}
}

func (c *Client) Key() model.ClientKey { return c.key }

// SendStream sends dataSize bytes in segSize chunks; each chunk waits for ACK.
func (c *Client) SendStream(dataSize int, delayBetween time.Duration) {
	go func() {
		remaining := dataSize
		for remaining > 0 {
			chunk := SEGMENT_SIZE
			if chunk > remaining {
				chunk = remaining
			}

			dataPkt := GeneratePacket(c.key, model.FlagPSH, chunk, delayBetween)
			time.Sleep(dataPkt.Delay) // simulate network gap
			c.lastSent = dataPkt
			c.outChannel <- dataPkt // send segment
			c.startTimer(SEGMENT_TIMEOUT, c.retransmitData)

			<-c.waitAck // block until ACK received
			remaining -= chunk
		}
		c.Close()
	}()
}

func (c *Client) retransmitSyn() {
	if c.retrySyn >= MAX_RETRANSMISIONS {
		return
	}
	c.retrySyn++
	retry := c.lastSent
	retry.Flag = model.FlagSYN
	c.outChannel <- retry
	c.startTimer(SYN_TIMEOUT, c.retransmitSyn)
}

func (c *Client) retransmitData() {
	if c.retryData >= MAX_RETRANSMISIONS {
		return
	}
	c.retryData++
	retry := c.lastSent
	retry.Flag = model.FlagPSH
	c.outChannel <- retry
	c.startTimer(SEGMENT_TIMEOUT, c.retransmitData)
}

func (c *Client) ExpectResponse() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), READ_DEADLINE)
		defer cancel()

		for {
			select {
			case pkt := <-c.inChannel:
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
					// unblock sender to start data stream
					close(c.waitAck)

				// if the received packet is of type ACK, reset the retry counters
				case pkt.Flag&model.FlagACK != 0:
					// ACK of data
					if c.responseTimer != nil {
						c.responseTimer.Stop()
					}
					c.retryData = 0
					c.waitAck <- struct{}{} // let sender push next chunk

				case pkt.Flag == model.FlagFIN:
					return
				}

			case <-ctx.Done():
				return
			}
		}
	}()
}

// FloodSyn sends a specified number of SYN packets to the server with a defined gap between each packet.
func (c *Client) FloodSyn(count int, gap time.Duration) {
	go func() {
		for i := 0; i < count; i++ {
			synPacket := GeneratePacket(c.key, model.FlagSYN, 0, 0)
			c.outChannel <- synPacket
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
