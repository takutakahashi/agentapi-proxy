package proxy

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// SettingsHandlers handles settings-related HTTP requests
type SettingsHandlers struct {
	repo repositories.SettingsRepository
}

// NewSettingsHandlers creates new settings handlers
func NewSettingsHandlers(repo repositories.SettingsRepository) *SettingsHandlers {
	return &SettingsHandlers{
		repo: repo,
	}
}

// BedrockSettingsRequest is the request body for Bedrock settings
type BedrockSettingsRequest struct {
	Enabled         bool   `json:"enabled"`
	Region          string `json:"region"`
	Model           string `json:"model,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	RoleARN         string `json:"role_arn,omitempty"`
	Profile         string `json:"profile,omitempty"`
}

// UpdateSettingsRequest is the request body for updating settings
type UpdateSettingsRequest struct {
	Bedrock *BedrockSettingsRequest `json:"bedrock"`
}

// BedrockSettingsResponse is the response body for Bedrock settings
type BedrockSettingsResponse struct {
	Enabled         bool   `json:"enabled"`
	Region          string `json:"region"`
	Model           string `json:"model,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	RoleARN         string `json:"role_arn,omitempty"`
	Profile         string `json:"profile,omitempty"`
}

// SettingsResponse is the response body for settings
type SettingsResponse struct {
	Name      string                   `json:"name"`
	Bedrock   *BedrockSettingsResponse `json:"bedrock,omitempty"`
	CreatedAt string                   `json:"created_at"`
	UpdatedAt string                   `json:"updated_at"`
}

// GetSettings handles GET /settings/:name
func (h *SettingsHandlers) GetSettings(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	name := c.Param("name")
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Name is required")
	}

	// Check access permission
	if !h.canAccess(user, name) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	settings, err := h.repo.FindByName(c.Request().Context(), name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "Settings not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get settings")
	}

	return c.JSON(http.StatusOK, h.toResponse(settings))
}

// UpdateSettings handles PUT /settings/:name
func (h *SettingsHandlers) UpdateSettings(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	name := c.Param("name")
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Name is required")
	}

	// Check modify permission
	if !h.canModify(user, name) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	var req UpdateSettingsRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid request body")
	}

	// Get existing settings or create new one
	settings, err := h.repo.FindByName(c.Request().Context(), name)
	if err != nil {
		// Create new settings if not exists
		settings = entities.NewSettings(name)
	}

	// Update Bedrock settings
	if req.Bedrock != nil {
		bedrock := entities.NewBedrockSettings(req.Bedrock.Enabled, req.Bedrock.Region)
		bedrock.SetModel(req.Bedrock.Model)
		bedrock.SetAccessKeyID(req.Bedrock.AccessKeyID)
		bedrock.SetSecretAccessKey(req.Bedrock.SecretAccessKey)
		bedrock.SetRoleARN(req.Bedrock.RoleARN)
		bedrock.SetProfile(req.Bedrock.Profile)
		settings.SetBedrock(bedrock)
	}

	// Validate
	if err := settings.Validate(); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Save
	if err := h.repo.Save(c.Request().Context(), settings); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save settings")
	}

	return c.JSON(http.StatusOK, h.toResponse(settings))
}

// DeleteSettings handles DELETE /settings/:name
func (h *SettingsHandlers) DeleteSettings(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	name := c.Param("name")
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Name is required")
	}

	// Check modify permission
	if !h.canModify(user, name) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	err := h.repo.Delete(c.Request().Context(), name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "Settings not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete settings")
	}

	return c.JSON(http.StatusOK, map[string]bool{
		"success": true,
	})
}

// canAccess checks if the user can access settings for the given name
func (h *SettingsHandlers) canAccess(user *entities.User, name string) bool {
	// Admin can access all settings
	if user.IsAdmin() {
		return true
	}

	// Check if it's the user's own settings
	if h.sanitizeName(string(user.ID())) == name {
		return true
	}

	// Check if user belongs to the team
	if user.GitHubInfo() != nil {
		for _, team := range user.GitHubInfo().Teams() {
			teamName := team.Organization + "/" + team.TeamSlug
			if h.sanitizeName(teamName) == name {
				return true
			}
		}
	}

	return false
}

// canModify checks if the user can modify settings for the given name
func (h *SettingsHandlers) canModify(user *entities.User, name string) bool {
	// Admin can modify all settings
	if user.IsAdmin() {
		return true
	}

	// Check if it's the user's own settings
	if h.sanitizeName(string(user.ID())) == name {
		return true
	}

	// Check if user has developer/admin role in the team
	if user.GitHubInfo() != nil {
		for _, team := range user.GitHubInfo().Teams() {
			teamName := team.Organization + "/" + team.TeamSlug
			if h.sanitizeName(teamName) == name {
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
func (h *SettingsHandlers) sanitizeName(s string) string {
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

// toResponse converts Settings entity to response
func (h *SettingsHandlers) toResponse(settings *entities.Settings) *SettingsResponse {
	resp := &SettingsResponse{
		Name:      settings.Name(),
		CreatedAt: settings.CreatedAt().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: settings.UpdatedAt().Format("2006-01-02T15:04:05Z07:00"),
	}

	if bedrock := settings.Bedrock(); bedrock != nil {
		resp.Bedrock = &BedrockSettingsResponse{
			Enabled: bedrock.Enabled(),
			Region:  bedrock.Region(),
			Model:   bedrock.Model(),
			// AccessKeyID and SecretAccessKey are not returned for security reasons
			RoleARN: bedrock.RoleARN(),
			Profile: bedrock.Profile(),
		}
	}

	return resp
}
