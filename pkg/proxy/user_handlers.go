package proxy

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// UserHandlers handles user-related endpoints
type UserHandlers struct {
	proxy *Proxy
}

// UserInfoResponse represents the response for /user/info endpoint
type UserInfoResponse struct {
	Username string   `json:"username"`
	Teams    []string `json:"teams"`
}

// NewUserHandlers creates a new UserHandlers instance
func NewUserHandlers(proxy *Proxy) *UserHandlers {
	return &UserHandlers{
		proxy: proxy,
	}
}

// GetUserInfo handles GET /user/info requests
func (h *UserHandlers) GetUserInfo(c echo.Context) error {
	user := auth.GetUserFromContext(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required")
	}

	response := UserInfoResponse{
		Teams: []string{},
	}

	if githubInfo := user.GitHubInfo(); githubInfo != nil {
		response.Username = githubInfo.Login()
		for _, team := range githubInfo.Teams() {
			teamSlug := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
			response.Teams = append(response.Teams, teamSlug)
		}
	}

	return c.JSON(http.StatusOK, response)
}
