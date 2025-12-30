package proxy

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// ShareHandlers handles session sharing endpoints
type ShareHandlers struct {
	proxy     *Proxy
	shareRepo ShareRepository
}

// NewShareHandlers creates a new ShareHandlers instance
func NewShareHandlers(proxy *Proxy, shareRepo ShareRepository) *ShareHandlers {
	return &ShareHandlers{
		proxy:     proxy,
		shareRepo: shareRepo,
	}
}

// GetName returns the name of this handler for logging
func (h *ShareHandlers) GetName() string {
	return "ShareHandlers"
}

// CreateShare handles POST /sessions/:sessionId/share to create a share URL
func (h *ShareHandlers) CreateShare(c echo.Context) error {
	h.setCORSHeaders(c)

	sessionID := c.Param("sessionId")
	if sessionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Session ID is required")
	}

	// Verify session exists
	session := h.proxy.GetSessionManager().GetSession(sessionID)
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	// Only session owner or admin can create share
	if !auth.UserOwnsSession(c, session.UserID()) {
		return echo.NewHTTPError(http.StatusForbidden, "Only session owner can create share URL")
	}

	// Get user info
	user := auth.GetUserFromContext(c)
	var userID string
	if user != nil {
		userID = string(user.ID())
	} else {
		userID = "anonymous"
	}

	// Check if share already exists
	existingShare, err := h.shareRepo.FindBySessionID(sessionID)
	if err == nil && existingShare != nil {
		// Return existing share
		return c.JSON(http.StatusOK, map[string]interface{}{
			"token":      existingShare.Token(),
			"session_id": existingShare.SessionID(),
			"share_url":  fmt.Sprintf("/s/%s/", existingShare.Token()),
			"created_at": existingShare.CreatedAt(),
			"expires_at": existingShare.ExpiresAt(),
		})
	}

	// Create new share
	share := NewSessionShare(sessionID, userID)

	// Save share
	if err := h.shareRepo.Save(share); err != nil {
		log.Printf("Failed to save share for session %s: %v", sessionID, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to create share")
	}

	log.Printf("Created share for session %s by user %s, token: %s", sessionID, userID, share.Token())

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"token":      share.Token(),
		"session_id": share.SessionID(),
		"share_url":  fmt.Sprintf("/s/%s/", share.Token()),
		"created_at": share.CreatedAt(),
		"expires_at": share.ExpiresAt(),
	})
}

// GetShare handles GET /sessions/:sessionId/share to get share status
func (h *ShareHandlers) GetShare(c echo.Context) error {
	h.setCORSHeaders(c)

	sessionID := c.Param("sessionId")
	if sessionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Session ID is required")
	}

	// Verify session exists
	session := h.proxy.GetSessionManager().GetSession(sessionID)
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	// Only session owner or admin can view share status
	if !auth.UserOwnsSession(c, session.UserID()) {
		return echo.NewHTTPError(http.StatusForbidden, "Only session owner can view share status")
	}

	// Get share
	share, err := h.shareRepo.FindBySessionID(sessionID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Share not found for this session")
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"token":      share.Token(),
		"session_id": share.SessionID(),
		"share_url":  fmt.Sprintf("/s/%s/", share.Token()),
		"created_by": share.CreatedBy(),
		"created_at": share.CreatedAt(),
		"expires_at": share.ExpiresAt(),
		"is_expired": share.IsExpired(),
	})
}

// DeleteShare handles DELETE /sessions/:sessionId/share to revoke share
func (h *ShareHandlers) DeleteShare(c echo.Context) error {
	h.setCORSHeaders(c)

	sessionID := c.Param("sessionId")
	if sessionID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Session ID is required")
	}

	// Verify session exists
	session := h.proxy.GetSessionManager().GetSession(sessionID)
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	// Only session owner or admin can delete share
	if !auth.UserOwnsSession(c, session.UserID()) {
		return echo.NewHTTPError(http.StatusForbidden, "Only session owner can revoke share URL")
	}

	// Delete share
	if err := h.shareRepo.Delete(sessionID); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Share not found for this session")
	}

	log.Printf("Deleted share for session %s", sessionID)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"message":    "Share revoked successfully",
		"session_id": sessionID,
	})
}

// RouteToSharedSession handles ANY /s/:shareToken/* to access shared session
func (h *ShareHandlers) RouteToSharedSession(c echo.Context) error {
	shareToken := c.Param("shareToken")

	// Find share by token
	share, err := h.shareRepo.FindByToken(shareToken)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Invalid share token")
	}

	// Check if share is expired
	if share.IsExpired() {
		return echo.NewHTTPError(http.StatusGone, "Share link has expired")
	}

	// Get session
	session := h.proxy.GetSessionManager().GetSession(share.SessionID())
	if session == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}

	// Only allow GET requests for shared sessions
	if c.Request().Method != http.MethodGet && c.Request().Method != http.MethodOptions {
		return echo.NewHTTPError(http.StatusForbidden, "Shared sessions are read-only")
	}

	// Skip auth check for OPTIONS requests
	if c.Request().Method == http.MethodOptions {
		h.setCORSHeaders(c)
		return c.NoContent(http.StatusNoContent)
	}

	// Determine target URL using session address
	targetURL := fmt.Sprintf("http://%s", session.Addr())
	target, err := url.Parse(targetURL)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Invalid target URL: %v", err))
	}

	req := c.Request()
	w := c.Response()

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = time.Millisecond * 100

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)

		// Remove /s/:shareToken from path before forwarding
		// Path format: /s/{shareToken}/{remaining_path}
		originalPath := req.URL.Path
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
		// Add header to indicate this is a shared session access
		req.Header.Set("X-Shared-Session", "true")
		req.Header.Set("X-Share-Token", shareToken)
	}

	originalModifyResponse := proxy.ModifyResponse
	proxy.ModifyResponse = func(resp *http.Response) error {
		// Set CORS headers
		resp.Header.Set("Access-Control-Allow-Origin", "*")
		resp.Header.Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
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
		log.Printf("Proxy error for shared session %s (token: %s): %v", share.SessionID(), shareToken, err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
	}

	proxy.ServeHTTP(w, req)
	return nil
}

// setCORSHeaders sets CORS headers for share endpoints
func (h *ShareHandlers) setCORSHeaders(c echo.Context) {
	c.Response().Header().Set("Access-Control-Allow-Origin", "*")
	c.Response().Header().Set("Access-Control-Allow-Methods", "GET, HEAD, PUT, PATCH, POST, DELETE, OPTIONS")
	c.Response().Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, X-Requested-With, X-Forwarded-For, X-Forwarded-Proto, X-Forwarded-Host, X-API-Key")
	c.Response().Header().Set("Access-Control-Allow-Credentials", "true")
	c.Response().Header().Set("Access-Control-Max-Age", "86400")
}
