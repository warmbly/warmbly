package models

import (
	"time"

	"github.com/google/uuid"
)

// PlacementOriginBatch is a test a placement batch started for one sender.
const PlacementOriginBatch = "batch"

// Placement batch statuses.
const (
	PlacementBatchQueued                = "queued"
	PlacementBatchRunning               = "running"
	PlacementBatchCompleted             = "completed"
	PlacementBatchCompletedWithWarnings = "completed_with_warnings"
	PlacementBatchCancelled             = "cancelled"
	PlacementBatchFailed                = "failed"
)

// PlacementBatchFinished reports whether a batch status is final.
func PlacementBatchFinished(status string) bool {
	switch status {
	case PlacementBatchCompleted, PlacementBatchCompletedWithWarnings, PlacementBatchCancelled, PlacementBatchFailed:
		return true
	}
	return false
}

// Statuses of one sender in a batch.
const (
	PlacementSenderQueued = "queued"
	// PlacementSenderDeferred could not run yet (no daily headroom, busy,
	// offline) and is retried later.
	PlacementSenderDeferred  = "deferred"
	PlacementSenderRunning   = "running"
	PlacementSenderCompleted = "completed"
	PlacementSenderSkipped   = "skipped"
	PlacementSenderFailed    = "failed"
	PlacementSenderCancelled = "cancelled"
)

// What a batch does with a sender that cannot run when its turn comes.
const (
	PlacementUnavailableSkip  = "skip"
	PlacementUnavailableDefer = "defer"
)

// Sender scopes a batch resolves on the server.
const (
	PlacementScopeCampaign  = "campaign"
	PlacementScopeWorkspace = "workspace"
)

// Sampling modes.
const (
	PlacementSampleAll         = "all"
	PlacementSampleRandom      = "random"
	PlacementSamplePercent     = "percent"
	PlacementSamplePerDomain   = "per_domain"
	PlacementSamplePerProvider = "per_provider"
)

// PlacementSenderScope selects a batch's senders on the server, so a fleet of
// thousands needs no id list. Filters narrow whichever scope is chosen.
type PlacementSenderScope struct {
	Type       string     `json:"type"`
	CampaignID *uuid.UUID `json:"campaign_id,omitempty"`
	// Providers keeps mailboxes hosted by these mailhost families.
	Providers []string    `json:"providers,omitempty"`
	Domains   []string    `json:"domains,omitempty"`
	TagIDs    []uuid.UUID `json:"tag_ids,omitempty"`
	// IncludeInactive keeps disconnected mailboxes; they are skipped or
	// deferred when their turn comes.
	IncludeInactive bool `json:"include_inactive,omitempty"`
	// UntestedDays keeps mailboxes with no delivered placement test in the
	// last that many days.
	UntestedDays int `json:"untested_days,omitempty"`
}

// PlacementSample picks part of the resolved senders.
type PlacementSample struct {
	Mode    string `json:"mode"`
	Count   int    `json:"count,omitempty"`
	Percent int    `json:"percent,omitempty"`
	// Stratify spreads a random or percent sample across "provider" or
	// "domain" in proportion to each group's size.
	Stratify string `json:"stratify,omitempty"`
}

// PlacementBatchSelection is how a batch's senders were chosen, stored for
// display. The sender rows are the snapshot.
type PlacementBatchSelection struct {
	SenderAccountIDs int                   `json:"sender_account_ids,omitempty"`
	Scope            *PlacementSenderScope `json:"sender_scope,omitempty"`
	Sample           PlacementSample       `json:"sample"`
	// Matched is how many senders the scope resolved to before sampling.
	Matched int `json:"matched"`
}

// PlacementBatch runs one placement test from many senders.
type PlacementBatch struct {
	ID             uuid.UUID               `json:"id"`
	OrganizationID uuid.UUID               `json:"-"`
	CreatedBy      *uuid.UUID              `json:"created_by"`
	CampaignID     *uuid.UUID              `json:"campaign_id"`
	SequenceID     *uuid.UUID              `json:"sequence_id"`
	ContactID      *uuid.UUID              `json:"contact_id"`
	Subject        string                  `json:"subject"`
	BodyHTML       string                  `json:"body_html,omitempty"`
	BodyPlain      string                  `json:"body_plain,omitempty"`
	Tracking       string                  `json:"tracking"`
	Panel          string                  `json:"panel"`
	Pace           string                  `json:"pace"`
	Families       []string                `json:"families"`
	SeedIDs        []uuid.UUID             `json:"seed_ids"`
	OnUnavailable  string                  `json:"on_unavailable"`
	Selection      PlacementBatchSelection `json:"selection"`
	SenderCount    int                     `json:"sender_count"`
	MaxCredits     int                     `json:"max_credits"`
	CreditsSpent   int                     `json:"credits_spent"`
	// MaxMicro and SpentMicro are the same in micro-dollars; the credit
	// fields are their legacy figures.
	MaxMicro   Micro      `json:"-"`
	SpentMicro Micro      `json:"-"`
	MaxUSD     float64    `json:"max_usd"`
	SpentUSD   float64    `json:"spent_usd"`
	Status     string     `json:"status"`
	Error      string     `json:"error,omitempty"`
	Active     bool       `json:"-"`
	LastTickAt *time.Time `json:"-"`
	RetryUntil time.Time  `json:"retry_until"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// PlacementBatchSender is one sender of a batch.
type PlacementBatchSender struct {
	ID             uuid.UUID  `json:"id"`
	BatchID        uuid.UUID  `json:"batch_id"`
	EmailAccountID *uuid.UUID `json:"email_account_id"`
	SenderEmail    string     `json:"sender_email"`
	SenderDomain   string     `json:"sender_domain"`
	SenderFamily   string     `json:"sender_family"`
	Position       int        `json:"-"`
	Status         string     `json:"status"`
	Reason         string     `json:"reason,omitempty"`
	Detail         string     `json:"detail,omitempty"`
	Attempts       int        `json:"attempts"`
	NextAttemptAt  time.Time  `json:"next_attempt_at"`
	StartedAt      *time.Time `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
}

// PlacementBatchProgress counts a batch's senders by status.
type PlacementBatchProgress struct {
	Total     int `json:"total"`
	Queued    int `json:"queued"`
	Deferred  int `json:"deferred"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
	Cancelled int `json:"cancelled"`
}

// Add counts one sender.
func (p *PlacementBatchProgress) Add(status string, n int) {
	p.Total += n
	switch status {
	case PlacementSenderQueued:
		p.Queued += n
	case PlacementSenderDeferred:
		p.Deferred += n
	case PlacementSenderRunning:
		p.Running += n
	case PlacementSenderCompleted:
		p.Completed += n
	case PlacementSenderSkipped:
		p.Skipped += n
	case PlacementSenderFailed:
		p.Failed += n
	case PlacementSenderCancelled:
		p.Cancelled += n
	}
}

// Open is how many senders still have something to do.
func (p PlacementBatchProgress) Open() int { return p.Queued + p.Deferred + p.Running }
