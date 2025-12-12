package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	"github.com/takutakahashi/agentapi-proxy/pkg/utils"
)

// hashToken creates a hash of the token for use as cache key
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

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

// UserCache represents cached user information, role and permissions
type UserCache struct {
	User        *GitHubUserInfo
	Role        string
	Permissions []string
	EnvFile     string
}

// GitHubAuthProvider handles GitHub OAuth authentication
type GitHubAuthProvider struct {
	config    *config.GitHubAuthConfig
	client    *http.Client
	userCache *utils.TTLCache
}

// NewGitHubAuthProvider creates a new GitHub authentication provider
func NewGitHubAuthProvider(cfg *config.GitHubAuthConfig) *GitHubAuthProvider {
	// Use very short cache TTL in tests to reduce race conditions
	cacheTTL := 1 * time.Hour
	if isTestEnvironment() {
		cacheTTL = 1 * time.Millisecond // Very short TTL for tests
	}

	return &GitHubAuthProvider{
		config:    cfg,
		client:    utils.NewDefaultHTTPClient(),
		userCache: utils.NewTTLCache(cacheTTL),
	}
}

// isTestEnvironment detects if running in test environment
func isTestEnvironment() bool {
	// Check for test environment indicators
	for _, arg := range []string{"-test.v", "-test.run", "-test.timeout"} {
		for _, osArg := range os.Args {
			if strings.Contains(osArg, arg) {
				return true
			}
		}
	}
	return false
}

// Authenticate authenticates a user using GitHub OAuth token
func (p *GitHubAuthProvider) Authenticate(ctx context.Context, token string) (*UserContext, error) {
	// Check if user information is cached (disabled in test environment)
	var cachedUser *UserCache
	if !isTestEnvironment() {
		cacheKey := fmt.Sprintf("user:%s", hashToken(token))
		if cached, found := p.userCache.Get(cacheKey); found {
			cachedUser = cached.(*UserCache)
			log.Printf("[AUTH_DEBUG] Using cached user info for %s: role=%s, permissions=%v, envFile=%s",
				cachedUser.User.Login, cachedUser.Role, cachedUser.Permissions, cachedUser.EnvFile)

			// Re-apply environment variables from cached env file if specified
			if cachedUser.EnvFile != "" {
				envVars, err := config.LoadTeamEnvVars(cachedUser.EnvFile)
				if err != nil {
					log.Printf("[AUTH_DEBUG] Warning: Failed to load cached env file %s: %v", cachedUser.EnvFile, err)
				} else {
					// Apply environment variables
					applied := config.ApplyEnvVars(envVars)
					log.Printf("[AUTH_DEBUG] Applied %d environment variables from cached %s", len(applied), cachedUser.EnvFile)
				}
			}

			return &UserContext{
				UserID:      cachedUser.User.Login,
				Role:        cachedUser.Role,
				Permissions: cachedUser.Permissions,
				AuthType:    "github_oauth",
				GitHubUser:  cachedUser.User,
				EnvFile:     cachedUser.EnvFile,
			}, nil
		}
	}

	// Get user information from GitHub API
	user, err := p.getUser(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("failed to get user info: %w", err)
	}

	// Get user's team memberships - optimized to check only configured teams
	teams, err := p.getUserTeamsOptimized(ctx, token, user.Login)
	if err != nil {
		log.Printf("Warning: Failed to get user teams for %s: %v", user.Login, err)
		teams = []GitHubTeamMembership{}
	}

	user.Teams = teams

	// Map user permissions based on team memberships
	role, permissions, envFile := p.mapUserPermissions(teams)

	// Load environment variables from team-specific env file if specified
	if envFile != "" {
		envVars, err := config.LoadTeamEnvVars(envFile)
		if err != nil {
			log.Printf("[AUTH_DEBUG] Warning: Failed to load env file %s: %v", envFile, err)
		} else {
			// Apply environment variables
			applied := config.ApplyEnvVars(envVars)
			log.Printf("[AUTH_DEBUG] Applied %d environment variables from %s", len(applied), envFile)
		}
	}

	// Cache the complete user information (disabled in test environment)
	if !isTestEnvironment() {
		cacheKey := fmt.Sprintf("user:%s", hashToken(token))
		p.userCache.Set(cacheKey, &UserCache{
			User:        user,
			Role:        role,
			Permissions: permissions,
			EnvFile:     envFile,
		})
		log.Printf("[AUTH_DEBUG] Cached user info for %s: role=%s, permissions=%v", user.Login, role, permissions)
	}

	return &UserContext{
		UserID:      user.Login,
		Role:        role,
		Permissions: permissions,
		AuthType:    "github_oauth",
		GitHubUser:  user,
		EnvFile:     envFile,
	}, nil
}

// getUser retrieves user information from GitHub API without caching
func (p *GitHubAuthProvider) getUser(ctx context.Context, token string) (*GitHubUserInfo, error) {
	url := fmt.Sprintf("%s/user", strings.TrimSuffix(p.config.BaseURL, "/"))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	logVerbose("Making GitHub API request: GET %s", url)
	resp, err := p.client.Do(req)
	if err != nil {
		logVerbose("GitHub API request failed: %v", err)
		return nil, err
	}
	defer utils.SafeCloseResponse(resp)

	logVerbose("GitHub API response: %d %s", resp.StatusCode, resp.Status)
	if err := utils.CheckHTTPResponse(resp, url); err != nil {
		return nil, err
	}

	var user GitHubUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}

	return &user, nil
}

// getUserTeamsOptimized retrieves user's teams from GitHub API and filters by configured team mappings
func (p *GitHubAuthProvider) getUserTeamsOptimized(ctx context.Context, token, username string) ([]GitHubTeamMembership, error) {
	// Extract configured team mappings
	configuredTeams := make(map[string]bool) // "org/team" -> true
	for teamKey := range p.config.UserMapping.TeamRoleMapping {
		configuredTeams[teamKey] = true
	}

	if len(configuredTeams) == 0 {
		log.Printf("[AUTH_DEBUG] No configured team mappings found, returning empty teams")
		return []GitHubTeamMembership{}, nil
	}

	log.Printf("[AUTH_DEBUG] Configured team mappings: %v", configuredTeams)

	// Get all teams the user belongs to via /user/teams API
	allUserTeams, err := p.getUserTeams(ctx, token)
	if err != nil {
		log.Printf("[AUTH_DEBUG] Failed to get user teams: %v", err)
		return []GitHubTeamMembership{}, err
	}

	log.Printf("[AUTH_DEBUG] User %s belongs to %d teams from API", username, len(allUserTeams))
	for _, t := range allUserTeams {
		log.Printf("[AUTH_DEBUG]   - %s/%s (role: %s)", t.Organization, t.TeamSlug, t.Role)
	}

	// Filter to only configured teams
	var matchingTeams []GitHubTeamMembership
	for _, team := range allUserTeams {
		teamKey := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
		if configuredTeams[teamKey] {
			log.Printf("[AUTH_DEBUG] Found matching configured team: %s", teamKey)
			matchingTeams = append(matchingTeams, team)
		}
	}

	log.Printf("[AUTH_DEBUG] Found %d matching teams for user %s", len(matchingTeams), username)
	return matchingTeams, nil
}

// getUserTeams retrieves all teams the user belongs to from GitHub API
func (p *GitHubAuthProvider) getUserTeams(ctx context.Context, token string) ([]GitHubTeamMembership, error) {
	url := fmt.Sprintf("%s/user/teams", strings.TrimSuffix(p.config.BaseURL, "/"))

	log.Printf("[AUTH_DEBUG] Fetching user teams from: %s", url)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := p.client.Do(req)
	if err != nil {
		log.Printf("[AUTH_DEBUG] Failed to fetch user teams: %v", err)
		return nil, err
	}
	defer utils.SafeCloseResponse(resp)

	log.Printf("[AUTH_DEBUG] /user/teams response status: %d", resp.StatusCode)

	if err := utils.CheckHTTPResponse(resp, url); err != nil {
		return nil, err
	}

	// GitHub /user/teams returns an array of team objects
	var teams []struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		Slug         string `json:"slug"`
		Permission   string `json:"permission"`
		Organization struct {
			Login string `json:"login"`
		} `json:"organization"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&teams); err != nil {
		log.Printf("[AUTH_DEBUG] Failed to decode teams response: %v", err)
		return nil, err
	}

	var result []GitHubTeamMembership
	for _, t := range teams {
		result = append(result, GitHubTeamMembership{
			Organization: t.Organization.Login,
			TeamSlug:     t.Slug,
			TeamName:     t.Name,
			Role:         t.Permission,
		})
	}

	return result, nil
}

// getUserOrganizations retrieves user's organizations from GitHub API without caching
func (p *GitHubAuthProvider) getUserOrganizations(ctx context.Context, token string) ([]GitHubOrganization, error) {
	url := fmt.Sprintf("%s/user/orgs", strings.TrimSuffix(p.config.BaseURL, "/"))

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	logVerbose("Making GitHub API request: GET %s", url)
	resp, err := p.client.Do(req)
	if err != nil {
		logVerbose("GitHub API request failed: %v", err)
		return nil, err
	}
	defer utils.SafeCloseResponse(resp)

	logVerbose("GitHub API response: %d %s", resp.StatusCode, resp.Status)
	if err := utils.CheckHTTPResponse(resp, url); err != nil {
		return nil, err
	}

	var orgs []GitHubOrganization
	if err := json.NewDecoder(resp.Body).Decode(&orgs); err != nil {
		return nil, err
	}

	return orgs, nil
}

// mapUserPermissions maps user's team memberships to roles and permissions
// Returns: role, permissions, envFile
func (p *GitHubAuthProvider) mapUserPermissions(teams []GitHubTeamMembership) (string, []string, string) {
	log.Printf("[AUTH_DEBUG] Starting mapUserPermissions")
	log.Printf("[AUTH_DEBUG] Default role: %s", p.config.UserMapping.DefaultRole)
	log.Printf("[AUTH_DEBUG] Default permissions: %v", p.config.UserMapping.DefaultPermissions)
	log.Printf("[AUTH_DEBUG] Team role mappings: %+v", p.config.UserMapping.TeamRoleMapping)
	log.Printf("[AUTH_DEBUG] User teams: %+v", teams)

	highestRole := p.config.UserMapping.DefaultRole
	allPermissions := make(map[string]bool)
	envFile := "" // Track the env file for the highest priority team

	// Add default permissions
	for _, perm := range p.config.UserMapping.DefaultPermissions {
		allPermissions[perm] = true
		log.Printf("[AUTH_DEBUG] Added default permission: %s", perm)
	}

	// Check each team membership against configured rules
	for _, team := range teams {
		teamKey := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)
		log.Printf("[AUTH_DEBUG] Checking team key: %s", teamKey)

		if rule, exists := p.config.UserMapping.TeamRoleMapping[teamKey]; exists {
			log.Printf("[AUTH_DEBUG] Found matching rule for %s: role=%s, permissions=%v", teamKey, rule.Role, rule.Permissions)

			// Apply higher role if found
			if p.isHigherRole(rule.Role, highestRole) {
				log.Printf("[AUTH_DEBUG] Upgrading role from %s to %s", highestRole, rule.Role)
				highestRole = rule.Role
				// Update env file to match the highest role's team
				if rule.EnvFile != "" {
					envFile = rule.EnvFile
					log.Printf("[AUTH_DEBUG] Setting env file to: %s", envFile)
				}
			}

			// Add permissions from this rule
			for _, perm := range rule.Permissions {
				allPermissions[perm] = true
				log.Printf("[AUTH_DEBUG] Added team permission: %s", perm)
			}
		} else {
			log.Printf("[AUTH_DEBUG] No rule found for team key: %s", teamKey)
			log.Printf("[AUTH_DEBUG] Available team mappings:")
			for availableKey := range p.config.UserMapping.TeamRoleMapping {
				log.Printf("[AUTH_DEBUG]   - %s", availableKey)
			}
		}
	}

	// Convert permissions map to slice
	permissions := make([]string, 0, len(allPermissions))
	for perm := range allPermissions {
		permissions = append(permissions, perm)
	}

	log.Printf("[AUTH_DEBUG] Final role: %s, permissions: %v, envFile: %s", highestRole, permissions, envFile)
	return highestRole, permissions, envFile
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
