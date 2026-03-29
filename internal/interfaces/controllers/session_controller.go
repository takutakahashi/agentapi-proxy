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
	"github.com/takutakahashi/agentapi-proxy/pkg/hmacutil"
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
	sessionRouteRepo       repositories.SessionRouteRepository
	settingsRepo           repositories.SettingsRepository
}

// NewSessionController creates a new SessionController instance
func NewSessionController(
	sessionManagerProvider SessionManagerProvider,
	sessionCreator SessionCreator,
	opts ...SessionControllerOption,
) *SessionController {
	c := &SessionController{
		sessionManagerProvider: sessionManagerProvider,
		sessionCreator:         sessionCreator,
		validateTeamUC:         sessionuc.NewValidateTeamAccessUseCase(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// SessionControllerOption is a functional option for SessionController
type SessionControllerOption func(*SessionController)

// WithSessionRouteRepository sets the session route repository on the controller
func WithSessionRouteRepository(repo repositories.SessionRouteRepository) SessionControllerOption {
	return func(c *SessionController) {
		c.sessionRouteRepo = repo
	}
}

// WithSettingsRepository sets the settings repository on the controller
func WithSettingsRepository(repo repositories.SettingsRepository) SessionControllerOption {
	return func(c *SessionController) {
		c.settingsRepo = repo
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

	// Get authorization context from middleware (guaranteed to be non-nil by AuthMiddleware)
	authzCtx := auth.GetAuthorizationContext(ctx)
	user := authzCtx.User
	userID := string(user.ID())

	var userRole string
	if len(user.Roles()) > 0 {
		userRole = string(user.Roles()[0])
	} else {
		userRole = "user"
	}

	// Use pre-resolved team information from authorization context
	teams := authzCtx.TeamScope.Teams
	log.Printf("[SESSION_DEBUG] Using authz context for user %s, teams count: %d", userID, len(teams))

	// Normalize scope: default to "user" if not specified.
	// This prevents downstream failures (e.g. memory dump) that require a valid scope.
	if startReq.Scope == "" {
		startReq.Scope = entities.ScopeUser
	}

	// Validate team scope authorization
	if startReq.Scope == entities.ScopeTeam {
		if startReq.TeamID == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "team_id is required for team scope")
		}

		// Check if user can create in this team
		if !authzCtx.CanCreateInTeam(startReq.TeamID) {
			return echo.NewHTTPError(http.StatusForbidden, fmt.Sprintf("user is not a member of team %s", startReq.TeamID))
		}
	} else {
		// Personal scope - check if user can create personal resources
		if !authzCtx.PersonalScope.CanCreate {
			return echo.NewHTTPError(http.StatusForbidden, "user does not have permission to create sessions")
		}
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

	// Get authorization context from middleware (guaranteed to be non-nil by AuthMiddleware)
	authzCtx := auth.GetAuthorizationContext(ctx)
	status := ctx.QueryParam("status")
	scopeFilter := ctx.QueryParam("scope")
	teamIDFilter := ctx.QueryParam("team_id")

	userID := authzCtx.PersonalScope.UserID
	userTeamIDs := authzCtx.TeamScope.Teams

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

	// For non-team scope, always filter by user ID - even admins should not see
	// other users' personal sessions. Admin privileges apply to team-scoped resources only.
	if scopeFilter != "team" && teamIDFilter == "" {
		filter.UserID = userID
	}

	// Get sessions from session manager
	sessions := c.getSessionManager().ListSessions(filter)

	// Filter by user authorization using authorization context
	matchingSessions := make([]entities.Session, 0)
	for _, session := range sessions {
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

		// Check authorization using pre-resolved context
		// Admin bypasses are handled within CanAccessResource for team-scoped resources only
		if authzCtx.CanAccessResource(session.UserID(), string(sessionScope), session.TeamID()) {
			matchingSessions = append(matchingSessions, session)
		}
	}

	// Exclude hidden sessions by default (unless caller explicitly requests them via tag.hidden=true)
	_, hiddenExplicitlyRequested := tagFilters["hidden"]
	if !hiddenExplicitlyRequested {
		matchingSessions = filterHiddenSessions(matchingSessions)
	}

	// Sort by start time (newest first)
	sort.Slice(matchingSessions, func(i, j int) bool {
		return matchingSessions[i].StartedAt().After(matchingSessions[j].StartedAt())
	})

	filteredSessions := make([]map[string]interface{}, 0, len(matchingSessions))
	// Track session IDs already present to avoid duplicates from route-based sessions
	localSessionIDs := make(map[string]struct{}, len(matchingSessions))
	for _, session := range matchingSessions {
		localSessionIDs[session.ID()] = struct{}{}

		// Use session.Description() which returns the in-memory cached initial message.
		// This avoids reading from Kubernetes Secret (which is created asynchronously
		// after provisioning completes and would return empty for newly created sessions).
		// After a proxy restart, Description() is populated from the Secret during session
		// restoration in restoreSessionFromService.
		initialMessage := session.Description()

		sessionData := map[string]interface{}{
			"session_id":      session.ID(),
			"user_id":         session.UserID(),
			"scope":           session.Scope(),
			"team_id":         session.TeamID(),
			"status":          session.Status(),
			"started_at":      session.StartedAt(),
			"updated_at":      session.UpdatedAt(),
			"last_message_at": session.LastMessageAt(),
			"addr":            session.Addr(),
			"tags":            session.Tags(),
			"metadata": map[string]interface{}{
				"description": initialMessage,
			},
		}
		filteredSessions = append(filteredSessions, sessionData)
	}

	// Include ESM-created sessions from session routes
	if c.sessionRouteRepo != nil {
		routes, err := c.sessionRouteRepo.List(ctx.Request().Context(), userID)
		if err != nil {
			log.Printf("[SEARCH] Failed to list session routes: %v", err)
		} else {
			for _, route := range routes {
				// Skip sessions already present in the local session manager
				if _, exists := localSessionIDs[route.SessionID]; exists {
					continue
				}
				// Apply scope filter
				if scopeFilter == string(entities.ScopeTeam) && route.Scope != string(entities.ScopeTeam) {
					continue
				}
				if scopeFilter != string(entities.ScopeTeam) && route.Scope == string(entities.ScopeTeam) {
					continue
				}
				if !authzCtx.CanAccessResource(route.UserID, route.Scope, route.TeamID) {
					continue
				}
				tags := route.Tags
				if tags == nil {
					tags = map[string]string{}
				}
				// Apply tag filters
				match := true
				for k, v := range tagFilters {
					if tags[k] != v {
						match = false
						break
					}
				}
				if !match {
					continue
				}
				filteredSessions = append(filteredSessions, map[string]interface{}{
					"session_id":      route.SessionID,
					"user_id":         route.UserID,
					"scope":           route.Scope,
					"team_id":         route.TeamID,
					"status":          "active",
					"started_at":      route.StartedAt,
					"updated_at":      route.StartedAt,
					"last_message_at": route.StartedAt,
					"addr":            "",
					"tags":            tags,
					"metadata": map[string]interface{}{
						"description": route.InitialMessage,
					},
				})
			}
		}
	}

	// Fetch sessions from remote ESMs and merge
	// Remote sessions (fetched from ESMs) are excluded by default unless caller explicitly
	// requests them via tag.remote=true
	_, remoteExplicitlyRequested := tagFilters["remote"]
	if c.settingsRepo != nil && remoteExplicitlyRequested {
		userID := authzCtx.PersonalScope.UserID
		teams := authzCtx.TeamScope.Teams
		remoteSessions := c.fetchRemoteSessions(ctx.Request().Context(), userID, teams, filter)
		filteredSessions = append(filteredSessions, remoteSessions...)
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"sessions": filteredSessions,
	})
}

// DeleteSession handles DELETE /sessions/:sessionId requests to terminate a session
func (c *SessionController) DeleteSession(ctx echo.Context) error {
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
		// Check if it's a remote session
		if c.sessionRouteRepo != nil {
			route, err := c.sessionRouteRepo.Get(ctx.Request().Context(), sessionID)
			if err != nil {
				log.Printf("Delete session: failed to look up route for %s: %v", sessionID, err)
			} else if route != nil {
				return c.deleteRemoteSession(ctx, route)
			}
		}
		log.Printf("Delete session failed: session %s not found (requested by %s)", sessionID, clientIP)
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	// Check authorization using pre-resolved authorization context (guaranteed to be non-nil by AuthMiddleware)
	authzCtx := auth.GetAuthorizationContext(ctx)
	if !authzCtx.CanModifyResource(session.UserID(), string(session.Scope()), session.TeamID()) {
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

// RouteToSession routes requests to the appropriate agentapi server instance
func (c *SessionController) RouteToSession(ctx echo.Context) error {
	sessionID := ctx.Param("sessionId")

	session := c.getSessionManager().GetSession(sessionID)
	if session == nil {
		// Check if this is a remote session on Proxy B
		if c.sessionRouteRepo != nil {
			route, err := c.sessionRouteRepo.Get(ctx.Request().Context(), sessionID)
			if err != nil {
				log.Printf("[ROUTE] Failed to look up session route for %s: %v", sessionID, err)
			} else if route != nil {
				return c.routeToRemoteSession(ctx, route)
			}
		}
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	// Skip auth check for OPTIONS requests
	if ctx.Request().Method != "OPTIONS" {
		// Check authorization using pre-resolved context (guaranteed to be non-nil by AuthMiddleware)
		authzCtx := auth.GetAuthorizationContext(ctx)
		if !authzCtx.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
			log.Printf("User does not have access to session %s", sessionID)
			return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to access this session")
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

		// When the request is for the agent's /status endpoint and agentapi is
		// unreachable, check the provisioner's own /status to distinguish a
		// permanent failure (provisioner error → HTTP 500) from a transient
		// startup delay (provisioner still pending/provisioning → HTTP 502).
		if strings.HasSuffix(r.URL.Path, "/status") {
			if ks, ok := session.(*services.KubernetesSession); ok {
				provisionerURL := fmt.Sprintf("http://%s:%d/status", ks.ServiceDNS(), services.ProvisionerPort)
				provClient := &http.Client{Timeout: 2 * time.Second}
				provResp, provErr := provClient.Get(provisionerURL)
				if provErr == nil {
					defer func() { _ = provResp.Body.Close() }()
					if provResp.StatusCode == http.StatusInternalServerError {
						// Provisioner has permanently failed; relay its JSON error body.
						body, _ := io.ReadAll(provResp.Body)
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusInternalServerError)
						_, _ = w.Write(body)
						return
					}
				}
			}
		}

		http.Error(w, "Bad Gateway", http.StatusBadGateway)
	}

	proxy.ServeHTTP(w, req)
	return nil
}

// routeToRemoteSession proxies a session request to an external session manager (Proxy B).
// It signs the request with HMAC-SHA256 before forwarding.
func (c *SessionController) routeToRemoteSession(ctx echo.Context, route *repositories.SessionRoute) error {
	sessionID := ctx.Param("sessionId")

	// Check authorization
	if ctx.Request().Method != "OPTIONS" {
		authzCtx := auth.GetAuthorizationContext(ctx)
		if authzCtx == nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
		}
		// Basic auth check - we can't check session ownership without the actual session object,
		// but we can verify the user is authenticated. Full ownership check would require
		// fetching session info from B.
	}

	// Build target URL: replace A's session ID with B's remote session ID in the path
	originalPath := ctx.Request().URL.Path
	// Path is /<sessionId>/rest/of/path - replace sessionId with remoteSessionID
	suffix := strings.TrimPrefix(originalPath, "/"+sessionID)
	targetPath := "/" + route.RemoteSessionID + suffix

	targetURL := strings.TrimRight(route.ProxyURL, "/") + targetPath
	if ctx.Request().URL.RawQuery != "" {
		targetURL += "?" + ctx.Request().URL.RawQuery
	}

	// Read body for HMAC signing
	body, err := io.ReadAll(ctx.Request().Body)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Failed to read request body")
	}
	ctx.Request().Body = io.NopCloser(bytes.NewReader(body))

	// Build upstream request
	upstreamReq, err := http.NewRequestWithContext(ctx.Request().Context(), ctx.Request().Method, targetURL, bytes.NewReader(body))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to build upstream request")
	}

	// Compute HMAC signature over METHOD\nPATH?QUERY\nTIMESTAMP\nBODY
	ts := hmacutil.NowTimestamp()
	pathWithQuery := upstreamReq.URL.RequestURI()
	msg := hmacutil.BuildMessage(ctx.Request().Method, pathWithQuery, ts, body)
	sig := hmacutil.Sign([]byte(route.HMACSecret), msg)

	// Copy headers
	for key, values := range ctx.Request().Header {
		for _, v := range values {
			upstreamReq.Header.Add(key, v)
		}
	}
	upstreamReq.Header.Set("X-Hub-Signature-256", sig)
	upstreamReq.Header.Set(hmacutil.TimestampHeader, ts)

	// Include original user identity so Proxy B can enforce access control.
	// X-Forwarded-User is mandatory on Proxy B — always set it when proxying.
	authzCtx := auth.GetAuthorizationContext(ctx)
	if authzCtx != nil && authzCtx.PersonalScope.UserID != "" {
		upstreamReq.Header.Set("X-Forwarded-User", authzCtx.PersonalScope.UserID)
	}
	// For team-scoped sessions, also forward the team ID so Proxy B can build
	// the correct authorization context (service account tied to that team).
	if route.TeamID != "" {
		upstreamReq.Header.Set("X-Forwarded-Team", route.TeamID)
	}

	// Forward to Proxy B
	httpClient := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpClient.Do(upstreamReq)
	if err != nil {
		log.Printf("[REMOTE_ROUTE] Failed to proxy request to Proxy B for session %s: %v", sessionID, err)
		return echo.NewHTTPError(http.StatusBadGateway, "Failed to reach external session manager")
	}
	defer func() { _ = resp.Body.Close() }()

	// Copy response headers
	for key, values := range resp.Header {
		for _, v := range values {
			ctx.Response().Header().Add(key, v)
		}
	}
	ctx.Response().Header().Set("Access-Control-Allow-Origin", "*")

	// Copy response body
	ctx.Response().WriteHeader(resp.StatusCode)
	if _, err := io.Copy(ctx.Response().Writer, resp.Body); err != nil {
		log.Printf("[REMOTE_ROUTE] Failed to copy response body: %v", err)
	}
	return nil
}

// deleteRemoteSession deletes a session on Proxy B via the session manager API.
func (c *SessionController) deleteRemoteSession(ctx echo.Context, route *repositories.SessionRoute) error {
	sessionID := ctx.Param("sessionId")

	targetURL := strings.TrimRight(route.ProxyURL, "/") + "/api/v1/sessions/" + route.RemoteSessionID

	req, err := http.NewRequestWithContext(ctx.Request().Context(), http.MethodDelete, targetURL, nil)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to build delete request")
	}

	// Compute HMAC signature over METHOD\nPATH?QUERY\nTIMESTAMP\n(empty body)
	ts := hmacutil.NowTimestamp()
	parsedTarget, _ := url.Parse(targetURL)
	msg := hmacutil.BuildMessage(http.MethodDelete, parsedTarget.RequestURI(), ts, nil)
	sig := hmacutil.Sign([]byte(route.HMACSecret), msg)
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set(hmacutil.TimestampHeader, ts)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("[REMOTE_DELETE] Failed to delete remote session %s on %s: %v", route.RemoteSessionID, route.ProxyURL, err)
		return echo.NewHTTPError(http.StatusBadGateway, "Failed to reach external session manager")
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK:
		// success
	case http.StatusNotFound:
		// Session already gone on Proxy B — treat as success so we can
		// still clean up the local route entry.
		log.Printf("[REMOTE_DELETE] Remote session %s not found on Proxy B (already deleted), cleaning up local route", route.RemoteSessionID)
	default:
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[REMOTE_DELETE] Proxy B returned status %d: %s", resp.StatusCode, string(respBody))
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete remote session")
	}

	// Clean up local route entry regardless of whether Proxy B had the session.
	if c.sessionRouteRepo != nil {
		if err := c.sessionRouteRepo.Delete(ctx.Request().Context(), sessionID); err != nil {
			log.Printf("[REMOTE_DELETE] Warning: failed to delete route entry for session %s: %v", sessionID, err)
		}
	}

	log.Printf("[REMOTE_DELETE] Deleted remote session %s (remote ID: %s) on %s", sessionID, route.RemoteSessionID, route.ProxyURL)
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"message":    "Session terminated successfully",
		"session_id": sessionID,
		"status":     "terminated",
	})
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

// updateSessionTimestamp updates the session's updated_at and last_message_at timestamps.
// Called on every POST /message request routed through the proxy.
func (c *SessionController) updateSessionTimestamp(ctx echo.Context, session entities.Session) {
	// Update in-memory timestamps
	if ks, ok := session.(*services.KubernetesSession); ok {
		now := time.Now()
		ks.TouchUpdatedAt()
		ks.SetLastMessageAt(now)

		// Update Service annotations asynchronously to avoid blocking the request
		go func() {
			updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if manager, ok := c.getSessionManager().(*services.KubernetesSessionManager); ok {
				updatedAt := ks.UpdatedAt().Format(time.RFC3339)
				if err := manager.UpdateServiceAnnotation(updateCtx, session.ID(), "agentapi.proxy/updated-at", updatedAt); err != nil {
					log.Printf("[SESSION] Failed to update updated-at annotation for session %s: %v", session.ID(), err)
				}
				lastMessageAt := now.UTC().Format(time.RFC3339)
				if err := manager.UpdateServiceAnnotation(updateCtx, session.ID(), "agentapi.proxy/last-message-at", lastMessageAt); err != nil {
					log.Printf("[SESSION] Failed to update last-message-at annotation for session %s: %v", session.ID(), err)
				}
			}
		}()
	}
}

// filterHiddenSessions removes sessions tagged with hidden=true from the list.
func filterHiddenSessions(sessions []entities.Session) []entities.Session {
	result := make([]entities.Session, 0, len(sessions))
	for _, s := range sessions {
		if s.Tags()["hidden"] != "true" {
			result = append(result, s)
		}
	}
	return result
}

// fetchRemoteSessions queries all known external session managers (Proxy B instances) and returns
// their sessions as raw maps suitable for inclusion in the search response.
func (c *SessionController) fetchRemoteSessions(ctx context.Context, userID string, teams []string, filter entities.SessionFilter) []map[string]interface{} {
	if c.settingsRepo == nil {
		return nil
	}

	// Collect unique ESMs from user settings and team settings
	seen := make(map[string]struct{}) // deduplicate by ESM URL
	type esmEntry struct {
		url    string
		secret string
	}
	var esms []esmEntry

	addESMs := func(settingsName string) {
		settings, err := c.settingsRepo.FindByName(ctx, settingsName)
		if err != nil || settings == nil {
			return
		}
		for _, esm := range settings.ExternalSessionManagers() {
			if _, exists := seen[esm.URL]; !exists {
				seen[esm.URL] = struct{}{}
				esms = append(esms, esmEntry{url: esm.URL, secret: esm.HMACSecret})
			}
		}
	}

	addESMs(userID)
	for _, teamID := range teams {
		addESMs(teamID)
	}

	if len(esms) == 0 {
		return nil
	}

	var result []map[string]interface{}
	for _, esm := range esms {
		sessions := c.fetchSessionsFromESM(ctx, esm.url, esm.secret, userID, filter)
		result = append(result, sessions...)
	}
	return result
}

// fetchSessionsFromESM calls a single Proxy B's /api/v1/sessions endpoint and returns the sessions
func (c *SessionController) fetchSessionsFromESM(ctx context.Context, proxyURL, hmacSecret, userID string, filter entities.SessionFilter) []map[string]interface{} {
	// Build query params
	params := url.Values{}
	if filter.UserID != "" {
		params.Set("user_id", filter.UserID)
	} else {
		params.Set("user_id", userID)
	}
	if filter.Status != "" {
		params.Set("status", filter.Status)
	}
	if filter.Scope != "" {
		params.Set("scope", string(filter.Scope))
	}
	if filter.TeamID != "" {
		params.Set("team_id", filter.TeamID)
	}

	targetURL := strings.TrimRight(proxyURL, "/") + "/api/v1/sessions"
	if len(params) > 0 {
		targetURL += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		log.Printf("[REMOTE_SEARCH] Failed to build request to %s: %v", proxyURL, err)
		return nil
	}

	// Compute HMAC signature over METHOD\nPATH?QUERY\nTIMESTAMP\n(empty body)
	ts := hmacutil.NowTimestamp()
	parsedTarget, _ := url.Parse(targetURL)
	msg := hmacutil.BuildMessage(http.MethodGet, parsedTarget.RequestURI(), ts, nil)
	sig := hmacutil.Sign([]byte(hmacSecret), msg)
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set(hmacutil.TimestampHeader, ts)

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("[REMOTE_SEARCH] Failed to query ESM at %s: %v", proxyURL, err)
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[REMOTE_SEARCH] ESM at %s returned status %d", proxyURL, resp.StatusCode)
		return nil
	}

	var listResp struct {
		Sessions []struct {
			ID        string    `json:"id"`
			UserID    string    `json:"user_id"`
			Status    string    `json:"status"`
			CreatedAt time.Time `json:"created_at"`
		} `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		log.Printf("[REMOTE_SEARCH] Failed to decode response from %s: %v", proxyURL, err)
		return nil
	}

	result := make([]map[string]interface{}, 0, len(listResp.Sessions))
	for _, s := range listResp.Sessions {
		result = append(result, map[string]interface{}{
			"session_id":      s.ID,
			"user_id":         s.UserID,
			"scope":           "user",
			"team_id":         "",
			"status":          s.Status,
			"started_at":      s.CreatedAt,
			"updated_at":      s.CreatedAt,
			"last_message_at": s.CreatedAt,
			"addr":            "",
			"tags": map[string]string{
				"remote": "true", // mark as a remote (ESM) session
			},
			"metadata": map[string]interface{}{
				"description": "",
			},
			"proxy_url": proxyURL, // indicate this is a remote session
		})
	}
	return result
}

// setCORSHeaders sets CORS headers for all session management endpoints
func (c *SessionController) setCORSHeaders(ctx echo.Context) {
	ctx.Response().Header().Set("Access-Control-Allow-Origin", "*")
	ctx.Response().Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
	ctx.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
	ctx.Response().Header().Set("Access-Control-Allow-Credentials", "true")
	ctx.Response().Header().Set("Access-Control-Max-Age", "86400")
}
