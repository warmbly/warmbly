package models

import (
	"time"

	"github.com/google/uuid"
)

// DeletionResourceType identifies what a ScheduledDeletion targets.
type DeletionResourceType string

const (
	DeletionResourceOrganization DeletionResourceType = "organization"
	DeletionResourceUser         DeletionResourceType = "user"
)

// DeletionStatus is the lifecycle state of a ScheduledDeletion.
type DeletionStatus string

const (
	DeletionStatusPending   DeletionStatus = "pending"
	DeletionStatusExecuting DeletionStatus = "executing"
	DeletionStatusCompleted DeletionStatus = "completed"
	DeletionStatusCancelled DeletionStatus = "cancelled"
	DeletionStatusFailed    DeletionStatus = "failed"
)

// Notification bits track which reminder emails have been sent for a
// scheduled deletion, so the background job can be idempotent.
const (
	DeletionNotifInitial    = 1 << 0 // sent immediately on schedule
	DeletionNotif7Day       = 1 << 1 // sent 7 days before execute_after
	DeletionNotif24Hour     = 1 << 2 // sent 24 hours before execute_after
	DeletionNotifCompletion = 1 << 3 // sent after a successful hard delete
	DeletionNotifCancelled  = 1 << 4 // sent after a cancellation
)

// Default grace windows. We picked 30 days for both because that matches
// what GitLab / GCP / Atlassian use for the same kinds of resources and
// users have come to expect roughly a month.
const (
	OrganizationDeletionGraceDays = 30
	UserDeletionGraceDays         = 30
)

// ScheduledDeletion is a row in the scheduled_deletions table. It tracks
// one pending soft-delete for a single resource.
type ScheduledDeletion struct {
	ID uuid.UUID `json:"id"`

	ResourceType DeletionResourceType `json:"resource_type"`
	ResourceID   uuid.UUID            `json:"resource_id"`

	OrganizationID *uuid.UUID `json:"organization_id,omitempty"`

	RequestedByUserID uuid.UUID `json:"requested_by_user_id"`
	Reason            *string   `json:"reason,omitempty"`

	ScheduledAt  time.Time `json:"scheduled_at"`
	ExecuteAfter time.Time `json:"execute_after"`
	GraceDays    int       `json:"grace_days"`

	Status DeletionStatus `json:"status"`

	CancelledAt       *time.Time `json:"cancelled_at,omitempty"`
	CancelledByUserID *uuid.UUID `json:"cancelled_by_user_id,omitempty"`
	CancelledReason   *string    `json:"cancelled_reason,omitempty"`

	ExecutedAt     *time.Time `json:"executed_at,omitempty"`
	ExecutionError *string    `json:"execution_error,omitempty"`

	NotificationsSent int        `json:"-"`
	LastReminderAt    *time.Time `json:"last_reminder_at,omitempty"`
}

// IsActive reports whether the deletion is still pending (not yet
// executed, cancelled, or failed).
func (d *ScheduledDeletion) IsActive() bool {
	return d.Status == DeletionStatusPending
}

// TimeRemaining is how long the user still has to cancel, or zero if
// the window has passed.
func (d *ScheduledDeletion) TimeRemaining() time.Duration {
	r := time.Until(d.ExecuteAfter)
	if r < 0 {
		return 0
	}
	return r
}

// HasNotif checks whether the given notification bit has been sent.
func (d *ScheduledDeletion) HasNotif(bit int) bool {
	return d.NotificationsSent&bit == bit
}

// ScheduleDeletionRequest is the body for POST /<resource>/danger-zone/delete.
//
// We require a free-text confirmation phrase (the org name or the user's
// email) to make sure the click was intentional, not an accident.
type ScheduleDeletionRequest struct {
	Confirmation string `json:"confirmation" binding:"required"`
	Reason       string `json:"reason,omitempty"`
}

// CancelDeletionRequest is the body for DELETE /<resource>/danger-zone/delete.
type CancelDeletionRequest struct {
	Reason string `json:"reason,omitempty"`
}

// DangerZoneStatus is what the client gets from GET /<resource>/danger-zone.
// It always contains the resource summary; PendingDeletion is set only
// when the resource is currently scheduled for hard delete.
type DangerZoneStatus struct {
	ResourceType     DeletionResourceType `json:"resource_type"`
	ResourceID       uuid.UUID            `json:"resource_id"`
	ResourceName     string               `json:"resource_name"`
	ConfirmationHint string               `json:"confirmation_hint"`
	GraceDays        int                  `json:"grace_days"`
	PendingDeletion  *ScheduledDeletion   `json:"pending_deletion,omitempty"`
}
