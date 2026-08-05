package utils

import (
	"sync"
	"time"
)

// TTLCacheItem represents a cache item with expiration
type TTLCacheItem struct {
	Value     interface{}
	ExpiresAt time.Time
}

// IsExpired checks if the cache item has expired
func (item TTLCacheItem) IsExpired() bool {
	return time.Now().After(item.ExpiresAt)
}

// TTLCache is a thread-safe cache with TTL (Time To Live) support
type TTLCache struct {
	items map[string]TTLCacheItem
	mutex sync.RWMutex
	ttl   time.Duration
}

// NewTTLCache creates a new TTL cache with the specified default TTL
func NewTTLCache(ttl time.Duration) *TTLCache {
	return &TTLCache{
		items: make(map[string]TTLCacheItem),
		ttl:   ttl,
	}
}

// Set stores a value in the cache with the default TTL
func (c *TTLCache) Set(key string, value interface{}) {
	c.SetWithTTL(key, value, c.ttl)
}

// SetWithTTL stores a value in the cache with a custom TTL
func (c *TTLCache) SetWithTTL(key string, value interface{}, ttl time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.items[key] = TTLCacheItem{
		Value:     value,
		ExpiresAt: time.Now().Add(ttl),
	}
}

// Get retrieves a value from the cache
// Returns the value and true if found and not expired, nil and false otherwise
func (c *TTLCache) Get(key string) (interface{}, bool) {
	c.mutex.RLock()
	item, exists := c.items[key]
	c.mutex.RUnlock()

	if !exists || item.IsExpired() {
		// Clean up expired item
		if exists && item.IsExpired() {
			c.Delete(key)
		}
		return nil, false
	}

	return item.Value, true
}

// Delete removes a value from the cache
func (c *TTLCache) Delete(key string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	delete(c.items, key)
}

// Clear removes all items from the cache
func (c *TTLCache) Clear() {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	c.items = make(map[string]TTLCacheItem)
}

// Size returns the number of items in the cache (including expired items)
func (c *TTLCache) Size() int {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return len(c.items)
}

// CleanupExpired removes all expired items from the cache
func (c *TTLCache) CleanupExpired() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	count := 0
	for key, item := range c.items {
		if item.IsExpired() {
			delete(c.items, key)
			count++
		}
	}

	return count
}

// StartCleanupGoroutine starts a background goroutine that periodically cleans up expired items
func (c *TTLCache) StartCleanupGoroutine(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			c.CleanupExpired()
		}
	}()
}
