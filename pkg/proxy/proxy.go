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
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/takutakahashi/agentapi-proxy/internal/di"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/logger"
	"github.com/takutakahashi/agentapi-proxy/pkg/notification"
	"github.com/takutakahashi/agentapi-proxy/pkg/userdir"
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
	UserID                    string
	MCPConfigs                string
}

// StartRequest represents the request body for starting a new agentapi server
type StartRequest struct {
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
	config             *config.Config
	echo               *echo.Echo
	verbose            bool
	sessions           map[string]*AgentSession
	sessionsMutex      sync.RWMutex
	nextPort           int
	portMutex          sync.Mutex
	logger             *logger.Logger
	oauthProvider      *auth.GitHubOAuthProvider
	githubAuthProvider *auth.GitHubAuthProvider
	oauthSessions      sync.Map // sessionID -> OAuthSession
	userDirMgr         *userdir.Manager
	notificationSvc    *notification.Service
	sessionMonitor     *SessionMonitor
	container          *di.Container // Internal DI container
}

// NewProxy creates a new proxy instance
func NewProxy(cfg *config.Config, verbose bool) *Proxy {
	e := echo.New()

	// Disable Echo's default logger and use custom logging
	e.Logger.SetOutput(io.Discard)

	// Add recovery middleware
	e.Use(middleware.Recover())

	// Add security headers middleware
	e.Use(middleware.SecureWithConfig(middleware.SecureConfig{
		XSSProtection:         "1; mode=block",
		ContentTypeNosniff:    "nosniff",
		XFrameOptions:         "DENY",
		HSTSMaxAge:            31536000, // 1 year
		HSTSExcludeSubdomains: false,
		ContentSecurityPolicy: "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'",
		ReferrerPolicy:        "strict-origin-when-cross-origin",
	}))

	// Add CORS middleware with secure configuration (only for non-proxy routes)
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		Skipper: func(c echo.Context) bool {
			// Skip CORS middleware only for proxy routes (/:sessionId/* pattern)
			// These routes handle CORS manually in the proxy
			path := c.Request().URL.Path
			pathParts := strings.Split(path, "/")
			// Skip CORS only for proxy routes that match /:sessionId/* pattern
			// (at least 3 parts, not starting with "start", "search", "sessions", "oauth", "auth", "notification", or "notifications")
			if len(pathParts) >= 3 && pathParts[1] != "" {
				firstSegment := pathParts[1]
				return firstSegment != "start" && firstSegment != "search" && firstSegment != "sessions" && firstSegment != "oauth" && firstSegment != "auth" && firstSegment != "notification" && firstSegment != "notifications"
			}
			return false
		},
		AllowOriginFunc: func(origin string) (bool, error) {
			// Get allowed origins from environment variable
			allowedOrigins := os.Getenv("ALLOWED_ORIGINS")
			if allowedOrigins == "" {
				// Fallback to localhost for development
				allowed := strings.HasPrefix(origin, "http://localhost") ||
					strings.HasPrefix(origin, "https://localhost") ||
					strings.HasPrefix(origin, "http://127.0.0.1") ||
					strings.HasPrefix(origin, "https://127.0.0.1")
				return allowed, nil
			}
			// Parse comma-separated allowed origins
			origins := strings.Split(allowedOrigins, ",")
			for _, allowed := range origins {
				if strings.TrimSpace(allowed) == origin {
					return true, nil
				}
			}
			return false, nil
		},
		AllowMethods:     []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodPost, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization, "X-Requested-With", "X-Forwarded-For", "X-Forwarded-Proto", "X-Forwarded-Host", "X-API-Key"},
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

	// Initialize user directory manager
	userDirMgr := userdir.NewManager("./data", cfg.EnableMultipleUsers)

	// Initialize internal DI container
	container := di.NewContainer()

	p := &Proxy{
		config:        cfg,
		echo:          e,
		verbose:       verbose,
		sessions:      make(map[string]*AgentSession),
		sessionsMutex: sync.RWMutex{},
		nextPort:      cfg.StartPort,
		logger:        logger.NewLogger(),
		userDirMgr:    userDirMgr,
		container:     container,
	}

	// Add logging middleware if verbose
	if verbose {
		e.Use(p.loggingMiddleware())
	}

	// Initialize GitHub auth provider if configured
	if cfg.Auth.GitHub != nil && cfg.Auth.GitHub.Enabled {
		log.Printf("[AUTH_INIT] Initializing GitHub auth provider...")
		p.githubAuthProvider = auth.NewGitHubAuthProvider(cfg.Auth.GitHub)
		log.Printf("[AUTH_INIT] GitHub auth provider initialized successfully")

		// Configure the internal auth service with GitHub settings
		if simpleAuth, ok := container.AuthService.(*services.SimpleAuthService); ok {
			simpleAuth.SetGitHubAuthConfig(cfg.Auth.GitHub)
			log.Printf("[AUTH_INIT] GitHub auth config set for internal auth service")
		}
	}

	// Add authentication middleware using internal auth service
	e.Use(auth.AuthMiddleware(cfg, container.AuthService))

	// Initialize OAuth provider if configured
	log.Printf("[OAUTH_INIT] Checking OAuth configuration...")
	log.Printf("[OAUTH_INIT] cfg.Auth.GitHub != nil: %v", cfg.Auth.GitHub != nil)
	if cfg.Auth.GitHub != nil {
		log.Printf("[OAUTH_INIT] cfg.Auth.GitHub.OAuth != nil: %v", cfg.Auth.GitHub.OAuth != nil)
		if cfg.Auth.GitHub.OAuth != nil {
			log.Printf("[OAUTH_INIT] OAuth ClientID configured: %v", cfg.Auth.GitHub.OAuth.ClientID != "")
			log.Printf("[OAUTH_INIT] OAuth ClientSecret configured: %v", cfg.Auth.GitHub.OAuth.ClientSecret != "")
		}
	}
	if cfg.Auth.GitHub != nil && cfg.Auth.GitHub.OAuth != nil &&
		cfg.Auth.GitHub.OAuth.ClientID != "" && cfg.Auth.GitHub.OAuth.ClientSecret != "" {
		log.Printf("[OAUTH_INIT] Initializing GitHub OAuth provider...")
		p.oauthProvider = auth.NewGitHubOAuthProvider(cfg.Auth.GitHub.OAuth, cfg.Auth.GitHub)
		log.Printf("[OAUTH_INIT] OAuth provider initialized successfully")
		// Start cleanup goroutine for expired OAuth sessions
		go p.cleanupExpiredOAuthSessions()
	} else {
		log.Printf("[OAUTH_INIT] OAuth provider not initialized - configuration missing or incomplete")
		if cfg.Auth.GitHub != nil && cfg.Auth.GitHub.OAuth != nil {
			log.Printf("[OAUTH_INIT] OAuth configuration found but credentials are empty")
		}
	}

	// Initialize notification service
	baseDir := notification.GetBaseDir()
	notificationSvc, err := notification.NewService(baseDir)
	if err != nil {
		log.Printf("Failed to initialize notification service: %v", err)
	} else {
		p.notificationSvc = notificationSvc
		log.Printf("Notification service initialized successfully")
	}

	// Start cleanup goroutine for defunct processes
	go p.cleanupDefunctProcesses()

	// Initialize session monitor (will be started later if not in test mode)
	p.sessionMonitor = NewSessionMonitor(p, 3*time.Minute)

	p.setupRoutes()

	return p
}

// StartMonitoring starts the session monitoring (called after proxy is fully initialized)
func (p *Proxy) StartMonitoring() {
	// Session monitoring disabled - notifications handled by Claude Code hooks
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
	// Register health controller routes
	p.container.HealthController.RegisterRoutes(p.echo)

	// Add session management routes according to API specification
	p.echo.POST("/start", p.startAgentAPIServer, auth.RequirePermission(entities.PermissionSessionCreate, p.container.AuthService))
	p.echo.GET("/search", p.searchSessions, auth.RequirePermission(entities.PermissionSessionRead, p.container.AuthService))
	p.echo.DELETE("/sessions/:sessionId", p.deleteSession, auth.RequirePermission(entities.PermissionSessionDelete, p.container.AuthService))

	// Add authentication info routes
	authInfoHandlers := NewAuthInfoHandlers(p.config)
	p.echo.GET("/auth/types", authInfoHandlers.GetAuthTypes)
	p.echo.GET("/auth/status", authInfoHandlers.GetAuthStatus)

	// Add notification routes if service is available
	if p.notificationSvc != nil {
		notificationHandlers := NewNotificationHandlers(p.notificationSvc)
		// UI-compatible routes (proxied from agentapi-ui)
		p.echo.POST("/notification/subscribe", notificationHandlers.Subscribe, auth.RequirePermission(entities.PermissionSessionRead, p.container.AuthService))
		p.echo.GET("/notification/subscribe", notificationHandlers.GetSubscriptions, auth.RequirePermission(entities.PermissionSessionRead, p.container.AuthService))
		p.echo.DELETE("/notification/subscribe", notificationHandlers.DeleteSubscription, auth.RequirePermission(entities.PermissionSessionRead, p.container.AuthService))

		// Internal routes
		p.echo.POST("/notifications/webhook", notificationHandlers.Webhook)
		p.echo.GET("/notifications/history", notificationHandlers.GetHistory, auth.RequirePermission(entities.PermissionSessionRead, p.container.AuthService))
	}

	// Add OAuth routes if OAuth is configured
	log.Printf("[ROUTES] OAuth provider configured: %v", p.oauthProvider != nil)
	if p.oauthProvider != nil {
		log.Printf("[ROUTES] Registering OAuth endpoints...")
		// OAuth endpoints don't require existing authentication
		p.echo.POST("/oauth/authorize", p.handleOAuthLogin)
		p.echo.GET("/oauth/callback", p.handleOAuthCallback)
		p.echo.POST("/oauth/logout", p.handleOAuthLogout)
		p.echo.POST("/oauth/refresh", p.handleOAuthRefresh)
		log.Printf("[ROUTES] OAuth endpoints registered: /oauth/authorize, /oauth/callback, /oauth/logout, /oauth/refresh")
	} else {
		log.Printf("[ROUTES] OAuth endpoints not registered - OAuth provider not configured")
	}

	// Add explicit OPTIONS handler for DELETE endpoint to ensure CORS preflight works
	p.echo.OPTIONS("/sessions/:sessionId", func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})
	// Add explicit OPTIONS handler for session proxy routes to ensure CORS preflight works
	p.echo.OPTIONS("/:sessionId/*", func(c echo.Context) error {
		// Set CORS headers for preflight
		c.Response().Header().Set("Access-Control-Allow-Origin", "*")
		c.Response().Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
		c.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
		c.Response().Header().Set("Access-Control-Allow-Credentials", "true")
		c.Response().Header().Set("Access-Control-Max-Age", "86400")
		return c.NoContent(http.StatusNoContent)
	})
	p.echo.Any("/:sessionId/*", p.routeToSession, auth.RequirePermission(entities.PermissionSessionRead, p.container.AuthService))
}

// searchSessions handles GET /search requests to list and filter sessions
func (p *Proxy) searchSessions(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	status := c.QueryParam("status")

	// Determine userID for filtering based on authentication
	var userID string
	if user != nil && !user.IsAdmin() {
		// Non-admin users can only see their own sessions
		userID = string(user.ID())
	}
	// Admin users can see all sessions (userID remains empty for no filtering)

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

	// First, collect matching sessions
	matchingSessions := make([]*AgentSession, 0)

	for _, session := range p.sessions {
		// Apply user filtering based on role
		if user != nil && !user.IsAdmin() && session.UserID != string(user.ID()) {
			continue
		}

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

		matchingSessions = append(matchingSessions, session)
	}

	// Sort sessions by creation time (newest first)
	sort.Slice(matchingSessions, func(i, j int) bool {
		return matchingSessions[i].StartedAt.After(matchingSessions[j].StartedAt)
	})

	// Convert sorted sessions to response format
	filteredSessions := make([]map[string]interface{}, 0, len(matchingSessions))
	for _, session := range matchingSessions {
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
	clientIP := c.RealIP()

	log.Printf("Request: DELETE /sessions/%s from %s", sessionID, clientIP)

	if sessionID == "" {
		log.Printf("Delete session failed: missing session ID from %s", clientIP)
		return echo.NewHTTPError(http.StatusBadRequest, "Session ID is required")
	}

	p.sessionsMutex.RLock()
	session, exists := p.sessions[sessionID]
	sessionStatus := "unknown"
	if exists {
		sessionStatus = session.Status
	}
	p.sessionsMutex.RUnlock()

	if !exists {
		log.Printf("Delete session failed: session %s not found (requested by %s)", sessionID, clientIP)
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	// Check if user has access to this session
	if !auth.UserOwnsSession(c, session.UserID) {
		log.Printf("Delete session failed: user does not own session %s (requested by %s)", sessionID, clientIP)
		return echo.NewHTTPError(http.StatusForbidden, "You can only delete your own sessions")
	}

	log.Printf("Deleting session %s (status: %s, user: %s) requested by %s",
		sessionID, sessionStatus, session.UserID, clientIP)

	// Cancel the session context to trigger graceful shutdown
	if session.Cancel != nil {
		session.Cancel()
		log.Printf("Successfully cancelled context for session %s", sessionID)
	} else {
		log.Printf("Warning: session %s had no cancel function", sessionID)
	}

	// Wait for session cleanup with timeout
	maxWaitTime := 5 * time.Second
	waitInterval := 50 * time.Millisecond
	startTime := time.Now()

	for {
		// Check if session was actually cleaned up
		p.sessionsMutex.RLock()
		_, stillExists := p.sessions[sessionID]
		p.sessionsMutex.RUnlock()

		if !stillExists {
			log.Printf("Session %s successfully removed from active sessions", sessionID)
			break
		}

		// Check if we've exceeded the maximum wait time
		if time.Since(startTime) >= maxWaitTime {
			log.Printf("Warning: session %s still exists after %v, forcing removal", sessionID, maxWaitTime)

			// Force remove the session from the map
			p.sessionsMutex.Lock()
			delete(p.sessions, sessionID)
			p.sessionsMutex.Unlock()

			break
		}

		time.Sleep(waitInterval)
	}

	log.Printf("Session %s deletion completed successfully", sessionID)

	// Log session end with estimated message count
	// Since we don't track actual message count, we'll use 0 as placeholder
	if err := p.logger.LogSessionEnd(sessionID, 0); err != nil {
		log.Printf("Failed to log session end for %s: %v", sessionID, err)
	}

	// Clean up session working directory only on explicit deletion
	// Safety check: ensure sessionID is not empty
	if sessionID != "" {
		workDir := fmt.Sprintf("/home/agentapi/workdir/%s", sessionID)
		if _, err := os.Stat(workDir); err == nil {
			log.Printf("Removing session working directory: %s", workDir)
			if err := os.RemoveAll(workDir); err != nil {
				log.Printf("Failed to remove session working directory %s: %v", workDir, err)
			} else {
				log.Printf("Successfully removed session working directory: %s", workDir)
			}
		}
	} else {
		log.Printf("WARNING: Attempted to delete working directory with empty session ID - operation skipped")
	}

	// Return success response
	return c.JSON(http.StatusOK, map[string]interface{}{
		"message":    "Session terminated successfully",
		"session_id": sessionID,
		"status":     "terminated",
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

	// Get user_id from authenticated context
	user := auth.GetUserFromContext(c)
	var userID string
	var userRole string
	if user != nil {
		userID = string(user.ID())
		// Get first role or default to "user"
		if len(user.Roles()) > 0 {
			userRole = string(user.Roles()[0])
		} else {
			userRole = "user"
		}
	} else {
		userID = "anonymous"
		userRole = "guest"
	}

	// Get auth team env file from user context if available
	var authTeamEnvFile string
	if user != nil && user.EnvFile() != "" {
		authTeamEnvFile = user.EnvFile()
		log.Printf("[ENV] Auth team env file from user context: %s", authTeamEnvFile)
	}

	// Merge environment variables from multiple sources
	envConfig := EnvMergeConfig{
		RoleEnvFiles:    &p.config.RoleEnvFiles,
		UserRole:        userRole,
		TeamEnvFile:     ExtractTeamEnvFile(startReq.Tags),
		AuthTeamEnvFile: authTeamEnvFile,
		RequestEnv:      startReq.Environment,
	}

	mergedEnv, err := MergeEnvironmentVariables(envConfig)
	if err != nil {
		log.Printf("[ENV] Failed to merge environment variables: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to merge environment variables")
	}

	// Replace the request environment with merged values
	startReq.Environment = mergedEnv

	// Debug log merged environment variables
	if len(mergedEnv) > 0 {
		log.Printf("[ENV] Merged environment variables (%d):", len(mergedEnv))
		for key, value := range mergedEnv {
			log.Printf("[ENV]   %s=%s", key, value)
		}
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

	log.Printf("[SESSION_CREATED] ID: %s, Port: %d, User: %s, Tags: %v",
		sessionID, port, userID, startReq.Tags)

	log.Printf("session: %+v", session)
	log.Printf("scriptName: %s", scriptName)

	// Log session start
	repository := ""
	if repoInfo != nil {
		repository = repoInfo.FullName
	}
	if err := p.logger.LogSessionStart(sessionID, repository); err != nil {
		log.Printf("Failed to log session start for %s: %v", sessionID, err)
	}

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

	// Skip session access check for OPTIONS requests (CORS preflight)
	if c.Request().Method == "OPTIONS" {
		// For OPTIONS requests, skip session access validation
		// since auth middleware already skipped authentication
	} else {
		// Check if user has access to this session (only if auth is enabled)
		cfg := auth.GetConfigFromContext(c)
		if cfg != nil && cfg.Auth.Enabled {
			if !auth.UserOwnsSession(c, session.UserID) {
				log.Printf("User does not have access to session %s", sessionID)
				return echo.NewHTTPError(http.StatusForbidden, "You can only access your own sessions")
			}
		}
	}

	// Create target URL for the agentapi server
	targetURL := fmt.Sprintf("http://localhost:%d", session.Port)
	target, err := url.Parse(targetURL)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Invalid target URL: %v", err))
	}

	// Check if this is a POST to /message and capture the first message for description
	if c.Request().Method == "POST" && strings.HasSuffix(c.Request().URL.Path, "/message") {
		p.captureFirstMessage(c, session)
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
		resp.Header.Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
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

// getEnvFromSession retrieves an environment variable from the session environment,
// falling back to the default value if not found
func getEnvFromSession(session *AgentSession, key string, defaultValue string) string {
	if session.Environment != nil {
		if value, exists := session.Environment[key]; exists && value != "" {
			return value
		}
	}
	return defaultValue
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

// runAgentAPIServer runs an agentapi server instance using Go functions instead of scripts
func (p *Proxy) runAgentAPIServer(ctx context.Context, session *AgentSession, scriptName string, repoInfo *RepositoryInfo, initialMessage string) {
	defer func() {
		// Clean up session when server stops
		p.sessionsMutex.Lock()
		// Check if session still exists (might have been removed by deleteSession)
		_, sessionExists := p.sessions[session.ID]
		if sessionExists {
			delete(p.sessions, session.ID)
		}
		p.sessionsMutex.Unlock()

		// Remove session from persistent storage only if it still existed in the map
		if sessionExists {
			// Log session end when process terminates naturally (not via deleteSession)
			if err := p.logger.LogSessionEnd(session.ID, 0); err != nil {
				log.Printf("Failed to log session end for %s: %v", session.ID, err)
			}
		}

		if p.verbose {
			log.Printf("Cleaned up session %s", session.ID)
		}
	}()

	// Create startup manager
	startupManager := NewStartupManager(p.config, p.verbose)

	// Prepare startup configuration
	cfg := &StartupConfig{
		Port:                      session.Port,
		UserID:                    session.UserID,
		GitHubToken:               getEnvFromSession(session, "GITHUB_TOKEN", os.Getenv("GITHUB_TOKEN")),
		GitHubAppID:               os.Getenv("GITHUB_APP_ID"),
		GitHubInstallationID:      os.Getenv("GITHUB_INSTALLATION_ID"),
		GitHubAppPEMPath:          os.Getenv("GITHUB_APP_PEM_PATH"),
		GitHubAPI:                 os.Getenv("GITHUB_API"),
		GitHubPersonalAccessToken: os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN"),
		AgentAPIArgs:              os.Getenv("AGENTAPI_ARGS"),
		ClaudeArgs:                os.Getenv("CLAUDE_ARGS"),
		Environment:               session.Environment,
		Config:                    p.config,
		Verbose:                   p.verbose,
	}

	// Add repository information if available
	if repoInfo != nil {
		cfg.RepoFullName = repoInfo.FullName
		cfg.CloneDir = repoInfo.CloneDir
	} else {
		// Always set CloneDir to session ID, even when no repository is specified
		cfg.CloneDir = session.ID
	}

	// Extract MCP configurations from tags if available
	if session.Tags != nil {
		if mcpConfigs, exists := session.Tags["claude.mcp_configs"]; exists && mcpConfigs != "" {
			cfg.MCPConfigs = mcpConfigs
		}
	}

	// Start the AgentAPI session using Go functions
	cmd, err := startupManager.StartAgentAPISession(ctx, cfg)
	if err != nil {
		log.Printf("Failed to start AgentAPI session for %s: %v", session.ID, err)
		return
	}

	// Log startup details
	log.Printf("Starting agentapi process for session %s on %d using Go functions", session.ID, session.Port)
	log.Printf("Session startup parameters:")
	log.Printf("  Port: %d", session.Port)
	log.Printf("  Session ID: %s", session.ID)
	log.Printf("  User ID: %s", session.UserID)
	if cfg.RepoFullName != "" {
		log.Printf("  Repository: %s", cfg.RepoFullName)
		log.Printf("  Clone dir: %s", cfg.CloneDir)
	}
	if len(session.Tags) > 0 {
		log.Printf("  Request tags:")
		for key, value := range session.Tags {
			log.Printf("    %s=%s", key, value)
		}
	}

	// Capture stderr output for logging on exit code 1
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	// Store the command in the session and start the process
	session.processMutex.Lock()
	session.Process = cmd
	err = cmd.Start()
	session.processMutex.Unlock()

	if err != nil {
		log.Printf("Failed to start agentapi process for session %s: %v", session.ID, err)
		return
	}

	// Update session in storage after process is started

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
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Recovered from panic in cmd.Wait() for session %s: %v", session.ID, r)
				done <- fmt.Errorf("panic in cmd.Wait(): %v", r)
			}
		}()
		done <- cmd.Wait()
	}()

	// Ensure the process is cleaned up to prevent zombie processes
	defer func() {
		// This defer ensures cmd.Wait() is called if it hasn't been called yet
		if cmd.Process != nil && cmd.ProcessState == nil {
			// Process is still running, wait for it to prevent zombie
			select {
			case <-done:
				// Wait completed in the main logic
			case <-time.After(10 * time.Second):
				// Increased timeout to 10 seconds to allow proper cleanup
				log.Printf("Warning: Process %d cleanup timed out after 10 seconds", cmd.Process.Pid)
				// Force kill if still running
				if cmd.Process != nil {
					log.Printf("Force killing process %d to prevent zombie", cmd.Process.Pid)
					if err := cmd.Process.Kill(); err != nil {
						log.Printf("Failed to kill process %d: %v", cmd.Process.Pid, err)
					}
					// Wait for the killed process to prevent zombie
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
		if p.verbose {
			log.Printf("Terminating agentapi process for session %s", session.ID)
		}

		// Try graceful shutdown first (SIGTERM to process group)
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM); err != nil {
			// If process group signal fails, try individual process
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
			if p.verbose {
				log.Printf("AgentAPI process for session %s terminated gracefully", session.ID)
			}
			if waitErr != nil && p.verbose {
				log.Printf("Process wait error for session %s: %v", session.ID, waitErr)
			}
		case <-gracefulTimeout:
			// Force kill if graceful shutdown failed
			if p.verbose {
				log.Printf("Force killing agentapi process for session %s", session.ID)
			}
			// Kill entire process group
			if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
				log.Printf("Failed to kill process group %d: %v", cmd.Process.Pid, err)
				// If process group kill fails, try individual process
				if killErr := cmd.Process.Kill(); killErr != nil {
					log.Printf("Failed to kill process %d: %v", cmd.Process.Pid, killErr)
				}
			} else {
				log.Printf("Sent SIGKILL to process group %d", cmd.Process.Pid)
			}
			// Always wait for the process to prevent zombie
			select {
			case waitErr := <-done:
				if waitErr != nil && p.verbose {
					log.Printf("Process wait error after kill for session %s: %v", session.ID, waitErr)
				}
			case <-time.After(2 * time.Second):
				log.Printf("Warning: Process %d may not have exited cleanly", cmd.Process.Pid)
				// Even if timed out, try to consume from done channel to prevent goroutine leak
				go func() {
					select {
					case <-done: // Consume the value when available
					case <-time.After(5 * time.Second):
						// If we can't consume within 5 seconds, just exit the goroutine
						log.Printf("Warning: Could not consume done channel for process %d", cmd.Process.Pid)
					}
				}()
			}
		}

	case err := <-done:
		// Process finished on its own
		if err != nil {
			// Check if error is exit code 1 and log stderr output if available
			if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() == 1 {
				log.Printf("AgentAPI process for session %s exited with code 1: %v", session.ID, err)
				// Log stderr output if available
				if stderrOutput := stderrBuf.String(); stderrOutput != "" {
					log.Printf("Stderr output for session %s: %s", session.ID, stderrOutput)
				}
			} else {
				log.Printf("AgentAPI process for session %s exited with error: %v", session.ID, err)
			}
		} else if p.verbose {
			log.Printf("AgentAPI process for session %s exited normally", session.ID)
		}
	}
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
	// Stop session monitor first (if enabled)
	if p.sessionMonitor != nil {
		p.sessionMonitor.Stop()
	}

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

// cleanupDefunctProcesses periodically checks for and cleans up defunct processes
func (p *Proxy) cleanupDefunctProcesses() {
	ticker := time.NewTicker(5 * time.Minute) // Check every 5 minutes
	defer ticker.Stop()

	for range ticker.C {
		p.cleanupDefunctProcessesOnce()
	}
}

// cleanupDefunctProcessesOnce performs a single cleanup of defunct processes
func (p *Proxy) cleanupDefunctProcessesOnce() {
	// Find defunct processes
	cmd := exec.Command("ps", "aux")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("Failed to get process list for defunct cleanup: %v", err)
		return
	}

	lines := strings.Split(string(output), "\n")
	defunctCount := 0

	for _, line := range lines {
		if strings.Contains(line, "<defunct>") || strings.Contains(line, " Z ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				pidStr := fields[1]
				if pid, err := strconv.Atoi(pidStr); err == nil {
					// Try to reap the defunct process by sending signal 0
					// This doesn't actually send a signal but checks if we can access the process
					if err := syscall.Kill(pid, 0); err != nil {
						// Process is already gone or we can't access it
						continue
					}
					defunctCount++
				}
			}
		}
	}

	if defunctCount > 0 {
		log.Printf("Found %d defunct processes during periodic cleanup", defunctCount)

		// Try to trigger process reaping by the init system
		// This is a best-effort approach
		if defunctCount > 10 {
			log.Printf("High number of defunct processes detected (%d). Consider investigating process management.", defunctCount)
		}
	}
}

// GetEcho returns the Echo instance for external access
func (p *Proxy) GetEcho() *echo.Echo {
	return p.echo
}

// captureFirstMessage captures the first message content for description
func (p *Proxy) captureFirstMessage(c echo.Context, session *AgentSession) {
	// Check if description is already set
	if session.Tags != nil {
		if _, exists := session.Tags["description"]; exists {
			return // Description already set
		}
	}

	// Read the request body
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return // Best-effort operation
	}

	// Restore the request body for the proxy
	c.Request().Body = io.NopCloser(bytes.NewBuffer(body))

	// Parse the message request
	var messageReq map[string]interface{}
	if err := json.Unmarshal(body, &messageReq); err != nil {
		return // Best-effort operation
	}

	// Check if this is a user message with content
	if msgType, ok := messageReq["type"].(string); ok && msgType == "user" {
		if content, ok := messageReq["content"].(string); ok && content != "" {
			// Set description in session tags
			p.sessionsMutex.Lock()
			if session.Tags == nil {
				session.Tags = make(map[string]string)
			}
			session.Tags["description"] = content
			p.sessionsMutex.Unlock()

			// Update the persisted session
		}
	}
}
