package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
)

// localSession is the internal implementation of Session for local process-based sessions
type localSession struct {
	id           string
	request      *RunServerRequest
	process      *exec.Cmd
	cancel       context.CancelFunc
	startedAt    time.Time
	status       string
	processMutex sync.RWMutex
}

// ID returns the session ID
func (s *localSession) ID() string {
	return s.id
}

// Addr returns the address (host:port) the session is running on
func (s *localSession) Addr() string {
	return fmt.Sprintf("localhost:%d", s.request.Port)
}

// UserID returns the user ID that owns this session
func (s *localSession) UserID() string {
	return s.request.UserID
}

// Scope returns the resource scope ("user" or "team")
func (s *localSession) Scope() ResourceScope {
	if s.request.Scope == "" {
		return ScopeUser
	}
	return s.request.Scope
}

// TeamID returns the team ID when Scope is "team"
func (s *localSession) TeamID() string {
	return s.request.TeamID
}

// Tags returns the session tags
func (s *localSession) Tags() map[string]string {
	return s.request.Tags
}

// Status returns the current status of the session
func (s *localSession) Status() string {
	return s.status
}

// StartedAt returns when the session was started
func (s *localSession) StartedAt() time.Time {
	return s.startedAt
}

// Description returns the session description
// Returns tags["description"] if exists, otherwise returns InitialMessage
func (s *localSession) Description() string {
	if s.request != nil && s.request.Tags != nil {
		if desc, exists := s.request.Tags["description"]; exists && desc != "" {
			return desc
		}
	}
	if s.request != nil && s.request.InitialMessage != "" {
		return s.request.InitialMessage
	}
	return ""
}

// Cancel cancels the session context to trigger shutdown
func (s *localSession) Cancel() {
	if s.cancel != nil {
		s.cancel()
	}
}

// Request returns the run server request (for internal use)
func (s *localSession) Request() *RunServerRequest {
	return s.request
}

// SetTags updates the session tags (for internal use)
func (s *localSession) SetTags(tags map[string]string) {
	s.request.Tags = tags
}

// LocalSessionManager manages sessions using local processes
type LocalSessionManager struct {
	config   *config.Config
	verbose  bool
	logger   *logger.Logger
	sessions map[string]*localSession
	mutex    sync.RWMutex
	nextPort int
	portMux  sync.Mutex
}

// NewLocalSessionManager creates a new LocalSessionManager
func NewLocalSessionManager(cfg *config.Config, verbose bool, lgr *logger.Logger, startPort int) *LocalSessionManager {
	return &LocalSessionManager{
		config:   cfg,
		verbose:  verbose,
		logger:   lgr,
		sessions: make(map[string]*localSession),
		nextPort: startPort,
	}
}

// CreateSession creates a new session and starts it
func (m *LocalSessionManager) CreateSession(ctx context.Context, id string, req *RunServerRequest) (Session, error) {
	// Find available port
	port, err := m.getAvailablePort()
	if err != nil {
		return nil, fmt.Errorf("failed to allocate port: %w", err)
	}
	req.Port = port

	// Create session context
	sessionCtx, cancel := context.WithCancel(context.Background())

	session := &localSession{
		id:        id,
		request:   req,
		cancel:    cancel,
		startedAt: time.Now(),
		status:    "active",
	}

	// Store session
	m.mutex.Lock()
	m.sessions[id] = session
	m.mutex.Unlock()

	log.Printf("[SESSION_CREATED] ID: %s, Port: %d, User: %s, Tags: %v",
		id, port, req.UserID, req.Tags)

	// Log session start
	repository := ""
	if req.RepoInfo != nil {
		repository = req.RepoInfo.FullName
	}
	if err := m.logger.LogSessionStart(id, repository); err != nil {
		log.Printf("Failed to log session start for %s: %v", id, err)
	}

	// Start agentapi server in goroutine
	go m.runSession(sessionCtx, session)

	if m.verbose {
		log.Printf("Started agentapi server for session %s on port %d", id, port)
	}

	return session, nil
}

// GetSession returns a session by ID, nil if not found
func (m *LocalSessionManager) GetSession(id string) Session {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	session, exists := m.sessions[id]
	if !exists {
		return nil
	}
	return session
}

// GetLocalSession returns the internal localSession for handlers that need it
func (m *LocalSessionManager) GetLocalSession(id string) *localSession {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	return m.sessions[id]
}

// ListSessions returns all sessions matching the filter
func (m *LocalSessionManager) ListSessions(filter SessionFilter) []Session {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	var result []Session
	for _, session := range m.sessions {
		// User ID filter
		if filter.UserID != "" && session.request.UserID != filter.UserID {
			continue
		}

		// Status filter
		if filter.Status != "" && session.status != filter.Status {
			continue
		}

		// Scope filter
		if filter.Scope != "" && session.request.Scope != filter.Scope {
			continue
		}

		// TeamID filter
		if filter.TeamID != "" && session.request.TeamID != filter.TeamID {
			continue
		}

		// TeamIDs filter (for team-scoped sessions, check if session's team is in user's teams)
		if len(filter.TeamIDs) > 0 && session.request.Scope == ScopeTeam {
			teamMatch := false
			for _, teamID := range filter.TeamIDs {
				if session.request.TeamID == teamID {
					teamMatch = true
					break
				}
			}
			if !teamMatch {
				continue
			}
		}

		// Tag filters
		if len(filter.Tags) > 0 {
			matchAllTags := true
			for tagKey, tagValue := range filter.Tags {
				sessionTagValue, exists := session.request.Tags[tagKey]
				if !exists || sessionTagValue != tagValue {
					matchAllTags = false
					break
				}
			}
			if !matchAllTags {
				continue
			}
		}

		result = append(result, session)
	}

	return result
}

// DeleteSession stops and removes a session
func (m *LocalSessionManager) DeleteSession(id string) error {
	m.mutex.RLock()
	session, exists := m.sessions[id]
	m.mutex.RUnlock()

	if !exists {
		return fmt.Errorf("session not found")
	}

	// Cancel the session context to trigger graceful shutdown
	if session.cancel != nil {
		session.cancel()
		log.Printf("Successfully cancelled context for session %s", id)
	} else {
		log.Printf("Warning: session %s had no cancel function", id)
	}

	// Wait for session cleanup with timeout
	maxWaitTime := 5 * time.Second
	waitInterval := 50 * time.Millisecond
	startTime := time.Now()

	for {
		m.mutex.RLock()
		_, stillExists := m.sessions[id]
		m.mutex.RUnlock()

		if !stillExists {
			log.Printf("Session %s successfully removed from active sessions", id)
			break
		}

		if time.Since(startTime) >= maxWaitTime {
			log.Printf("Warning: session %s still exists after %v, forcing removal", id, maxWaitTime)

			m.mutex.Lock()
			delete(m.sessions, id)
			m.mutex.Unlock()

			break
		}

		time.Sleep(waitInterval)
	}

	// Log session end
	if err := m.logger.LogSessionEnd(id, 0); err != nil {
		log.Printf("Failed to log session end for %s: %v", id, err)
	}

	// Clean up session working directory
	if id != "" {
		workDir := fmt.Sprintf("/home/agentapi/workdir/%s", id)
		if _, err := os.Stat(workDir); err == nil {
			log.Printf("Removing session working directory: %s", workDir)
			if err := os.RemoveAll(workDir); err != nil {
				log.Printf("Failed to remove session working directory %s: %v", workDir, err)
			} else {
				log.Printf("Successfully removed session working directory: %s", workDir)
			}
		}
	}

	return nil
}

// Shutdown gracefully stops all sessions
func (m *LocalSessionManager) Shutdown(timeout time.Duration) error {
	m.mutex.RLock()
	sessions := make([]*localSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		sessions = append(sessions, session)
	}
	m.mutex.RUnlock()

	log.Printf("Shutting down, terminating %d active sessions...", len(sessions))

	if len(sessions) == 0 {
		log.Printf("No active sessions to terminate")
		return nil
	}

	// Cancel all sessions
	for _, session := range sessions {
		if session.cancel != nil {
			session.processMutex.RLock()
			process := session.process
			session.processMutex.RUnlock()

			if process != nil && process.Process != nil {
				log.Printf("Terminating session %s (PID: %d)", session.id, process.Process.Pid)
			} else {
				log.Printf("Terminating session %s", session.id)
			}
			session.cancel()
		}
	}

	// Wait for all sessions to complete with timeout
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			m.mutex.RLock()
			remaining := len(m.sessions)
			m.mutex.RUnlock()

			if remaining == 0 {
				return
			}

			time.Sleep(100 * time.Millisecond)
		}
	}()

	select {
	case <-done:
		log.Printf("All sessions terminated gracefully")
		return nil
	case <-time.After(timeout):
		m.mutex.RLock()
		remaining := len(m.sessions)
		m.mutex.RUnlock()
		log.Printf("Timeout reached, %d sessions may still be running", remaining)
		return fmt.Errorf("shutdown timeout reached with %d sessions still running", remaining)
	}
}

// getAvailablePort finds an available port
func (m *LocalSessionManager) getAvailablePort() (int, error) {
	m.portMux.Lock()
	defer m.portMux.Unlock()

	startPort := m.nextPort
	for port := startPort; port < startPort+1000; port++ {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			if err := ln.Close(); err != nil {
				log.Printf("Failed to close listener: %v", err)
			}
			m.nextPort = port + 1
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available ports in range %d-%d", startPort, startPort+1000)
}

// runSession runs the agentapi server process for a session
func (m *LocalSessionManager) runSession(ctx context.Context, session *localSession) {
	req := session.request

	defer func() {
		// Clean up session when server stops
		m.mutex.Lock()
		_, sessionExists := m.sessions[session.id]
		if sessionExists {
			delete(m.sessions, session.id)
		}
		m.mutex.Unlock()

		// Log session end when process terminates naturally (not via deleteSession)
		if sessionExists {
			if err := m.logger.LogSessionEnd(session.id, 0); err != nil {
				log.Printf("Failed to log session end for %s: %v", session.id, err)
			}
		}

		if m.verbose {
			log.Printf("Cleaned up session %s", session.id)
		}
	}()

	// Create startup manager
	startupManager := NewStartupManager(m.config, m.verbose)

	// Prepare startup configuration
	cfg := m.buildStartupConfig(session)

	// Add repository information if available
	if req.RepoInfo != nil {
		cfg.RepoFullName = req.RepoInfo.FullName
		cfg.CloneDir = req.RepoInfo.CloneDir
	} else {
		// Always set CloneDir to session ID, even when no repository is specified
		cfg.CloneDir = session.id
	}

	// Extract MCP configurations from tags if available
	if req.Tags != nil {
		if mcpConfigs, exists := req.Tags["claude.mcp_configs"]; exists && mcpConfigs != "" {
			cfg.MCPConfigs = mcpConfigs
		}
	}

	// Start the AgentAPI session using Go functions
	cmd, err := startupManager.StartAgentAPISession(ctx, cfg)
	if err != nil {
		log.Printf("Failed to start AgentAPI session for %s: %v", session.id, err)
		return
	}

	// Log startup details
	log.Printf("Starting agentapi process for session %s on %d using Go functions", session.id, req.Port)
	log.Printf("Session startup parameters:")
	log.Printf("  Port: %d", req.Port)
	log.Printf("  Session ID: %s", session.id)
	log.Printf("  User ID: %s", req.UserID)
	if cfg.RepoFullName != "" {
		log.Printf("  Repository: %s", cfg.RepoFullName)
		log.Printf("  Clone dir: %s", cfg.CloneDir)
	}
	if len(req.Tags) > 0 {
		log.Printf("  Request tags:")
		for key, value := range req.Tags {
			log.Printf("    %s=%s", key, value)
		}
	}

	// Capture stderr output for logging on exit code 1
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	// Store the command in the session and start the process
	session.processMutex.Lock()
	session.process = cmd
	err = cmd.Start()
	session.processMutex.Unlock()

	if err != nil {
		log.Printf("Failed to start agentapi process for session %s: %v", session.id, err)
		return
	}

	if m.verbose {
		log.Printf("AgentAPI process started for session %s (PID: %d)", session.id, cmd.Process.Pid)
	}

	// Send initial message if provided
	if req.InitialMessage != "" {
		go m.sendInitialMessage(session, req.InitialMessage)
	}

	// Wait for the process to finish or context cancellation
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Recovered from panic in cmd.Wait() for session %s: %v", session.id, r)
				done <- fmt.Errorf("panic in cmd.Wait(): %v", r)
			}
		}()
		done <- cmd.Wait()
	}()

	// Ensure the process is cleaned up to prevent zombie processes
	defer func() {
		if cmd.Process != nil && cmd.ProcessState == nil {
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				log.Printf("Warning: Process %d cleanup timed out after 10 seconds", cmd.Process.Pid)
				if cmd.Process != nil {
					log.Printf("Force killing process %d to prevent zombie", cmd.Process.Pid)
					if err := cmd.Process.Kill(); err != nil {
						log.Printf("Failed to kill process %d: %v", cmd.Process.Pid, err)
					}
					go func() {
						if waitErr := cmd.Wait(); waitErr != nil {
							log.Printf("Wait error after force kill for process %d: %v", cmd.Process.Pid, waitErr)
						}
					}()
				}
			}
		}
	}()

	select {
	case <-ctx.Done():
		// Context cancelled, terminate the process
		if m.verbose {
			log.Printf("Terminating agentapi process for session %s", session.id)
		}

		// Try graceful shutdown first (SIGTERM to process group)
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
			if termErr := cmd.Process.Signal(syscall.SIGTERM); termErr != nil {
				log.Printf("Failed to send SIGTERM to process %d: %v", cmd.Process.Pid, termErr)
			}
		} else {
			log.Printf("Sent SIGTERM to process group %d", cmd.Process.Pid)
		}

		// Wait for graceful shutdown with timeout
		gracefulTimeout := time.After(5 * time.Second)
		select {
		case waitErr := <-done:
			if m.verbose {
				log.Printf("AgentAPI process for session %s terminated gracefully", session.id)
			}
			if waitErr != nil && m.verbose {
				log.Printf("Process wait error for session %s: %v", session.id, waitErr)
			}
		case <-gracefulTimeout:
			if m.verbose {
				log.Printf("Force killing agentapi process for session %s", session.id)
			}
			if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
				log.Printf("Failed to kill process group %d: %v", cmd.Process.Pid, err)
				if killErr := cmd.Process.Kill(); killErr != nil {
					log.Printf("Failed to kill process %d: %v", cmd.Process.Pid, killErr)
				}
			} else {
				log.Printf("Sent SIGKILL to process group %d", cmd.Process.Pid)
			}
			select {
			case waitErr := <-done:
				if waitErr != nil && m.verbose {
					log.Printf("Process wait error after kill for session %s: %v", session.id, waitErr)
				}
			case <-time.After(2 * time.Second):
				log.Printf("Warning: Process %d may not have exited cleanly", cmd.Process.Pid)
				go func() {
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						log.Printf("Warning: Could not consume done channel for process %d", cmd.Process.Pid)
					}
				}()
			}
		}

	case err := <-done:
		// Process finished on its own
		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() == 1 {
				log.Printf("AgentAPI process for session %s exited with code 1: %v", session.id, err)
				if stderrOutput := stderrBuf.String(); stderrOutput != "" {
					log.Printf("Stderr output for session %s: %s", session.id, stderrOutput)
				}
			} else {
				log.Printf("AgentAPI process for session %s exited with error: %v", session.id, err)
			}
		} else if m.verbose {
			log.Printf("AgentAPI process for session %s exited normally", session.id)
		}
	}
}

// buildStartupConfig builds the startup configuration for a session
func (m *LocalSessionManager) buildStartupConfig(session *localSession) *StartupConfig {
	req := session.request
	return &StartupConfig{
		Port:                      req.Port,
		UserID:                    req.UserID,
		GitHubToken:               getEnvFromRequest(req, "GITHUB_TOKEN", os.Getenv("GITHUB_TOKEN")),
		GitHubAppID:               getEnvFromRequest(req, "GITHUB_APP_ID", os.Getenv("GITHUB_APP_ID")),
		GitHubInstallationID:      getEnvFromRequest(req, "GITHUB_INSTALLATION_ID", os.Getenv("GITHUB_INSTALLATION_ID")),
		GitHubAppPEMPath:          getEnvFromRequest(req, "GITHUB_APP_PEM_PATH", os.Getenv("GITHUB_APP_PEM_PATH")),
		GitHubAPI:                 getEnvFromRequest(req, "GITHUB_API", os.Getenv("GITHUB_API")),
		GitHubPersonalAccessToken: getEnvFromRequest(req, "GITHUB_PERSONAL_ACCESS_TOKEN", os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN")),
		AgentAPIArgs:              getEnvFromRequest(req, "AGENTAPI_ARGS", os.Getenv("AGENTAPI_ARGS")),
		ClaudeArgs:                getEnvFromRequest(req, "CLAUDE_ARGS", os.Getenv("CLAUDE_ARGS")),
		Environment:               req.Environment,
		Config:                    m.config,
		Verbose:                   m.verbose,
	}
}

// sendInitialMessage sends an initial message to the agentapi server after startup
func (m *LocalSessionManager) sendInitialMessage(session *localSession, message string) {
	port := session.request.Port

	// Wait a bit for the server to start up
	time.Sleep(2 * time.Second)

	// Check server health first
	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", port))
		if err == nil {
			if closeErr := resp.Body.Close(); closeErr != nil {
				log.Printf("Failed to close response body: %v", closeErr)
			}
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if i == maxRetries-1 {
			log.Printf("AgentAPI server for session %s not ready after %d retries, skipping initial message", session.id, maxRetries)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Prepare message request
	messageReq := map[string]interface{}{
		"content": message,
		"type":    "user",
	}

	jsonBody, err := json.Marshal(messageReq)
	if err != nil {
		log.Printf("Failed to marshal message request for session %s: %v", session.id, err)
		return
	}

	// Send message to agentapi
	url := fmt.Sprintf("http://localhost:%d/message", port)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		log.Printf("Failed to send initial message to session %s: %v", session.id, err)
		return
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("Failed to close response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Failed to send initial message to session %s (status: %d): %s", session.id, resp.StatusCode, string(body))
		return
	}

	log.Printf("Successfully sent initial message to session %s", session.id)
}
