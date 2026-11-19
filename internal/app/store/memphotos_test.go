package store

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// photoTry is a mailbox's photo retry state, which the SQL keeps on the mailbox row.
type photoTry struct {
	attempts int
	at       time.Time
}

func (r *memRepo) CreatePhoto(_ context.Context, p *models.StorePhoto) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := cp(p)
	n.CreatedAt = r.now()
	r.photos[p.ID] = n
	return nil
}

func (r *memRepo) GetPhoto(_ context.Context, orgID, id uuid.UUID) (*models.StorePhoto, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.photos[id]
	if p == nil || p.OrganizationID != orgID {
		return nil, nil
	}
	return cp(p), nil
}

func (r *memRepo) CountPhotosSince(_ context.Context, orgID uuid.UUID, since time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, p := range r.photos {
		if p.OrganizationID == orgID && !p.CreatedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

func (r *memRepo) MarkPhotosUsed(_ context.Context, orgID uuid.UUID, ids []uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	for _, id := range ids {
		if p := r.photos[id]; p != nil && p.OrganizationID == orgID {
			p.UsedAt = &now
		}
	}
	return nil
}

func (r *memRepo) SetMailboxPhoto(_ context.Context, orgID, id, photoID uuid.UUID, onlyUnset bool) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.mailboxes[id]
	p := r.photos[photoID]
	if m == nil || m.OrganizationID != orgID || p == nil || p.OrganizationID != orgID || (onlyUnset && m.PhotoID != nil) {
		return false, nil
	}
	m.PhotoID = &photoID
	delete(r.photoTry, id)
	return true, nil
}

func (r *memRepo) MarkPhotoApplied(_ context.Context, orgID, id, photoID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	m := r.mailboxes[id]
	if m == nil || m.OrganizationID != orgID || m.PhotoID == nil || *m.PhotoID != photoID {
		return nil
	}
	m.PhotoAppliedID = &photoID
	r.photoTry[id] = photoTry{at: r.now()}
	return nil
}

func (r *memRepo) RecordPhotoFailure(_ context.Context, orgID, id uuid.UUID, giveUp bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m := r.mailboxes[id]; m == nil || m.OrganizationID != orgID {
		return nil
	}
	t := r.photoTry[id]
	t.attempts++
	if giveUp {
		t.attempts = 1000
	}
	t.at = r.now()
	r.photoTry[id] = t
	return nil
}

func (r *memRepo) PendingPhotos(_ context.Context, triedBefore time.Time, maxAttempts, limit int) ([]models.StoreMailbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StoreMailbox
	for _, m := range r.mailboxes {
		if m.PhotoID == nil || (m.PhotoAppliedID != nil && *m.PhotoAppliedID == *m.PhotoID) || !liveMailbox(m) {
			continue
		}
		t := r.photoTry[m.ID]
		if t.attempts >= maxAttempts || (!t.at.IsZero() && !t.at.Before(triedBefore)) {
			continue
		}
		out = append(out, *m)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (r *memRepo) StalePhotos(_ context.Context, unusedBefore, usedBefore time.Time, limit int) ([]models.StorePhoto, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []models.StorePhoto
	for _, p := range r.photos {
		stale := (p.UsedAt == nil && p.CreatedAt.Before(unusedBefore)) || (p.UsedAt != nil && p.UsedAt.Before(usedBefore))
		carried := slices.ContainsFunc(mapValues(r.mailboxes), func(m *models.StoreMailbox) bool { return m.PhotoID != nil && *m.PhotoID == p.ID })
		if stale && !carried {
			out = append(out, *p)
		}
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func (r *memRepo) DeletePhoto(_ context.Context, orgID, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p := r.photos[id]; p != nil && p.OrganizationID == orgID {
		delete(r.photos, id)
	}
	return nil
}

func mapValues[K comparable, V any](m map[K]V) []V {
	out := make([]V, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}
