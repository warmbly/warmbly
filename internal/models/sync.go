package models

import (
	"time"

	"github.com/google/uuid"
)

// SyncPolicy is the fair-use budget a mailbox syncs under. The control plane
// resolves it when the mailbox is loaded onto a worker (instance settings,
// see internal/app/instancesettings) and ships it inside ADD_EMAIL, so the
// worker never needs to read configuration of its own.
type SyncPolicy struct {
	// BackfillDays is how far back the initial import reaches.
	BackfillDays int `json:"backfill_days" avro:"backfill_days"`
	// BackfillMessages caps how many messages the initial import stores.
	BackfillMessages int `json:"backfill_messages" avro:"backfill_messages"`
	// DailyMessages caps new (live) messages stored per UTC day. Replies to
	// the mailbox's own sends have a separate budget of the same size.
	DailyMessages int `json:"daily_messages" avro:"daily_messages"`
	// OrgDailyMessages caps new plus backfilled messages stored across the
	// whole organization per UTC day.
	OrgDailyMessages int `json:"org_daily_messages" avro:"org_daily_messages"`
}

// SyncBackfillStatus is where the initial import stands.
type SyncBackfillStatus string

const (
	// SyncBackfillPending: the mailbox is loaded but the import has not run yet.
	SyncBackfillPending SyncBackfillStatus = "pending"
	// SyncBackfillRunning: the import is walking history and may be paced.
	SyncBackfillRunning SyncBackfillStatus = "running"
	// SyncBackfillComplete: the window is exhausted or the cap was reached.
	SyncBackfillComplete SyncBackfillStatus = "complete"
)

// SyncFolderCursor is the resumable position inside one folder of a backfill.
// IMAP keys folders by UIDVALIDITY and walks UIDs downward; Graph keys by
// well-known folder name and follows @odata.nextLink; Gmail has no folders and
// uses SyncCursor.PageToken.
type SyncFolderCursor struct {
	// Next is an opaque continuation (Graph nextLink).
	Next string `json:"next,omitempty" avro:"next"`
	// UID is the lowest UID already imported; the walk continues below it.
	UID uint32 `json:"uid,omitempty" avro:"uid"`
	// Done marks the folder exhausted for this window.
	Done bool `json:"done,omitempty" avro:"done"`
}

// SyncCursor is the resumable position of a mailbox's backfill. It is stored
// as jsonb: read-then-execute state that is never filtered in SQL.
type SyncCursor struct {
	// PageToken is Gmail's messages.list continuation.
	PageToken string `json:"page_token,omitempty" avro:"page_token"`
	// Folders is the per-folder position for IMAP and Graph.
	Folders map[string]SyncFolderCursor `json:"folders,omitempty" avro:"folders"`
}

// SyncState is what the platform knows about a mailbox's sync: backfill
// progress, whether fair use is holding it, and when it last ran. The worker
// owns the live copy and relays every change as SYNC_STATE; the consumer
// persists it and the loader hands it back on the next (re)assignment.
type SyncState struct {
	BackfillStatus SyncBackfillStatus `json:"backfill_status" avro:"backfill_status"`
	BackfillCursor SyncCursor         `json:"backfill_cursor" avro:"backfill_cursor"`
	// BackfillSynced counts messages the import has stored so far.
	BackfillSynced int `json:"backfill_synced" avro:"backfill_synced"`
	// BackfillSince is the cutoff the running import uses; fixed at start so
	// a later settings change does not move the goalposts mid-walk.
	BackfillSince       *time.Time `json:"backfill_since,omitempty" avro:"backfill_since"`
	BackfillStartedAt   *time.Time `json:"backfill_started_at,omitempty" avro:"backfill_started_at"`
	BackfillCompletedAt *time.Time `json:"backfill_completed_at,omitempty" avro:"backfill_completed_at"`

	// ThrottledUntil is set while fair use is deferring live mail; nil when
	// the mailbox is within budget.
	ThrottledUntil *time.Time `json:"throttled_until,omitempty" avro:"throttled_until"`
	// ThrottleReason names the exhausted budget (see SyncThrottle* constants).
	ThrottleReason string `json:"throttle_reason,omitempty" avro:"throttle_reason"`
	// Deferred counts live messages currently waiting on budget: seen on the
	// server but not yet stored. Drops back to zero once they are admitted.
	Deferred int `json:"deferred" avro:"deferred"`

	LastSyncedAt *time.Time `json:"last_synced_at,omitempty" avro:"last_synced_at"`
}

// Throttle reasons, stable strings the dashboard maps to copy.
const (
	SyncThrottleBurst        = "burst"
	SyncThrottleHourly       = "hourly"
	SyncThrottleDaily        = "daily"
	SyncThrottleOrgDaily     = "org_daily"
	SyncThrottlePriorityFull = "priority_daily"
)

// AddWorkerEmailSyncData seeds a mailbox's sync on the worker: the budget it
// runs under and where a previous worker left off. State is nil on first
// connect.
type AddWorkerEmailSyncData struct {
	Policy SyncPolicy `json:"policy" avro:"policy"`
	State  *SyncState `json:"state" avro:"state"`
}

// JobEventSyncState is the worker's SYNC_STATE relay: the full current state,
// not a delta, so a lost event is repaired by the next one.
type JobEventSyncState struct {
	UserID  uuid.UUID `json:"user_id"`
	EmailID uuid.UUID `json:"email_id"`
	State   SyncState `json:"state"`
}
