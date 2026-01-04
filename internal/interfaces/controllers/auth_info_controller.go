package controllers

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// AuthTypesResponse represents the response for available auth types
type AuthTypesResponse struct {
	Enabled bool       `json:"enabled"`
	Types   []AuthType `json:"types"`
}

// AuthType represents a single authentication type
type AuthType struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
}

// AuthStatusResponse represents the response for auth status
type AuthStatusResponse struct {
	Authenticated bool                 `json:"authenticated"`
	AuthType      string               `json:"auth_type,omitempty"`
	UserID        string               `json:"user_id,omitempty"`
	Role          string               `json:"role,omitempty"`
	Permissions   []string             `json:"permissions,omitempty"`
	GitHubUser    *auth.GitHubUserInfo `json:"github_user,omitempty"`
}

// AuthInfoController handles auth info-related HTTP requests
type AuthInfoController struct {
	cfg *config.Config
}

// NewAuthInfoController creates a new AuthInfoController
func NewAuthInfoController(cfg *config.Config) *AuthInfoController {
	return &AuthInfoController{cfg: cfg}
}

// GetAuthTypes returns available authentication types
func (c *AuthInfoController) GetAuthTypes(ctx echo.Context) error {
	response := AuthTypesResponse{
		Enabled: c.cfg.Auth.Enabled,
		Types:   []AuthType{},
	}

	if !c.cfg.Auth.Enabled {
		return ctx.JSON(http.StatusOK, response)
	}

	if c.cfg.Auth.Static != nil && c.cfg.Auth.Static.Enabled {
		response.Types = append(response.Types, AuthType{
			Type:      "api_key",
			Name:      "API Key",
			Available: true,
		})
	}

	if c.cfg.Auth.GitHub != nil && c.cfg.Auth.GitHub.Enabled {
		githubAuth := AuthType{
			Type:      "github",
			Name:      "GitHub",
			Available: true,
		}

		if c.cfg.Auth.GitHub.OAuth != nil &&
			c.cfg.Auth.GitHub.OAuth.ClientID != "" &&
			c.cfg.Auth.GitHub.OAuth.ClientSecret != "" {
			githubAuth.Type = "github_oauth"
			githubAuth.Name = "GitHub OAuth"
		}

		response.Types = append(response.Types, githubAuth)
	}

	return ctx.JSON(http.StatusOK, response)
}

// GetAuthStatus returns current authentication status
func (c *AuthInfoController) GetAuthStatus(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)

	if user == nil {
		return ctx.JSON(http.StatusOK, AuthStatusResponse{
			Authenticated: false,
		})
	}

	// Convert roles and permissions to strings
	var role string
	if len(user.Roles()) > 0 {
		role = string(user.Roles()[0])
	} else {
		role = "user"
	}

	permissions := make([]string, len(user.Permissions()))
	for i, perm := range user.Permissions() {
		permissions[i] = string(perm)
	}

	response := AuthStatusResponse{
		Authenticated: true,
		AuthType:      string(user.UserType()),
		UserID:        string(user.ID()),
		Role:          role,
		Permissions:   permissions,
	}

	if user.UserType() == entities.UserTypeGitHub && user.GitHubInfo() != nil {
		githubInfo := user.GitHubInfo()
		response.GitHubUser = &auth.GitHubUserInfo{
			Login: githubInfo.Login(),
			ID:    githubInfo.ID(),
			Name:  githubInfo.Name(),
			Email: githubInfo.Email(),
		}
	}

	return ctx.JSON(http.StatusOK, response)
}
