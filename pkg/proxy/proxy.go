package proxy

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
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
	"text/template"
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

// ScriptTemplateData holds data for script templates
type ScriptTemplateData struct {
	AgentAPIArgs              string
	ClaudeArgs                string
	GitHubToken               string
	GitHubAppID               string
	GitHubInstallationID      string
	GitHubAppPEMPath          string
	GitHubAPI                 string
	GitHubPersonalAccessToken string
	RepoFullName              string
	CloneDir                  string
}

// StartRequest represents the request body for starting a new agentapi server
type StartRequest struct {
	UserID      string            `json:"user_id,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Message     string            `json:"message,omitempty"`
}

// RepositoryInfo contains repository information extracted from tags
type RepositoryInfo struct {
	FullName string
	CloneDir string
}

// AgentSession represents a running agentapi server instance
type AgentSession struct {
	ID           string
	Port         int
	Process      *exec.Cmd
	Cancel       context.CancelFunc
	StartedAt    time.Time
	UserID       string
	Status       string
	Environment  map[string]string
	Tags         map[string]string
	processMutex sync.RWMutex
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

	// Add CORS middleware with proper configuration (only for non-proxy routes)
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		Skipper: func(c echo.Context) bool {
			// Skip CORS middleware for proxy routes (they handle CORS manually)
			path := c.Request().URL.Path
			return len(strings.Split(path, "/")) >= 3 && path != "/start" && path != "/search"
		},
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodPost, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization, "X-Requested-With", "X-Forwarded-For", "X-Forwarded-Proto", "X-Forwarded-Host"},
		AllowCredentials: true,
		MaxAge:           86400,
	}))

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
	p.echo.DELETE("/sessions/:sessionId", p.deleteSession)
	p.echo.Any("/:sessionId/*", p.routeToSession)
}

// searchSessions handles GET /search requests to list and filter sessions
func (p *Proxy) searchSessions(c echo.Context) error {
	userID := c.QueryParam("user_id")
	status := c.QueryParam("status")

	// Extract tag filters from query parameters
	tagFilters := make(map[string]string)
	for paramName, paramValues := range c.QueryParams() {
		if strings.HasPrefix(paramName, "tag.") && len(paramValues) > 0 {
			tagKey := strings.TrimPrefix(paramName, "tag.")
			tagFilters[tagKey] = paramValues[0]
		}
	}

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

		// Apply tag filters
		matchAllTags := true
		for tagKey, tagValue := range tagFilters {
			sessionTagValue, exists := session.Tags[tagKey]
			if !exists || sessionTagValue != tagValue {
				matchAllTags = false
				break
			}
		}
		if !matchAllTags {
			continue
		}

		sessionData := map[string]interface{}{
			"session_id": session.ID,
			"user_id":    session.UserID,
			"status":     session.Status,
			"started_at": session.StartedAt,
			"port":       session.Port,
			"tags":       session.Tags,
		}
		filteredSessions = append(filteredSessions, sessionData)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"sessions": filteredSessions,
	})
}

// deleteSession handles DELETE /sessions/:sessionId requests to terminate a session
func (p *Proxy) deleteSession(c echo.Context) error {
	sessionID := c.Param("sessionId")

	if sessionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Session ID is required")
	}

	p.sessionsMutex.Lock()
	session, exists := p.sessions[sessionID]
	if !exists {
		p.sessionsMutex.Unlock()
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}
	p.sessionsMutex.Unlock()

	// Cancel the session context to trigger graceful shutdown
	if session.Cancel != nil {
		session.Cancel()
	}

	if p.verbose {
		log.Printf("Initiated termination of session %s", sessionID)
	}

	// Return success response
	return c.JSON(http.StatusOK, map[string]interface{}{
		"message":    "Session terminated successfully",
		"session_id": sessionID,
	})
}

// startAgentAPIServer starts a new agentapi server instance and returns session ID
func (p *Proxy) startAgentAPIServer(c echo.Context) error {
	// Generate UUID for session
	sessionID := uuid.New().String()

	// Parse request body for environment variables and other parameters
	var startReq StartRequest

	// Try to parse JSON body, but don't fail if it's empty or invalid
	if err := c.Bind(&startReq); err != nil {
		if p.verbose {
			log.Printf("Failed to parse request body (using defaults): %v", err)
		}
	}

	// Get user_id from query parameters, request body, or default
	userID := c.QueryParam("user_id")
	if userID == "" && startReq.UserID != "" {
		userID = startReq.UserID
	}
	if userID == "" {
		userID = "anonymous"
	}

	// Find available port
	port, err := p.getAvailablePort()
	if err != nil {
		log.Printf("Failed to find available port: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to allocate port")
	}

	// Extract repository information from tags
	repoInfo := p.extractRepositoryInfo(sessionID, startReq.Tags)

	// Determine which script to use based on request parameters
	scriptName := p.selectScript(c, scriptCache, startReq.Tags)

	// Determine initial message - check tags.message first, then startReq.Message
	var initialMessage string
	if startReq.Tags != nil {
		if msg, exists := startReq.Tags["message"]; exists && msg != "" {
			initialMessage = msg
		}
	}
	if initialMessage == "" && startReq.Message != "" {
		initialMessage = startReq.Message
	}

	// Start agentapi server in goroutine
	ctx, cancel := context.WithCancel(context.Background())

	session := &AgentSession{
		ID:          sessionID,
		Port:        port,
		Cancel:      cancel,
		StartedAt:   time.Now(),
		UserID:      userID,
		Status:      "active",
		Environment: startReq.Environment,
		Tags:        startReq.Tags,
	}

	// Store session
	p.sessionsMutex.Lock()
	p.sessions[sessionID] = session
	p.sessionsMutex.Unlock()
	log.Printf("session: %+v", session)
	log.Printf("scriptName: %s", scriptName)
	// Start agentapi server in goroutine
	go p.runAgentAPIServer(ctx, session, scriptName, repoInfo, initialMessage)

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

	// Configure for streaming responses (SSE support)
	proxy.FlushInterval = time.Millisecond * 100 // Flush every 100ms for real-time streaming

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

	// Add CORS headers to response
	originalModifyResponse := proxy.ModifyResponse
	proxy.ModifyResponse = func(resp *http.Response) error {
		// Set CORS headers
		resp.Header.Set("Access-Control-Allow-Origin", "*")
		resp.Header.Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
		resp.Header.Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host")
		resp.Header.Set("Access-Control-Allow-Credentials", "true")
		resp.Header.Set("Access-Control-Max-Age", "86400")

		// Handle Server-Sent Events (SSE) specific headers
		if resp.Header.Get("Content-Type") == "text/event-stream" {
			// Ensure proper SSE headers are maintained
			resp.Header.Set("Cache-Control", "no-cache")
			resp.Header.Set("Connection", "keep-alive")
			// Don't set Content-Length for streaming responses
			resp.Header.Del("Content-Length")
		}

		if originalModifyResponse != nil {
			return originalModifyResponse(resp)
		}
		return nil
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
func (p *Proxy) runAgentAPIServer(ctx context.Context, session *AgentSession, scriptName string, repoInfo *RepositoryInfo, initialMessage string) {
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

	// Prepare template data with environment variables and repository info
	templateData := &ScriptTemplateData{
		AgentAPIArgs:              os.Getenv("AGENTAPI_ARGS"),
		ClaudeArgs:                os.Getenv("CLAUDE_ARGS"),
		GitHubToken:               os.Getenv("GITHUB_TOKEN"),
		GitHubAppID:               os.Getenv("GITHUB_APP_ID"),
		GitHubInstallationID:      os.Getenv("GITHUB_INSTALLATION_ID"),
		GitHubAppPEMPath:          os.Getenv("GITHUB_APP_PEM_PATH"),
		GitHubAPI:                 os.Getenv("GITHUB_API"),
		GitHubPersonalAccessToken: os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN"),
	}

	// Add repository information to template data if available
	if repoInfo != nil {
		templateData.RepoFullName = repoInfo.FullName
		templateData.CloneDir = repoInfo.CloneDir
	}

	if scriptName != "" {
		// Extract script to temporary file
		var err error
		tmpScriptPath, err = p.extractScriptToTempFile(scriptName, templateData)
		if err != nil {
			log.Printf("Failed to extract script %s for session %s: %v", scriptName, session.ID, err)
			return
		}

		// Execute script with port parameter (repository info is now embedded in template)
		args := []string{tmpScriptPath, strconv.Itoa(session.Port)}
		cmd = exec.CommandContext(ctx, "/bin/bash", args...)

		// Log script execution details
		log.Printf("Starting agentapi process for session %s on %d using script %s", session.ID, session.Port, scriptName)
		log.Printf("Script execution parameters:")
		log.Printf("  Script: %s", scriptName)
		log.Printf("  Port: %d", session.Port)
		log.Printf("  Session ID: %s", session.ID)
		if len(session.Tags) > 0 {
			log.Printf("  Request tags:")
			for key, value := range session.Tags {
				log.Printf("    %s=%s", key, value)
			}
		}
		log.Printf("  Full command: /bin/bash %s", strings.Join(args, " "))
		if p.verbose {
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

	// Set environment variables for the process
	// Start with the current environment from agentapi-proxy
	cmd.Env = os.Environ()

	// Log environment variable setup
	log.Printf("Environment variables for session %s:", session.ID)
	if len(session.Environment) > 0 {
		log.Printf("  Custom environment variables:")
		for key, value := range session.Environment {
			// Add or override environment variable
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
			log.Printf("    %s=%s", key, value)
		}
	} else {
		log.Printf("  Using default environment (no custom variables)")
	}

	// Log template arguments that are embedded in the script
	if scriptName != "" {
		log.Printf("  Template arguments embedded in script:")
		if templateData.AgentAPIArgs != "" {
			log.Printf("    AgentAPIArgs=%s", templateData.AgentAPIArgs)
		}
		if templateData.ClaudeArgs != "" {
			log.Printf("    ClaudeArgs=%s", templateData.ClaudeArgs)
		}
		if templateData.GitHubToken != "" {
			log.Printf("    GitHubToken=%s", maskToken(templateData.GitHubToken))
		}
		if templateData.GitHubAppID != "" {
			log.Printf("    GitHubAppID=%s", templateData.GitHubAppID)
		}
		if templateData.GitHubInstallationID != "" {
			log.Printf("    GitHubInstallationID=%s", templateData.GitHubInstallationID)
		}
		if templateData.GitHubAppPEMPath != "" {
			log.Printf("    GitHubAppPEMPath=%s", templateData.GitHubAppPEMPath)
		}
		if templateData.GitHubAPI != "" {
			log.Printf("    GitHubAPI=%s", templateData.GitHubAPI)
		}
		if templateData.GitHubPersonalAccessToken != "" {
			log.Printf("    GitHubPersonalAccessToken=%s", maskToken(templateData.GitHubPersonalAccessToken))
		}
		if templateData.RepoFullName != "" {
			log.Printf("    RepoFullName=%s", templateData.RepoFullName)
		}
		if templateData.CloneDir != "" {
			log.Printf("    CloneDir=%s", templateData.CloneDir)
		}
		if templateData.AgentAPIArgs == "" && templateData.ClaudeArgs == "" &&
			templateData.GitHubToken == "" && templateData.GitHubAppID == "" &&
			templateData.GitHubInstallationID == "" && templateData.GitHubAppPEMPath == "" &&
			templateData.GitHubAPI == "" && templateData.GitHubPersonalAccessToken == "" &&
			templateData.RepoFullName == "" && templateData.CloneDir == "" {
			log.Printf("    No template arguments specified")
		}
	}

	// Store the command in the session and start the process
	session.processMutex.Lock()
	session.Process = cmd
	err := cmd.Start()
	session.processMutex.Unlock()

	if err != nil {
		log.Printf("Failed to start agentapi process for session %s: %v", session.ID, err)
		return
	}

	if p.verbose {
		log.Printf("AgentAPI process started for session %s (PID: %d)", session.ID, cmd.Process.Pid)
	}

	// Send initial message if provided
	if initialMessage != "" {
		go p.sendInitialMessage(session, initialMessage)
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
func (p *Proxy) extractScriptToTempFile(scriptName string, templateData *ScriptTemplateData) (string, error) {
	// scriptCache からスクリプト内容を取得
	content, ok := scriptCache[scriptName]
	if !ok {
		return "", fmt.Errorf("script %s not found in embedded cache", scriptName)
	}

	// Process content as a template if templateData is provided
	var processedContent []byte
	if templateData != nil {
		tmpl, err := template.New(scriptName).Parse(string(content))
		if err != nil {
			return "", fmt.Errorf("failed to parse script template: %v", err)
		}

		var buf strings.Builder
		if err := tmpl.Execute(&buf, templateData); err != nil {
			return "", fmt.Errorf("failed to execute script template: %v", err)
		}
		processedContent = []byte(buf.String())
	} else {
		processedContent = content
	}

	// Log script content when verbose mode is enabled
	if p.verbose {
		log.Printf("[VERBOSE] Script content for %s:", scriptName)
		log.Printf("--- Script Start ---")
		log.Printf("%s", string(processedContent))
		log.Printf("--- Script End ---")
	}

	// Create temporary file
	tmpFile, err := os.CreateTemp("", "agentapi-script-*.sh")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %v", err)
	}

	// Write script content to temporary file
	if _, err := tmpFile.Write(processedContent); err != nil {
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
func (p *Proxy) selectScript(c echo.Context, scriptCache map[string][]byte, tags map[string]string) string {
	// Always use default script
	if _, ok := scriptCache[ScriptDefault]; ok {
		return ScriptDefault
	}
	return ""
}

// Shutdown gracefully stops all running sessions and waits for them to terminate
func (p *Proxy) Shutdown(timeout time.Duration) error {
	log.Printf("Shutting down proxy, terminating %d active sessions...", len(p.sessions))

	// Get all session cancel functions
	p.sessionsMutex.RLock()
	var sessions []*AgentSession
	for _, session := range p.sessions {
		sessions = append(sessions, session)
	}
	p.sessionsMutex.RUnlock()

	if len(sessions) == 0 {
		log.Printf("No active sessions to terminate")
		return nil
	}

	// Cancel all sessions
	for _, session := range sessions {
		if session.Cancel != nil {
			session.processMutex.RLock()
			process := session.Process
			session.processMutex.RUnlock()

			if process != nil && process.Process != nil {
				log.Printf("Terminating session %s (PID: %d)", session.ID, process.Process.Pid)
			} else {
				log.Printf("Terminating session %s", session.ID)
			}
			session.Cancel()
		}
	}

	// Wait for all sessions to complete with timeout
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			p.sessionsMutex.RLock()
			remaining := len(p.sessions)
			p.sessionsMutex.RUnlock()

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
		p.sessionsMutex.RLock()
		remaining := len(p.sessions)
		p.sessionsMutex.RUnlock()
		log.Printf("Timeout reached, %d sessions may still be running", remaining)
		return fmt.Errorf("shutdown timeout reached with %d sessions still running", remaining)
	}
}

// extractRepositoryInfo extracts repository information from tags
func (p *Proxy) extractRepositoryInfo(sessionID string, tags map[string]string) *RepositoryInfo {
	if tags == nil {
		return nil
	}

	repoURL, exists := tags["repository"]
	if !exists || repoURL == "" {
		return nil
	}

	// Only process repository URLs that look like valid GitHub URLs
	if !isValidRepositoryURL(repoURL) {
		if p.verbose {
			log.Printf("Repository tag found: %s, but it's not a valid repository URL. Skipping repository setup.", repoURL)
		}
		return nil
	}

	if p.verbose {
		log.Printf("Repository tag found: %s. Will pass to script as parameters.", repoURL)
	}

	// Extract org/repo format from repository URL
	repoFullName, err := extractRepoFullNameFromURL(repoURL)
	if err != nil {
		log.Printf("Failed to extract repository full name from URL %s: %v", repoURL, err)
		return nil
	}

	if p.verbose {
		log.Printf("Extracted repository info - FullName: %s, CloneDir: %s", repoFullName, sessionID)
	}

	return &RepositoryInfo{
		FullName: repoFullName,
		CloneDir: sessionID,
	}
}

// isValidRepositoryURL checks if a repository URL is valid for GitHub
func isValidRepositoryURL(repoURL string) bool {
	// Check for common GitHub URL patterns
	if strings.HasPrefix(repoURL, "https://github.com/") ||
		strings.HasPrefix(repoURL, "git@github.com:") ||
		strings.HasPrefix(repoURL, "http://github.com/") {
		return true
	}

	// Check for owner/repo format (e.g., "takutakahashi/agentapi-ui")
	parts := strings.Split(repoURL, "/")
	if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		// Simple validation: both owner and repo name should not be empty
		// and should not contain invalid characters for GitHub usernames/repo names
		return true
	}

	return false
}

// extractRepoFullNameFromURL extracts the org/repo format from a GitHub repository URL
func extractRepoFullNameFromURL(repoURL string) (string, error) {
	var repoPath string

	if strings.HasPrefix(repoURL, "https://github.com/") {
		repoPath = strings.TrimPrefix(repoURL, "https://github.com/")
	} else if strings.HasPrefix(repoURL, "git@github.com:") {
		repoPath = strings.TrimPrefix(repoURL, "git@github.com:")
	} else if strings.HasPrefix(repoURL, "http://github.com/") {
		repoPath = strings.TrimPrefix(repoURL, "http://github.com/")
	} else {
		// If it's not a full URL, assume it's already in owner/repo format
		repoPath = repoURL
	}

	// Remove .git suffix if present
	repoPath = strings.TrimSuffix(repoPath, ".git")

	// Split into org/repo
	parts := strings.Split(repoPath, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid repository path: %s", repoPath)
	}

	return repoPath, nil
}

// maskToken masks sensitive tokens for logging
func maskToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return "****"
	}
	return token[:4] + "****" + token[len(token)-4:]
}

// sendInitialMessage sends an initial message to the agentapi server after startup
func (p *Proxy) sendInitialMessage(session *AgentSession, message string) {
	// Wait a bit for the server to start up
	time.Sleep(2 * time.Second)

	// Check server health first
	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d/health", session.Port))
		if err == nil {
			if closeErr := resp.Body.Close(); closeErr != nil {
				log.Printf("Failed to close response body: %v", closeErr)
			}
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if i == maxRetries-1 {
			log.Printf("AgentAPI server for session %s not ready after %d retries, skipping initial message", session.ID, maxRetries)
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
		log.Printf("Failed to marshal message request for session %s: %v", session.ID, err)
		return
	}

	// Send message to agentapi
	url := fmt.Sprintf("http://localhost:%d/message", session.Port)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		log.Printf("Failed to send initial message to session %s: %v", session.ID, err)
		return
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("Failed to close response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("Failed to send initial message to session %s (status: %d): %s", session.ID, resp.StatusCode, string(body))
		return
	}

	log.Printf("Successfully sent initial message to session %s", session.ID)
}

// GetEcho returns the Echo instance for external access
func (p *Proxy) GetEcho() *echo.Echo {
	return p.echo
}
