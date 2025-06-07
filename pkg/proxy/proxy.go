package proxy

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
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
var scriptsFS embed.FS

// AgentSession represents a running agentapi server instance
type AgentSession struct {
	ID        string
	Port      int
	Process   *exec.Cmd
	Cancel    context.CancelFunc
	StartedAt time.Time
}

// Proxy represents the HTTP proxy server
type Proxy struct {
	config        *config.Config
	echo          *echo.Echo
	verbose       bool
	sessions      map[string]*AgentSession
	sessionsMutex sync.RWMutex
	nextPort      int
}

// NewProxy creates a new proxy instance
func NewProxy(cfg *config.Config, verbose bool) *Proxy {
	e := echo.New()

	// Disable Echo's default logger and use custom logging
	e.Logger.SetOutput(io.Discard)

	// Add recovery middleware
	e.Use(middleware.Recover())

	p := &Proxy{
		config:        cfg,
		echo:          e,
		verbose:       verbose,
		sessions:      make(map[string]*AgentSession),
		sessionsMutex: sync.RWMutex{},
		nextPort:      9000, // Starting port for agentapi servers
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
	// Add existing configured routes first (more specific routes)
	for pattern, backend := range p.config.Routes {
		p.addRoute(pattern, backend)
	}

	// Add agentapi session management routes with specific prefix
	p.echo.POST("/sessions/start", p.startAgentAPIServer)
	p.echo.Any("/sessions/:sessionId/*", p.routeToSession)

	// Add default handler for unmatched routes
	p.echo.Any("/*", p.defaultHandler)
}

// addRoute adds a single route to the router
func (p *Proxy) addRoute(pattern, backend string) {
	// Convert gorilla/mux pattern {param} to Echo pattern :param
	echoPattern := strings.ReplaceAll(pattern, "{", ":")
	echoPattern = strings.ReplaceAll(echoPattern, "}", "")

	if p.verbose {
		log.Printf("Adding route: %s -> %s (Echo pattern: %s)", pattern, backend, echoPattern)
	}

	handler := p.createProxyHandler(backend)
	p.echo.Any(echoPattern, handler)
}

// createProxyHandler creates a reverse proxy handler for the given backend
func (p *Proxy) createProxyHandler(backendURL string) echo.HandlerFunc {
	return func(c echo.Context) error {
		// Parse the backend URL
		target, err := url.Parse(backendURL)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Invalid backend URL: %v", err))
		}

		// Get request and response from Echo context
		req := c.Request()
		w := c.Response()

		// Create reverse proxy
		proxy := httputil.NewSingleHostReverseProxy(target)

		// Customize the director to preserve the original path
		originalDirector := proxy.Director
		proxy.Director = func(req *http.Request) {
			originalDirector(req)
			// Preserve the original Host header from the Echo context
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
			log.Printf("Proxy error for %s: %v", r.URL.Path, err)
			http.Error(w, "Bad Gateway", http.StatusBadGateway)
		}

		if p.verbose {
			log.Printf("Proxying %s %s to %s", req.Method, req.URL.Path, target)
		}

		proxy.ServeHTTP(w, req)
		return nil
	}
}

// defaultHandler handles requests that don't match any configured routes
func (p *Proxy) defaultHandler(c echo.Context) error {
	if p.config.DefaultBackend != "" {
		handler := p.createProxyHandler(p.config.DefaultBackend)
		return handler(c)
	}

	// No default backend configured, return 404
	req := c.Request()
	if p.verbose {
		log.Printf("No route found for %s %s", req.Method, req.URL.Path)
	}
	return echo.NewHTTPError(http.StatusNotFound, "Not Found")
}

// startAgentAPIServer starts a new agentapi server instance and returns session ID
func (p *Proxy) startAgentAPIServer(c echo.Context) error {
	// Generate UUID for session
	sessionID := uuid.New().String()

	// Find available port
	port, err := p.getAvailablePort()
	if err != nil {
		log.Printf("Failed to find available port: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to allocate port")
	}

	// Determine which script to use based on request parameters
	scriptName := p.selectScript(c)

	// Start agentapi server in goroutine
	ctx, cancel := context.WithCancel(context.Background())

	session := &AgentSession{
		ID:        sessionID,
		Port:      port,
		Cancel:    cancel,
		StartedAt: time.Now(),
	}

	// Store session
	p.sessionsMutex.Lock()
	p.sessions[sessionID] = session
	p.sessionsMutex.Unlock()

	// Start agentapi server in goroutine
	go p.runAgentAPIServer(ctx, session, scriptName)

	if p.verbose {
		if scriptName != "" {
			log.Printf("Started agentapi server for session %s on port %d using script %s", sessionID, port, scriptName)
		} else {
			log.Printf("Started agentapi server for session %s on port %d using direct command", sessionID, port)
		}
	}

	// Return session ID
	return c.JSON(http.StatusOK, map[string]interface{}{
		"session_id": sessionID,
		"port":       port,
		"started_at": session.StartedAt,
		"script":     scriptName,
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

		// Remove /sessions/sessionId prefix from path
		originalPath := req.URL.Path
		// Remove the /sessions/sessionId prefix from the path
		pathParts := strings.SplitN(originalPath, "/", 4)
		if len(pathParts) >= 4 {
			req.URL.Path = "/" + pathParts[3]
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
	for port := p.nextPort; port < p.nextPort+1000; port++ {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			ln.Close()
			p.nextPort = port + 1
			return port, nil
		}
	}
	return 0, fmt.Errorf("no available ports in range %d-%d", p.nextPort, p.nextPort+1000)
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
		}
	} else {
		// Use direct agentapi command (fallback)
		cmd = exec.CommandContext(ctx, "agentapi", "server", "--port", strconv.Itoa(session.Port))
		
		if p.verbose {
			log.Printf("Starting agentapi process for session %s on %d using direct command", session.ID, session.Port)
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

// extractScriptToTempFile extracts an embedded script to a temporary file and returns the file path
func (p *Proxy) extractScriptToTempFile(scriptName string) (string, error) {
	// Read the script content from embedded filesystem
	scriptPath := filepath.Join("scripts", scriptName)
	content, err := scriptsFS.ReadFile(scriptPath)
	if err != nil {
		return "", fmt.Errorf("failed to read embedded script %s: %v", scriptName, err)
	}

	// Create temporary file
	tmpFile, err := ioutil.TempFile("", fmt.Sprintf("agentapi-script-*.sh"))
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %v", err)
	}

	// Write script content to temporary file
	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to write script content: %v", err)
	}

	// Make the file executable
	if err := tmpFile.Chmod(0755); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to make script executable: %v", err)
	}

	tmpFile.Close()
	return tmpFile.Name(), nil
}

// selectScript determines which script to use based on request parameters
func (p *Proxy) selectScript(c echo.Context) string {
	// Check if github_repo parameter is present
	if githubRepo := c.QueryParam("github_repo"); githubRepo != "" {
		return "agentapi_with_github.sh"
	}

	// Default to direct agentapi execution (no script)
	return ""
}

// GetEcho returns the Echo instance for external access
func (p *Proxy) GetEcho() *echo.Echo {
	return p.echo
}
