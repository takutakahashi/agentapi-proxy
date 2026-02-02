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
	"sync"
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
	Repositories  []GitHubRepository     `json:"repositories"`
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

// GitHubRepository represents GitHub repository information
type GitHubRepository struct {
	Name     string `json:"name"`
	FullName string `json:"full_name"`
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

	// Get user's repositories
	repos, err := p.getUserRepositories(ctx, token)
	if err != nil {
		log.Printf("Warning: Failed to get user repositories for %s: %v", user.Login, err)
		repos = []GitHubRepository{}
	}

	user.Repositories = repos

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

// getUserTeamsOptimized retrieves only configured team memberships from GitHub API
func (p *GitHubAuthProvider) getUserTeamsOptimized(ctx context.Context, token, username string) ([]GitHubTeamMembership, error) {
	// Check if we have wildcard patterns - if so, use /user/teams API
	if p.hasWildcardPatterns() {
		return p.getUserTeamsWithWildcard(ctx, token)
	}

	// No wildcard patterns - use the optimized approach (check only configured teams)
	return p.getUserTeamsExactMatch(ctx, token, username)
}

// getUserTeamsWithWildcard retrieves user teams using /user/teams API and filters by configured patterns
func (p *GitHubAuthProvider) getUserTeamsWithWildcard(ctx context.Context, token string) ([]GitHubTeamMembership, error) {
	// Get all teams user belongs to
	allTeams, err := p.getAllUserTeams(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("failed to get user teams: %w", err)
	}

	// Get all configured patterns
	patterns := make([]string, 0, len(p.config.UserMapping.TeamRoleMapping))
	for pattern := range p.config.UserMapping.TeamRoleMapping {
		patterns = append(patterns, pattern)
	}

	log.Printf("[AUTH_DEBUG] Configured patterns: %v", patterns)

	// Filter teams that match any configured pattern
	var matchedTeams []GitHubTeamMembership
	for _, team := range allTeams {
		teamKey := fmt.Sprintf("%s/%s", team.Organization, team.TeamSlug)

		// Check each pattern for a match
		for _, pattern := range patterns {
			if matchTeamPattern(pattern, team.Organization, team.TeamSlug) {
				matchedTeams = append(matchedTeams, team)
				log.Printf("[AUTH_DEBUG] Pattern match: %s matched %s", pattern, teamKey)
				break // Only add the team once even if multiple patterns match
			}
		}
	}

	log.Printf("[AUTH_DEBUG] Found %d matching teams via /user/teams", len(matchedTeams))
	return matchedTeams, nil
}

// getUserTeamsExactMatch retrieves only exact-match configured team memberships from GitHub API
func (p *GitHubAuthProvider) getUserTeamsExactMatch(ctx context.Context, token, username string) ([]GitHubTeamMembership, error) {
	// Extract unique organizations from configured team mappings
	configuredOrgs := make(map[string][]string) // org -> []teamSlugs
	for teamKey := range p.config.UserMapping.TeamRoleMapping {
		parts := strings.Split(teamKey, "/")
		if len(parts) == 2 {
			org, teamSlug := parts[0], parts[1]
			configuredOrgs[org] = append(configuredOrgs[org], teamSlug)
		}
	}

	if len(configuredOrgs) == 0 {
		log.Printf("[AUTH_DEBUG] No configured team mappings found, returning empty teams")
		return []GitHubTeamMembership{}, nil
	}

	log.Printf("[AUTH_DEBUG] Checking %d configured organizations: %v", len(configuredOrgs), configuredOrgs)

	// Use buffered channel to limit concurrent requests
	maxConcurrent := 3
	semaphore := make(chan struct{}, maxConcurrent)

	type teamResult struct {
		team GitHubTeamMembership
		err  error
	}

	var wg sync.WaitGroup
	resultChan := make(chan teamResult, 100) // Large buffer for potential teams

	// Check membership for each configured team concurrently
	for org, teamSlugs := range configuredOrgs {
		for _, teamSlug := range teamSlugs {
			wg.Add(1)
			go func(orgName, slug string) {
				defer wg.Done()

				// Acquire semaphore
				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				if isMember, role := p.checkTeamMembership(ctx, token, orgName, slug, username); isMember {
					// Get team name (can be optimized further with batch API if needed)
					teamName := slug // Default to slug if name retrieval fails

					resultChan <- teamResult{
						team: GitHubTeamMembership{
							Organization: orgName,
							TeamSlug:     slug,
							TeamName:     teamName,
							Role:         role,
						},
						err: nil,
					}
				}
			}(org, teamSlug)
		}
	}

	// Wait for all goroutines to complete
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	var userTeams []GitHubTeamMembership
	for result := range resultChan {
		if result.err == nil {
			userTeams = append(userTeams, result.team)
		}
	}

	log.Printf("[AUTH_DEBUG] Found %d matching teams for user %s", len(userTeams), username)
	return userTeams, nil
}

// checkTeamMembership checks if user is a member of a specific team without caching
func (p *GitHubAuthProvider) checkTeamMembership(ctx context.Context, token, org, teamSlug, username string) (bool, string) {
	url := fmt.Sprintf("%s/orgs/%s/teams/%s/memberships/%s",
		strings.TrimSuffix(p.config.BaseURL, "/"), org, teamSlug, username)

	log.Printf("[AUTH_DEBUG] Checking team membership: org=%s, team=%s, user=%s", org, teamSlug, username)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		log.Printf("[AUTH_DEBUG] Failed to create request: %v", err)
		return false, ""
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := p.client.Do(req)
	if err != nil {
		log.Printf("[AUTH_DEBUG] GitHub API request failed: %v", err)
		return false, ""
	}
	defer utils.SafeCloseResponse(resp)

	log.Printf("[AUTH_DEBUG] GitHub API response status: %d %s", resp.StatusCode, resp.Status)
	if resp.StatusCode == http.StatusNotFound {
		log.Printf("[AUTH_DEBUG] Team membership not found (404) for %s in %s/%s", username, org, teamSlug)
		return false, ""
	}

	if err := utils.CheckHTTPResponse(resp, url); err != nil {
		log.Printf("[AUTH_DEBUG] HTTP response check failed: %v", err)
		return false, ""
	}

	var membership struct {
		State string `json:"state"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&membership); err != nil {
		log.Printf("[AUTH_DEBUG] Failed to decode membership response: %v", err)
		return false, ""
	}

	log.Printf("[AUTH_DEBUG] Team membership response: state=%s, role=%s", membership.State, membership.Role)
	return membership.State == "active", membership.Role
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
		log.Printf("[AUTH_DEBUG] Checking team: %s", teamKey)

		matchFound := false

		// Check all configured patterns for matches
		for pattern, rule := range p.config.UserMapping.TeamRoleMapping {
			if matchTeamPattern(pattern, team.Organization, team.TeamSlug) {
				matchFound = true
				log.Printf("[AUTH_DEBUG] Found matching rule for %s (pattern: %s): role=%s, permissions=%v", teamKey, pattern, rule.Role, rule.Permissions)

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
			}
		}

		if !matchFound {
			log.Printf("[AUTH_DEBUG] No rule found for team: %s", teamKey)
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

// matchWildcard matches a string against a pattern with wildcard support
// Supported patterns:
//   - "exact" - exact match
//   - "*" - matches any string
//   - "prefix-*" - matches strings starting with "prefix-"
//   - "*-suffix" - matches strings ending with "-suffix"
func matchWildcard(pattern, str string) bool {
	// Exact match for "*"
	if pattern == "*" {
		return true
	}

	// No wildcard - exact match required
	if !strings.Contains(pattern, "*") {
		return pattern == str
	}

	// Prefix pattern: "prefix-*"
	if strings.HasSuffix(pattern, "-*") {
		prefix := strings.TrimSuffix(pattern, "-*")
		return strings.HasPrefix(str, prefix+"-")
	}

	// Suffix pattern: "*-suffix"
	if strings.HasPrefix(pattern, "*-") {
		suffix := strings.TrimPrefix(pattern, "*-")
		return strings.HasSuffix(str, "-"+suffix)
	}

	// If we reach here, the pattern has * in an unsupported position
	return false
}

// matchTeamPattern matches a team against a pattern
// Pattern format: "org/team" where both org and team can contain wildcards
// Examples:
//   - "myorg/myteam" - exact match
//   - "*/myteam" - any org, exact team name
//   - "myorg/*" - exact org, any team
//   - "myorg/*-engineer" - exact org, team ending with "-engineer"
//   - "myorg/backend-*" - exact org, team starting with "backend-"
func matchTeamPattern(pattern, org, teamSlug string) bool {
	parts := strings.Split(pattern, "/")
	if len(parts) != 2 {
		return false
	}

	orgPattern, teamPattern := parts[0], parts[1]

	// Check organization match
	if !matchWildcard(orgPattern, org) {
		return false
	}

	// Check team match
	return matchWildcard(teamPattern, teamSlug)
}

// hasWildcardPatterns checks if any team mappings contain wildcard patterns
func (p *GitHubAuthProvider) hasWildcardPatterns() bool {
	for teamKey := range p.config.UserMapping.TeamRoleMapping {
		if strings.Contains(teamKey, "*") {
			return true
		}
	}
	return false
}

// getAllUserTeams retrieves all teams the user belongs to using /user/teams API
func (p *GitHubAuthProvider) getAllUserTeams(ctx context.Context, token string) ([]GitHubTeamMembership, error) {
	var allTeams []GitHubTeamMembership
	page := 1
	perPage := 100 // GitHub API max

	for {
		url := fmt.Sprintf("%s/user/teams?per_page=%d&page=%d",
			strings.TrimSuffix(p.config.BaseURL, "/"), perPage, page)

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

		var teams []struct {
			ID           int64  `json:"id"`
			Slug         string `json:"slug"`
			Name         string `json:"name"`
			Permission   string `json:"permission"`
			Organization struct {
				Login string `json:"login"`
			} `json:"organization"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&teams); err != nil {
			return nil, err
		}

		if len(teams) == 0 {
			break
		}

		for _, team := range teams {
			allTeams = append(allTeams, GitHubTeamMembership{
				Organization: team.Organization.Login,
				TeamSlug:     team.Slug,
				TeamName:     team.Name,
				Role:         team.Permission,
			})
		}

		// Check if there are more pages
		if len(teams) < perPage {
			break
		}
		page++
	}

	log.Printf("[AUTH_DEBUG] Retrieved %d total teams for user via /user/teams", len(allTeams))
	return allTeams, nil
}

// getUserRepositories retrieves all repositories the user has access to
func (p *GitHubAuthProvider) getUserRepositories(ctx context.Context, token string) ([]GitHubRepository, error) {
	var allRepos []GitHubRepository
	page := 1
	perPage := 100 // GitHub API max

	for {
		url := fmt.Sprintf("%s/user/repos?per_page=%d&page=%d&affiliation=owner,collaborator,organization_member",
			strings.TrimSuffix(p.config.BaseURL, "/"), perPage, page)

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

		var repos []struct {
			Name     string `json:"name"`
			FullName string `json:"full_name"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
			return nil, err
		}

		if len(repos) == 0 {
			break
		}

		for _, repo := range repos {
			allRepos = append(allRepos, GitHubRepository{
				Name:     repo.Name,
				FullName: repo.FullName,
			})
		}

		// Check if there are more pages
		if len(repos) < perPage {
			break
		}
		page++
	}

	log.Printf("[AUTH_DEBUG] Retrieved %d total repositories for user via /user/repos", len(allRepos))
	return allRepos, nil
}
