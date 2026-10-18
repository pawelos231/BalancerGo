package httpserver

import (
	"log"
	"net/http"
	"os"
)

func StartServer(hub *Hub) {
	http.HandleFunc("/ws", hub.ServeWs)
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	address := os.Getenv("LB_HTTP_ADDR")
	if address == "" {
		address = ":8080"
	}
	log.Printf("WebSocket server running on %s/ws", address)
	if err := http.ListenAndServe(address, nil); err != nil {
		log.Fatal(err)
	}
}
