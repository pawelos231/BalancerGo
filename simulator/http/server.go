package httpserver

import (
	"errors"
	"log"
	"net/http"
	"os"
	"time"
)

// NewServer creates an independently managed HTTP server. Its owner starts
// ListenAndServe and calls Shutdown; canceling Hub.Run closes upgraded sockets.
func NewServer(hub *Hub, address string, api ...ScenarioService) *http.Server {
	if len(api) > 1 {
		panic("httpserver: at most one scenario service is allowed")
	}
	if address == "" {
		address = ":8080"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.ServeWs)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	if len(api) == 1 && api[0] != nil {
		mountScenarioRoutes(mux, api[0])
	}
	return &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// StartServer retains the original convenience entry point. New integrations
// should use NewServer so the application owns graceful shutdown.
func StartServer(hub *Hub) {
	address := os.Getenv("LB_HTTP_ADDR")
	srv := NewServer(hub, address)
	log.Printf("WebSocket server running on %s/ws", srv.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("HTTP server stopped: %v", err)
	}
}
