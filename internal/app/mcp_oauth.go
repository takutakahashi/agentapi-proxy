package app

import (
	"bytes"
	"html/template"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/mcpoauth"
	"github.com/takutakahashi/agentapi-proxy/pkg/mcpoauth/callbackui"
)

// MCPOAuthConnectRequest is the body for POST /mcp-oauth/connect.
type MCPOAuthConnectRequest struct {
	// ServerName is the key in the user's mcp_servers settings map.
	ServerName string `json:"server_name"`
	// MCPServerURL is the remote MCP server URL (required).
	MCPServerURL string `json:"mcp_server_url"`
	// ClientID overrides DCR (optional).
	ClientID string `json:"client_id,omitempty"`
	// ClientSecret is used for confidential clients (optional).
	ClientSecret string `json:"client_secret,omitempty"`
	// Scopes is the list of OAuth scopes to request (optional).
	Scopes []string `json:"scopes,omitempty"`
}

// MCPOAuthConnectResponse is returned by POST /mcp-oauth/connect.
type MCPOAuthConnectResponse struct {
	AuthorizationURL string `json:"authorization_url"`
	State            string `json:"state"`
}

// MCPOAuthStatusResponse is returned by GET /mcp-oauth/status/:serverName.
type MCPOAuthStatusResponse struct {
	ServerName      string `json:"server_name"`
	Connected       bool   `json:"connected"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	HasRefreshToken bool   `json:"has_refresh_token"`
}

// setupMCPOAuthRoutes registers all MCP OAuth endpoints.
func (s *Server) setupMCPOAuthRoutes() {
	// Authenticated endpoints.
	s.echo.POST("/mcp-oauth/connect", s.handleMCPOAuthConnect,
		auth.RequirePermission(entities.PermissionSessionRead, s.container.AuthService))
	s.echo.GET("/mcp-oauth/status/:serverName", s.handleMCPOAuthStatus,
		auth.RequirePermission(entities.PermissionSessionRead, s.container.AuthService))
	s.echo.DELETE("/mcp-oauth/disconnect/:serverName", s.handleMCPOAuthDisconnect,
		auth.RequirePermission(entities.PermissionSessionRead, s.container.AuthService))

	// Callback is unauthenticated (browser redirect from OAuth provider).
	s.echo.GET("/mcp-oauth/callback", s.handleMCPOAuthCallback)

	log.Println("[MCP-OAUTH] Routes registered: /mcp-oauth/connect, /mcp-oauth/callback, /mcp-oauth/status/:serverName, /mcp-oauth/disconnect/:serverName")
}

// handleMCPOAuthConnect begins the OAuth flow and returns the authorization URL.
func (s *Server) handleMCPOAuthConnect(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.ErrUnauthorized
	}

	var req MCPOAuthConnectRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.ServerName == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "server_name is required")
	}
	if req.MCPServerURL == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "mcp_server_url is required")
	}

	result, err := s.mcpOAuthManager.Connect(c.Request().Context(), mcpoauth.ConnectRequest{
		UserID:       string(user.ID()),
		ServerName:   req.ServerName,
		MCPServerURL: req.MCPServerURL,
		ClientID:     req.ClientID,
		ClientSecret: req.ClientSecret,
		Scopes:       req.Scopes,
	})
	if err != nil {
		log.Printf("[MCP-OAUTH] connect error for user=%s server=%s: %v", user.ID(), req.ServerName, err)
		return echo.NewHTTPError(http.StatusBadGateway, "OAuth discovery failed: "+err.Error())
	}

	return c.JSON(http.StatusOK, MCPOAuthConnectResponse{
		AuthorizationURL: result.AuthorizationURL,
		State:            result.State,
	})
}

// handleMCPOAuthCallback receives the authorization code redirect and exchanges it for tokens.
func (s *Server) handleMCPOAuthCallback(c echo.Context) error {
	code := c.QueryParam("code")
	state := c.QueryParam("state")
	errParam := c.QueryParam("error")

	if errParam != "" {
		desc := c.QueryParam("error_description")
		if desc == "" {
			desc = errParam
		}
		return c.HTML(http.StatusOK, renderCallbackHTML("", desc))
	}
	if code == "" || state == "" {
		return c.HTML(http.StatusBadRequest, renderCallbackHTML("", "missing code or state parameter"))
	}

	token, err := s.mcpOAuthManager.HandleCallback(c.Request().Context(), code, state)
	if err != nil {
		log.Printf("[MCP-OAUTH] callback error: %v", err)
		return c.HTML(http.StatusOK, renderCallbackHTML("", err.Error()))
	}

	return c.HTML(http.StatusOK, renderCallbackHTML(token.ServerName(), ""))
}

// handleMCPOAuthStatus returns the connection status for a specific MCP server.
func (s *Server) handleMCPOAuthStatus(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.ErrUnauthorized
	}
	serverName := c.Param("serverName")

	token, err := s.mcpOAuthTokenRepo.FindByUserAndServer(c.Request().Context(), string(user.ID()), serverName)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	resp := MCPOAuthStatusResponse{ServerName: serverName}
	if token != nil && !token.IsEmpty() {
		resp.Connected = true
		resp.HasRefreshToken = token.RefreshToken() != ""
		if !token.ExpiresAt().IsZero() {
			resp.ExpiresAt = token.ExpiresAt().UTC().Format("2006-01-02T15:04:05Z")
		}
	}
	return c.JSON(http.StatusOK, resp)
}

// handleMCPOAuthDisconnect removes the stored token for a specific MCP server.
func (s *Server) handleMCPOAuthDisconnect(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.ErrUnauthorized
	}
	serverName := c.Param("serverName")

	if err := s.mcpOAuthTokenRepo.Delete(c.Request().Context(), string(user.ID()), serverName); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.NoContent(http.StatusNoContent)
}

// ---- helpers ----

type callbackTemplateData struct {
	ServerName string
	Error      string
}

var callbackTmpl = template.Must(template.New("callback").Parse(callbackui.CallbackHTML))

func renderCallbackHTML(serverName, errMsg string) string {
	var buf bytes.Buffer
	_ = callbackTmpl.Execute(&buf, callbackTemplateData{
		ServerName: serverName,
		Error:      errMsg,
	})
	return buf.String()
}
