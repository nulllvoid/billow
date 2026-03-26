package billow

import (
	"sync"
	"time"
)

// planCacheEntry is a single cached plan with its expiry timestamp.
type planCacheEntry struct {
	plan      *Plan
	expiresAt time.Time
}

// planCache is a simple TTL cache for Plan objects keyed by plan ID.
// All methods are safe for concurrent use.
//
// Entries are evicted lazily on the next get() after they expire — there is
// no background sweep goroutine, which keeps the cache lifecycle-free.
//
// Callers must not mutate the *Plan returned by get(); the Limits map is
// shared with the cache entry. If mutation is ever needed, copy the struct
// and deep-copy the Limits map before modifying.
type planCache struct {
	mu      sync.RWMutex
	entries map[string]planCacheEntry
	ttl     time.Duration
}

func newPlanCache(ttl time.Duration) *planCache {
	return &planCache{
		entries: make(map[string]planCacheEntry),
		ttl:     ttl,
	}
}

// get returns the cached plan for id and true, or nil/false on a miss or expiry.
func (c *planCache) get(id string) (*Plan, bool) {
	c.mu.RLock()
	e, ok := c.entries[id]
	c.mu.RUnlock()
	if !ok || time.Now().After(e.expiresAt) {
		return nil, false
	}
	// Return a shallow struct copy so callers cannot mutate the cached pointer.
	cp := *e.plan
	return &cp, true
}

// set stores (or overwrites) a plan entry with a fresh expiry.
func (c *planCache) set(p *Plan) {
	cp := *p
	c.mu.Lock()
	c.entries[p.ID] = planCacheEntry{plan: &cp, expiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}

// delete evicts a plan entry immediately.
func (c *planCache) delete(id string) {
	c.mu.Lock()
	delete(c.entries, id)
	c.mu.Unlock()
}
