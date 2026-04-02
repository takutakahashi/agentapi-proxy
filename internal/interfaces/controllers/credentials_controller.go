package controllers

import (
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// CredentialsController handles credentials-related HTTP requests
type CredentialsController struct {
	repo repositories.CredentialsRepository
}

// NewCredentialsController creates a new CredentialsController
func NewCredentialsController(repo repositories.CredentialsRepository) *CredentialsController {
	return &CredentialsController{
		repo: repo,
	}
}

// GetName returns the name of this controller for logging
func (c *CredentialsController) GetName() string {
	return "CredentialsController"
}

// CredentialsResponse is the response body for credentials (data is never returned)
type CredentialsResponse struct {
	Name      string `json:"name"`
	HasData   bool   `json:"has_data"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ListCredentialsResponse is the response for listing credentials
type ListCredentialsResponse struct {
	Credentials []CredentialsResponse `json:"credentials"`
}

// GetCredentials handles GET /credentials/:name
// Returns metadata only — the raw credential data is never exposed via the API
func (c *CredentialsController) GetCredentials(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	name := ctx.Param("name")
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Name is required")
	}

	if !c.canAccess(user, name) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	creds, err := c.repo.FindByName(ctx.Request().Context(), name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "Credentials not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get credentials")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(creds))
}

// UploadCredentials handles PUT /credentials/:name
// Accepts raw JSON body as credential data (e.g., auth.json contents)
func (c *CredentialsController) UploadCredentials(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	name := ctx.Param("name")
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Name is required")
	}

	if !c.canModify(user, name) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	// Read raw body as JSON
	var rawData json.RawMessage
	if err := ctx.Bind(&rawData); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid JSON body")
	}
	if len(rawData) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "Request body must not be empty")
	}

	// Validate that the body is valid JSON
	var tmp interface{}
	if err := json.Unmarshal(rawData, &tmp); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Request body must be valid JSON")
	}

	// Get existing credentials or create new
	creds, err := c.repo.FindByName(ctx.Request().Context(), name)
	if err != nil {
		// Create new
		creds = entities.NewCredentials(name, rawData)
	} else {
		// Update existing
		creds.SetData(rawData)
	}

	if err := c.repo.Save(ctx.Request().Context(), creds); err != nil {
		log.Printf("[CREDENTIALS] Failed to save credentials %s: %v", name, err)
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to save credentials")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(creds))
}

// DeleteCredentials handles DELETE /credentials/:name
func (c *CredentialsController) DeleteCredentials(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	name := ctx.Param("name")
	if name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "Name is required")
	}

	if !c.canModify(user, name) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied")
	}

	err := c.repo.Delete(ctx.Request().Context(), name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return echo.NewHTTPError(http.StatusNotFound, "Credentials not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "Failed to delete credentials")
	}

	return ctx.JSON(http.StatusOK, map[string]bool{
		"success": true,
	})
}

// canAccess checks if the user can access credentials for the given name
func (c *CredentialsController) canAccess(user *entities.User, name string) bool {
	log.Printf("[CREDENTIALS_ACCESS] Checking access for user=%s, requestedName=%s", user.ID(), name)

	// Admin can access everything
	if user.IsAdmin() {
		log.Printf("[CREDENTIALS_ACCESS] GRANTED: user=%s is admin", user.ID())
		return true
	}

	sanitizedInputName := c.sanitizeName(name)
	sanitizedUserID := c.sanitizeName(string(user.ID()))

	// Check if it's the user's own credentials
	if sanitizedUserID == sanitizedInputName {
		log.Printf("[CREDENTIALS_ACCESS] GRANTED: user=%s owns credentials (userID match)", user.ID())
		return true
	}

	// Check if user belongs to the team
	if user.GitHubInfo() != nil {
		for _, team := range user.GitHubInfo().Teams() {
			teamName := team.Organization + "/" + team.TeamSlug
			sanitizedTeamName := c.sanitizeName(teamName)
			if sanitizedTeamName == sanitizedInputName {
				log.Printf("[CREDENTIALS_ACCESS] GRANTED: user=%s is member of team %s", user.ID(), teamName)
				return true
			}
		}
	}

	log.Printf("[CREDENTIALS_ACCESS] DENIED: user=%s has no access to credentials %q", user.ID(), name)
	return false
}

// canModify checks if the user can modify credentials for the given name
func (c *CredentialsController) canModify(user *entities.User, name string) bool {
	log.Printf("[CREDENTIALS_MODIFY] Checking modify permission for user=%s, requestedName=%s", user.ID(), name)

	// Admin can modify everything
	if user.IsAdmin() {
		log.Printf("[CREDENTIALS_MODIFY] GRANTED: user=%s is admin", user.ID())
		return true
	}

	sanitizedInputName := c.sanitizeName(name)
	sanitizedUserID := c.sanitizeName(string(user.ID()))

	// Check if it's the user's own credentials
	if sanitizedUserID == sanitizedInputName {
		log.Printf("[CREDENTIALS_MODIFY] GRANTED: user=%s owns credentials (userID match)", user.ID())
		return true
	}

	// Check if user belongs to the team
	if user.GitHubInfo() != nil {
		for _, team := range user.GitHubInfo().Teams() {
			teamName := team.Organization + "/" + team.TeamSlug
			sanitizedTeamName := c.sanitizeName(teamName)
			if sanitizedTeamName == sanitizedInputName {
				log.Printf("[CREDENTIALS_MODIFY] GRANTED: user=%s is member of team %s", user.ID(), teamName)
				return true
			}
		}
	}

	log.Printf("[CREDENTIALS_MODIFY] DENIED: user=%s has no modify permission for credentials %q", user.ID(), name)
	return false
}

// sanitizeName sanitizes a name for comparison
func (c *CredentialsController) sanitizeName(s string) string {
	sanitized := strings.ToLower(s)
	re := regexp.MustCompile(`[^a-z0-9-]`)
	sanitized = re.ReplaceAllString(sanitized, "-")
	sanitized = strings.Trim(sanitized, "-")
	re = regexp.MustCompile(`-+`)
	sanitized = re.ReplaceAllString(sanitized, "-")
	return sanitized
}

// toResponse converts Credentials entity to response (never exposes raw data)
func (c *CredentialsController) toResponse(creds *entities.Credentials) *CredentialsResponse {
	return &CredentialsResponse{
		Name:      creds.Name(),
		HasData:   len(creds.Data()) > 0,
		CreatedAt: creds.CreatedAt().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: creds.UpdatedAt().Format("2006-01-02T15:04:05Z07:00"),
	}
}
