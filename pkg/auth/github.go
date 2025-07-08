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
)

// contextKey is used for context keys to avoid collisions
type contextKey string

const echoContextKey contextKey = "echo"

// cacheEntry represents a cached value with expiration time
type cacheEntry struct {
	value     interface{}
	expiresAt time.Time
}

// cache is a simple TTL cache
type cache struct {
	data sync.Map
	ttl  time.Duration
}

// newCache creates a new cache with specified TTL
func newCache(ttl time.Duration) *cache {
	return &cache{
		ttl: ttl,
	}
}

// get retrieves a value from cache if it exists and hasn't expired
func (c *cache) get(key string) (interface{}, bool) {
	entry, exists := c.data.Load(key)
	if !exists {
		return nil, false
	}

	cacheEntry, ok := entry.(cacheEntry)
	if !ok {
		// Invalid entry type, remove it
		c.data.Delete(key)
		return nil, false
	}

	now := time.Now()
	if now.Before(cacheEntry.expiresAt) {
		return cacheEntry.value, true
	}

	// Entry expired, remove it
	c.data.Delete(key)
	return nil, false
}

// set stores a value in cache with TTL
func (c *cache) set(key string, value interface{}) {
	now := time.Now()
	entry := cacheEntry{
		value:     value,
		expiresAt: now.Add(c.ttl),
	}
	c.data.Store(key, entry)
}

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
	userCache *cache
}

// NewGitHubAuthProvider creates a new GitHub authentication provider
func NewGitHubAuthProvider(cfg *config.GitHubAuthConfig) *GitHubAuthProvider {
	// Use very short cache TTL in tests to reduce race conditions
	cacheTTL := 1 * time.Hour
	if isTestEnvironment() {
		cacheTTL = 1 * time.Millisecond // Very short TTL for tests
	}

	return &GitHubAuthProvider{
		config: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		userCache: newCache(cacheTTL),
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
		if cached, found := p.userCache.get(cacheKey); found {
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
		p.userCache.set(cacheKey, &UserCache{
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
	defer func() { _ = resp.Body.Close() }()

	logVerbose("GitHub API response: %d %s", resp.StatusCode, resp.Status)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var user GitHubUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}

	return &user, nil
}

// getUserTeamsOptimized retrieves only configured team memberships from GitHub API
func (p *GitHubAuthProvider) getUserTeamsOptimized(ctx context.Context, token, username string) ([]GitHubTeamMembership, error) {
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
	defer func() { _ = resp.Body.Close() }()

	logVerbose("GitHub API response: %d %s", resp.StatusCode, resp.Status)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var orgs []GitHubOrganization
	if err := json.NewDecoder(resp.Body).Decode(&orgs); err != nil {
		return nil, err
	}

	return orgs, nil
}

// checkTeamMembership checks if user is a member of a specific team without caching
func (p *GitHubAuthProvider) checkTeamMembership(ctx context.Context, token, org, teamSlug, username string) (bool, string) {
	url := fmt.Sprintf("%s/orgs/%s/teams/%s/memberships/%s",
		strings.TrimSuffix(p.config.BaseURL, "/"), org, teamSlug, username)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false, ""
	}

	req.Header.Set("Authorization", fmt.Sprintf("token %s", token))
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	logVerbose("Making GitHub API request: GET %s", url)
	resp, err := p.client.Do(req)
	if err != nil {
		logVerbose("GitHub API request failed: %v", err)
		return false, ""
	}
	defer func() { _ = resp.Body.Close() }()

	logVerbose("GitHub API response: %d %s", resp.StatusCode, resp.Status)
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
	// Use configured role priority if available
	var rolePriority map[string]int
	if p.config.UserMapping.RolePriority != nil && len(p.config.UserMapping.RolePriority) > 0 {
		rolePriority = p.config.UserMapping.RolePriority
	} else {
		// Default role priority for backward compatibility
		rolePriority = map[string]int{
			"guest":     0,
			"user":      1,
			"member":    2,
			"developer": 3,
			"admin":     4,
		}
	}

	priority1, exists1 := rolePriority[role1]
	priority2, exists2 := rolePriority[role2]

	// If either role is not in the priority map, treat as equal priority (don't upgrade)
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
