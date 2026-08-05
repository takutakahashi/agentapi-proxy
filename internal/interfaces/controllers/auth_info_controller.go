package controllers

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

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
