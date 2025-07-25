package utils

import (
	"testing"
	"time"
)

func TestNewTTLCache(t *testing.T) {
	ttl := 5 * time.Minute
	cache := NewTTLCache(ttl)

	if cache == nil {
		t.Fatal("NewTTLCache returned nil")
	}

	if cache.ttl != ttl {
		t.Errorf("Expected TTL %v, got %v", ttl, cache.ttl)
	}

	if cache.Size() != 0 {
		t.Errorf("Expected empty cache, got size %d", cache.Size())
	}
}

func TestTTLCache_SetAndGet(t *testing.T) {
	cache := NewTTLCache(1 * time.Hour)

	// Test setting and getting a value
	key := "test_key"
	value := "test_value"

	cache.Set(key, value)

	retrievedValue, found := cache.Get(key)
	if !found {
		t.Error("Expected to find the cached value")
	}

	if retrievedValue != value {
		t.Errorf("Expected value %q, got %q", value, retrievedValue)
	}
}

func TestTTLCache_SetWithTTL(t *testing.T) {
	cache := NewTTLCache(1 * time.Hour)

	// Test setting with custom TTL
	key := "ttl_key"
	value := "ttl_value"
	customTTL := 50 * time.Millisecond

	cache.SetWithTTL(key, value, customTTL)

	// Should be available immediately
	retrievedValue, found := cache.Get(key)
	if !found {
		t.Error("Expected to find the cached value immediately")
	}

	if retrievedValue != value {
		t.Errorf("Expected value %q, got %q", value, retrievedValue)
	}

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Should be expired now
	_, found = cache.Get(key)
	if found {
		t.Error("Expected cached value to be expired")
	}
}

func TestTTLCache_GetNonExistent(t *testing.T) {
	cache := NewTTLCache(1 * time.Hour)

	// Test getting non-existent key
	_, found := cache.Get("nonexistent")
	if found {
		t.Error("Expected not to find non-existent key")
	}
}

func TestTTLCache_Delete(t *testing.T) {
	cache := NewTTLCache(1 * time.Hour)

	key := "delete_key"
	value := "delete_value"

	// Set and verify
	cache.Set(key, value)
	_, found := cache.Get(key)
	if !found {
		t.Error("Expected to find the cached value before deletion")
	}

	// Delete and verify
	cache.Delete(key)
	_, found = cache.Get(key)
	if found {
		t.Error("Expected not to find the cached value after deletion")
	}
}

func TestTTLCache_Clear(t *testing.T) {
	cache := NewTTLCache(1 * time.Hour)

	// Add multiple items
	cache.Set("key1", "value1")
	cache.Set("key2", "value2")
	cache.Set("key3", "value3")

	if cache.Size() != 3 {
		t.Errorf("Expected cache size 3, got %d", cache.Size())
	}

	// Clear cache
	cache.Clear()

	if cache.Size() != 0 {
		t.Errorf("Expected cache size 0 after clear, got %d", cache.Size())
	}

	// Verify items are gone
	_, found := cache.Get("key1")
	if found {
		t.Error("Expected not to find key1 after clear")
	}
}

func TestTTLCache_Size(t *testing.T) {
	cache := NewTTLCache(1 * time.Hour)

	// Initially empty
	if cache.Size() != 0 {
		t.Errorf("Expected size 0, got %d", cache.Size())
	}

	// Add items
	cache.Set("key1", "value1")
	if cache.Size() != 1 {
		t.Errorf("Expected size 1, got %d", cache.Size())
	}

	cache.Set("key2", "value2")
	if cache.Size() != 2 {
		t.Errorf("Expected size 2, got %d", cache.Size())
	}

	// Overwrite existing key (should not increase size)
	cache.Set("key1", "new_value1")
	if cache.Size() != 2 {
		t.Errorf("Expected size 2 after overwrite, got %d", cache.Size())
	}
}

func TestTTLCache_CleanupExpired(t *testing.T) {
	cache := NewTTLCache(1 * time.Hour)

	// Add items with different TTLs
	cache.SetWithTTL("persistent", "value", 1*time.Hour)
	cache.SetWithTTL("short1", "value", 50*time.Millisecond)
	cache.SetWithTTL("short2", "value", 50*time.Millisecond)

	if cache.Size() != 3 {
		t.Errorf("Expected size 3, got %d", cache.Size())
	}

	// Wait for short TTL items to expire
	time.Sleep(100 * time.Millisecond)

	// Clean up expired items
	cleaned := cache.CleanupExpired()

	if cleaned != 2 {
		t.Errorf("Expected to clean up 2 items, got %d", cleaned)
	}

	if cache.Size() != 1 {
		t.Errorf("Expected size 1 after cleanup, got %d", cache.Size())
	}

	// Verify persistent item is still there
	_, found := cache.Get("persistent")
	if !found {
		t.Error("Expected persistent item to still be cached")
	}
}

func TestTTLCacheItem_IsExpired(t *testing.T) {
	// Test non-expired item
	futureTime := time.Now().Add(1 * time.Hour)
	item := TTLCacheItem{
		Value:     "test",
		ExpiresAt: futureTime,
	}

	if item.IsExpired() {
		t.Error("Expected item not to be expired")
	}

	// Test expired item
	pastTime := time.Now().Add(-1 * time.Hour)
	expiredItem := TTLCacheItem{
		Value:     "test",
		ExpiresAt: pastTime,
	}

	if !expiredItem.IsExpired() {
		t.Error("Expected item to be expired")
	}
}

func TestTTLCache_ConcurrentAccess(t *testing.T) {
	cache := NewTTLCache(1 * time.Hour)

	// Test basic concurrent safety (this is a simple test, not comprehensive)
	done := make(chan bool, 2)

	// Goroutine 1: Set values
	go func() {
		for i := 0; i < 100; i++ {
			cache.Set("key", i)
		}
		done <- true
	}()

	// Goroutine 2: Get values
	go func() {
		for i := 0; i < 100; i++ {
			cache.Get("key")
		}
		done <- true
	}()

	// Wait for both goroutines to complete
	<-done
	<-done

	// If we reach here without panicking, basic concurrent safety is working
}
