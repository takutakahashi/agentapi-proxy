// Package sessionmanager implements the session manager forwarding endpoint.
//
// This package enables "small-cluster mode": a Proxy B instance that accepts
// pre-built SessionSettings from a trusted upstream Proxy A and creates sessions
// without needing any local secrets (agentapi-settings-*, GitHub secrets, etc.).
//
// All requests to /api/v1/sessions must carry an HMAC-SHA256 signature in the
// X-Hub-Signature-256 header, computed over the raw request body using the
// shared secret configured via SESSION_MANAGER_HMAC_SECRET (or config file).
package sessionmanager

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

// Handlers implements app.CustomHandler for the session manager forwarding endpoint.
type Handlers struct {
	sessionManager repositories.SessionManager
	hmacSecret     []byte
}

// NewHandlers creates a new Handlers instance.
// hmacSecret must be non-empty; if it is empty, RegisterRoutes will refuse to register.
func NewHandlers(sessionManager repositories.SessionManager, hmacSecret string) *Handlers {
	return &Handlers{
		sessionManager: sessionManager,
		hmacSecret:     []byte(hmacSecret),
	}
}

// GetName returns the handler name for logging.
func (h *Handlers) GetName() string {
	return "SessionManagerHandlers"
}

// RegisterRoutes registers /api/v1/sessions routes with HMAC middleware.
// If hmacSecret is empty, registration is skipped with a warning.
func (h *Handlers) RegisterRoutes(e *echo.Echo, _ *app.Server) error {
	if len(h.hmacSecret) == 0 {
		log.Printf("[SESSION_MANAGER] Warning: HMAC secret is empty, skipping route registration")
		return nil
	}

	g := e.Group("/api/v1/sessions")
	g.Use(h.hmacMiddleware())

	g.POST("", h.CreateSession)
	g.GET("", h.ListSessions)
	g.GET("/:sessionId", h.GetSession)
	g.DELETE("/:sessionId", h.DeleteSession)

	log.Printf("[SESSION_MANAGER] Registered routes under /api/v1/sessions")
	return nil
}

// ---------------------------------------------------------------------------
// HMAC middleware
// ---------------------------------------------------------------------------

// hmacMiddleware verifies X-Hub-Signature-256: sha256=<hex> on every request.
// It reads and restores the body so downstream handlers can also read it.
func (h *Handlers) hmacMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			sig := c.Request().Header.Get("X-Hub-Signature-256")
			if sig == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing X-Hub-Signature-256 header")
			}

			// Read body and immediately restore it for downstream handlers.
			body, err := io.ReadAll(c.Request().Body)
			if err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "failed to read request body")
			}
			c.Request().Body = io.NopCloser(bytes.NewReader(body))

			// Compute expected signature.
			mac := hmac.New(sha256.New, h.hmacSecret)
			mac.Write(body)
			expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))

			// Constant-time comparison to prevent timing attacks.
			if !hmac.Equal([]byte(sig), []byte(expected)) {
				log.Printf("[SESSION_MANAGER] HMAC verification failed for %s %s",
					c.Request().Method, c.Request().URL.Path)
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid signature")
			}

			return next(c)
		}
	}
}

// ---------------------------------------------------------------------------
// Request / Response types
// ---------------------------------------------------------------------------

// CreateSessionResponse is returned after successful session creation.
type CreateSessionResponse struct {
	SessionID string `json:"session_id"`
}

// SessionInfo is a lightweight session representation returned by list/get.
type SessionInfo struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// ListSessionsResponse wraps the list of sessions.
type ListSessionsResponse struct {
	Sessions []SessionInfo `json:"sessions"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// CreateSession handles POST /api/v1/sessions.
//
// Body: sessionsettings.SessionSettings JSON (pre-built by upstream Proxy A).
// The settings are used verbatim as the provision payload; no local secrets
// are resolved on Proxy B.
func (h *Handlers) CreateSession(c echo.Context) error {
	var settings sessionsettings.SessionSettings
	if err := c.Bind(&settings); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	if settings.Session.UserID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "session.user_id is required")
	}

	sessionID := uuid.New().String()

	// Reconstruct a minimal RunServerRequest from the settings metadata.
	// Secrets are NOT re-resolved here — they are already embedded in settings.Env.
	req := &entities.RunServerRequest{
		UserID:            settings.Session.UserID,
		Scope:             entities.ResourceScope(settings.Session.Scope),
		TeamID:            settings.Session.TeamID,
		AgentType:         settings.Session.AgentType,
		Oneshot:           settings.Session.Oneshot,
		Teams:             settings.Session.Teams,
		InitialMessage:    settings.InitialMessage,
		ProvisionSettings: &settings,
	}
	if settings.Repository != nil {
		req.RepoInfo = &entities.RepositoryInfo{
			FullName: settings.Repository.FullName,
			CloneDir: settings.Repository.CloneDir,
		}
	}

	session, err := h.sessionManager.CreateSession(c.Request().Context(), sessionID, req, nil)
	if err != nil {
		log.Printf("[SESSION_MANAGER] Failed to create session %s: %v", sessionID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to create session: %v", err))
	}

	log.Printf("[SESSION_MANAGER] Created session %s for user %s", session.ID(), settings.Session.UserID)
	return c.JSON(http.StatusCreated, CreateSessionResponse{SessionID: session.ID()})
}

// ListSessions handles GET /api/v1/sessions.
//
// Optional query parameters:
//   - user_id  : filter by user ID
//   - scope    : "user" or "team"
func (h *Handlers) ListSessions(c echo.Context) error {
	filter := entities.SessionFilter{}

	if userID := c.QueryParam("user_id"); userID != "" {
		filter.UserID = userID
	}
	if scope := c.QueryParam("scope"); scope != "" {
		filter.Scope = entities.ResourceScope(scope)
	}

	sessions := h.sessionManager.ListSessions(filter)

	infos := make([]SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		infos = append(infos, SessionInfo{
			ID:     s.ID(),
			UserID: s.UserID(),
			Status: strings.ToLower(string(s.Status())),
		})
	}

	return c.JSON(http.StatusOK, ListSessionsResponse{Sessions: infos})
}

// GetSession handles GET /api/v1/sessions/:sessionId.
func (h *Handlers) GetSession(c echo.Context) error {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "sessionId is required")
	}

	session := h.sessionManager.GetSession(sessionID)
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "session not found")
	}

	return c.JSON(http.StatusOK, SessionInfo{
		ID:     session.ID(),
		UserID: session.UserID(),
		Status: strings.ToLower(string(session.Status())),
	})
}

// DeleteSession handles DELETE /api/v1/sessions/:sessionId.
func (h *Handlers) DeleteSession(c echo.Context) error {
	sessionID := c.Param("sessionId")
	if sessionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "sessionId is required")
	}

	if err := h.sessionManager.DeleteSession(sessionID); err != nil {
		log.Printf("[SESSION_MANAGER] Failed to delete session %s: %v", sessionID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to delete session: %v", err))
	}

	log.Printf("[SESSION_MANAGER] Deleted session %s", sessionID)
	return c.NoContent(http.StatusNoContent)
}
