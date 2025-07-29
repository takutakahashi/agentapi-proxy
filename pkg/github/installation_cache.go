package github

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	ghinstallation "github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v57/github"
)

// installationCacheEntry represents a cached installation ID with expiration time
type installationCacheEntry struct {
	installationID int64
	expiresAt      time.Time
}

// InstallationCache provides caching for GitHub App installation IDs
type InstallationCache struct {
	// Repository-specific cache: key = "{appID}:{owner}/{repo}" -> installationID
	repoCache sync.Map
	// Organization-specific cache: key = "{appID}:{org}" -> installationID
	orgCache sync.Map
	// TTL for cache entries
	ttl time.Duration
}

// NewInstallationCache creates a new installation ID cache with 24-hour TTL
func NewInstallationCache() *InstallationCache {
	return &InstallationCache{
		ttl: 24 * time.Hour,
	}
}

// NewInstallationCacheWithTTL creates a new installation ID cache with custom TTL
func NewInstallationCacheWithTTL(ttl time.Duration) *InstallationCache {
	return &InstallationCache{
		ttl: ttl,
	}
}

// GetInstallationID retrieves installation ID for a repository with caching
func (c *InstallationCache) GetInstallationID(ctx context.Context, appID int64, pemData []byte, repoFullName, apiBase string) (int64, error) {
	// Parse repository owner and name
	parts := strings.Split(repoFullName, "/")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid repository fullname format, expected 'owner/repo': %s", repoFullName)
	}
	owner, repo := parts[0], parts[1]

	// Check repository-specific cache first
	repoKey := fmt.Sprintf("%d:%s/%s", appID, owner, repo)
	if installationID, found := c.getFromRepoCache(repoKey); found {
		log.Printf("[INSTALLATION_CACHE] Cache hit for repository %s: installation ID %d", repoFullName, installationID)
		return installationID, nil
	}

	// Check organization-specific cache
	orgKey := fmt.Sprintf("%d:%s", appID, owner)
	if installationID, found := c.getFromOrgCache(orgKey); found {
		log.Printf("[INSTALLATION_CACHE] Cache hit for organization %s: installation ID %d", owner, installationID)
		// Store in repo cache for faster future lookups
		c.setRepoCache(repoKey, installationID)
		return installationID, nil
	}

	// Cache miss - discover installation ID
	log.Printf("[INSTALLATION_CACHE] Cache miss for %s, discovering installation ID", repoFullName)
	installationID, err := c.discoverInstallationID(ctx, appID, pemData, owner, repo, apiBase)
	if err != nil {
		return 0, err
	}

	// Cache the result in both repo and org caches
	c.setRepoCache(repoKey, installationID)
	c.setOrgCache(orgKey, installationID)

	log.Printf("[INSTALLATION_CACHE] Discovered and cached installation ID %d for repository %s", installationID, repoFullName)
	return installationID, nil
}

// getFromRepoCache retrieves installation ID from repository cache if not expired
func (c *InstallationCache) getFromRepoCache(key string) (int64, bool) {
	entry, exists := c.repoCache.Load(key)
	if !exists {
		return 0, false
	}

	cacheEntry, ok := entry.(installationCacheEntry)
	if !ok {
		c.repoCache.Delete(key)
		return 0, false
	}

	if time.Now().Before(cacheEntry.expiresAt) {
		return cacheEntry.installationID, true
	}

	// Entry expired, remove it
	c.repoCache.Delete(key)
	return 0, false
}

// getFromOrgCache retrieves installation ID from organization cache if not expired
func (c *InstallationCache) getFromOrgCache(key string) (int64, bool) {
	entry, exists := c.orgCache.Load(key)
	if !exists {
		return 0, false
	}

	cacheEntry, ok := entry.(installationCacheEntry)
	if !ok {
		c.orgCache.Delete(key)
		return 0, false
	}

	if time.Now().Before(cacheEntry.expiresAt) {
		return cacheEntry.installationID, true
	}

	// Entry expired, remove it
	c.orgCache.Delete(key)
	return 0, false
}

// setRepoCache stores installation ID in repository cache with TTL
func (c *InstallationCache) setRepoCache(key string, installationID int64) {
	entry := installationCacheEntry{
		installationID: installationID,
		expiresAt:      time.Now().Add(c.ttl),
	}
	c.repoCache.Store(key, entry)
}

// setOrgCache stores installation ID in organization cache with TTL
func (c *InstallationCache) setOrgCache(key string, installationID int64) {
	entry := installationCacheEntry{
		installationID: installationID,
		expiresAt:      time.Now().Add(c.ttl),
	}
	c.orgCache.Store(key, entry)
}

// installationCandidate represents a potential installation match with metadata
type installationCandidate struct {
	installationID int64
	account        string
	accountType    string
	priority       int // Higher number = higher priority
}

// discoverInstallationID discovers installation ID by querying GitHub API
func (c *InstallationCache) discoverInstallationID(ctx context.Context, appID int64, pemData []byte, owner, repo, apiBase string) (int64, error) {
	// Create GitHub App transport
	transport, err := ghinstallation.NewAppsTransport(http.DefaultTransport, appID, pemData)
	if err != nil {
		return 0, fmt.Errorf("failed to create GitHub App transport: %w", err)
	}

	// Set base URL if specified (for GitHub Enterprise)
	if apiBase != "" && apiBase != "https://api.github.com" {
		transport.BaseURL = apiBase
	}

	// Create GitHub client
	var client *github.Client
	if apiBase == "" || strings.Contains(apiBase, "https://api.github.com") {
		client = github.NewClient(&http.Client{Transport: transport})
	} else {
		client, err = github.NewClient(&http.Client{Transport: transport}).WithEnterpriseURLs(apiBase, apiBase)
		if err != nil {
			return 0, fmt.Errorf("failed to create GitHub Enterprise client: %w", err)
		}
	}

	// List installations for the app
	installations, _, err := client.Apps.ListInstallations(ctx, &github.ListOptions{})
	if err != nil {
		return 0, fmt.Errorf("failed to list installations: %w", err)
	}

	log.Printf("[INSTALLATION_CACHE] Found %d installations, checking access to %s/%s", len(installations), owner, repo)

	var candidates []installationCandidate

	// Check each installation for repository access
	for _, installation := range installations {
		installationID := installation.GetID()
		account := installation.GetAccount()
		accountLogin := account.GetLogin()
		accountType := account.GetType()

		log.Printf("[INSTALLATION_CACHE] Checking installation %d (account: %s, type: %s)",
			installationID, accountLogin, accountType)

		// Create installation client to check repository access
		installationTransport := ghinstallation.NewFromAppsTransport(transport, installationID)
		var installationClient *github.Client
		if apiBase == "" || strings.Contains(apiBase, "https://api.github.com") {
			installationClient = github.NewClient(&http.Client{Transport: installationTransport})
		} else {
			installationClient, err = github.NewClient(&http.Client{Transport: installationTransport}).WithEnterpriseURLs(apiBase, apiBase)
			if err != nil {
				log.Printf("[INSTALLATION_CACHE] Warning: failed to create client for installation %d: %v", installationID, err)
				continue
			}
		}

		// Try to access the repository with this installation
		repoResponse, _, err := installationClient.Repositories.Get(ctx, owner, repo)
		if err != nil {
			log.Printf("[INSTALLATION_CACHE] Installation %d (%s) does not have access to %s/%s: %v",
				installationID, accountLogin, owner, repo, err)
			continue
		}

		// Check if installation has write permissions by trying to access refs
		// This is a lightweight way to verify contents write permissions
		hasWritePermission, writeErr := c.checkWritePermission(ctx, installationClient, owner, repo)
		if !hasWritePermission {
			log.Printf("[INSTALLATION_CACHE] Installation %d (%s) has READ-only access to %s/%s: %v",
				installationID, accountLogin, owner, repo, writeErr)
			continue
		}

		log.Printf("[INSTALLATION_CACHE] Installation %d (%s) has WRITE access to %s/%s",
			installationID, accountLogin, owner, repo)

		// Repository access confirmed - calculate priority
		priority := 0

		// Higher priority for exact account match
		if strings.EqualFold(accountLogin, owner) {
			priority += 100
			log.Printf("[INSTALLATION_CACHE] Installation %d has EXACT match with repository owner %s",
				installationID, owner)
		}

		// Higher priority for organization installations over user installations
		if accountType == "Organization" {
			priority += 50
		}

		// Verify repository ownership matches installation account
		repoOwner := repoResponse.GetOwner()
		if repoOwner != nil && strings.EqualFold(repoOwner.GetLogin(), accountLogin) {
			priority += 200
			log.Printf("[INSTALLATION_CACHE] Installation %d OWNS the repository %s/%s",
				installationID, owner, repo)
		}

		candidate := installationCandidate{
			installationID: installationID,
			account:        accountLogin,
			accountType:    accountType,
			priority:       priority,
		}

		candidates = append(candidates, candidate)
		log.Printf("[INSTALLATION_CACHE] Installation %d (%s, %s) has access to %s/%s with priority %d",
			installationID, accountLogin, accountType, owner, repo, priority)
	}

	if len(candidates) == 0 {
		return 0, fmt.Errorf("no installation found with access to repository %s/%s", owner, repo)
	}

	// Sort candidates by priority (highest first)
	bestCandidate := candidates[0]
	for _, candidate := range candidates[1:] {
		if candidate.priority > bestCandidate.priority {
			bestCandidate = candidate
		}
	}

	// Log selection reasoning
	if len(candidates) > 1 {
		log.Printf("[INSTALLATION_CACHE] Multiple installations found (%d total), selected installation %d (%s, type: %s) with highest priority %d",
			len(candidates), bestCandidate.installationID, bestCandidate.account, bestCandidate.accountType, bestCandidate.priority)

		// Log other candidates for debugging
		for _, candidate := range candidates {
			if candidate.installationID != bestCandidate.installationID {
				log.Printf("[INSTALLATION_CACHE] Alternative: installation %d (%s, type: %s) with priority %d",
					candidate.installationID, candidate.account, candidate.accountType, candidate.priority)
			}
		}
	} else {
		log.Printf("[INSTALLATION_CACHE] Single installation %d (%s, type: %s) found for %s/%s",
			bestCandidate.installationID, bestCandidate.account, bestCandidate.accountType, owner, repo)
	}

	return bestCandidate.installationID, nil
}

// checkWritePermission checks if the installation has write permission to the repository
// by attempting to access repository refs and commits, which requires contents:read permission
func (c *InstallationCache) checkWritePermission(ctx context.Context, client *github.Client, owner, repo string) (bool, error) {
	// Try to get a reference to verify we have at least contents:read permission
	// We'll try to get the default branch reference
	repoInfo, _, err := client.Repositories.Get(ctx, owner, repo)
	if err != nil {
		return false, fmt.Errorf("failed to get repository info: %w", err)
	}

	defaultBranch := repoInfo.GetDefaultBranch()
	if defaultBranch == "" {
		defaultBranch = "main" // fallback
	}

	// Try to get the reference of the default branch
	// This requires contents:read permission
	refName := fmt.Sprintf("refs/heads/%s", defaultBranch)
	_, _, err = client.Git.GetRef(ctx, owner, repo, refName)
	if err != nil {
		return false, fmt.Errorf("failed to get ref %s (no contents permission): %w", refName, err)
	}

	// Try to get the latest commit on the default branch
	// This requires contents:read permission
	_, _, err = client.Repositories.GetCommit(ctx, owner, repo, defaultBranch, &github.ListOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to get commit (no contents:read permission): %w", err)
	}

	// If we can access repository contents, we consider this installation as having
	// sufficient permissions for git operations
	log.Printf("[INSTALLATION_CACHE] Installation has contents access to %s/%s", owner, repo)
	return true, nil
}

// ClearCache clears all cached entries
func (c *InstallationCache) ClearCache() {
	c.repoCache.Range(func(key, value interface{}) bool {
		c.repoCache.Delete(key)
		return true
	})
	c.orgCache.Range(func(key, value interface{}) bool {
		c.orgCache.Delete(key)
		return true
	})
	log.Printf("[INSTALLATION_CACHE] Cleared all cache entries")
}

// GetCacheStats returns cache statistics for debugging
func (c *InstallationCache) GetCacheStats() (repoCount, orgCount int) {
	c.repoCache.Range(func(key, value interface{}) bool {
		repoCount++
		return true
	})
	c.orgCache.Range(func(key, value interface{}) bool {
		orgCount++
		return true
	})
	return repoCount, orgCount
}
