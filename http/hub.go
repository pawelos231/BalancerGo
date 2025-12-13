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
	"reflect"
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

func (h *Hub) Run(lb *balancer.LoadBalancer) {
	for {
		select {
		case cl := <-h.register:
			h.mu.Lock()
			h.clients[cl] = true
			log.Printf("New client connected: %s", cl.RemoteAddr())
			// listen for messages from client
			go func(c *websocket.Conn) {
				defer func() {
					for {
						var msg interface{}
						err := c.ReadJSON(&msg)
						if err != nil {
							log.Println("read error:", err)
							break
						}
						typeOfMsg := reflect.TypeOf(msg)
						switch typeOfMsg.Kind() {
						case reflect.Map:
							{
								fmt.Printf("%v\n", msg)
								typeField, ok := msg.(map[string]interface{})["type"]
								if !ok {
									log.Printf("no type field in message")
									continue
								}

								stringiedTypeField, ok := typeField.(string)
								if !ok {
									log.Printf("type field is not a string")
									continue
								}

								switch stringiedTypeField {
								case "setAlgorithm":
									{
										algorithmField, ok := msg.(map[string]interface{})["algorithm"]
										if !ok {
											log.Printf("no algorithm field in message")
											continue
										}
										stringiedAlgorithmField, ok := algorithmField.(string)
										if !ok {
											log.Printf("algorithm field is not a string")
											continue
										}
										var algorithm algorithms.Algorithm
										switch stringiedAlgorithmField {
										case "roundRobin":
											algorithm = &algorithms.RoundRobin{}
										case "random":
											algorithm = &algorithms.Random{}
										default:
											log.Printf("unknown algorithm: %s", stringiedAlgorithmField)
											continue
										}
										log.Printf("Setting algorithm to %s", stringiedAlgorithmField)
										lb.SetAlgorithm(algorithm)
										break
									}
								case "spawnClient":
									countField, ok := msg.(map[string]interface{})["count"]
									if !ok {
										log.Printf("no count field in message")
										continue
									}
									count, ok := countField.(float64)
									if !ok {
										log.Printf("count field is not a int")
										continue
									}
									for i := 0; i < int(count); i++ {
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
								}
								break
							}

						case reflect.Slice:
							log.Printf("it is a slice")
							break
						default:
							log.Printf("what the fuck is this")
						}
					}
				}()
			}(cl)

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
