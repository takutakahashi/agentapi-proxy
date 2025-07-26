package proxy

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

type AuthTypesResponse struct {
	Enabled bool       `json:"enabled"`
	Types   []AuthType `json:"types"`
}

type AuthType struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
}

type AuthStatusResponse struct {
	Authenticated bool                 `json:"authenticated"`
	AuthType      string               `json:"auth_type,omitempty"`
	UserID        string               `json:"user_id,omitempty"`
	Role          string               `json:"role,omitempty"`
	Permissions   []string             `json:"permissions,omitempty"`
	GitHubUser    *auth.GitHubUserInfo `json:"github_user,omitempty"`
}

func NewAuthInfoHandlers(cfg *config.Config) *AuthInfoHandlers {
	return &AuthInfoHandlers{cfg: cfg}
}

type AuthInfoHandlers struct {
	cfg *config.Config
}

func (h *AuthInfoHandlers) GetAuthTypes(c echo.Context) error {
	response := AuthTypesResponse{
		Enabled: h.cfg.Auth.Enabled,
		Types:   []AuthType{},
	}

	if !h.cfg.Auth.Enabled {
		return c.JSON(http.StatusOK, response)
	}

	if h.cfg.Auth.Static != nil && h.cfg.Auth.Static.Enabled {
		response.Types = append(response.Types, AuthType{
			Type:      "api_key",
			Name:      "API Key",
			Available: true,
		})
	}

	if h.cfg.Auth.GitHub != nil && h.cfg.Auth.GitHub.Enabled {
		githubAuth := AuthType{
			Type:      "github",
			Name:      "GitHub",
			Available: true,
		}

		if h.cfg.Auth.GitHub.OAuth != nil &&
			h.cfg.Auth.GitHub.OAuth.ClientID != "" &&
			h.cfg.Auth.GitHub.OAuth.ClientSecret != "" {
			githubAuth.Type = "github_oauth"
			githubAuth.Name = "GitHub OAuth"
		}

		response.Types = append(response.Types, githubAuth)
	}

	return c.JSON(http.StatusOK, response)
}

func (h *AuthInfoHandlers) GetAuthStatus(c echo.Context) error {
	user := auth.GetInternalUserFromContext(c)

	if user == nil {
		return c.JSON(http.StatusOK, AuthStatusResponse{
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

	return c.JSON(http.StatusOK, response)
}
