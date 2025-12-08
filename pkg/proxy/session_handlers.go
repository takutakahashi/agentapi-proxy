package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// SessionHandlers handles session management endpoints without persistence
type SessionHandlers struct {
	proxy *Proxy
}

// NewSessionHandlers creates a new SessionHandlers instance
func NewSessionHandlers(proxy *Proxy) *SessionHandlers {
	return &SessionHandlers{
		proxy: proxy,
	}
}

// GetName returns the name of this handler for logging
func (h *SessionHandlers) GetName() string {
	return "SessionHandlers"
}

// RegisterRoutes registers session management routes
func (h *SessionHandlers) RegisterRoutes(e *echo.Echo, proxy *Proxy) error {
	// Session management routes
	e.POST("/start", h.StartSession)
	e.GET("/search", h.SearchSessions)
	e.DELETE("/sessions/:sessionId", h.DeleteSession)

	// Session proxy route
	e.Any("/:sessionId/*", h.RouteToSession)

	log.Printf("Registered session management routes")
	return nil
}

// StartSession handles POST /start requests to start a new agentapi server
func (h *SessionHandlers) StartSession(c echo.Context) error {
	// Set CORS headers
	h.setCORSHeaders(c)

	sessionID := uuid.New().String()

	var startReq StartRequest
	if err := c.Bind(&startReq); err != nil {
		log.Printf("Failed to parse request body (using defaults): %v", err)
	}

	user := auth.GetUserFromContext(c)
	var userID, userRole string
	if user != nil {
		userID = string(user.ID())
		if len(user.Roles()) > 0 {
			userRole = string(user.Roles()[0])
		} else {
			userRole = "user"
		}
	} else {
		userID = "anonymous"
		userRole = "guest"
	}

	session, err := h.proxy.CreateSession(sessionID, StartRequest{
		Environment: startReq.Environment,
		Tags:        startReq.Tags,
		Message:     startReq.Message,
	}, userID, userRole)
	if err != nil {
		log.Printf("Failed to create session: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create session")
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"session_id": session.ID,
	})
}

// SearchSessions handles GET /search requests to list and filter active sessions (memory only)
func (h *SessionHandlers) SearchSessions(c echo.Context) error {
	// Set CORS headers
	h.setCORSHeaders(c)

	user := auth.GetUserFromContext(c)
	status := c.QueryParam("status")

	var userID string
	if user != nil && !user.IsAdmin() {
		userID = string(user.ID())
	}

	tagFilters := make(map[string]string)
	for paramName, paramValues := range c.QueryParams() {
		if strings.HasPrefix(paramName, "tag.") && len(paramValues) > 0 {
			tagKey := strings.TrimPrefix(paramName, "tag.")
			tagFilters[tagKey] = paramValues[0]
		}
	}

	h.proxy.sessionsMutex.RLock()
	defer h.proxy.sessionsMutex.RUnlock()

	sessions := h.proxy.sessions
	matchingSessions := make([]*AgentSession, 0)

	for _, session := range sessions {
		req := session.Request
		// User authorization check
		if user != nil && !user.IsAdmin() && req.UserID != string(user.ID()) {
			continue
		}

		// User ID filter
		if userID != "" && req.UserID != userID {
			continue
		}

		// Status filter
		if status != "" && session.Status != status {
			continue
		}

		// Tag filters
		matchAllTags := true
		for tagKey, tagValue := range tagFilters {
			sessionTagValue, exists := req.Tags[tagKey]
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

	// Sort by start time (newest first)
	sort.Slice(matchingSessions, func(i, j int) bool {
		return matchingSessions[i].StartedAt.After(matchingSessions[j].StartedAt)
	})

	filteredSessions := make([]map[string]interface{}, 0, len(matchingSessions))
	for _, session := range matchingSessions {
		req := session.Request
		sessionData := map[string]interface{}{
			"session_id": session.ID,
			"user_id":    req.UserID,
			"status":     session.Status,
			"started_at": session.StartedAt,
			"port":       req.Port,
			"tags":       req.Tags,
		}
		filteredSessions = append(filteredSessions, sessionData)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"sessions": filteredSessions,
	})
}

// DeleteSession handles DELETE /sessions/:sessionId requests to terminate a session
func (h *SessionHandlers) DeleteSession(c echo.Context) error {
	// Set CORS headers
	h.setCORSHeaders(c)

	sessionID := c.Param("sessionId")
	clientIP := c.RealIP()

	log.Printf("Request: DELETE /sessions/%s from %s", sessionID, clientIP)

	if sessionID == "" {
		log.Printf("Delete session failed: missing session ID from %s", clientIP)
		return echo.NewHTTPError(http.StatusBadRequest, "Session ID is required")
	}

	h.proxy.sessionsMutex.RLock()
	sessions := h.proxy.sessions
	session, exists := sessions[sessionID]
	var sessionStatus = "unknown"
	if exists {
		sessionStatus = session.Status
	}
	h.proxy.sessionsMutex.RUnlock()

	if !exists {
		log.Printf("Delete session failed: session %s not found (requested by %s)", sessionID, clientIP)
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	if !auth.UserOwnsSession(c, session.Request.UserID) {
		log.Printf("Delete session failed: user does not own session %s (requested by %s)", sessionID, clientIP)
		return echo.NewHTTPError(http.StatusForbidden, "You can only delete your own sessions")
	}

	log.Printf("Deleting session %s (status: %s, user: %s) requested by %s",
		sessionID, sessionStatus, session.Request.UserID, clientIP)

	if err := h.proxy.DeleteSessionByID(sessionID); err != nil {
		log.Printf("Failed to delete session %s: %v", sessionID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete session")
	}

	log.Printf("Session %s deletion completed successfully", sessionID)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message":    "Session terminated successfully",
		"session_id": sessionID,
		"status":     "terminated",
	})
}

// RouteToSession routes requests to the appropriate agentapi server instance
func (h *SessionHandlers) RouteToSession(c echo.Context) error {
	sessionID := c.Param("sessionId")

	h.proxy.sessionsMutex.RLock()
	sessions := h.proxy.sessions
	session, exists := sessions[sessionID]
	h.proxy.sessionsMutex.RUnlock()

	if !exists {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	// Skip auth check for OPTIONS requests
	if c.Request().Method != "OPTIONS" {
		cfg := auth.GetConfigFromContext(c)
		if cfg != nil && cfg.Auth.Enabled {
			if !auth.UserOwnsSession(c, session.Request.UserID) {
				log.Printf("User does not have access to session %s", sessionID)
				return echo.NewHTTPError(http.StatusForbidden, "You can only access your own sessions")
			}
		}
	}

	targetURL := fmt.Sprintf("http://localhost:%d", session.Request.Port)
	target, err := url.Parse(targetURL)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Invalid target URL: %v", err))
	}

	// Capture first message for session description
	if c.Request().Method == "POST" && strings.HasSuffix(c.Request().URL.Path, "/message") {
		h.captureFirstMessage(c, session)
	}

	req := c.Request()
	w := c.Response()

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = time.Millisecond * 100

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)

		// Remove session ID from path before forwarding
		originalPath := req.URL.Path
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

	originalModifyResponse := proxy.ModifyResponse
	proxy.ModifyResponse = func(resp *http.Response) error {
		// Set CORS headers
		resp.Header.Set("Access-Control-Allow-Origin", "*")
		resp.Header.Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
		resp.Header.Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
		resp.Header.Set("Access-Control-Allow-Credentials", "true")
		resp.Header.Set("Access-Control-Max-Age", "86400")

		// Handle SSE streams
		if resp.Header.Get("Content-Type") == "text/event-stream" {
			resp.Header.Set("Cache-Control", "no-cache")
			resp.Header.Set("Connection", "keep-alive")
			resp.Header.Del("Content-Length")
		}

		if originalModifyResponse != nil {
			return originalModifyResponse(resp)
		}
		return nil
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("Proxy error for session %s: %v", sessionID, err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
	}

	proxy.ServeHTTP(w, req)
	return nil
}

// captureFirstMessage captures the first message content for session description
func (h *SessionHandlers) captureFirstMessage(c echo.Context, session *AgentSession) {
	// Skip if description already exists
	if session.Request.Tags != nil {
		if _, exists := session.Request.Tags["description"]; exists {
			return
		}
	}

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return
	}

	// Restore the request body for further processing
	c.Request().Body = io.NopCloser(bytes.NewBuffer(body))

	var messageReq map[string]interface{}
	if err := json.Unmarshal(body, &messageReq); err != nil {
		return
	}

	// Extract description from user message content
	if msgType, ok := messageReq["type"].(string); ok && msgType == "user" {
		if content, ok := messageReq["content"].(string); ok && content != "" {
			h.proxy.sessionsMutex.Lock()
			if session.Request.Tags == nil {
				session.Request.Tags = make(map[string]string)
			}
			session.Request.Tags["description"] = content
			h.proxy.sessionsMutex.Unlock()
		}
	}
}

// setCORSHeaders sets CORS headers for all session management endpoints
func (h *SessionHandlers) setCORSHeaders(c echo.Context) {
	c.Response().Header().Set("Access-Control-Allow-Origin", "*")
	c.Response().Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
	c.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
	c.Response().Header().Set("Access-Control-Allow-Credentials", "true")
	c.Response().Header().Set("Access-Control-Max-Age", "86400")
}
