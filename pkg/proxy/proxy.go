package proxy

import (
	"context"
	"embed"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

//go:embed scripts/*
var embeddedScripts embed.FS

// embedded script cache
var scriptCache map[string][]byte

const (
	ScriptWithGithub = "agentapi_with_github.sh"
	ScriptDefault    = "agentapi_default.sh"
)

// AgentSession represents a running agentapi server instance
type AgentSession struct {
	ID        string
	Port      int
	Process   *exec.Cmd
	Cancel    context.CancelFunc
	StartedAt time.Time
	UserID    string
	Status    string
}

// Proxy represents the HTTP proxy server
type Proxy struct {
	config        *config.Config
	echo          *echo.Echo
	verbose       bool
	sessions      map[string]*AgentSession
	sessionsMutex sync.RWMutex
	nextPort      int
	portMutex     sync.Mutex
}

// NewProxy creates a new proxy instance
func NewProxy(cfg *config.Config, verbose bool) *Proxy {
	e := echo.New()

	// Disable Echo's default logger and use custom logging
	e.Logger.SetOutput(io.Discard)

	// Add recovery middleware
	e.Use(middleware.Recover())

	// --- scriptCache初期化 ---
	if scriptCache == nil {
		scriptCache = make(map[string][]byte)
		entries, err := embeddedScripts.ReadDir("scripts")
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					b, err := embeddedScripts.ReadFile("scripts/" + entry.Name())
					if err == nil {
						scriptCache[entry.Name()] = b
					}
				}
			}
		}
	}
	// --- ここまで ---

	p := &Proxy{
		config:        cfg,
		echo:          e,
		verbose:       verbose,
		sessions:      make(map[string]*AgentSession),
		sessionsMutex: sync.RWMutex{},
		nextPort:      cfg.StartPort,
	}

	// Add logging middleware if verbose
	if verbose {
		e.Use(p.loggingMiddleware())
	}

	p.setupRoutes()
	return p
}

// loggingMiddleware returns Echo middleware for request logging
func (p *Proxy) loggingMiddleware() echo.MiddlewareFunc {
	return echo.MiddlewareFunc(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			req := c.Request()
			log.Printf("Request: %s %s from %s", req.Method, req.URL.Path, req.RemoteAddr)
			return next(c)
		}
	})
}

// setupRoutes configures the router with all defined routes
func (p *Proxy) setupRoutes() {
	// Add session management routes according to API specification
	p.echo.POST("/start", p.startAgentAPIServer)
	p.echo.GET("/search", p.searchSessions)
	p.echo.Any("/:sessionId/*", p.routeToSession)
}

// searchSessions handles GET /search requests to list and filter sessions
func (p *Proxy) searchSessions(c echo.Context) error {
	userID := c.QueryParam("user_id")
	status := c.QueryParam("status")

	p.sessionsMutex.RLock()
	defer p.sessionsMutex.RUnlock()

	var filteredSessions []map[string]interface{}

	for _, session := range p.sessions {
		// Apply filters
		if userID != "" && session.UserID != userID {
			continue
		}
		if status != "" && session.Status != status {
			continue
		}

		sessionData := map[string]interface{}{
			"session_id": session.ID,
			"user_id":    session.UserID,
			"status":     session.Status,
			"started_at": session.StartedAt,
			"port":       session.Port,
		}
		filteredSessions = append(filteredSessions, sessionData)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"sessions": filteredSessions,
	})
}

// startAgentAPIServer starts a new agentapi server instance and returns session ID
func (p *Proxy) startAgentAPIServer(c echo.Context) error {
	// Generate UUID for session
	sessionID := uuid.New().String()

	// Get user_id from query parameters or request
	userID := c.QueryParam("user_id")
	if userID == "" {
		userID = "anonymous"
	}

	// Find available port
	port, err := p.getAvailablePort()
	if err != nil {
		log.Printf("Failed to find available port: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to allocate port")
	}

	// Determine which script to use based on request parameters
	scriptName := p.selectScript(c, scriptCache)

	// Start agentapi server in goroutine
	ctx, cancel := context.WithCancel(context.Background())

	session := &AgentSession{
		ID:        sessionID,
		Port:      port,
		Cancel:    cancel,
		StartedAt: time.Now(),
		UserID:    userID,
		Status:    "active",
	}

	// Store session
	p.sessionsMutex.Lock()
	p.sessions[sessionID] = session
	p.sessionsMutex.Unlock()
	log.Printf("session: %+v", session)
	log.Printf("scriptName: %s", scriptName)
	// Start agentapi server in goroutine
	go p.runAgentAPIServer(ctx, session, scriptName)

	if p.verbose {
		if scriptName != "" {
			log.Printf("Started agentapi server for session %s on port %d using script %s", sessionID, port, scriptName)
		} else {
			log.Printf("Started agentapi server for session %s on port %d using direct command", sessionID, port)
		}
	}

	// Return session ID according to API specification
	return c.JSON(http.StatusOK, map[string]interface{}{
		"session_id": sessionID,
	})
}

// routeToSession routes requests to the appropriate agentapi server instance
func (p *Proxy) routeToSession(c echo.Context) error {
	sessionID := c.Param("sessionId")

	p.sessionsMutex.RLock()
	session, exists := p.sessions[sessionID]
	p.sessionsMutex.RUnlock()

	if !exists {
		if p.verbose {
			log.Printf("Session %s not found", sessionID)
		}
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	// Create target URL for the agentapi server
	targetURL := fmt.Sprintf("http://localhost:%d", session.Port)
	target, err := url.Parse(targetURL)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Invalid target URL: %v", err))
	}

	// Get request and response from Echo context
	req := c.Request()
	w := c.Response()

	// Create reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(target)

	// Customize the director to preserve the original path (minus the session ID part)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)

		// Remove /sessionId prefix from path
		originalPath := req.URL.Path
		// Remove the /sessionId prefix from the path
		pathParts := strings.SplitN(originalPath, "/", 3)
		if len(pathParts) >= 3 {
			req.URL.Path = "/" + pathParts[2]
		} else {
			req.URL.Path = "/"
		}

		// Set forwarded headers
		originalHost := c.Request().Host
		if originalHost == "" {
			originalHost = c.Request().Header.Get("Host")
		}
		req.Header.Set("X-Forwarded-Host", originalHost)
		req.Header.Set("X-Forwarded-Proto", "http")
		if req.TLS != nil {
			req.Header.Set("X-Forwarded-Proto", "https")
		}
	}

	// Custom error handler
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("Proxy error for session %s: %v", sessionID, err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
	}

	if p.verbose {
		log.Printf("Routing request %s %s to session %s (port %d)", req.Method, req.URL.Path, sessionID, session.Port)
	}

	proxy.ServeHTTP(w, req)
	return nil
}

// getAvailablePort finds an available port starting from nextPort
func (p *Proxy) getAvailablePort() (int, error) {
	p.portMutex.Lock()
	defer p.portMutex.Unlock()

	startPort := p.nextPort
	for port := startPort; port < startPort+1000; port++ {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			if err := ln.Close(); err != nil {
				log.Printf("Failed to close listener: %v", err)
			}
			p.nextPort = port + 1
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available ports in range %d-%d", startPort, startPort+1000)
}

// runAgentAPIServer runs an agentapi server instance using exec.Command or scripts
func (p *Proxy) runAgentAPIServer(ctx context.Context, session *AgentSession, scriptName string) {
	var tmpScriptPath string

	defer func() {
		// Clean up temporary script file if it was created
		if tmpScriptPath != "" {
			if err := os.Remove(tmpScriptPath); err != nil && p.verbose {
				log.Printf("Failed to remove temporary script file %s: %v", tmpScriptPath, err)
			}
		}

		// Clean up session when server stops
		p.sessionsMutex.Lock()
		delete(p.sessions, session.ID)
		p.sessionsMutex.Unlock()

		if p.verbose {
			log.Printf("Cleaned up session %s", session.ID)
		}
	}()

	var cmd *exec.Cmd

	if scriptName != "" {
		// Extract script to temporary file
		var err error
		tmpScriptPath, err = p.extractScriptToTempFile(scriptName)
		if err != nil {
			log.Printf("Failed to extract script %s for session %s: %v", scriptName, session.ID, err)
			return
		}

		// Execute script with port as argument
		cmd = exec.CommandContext(ctx, "/bin/bash", tmpScriptPath, strconv.Itoa(session.Port))

		if p.verbose {
			log.Printf("Starting agentapi process for session %s on %d using script %s", session.ID, session.Port, scriptName)
			log.Printf("[VERBOSE] Executing command: /bin/bash %s %s", tmpScriptPath, strconv.Itoa(session.Port))
		}
	} else {
		// Use direct agentapi command (fallback)
		cmd = exec.CommandContext(ctx, "agentapi", "server", "--port", strconv.Itoa(session.Port))

		if p.verbose {
			log.Printf("Starting agentapi process for session %s on %d using direct command", session.ID, session.Port)
			log.Printf("[VERBOSE] Executing command: agentapi server --port %s", strconv.Itoa(session.Port))
		}
	}

	// Set process group ID for proper cleanup
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Store the command in the session
	session.Process = cmd

	// Start the process
	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start agentapi process for session %s: %v", session.ID, err)
		return
	}

	if p.verbose {
		log.Printf("AgentAPI process started for session %s (PID: %d)", session.ID, cmd.Process.Pid)
	}

	// Wait for the process to finish or context cancellation
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		// Context cancelled, terminate the process
		if p.verbose {
			log.Printf("Terminating agentapi process for session %s", session.ID)
		}

		// Try graceful shutdown first (SIGTERM)
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			log.Printf("Failed to send SIGTERM to process %d: %v", cmd.Process.Pid, err)
		}

		// Wait for graceful shutdown with timeout
		gracefulTimeout := time.After(5 * time.Second)
		select {
		case <-done:
			if p.verbose {
				log.Printf("AgentAPI process for session %s terminated gracefully", session.ID)
			}
		case <-gracefulTimeout:
			// Force kill if graceful shutdown failed
			if p.verbose {
				log.Printf("Force killing agentapi process for session %s", session.ID)
			}
			if err := cmd.Process.Kill(); err != nil {
				log.Printf("Failed to kill process %d: %v", cmd.Process.Pid, err)
			} else {
				<-done // Wait for the process to actually exit
			}
		}

	case err := <-done:
		// Process finished on its own
		if err != nil {
			log.Printf("AgentAPI process for session %s exited with error: %v", session.ID, err)
		} else if p.verbose {
			log.Printf("AgentAPI process for session %s exited normally", session.ID)
		}
	}
}

// extractScriptToTempFile extracts a script to a temporary file and returns the file path
func (p *Proxy) extractScriptToTempFile(scriptName string) (string, error) {
	// scriptCache からスクリプト内容を取得
	content, ok := scriptCache[scriptName]
	if !ok {
		return "", fmt.Errorf("script %s not found in embedded cache", scriptName)
	}

	// Create temporary file
	tmpFile, err := os.CreateTemp("", "agentapi-script-*.sh")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %v", err)
	}

	// Write script content to temporary file
	if _, err := tmpFile.Write(content); err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			log.Printf("Failed to close temp file: %v", closeErr)
		}
		if removeErr := os.Remove(tmpFile.Name()); removeErr != nil {
			log.Printf("Failed to remove temp file: %v", removeErr)
		}
		return "", fmt.Errorf("failed to write script content: %v", err)
	}

	// Make the file executable
	if err := tmpFile.Chmod(0755); err != nil {
		if closeErr := tmpFile.Close(); closeErr != nil {
			log.Printf("Failed to close temp file: %v", closeErr)
		}
		if removeErr := os.Remove(tmpFile.Name()); removeErr != nil {
			log.Printf("Failed to remove temp file: %v", removeErr)
		}
		return "", fmt.Errorf("failed to make script executable: %v", err)
	}

	if err := tmpFile.Close(); err != nil {
		log.Printf("Warning: failed to close temp file: %v", err)
	}
	return tmpFile.Name(), nil
}

// selectScript determines which script to use based on request parameters
func (p *Proxy) selectScript(c echo.Context, scriptCache map[string][]byte) string {
	if githubRepo := c.QueryParam("github_repo"); githubRepo != "" {
		if _, ok := scriptCache[ScriptWithGithub]; ok {
			return ScriptWithGithub
		}
	}
	if _, ok := scriptCache[ScriptDefault]; ok {
		return ScriptDefault
	}
	return ""
}

// GetEcho returns the Echo instance for external access
func (p *Proxy) GetEcho() *echo.Echo {
	return p.echo
}
