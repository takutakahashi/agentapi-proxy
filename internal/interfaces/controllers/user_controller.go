package controllers

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// UserController handles user-related endpoints
type UserController struct{}

// UserInfoResponse represents the response for /user/info endpoint
type UserInfoResponse struct {
	UserID      string   `json:"user_id"`
	Username    string   `json:"username"`
	UserType    string   `json:"user_type"`
	Teams       []string `json:"teams"`
	TeamID      string   `json:"team_id,omitempty"`
	IsAdmin     bool     `json:"is_admin"`
	Permissions []string `json:"permissions"`
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

	permissions := make([]string, len(user.Permissions()))
	for i, p := range user.Permissions() {
		permissions[i] = string(p)
	}

	response := UserInfoResponse{
		UserID:      string(user.ID()),
		Username:    user.Username(),
		UserType:    string(user.UserType()),
		Teams:       []string{},
		IsAdmin:     user.IsAdmin(),
		Permissions: permissions,
	}

	switch user.UserType() {
	case entities.UserTypeGitHub:
		if githubInfo := user.GitHubInfo(); githubInfo != nil {
			response.Username = githubInfo.Login()
			for _, team := range githubInfo.Teams() {
				teamSlug := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
				response.Teams = append(response.Teams, teamSlug)
			}
		}
	case entities.UserTypeServiceAccount:
		// Service accounts are tied to a specific team
		response.TeamID = user.TeamID()
		if user.TeamID() != "" {
			response.Teams = []string{user.TeamID()}
		}
	}

	return ctx.JSON(http.StatusOK, response)
}
