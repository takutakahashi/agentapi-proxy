package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
)

// contextKey is used for context keys to avoid collisions
type contextKey string

const echoContextKey contextKey = "echo"

// GitHubUserInfo represents GitHub user information
type GitHubUserInfo struct {
	Login         string                 `json:"login"`
	ID            int64                  `json:"id"`
	Email         string                 `json:"email"`
	Name          string                 `json:"name"`
	Organizations []GitHubOrganization   `json:"organizations"`
	Teams         []GitHubTeamMembership `json:"teams"`
}

// GitHubOrganization represents GitHub organization information
type GitHubOrganization struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

// GitHubTeamMembership represents GitHub team membership
type GitHubTeamMembership struct {
	Organization string `json:"organization"`
	TeamSlug     string `json:"team_slug"`
	TeamName     string `json:"team_name"`
	Role         string `json:"role"`
}

// GitHubAuthProvider handles GitHub OAuth authentication
type GitHubAuthProvider struct {
	config *config.GitHubAuthConfig
	client *http.Client
}

// NewGitHubAuthProvider creates a new GitHub authentication provider
func NewGitHubAuthProvider(cfg *config.GitHubAuthConfig) *GitHubAuthProvider {
	return &GitHubAuthProvider{
		config: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Authenticate authenticates a user using GitHub OAuth token
func (p *GitHubAuthProvider) Authenticate(ctx context.Context, token string) (*UserContext, error) {
	// Get user information from GitHub API
	user, err := p.getUser(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}

	// Get user's team memberships
	teams, err := p.getUserTeams(ctx, token, user.Login)
	if err != nil {
		log.Printf("Warning: Failed to get user teams for %s: %v", user.Login, err)
		teams = []GitHubTeamMembership{}
	}

	user.Teams = teams

	// Map user permissions based on team memberships
	role, permissions := p.mapUserPermissions(teams)

	return &UserContext{
		UserID:      user.Login,
		Role:        role,
		Permissions: permissions,
		AuthType:    "github_oauth",
		GitHubUser:  user,
	}, nil
}

// getUser retrieves user information from GitHub API
func (p *GitHubAuthProvider) getUser(ctx context.Context, token string) (*GitHubUserInfo, error) {
	url := fmt.Sprintf("%s/user", strings.TrimSuffix(p.config.BaseURL, "/"))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var user GitHubUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}

	return &user, nil
}

// getUserTeams retrieves user's team memberships from GitHub API
func (p *GitHubAuthProvider) getUserTeams(ctx context.Context, token, username string) ([]GitHubTeamMembership, error) {
	// First, get user's organizations
	orgs, err := p.getUserOrganizations(ctx, token)
	if err != nil {
		return nil, err
	}

	var allTeams []GitHubTeamMembership

	// For each organization, get user's team memberships
	for _, org := range orgs {
		teams, err := p.getUserTeamsInOrg(ctx, token, org.Login, username)
		if err != nil {
			log.Printf("Warning: Failed to get teams for org %s: %v", org.Login, err)
			continue
		}
		allTeams = append(allTeams, teams...)
	}

	return allTeams, nil
}

// getUserOrganizations retrieves user's organizations from GitHub API
func (p *GitHubAuthProvider) getUserOrganizations(ctx context.Context, token string) ([]GitHubOrganization, error) {
	url := fmt.Sprintf("%s/user/orgs", strings.TrimSuffix(p.config.BaseURL, "/"))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var orgs []GitHubOrganization
	if err := json.NewDecoder(resp.Body).Decode(&orgs); err != nil {
		return nil, err
	}

	return orgs, nil
}

// getUserTeamsInOrg retrieves user's team memberships in a specific organization
func (p *GitHubAuthProvider) getUserTeamsInOrg(ctx context.Context, token, org, username string) ([]GitHubTeamMembership, error) {
	url := fmt.Sprintf("%s/orgs/%s/teams", strings.TrimSuffix(p.config.BaseURL, "/"), org)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d for org %s", resp.StatusCode, org)
	}

	var teams []struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&teams); err != nil {
		return nil, err
	}

	var userTeams []GitHubTeamMembership
	for _, team := range teams {
		// Check if user is a member of this team
		if isMember, role := p.checkTeamMembership(ctx, token, org, team.Slug, username); isMember {
			userTeams = append(userTeams, GitHubTeamMembership{
				Organization: org,
				TeamSlug:     team.Slug,
				TeamName:     team.Name,
				Role:         role,
			})
		}
	}

	return userTeams, nil
}

// checkTeamMembership checks if user is a member of a specific team
func (p *GitHubAuthProvider) checkTeamMembership(ctx context.Context, token, org, teamSlug, username string) (bool, string) {
	url := fmt.Sprintf("%s/orgs/%s/teams/%s/memberships/%s",
		strings.TrimSuffix(p.config.BaseURL, "/"), org, teamSlug, username)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false, ""
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := p.client.Do(req)
	if err != nil {
		return false, ""
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return false, ""
	}

	if resp.StatusCode != http.StatusOK {
		return false, ""
	}

	var membership struct {
		State string `json:"state"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&membership); err != nil {
		return false, ""
	}

	return membership.State == "active", membership.Role
}

// mapUserPermissions maps user's team memberships to roles and permissions
func (p *GitHubAuthProvider) mapUserPermissions(teams []GitHubTeamMembership) (string, []string) {
	highestRole := p.config.UserMapping.DefaultRole
	allPermissions := make(map[string]bool)

	// Add default permissions
	for _, perm := range p.config.UserMapping.DefaultPermissions {
		allPermissions[perm] = true
	}

	// Check each team membership against configured rules
	for _, team := range teams {
		teamKey := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
		if rule, exists := p.config.UserMapping.TeamRoleMapping[teamKey]; exists {
			// Apply higher role if found
			if p.isHigherRole(rule.Role, highestRole) {
				highestRole = rule.Role
			}

			// Add permissions from this rule
			for _, perm := range rule.Permissions {
				allPermissions[perm] = true
			}
		}
	}

	// Convert permissions map to slice
	permissions := make([]string, 0, len(allPermissions))
	for perm := range allPermissions {
		permissions = append(permissions, perm)
	}

	return highestRole, permissions
}

// isHigherRole checks if role1 has higher priority than role2
func (p *GitHubAuthProvider) isHigherRole(role1, role2 string) bool {
	rolePriority := map[string]int{
		"guest":     0,
		"user":      1,
		"member":    2,
		"developer": 3,
		"admin":     4,
	}

	priority1, exists1 := rolePriority[role1]
	priority2, exists2 := rolePriority[role2]

	if !exists1 || !exists2 {
		return false
	}

	return priority1 > priority2
}

// ExtractTokenFromHeader extracts GitHub token from Authorization header
func ExtractTokenFromHeader(header string) string {
	if header == "" {
		return ""
	}

	// Handle "Bearer <token>" format
	if strings.HasPrefix(header, "Bearer ") {
		return strings.TrimPrefix(header, "Bearer ")
	}

	// Handle "token <token>" format
	if strings.HasPrefix(header, "token ") {
		return strings.TrimPrefix(header, "token ")
	}

	// Handle raw token
	return header
}
