package dispatch

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// AtomicReserveInput is the full reserve decision payload (evaluated under store lock).
type AtomicReserveInput struct {
	Req      ReserveRequest
	Now      time.Time
	Cap      int
	MinGap   time.Duration
	LeaseTTL time.Duration
	Window   time.Duration
}

// AtomicReserveOutput is the result of a serialized reserve decision.
type AtomicReserveOutput struct {
	Allowed          bool
	AlreadyCommitted bool
	Reservation      *Reservation
	Reason           string
	NextSlot         time.Time
	SentLastHour     int
}

// Store is the durable multi-worker-safe backend for the governor.
type Store interface {
	GetControl(ctx context.Context) (ControlState, error)
	SetPaused(ctx context.Context, paused bool, reason string, by *uuid.UUID) error

	// TryReserveAtomic expires stale leases, checks idempotency, re-counts
	// occupied slots, and inserts a reservation under a single global lock /
	// transaction so concurrent workers cannot oversubscribe the cap.
	TryReserveAtomic(ctx context.Context, in AtomicReserveInput) (AtomicReserveOutput, error)

	GetReservationByKey(ctx context.Context, messageKey string) (*Reservation, error)
	GetSendByKey(ctx context.Context, messageKey string) (sentAt time.Time, ok bool, err error)

	RefreshReservation(ctx context.Context, id uuid.UUID, leaseUntil time.Time, workerToken string) error
	CommitReservation(ctx context.Context, id uuid.UUID, sentAt time.Time) error
	ReleaseReservation(ctx context.Context, id uuid.UUID, state, errText string) error
	ExpireStaleReservations(ctx context.Context, now time.Time) (int, error)

	// ListOccupied is for status/observability (not the reserve hot path).
	ListOccupied(ctx context.Context, now time.Time, window time.Duration) (times []time.Time, last time.Time, err error)

	Enqueue(ctx context.Context, item *QueueItem) error
	CancelQueue(ctx context.Context, messageKey, reason string) error
	// CancelQueueByRecipient cancels queued items for a recipient (email/phone) before reserve.
	CancelQueueByRecipient(ctx context.Context, orgID uuid.UUID, recipientRef, reason string) (int, error)
	CountQueued(ctx context.Context, orgID *uuid.UUID) (int, error)
	// ClaimNextQueued transactionally selects the next fair due item and marks it reserved.
	ClaimNextQueued(ctx context.Context, now time.Time) (*QueueItem, error)
	UpdateQueueStatus(ctx context.Context, id uuid.UUID, status, errText string) error

	RecordFailure(ctx context.Context, f FailureRecord) error
	ListRecentFailures(ctx context.Context, limit int) ([]FailureRecord, error)

	CountActiveLeases(ctx context.Context, now time.Time) (int, error)
	CountSendsSince(ctx context.Context, since time.Time) (int, error)
}
