package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

// UserController handles user-related endpoints
type UserController struct {
	githubBaseURL string
}

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
func NewUserController(githubBaseURL string) *UserController {
	if githubBaseURL == "" {
		githubBaseURL = "https://api.github.com"
	}
	return &UserController{githubBaseURL: githubBaseURL}
}

// GetName returns the name of this controller for logging
func (c *UserController) GetName() string {
	return "UserController"
}

// fetchAllGitHubTeams calls the GitHub /user/teams API and returns all teams as "org/slug" strings.
// It handles pagination automatically (up to 1000 teams / 10 pages).
func (c *UserController) fetchAllGitHubTeams(ctx context.Context, token string) ([]string, error) {
	var result []string
	perPage := 100
	baseURL := strings.TrimSuffix(c.githubBaseURL, "/")

	for page := 1; page <= 10; page++ {
		url := fmt.Sprintf("%s/user/teams?per_page=%d&page=%d", baseURL, perPage, page)
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Authorization", fmt.Sprintf("token %s", token))
		req.Header.Set("Accept", "application/vnd.github.v3+json")

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("GitHub API request failed: %w", err)
		}
		defer resp.Body.Close() //nolint:errcheck

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
		}

		var teams []struct {
			Slug         string `json:"slug"`
			Organization struct {
				Login string `json:"login"`
			} `json:"organization"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&teams); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		for _, t := range teams {
			result = append(result, fmt.Sprintf("%s/%s", t.Organization.Login, t.Slug))
		}

		if len(teams) < perPage {
			break
		}
	}

	return result, nil
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
			// Add teams already fetched during authentication (TeamRoleMapping-filtered)
			for _, team := range githubInfo.Teams() {
				teamSlug := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
				response.Teams = append(response.Teams, teamSlug)
			}
		}
		// Additionally call GitHub API to return ALL teams, regardless of TeamRoleMapping config.
		// This allows the UI to show all teams for scoping/filtering purposes.
		token := extractBearerToken(ctx.Request().Header.Get("Authorization"))
		if token != "" {
			allTeams, err := c.fetchAllGitHubTeams(ctx.Request().Context(), token)
			if err != nil {
				log.Printf("[USER_INFO] Warning: failed to fetch all GitHub teams: %v", err)
			} else {
				// Merge without duplicates
				teamSet := make(map[string]bool)
				for _, t := range response.Teams {
					teamSet[t] = true
				}
				for _, t := range allTeams {
					if !teamSet[t] {
						response.Teams = append(response.Teams, t)
						teamSet[t] = true
					}
				}
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

// extractBearerToken extracts the token value from an Authorization header.
// Supports "Bearer <token>" and "token <token>" formats.
func extractBearerToken(authHeader string) string {
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}
	if strings.HasPrefix(authHeader, "token ") {
		return strings.TrimPrefix(authHeader, "token ")
	}
	return ""
}
