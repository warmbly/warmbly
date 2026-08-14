package dispatch

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MemoryStore is a multi-goroutine-safe in-memory Store for unit tests.
type MemoryStore struct {
	mu           sync.Mutex
	control      ControlState
	reservations map[string]*Reservation
	byResID      map[uuid.UUID]*Reservation
	sends        map[string]time.Time
	sendTimes    []time.Time
	queue        map[string]*QueueItem
	queueByID    map[uuid.UUID]*QueueItem
	failures     []FailureRecord
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		reservations: map[string]*Reservation{},
		byResID:      map[uuid.UUID]*Reservation{},
		sends:        map[string]time.Time{},
		queue:        map[string]*QueueItem{},
		queueByID:    map[uuid.UUID]*QueueItem{},
	}
}

func (m *MemoryStore) GetControl(ctx context.Context) (ControlState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.control, nil
}

func (m *MemoryStore) SetPaused(ctx context.Context, paused bool, reason string, by *uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.control.Paused = paused
	m.control.PauseReason = reason
	m.control.PausedBy = by
	if paused {
		now := time.Now().UTC()
		m.control.PausedAt = &now
	} else {
		m.control.PausedAt = nil
		m.control.PauseReason = ""
	}
	return nil
}

func (m *MemoryStore) expireLocked(now time.Time) {
	for _, r := range m.reservations {
		if r.State == StateReserved && !r.LeaseUntil.After(now) {
			r.State = StateReleased
			r.LastError = "lease_expired"
		}
	}
}

func (m *MemoryStore) occupiedLocked(now time.Time, window time.Duration) ([]time.Time, time.Time) {
	cutoff := now.Add(-window)
	var times []time.Time
	var last time.Time
	for _, t := range m.sendTimes {
		if !t.Before(cutoff) && !t.After(now) {
			times = append(times, t)
			if t.After(last) {
				last = t
			}
		}
	}
	for _, r := range m.reservations {
		if r.State != StateReserved || !r.LeaseUntil.After(now) {
			continue
		}
		if !r.ReservedAt.Before(cutoff) && !r.ReservedAt.After(now) {
			times = append(times, r.ReservedAt)
			if r.ReservedAt.After(last) {
				last = r.ReservedAt
			}
		}
	}
	return times, last
}

// TryReserveAtomic holds the store mutex for the full decision (multi-instance safe).
func (m *MemoryStore) TryReserveAtomic(ctx context.Context, in AtomicReserveInput) (AtomicReserveOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := in.Now.UTC()
	window := in.Window
	if window <= 0 {
		window = RollingWindow
	}
	out := AtomicReserveOutput{}

	m.expireLocked(now)

	if t, ok := m.sends[in.Req.MessageKey]; ok {
		_ = t
		out.Allowed = true
		out.AlreadyCommitted = true
		out.Reason = "already_sent"
		out.SentLastHour = countTimes(m.sendTimes, now.Add(-window), now)
		return out, nil
	}

	if existing := m.reservations[in.Req.MessageKey]; existing != nil &&
		existing.State == StateReserved && existing.LeaseUntil.After(now) {
		existing.LeaseUntil = now.Add(in.LeaseTTL)
		cp := *existing
		out.Allowed = true
		out.Reservation = &cp
		out.Reason = "existing_lease"
		out.SentLastHour = countTimes(m.sendTimes, now.Add(-window), now)
		return out, nil
	}

	occupied, last := m.occupiedLocked(now, window)
	snap := WindowSnapshot{
		OccupiedAt: occupied, LastOccupied: last,
		Cap: in.Cap, MinGap: in.MinGap, Window: window, Now: now,
	}
	out.SentLastHour = snap.OccupiedCount()
	ok, reason, next := snap.CanGrant()
	if !ok {
		out.Reason = reason
		out.NextSlot = next
		return out, nil
	}

	// Clear released/failed/expired row for this key.
	if existing := m.reservations[in.Req.MessageKey]; existing != nil {
		delete(m.byResID, existing.ID)
		delete(m.reservations, in.Req.MessageKey)
	}

	res := &Reservation{
		ID:             uuid.New(),
		OrganizationID: in.Req.OrganizationID,
		Channel:        in.Req.Channel,
		MessageKey:     in.Req.MessageKey,
		DraftID:        in.Req.DraftID,
		State:          StateReserved,
		ReservedAt:     now,
		LeaseUntil:     now.Add(in.LeaseTTL),
		WorkerToken:    in.Req.WorkerToken,
	}
	cp := *res
	m.reservations[res.MessageKey] = &cp
	m.byResID[cp.ID] = &cp
	out.Allowed = true
	out.Reservation = res
	out.Reason = "reserved"
	out.SentLastHour = snap.OccupiedCount() + 1
	return out, nil
}

func countTimes(times []time.Time, since, until time.Time) int {
	n := 0
	for _, t := range times {
		if !t.Before(since) && !t.After(until) {
			n++
		}
	}
	return n
}

func (m *MemoryStore) ListOccupied(ctx context.Context, now time.Time, window time.Duration) ([]time.Time, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	times, last := m.occupiedLocked(now, window)
	return times, last, nil
}

func (m *MemoryStore) GetReservationByKey(ctx context.Context, messageKey string) (*Reservation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.reservations[messageKey]
	if r == nil {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}

func (m *MemoryStore) GetSendByKey(ctx context.Context, messageKey string) (time.Time, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.sends[messageKey]
	return t, ok, nil
}

func (m *MemoryStore) RefreshReservation(ctx context.Context, id uuid.UUID, leaseUntil time.Time, workerToken string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.byResID[id]
	if r == nil {
		return fmt.Errorf("reservation not found")
	}
	r.LeaseUntil = leaseUntil
	return nil
}

func (m *MemoryStore) CommitReservation(ctx context.Context, id uuid.UUID, sentAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.byResID[id]
	if r == nil {
		return fmt.Errorf("reservation not found")
	}
	if r.State == StateCommitted {
		return nil
	}
	if _, ok := m.sends[r.MessageKey]; ok {
		r.State = StateCommitted
		t := sentAt
		r.CommittedAt = &t
		return nil
	}
	r.State = StateCommitted
	t := sentAt
	r.CommittedAt = &t
	m.sends[r.MessageKey] = sentAt
	m.sendTimes = append(m.sendTimes, sentAt)
	return nil
}

func (m *MemoryStore) ReleaseReservation(ctx context.Context, id uuid.UUID, state, errText string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.byResID[id]
	if r == nil {
		return fmt.Errorf("reservation not found")
	}
	if r.State == StateCommitted {
		return nil
	}
	if state == "" {
		state = StateReleased
	}
	r.State = state
	r.LastError = errText
	return nil
}

func (m *MemoryStore) ExpireStaleReservations(ctx context.Context, now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.reservations {
		if r.State == StateReserved && !r.LeaseUntil.After(now) {
			r.State = StateReleased
			r.LastError = "lease_expired"
			n++
		}
	}
	return n, nil
}

func (m *MemoryStore) Enqueue(ctx context.Context, item *QueueItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.queue[item.MessageKey]; existing != nil {
		if existing.Status == QueueSent || existing.Status == QueueCancelled {
			return nil
		}
		existing.DueAt = item.DueAt
		existing.Priority = item.Priority
		existing.RecipientRef = item.RecipientRef
		existing.Status = QueueQueued
		return nil
	}
	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	if item.Status == "" {
		item.Status = QueueQueued
	}
	cp := *item
	m.queue[item.MessageKey] = &cp
	m.queueByID[cp.ID] = &cp
	return nil
}

func (m *MemoryStore) CancelQueue(ctx context.Context, messageKey, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	q := m.queue[messageKey]
	if q == nil {
		return nil
	}
	if q.Status == QueueSent {
		return nil
	}
	q.Status = QueueCancelled
	q.CancelReason = reason
	return nil
}

func (m *MemoryStore) CancelQueueByRecipient(ctx context.Context, orgID uuid.UUID, recipientRef, reason string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if recipientRef == "" {
		return 0, nil
	}
	n := 0
	for _, q := range m.queue {
		if q.OrganizationID != orgID || q.Status != QueueQueued {
			continue
		}
		if q.RecipientRef == recipientRef {
			q.Status = QueueCancelled
			q.CancelReason = reason
			n++
		}
	}
	return n, nil
}

func (m *MemoryStore) CountQueued(ctx context.Context, orgID *uuid.UUID) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, q := range m.queue {
		if q.Status != QueueQueued {
			continue
		}
		if orgID != nil && q.OrganizationID != *orgID {
			continue
		}
		n++
	}
	return n, nil
}

// ClaimNextQueued picks the next fair due item and marks it reserved under the lock.
func (m *MemoryStore) ClaimNextQueued(ctx context.Context, now time.Time) (*QueueItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var candidates []*QueueItem
	for _, q := range m.queue {
		if q.Status != QueueQueued || q.DueAt.After(now) {
			continue
		}
		candidates = append(candidates, q)
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if !a.DueAt.Equal(b.DueAt) {
			return a.DueAt.Before(b.DueAt)
		}
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		return a.CreatedAt.Before(b.CreatedAt)
	})
	chosen := candidates[0]
	chosen.Status = QueueReserved
	cp := *chosen
	return &cp, nil
}

func (m *MemoryStore) UpdateQueueStatus(ctx context.Context, id uuid.UUID, status, errText string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	q := m.queueByID[id]
	if q == nil {
		return fmt.Errorf("queue item not found")
	}
	q.Status = status
	if errText != "" {
		q.LastError = errText
	}
	return nil
}

func (m *MemoryStore) RecordFailure(ctx context.Context, f FailureRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	if f.OccurredAt.IsZero() {
		f.OccurredAt = time.Now().UTC()
	}
	m.failures = append(m.failures, f)
	return nil
}

func (m *MemoryStore) ListRecentFailures(ctx context.Context, limit int) ([]FailureRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit < 1 {
		limit = DefaultMaxRecentFails
	}
	out := make([]FailureRecord, len(m.failures))
	copy(out, m.failures)
	sort.Slice(out, func(i, j int) bool { return out[i].OccurredAt.After(out[j].OccurredAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemoryStore) CountActiveLeases(ctx context.Context, now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.reservations {
		if r.State == StateReserved && r.LeaseUntil.After(now) {
			n++
		}
	}
	return n, nil
}

func (m *MemoryStore) CountSendsSince(ctx context.Context, since time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, t := range m.sendTimes {
		if !t.Before(since) {
			n++
		}
	}
	return n, nil
}
