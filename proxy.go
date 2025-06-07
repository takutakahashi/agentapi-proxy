package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/agentapi"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// AgentSession represents a running agentapi server instance
type AgentSession struct {
	ID       string
	Port     int
	Server   *http.Server
	Cancel   context.CancelFunc
	StartedAt time.Time
}

// Proxy represents the HTTP proxy server
type Proxy struct {
	config        *Config
	echo          *echo.Echo
	verbose       bool
	sessions      map[string]*AgentSession
	sessionsMutex sync.RWMutex
	nextPort      int
}

// NewProxy creates a new proxy instance
func NewProxy(config *Config, verbose bool) *Proxy {
	e := echo.New()
	
	// Disable Echo's default logger and use custom logging
	e.Logger.SetOutput(io.Discard)
	
	// Add recovery middleware
	e.Use(middleware.Recover())
	
	p := &Proxy{
		config:        config,
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
	// Add agentapi session management routes
	p.echo.POST("/start", p.startAgentAPIServer)
	p.echo.Any("/:sessionId/*", p.routeToSession)
	
	// Add existing configured routes
	for pattern, backend := range p.config.Routes {
		p.addRoute(pattern, backend)
	}

	// Add default handler for unmatched routes - Echo will handle this automatically
	// if no routes match, but we can add a catch-all route
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
	go p.runAgentAPIServer(ctx, session)
	
	if p.verbose {
		log.Printf("Started agentapi server for session %s on port %d", sessionID, port)
	}
	
	// Return session ID
	return c.JSON(http.StatusOK, map[string]interface{}{
		"session_id": sessionID,
		"port":       port,
		"started_at": session.StartedAt,
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
		
		// Remove session ID from path
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

// runAgentAPIServer runs an agentapi server instance
func (p *Proxy) runAgentAPIServer(ctx context.Context, session *AgentSession) {
	defer func() {
		// Clean up session when server stops
		p.sessionsMutex.Lock()
		delete(p.sessions, session.ID)
		p.sessionsMutex.Unlock()
		
		if p.verbose {
			log.Printf("Cleaned up session %s", session.ID)
		}
	}()
	
	// Create agentapi server configuration
	serverAddr := fmt.Sprintf(":%d", session.Port)
	
	// Create agentapi server instance
	server, err := agentapi.NewServer(agentapi.ServerOptions{
		Address: serverAddr,
	})
	if err != nil {
		log.Printf("Failed to create agentapi server for session %s: %v", session.ID, err)
		return
	}
	
	// Create HTTP server with agentapi handler
	srv := &http.Server{
		Addr:    serverAddr,
		Handler: server,
	}
	
	session.Server = srv
	
	// Start server in a goroutine
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("AgentAPI server error for session %s: %v", session.ID, err)
		}
	}()
	
	if p.verbose {
		log.Printf("AgentAPI server started for session %s on %s", session.ID, serverAddr)
	}
	
	// Wait for context cancellation
	<-ctx.Done()
	
	// Shutdown server gracefully
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Error shutting down agentapi server for session %s: %v", session.ID, err)
	} else if p.verbose {
		log.Printf("AgentAPI server stopped for session %s", session.ID)
	}
}

// GetEcho returns the Echo instance for external access
func (p *Proxy) GetEcho() *echo.Echo {
	return p.echo
}