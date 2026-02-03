package controllers

import (
	"bytes"
	"context"
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
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	sessionuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// SessionCreator is an interface for creating sessions
type SessionCreator interface {
	CreateSession(sessionID string, req entities.StartRequest, userID, userRole string, teams []string) (entities.Session, error)
	DeleteSessionByID(sessionID string) error
}

// SessionManagerProvider provides access to the session manager
// This allows the session manager to be swapped at runtime (e.g., for testing)
type SessionManagerProvider interface {
	GetSessionManager() repositories.SessionManager
}

// SessionController handles session management endpoints
type SessionController struct {
	sessionManagerProvider SessionManagerProvider
	sessionCreator         SessionCreator
	validateTeamUC         *sessionuc.ValidateTeamAccessUseCase
}

// NewSessionController creates a new SessionController instance
func NewSessionController(
	sessionManagerProvider SessionManagerProvider,
	sessionCreator SessionCreator,
) *SessionController {
	return &SessionController{
		sessionManagerProvider: sessionManagerProvider,
		sessionCreator:         sessionCreator,
		validateTeamUC:         sessionuc.NewValidateTeamAccessUseCase(),
	}
}

// getSessionManager returns the current session manager
func (c *SessionController) getSessionManager() repositories.SessionManager {
	return c.sessionManagerProvider.GetSessionManager()
}

// GetName returns the name of this handler for logging
func (c *SessionController) GetName() string {
	return "SessionController"
}

// RegisterRoutes registers session management routes
func (c *SessionController) RegisterRoutes(e *echo.Echo) error {
	// Session management routes
	e.POST("/start", c.StartSession)
	e.GET("/search", c.SearchSessions)
	// Register /sessions/:sessionId/action BEFORE /:sessionId/* to avoid matching conflict
	e.POST("/sessions/:sessionId/action", c.SessionAction)
	e.DELETE("/sessions/:sessionId", c.DeleteSession)

	// Session proxy route
	e.Any("/:sessionId/*", c.RouteToSession)

	log.Printf("Registered session management routes")
	return nil
}

// StartSession handles POST /start requests to start a new agentapi server
func (c *SessionController) StartSession(ctx echo.Context) error {
	c.setCORSHeaders(ctx)

	sessionID := uuid.New().String()

	var startReq entities.StartRequest
	if err := ctx.Bind(&startReq); err != nil {
		log.Printf("Failed to parse request body (using defaults): %v", err)
	}

	user := auth.GetUserFromContext(ctx)
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

	// Validate team scope using use case
	if err := c.validateTeamUC.ValidateTeamScope(
		startReq.Scope,
		startReq.TeamID,
		teams,
		user != nil,
	); err != nil {
		if strings.Contains(err.Error(), "team_id is required") {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
		if strings.Contains(err.Error(), "authentication required") {
			return echo.NewHTTPError(http.StatusUnauthorized, err.Error())
		}
		return echo.NewHTTPError(http.StatusForbidden, err.Error())
	}

	session, err := c.sessionCreator.CreateSession(sessionID, startReq, userID, userRole, teams)
	if err != nil {
		log.Printf("Failed to create session: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create session")
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"session_id": session.ID(),
	})
}

// SearchSessions handles GET /search requests to list and filter active sessions
func (c *SessionController) SearchSessions(ctx echo.Context) error {
	c.setCORSHeaders(ctx)

	user := auth.GetUserFromContext(ctx)
	status := ctx.QueryParam("status")
	scopeFilter := ctx.QueryParam("scope")
	teamIDFilter := ctx.QueryParam("team_id")

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
	for paramName, paramValues := range ctx.QueryParams() {
		if strings.HasPrefix(paramName, "tag.") && len(paramValues) > 0 {
			tagKey := strings.TrimPrefix(paramName, "tag.")
			tagFilters[tagKey] = paramValues[0]
		}
	}

	// Build filter
	filter := entities.SessionFilter{
		Status:  status,
		Tags:    tagFilters,
		Scope:   entities.ResourceScope(scopeFilter),
		TeamID:  teamIDFilter,
		TeamIDs: userTeamIDs,
	}

	// For non-admin users, set UserID filter only if not filtering by team
	if user != nil && !user.IsAdmin() && scopeFilter != "team" && teamIDFilter == "" {
		filter.UserID = userID
	}

	// Get sessions from session manager
	sessions := c.getSessionManager().ListSessions(filter)

	// Check if auth is enabled
	cfg := auth.GetConfigFromContext(ctx)
	authEnabled := cfg != nil && cfg.Auth.Enabled

	// Filter by user authorization
	matchingSessions := make([]entities.Session, 0)
	for _, session := range sessions {
		if !authEnabled {
			matchingSessions = append(matchingSessions, session)
			continue
		}

		// Scope isolation
		sessionScope := session.Scope()
		if scopeFilter == string(entities.ScopeTeam) {
			if sessionScope != entities.ScopeTeam {
				continue
			}
		} else {
			if sessionScope == entities.ScopeTeam {
				continue
			}
		}

		// Admin can see all sessions within the filtered scope
		if user != nil && user.IsAdmin() {
			matchingSessions = append(matchingSessions, session)
			continue
		}

		// Check authorization based on scope
		if sessionScope == entities.ScopeTeam {
			if user != nil && user.IsMemberOfTeam(session.TeamID()) {
				matchingSessions = append(matchingSessions, session)
			}
		} else {
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
		// Get initial message from Secret if available
		var initialMessage string
		if ksManager, ok := c.getSessionManager().(*services.KubernetesSessionManager); ok {
			if ks, ok := session.(*services.KubernetesSession); ok {
				initialMessage = ksManager.GetInitialMessage(context.Background(), ks)
			}
		}

		sessionData := map[string]interface{}{
			"session_id": session.ID(),
			"user_id":    session.UserID(),
			"scope":      session.Scope(),
			"team_id":    session.TeamID(),
			"status":     session.Status(),
			"started_at": session.StartedAt(),
			"updated_at": session.UpdatedAt(),
			"addr":       session.Addr(),
			"tags":       session.Tags(),
			"metadata": map[string]interface{}{
				"description": initialMessage,
			},
		}
		filteredSessions = append(filteredSessions, sessionData)
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"sessions": filteredSessions,
	})
}

// DeleteSession handles DELETE /sessions/:sessionId requests to terminate a session
func (c *SessionController) DeleteSession(ctx echo.Context) error {
	log.Printf("[ROUTE] DeleteSession handler called: %s %s", ctx.Request().Method, ctx.Request().URL.Path)
	c.setCORSHeaders(ctx)

	sessionID := ctx.Param("sessionId")
	clientIP := ctx.RealIP()

	log.Printf("Request: DELETE /sessions/%s from %s", sessionID, clientIP)

	if sessionID == "" {
		log.Printf("Delete session failed: missing session ID from %s", clientIP)
		return echo.NewHTTPError(http.StatusBadRequest, "Session ID is required")
	}

	session := c.getSessionManager().GetSession(sessionID)
	if session == nil {
		log.Printf("Delete session failed: session %s not found (requested by %s)", sessionID, clientIP)
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	// Check authorization based on scope
	user := auth.GetUserFromContext(ctx)
	canAccess := false
	if user != nil {
		canAccess = user.CanAccessResource(
			entities.UserID(session.UserID()),
			string(session.Scope()),
			session.TeamID(),
		)
	} else {
		canAccess = auth.UserOwnsSession(ctx, session.UserID())
	}

	if !canAccess {
		log.Printf("Delete session failed: user does not have access to session %s (requested by %s)", sessionID, clientIP)
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to delete this session")
	}

	log.Printf("Deleting session %s (status: %s, user: %s) requested by %s",
		sessionID, session.Status(), session.UserID(), clientIP)

	if err := c.sessionCreator.DeleteSessionByID(sessionID); err != nil {
		log.Printf("Failed to delete session %s: %v", sessionID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete session")
	}

	log.Printf("Session %s deletion completed successfully", sessionID)

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"message":    "Session terminated successfully",
		"session_id": sessionID,
		"status":     "terminated",
	})
}

// SessionActionRequest represents the request body for session actions
type SessionActionRequest struct {
	Action string `json:"action"`
}

// SessionAction handles POST /sessions/:sessionId/action requests to perform actions on sessions
func (c *SessionController) SessionAction(ctx echo.Context) error {
	log.Printf("[ROUTE] SessionAction handler called: %s %s", ctx.Request().Method, ctx.Request().URL.Path)
	c.setCORSHeaders(ctx)

	sessionID := ctx.Param("sessionId")
	clientIP := ctx.RealIP()

	// Parse action from request body
	var req SessionActionRequest
	if err := ctx.Bind(&req); err != nil {
		log.Printf("Session action failed: invalid request body from %s: %v", clientIP, err)
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	log.Printf("Request: POST /sessions/%s/action (action=%s) from %s", sessionID, req.Action, clientIP)

	if sessionID == "" {
		log.Printf("Session action failed: missing session ID from %s", clientIP)
		return echo.NewHTTPError(http.StatusBadRequest, "Session ID is required")
	}

	if req.Action == "" {
		log.Printf("Session action failed: missing action from %s", clientIP)
		return echo.NewHTTPError(http.StatusBadRequest, "Action is required")
	}

	session := c.getSessionManager().GetSession(sessionID)
	if session == nil {
		log.Printf("Session action failed: session %s not found (requested by %s)", sessionID, clientIP)
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	// Check authorization based on scope
	user := auth.GetUserFromContext(ctx)
	canAccess := false
	if user != nil {
		canAccess = user.CanAccessResource(
			entities.UserID(session.UserID()),
			string(session.Scope()),
			session.TeamID(),
		)
	} else {
		canAccess = auth.UserOwnsSession(ctx, session.UserID())
	}

	if !canAccess {
		log.Printf("Session action failed: user does not have access to session %s (requested by %s)", sessionID, clientIP)
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to perform this action on this session")
	}

	// Handle different actions
	switch req.Action {
	case "resume":
		return c.handleResumeAction(ctx, session, sessionID, clientIP)
	case "suspend":
		return c.handleSuspendAction(ctx, session, sessionID, clientIP)
	default:
		log.Printf("Session action failed: unknown action '%s' from %s", req.Action, clientIP)
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Unknown action: %s", req.Action))
	}
}

// handleResumeAction handles the resume action for a session
func (c *SessionController) handleResumeAction(ctx echo.Context, session entities.Session, sessionID, clientIP string) error {
	// Check if session is actually suspended
	status := session.Status()
	if status != "suspend" {
		log.Printf("Resume session skipped: session %s is not suspended (status: %s)", sessionID, status)
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"message":    "Session is not suspended",
			"session_id": sessionID,
			"status":     status,
		})
	}

	log.Printf("Resuming session %s (status: %s, user: %s) requested by %s",
		sessionID, session.Status(), session.UserID(), clientIP)

	// Resume the session
	if manager, ok := c.getSessionManager().(*services.KubernetesSessionManager); ok {
		resumeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := manager.ResumeSession(resumeCtx, sessionID); err != nil {
			log.Printf("Failed to resume session %s: %v", sessionID, err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to resume session")
		}
	} else {
		return echo.NewHTTPError(http.StatusInternalServerError, "Session manager does not support resume")
	}

	log.Printf("Session %s resume initiated successfully", sessionID)

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"message":    "Session resume initiated",
		"session_id": sessionID,
		"status":     "starting",
	})
}

// handleSuspendAction handles the suspend action for a session
func (c *SessionController) handleSuspendAction(ctx echo.Context, session entities.Session, sessionID, clientIP string) error {
	// Check if session is already suspended
	status := session.Status()
	if status == "suspend" {
		log.Printf("Suspend session skipped: session %s is already suspended", sessionID)
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"message":    "Session is already suspended",
			"session_id": sessionID,
			"status":     status,
		})
	}

	log.Printf("Suspending session %s (status: %s, user: %s) requested by %s",
		sessionID, session.Status(), session.UserID(), clientIP)

	// Suspend the session
	if manager, ok := c.getSessionManager().(*services.KubernetesSessionManager); ok {
		suspendCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := manager.SuspendSession(suspendCtx, sessionID); err != nil {
			log.Printf("Failed to suspend session %s: %v", sessionID, err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to suspend session")
		}
	} else {
		return echo.NewHTTPError(http.StatusInternalServerError, "Session manager does not support suspend")
	}

	log.Printf("Session %s suspended successfully", sessionID)

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"message":    "Session suspended successfully",
		"session_id": sessionID,
		"status":     "suspend",
	})
}

// RouteToSession routes requests to the appropriate agentapi server instance
func (c *SessionController) RouteToSession(ctx echo.Context) error {
	log.Printf("[ROUTE] RouteToSession handler called: %s %s", ctx.Request().Method, ctx.Request().URL.Path)
	sessionID := ctx.Param("sessionId")

	session := c.getSessionManager().GetSession(sessionID)
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	// Skip auth check for OPTIONS requests
	if ctx.Request().Method != "OPTIONS" {
		cfg := auth.GetConfigFromContext(ctx)
		if cfg != nil && cfg.Auth.Enabled {
			user := auth.GetUserFromContext(ctx)
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

	// Capture first message for session description and update timestamp
	if ctx.Request().Method == "POST" && strings.HasSuffix(ctx.Request().URL.Path, "/message") {
		c.captureFirstMessage(ctx, session)
		c.updateSessionTimestamp(ctx, session)
	}

	req := ctx.Request()
	w := ctx.Response()

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
		originalHost := ctx.Request().Host
		if originalHost == "" {
			originalHost = ctx.Request().Header.Get("Host")
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
func (c *SessionController) captureFirstMessage(ctx echo.Context, session entities.Session) {
	// Skip if description already exists
	if session.Tags() != nil {
		if _, exists := session.Tags()["description"]; exists {
			return
		}
	}

	body, err := io.ReadAll(ctx.Request().Body)
	if err != nil {
		return
	}

	// Restore the request body for further processing
	ctx.Request().Body = io.NopCloser(bytes.NewBuffer(body))

	var messageReq map[string]interface{}
	if err := json.Unmarshal(body, &messageReq); err != nil {
		return
	}
}

// updateSessionTimestamp updates the session's updated_at timestamp
func (c *SessionController) updateSessionTimestamp(ctx echo.Context, session entities.Session) {
	// Update in-memory timestamp
	if ks, ok := session.(*services.KubernetesSession); ok {
		ks.TouchUpdatedAt()

		// Update Service annotation asynchronously to avoid blocking the request
		go func() {
			updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if manager, ok := c.getSessionManager().(*services.KubernetesSessionManager); ok {
				updatedAt := ks.UpdatedAt().Format(time.RFC3339)
				if err := manager.UpdateServiceAnnotation(updateCtx, session.ID(), "agentapi.proxy/updated-at", updatedAt); err != nil {
					log.Printf("[SESSION] Failed to update Service annotation for session %s: %v", session.ID(), err)
				}
			}
		}()
	}
}

// setCORSHeaders sets CORS headers for all session management endpoints
func (c *SessionController) setCORSHeaders(ctx echo.Context) {
	ctx.Response().Header().Set("Access-Control-Allow-Origin", "*")
	ctx.Response().Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
	ctx.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
	ctx.Response().Header().Set("Access-Control-Allow-Credentials", "true")
	ctx.Response().Header().Set("Access-Control-Max-Age", "86400")
}
