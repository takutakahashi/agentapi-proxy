package controllers

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// SettingsController handles settings-related HTTP requests
type SettingsController struct {
	repo              repositories.SettingsRepository
	syncer            services.CredentialsSecretSyncer
	mcpSyncer         services.MCPSecretSyncer
	marketplaceSyncer services.MarketplaceSecretSyncer
}

// NewSettingsController creates new settings controller
func NewSettingsController(repo repositories.SettingsRepository) *SettingsController {
	return &SettingsController{
		repo: repo,
	}
}

// SetCredentialsSecretSyncer sets the credentials secret syncer
func (c *SettingsController) SetCredentialsSecretSyncer(syncer services.CredentialsSecretSyncer) {
	c.syncer = syncer
}

// SetMCPSecretSyncer sets the MCP secret syncer
func (c *SettingsController) SetMCPSecretSyncer(syncer services.MCPSecretSyncer) {
	c.mcpSyncer = syncer
}

// SetMarketplaceSecretSyncer sets the marketplace secret syncer
func (c *SettingsController) SetMarketplaceSecretSyncer(syncer services.MarketplaceSecretSyncer) {
	c.marketplaceSyncer = syncer
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
	Bedrock        *BedrockSettingsRequest        `json:"bedrock"`
	MCPServers     map[string]*MCPServerRequest   `json:"mcp_servers,omitempty"`
	Marketplaces   map[string]*MarketplaceRequest `json:"marketplaces,omitempty"`
	EnabledPlugins []string                       `json:"enabled_plugins,omitempty"` // plugin@marketplace format
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
	Name           string                          `json:"name"`
	Bedrock        *BedrockSettingsResponse        `json:"bedrock,omitempty"`
	MCPServers     map[string]*MCPServerResponse   `json:"mcp_servers,omitempty"`
	Marketplaces   map[string]*MarketplaceResponse `json:"marketplaces,omitempty"`
	EnabledPlugins []string                        `json:"enabled_plugins,omitempty"` // plugin@marketplace format
	CreatedAt      string                          `json:"created_at"`
	UpdatedAt      string                          `json:"updated_at"`
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

	// Validate team settings name format
	if err := c.validateTeamSettingsName(user, name); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var req UpdateSettingsRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Get existing settings or create new one
	settings, err := c.repo.FindByName(ctx.Request().Context(), name)
	if err != nil {
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

	// Validate
	if err := settings.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Save
	if err := c.repo.Save(ctx.Request().Context(), settings); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save settings")
	}

	// Sync credentials secret
	if c.syncer != nil {
		if err := c.syncer.Sync(ctx.Request().Context(), settings); err != nil {
			log.Printf("[SETTINGS] Failed to sync credentials secret for %s: %v", name, err)
			// Don't fail the request, just log the error
		}
	}

	// Sync MCP servers secret
	if c.mcpSyncer != nil {
		if err := c.mcpSyncer.Sync(ctx.Request().Context(), settings); err != nil {
			log.Printf("[SETTINGS] Failed to sync MCP servers secret for %s: %v", name, err)
			// Don't fail the request, just log the error
		}
	}

	// Sync marketplace secret
	if c.marketplaceSyncer != nil {
		if err := c.marketplaceSyncer.Sync(ctx.Request().Context(), settings); err != nil {
			log.Printf("[SETTINGS] Failed to sync marketplace secret for %s: %v", name, err)
			// Don't fail the request, just log the error
		}
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

	// Delete credentials secret
	if c.syncer != nil {
		if err := c.syncer.Delete(ctx.Request().Context(), name); err != nil {
			log.Printf("[SETTINGS] Failed to delete credentials secret for %s: %v", name, err)
			// Don't fail the request, just log the error
		}
	}

	// Delete MCP servers secret
	if c.mcpSyncer != nil {
		if err := c.mcpSyncer.Delete(ctx.Request().Context(), name); err != nil {
			log.Printf("[SETTINGS] Failed to delete MCP servers secret for %s: %v", name, err)
			// Don't fail the request, just log the error
		}
	}

	// Delete marketplace secret
	if c.marketplaceSyncer != nil {
		if err := c.marketplaceSyncer.Delete(ctx.Request().Context(), name); err != nil {
			log.Printf("[SETTINGS] Failed to delete marketplace secret for %s: %v", name, err)
			// Don't fail the request, just log the error
		}
	}

	return ctx.JSON(http.StatusOK, map[string]bool{
		"success": true,
	})
}

// canAccess checks if the user can access settings for the given name
func (c *SettingsController) canAccess(user *entities.User, name string) bool {
	// Admin can access all settings
	if user.IsAdmin() {
		return true
	}

	// Check if it's the user's own settings
	if c.sanitizeName(string(user.ID())) == name {
		return true
	}

	// Check if user belongs to the team
	if user.GitHubInfo() != nil {
		for _, team := range user.GitHubInfo().Teams() {
			teamName := team.Organization + "/" + team.TeamSlug
			if c.sanitizeName(teamName) == name {
				return true
			}
		}
	}

	return false
}

// canModify checks if the user can modify settings for the given name
func (c *SettingsController) canModify(user *entities.User, name string) bool {
	// Admin can modify all settings
	if user.IsAdmin() {
		return true
	}

	// Check if it's the user's own settings
	if c.sanitizeName(string(user.ID())) == name {
		return true
	}

	// Check if user has developer/admin role in the team
	if user.GitHubInfo() != nil {
		for _, team := range user.GitHubInfo().Teams() {
			teamName := team.Organization + "/" + team.TeamSlug
			if c.sanitizeName(teamName) == name {
				// Allow if user has admin or maintainer role in the team
				if team.Role == "admin" || team.Role == "maintainer" {
					return true
				}
			}
		}
	}

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

// validateTeamSettingsName validates that team settings use the correct {org}/{team-slug} format.
// If a user tries to create settings with just the team-slug (without org prefix),
// returns an error suggesting the correct format.
func (c *SettingsController) validateTeamSettingsName(user *entities.User, name string) error {
	// Skip validation for non-GitHub users or users without team membership
	if user == nil || user.GitHubInfo() == nil {
		return nil
	}

	teams := user.GitHubInfo().Teams()
	if len(teams) == 0 {
		return nil
	}

	// If name already contains a slash, it's likely in the correct format
	if strings.Contains(name, "/") {
		return nil
	}

	// Check if the name matches any team slug without the org prefix
	sanitizedName := c.sanitizeName(name)
	for _, team := range teams {
		sanitizedSlug := c.sanitizeName(team.TeamSlug)
		if sanitizedName == sanitizedSlug {
			// User is trying to use team-slug only format
			correctFormat := team.Organization + "/" + team.TeamSlug
			return fmt.Errorf("team settings should use format '{org}/{team-slug}'. Use '%s' instead of '%s'", correctFormat, name)
		}
	}

	return nil
}

// toResponse converts Settings entity to response
func (c *SettingsController) toResponse(settings *entities.Settings) *SettingsResponse {
	resp := &SettingsResponse{
		Name:      settings.Name(),
		CreatedAt: settings.CreatedAt().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: settings.UpdatedAt().Format("2006-01-02T15:04:05Z07:00"),
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

	return resp
}

// mergeSecrets merges existing and new secret maps
// If a key exists in new but has empty value, use existing value
func (c *SettingsController) mergeSecrets(existing, new map[string]string) map[string]string {
	if new == nil {
		return existing
	}
	if existing == nil {
		return new
	}

	result := make(map[string]string)
	for k, v := range new {
		if v == "" {
			// Preserve existing value if new value is empty
			if existingVal, ok := existing[k]; ok {
				result[k] = existingVal
			}
		} else {
			result[k] = v
		}
	}
	return result
}
