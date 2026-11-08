package httpserver

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"load-balancer/internal/contract"
)

const (
	maxCommandBytes       = 64 * 1024
	writeWait             = 5 * time.Second
	pongWait              = 60 * time.Second
	pingPeriod            = 20 * time.Second
	criticalQueueCapacity = 8
)

type wsClient struct {
	conn *websocket.Conn

	mu        sync.Mutex
	critical  [][]byte
	latest    []byte
	wake      chan struct{}
	done      chan struct{}
	stopped   bool
	closeOnce sync.Once
}

func newWSClient(conn *websocket.Conn) *wsClient {
	return &wsClient{
		conn: conn, critical: make([][]byte, 0, criticalQueueCapacity),
		wake: make(chan struct{}, 1), done: make(chan struct{}),
	}
}

func (c *wsClient) stopLocked() {
	if !c.stopped {
		c.stopped = true
		c.critical = nil
		c.latest = nil
		close(c.done)
	}
}

func (c *wsClient) closeConnection() {
	c.closeOnce.Do(func() {
		// Closing the connection also interrupts a blocked reader or writer.
		if c.conn != nil {
			c.conn.Close()
		}
	})
}

func (c *wsClient) stop() {
	c.mu.Lock()
	c.stopLocked()
	c.mu.Unlock()
	c.closeConnection()
}

func (c *wsClient) signalLocked() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// Ordinary measurements share one replaceable slot behind critical frames.
func (c *wsClient) queueLatest(frame []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return
	}
	c.latest = frame
	c.signalLocked()
}

// Ends and private replies share one FIFO. An end discards older measurements.
// On overflow the caller closes the socket after releasing publication locks.
func (c *wsClient) queueCritical(frame []byte, barrier bool) (overflow bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return false
	}
	if len(c.critical) == criticalQueueCapacity {
		c.stopLocked()
		return true
	}
	if barrier {
		c.latest = nil
	}
	c.critical = append(c.critical, frame)
	c.signalLocked()
	return false
}

func (c *wsClient) closeOverflow() {
	c.closeConnection()
	log.Printf("WebSocket connection closed: critical frame queue full (capacity %d)", criticalQueueCapacity)
}

// The writer chooses critical frames and measurements under the same lock.
func (c *wsClient) takeFrame() ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return nil, false
	}
	if len(c.critical) != 0 {
		frame := c.critical[0]
		copy(c.critical, c.critical[1:])
		c.critical[len(c.critical)-1] = nil
		c.critical = c.critical[:len(c.critical)-1]
		return frame, true
	}
	if c.latest != nil {
		frame := c.latest
		c.latest = nil
		return frame, true
	}
	return nil, false
}

func (c *wsClient) queueReply(message any) {
	frame, err := json.Marshal(message)
	if err != nil {
		log.Printf("WebSocket command reply serialization failed: %v", err)
		c.stop()
		return
	}
	if c.queueCritical(frame, false) {
		c.closeOverflow()
	}
}

func (c *wsClient) replyCommand(result contract.CommandResultFrame) {
	c.queueReply(result)
}

func (h *Hub) readCommands(c *wsClient) {
	defer h.unregisterClient(c)
	c.conn.SetReadLimit(maxCommandBytes)
	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		return
	}
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		var envelope contract.Envelope
		if err := c.conn.ReadJSON(&envelope); err != nil {
			return
		}
		if h.submit == nil {
			continue
		}
		if err := h.submit(envelope, c.replyCommand); err != nil {
			log.Printf("UI command %q rejected: %v", envelope.Type, err)
			if envelope.Type == contract.StopSimulation {
				c.replyCommand(contract.CommandResultFrame{
					Type:        "commandResult",
					CommandType: envelope.Type,
					RunID:       envelope.RunID,
					CommandID:   envelope.CommandID,
					Status:      contract.CommandRejected,
					Error:       err.Error(),
				})
				continue
			}
			c.queueReply(contract.CommandErrorFrame{
				Type: "commandError", CommandType: envelope.Type, Error: err.Error(),
			})
		}
	}
}

func (h *Hub) writeFrames(c *wsClient) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	defer c.stop()
	for {
		select {
		case <-h.done:
			return
		case <-c.done:
			return
		case <-ticker.C:
			if err := c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
				return
			}
		default:
		}
		if frame, ok := c.takeFrame(); ok {
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, frame); err != nil {
				return
			}
			continue
		}
		select {
		case <-h.done:
			return
		case <-c.done:
			return
		case <-c.wake:
		case <-ticker.C:
			if err := c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
				return
			}
		}
	}
}
