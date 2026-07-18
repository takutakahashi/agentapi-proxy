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

type sessionAnnotationUpdater interface {
	UpdateSessionAnnotations(ctx context.Context, sessionID string, patch entities.UpdateSessionAnnotationsRequest) (entities.SessionAnnotations, error)
}

type sessionAnnotationsProvider interface {
	Annotations() entities.SessionAnnotations
}

// SessionController handles session management endpoints
type SessionController struct {
	sessionManagerProvider SessionManagerProvider
	sessionCreator         SessionCreator
	validateTeamUC         *sessionuc.ValidateTeamAccessUseCase
	sessionRouteRepo       repositories.SessionRouteRepository
	settingsRepo           repositories.SettingsRepository
	sessionProfileRepo     repositories.SessionProfileRepository
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

// WithSessionProfileRepository sets the session profile repository on the controller
func WithSessionProfileRepository(repo repositories.SessionProfileRepository) SessionControllerOption {
	return func(c *SessionController) {
		c.sessionProfileRepo = repo
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
	e.PATCH("/sessions/:sessionId/annotations", c.UpdateSessionAnnotations)
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

	// Service accounts cannot use user scope.
	// Automatically route to the service account's team scope.
	{
		resolvedScope, resolvedTeamID := authzCtx.ResolveScope(string(startReq.Scope), startReq.TeamID)
		if resolvedScope != string(startReq.Scope) || resolvedTeamID != startReq.TeamID {
			log.Printf("[SESSION_DEBUG] Service account %s: routing scope %q → %q (team %q)", userID, startReq.Scope, resolvedScope, resolvedTeamID)
		}
		startReq.Scope = entities.ResourceScope(resolvedScope)
		startReq.TeamID = resolvedTeamID
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

	// Resolve session profile: merge profile config into startReq fields.
	// When SessionProfileID is set, use that profile. Otherwise fall back to the
	// user/team's default profile. The profile is the base; explicit request fields override.
	if c.sessionProfileRepo != nil {
		profile := c.resolveSessionProfile(ctx.Request().Context(), startReq.SessionProfileID, userID, startReq.Scope, startReq.TeamID, startReq.Tags)
		if profile != nil {
			cfg := profile.Config()

			// Environment: profile is base, request keys override
			if len(cfg.Environment()) > 0 {
				merged := make(map[string]string, len(cfg.Environment()))
				for k, v := range cfg.Environment() {
					merged[k] = v
				}
				for k, v := range startReq.Environment {
					merged[k] = v
				}
				startReq.Environment = merged
			}

			// Tags: profile is base, request keys override
			if len(cfg.Tags()) > 0 {
				merged := make(map[string]string, len(cfg.Tags()))
				for k, v := range cfg.Tags() {
					merged[k] = v
				}
				for k, v := range startReq.Tags {
					merged[k] = v
				}
				startReq.Tags = merged
			}

			// Params: profile is base, request fields override per-field
			if cfg.Params() != nil {
				if startReq.Params == nil {
					startReq.Params = cfg.Params()
				} else {
					startReq.Params = mergeSessionParams(cfg.Params(), startReq.Params)
				}
			}

			// MemoryKey: profile is base, request keys override
			if len(cfg.MemoryKey()) > 0 {
				merged := make(map[string]string, len(cfg.MemoryKey()))
				for k, v := range cfg.MemoryKey() {
					merged[k] = v
				}
				for k, v := range startReq.MemoryKey {
					merged[k] = v
				}
				startReq.MemoryKey = merged
			}

			// SandboxPolicyID: apply profile's policy when request does not already specify one.
			if startReq.Params == nil {
				startReq.Params = &entities.SessionParams{}
			}
			applyProfileSandboxDefaults(cfg, startReq.Params)

			// SessionTTL: apply profile's TTL when request does not already specify one.
			if cfg.SessionTTL() != "" {
				if startReq.Params == nil {
					startReq.Params = &entities.SessionParams{}
				}
				if startReq.Params.SessionTTL == "" {
					startReq.Params.SessionTTL = cfg.SessionTTL()
				}
			}
			if len(cfg.UnsyncedFilePaths()) > 0 {
				if startReq.Params == nil {
					startReq.Params = &entities.SessionParams{}
				}
				if len(startReq.Params.UnsyncedFilePaths) == 0 {
					startReq.Params.UnsyncedFilePaths = cfg.UnsyncedFilePaths()
				}
			}
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

	// Service accounts cannot use user scope.
	// Automatically route to the service account's team scope when scope is not explicitly "team".
	{
		resolvedScope, resolvedTeamID := authzCtx.ResolveScope(scopeFilter, teamIDFilter)
		if resolvedScope != scopeFilter || resolvedTeamID != teamIDFilter {
			log.Printf("[SESSION_DEBUG] Service account %s search: routing scope %q → %q (team %q)", userID, scopeFilter, resolvedScope, resolvedTeamID)
		}
		scopeFilter = resolvedScope
		teamIDFilter = resolvedTeamID
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

	// Load ESM routes before building the response so the allocated session IDs
	// can be removed from the local list. Only the parent proxy's SessionID is
	// public; RemoteSessionID is an implementation detail used for routing.
	var routes []*repositories.SessionRoute
	allocatedSessions := make(map[string]entities.Session)
	if c.sessionRouteRepo != nil {
		var err error
		routes, err = c.sessionRouteRepo.List(ctx.Request().Context(), userID)
		if err != nil {
			log.Printf("[SEARCH] Failed to list session routes: %v", err)
			routes = nil
		} else {
			allocatedSessions = indexAllocatedSessions(matchingSessions, routes)
			matchingSessions = excludeAllocatedSessions(matchingSessions, routes)
		}
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

		annotations := getSessionAnnotations(session)
		description := initialMessage
		if annotations.Description != "" {
			description = annotations.Description
		}
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
			"annotations":     annotations,
			"metadata": map[string]interface{}{
				"description": description,
			},
		}
		if ks, ok := session.(*services.KubernetesSession); ok {
			if req := ks.Request(); req != nil && req.Sandbox != nil {
				sessionData["sandbox_policy_id"] = req.Sandbox.PolicyID
			}
		}
		filteredSessions = append(filteredSessions, sessionData)
	}

	// Include ESM-created sessions from session routes
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
		status := routedSessionStatus(route, allocatedSessions)
		filteredSessions = append(filteredSessions, map[string]interface{}{
			"session_id":           route.SessionID,
			"allocated_session_id": route.RemoteSessionID,
			"user_id":              route.UserID,
			"scope":                route.Scope,
			"team_id":              route.TeamID,
			"status":               status,
			"started_at":           route.StartedAt,
			"updated_at":           route.StartedAt,
			"last_message_at":      route.StartedAt,
			"addr":                 "",
			"tags":                 tags,
			"annotations":          entities.SessionAnnotations{},
			"metadata": map[string]interface{}{
				"description": route.InitialMessage,
			},
		})
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"sessions": filteredSessions,
	})
}

func routedSessionStatus(route *repositories.SessionRoute, allocatedSessions map[string]entities.Session) string {
	if route.RemoteSessionID == "" {
		return "creating"
	}
	if allocatedSession := allocatedSessions[route.RemoteSessionID]; allocatedSession != nil {
		return allocatedSession.Status()
	}
	return "active"
}

func indexAllocatedSessions(sessions []entities.Session, routes []*repositories.SessionRoute) map[string]entities.Session {
	allocatedIDs := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		if route.RemoteSessionID != "" && route.RemoteSessionID != route.SessionID {
			allocatedIDs[route.RemoteSessionID] = struct{}{}
		}
	}

	allocatedSessions := make(map[string]entities.Session, len(allocatedIDs))
	for _, session := range sessions {
		if _, allocated := allocatedIDs[session.ID()]; allocated {
			allocatedSessions[session.ID()] = session
		}
	}
	return allocatedSessions
}

func excludeAllocatedSessions(sessions []entities.Session, routes []*repositories.SessionRoute) []entities.Session {
	allocatedIDs := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		if route.RemoteSessionID != "" && route.RemoteSessionID != route.SessionID {
			allocatedIDs[route.RemoteSessionID] = struct{}{}
		}
	}

	filtered := make([]entities.Session, 0, len(sessions))
	for _, session := range sessions {
		if _, allocated := allocatedIDs[session.ID()]; !allocated {
			filtered = append(filtered, session)
		}
	}
	return filtered
}

func getSessionAnnotations(session entities.Session) entities.SessionAnnotations {
	if annotated, ok := session.(sessionAnnotationsProvider); ok {
		return annotated.Annotations()
	}
	return entities.SessionAnnotations{}
}

// UpdateSessionAnnotations handles PATCH /sessions/:sessionId/annotations.
func (c *SessionController) UpdateSessionAnnotations(ctx echo.Context) error {
	c.setCORSHeaders(ctx)

	sessionID := ctx.Param("sessionId")
	if sessionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Session ID is required")
	}

	session := c.getSessionManager().GetSession(sessionID)
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	authzCtx := auth.GetAuthorizationContext(ctx)
	if !authzCtx.CanModifyResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to update this session")
	}

	var req entities.UpdateSessionAnnotationsRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	updater, ok := c.getSessionManager().(sessionAnnotationUpdater)
	if !ok {
		return echo.NewHTTPError(http.StatusInternalServerError, "Session annotations are not supported")
	}

	annotations, err := updater.UpdateSessionAnnotations(ctx.Request().Context(), sessionID, req)
	if err != nil {
		log.Printf("Failed to update session annotations for %s: %v", sessionID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to update session annotations")
	}

	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"session_id":  sessionID,
		"annotations": annotations,
		"metadata": map[string]interface{}{
			"description": annotations.Description,
		},
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
				if route.ProxyURL == "" && route.RemoteSessionID != "" {
					return c.deleteLocalSessionAlias(ctx, route)
				}
				return c.deleteRemoteSession(ctx, route)
			}
		}
		log.Printf("Delete session failed: session %s not found (requested by %s)", sessionID, clientIP)
		if session == nil {
			return echo.NewHTTPError(http.StatusNotFound, "Session not found")
		}
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
		// Check if this is a remote session on External Session Manager
		if c.sessionRouteRepo != nil {
			route, err := c.sessionRouteRepo.Get(ctx.Request().Context(), sessionID)
			if err != nil {
				log.Printf("[ROUTE] Failed to look up session route for %s: %v", sessionID, err)
			} else if route != nil {
				if route.ProxyURL == "" && route.RemoteSessionID != "" {
					session = c.getSessionManager().GetSession(route.RemoteSessionID)
					if session != nil {
						sessionID = route.SessionID
					} else {
						return echo.NewHTTPError(http.StatusNotFound, "Session not found")
					}
				} else {
					return c.routeToRemoteSession(ctx, route)
				}
			}
		}
		if session == nil {
			return echo.NewHTTPError(http.StatusNotFound, "Session not found")
		}
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

func (c *SessionController) deleteLocalSessionAlias(ctx echo.Context, route *repositories.SessionRoute) error {
	session := c.getSessionManager().GetSession(route.RemoteSessionID)
	if session == nil {
		// The runtime may already have removed itself (for example via a oneshot
		// Stop hook). Authorize from the persisted route metadata and make DELETE
		// idempotently clean up the stale public alias.
		authzCtx := auth.GetAuthorizationContext(ctx)
		if !authzCtx.CanModifyResource(route.UserID, route.Scope, route.TeamID) {
			return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to delete this session")
		}
		if err := c.sessionRouteRepo.Delete(ctx.Request().Context(), route.SessionID); err != nil {
			log.Printf("Failed to delete stale session alias %s: %v", route.SessionID, err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete session alias")
		}
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"message": "Stale session alias removed", "session_id": route.SessionID, "status": "terminated",
		})
	}
	authzCtx := auth.GetAuthorizationContext(ctx)
	if !authzCtx.CanModifyResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to delete this session")
	}
	if err := c.sessionCreator.DeleteSessionByID(route.RemoteSessionID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete session")
	}
	if err := c.sessionRouteRepo.Delete(ctx.Request().Context(), route.SessionID); err != nil {
		log.Printf("Failed to delete session alias %s: %v", route.SessionID, err)
	}
	return ctx.JSON(http.StatusOK, map[string]interface{}{
		"message": "Session terminated successfully", "session_id": route.SessionID, "status": "terminated",
	})
}

// routeToRemoteSession proxies a session request to an external session manager (External Session Manager).
// It signs the request with HMAC-SHA256 before forwarding.
func (c *SessionController) routeToRemoteSession(ctx echo.Context, route *repositories.SessionRoute) error {
	sessionID := ctx.Param("sessionId")
	if route.ProxyURL == "" || route.RemoteSessionID == "" {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "External session manager has not reported a routable session yet")
	}

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

	// Include original user identity so External Session Manager can enforce access control.
	// X-Forwarded-User is mandatory on External Session Manager — always set it when proxying.
	authzCtx := auth.GetAuthorizationContext(ctx)
	if authzCtx != nil && authzCtx.PersonalScope.UserID != "" {
		upstreamReq.Header.Set("X-Forwarded-User", authzCtx.PersonalScope.UserID)
	}
	// For team-scoped sessions, also forward the team ID so External Session Manager can build
	// the correct authorization context (service account tied to that team).
	if route.TeamID != "" {
		upstreamReq.Header.Set("X-Forwarded-Team", route.TeamID)
	}

	// Forward to External Session Manager
	httpClient := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpClient.Do(upstreamReq)
	if err != nil {
		log.Printf("[REMOTE_ROUTE] Failed to proxy request to External Session Manager for session %s: %v", sessionID, err)
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

// deleteRemoteSession deletes a session on External Session Manager via the session manager API.
func (c *SessionController) deleteRemoteSession(ctx echo.Context, route *repositories.SessionRoute) error {
	sessionID := ctx.Param("sessionId")
	if route.ProxyURL == "" || route.RemoteSessionID == "" {
		if c.sessionRouteRepo != nil {
			if err := c.sessionRouteRepo.Delete(ctx.Request().Context(), sessionID); err != nil {
				log.Printf("[REMOTE_DELETE] Warning: failed to delete pending route entry for session %s: %v", sessionID, err)
			}
		}
		return ctx.JSON(http.StatusOK, map[string]interface{}{
			"message":    "Pending external session removed",
			"session_id": sessionID,
			"status":     "terminated",
		})
	}

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
		// Session already gone on External Session Manager — treat as success so we can
		// still clean up the local route entry.
		log.Printf("[REMOTE_DELETE] Remote session %s not found on External Session Manager (already deleted), cleaning up local route", route.RemoteSessionID)
	default:
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[REMOTE_DELETE] External Session Manager returned status %d: %s", resp.StatusCode, string(respBody))
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete remote session")
	}

	// Clean up local route entry regardless of whether External Session Manager had the session.
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

// setCORSHeaders sets CORS headers for all session management endpoints
func (c *SessionController) setCORSHeaders(ctx echo.Context) {
	ctx.Response().Header().Set("Access-Control-Allow-Origin", "*")
	ctx.Response().Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
	ctx.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
	ctx.Response().Header().Set("Access-Control-Allow-Credentials", "true")
	ctx.Response().Header().Set("Access-Control-Max-Age", "86400")
}

// mergeSessionParams merges base (profile) params with override (request) params.
// For each field: if the override field is the zero value, the base value is used.
func mergeSessionParams(base, override *entities.SessionParams) *entities.SessionParams {
	merged := *base // start from profile defaults
	if override.Message != "" {
		merged.Message = override.Message
	}
	if override.GithubToken != "" {
		merged.GithubToken = override.GithubToken
	}
	if override.AgentType != "" {
		merged.AgentType = override.AgentType
	}
	if override.Slack != nil {
		merged.Slack = override.Slack
	}
	if override.Oneshot {
		merged.Oneshot = override.Oneshot
	}
	if override.InitialMessageWaitSecond != nil {
		merged.InitialMessageWaitSecond = override.InitialMessageWaitSecond
	}
	if override.ManagerID != "" {
		merged.ManagerID = override.ManagerID
	}
	if override.CycleMessage != "" {
		merged.CycleMessage = override.CycleMessage
	}
	if override.CycleMaxCount != 0 {
		merged.CycleMaxCount = override.CycleMaxCount
	}
	if override.RepoFullName != "" {
		merged.RepoFullName = override.RepoFullName
	}
	if override.Sandbox != nil {
		merged.Sandbox = override.Sandbox
	}
	if override.Docker != nil {
		merged.Docker = override.Docker
	}
	if override.AuthProxy != nil {
		merged.AuthProxy = override.AuthProxy
	}
	if override.SessionTTL != "" {
		merged.SessionTTL = override.SessionTTL
	}
	if len(override.UnsyncedFilePaths) > 0 {
		merged.UnsyncedFilePaths = append([]string(nil), override.UnsyncedFilePaths...)
	}
	if override.CredentialSource != "" {
		merged.CredentialSource = override.CredentialSource
	}
	return &merged
}

func applyProfileSandboxDefaults(cfg entities.SessionProfileConfig, params *entities.SessionParams) {
	if params == nil {
		return
	}
	if cfg.SandboxPolicyID() != "" {
		if params.Sandbox == nil {
			params.Sandbox = &entities.SandboxParams{Enabled: true, PolicyID: cfg.SandboxPolicyID()}
		} else if params.Sandbox.PolicyID == "" {
			params.Sandbox.Enabled = true
			params.Sandbox.PolicyID = cfg.SandboxPolicyID()
		}
		return
	}
	if params.Sandbox == nil {
		params.Sandbox = &entities.SandboxParams{Enabled: true, CountMode: true}
	} else if params.Sandbox.PolicyID == "" {
		params.Sandbox.Enabled = true
		params.Sandbox.CountMode = true
	}
}

// SandboxDomainsResponse is the JSON body returned by GET /sessions/:sessionId/sandbox-domains.
type SandboxDomainsResponse struct {
	Allowed []string `json:"allowed"`
	Denied  []string `json:"denied"`
}

// GetSessionSandboxDomains handles GET /sessions/:sessionId/sandbox-domains.
// It forwards the request to the session's agent-provisioner /sandbox-domains endpoint,
// which in turn queries the network filter control server (127.0.0.1:3129/domains).
// Returns 404 when the session does not exist, 503 when the network filter is unavailable.
func (c *SessionController) GetSessionSandboxDomains(ctx echo.Context) error {
	sessionID := ctx.Param("sessionId")

	session := c.getSessionManager().GetSession(sessionID)
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	authzCtx := auth.GetAuthorizationContext(ctx)
	if !authzCtx.CanAccessResource(session.UserID(), string(session.Scope()), session.TeamID()) {
		return echo.NewHTTPError(http.StatusForbidden, "You don't have permission to access this session")
	}

	ks, ok := session.(*services.KubernetesSession)
	if !ok {
		return echo.NewHTTPError(http.StatusNotImplemented, "Sandbox domains not available for this session type")
	}

	provisionerURL := fmt.Sprintf("http://%s:%d/sandbox-domains", ks.ServiceDNS(), services.ProvisionerPort)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(provisionerURL)
	if err != nil {
		log.Printf("[SESSION] Failed to fetch sandbox domains for %s: %v", sessionID, err)
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Network filter not available")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusServiceUnavailable {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Network filter not available for this session")
	}

	var domainsResp SandboxDomainsResponse
	if err := json.NewDecoder(resp.Body).Decode(&domainsResp); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to parse domain response")
	}
	if domainsResp.Allowed == nil {
		domainsResp.Allowed = []string{}
	}
	if domainsResp.Denied == nil {
		domainsResp.Denied = []string{}
	}

	return ctx.JSON(http.StatusOK, domainsResp)
}

// resolveSessionProfile returns the session profile to apply for a session creation request.
// If profileID is set, it fetches that profile directly.
// Otherwise it searches for a selector_tags match before falling back to the settings default,
// then the legacy profile-level default flag.
func (c *SessionController) resolveSessionProfile(ctx context.Context, profileID, userID string, scope entities.ResourceScope, teamID string, tags map[string]string) *entities.SessionProfile {
	if profileID != "" {
		profile, err := c.sessionProfileRepo.Get(ctx, profileID)
		if err != nil {
			log.Printf("[SESSION] Warning: could not resolve session_profile_id %q: %v", profileID, err)
			return nil
		}
		return profile
	}

	filter := repositories.SessionProfileFilter{
		UserID: userID,
		Scope:  scope,
	}
	if scope == entities.ScopeTeam {
		filter.TeamID = teamID
	}
	profiles, err := c.sessionProfileRepo.List(ctx, filter)
	if err != nil {
		log.Printf("[SESSION] Warning: could not list session profiles for default lookup: %v", err)
		return nil
	}
	if profile := selectSessionProfileByTags(profiles, tags); profile != nil {
		log.Printf("[SESSION] Applying tag-selected session profile %q (%s) for user %s", profile.ID(), profile.Name(), userID)
		return profile
	}
	if profile := c.resolveSettingsDefaultSessionProfile(ctx, userID, scope, teamID, profiles); profile != nil {
		log.Printf("[SESSION] Applying settings default session profile %q (%s) for user %s", profile.ID(), profile.Name(), userID)
		return profile
	}
	for _, p := range profiles {
		if p.IsDefault() {
			log.Printf("[SESSION] Applying default session profile %q (%s) for user %s", p.ID(), p.Name(), userID)
			return p
		}
	}
	return nil
}

func (c *SessionController) resolveSettingsDefaultSessionProfile(ctx context.Context, userID string, scope entities.ResourceScope, teamID string, profiles []*entities.SessionProfile) *entities.SessionProfile {
	if c.settingsRepo == nil {
		return nil
	}
	settingsName := userID
	if scope == entities.ScopeTeam && teamID != "" {
		settingsName = teamID
	}
	settings, err := c.settingsRepo.FindByName(ctx, settingsName)
	if err != nil || settings == nil || settings.DefaultSessionProfileID() == "" {
		return nil
	}
	defaultID := settings.DefaultSessionProfileID()
	for _, p := range profiles {
		if p.ID() == defaultID {
			return p
		}
	}
	profile, err := c.sessionProfileRepo.Get(ctx, defaultID)
	if err != nil {
		log.Printf("[SESSION] Warning: could not resolve default_session_profile_id %q from settings %q: %v", defaultID, settingsName, err)
		return nil
	}
	return profile
}

func selectSessionProfileByTags(profiles []*entities.SessionProfile, tags map[string]string) *entities.SessionProfile {
	var matches []*entities.SessionProfile
	for _, p := range profiles {
		if p.MatchesSelectorTags(tags) {
			matches = append(matches, p)
		}
	}
	if len(matches) == 0 {
		return nil
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].SelectorSpecificity() != matches[j].SelectorSpecificity() {
			return matches[i].SelectorSpecificity() > matches[j].SelectorSpecificity()
		}
		if matches[i].IsDefault() != matches[j].IsDefault() {
			return matches[i].IsDefault()
		}
		if matches[i].Name() != matches[j].Name() {
			return matches[i].Name() < matches[j].Name()
		}
		return matches[i].ID() < matches[j].ID()
	})
	return matches[0]
}
