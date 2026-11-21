package wmail

import (
	"context"
	"hash/maphash"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/repository"
)

// cachedMessageMap remembers one mailbox's message map answers across passes,
// found and not found, so a delta or history walk that re-reads the same pages
// (its cursor held behind a deferred message) does not ask the control plane
// about the same ids every pass. Only Gmail and Graph use it: nothing outside
// the worker writes their map rows, and every write here goes through Add,
// Del or AdmitArrival, which drop the key. IMAP rows are also purged by the
// control plane with their folder, so IMAP is not wrapped.
type cachedMessageMap struct {
	inner repository.EmailMessageMapRepository

	mu      sync.Mutex
	entries map[mapCacheKey]mapAnswer
	// gen moves on every write, so an answer read before it is not kept.
	gen  uint64
	seed [2]maphash.Seed
}

// mapCacheKey is a 128-bit digest of the map key: Graph ids are ~150 bytes and
// a held mailbox holds thousands of answers.
type mapCacheKey [2]uint64

type mapAnswer struct {
	known    bool
	id       string
	threadID string
	at       time.Time
}

const (
	mapCacheTTL = 2 * time.Hour
	mapCacheMax = 5_000
)

var (
	_ repository.EmailMessageMapRepository      = (*cachedMessageMap)(nil)
	_ repository.EmailMessageMapBatchRepository = (*cachedMessageMap)(nil)
	_ repository.ArrivalAdmission               = (*cachedMessageMap)(nil)
)

func newCachedMessageMap(inner repository.EmailMessageMapRepository) *cachedMessageMap {
	return &cachedMessageMap{inner: inner, seed: [2]maphash.Seed{maphash.MakeSeed(), maphash.MakeSeed()}}
}

func (c *cachedMessageMap) key(messageID string) mapCacheKey {
	return mapCacheKey{maphash.String(c.seed[0], messageID), maphash.String(c.seed[1], messageID)}
}

// lookupLocked returns a fresh answer; mu is held.
func (c *cachedMessageMap) lookupLocked(k mapCacheKey, now time.Time) (mapAnswer, bool) {
	a, ok := c.entries[k]
	if !ok {
		return a, false
	}
	if now.Sub(a.at) > mapCacheTTL {
		delete(c.entries, k)
		return a, false
	}
	return a, true
}

// keep records answers read at generation gen, unless a write landed since.
func (c *cachedMessageMap) keep(gen uint64, ids []string, found map[string]*repository.EmailMessageData) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen != gen {
		return
	}
	if c.entries == nil {
		c.entries = map[mapCacheKey]mapAnswer{}
	}
	now := time.Now()
	if len(c.entries)+len(ids) > mapCacheMax {
		for k, a := range c.entries {
			if now.Sub(a.at) > mapCacheTTL || len(c.entries) >= mapCacheMax/2 {
				delete(c.entries, k)
			}
		}
	}
	for _, id := range ids {
		k := c.key(id)
		if _, held := c.entries[k]; !held && len(c.entries) >= mapCacheMax {
			continue
		}
		a := mapAnswer{at: now}
		if d := found[id]; d != nil {
			a.known, a.id, a.threadID = true, d.ID, d.ThreadID
		}
		c.entries[k] = a
	}
}

func (c *cachedMessageMap) forget(messageID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	delete(c.entries, c.key(messageID))
}

func (c *cachedMessageMap) Get(ctx context.Context, userID, emailID uuid.UUID, messageID string) (*repository.EmailMessageData, error) {
	c.mu.Lock()
	a, ok := c.lookupLocked(c.key(messageID), time.Now())
	gen := c.gen
	c.mu.Unlock()
	if ok {
		return a.data(userID, emailID, messageID), nil
	}
	d, err := c.inner.Get(ctx, userID, emailID, messageID)
	if err != nil {
		return nil, err
	}
	c.keep(gen, []string{messageID}, map[string]*repository.EmailMessageData{messageID: d})
	return d, nil
}

// GetMany answers what it remembers and asks the control plane for the rest in one call.
func (c *cachedMessageMap) GetMany(ctx context.Context, userID, emailID uuid.UUID, messageIDs []string) (map[string]*repository.EmailMessageData, error) {
	result := make(map[string]*repository.EmailMessageData, len(messageIDs))
	var missing []string
	now := time.Now()
	c.mu.Lock()
	for _, id := range messageIDs {
		if a, ok := c.lookupLocked(c.key(id), now); ok {
			if a.known {
				result[id] = a.data(userID, emailID, id)
			}
			continue
		}
		missing = append(missing, id)
	}
	gen := c.gen
	c.mu.Unlock()
	if len(missing) == 0 {
		return result, nil
	}
	bulk, ok := c.inner.(repository.EmailMessageMapBatchRepository)
	if !ok {
		return nil, repository.ErrMessageMapBatchUnsupported
	}
	found, err := bulk.GetMany(ctx, userID, emailID, missing)
	if err != nil {
		return nil, err
	}
	c.keep(gen, missing, found)
	for id, d := range found {
		result[id] = d
	}
	return result, nil
}

func (c *cachedMessageMap) Add(ctx context.Context, data repository.EmailMessageData) error {
	defer c.forget(data.MessageID)
	c.forget(data.MessageID)
	return c.inner.Add(ctx, data)
}

func (c *cachedMessageMap) Del(ctx context.Context, userID, emailID uuid.UUID, messageID string, id uuid.UUID) error {
	defer c.forget(messageID)
	c.forget(messageID)
	return c.inner.Del(ctx, userID, emailID, messageID, id)
}

func (c *cachedMessageMap) AdmitArrival(ctx context.Context, data repository.EmailMessageData, pending *repository.PendingArrival) error {
	durable, ok := c.inner.(repository.ArrivalAdmission)
	if !ok {
		return repository.ErrArrivalOutboxUnsupported
	}
	defer c.forget(data.MessageID)
	c.forget(data.MessageID)
	return durable.AdmitArrival(ctx, data, pending)
}

func (a mapAnswer) data(userID, emailID uuid.UUID, messageID string) *repository.EmailMessageData {
	if !a.known {
		return nil
	}
	return &repository.EmailMessageData{UserID: userID.String(), EmailID: emailID.String(), MessageID: messageID, ID: a.id, ThreadID: a.threadID}
}
