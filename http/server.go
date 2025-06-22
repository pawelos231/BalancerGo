package httpserver

import (
	"log"
	"net/http"
)

func StartServer(hub *Hub) {
	http.HandleFunc("/ws", hub.ServeWs)

	log.Println("WebSocket server running on :8080/ws")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
} 