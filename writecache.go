package mikrotik

import (
	"sync"
	"time"
)

// writeCache is a TTL-based idempotency guard that prevents redundant
// RouterOS address-list writes. Key = deviceAddr|list|target (masked CIDR).
// Thread-safe. Expired entries are lazily cleaned up on Has miss.
type writeCache struct {
	mu      sync.Mutex
	entries map[string]time.Time
	ttl     time.Duration
}

func newWriteCache(ttl time.Duration) *writeCache {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &writeCache{
		entries: make(map[string]time.Time),
		ttl:     ttl,
	}
}

// Has returns true if key is in the cache and not yet expired.
// Expired entries are deleted on miss (lazy cleanup).
func (wc *writeCache) Has(key string) bool {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	deadline, ok := wc.entries[key]
	if !ok {
		return false
	}
	if time.Now().Before(deadline) {
		return true
	}
	delete(wc.entries, key) // lazy cleanup
	return false
}

// Set stores key with the configured TTL from now.
func (wc *writeCache) Set(key string) {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	wc.entries[key] = time.Now().Add(wc.ttl)
}

// cacheKey builds a cache key from device address, list, and target.
func cacheKey(deviceAddr, list, target string) string {
	return deviceAddr + "|" + list + "|" + target
}
