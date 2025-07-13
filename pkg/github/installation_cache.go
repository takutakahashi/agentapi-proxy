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

	// Check each installation for repository access
	for _, installation := range installations {
		installationID := installation.GetID()

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
		_, _, err := installationClient.Repositories.Get(ctx, owner, repo)
		if err == nil {
			log.Printf("[INSTALLATION_CACHE] Installation %d has access to %s/%s", installationID, owner, repo)
			return installationID, nil
		}
		log.Printf("[INSTALLATION_CACHE] Installation %d does not have access to %s/%s: %v", installationID, owner, repo, err)
	}

	return 0, fmt.Errorf("no installation found with access to repository %s/%s", owner, repo)
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
