package controllers

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// UserController handles user-related endpoints
type UserController struct{}

// UserInfoResponse represents the response for /user/info endpoint
type UserInfoResponse struct {
	Username string   `json:"username"`
	Teams    []string `json:"teams"`
}

// NewUserController creates a new UserController instance
func NewUserController() *UserController {
	return &UserController{}
}

// GetName returns the name of this controller for logging
func (c *UserController) GetName() string {
	return "UserController"
}

// GetUserInfo handles GET /user/info requests
func (c *UserController) GetUserInfo(ctx echo.Context) error {
	user := auth.GetUserFromContext(ctx)
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

	return ctx.JSON(http.StatusOK, response)
}
