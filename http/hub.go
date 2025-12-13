package httpserver

import (
	"encoding/json"
	"fmt"
	"load-balancer/algorithms"
	"load-balancer/balancer"
	"load-balancer/client"
	"load-balancer/model"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upg = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type Hub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan []byte
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.Mutex
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
	}
}

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

func (h *Hub) registerProtocolHandlers(lb *balancer.LoadBalancer) *Protocol {
	proto := NewProtocol()

	Register(proto, "setAlgorithm", func(msg SetAlgorithmMsg) error {
		var algo algorithms.Algorithm

		switch msg.Name {
		case "roundRobin":
			algo = &algorithms.RoundRobin{}
		case "random":
			algo = &algorithms.Random{}
		default:
			return fmt.Errorf("unknown algorithm: %s", msg.Name)
		}

		lb.SetAlgorithm(algo)
		fmt.Printf("Load balancer algorithm set to: %s\n", msg.Name)
		return nil
	})

	Register(proto, "spawnClient", func(msg SpawnClientMsg) error {

		for i := 0; i < msg.Count; i++ {
			cl := client.NewClient(
				fmt.Sprintf("192.168.%d.%d", (i/254)+1, (i%254)+1),
				int16(100+i),
				"lb.local",
				80,
				"TCP",
				make(chan model.Packet),
				make(chan model.Packet),
			)

			lb.AddClient(cl)

			go func(c *client.Client) {
				c.ListenForResponse()
				c.OpenConnection()
				c.SendCompleteStream(5000*model.SEGMENT_SIZE, 100*time.Millisecond)
			}(cl)
		}
		fmt.Println("Spawned", msg.Count, "clients")
		return nil
	})

	return proto
}

func (h *Hub) handleClientMessages(c *websocket.Conn, lb *balancer.LoadBalancer) {
	proto := h.registerProtocolHandlers(lb)

	for {
		var env Envelope
		if err := c.ReadJSON(&env); err != nil {
			log.Println("read error:", err)
			return
		}

		handler, ok := proto.handlers[env.Type]
		if !ok {
			log.Printf("unknown message type: %s", env.Type)
			continue
		}

		if err := handler(env.Payload); err != nil {
			log.Printf("handler error (%s): %v", env.Type, err)
		}
	}
}

func (h *Hub) Run(lb *balancer.LoadBalancer) {
	for {
		select {
		case cl := <-h.register:
			h.mu.Lock()
			h.clients[cl] = true
			log.Printf("New client connected: %s", cl.RemoteAddr())
			// listen for messages from client
			go h.handleClientMessages(cl, lb)

			h.mu.Unlock()
		case cl := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[cl]; ok {
				delete(h.clients, cl)
				cl.Close()
			}
			h.mu.Unlock()
		case message := <-h.broadcast:
			h.mu.Lock()
			for cl := range h.clients {
				err := cl.WriteMessage(websocket.TextMessage, message)
				if err != nil {
					log.Printf("error: %v", err)
					cl.Close()
					delete(h.clients, cl)
				}
			}
			h.mu.Unlock()
		}
	}
}

func (h *Hub) BroadcastState(state interface{}) {
	bytes, err := json.Marshal(state)
	if err != nil {
		log.Printf("error: %v", err)
		return
	}
	h.broadcast <- bytes
}

func (h *Hub) ServeWs(w http.ResponseWriter, r *http.Request) {
	conn, err := upg.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	h.register <- conn
}
