package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"load-balancer/internal/contract"
)

var upg = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// The local visualizer is served from a different development port.
	CheckOrigin: func(r *http.Request) bool { return true },
}

var errHubClosed = errors.New("websocket hub is closed")

// Hub handles real I/O only. submit must enqueue a command without blocking;
// it must not read or mutate live simulation state. BroadcastState serializes a
// detached snapshot and never waits for a WebSocket connection or its writer.
type Hub struct {
	submit func(contract.Envelope, func(contract.CommandResultFrame)) error

	mu         sync.Mutex
	clients    map[*wsClient]struct{}
	latest     []byte
	currentEnd []byte
	closed     bool
	done       chan struct{}
	runOnce    sync.Once
}

func NewHub(submit func(contract.Envelope, func(contract.CommandResultFrame)) error) *Hub {
	return &Hub{
		submit:  submit,
		clients: make(map[*wsClient]struct{}),
		done:    make(chan struct{}),
	}
}

// Publications and registration work before Run starts. Run owns shutdown only.
func (h *Hub) Run(ctx context.Context) {
	h.runOnce.Do(func() {
		<-ctx.Done()
		h.mu.Lock()
		h.closed = true
		close(h.done)
		clients := h.clients
		h.clients = nil
		h.mu.Unlock()
		for c := range clients {
			c.stop()
		}
	})
}

func (h *Hub) BroadcastState(state interface{}) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return errHubClosed
	}
	h.latest = data
	h.currentEnd = nil
	for c := range h.clients {
		c.queueLatest(data)
	}
	return nil
}

// BroadcastEnded publishes a retained lifecycle barrier without waiting on I/O.
func (h *Hub) BroadcastEnded(ended contract.SimulationEndedFrame) error {
	data, err := json.Marshal(ended)
	if err != nil {
		return err
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return errHubClosed
	}
	h.latest = nil
	h.currentEnd = data
	var overflowed []*wsClient
	for c := range h.clients {
		if c.queueCritical(data, true) {
			overflowed = append(overflowed, c)
		}
	}
	h.mu.Unlock()
	for _, c := range overflowed {
		c.closeOverflow()
	}
	return nil
}

func (h *Hub) registerClient(c *wsClient) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return errHubClosed
	}
	h.clients[c] = struct{}{}
	overflow := false
	if h.currentEnd != nil {
		overflow = c.queueCritical(h.currentEnd, true)
	} else if h.latest != nil {
		c.queueLatest(h.latest)
	}
	h.mu.Unlock()
	if overflow {
		c.closeOverflow()
	}
	return nil
}

func (h *Hub) unregisterClient(c *wsClient) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	c.stop()
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
	c := newWSClient(conn)
	select {
	case <-r.Context().Done():
		c.stop()
		return
	default:
	}
	if err := h.registerClient(c); err != nil {
		c.stop()
		return
	}
	go h.writeFrames(c)
	// The upgraded HTTP handler supplies the single connection reader.
	h.readCommands(c)
}
