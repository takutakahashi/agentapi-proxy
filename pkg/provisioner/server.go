// Package provisioner provides the session Pod provisioner. The provisioner
// exposes local health/status endpoints and pulls provision requests from the
// proxy internal API.
package provisioner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

// defaultStartupScript is run on every Pod start regardless of agent type.
// It pre-fetches the latest ACP package binaries so that agent startup does
// not incur a network download when a provision request arrives.
// Override with the PROVISIONER_PRE_SCRIPT environment variable.
const defaultStartupScript = `bun install --global @agentclientprotocol/claude-agent-acp@latest
npm install --global @zed-industries/codex-acp@latest
`

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
	serverCtx context.Context // long-lived context for provisioning goroutines
	reporter  func(Status, string)
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

	// Run the common startup pre-script immediately in the background.
	// This pre-fetches ACP packages while the Pod is idle (stock inventory or
	// Pod restart), so provisioning does not have to wait for network downloads.
	go s.runStartupScript(ctx)

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
	mux.HandleFunc("/sandbox-domains", s.handleSandboxDomains)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}

	// Shutdown when context is cancelled.
	go func() {
		<-ctx.Done()
		log.Printf("[PROVISIONER] Context cancelled, shutting down HTTP server")
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

// setStatus updates the provisioning state thread-safely.
func (s *Server) setStatus(st Status, msg string) {
	s.mu.Lock()
	s.status = st
	s.message = msg
	reporter := s.reporter
	s.mu.Unlock()
	log.Printf("[PROVISIONER] Status changed to %s%s", st, func() string {
		if msg != "" {
			return ": " + msg
		}
		return ""
	}())
	if reporter != nil {
		reporter(st, msg)
	}
}

// GetStatus returns the current status (used by tests).
func (s *Server) GetStatus() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// SetStatusReporter installs a callback invoked on every status transition.
func (s *Server) SetStatusReporter(fn func(Status, string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reporter = fn
}

// handleSandboxDomains proxies GET /sandbox-domains to the network filter control
// server (127.0.0.1:3129/domains) and returns the accessed domain list.
// Returns 503 when the network filter is not running (no sandbox sidecar).
func (s *Server) handleSandboxDomains(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp, err := http.Get("http://127.0.0.1:3129/domains") //nolint:noctx
	if err != nil {
		http.Error(w, "network filter not available", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close() //nolint:errcheck

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// runStartupScript executes the common startup pre-script as soon as the Pod
// starts. It uses PROVISIONER_PRE_SCRIPT if set, otherwise defaultStartupScript.
// Failure is non-fatal: a warning is logged and the server continues normally.
func (s *Server) runStartupScript(ctx context.Context) {
	script := os.Getenv("PROVISIONER_PRE_SCRIPT")
	if script == "" {
		script = defaultStartupScript
	}
	if os.Getenv("AGENTAPI_SCIA_SESSION_SIDECAR_ENABLED") == "true" {
		waitForSciaProxy(ctx, "http://127.0.0.1:18081", 15*time.Second)
	}
	log.Printf("[PROVISIONER] Running startup pre-script")
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Printf("[PROVISIONER] Warning: startup pre-script failed (continuing): %v", err)
	} else {
		log.Printf("[PROVISIONER] Startup pre-script complete")
	}
}
