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
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
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
	var teams []string
	if user != nil {
		userID = string(user.ID())
		if len(user.Roles()) > 0 {
			userRole = string(user.Roles()[0])
		} else {
			userRole = "user"
		}
		// Extract team slugs from GitHub user info
		if githubInfo := user.GitHubInfo(); githubInfo != nil {
			log.Printf("[SESSION_DEBUG] GitHubInfo found for user %s, teams count: %d", userID, len(githubInfo.Teams()))
			for _, team := range githubInfo.Teams() {
				// Format: "org/team-slug"
				teamSlug := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
				teams = append(teams, teamSlug)
				log.Printf("[SESSION_DEBUG] Added team: %s", teamSlug)
			}
		} else {
			log.Printf("[SESSION_DEBUG] No GitHubInfo for user %s", userID)
		}
	} else {
		userID = "anonymous"
		userRole = "guest"
	}

	// Validate team scope: user must be a member of the team
	if startReq.Scope == ScopeTeam {
		if startReq.TeamID == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "team_id is required when scope is 'team'")
		}
		if user == nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required for team-scoped sessions")
		}
		if !user.IsMemberOfTeam(startReq.TeamID) {
			log.Printf("User %s is not a member of team %s", userID, startReq.TeamID)
			return echo.NewHTTPError(http.StatusForbidden, "You are not a member of this team")
		}
	}

	session, err := h.proxy.CreateSession(sessionID, StartRequest{
		Environment: startReq.Environment,
		Tags:        startReq.Tags,
		Params:      startReq.Params,
		Scope:       startReq.Scope,
		TeamID:      startReq.TeamID,
	}, userID, userRole, teams)
	if err != nil {
		log.Printf("Failed to create session: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create session")
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"session_id": session.ID(),
	})
}

// SearchSessions handles GET /search requests to list and filter active sessions (memory only)
func (h *SessionHandlers) SearchSessions(c echo.Context) error {
	// Set CORS headers
	h.setCORSHeaders(c)

	user := auth.GetUserFromContext(c)
	status := c.QueryParam("status")
	scopeFilter := c.QueryParam("scope")
	teamIDFilter := c.QueryParam("team_id")

	var userID string
	var userTeamIDs []string
	if user != nil && !user.IsAdmin() {
		userID = string(user.ID())
		// Extract user's team IDs for filtering team-scoped sessions
		if githubInfo := user.GitHubInfo(); githubInfo != nil {
			for _, team := range githubInfo.Teams() {
				teamSlug := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
				userTeamIDs = append(userTeamIDs, teamSlug)
			}
		}
	}

	tagFilters := make(map[string]string)
	for paramName, paramValues := range c.QueryParams() {
		if strings.HasPrefix(paramName, "tag.") && len(paramValues) > 0 {
			tagKey := strings.TrimPrefix(paramName, "tag.")
			tagFilters[tagKey] = paramValues[0]
		}
	}

	// Build filter (without UserID to get all sessions, we'll filter by authorization below)
	filter := SessionFilter{
		Status:  status,
		Tags:    tagFilters,
		Scope:   ResourceScope(scopeFilter),
		TeamID:  teamIDFilter,
		TeamIDs: userTeamIDs,
	}

	// For non-admin users, set UserID filter only if not filtering by team
	if user != nil && !user.IsAdmin() && scopeFilter != "team" && teamIDFilter == "" {
		filter.UserID = userID
	}

	// Get sessions from session manager
	sessions := h.proxy.GetSessionManager().ListSessions(filter)

	// Check if auth is enabled
	cfg := auth.GetConfigFromContext(c)
	authEnabled := cfg != nil && cfg.Auth.Enabled

	// Filter by user authorization (supports both user-scoped and team-scoped)
	matchingSessions := make([]Session, 0)
	for _, session := range sessions {
		// If auth is not enabled, return all sessions
		if !authEnabled {
			matchingSessions = append(matchingSessions, session)
			continue
		}

		// Admin can see all sessions
		if user != nil && user.IsAdmin() {
			matchingSessions = append(matchingSessions, session)
			continue
		}

		// Check authorization based on scope
		if session.Scope() == ScopeTeam {
			// Team-scoped: user must be a member of the team
			if user != nil && user.IsMemberOfTeam(session.TeamID()) {
				matchingSessions = append(matchingSessions, session)
			}
		} else {
			// User-scoped: only owner can see
			if user != nil && session.UserID() == string(user.ID()) {
				matchingSessions = append(matchingSessions, session)
			}
		}
	}

	// Sort by start time (newest first)
	sort.Slice(matchingSessions, func(i, j int) bool {
		return matchingSessions[i].StartedAt().After(matchingSessions[j].StartedAt())
	})

	filteredSessions := make([]map[string]interface{}, 0, len(matchingSessions))
	for _, session := range matchingSessions {
		sessionData := map[string]interface{}{
			"session_id":  session.ID(),
			"user_id":     session.UserID(),
			"scope":       session.Scope(),
			"team_id":     session.TeamID(),
			"status":      session.Status(),
			"started_at":  session.StartedAt(),
			"addr":        session.Addr(),
			"tags":        session.Tags(),
			"description": session.Description(),
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

	session := h.proxy.GetSessionManager().GetSession(sessionID)
	if session == nil {
		log.Printf("Delete session failed: session %s not found (requested by %s)", sessionID, clientIP)
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	// Check authorization based on scope
	user := auth.GetUserFromContext(c)
	canAccess := false
	if user != nil {
		canAccess = user.CanAccessResource(
			entities.UserID(session.UserID()),
			string(session.Scope()),
			session.TeamID(),
		)
	} else {
		// Fall back to legacy check if no user
		canAccess = auth.UserOwnsSession(c, session.UserID())
	}

	if !canAccess {
		log.Printf("Delete session failed: user does not have access to session %s (requested by %s)", sessionID, clientIP)
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to delete this session")
	}

	log.Printf("Deleting session %s (status: %s, user: %s) requested by %s",
		sessionID, session.Status(), session.UserID(), clientIP)

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

	session := h.proxy.GetSessionManager().GetSession(sessionID)
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	// Skip auth check for OPTIONS requests
	if c.Request().Method != "OPTIONS" {
		cfg := auth.GetConfigFromContext(c)
		if cfg != nil && cfg.Auth.Enabled {
			// Check authorization based on scope
			user := auth.GetUserFromContext(c)
			canAccess := false
			if user != nil {
				canAccess = user.CanAccessResource(
					entities.UserID(session.UserID()),
					string(session.Scope()),
					session.TeamID(),
				)
			}
			if !canAccess {
				log.Printf("User does not have access to session %s", sessionID)
				return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to access this session")
			}
		}
	}

	// Determine target URL using session address
	targetURL := fmt.Sprintf("http://%s", session.Addr())
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
func (h *SessionHandlers) captureFirstMessage(c echo.Context, session Session) {
	// Skip if description already exists
	if session.Tags() != nil {
		if _, exists := session.Tags()["description"]; exists {
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
			// Get the local session manager to update tags
			if lsm, ok := h.proxy.GetSessionManager().(*LocalSessionManager); ok {
				if localSess := lsm.GetLocalSession(session.ID()); localSess != nil {
					tags := localSess.Tags()
					if tags == nil {
						tags = make(map[string]string)
					}
					tags["description"] = content
					localSess.SetTags(tags)
				}
			}
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
