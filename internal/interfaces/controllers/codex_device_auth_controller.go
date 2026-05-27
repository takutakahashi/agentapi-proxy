package controllers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

// CodexDeviceAuthController handles Codex OAuth device authorization flow.
type CodexDeviceAuthController struct {
	cfg  *config.CodexAuthConfig
	repo repositories.CredentialsRepository
}

// NewCodexDeviceAuthController creates a new CodexDeviceAuthController.
func NewCodexDeviceAuthController(cfg *config.CodexAuthConfig, repo repositories.CredentialsRepository) *CodexDeviceAuthController {
	return &CodexDeviceAuthController{cfg: cfg, repo: repo}
}

// GetName returns the controller name for logging.
func (c *CodexDeviceAuthController) GetName() string {
	return "CodexDeviceAuthController"
}

// StartDeviceAuthResponse is returned when the device auth flow is initiated.
type StartDeviceAuthResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// PollDeviceAuthRequest is the request body for polling the device auth token.
type PollDeviceAuthRequest struct {
	DeviceCode string `json:"device_code"`
}

// PollDeviceAuthResponse is returned when polling for the device auth token.
type PollDeviceAuthResponse struct {
	// Status is one of "pending", "authorized", "expired", "denied".
	Status string `json:"status"`
}

// CodexAuthConfigResponse is returned by the GET /codex/device-auth/config endpoint.
type CodexAuthConfigResponse struct {
	Configured bool `json:"configured"`
}

// GetConfig handles GET /codex/device-auth/config
// Returns whether device auth is configured on this proxy instance.
func (c *CodexDeviceAuthController) GetConfig(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}
	configured := c.cfg != nil && c.cfg.DeviceAuthURL != "" && c.cfg.TokenURL != "" && c.cfg.ClientID != ""
	return ctx.JSON(http.StatusOK, CodexAuthConfigResponse{Configured: configured})
}

// StartDeviceAuth handles POST /codex/device-auth
// Initiates the OAuth 2.0 device authorization flow by calling the configured
// device authorization endpoint and returning the user code / verification URI.
func (c *CodexDeviceAuthController) StartDeviceAuth(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	if c.cfg == nil || c.cfg.DeviceAuthURL == "" || c.cfg.ClientID == "" {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Codex device auth not configured: set AGENTAPI_CODEX_AUTH_DEVICE_AUTH_URL, AGENTAPI_CODEX_AUTH_TOKEN_URL, and AGENTAPI_CODEX_AUTH_CLIENT_ID")
	}

	scope := c.cfg.Scope
	if scope == "" {
		scope = "openid email profile offline_access"
	}

	params := url.Values{}
	params.Set("client_id", c.cfg.ClientID)
	params.Set("scope", scope)

	resp, err := http.PostForm(c.cfg.DeviceAuthURL, params)
	if err != nil {
		log.Printf("[CODEX_AUTH] Device auth request failed: %v", err)
		return echo.NewHTTPError(http.StatusBadGateway, fmt.Sprintf("Device auth request failed: %v", err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "Failed to read device auth response")
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[CODEX_AUTH] Device auth endpoint returned %d: %s", resp.StatusCode, string(body))
		return echo.NewHTTPError(http.StatusBadGateway, fmt.Sprintf("Device auth endpoint returned %d", resp.StatusCode))
	}

	var result StartDeviceAuthResponse
	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[CODEX_AUTH] Failed to parse device auth response: %v", err)
		return echo.NewHTTPError(http.StatusBadGateway, "Failed to parse device auth response")
	}

	log.Printf("[CODEX_AUTH] Device auth started for user=%s, user_code=%s", user.ID(), result.UserCode)
	return ctx.JSON(http.StatusOK, result)
}

// PollDeviceAuth handles POST /codex/device-auth/token
// Polls the OAuth 2.0 token endpoint for the device auth result.
// On success, the token is persisted to the user's Codex credentials.
func (c *CodexDeviceAuthController) PollDeviceAuth(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	if c.cfg == nil || c.cfg.TokenURL == "" || c.cfg.ClientID == "" {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "Codex device auth not configured")
	}

	var req PollDeviceAuthRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}
	if req.DeviceCode == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "device_code is required")
	}

	params := url.Values{}
	params.Set("client_id", c.cfg.ClientID)
	params.Set("device_code", req.DeviceCode)
	params.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

	resp, err := http.PostForm(c.cfg.TokenURL, params)
	if err != nil {
		log.Printf("[CODEX_AUTH] Token poll request failed: %v", err)
		return echo.NewHTTPError(http.StatusBadGateway, fmt.Sprintf("Token request failed: %v", err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadGateway, "Failed to read token response")
	}

	var tokenResp map[string]interface{}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		log.Printf("[CODEX_AUTH] Failed to parse token response: %v", err)
		return echo.NewHTTPError(http.StatusBadGateway, "Failed to parse token response")
	}

	// Handle OAuth error responses
	if errCode, ok := tokenResp["error"].(string); ok {
		switch errCode {
		case "authorization_pending", "slow_down":
			return ctx.JSON(http.StatusOK, PollDeviceAuthResponse{Status: "pending"})
		case "expired_token":
			log.Printf("[CODEX_AUTH] Device code expired for user=%s", user.ID())
			return ctx.JSON(http.StatusOK, PollDeviceAuthResponse{Status: "expired"})
		case "access_denied", "authorization_declined":
			log.Printf("[CODEX_AUTH] Device auth denied for user=%s", user.ID())
			return ctx.JSON(http.StatusOK, PollDeviceAuthResponse{Status: "denied"})
		default:
			log.Printf("[CODEX_AUTH] Token endpoint error=%s for user=%s", errCode, user.ID())
			return ctx.JSON(http.StatusOK, PollDeviceAuthResponse{Status: "denied"})
		}
	}

	// Token received — convert expires_in to absolute expires_at and persist
	if expiresIn, ok := tokenResp["expires_in"].(float64); ok {
		tokenResp["expires_at"] = time.Now().Add(time.Duration(expiresIn) * time.Second).Unix()
		delete(tokenResp, "expires_in")
	}

	tokenJSON, err := json.Marshal(tokenResp)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to serialize token")
	}

	userName := string(user.ID())
	creds := entities.NewCredentials(userName, json.RawMessage(tokenJSON))
	creds.SetFileType(sessionsettings.FileTypeCodexAuth)

	if err := c.repo.Save(ctx.Request().Context(), creds); err != nil {
		log.Printf("[CODEX_AUTH] Failed to save credentials for user=%s: %v", user.ID(), err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save credentials")
	}

	log.Printf("[CODEX_AUTH] Device auth completed and credentials saved for user=%s", user.ID())
	return ctx.JSON(http.StatusOK, PollDeviceAuthResponse{Status: "authorized"})
}
