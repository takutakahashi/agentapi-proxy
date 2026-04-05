// Package provisioner provides an HTTP server that provisions session Pods.
// The agent-provisioner starts this server on startup. The proxy server
// calls POST /provision with session settings JSON to trigger the startup
// sequence (setup + agentapi launch + initial message sending).
package provisioner

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

// Status represents the provisioning lifecycle state.
type Status string

const (
	// StatusPending means no provisioning has been triggered yet.
	StatusPending Status = "pending"
	// StatusProvisioning means provisioning is currently in progress.
	StatusProvisioning Status = "provisioning"
	// StatusReady means provisioning completed successfully and agentapi is running.
	StatusReady Status = "ready"
	// StatusError means provisioning failed.
	StatusError Status = "error"
)

// StatusResponse is the JSON body returned by GET /status.
type StatusResponse struct {
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
}

// Server is the agent-provisioner HTTP server.
type Server struct {
	port         int
	settingsFile string // path to optional auto-provision settings file

	mu        sync.RWMutex
	status    Status
	message   string
	everReady bool            // true once status has reached StatusReady; never reset
	serverCtx context.Context // long-lived context for provisioning goroutines
}

// New creates a new Server.
//
//   - port:         TCP port to listen on (e.g. 9001)
//   - settingsFile: path to /session-settings/settings.yaml; if this file
//     exists at startup the server auto-provisions from it (Pod restart case).
func New(port int, settingsFile string) *Server {
	return &Server{
		port:         port,
		settingsFile: settingsFile,
		status:       StatusPending,
	}
}

// Start starts the HTTP server and blocks until ctx is cancelled or a fatal
// error occurs.
//
// If settingsFile exists at startup (Pod restart case), provisioning is
// started automatically in the background before the HTTP server begins
// accepting requests.
func (s *Server) Start(ctx context.Context) error {
	// Store the server-level context so that provisioning goroutines survive
	// beyond the HTTP request that triggered them.
	s.serverCtx = ctx

	// Auto-provision from Secret volume if available (Pod restart case).
	if s.settingsFile != "" {
		if _, err := os.Stat(s.settingsFile); err == nil {
			log.Printf("[PROVISIONER] Settings file found at %s – auto-provisioning", s.settingsFile)
			settings, err := sessionsettings.LoadSettings(s.settingsFile)
			if err != nil {
				log.Printf("[PROVISIONER] Failed to load settings for auto-provisioning: %v", err)
			} else {
				s.setStatus(StatusProvisioning, "")
				go s.runProvision(ctx, settings)
			}
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/provision", s.handleProvision)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}

	// Shutdown when context is cancelled.
	// Before shutting down, save session memory if the session was ready and
	// AGENTAPI_MEMORY_SAVE_ON_SHUTDOWN is not set to "false".
	go func() {
		<-ctx.Done()
		if s.wasEverReady() && os.Getenv("AGENTAPI_MEMORY_SAVE_ON_SHUTDOWN") != "false" {
			log.Printf("[PROVISIONER] Context cancelled, saving session memory before shutdown")
			saveSessionMemory()
			log.Printf("[PROVISIONER] Session memory save complete")
		}
		log.Printf("[PROVISIONER] Shutting down HTTP server")
		_ = srv.Shutdown(context.Background())
	}()

	log.Printf("[PROVISIONER] Listening on :%d", s.port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("provisioner server error: %w", err)
	}
	return nil
}

// handleHealthz returns 200 OK unconditionally (used by liveness/readiness probes).
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// handleStatus returns the current provisioning state as JSON.
// When the provisioner is in an error state, it returns HTTP 500 so that
// clients can distinguish a permanent failure from a transient startup delay.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	resp := StatusResponse{Status: s.status, Message: s.message}
	s.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	if resp.Status == StatusError {
		w.WriteHeader(http.StatusInternalServerError)
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("[PROVISIONER] Failed to encode status response: %v", err)
	}
}

// handleProvision accepts POST /provision with a SessionSettings JSON body.
//
//   - 202 Accepted  – provisioning started in background
//   - 200 OK        – already ready (idempotent)
//   - 409 Conflict  – provisioning already in progress
//   - 400 Bad Request – invalid JSON body
//   - 405 Method Not Allowed – non-POST request
func (s *Server) handleProvision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	current := s.status
	s.mu.RUnlock()

	switch current {
	case StatusReady:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(StatusResponse{Status: StatusReady})
		return
	case StatusProvisioning:
		http.Error(w, "provisioning already in progress", http.StatusConflict)
		return
	}

	// Parse settings from request body.
	var settings sessionsettings.SessionSettings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Note: We no longer write to s.settingsFile here. The Proxy server creates
	// the settings Secret after provisioning succeeds, so the mounted volume
	// (optional:true) will contain the data on Pod restart automatically.

	s.setStatus(StatusProvisioning, "")
	// Use the server-level context (not r.Context()) so that provisioning
	// survives after the HTTP response is written and the connection closes.
	go s.runProvision(s.serverCtx, &settings)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(StatusResponse{Status: StatusProvisioning})
}

// setStatus updates the provisioning state thread-safely.
func (s *Server) setStatus(st Status, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = st
	s.message = msg
	if st == StatusReady {
		s.everReady = true
	}
	log.Printf("[PROVISIONER] Status changed to %s%s", st, func() string {
		if msg != "" {
			return ": " + msg
		}
		return ""
	}())
}

// GetStatus returns the current status (used by tests).
func (s *Server) GetStatus() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// wasEverReady returns true if the provisioner has ever reached StatusReady.
// Unlike GetStatus, this is never reset even when the agent process exits,
// making it safe to use in the shutdown goroutine without a race condition.
func (s *Server) wasEverReady() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.everReady
}
