// Package instancesettings holds the handful of settings that live in the
// database rather than the environment.
//
// Every key here is one no environment variable owns, by construction, so
// there is no precedence to resolve and no locked-by-environment field
// anywhere in the UI. Reads are cached for 30 seconds with no cross-process
// invalidation: time is the only invalidation channel, so a write is visible
// on every process within that window.
package instancesettings

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// cacheTTL is the whole invalidation strategy. Do not add a bus for it: the
// eventbus is at-least-once and the worker deliberately has no SQL.
const cacheTTL = 30 * time.Second

// Service is the read and write surface for the settings document.
type Service interface {
	// Get never fails: a read error falls back to the last known document,
	// then to the defaults, because the invitation path must not break when a
	// settings row is missing.
	Get(ctx context.Context) Document
	Put(ctx context.Context, patch Patch, updatedBy *uuid.UUID) (Document, error)

	InvitationTTL(ctx context.Context) time.Duration
	InviteLinksEnabled(ctx context.Context) bool
	AllowInvitedSignup(ctx context.Context) bool
	// SyncBudget is the mailbox sync fair-use section, already normalized.
	SyncBudget(ctx context.Context) Sync
}

type service struct {
	store Store

	mu       sync.Mutex
	cached   Document
	cachedAt time.Time
	loaded   bool
}

// NewService wraps a store with the 30 second read cache.
func NewService(store Store) Service {
	return &service{store: store, cached: Defaults()}
}

func (s *service) Get(ctx context.Context) Document {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.loaded && time.Since(s.cachedAt) < cacheTTL {
		return s.cached
	}
	if s.store == nil {
		return s.cached
	}

	doc, err := s.store.Get(ctx)
	if err != nil {
		// Keep serving the last good document; a transient DB error must not
		// silently flip invite links off.
		return s.cached
	}
	s.cached, s.cachedAt, s.loaded = doc, time.Now(), true
	return doc
}

func (s *service) Put(ctx context.Context, patch Patch, updatedBy *uuid.UUID) (Document, error) {
	current := s.Get(ctx)
	next := patch.Apply(current)
	next.Normalize()

	if s.store != nil {
		if err := s.store.Put(ctx, next, updatedBy); err != nil {
			return current, err
		}
	}

	s.mu.Lock()
	s.cached, s.cachedAt, s.loaded = next, time.Now(), true
	s.mu.Unlock()
	return next, nil
}

func (s *service) InvitationTTL(ctx context.Context) time.Duration { return s.Get(ctx).TTL() }

func (s *service) InviteLinksEnabled(ctx context.Context) bool {
	return s.Get(ctx).Invitations.LinksEnabled
}

func (s *service) AllowInvitedSignup(ctx context.Context) bool {
	return s.Get(ctx).Access.AllowInvitedSignup
}

func (s *service) SyncBudget(ctx context.Context) Sync {
	sync := s.Get(ctx).Sync
	sync.Normalize()
	return sync
}
