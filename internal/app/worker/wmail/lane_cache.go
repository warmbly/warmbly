package wmail

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// laneCache remembers the lane a deferred message was classified into, so a
// message waiting on budget is not re-hydrated (Gmail, Graph) or re-checked
// against the control plane (IMAP) on every pass. Entries expire on their own
// and are dropped once the message is stored. Memory-only and disposable: a
// restarted worker just classifies once more.
type laneCache struct {
	mu      sync.Mutex
	entries map[string]laneEntry
}

type laneEntry struct {
	lane SyncLane
	at   time.Time
}

const (
	laneCacheTTL = 48 * time.Hour
	laneCacheMax = 20_000
)

func (c *laneCache) get(key string) (SyncLane, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return "", false
	}
	if time.Since(e.at) > laneCacheTTL {
		delete(c.entries, key)
		return "", false
	}
	return e.lane, true
}

func (c *laneCache) put(key string, lane SyncLane) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]laneEntry{}
	}
	if len(c.entries) >= laneCacheMax {
		// A flood is escalated long before this; the cap only guards memory
		// on a mailbox that is throttled for a long time.
		for k := range c.entries {
			delete(c.entries, k)
			if len(c.entries) < laneCacheMax/2 {
				break
			}
		}
	}
	c.entries[key] = laneEntry{lane: lane, at: time.Now()}
}

func (c *laneCache) forget(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

// newMessageID mints the internal id of a stored message.
func newMessageID() uuid.UUID { return uuid.New() }
