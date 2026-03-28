package controllers

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/notification"
)

// BaseSettingsName is the reserved name for global base settings (admin-only)
const BaseSettingsName = "base"

// SettingsController handles settings-related HTTP requests
type SettingsController struct {
	repo            repositories.SettingsRepository
	notificationSvc *notification.Service // Optional
}

// NewSettingsController creates new settings controller
func NewSettingsController(repo repositories.SettingsRepository, notificationSvc *notification.Service) *SettingsController {
	return &SettingsController{
		repo:            repo,
		notificationSvc: notificationSvc,
	}
}

// GetName returns the name of this controller for logging
func (c *SettingsController) GetName() string {
	return "SettingsController"
}

// BedrockSettingsRequest is the request body for Bedrock settings
type BedrockSettingsRequest struct {
	Enabled         bool   `json:"enabled"`
	Model           string `json:"model,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	RoleARN         string `json:"role_arn,omitempty"`
	Profile         string `json:"profile,omitempty"`
}

// MCPServerRequest is the request body for a single MCP server
type MCPServerRequest struct {
	Type    string            `json:"type"`              // "stdio", "http", "sse"
	URL     string            `json:"url,omitempty"`     // for http/sse
	Command string            `json:"command,omitempty"` // for stdio
	Args    []string          `json:"args,omitempty"`    // for stdio
	Env     map[string]string `json:"env,omitempty"`     // environment variables
	Headers map[string]string `json:"headers,omitempty"` // for http/sse
}

// MarketplaceRequest is the request body for a single marketplace
type MarketplaceRequest struct {
	URL string `json:"url"`
}

// UpdateSettingsRequest is the request body for updating settings
type UpdateSettingsRequest struct {
	Bedrock                 *BedrockSettingsRequest          `json:"bedrock"`
	MCPServers              map[string]*MCPServerRequest     `json:"mcp_servers,omitempty"`
	Marketplaces            map[string]*MarketplaceRequest   `json:"marketplaces,omitempty"`
	ClaudeCodeOAuthToken    *string                          `json:"claude_code_oauth_token,omitempty"`
	AuthMode                *string                          `json:"auth_mode,omitempty"`                 // "oauth" or "bedrock"
	EnabledPlugins          []string                         `json:"enabled_plugins,omitempty"`           // plugin@marketplace format
	EnvVars                 map[string]string                `json:"env_vars,omitempty"`                  // Custom environment variables
	PreferredTeamID         *string                          `json:"preferred_team_id,omitempty"`         // "org/team-slug" format; "" to clear
	SlackUserID             *string                          `json:"slack_user_id,omitempty"`             // Slack DM notification user ID
	NotificationChannels    *[]string                        `json:"notification_channels,omitempty"`     // Active notification channels (e.g. ["web", "slack"])
	ExternalSessionManagers *[]ExternalSessionManagerRequest `json:"external_session_managers,omitempty"` // External session managers (Proxy B registrations)
}

// ExternalSessionManagerRequest represents a single external session manager registration
type ExternalSessionManagerRequest struct {
	ID         string `json:"id,omitempty"`          // Auto-generated if empty
	Name       string `json:"name"`                  // Human-readable name
	URL        string `json:"url"`                   // Proxy B URL
	HMACSecret string `json:"hmac_secret,omitempty"` // Auto-generated if empty; omit to keep existing
	Default    bool   `json:"default,omitempty"`     // Use as default manager when no manager_id is specified
}

// BedrockSettingsResponse is the response body for Bedrock settings
type BedrockSettingsResponse struct {
	Enabled         bool   `json:"enabled"`
	Model           string `json:"model,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	RoleARN         string `json:"role_arn,omitempty"`
	Profile         string `json:"profile,omitempty"`
}

// MCPServerResponse is the response body for a single MCP server
type MCPServerResponse struct {
	Type       string   `json:"type"`
	URL        string   `json:"url,omitempty"`
	Command    string   `json:"command,omitempty"`
	Args       []string `json:"args,omitempty"`
	EnvKeys    []string `json:"env_keys,omitempty"`    // only keys, not values
	HeaderKeys []string `json:"header_keys,omitempty"` // only keys, not values
}

// MarketplaceResponse is the response body for a single marketplace
type MarketplaceResponse struct {
	URL string `json:"url"`
}

// SettingsResponse is the response body for settings
type SettingsResponse struct {
	Name                    string                           `json:"name"`
	Bedrock                 *BedrockSettingsResponse         `json:"bedrock,omitempty"`
	MCPServers              map[string]*MCPServerResponse    `json:"mcp_servers,omitempty"`
	Marketplaces            map[string]*MarketplaceResponse  `json:"marketplaces,omitempty"`
	HasClaudeCodeOAuthToken bool                             `json:"has_claude_code_oauth_token"`
	AuthMode                string                           `json:"auth_mode,omitempty"`
	EnabledPlugins          []string                         `json:"enabled_plugins,omitempty"`           // plugin@marketplace format
	EnvVarKeys              []string                         `json:"env_var_keys,omitempty"`              // only keys, not values
	PreferredTeamID         string                           `json:"preferred_team_id,omitempty"`         // "org/team-slug" format
	SlackUserID             string                           `json:"slack_user_id,omitempty"`             // Slack DM notification user ID
	NotificationChannels    []string                         `json:"notification_channels,omitempty"`     // Active notification channels
	ExternalSessionManagers []ExternalSessionManagerResponse `json:"external_session_managers,omitempty"` // Registered external session managers
	CreatedAt               string                           `json:"created_at"`
	UpdatedAt               string                           `json:"updated_at"`
}

// ExternalSessionManagerResponse represents a single external session manager in responses
type ExternalSessionManagerResponse struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	HMACSecret string `json:"hmac_secret,omitempty"`
	Default    bool   `json:"default,omitempty"` // true if this manager is used when no manager_id is specified
}

// AvailableManagerEntry represents a single available ESM entry returned by GET /settings/managers
type AvailableManagerEntry struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	Default    bool   `json:"default,omitempty"` // true if this manager is used when no manager_id is specified
	Source     string `json:"source"`            // "user" or "team"
	SourceName string `json:"source_name"`       // user ID or team ID
}

// AvailableManagersResponse is the response body for GET /settings/managers
type AvailableManagersResponse struct {
	Managers []AvailableManagerEntry `json:"managers"`
}

// GetAvailableManagers handles GET /settings/managers
// Returns all external session managers available to the authenticated user:
// managers from their own settings plus managers from every team they belong to.
// HMAC secrets are NOT included in this response.
func (c *SettingsController) GetAvailableManagers(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	managers := make([]AvailableManagerEntry, 0)
	userID := string(user.ID())

	// Collect from user's own settings
	if userSettings, err := c.repo.FindByName(ctx.Request().Context(), userID); err == nil {
		for _, m := range userSettings.ExternalSessionManagers() {
			managers = append(managers, AvailableManagerEntry{
				ID:         m.ID,
				Name:       m.Name,
				URL:        m.URL,
				Default:    m.Default,
				Source:     "user",
				SourceName: userID,
			})
		}
	}

	// Collect from each team the user belongs to
	if user.GitHubInfo() != nil {
		for _, team := range user.GitHubInfo().Teams() {
			teamID := team.Organization + "/" + team.TeamSlug
			if teamSettings, err := c.repo.FindByName(ctx.Request().Context(), teamID); err == nil {
				for _, m := range teamSettings.ExternalSessionManagers() {
					managers = append(managers, AvailableManagerEntry{
						ID:         m.ID,
						Name:       m.Name,
						URL:        m.URL,
						Default:    m.Default,
						Source:     "team",
						SourceName: teamID,
					})
				}
			}
		}
	}

	return ctx.JSON(http.StatusOK, &AvailableManagersResponse{Managers: managers})
}

// GetSettings handles GET /settings/:name
func (c *SettingsController) GetSettings(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	name := ctx.Param("name")
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Name is required")
	}

	// Check access permission
	if !c.canAccess(user, name) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	settings, err := c.repo.FindByName(ctx.Request().Context(), name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "Settings not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get settings")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(settings))
}

// UpdateSettings handles PUT /settings/:name
func (c *SettingsController) UpdateSettings(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	name := ctx.Param("name")
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Name is required")
	}

	// Check modify permission
	if !c.canModify(user, name) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	var req UpdateSettingsRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Get existing settings or create new one
	settings, err := c.repo.FindByName(ctx.Request().Context(), name)
	isNewSettings := err != nil
	if isNewSettings {
		// Create new settings if not exists
		settings = entities.NewSettings(name)
	}

	// Update Bedrock settings
	if req.Bedrock != nil {
		bedrock := entities.NewBedrockSettings(req.Bedrock.Enabled)
		bedrock.SetModel(req.Bedrock.Model)

		// Preserve existing credentials if new values are empty
		existingBedrock := settings.Bedrock()

		accessKeyID := req.Bedrock.AccessKeyID
		if accessKeyID == "" && existingBedrock != nil {
			accessKeyID = existingBedrock.AccessKeyID()
		}
		bedrock.SetAccessKeyID(accessKeyID)

		secretAccessKey := req.Bedrock.SecretAccessKey
		if secretAccessKey == "" && existingBedrock != nil {
			secretAccessKey = existingBedrock.SecretAccessKey()
		}
		bedrock.SetSecretAccessKey(secretAccessKey)

		roleARN := req.Bedrock.RoleARN
		if roleARN == "" && existingBedrock != nil {
			roleARN = existingBedrock.RoleARN()
		}
		bedrock.SetRoleARN(roleARN)

		profile := req.Bedrock.Profile
		if profile == "" && existingBedrock != nil {
			profile = existingBedrock.Profile()
		}
		bedrock.SetProfile(profile)

		settings.SetBedrock(bedrock)
	}

	// Update MCP servers settings
	if req.MCPServers != nil {
		// Get existing MCP servers for preserving secrets
		existingMCPServers := settings.MCPServers()

		mcpServers := entities.NewMCPServersSettings()
		for serverName, serverReq := range req.MCPServers {
			server := entities.NewMCPServer(serverName, serverReq.Type)
			server.SetURL(serverReq.URL)
			server.SetCommand(serverReq.Command)
			server.SetArgs(serverReq.Args)

			// Handle env: preserve existing values if new values are empty
			env := serverReq.Env
			if existingMCPServers != nil {
				if existingServer := existingMCPServers.GetServer(serverName); existingServer != nil {
					env = c.mergeSecrets(existingServer.Env(), serverReq.Env)
				}
			}
			server.SetEnv(env)

			// Handle headers: preserve existing values if new values are empty
			headers := serverReq.Headers
			if existingMCPServers != nil {
				if existingServer := existingMCPServers.GetServer(serverName); existingServer != nil {
					headers = c.mergeSecrets(existingServer.Headers(), serverReq.Headers)
				}
			}
			server.SetHeaders(headers)

			mcpServers.SetServer(serverName, server)
		}
		settings.SetMCPServers(mcpServers)
	}

	// Update Marketplaces settings
	if req.Marketplaces != nil {
		marketplaces := entities.NewMarketplacesSettings()
		for marketplaceName, marketplaceReq := range req.Marketplaces {
			marketplace := entities.NewMarketplace(marketplaceName)
			marketplace.SetURL(marketplaceReq.URL)
			marketplaces.SetMarketplace(marketplaceName, marketplace)
		}
		settings.SetMarketplaces(marketplaces)
	}

	// Update enabled plugins
	if req.EnabledPlugins != nil {
		settings.SetEnabledPlugins(req.EnabledPlugins)
	}

	// Update environment variables
	if req.EnvVars != nil {
		// Get existing env vars for preserving empty values
		existingEnvVars := settings.EnvVars()

		// Use mergeSecrets helper to preserve existing values when new value is empty
		envVars := c.mergeSecrets(existingEnvVars, req.EnvVars)
		settings.SetEnvVars(envVars)
	}

	// Update Claude Code OAuth Token
	if req.ClaudeCodeOAuthToken != nil {
		settings.SetClaudeCodeOAuthToken(*req.ClaudeCodeOAuthToken)
	}

	// Update preferred team ID
	if req.PreferredTeamID != nil {
		// Explicitly specified (empty string "" clears the setting)
		settings.SetPreferredTeamID(*req.PreferredTeamID)
	} else if isNewSettings {
		// Auto-select on first creation: if user belongs to exactly one team, set it
		if user.GitHubInfo() != nil {
			teams := user.GitHubInfo().Teams()
			if len(teams) == 1 {
				teamID := teams[0].Organization + "/" + teams[0].TeamSlug
				settings.SetPreferredTeamID(teamID)
			}
		}
	}

	// Update Slack User ID
	if req.SlackUserID != nil {
		settings.SetSlackUserID(*req.SlackUserID)

		if c.notificationSvc != nil {
			channels := settings.NotificationChannels()
			slackEnabled := len(channels) == 0 || containsString(channels, "slack")
			if *req.SlackUserID == "" {
				_ = c.notificationSvc.DeleteSlackSubscription(string(user.ID()))
			} else if slackEnabled {
				if _, err := c.notificationSvc.SubscribeSlack(user, *req.SlackUserID); err != nil {
					log.Printf("[SETTINGS] Failed to create Slack subscription: %v", err)
				}
			}
		}
	}

	// Update notification channels — toggle Active on existing subscriptions instead of deleting them
	if req.NotificationChannels != nil {
		settings.SetNotificationChannels(*req.NotificationChannels)

		if c.notificationSvc != nil {
			userID := string(user.ID())
			channels := *req.NotificationChannels
			// len == 0 means all channels enabled (backward compat)
			slackEnabled := len(channels) == 0 || containsString(channels, "slack")
			webEnabled := len(channels) == 0 || containsString(channels, "web")

			if err := c.notificationSvc.SetSubscriptionTypeActive(userID, notification.SubscriptionTypeSlack, slackEnabled); err != nil {
				log.Printf("[SETTINGS] Failed to update Slack subscription active state: %v", err)
			}
			if err := c.notificationSvc.SetSubscriptionTypeActive(userID, notification.SubscriptionTypeWebPush, webEnabled); err != nil {
				log.Printf("[SETTINGS] Failed to update WebPush subscription active state: %v", err)
			}
		}
	}

	// Update external session managers
	// For each entry: auto-generate ID if empty, auto-generate HMAC secret if empty.
	// Existing secrets are preserved when the entry already exists (matched by ID).
	if req.ExternalSessionManagers != nil {
		existing := make(map[string]entities.ExternalSessionManagerEntry)
		for _, e := range settings.ExternalSessionManagers() {
			existing[e.ID] = e
		}

		// Validate: at most one entry may have Default=true
		defaultCount := 0
		for _, m := range *req.ExternalSessionManagers {
			if m.Default {
				defaultCount++
			}
		}
		if defaultCount > 1 {
			return echo.NewHTTPError(http.StatusBadRequest, "at most one external session manager may be marked as default")
		}

		updated := make([]entities.ExternalSessionManagerEntry, 0, len(*req.ExternalSessionManagers))
		for _, m := range *req.ExternalSessionManagers {
			if m.ID == "" {
				m.ID = uuid.New().String()
			}
			// Preserve existing secret if not provided
			if m.HMACSecret == "" {
				if prev, ok := existing[m.ID]; ok {
					m.HMACSecret = prev.HMACSecret
				}
			}
			// Auto-generate secret if still empty
			if m.HMACSecret == "" {
				secret, err := generateSettingsESMSecret(32)
				if err != nil {
					log.Printf("[SETTINGS] Failed to generate HMAC secret for ESM %s: %v", m.Name, err)
					return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate HMAC secret")
				}
				m.HMACSecret = secret
			}
			updated = append(updated, entities.ExternalSessionManagerEntry{
				ID:         m.ID,
				Name:       m.Name,
				URL:        m.URL,
				HMACSecret: m.HMACSecret,
				Default:    m.Default,
			})
		}
		settings.SetExternalSessionManagers(updated)
	}

	// Determine and set auth_mode
	authMode := c.determineAuthMode(settings, req.AuthMode)
	settings.SetAuthMode(authMode)

	// Validate
	if err := settings.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Save
	if err := c.repo.Save(ctx.Request().Context(), settings); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save settings")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(settings))
}

// DeleteSettings handles DELETE /settings/:name
func (c *SettingsController) DeleteSettings(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	name := ctx.Param("name")
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Name is required")
	}

	// Check modify permission
	if !c.canModify(user, name) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	err := c.repo.Delete(ctx.Request().Context(), name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "Settings not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete settings")
	}

	return ctx.JSON(http.StatusOK, map[string]bool{
		"success": true,
	})
}

// canAccess checks if the user can access settings for the given name
func (c *SettingsController) canAccess(user *entities.User, name string) bool {
	log.Printf("[SETTINGS_ACCESS] Checking access for user=%s, userType=%s, requestedName=%s", user.ID(), user.UserType(), name)

	// "base" is admin-only (global base settings)
	if name == BaseSettingsName {
		if user.IsAdmin() {
			log.Printf("[SETTINGS_ACCESS] GRANTED: user=%s is admin, accessing base settings", user.ID())
			return true
		}
		log.Printf("[SETTINGS_ACCESS] DENIED: user=%s is not admin, base settings are admin-only", user.ID())
		return false
	}

	// Admin can access all settings
	if user.IsAdmin() {
		log.Printf("[SETTINGS_ACCESS] GRANTED: user=%s is admin", user.ID())
		return true
	}

	// Sanitize the input name for consistent comparison
	sanitizedInputName := c.sanitizeName(name)
	sanitizedUserID := c.sanitizeName(string(user.ID()))

	log.Printf("[SETTINGS_ACCESS] Comparison: sanitizedInputName=%q, sanitizedUserID=%q", sanitizedInputName, sanitizedUserID)

	// Check if it's the user's own settings
	if sanitizedUserID == sanitizedInputName {
		log.Printf("[SETTINGS_ACCESS] GRANTED: user=%s owns settings (userID match)", user.ID())
		return true
	}

	// Check if user belongs to the team
	if user.GitHubInfo() != nil {
		teams := user.GitHubInfo().Teams()
		log.Printf("[SETTINGS_ACCESS] User has %d teams", len(teams))
		for _, team := range teams {
			teamName := team.Organization + "/" + team.TeamSlug
			sanitizedTeamName := c.sanitizeName(teamName)
			log.Printf("[SETTINGS_ACCESS] Checking team: original=%q, sanitized=%q, role=%s", teamName, sanitizedTeamName, team.Role)
			if sanitizedTeamName == sanitizedInputName {
				log.Printf("[SETTINGS_ACCESS] GRANTED: user=%s is member of team %s", user.ID(), teamName)
				return true
			}
		}
	} else {
		log.Printf("[SETTINGS_ACCESS] User has no GitHubInfo")
	}

	log.Printf("[SETTINGS_ACCESS] DENIED: user=%s has no access to settings %q", user.ID(), name)
	return false
}

// canModify checks if the user can modify settings for the given name
func (c *SettingsController) canModify(user *entities.User, name string) bool {
	log.Printf("[SETTINGS_MODIFY] Checking modify permission for user=%s, userType=%s, requestedName=%s", user.ID(), user.UserType(), name)

	// "base" is admin-only (global base settings)
	if name == BaseSettingsName {
		if user.IsAdmin() {
			log.Printf("[SETTINGS_MODIFY] GRANTED: user=%s is admin, modifying base settings", user.ID())
			return true
		}
		log.Printf("[SETTINGS_MODIFY] DENIED: user=%s is not admin, base settings are admin-only", user.ID())
		return false
	}

	// Admin can modify all settings
	if user.IsAdmin() {
		log.Printf("[SETTINGS_MODIFY] GRANTED: user=%s is admin", user.ID())
		return true
	}

	// Sanitize the input name for consistent comparison
	sanitizedInputName := c.sanitizeName(name)
	sanitizedUserID := c.sanitizeName(string(user.ID()))

	log.Printf("[SETTINGS_MODIFY] Comparison: sanitizedInputName=%q, sanitizedUserID=%q", sanitizedInputName, sanitizedUserID)

	// Check if it's the user's own settings
	if sanitizedUserID == sanitizedInputName {
		log.Printf("[SETTINGS_MODIFY] GRANTED: user=%s owns settings (userID match)", user.ID())
		return true
	}

	// Check if user belongs to the team (any team member can modify)
	if user.GitHubInfo() != nil {
		teams := user.GitHubInfo().Teams()
		log.Printf("[SETTINGS_MODIFY] User has %d teams", len(teams))
		for _, team := range teams {
			teamName := team.Organization + "/" + team.TeamSlug
			sanitizedTeamName := c.sanitizeName(teamName)
			log.Printf("[SETTINGS_MODIFY] Checking team: original=%q, sanitized=%q, role=%s", teamName, sanitizedTeamName, team.Role)
			if sanitizedTeamName == sanitizedInputName {
				log.Printf("[SETTINGS_MODIFY] GRANTED: user=%s is member of team %s", user.ID(), teamName)
				return true
			}
		}
	} else {
		log.Printf("[SETTINGS_MODIFY] User has no GitHubInfo")
	}

	log.Printf("[SETTINGS_MODIFY] DENIED: user=%s has no modify permission for settings %q", user.ID(), name)
	return false
}

// sanitizeName sanitizes a name for comparison
func (c *SettingsController) sanitizeName(s string) string {
	// Convert to lowercase
	sanitized := strings.ToLower(s)
	// Replace non-alphanumeric characters (except dash) with dash
	re := regexp.MustCompile(`[^a-z0-9-]`)
	sanitized = re.ReplaceAllString(sanitized, "-")
	// Remove leading/trailing dashes
	sanitized = strings.Trim(sanitized, "-")
	// Collapse multiple dashes
	re = regexp.MustCompile(`-+`)
	sanitized = re.ReplaceAllString(sanitized, "-")
	return sanitized
}

// determineAuthMode determines the auth mode based on request and available credentials
func (c *SettingsController) determineAuthMode(settings *entities.Settings, requestedMode *string) entities.AuthMode {
	// 1. If explicitly specified, use that mode (if credentials are available)
	if requestedMode != nil && *requestedMode != "" {
		mode := entities.AuthMode(*requestedMode)
		switch mode {
		case entities.AuthModeOAuth:
			if settings.HasClaudeCodeOAuthToken() {
				return entities.AuthModeOAuth
			}
		case entities.AuthModeBedrock:
			if bedrock := settings.Bedrock(); bedrock != nil && bedrock.Enabled() {
				return entities.AuthModeBedrock
			}
		}
		// Requested mode's credentials not available, fall through to auto-detection
	}

	// 2. Auto-detect: OAuth takes priority
	if settings.HasClaudeCodeOAuthToken() {
		return entities.AuthModeOAuth
	}
	if bedrock := settings.Bedrock(); bedrock != nil && bedrock.Enabled() {
		return entities.AuthModeBedrock
	}

	// 3. No credentials available
	return ""
}

// toResponse converts Settings entity to response
func (c *SettingsController) toResponse(settings *entities.Settings) *SettingsResponse {
	resp := &SettingsResponse{
		Name:                    settings.Name(),
		HasClaudeCodeOAuthToken: settings.HasClaudeCodeOAuthToken(),
		AuthMode:                string(settings.AuthMode()),
		CreatedAt:               settings.CreatedAt().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:               settings.UpdatedAt().Format("2006-01-02T15:04:05Z07:00"),
	}

	if bedrock := settings.Bedrock(); bedrock != nil {
		resp.Bedrock = &BedrockSettingsResponse{
			Enabled: bedrock.Enabled(),
			Model:   bedrock.Model(),
			// AccessKeyID and SecretAccessKey are not returned for security reasons
			RoleARN: bedrock.RoleARN(),
			Profile: bedrock.Profile(),
		}
	}

	if mcpServers := settings.MCPServers(); mcpServers != nil && !mcpServers.IsEmpty() {
		resp.MCPServers = make(map[string]*MCPServerResponse)
		for name, server := range mcpServers.Servers() {
			resp.MCPServers[name] = &MCPServerResponse{
				Type:       server.Type(),
				URL:        server.URL(),
				Command:    server.Command(),
				Args:       server.Args(),
				EnvKeys:    server.EnvKeys(),
				HeaderKeys: server.HeaderKeys(),
			}
		}
	}

	if marketplaces := settings.Marketplaces(); marketplaces != nil && !marketplaces.IsEmpty() {
		resp.Marketplaces = make(map[string]*MarketplaceResponse)
		for name, marketplace := range marketplaces.Marketplaces() {
			resp.Marketplaces[name] = &MarketplaceResponse{
				URL: marketplace.URL(),
			}
		}
	}

	if plugins := settings.EnabledPlugins(); len(plugins) > 0 {
		resp.EnabledPlugins = plugins
	}

	if envVarKeys := settings.EnvVarKeys(); len(envVarKeys) > 0 {
		resp.EnvVarKeys = envVarKeys
	}

	resp.PreferredTeamID = settings.PreferredTeamID()
	resp.SlackUserID = settings.SlackUserID()
	resp.NotificationChannels = settings.NotificationChannels()

	// External session managers: return full secret
	if managers := settings.ExternalSessionManagers(); len(managers) > 0 {
		resp.ExternalSessionManagers = make([]ExternalSessionManagerResponse, 0, len(managers))
		for _, m := range managers {
			resp.ExternalSessionManagers = append(resp.ExternalSessionManagers, ExternalSessionManagerResponse{
				ID:         m.ID,
				Name:       m.Name,
				URL:        m.URL,
				HMACSecret: m.HMACSecret,
				Default:    m.Default,
			})
		}
	}

	return resp
}

// mergeSecrets merges existing and new secret maps.
// Keys in new with non-empty values are added or updated.
// Keys in new with empty string values are ignored (existing values are preserved).
// Keys in existing that are not present in new are preserved.
func (c *SettingsController) mergeSecrets(existing, new map[string]string) map[string]string {
	if new == nil {
		return existing
	}
	if existing == nil {
		// Filter out empty strings
		result := make(map[string]string)
		for k, v := range new {
			if v != "" {
				result[k] = v
			}
		}
		return result
	}

	// Start with all existing keys preserved
	result := make(map[string]string)
	for k, v := range existing {
		result[k] = v
	}

	// Apply changes from new (empty strings are ignored)
	for k, v := range new {
		if v != "" {
			// Non-empty value adds or updates the key
			result[k] = v
		}
		// Empty string is ignored - existing values are preserved
	}
	return result
}

// generateSettingsESMSecret generates a random hex HMAC secret of the given byte length
func generateSettingsESMSecret(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
