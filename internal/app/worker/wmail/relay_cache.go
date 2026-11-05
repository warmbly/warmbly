package wmail

import (
	"sync"
	"time"
)

// relayCache remembers the read state and folder last relayed for a known
// message, so a delta walk that re-reads a folder (its cursor held back by a
// deferred message) does not relay the same state for every message again.
// Entries expire so the stored state is still reconciled now and then.
// Memory-only and disposable: a restarted worker relays once more.
type relayCache struct {
	mu      sync.Mutex
	entries map[string]relayEntry
}

type relayState struct {
	seen   bool
	folder string
}

type relayEntry struct {
	state relayState
	at    time.Time
}

const (
	relayCacheTTL = 6 * time.Hour
	relayCacheMax = 20_000
)

// unchanged reports whether state is what was relayed for key within the TTL.
func (c *relayCache) unchanged(key string, state relayState) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	return ok && e.state == state && time.Since(e.at) <= relayCacheTTL
}

func (c *relayCache) put(key string, state relayState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]relayEntry{}
	}
	if len(c.entries) >= relayCacheMax {
		for k := range c.entries {
			delete(c.entries, k)
			if len(c.entries) < relayCacheMax/2 {
				break
			}
		}
	}
	c.entries[key] = relayEntry{state: state, at: time.Now()}
}

func (c *relayCache) forget(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}
