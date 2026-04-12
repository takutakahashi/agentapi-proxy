package acp

import (
	"context"
	"fmt"
	"log"
	"net/http"
)

// Server is a standalone HTTP server that exposes the ACP WebSocket endpoint.
type Server struct {
	port        int
	agentapiURL string
}

// NewServer creates an ACP server that listens on port and proxies ACP
// protocol traffic to the claude-agentapi server at agentapiURL.
func NewServer(port int, agentapiURL string) *Server {
	return &Server{port: port, agentapiURL: agentapiURL}
}

// Start starts the HTTP server and blocks until ctx is cancelled or a fatal
// error occurs.
func (s *Server) Start(ctx context.Context) error {
	handler := NewHandler(s.agentapiURL)

	mux := http.NewServeMux()
	// WebSocket ACP endpoint
	mux.Handle("/acp", handler)
	// Liveness probe
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		log.Printf("[ACP] Shutting down")
		_ = srv.Shutdown(context.Background())
	}()

	log.Printf("[ACP] Listening on :%d (agentapi=%s)", s.port, s.agentapiURL)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("acp server: %w", err)
	}
	return nil
}
