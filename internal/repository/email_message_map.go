package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// EmailMessageMapRepository maps a provider messageId to the internal email
// (id, threadId) for a given user mailbox. The worker writes and reads it on
// the mailbox-sync path to dedupe incoming messages and resolve threads.
//
// Two implementations exist: a Postgres-backed one used by the backend
// (NewEmailMessageMapRepository) and an HTTP-backed one used by the worker
// (NewHTTPEmailMessageMapRepository), which calls the backend's internal API so
// the worker never opens Postgres directly (per CLAUDE.md).
type EmailMessageMapRepository interface {
	Add(ctx context.Context, data EmailMessageData) error
	Get(ctx context.Context, userID, emailID uuid.UUID, messageID string) (*EmailMessageData, error)
	Del(ctx context.Context, userID, emailID uuid.UUID, messageID string, id uuid.UUID) error
}

var ErrArrivalOutboxUnsupported = errors.New("durable arrival admission unavailable; upgrade backend and consumer first")

// ArrivalAdmission is an optional internal protocol, separate from legacy map writes.
type ArrivalAdmission interface {
	AdmitArrival(context.Context, EmailMessageData, *PendingArrival) error
}

type PendingArrival struct {
	Arrival   *models.JobEventNewEmail         `json:"arrival"`
	Bounce    *models.JobEventInboundBounce    `json:"bounce,omitempty"`
	Complaint *models.JobEventInboundComplaint `json:"complaint,omitempty"`
}

type ArrivalOutbox interface {
	HasPendingArrival(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (bool, error)
	DeliverArrivals(context.Context, func(context.Context, models.JobEventType, any) error) error
}

// EmailMessageData is one (user, mailbox, providerMessageID) -> internal
// (id, threadID) mapping row. The UUID-typed values travel as strings to match
// the worker call sites, which pass uuid.String().
type EmailMessageData struct {
	UserID    string
	EmailID   string
	MessageID string
	ID        string
	ThreadID  string
}
