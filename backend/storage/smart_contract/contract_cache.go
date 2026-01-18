package smart_contract

import (
	"stargate-backend/core/smart_contract"
	"sync"
	"time"
)

// ContractCache provides in-memory caching for contract responses
type ContractCache struct {
	mu      sync.RWMutex
	cache   map[string]*ContractCacheEntry
	ttl     time.Duration
	maxSize int
}

// ContractCacheEntry represents a cached contract response
type ContractCacheEntry struct {
	Contracts []smart_contract.Contract
	CachedAt  time.Time
}

// NewContractCache creates a new contract cache with specified TTL and max size
func NewContractCache(ttl time.Duration, maxSize int) *ContractCache {
	cache := &ContractCache{
		cache:   make(map[string]*ContractCacheEntry),
		ttl:     ttl,
		maxSize: maxSize,
	}

	// Start cleanup goroutine
	go cache.startCleanup()

	return cache
}

// Get retrieves cached contracts if valid
func (c *ContractCache) Get(key string) ([]smart_contract.Contract, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.cache[key]
	if !exists {
		return nil, false
	}

	// Check TTL
	if time.Since(entry.CachedAt) > c.ttl {
		delete(c.cache, key)
		return nil, false
	}

	return entry.Contracts, true
}

// Set stores contracts in cache
func (c *ContractCache) Set(key string, contracts []smart_contract.Contract) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry := &ContractCacheEntry{
		Contracts: contracts,
		CachedAt:  time.Now(),
	}

	c.cache[key] = entry

	// Trigger cleanup if needed
	if len(c.cache) > c.maxSize {
		c.evictOldest()
	}
}

// Invalidate removes a specific cache entry
func (c *ContractCache) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.cache, key)
}

// InvalidateByContract removes cache entries containing specific contract
func (c *ContractCache) InvalidateByContract(contractID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Invalidate all entries that might contain this contract
	for key := range c.cache {
		if c.containsContract(key, contractID) {
			delete(c.cache, key)
		}
	}
}

// InvalidateAll clears all cache entries
func (c *ContractCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[string]*ContractCacheEntry)
}

// startCleanup runs periodic cleanup of expired entries
func (c *ContractCache) startCleanup() {
	ticker := time.NewTicker(c.ttl / 2) // Clean at half TTL interval
	defer ticker.Stop()

	for range ticker.C {
		c.cleanupExpired()
	}
}

// cleanupExpired removes expired entries
func (c *ContractCache) cleanupExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, entry := range c.cache {
		if now.Sub(entry.CachedAt) > c.ttl {
			delete(c.cache, key)
		}
	}
}

// evictOldest removes oldest entries when max size is exceeded
func (c *ContractCache) evictOldest() {
	if len(c.cache) <= c.maxSize {
		return
	}

	var oldestKey string
	var oldestTime time.Time = time.Now()

	for key, entry := range c.cache {
		if entry.CachedAt.Before(oldestTime) {
			oldestTime = entry.CachedAt
			oldestKey = key
		}
	}

	if oldestKey != "" {
		delete(c.cache, oldestKey)
	}
}

// containsContract checks if a cache key might contain a specific contract
func (c *ContractCache) containsContract(cacheKey, contractID string) bool {
	entry, exists := c.cache[cacheKey]
	if !exists {
		return false
	}

	for _, contract := range entry.Contracts {
		if contract.ContractID == contractID {
			return true
		}
	}

	return false
}
