package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	maxCommandBytes = 64 * 1024
	writeWait       = 5 * time.Second
	pongWait        = 60 * time.Second
	pingPeriod      = 20 * time.Second
)

var upg = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// The local visualizer is served from a different development port.
	CheckOrigin: func(r *http.Request) bool { return true },
}

var errHubClosed = errors.New("websocket hub is closed")

// Envelope is the UI command envelope. Payload decoding and all simulation
// mutations belong to the simulation owner, outside the HTTP goroutines.
type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type SetAlgorithmMsg struct {
	Name string `json:"name"`
}

type SpawnClientMsg struct {
	Count int `json:"count"`
}

type snapshotFrame struct {
	data []byte
}

type wsClient struct {
	conn     *websocket.Conn
	outgoing chan []byte
	done     chan struct{}
	stopOnce sync.Once
}

func (c *wsClient) stop() {
	c.stopOnce.Do(func() {
		close(c.done)
		// Closing the connection also interrupts a blocked reader or writer.
		c.conn.Close()
	})
}

// queueLatest has one producer (Hub.Run). Pending frames are superseded rather
// than accumulating behind a slow browser. A writer already sending a frame has
// a bounded write deadline and cannot hold up the hub or the simulation.
func (c *wsClient) queueLatest(frame []byte) {
	select {
	case <-c.done:
		return
	default:
	}
	select {
	case c.outgoing <- frame:
		return
	default:
	}
	select {
	case <-c.outgoing:
	default:
	}
	select {
	case c.outgoing <- frame:
	default:
	}
}

// Hub handles real I/O only. submit must enqueue a command without blocking;
// it must not read or mutate live simulation state. BroadcastState serializes a
// detached snapshot and never waits for a WebSocket connection or its writer.
type Hub struct {
	submit     func(Envelope) error
	latest     atomic.Pointer[snapshotFrame]
	wake       chan struct{}
	register   chan *wsClient
	unregister chan *wsClient
	done       chan struct{}
	runOnce    sync.Once
}

func NewHub(submit func(Envelope) error) *Hub {
	return &Hub{
		submit:     submit,
		wake:       make(chan struct{}, 1),
		register:   make(chan *wsClient),
		unregister: make(chan *wsClient),
		done:       make(chan struct{}),
	}
}

// Run owns the connection set until ctx is canceled. Snapshot caching also works
// before Run starts, allowing a browser to connect after a fast simulation ends.
func (h *Hub) Run(ctx context.Context) {
	h.runOnce.Do(func() {
		clients := make(map[*wsClient]struct{})
		defer func() {
			close(h.done)
			for c := range clients {
				c.stop()
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case c := <-h.register:
				clients[c] = struct{}{}
				if frame := h.latest.Load(); frame != nil {
					c.queueLatest(frame.data)
				}
			case c := <-h.unregister:
				delete(clients, c)
				c.stop()
			case <-h.wake:
				if frame := h.latest.Load(); frame != nil {
					for c := range clients {
						c.queueLatest(frame.data)
					}
				}
			}
		}
	})
}

func (h *Hub) BroadcastState(state interface{}) error {
	select {
	case <-h.done:
		return errHubClosed
	default:
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	h.latest.Store(&snapshotFrame{data: data})
	select {
	case h.wake <- struct{}{}:
	default:
	}
	return nil
}

func (h *Hub) readCommands(c *wsClient) {
	defer func() {
		c.stop()
		select {
		case h.unregister <- c:
		case <-h.done:
		}
	}()
	c.conn.SetReadLimit(maxCommandBytes)
	if err := c.conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		return
	}
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		var envelope Envelope
		if err := c.conn.ReadJSON(&envelope); err != nil {
			return
		}
		if h.submit == nil {
			continue
		}
		if err := h.submit(envelope); err != nil {
			log.Printf("UI command %q rejected: %v", envelope.Type, err)
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
		case frame := <-c.outgoing:
			if err := c.conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, frame); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
				return
			}
		}
	}
}

func (h *Hub) ServeWs(w http.ResponseWriter, r *http.Request) {
	select {
	case <-h.done:
		http.Error(w, "websocket hub is closed", http.StatusServiceUnavailable)
		return
	default:
	}
	conn, err := upg.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &wsClient{conn: conn, outgoing: make(chan []byte, 1), done: make(chan struct{})}
	select {
	case h.register <- c:
	case <-h.done:
		c.stop()
		return
	case <-r.Context().Done():
		c.stop()
		return
	}
	go h.writeFrames(c)
	// The upgraded HTTP handler supplies the single connection reader.
	h.readCommands(c)
}
