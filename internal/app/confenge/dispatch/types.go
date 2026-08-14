// Package dispatch implements the CONFENGE global outbound governor:
// a multi-worker-safe rolling-hour cap shared by email and WhatsApp.
//
// Pacing is operational capacity and reputation protection, not anti-spam evasion.
package dispatch

import (
	"time"

	"github.com/google/uuid"
)

const (
	ChannelEmail    = "EMAIL"
	ChannelWhatsApp = "WHATSAPP"
)

const (
	StateReserved  = "reserved"
	StateCommitted = "committed"
	StateReleased  = "released"
	StateFailed    = "failed"
)

const (
	QueueQueued    = "queued"
	QueueReserved  = "reserved"
	QueueSent      = "sent"
	QueueCancelled = "cancelled"
	QueueFailed    = "failed"
)

const (
	DefaultSendsPerHour   = 10
	DefaultMinGapSeconds  = 360
	DefaultTimezone       = "America/Sao_Paulo"
	DefaultWindowStart    = "09:00"
	DefaultWindowEnd      = "18:00"
	DefaultLeaseTTL       = 5 * time.Minute
	RollingWindow         = 60 * time.Minute
	DefaultMaxRecentFails = 20
)

type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

type FixedClock struct {
	T time.Time
}

func (c *FixedClock) Now() time.Time { return c.T.UTC() }

func (c *FixedClock) Advance(d time.Duration) { c.T = c.T.Add(d) }

func (c *FixedClock) Set(t time.Time) { c.T = t.UTC() }

type Config struct {
	SendsPerHour   int
	MinGap         time.Duration
	Timezone       string
	WindowStart    string
	WindowEnd      string
	LeaseTTL       time.Duration
	EnvPaused      bool
	EnvPauseReason string
	// BusinessDaysOnly rejects Sat/Sun even inside HH:MM window (default true).
	BusinessDaysOnly bool
	// Adaptive rate: when RateMode=="adaptive", SendsPerHour is the current effective cap.
	RateMode         string // "fixed" | "adaptive"
	RateStartPerHour int
	RateMaxPerHour   int
	// Health counters for adaptive step (in-memory; store may also track failures).
	AdaptiveBatchSize int // commits required before step-up evaluation
}

func DefaultConfig() Config {
	return Config{
		SendsPerHour:      DefaultSendsPerHour,
		MinGap:            time.Duration(DefaultMinGapSeconds) * time.Second,
		Timezone:          DefaultTimezone,
		WindowStart:       DefaultWindowStart,
		WindowEnd:         DefaultWindowEnd,
		LeaseTTL:          DefaultLeaseTTL,
		BusinessDaysOnly:  true,
		RateMode:          "adaptive",
		RateStartPerHour:  DefaultSendsPerHour,
		RateMaxPerHour:    20,
		AdaptiveBatchSize: 20,
	}
}

// MinGapForRate returns the nominal min-gap for a target hourly rate.
func MinGapForRate(sendsPerHour int) time.Duration {
	switch {
	case sendsPerHour >= 20:
		return 180 * time.Second
	case sendsPerHour >= 15:
		return 240 * time.Second
	default:
		return 360 * time.Second
	}
}

type Reservation struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	Channel        string
	MessageKey     string
	DraftID        *uuid.UUID
	State          string
	ReservedAt     time.Time
	LeaseUntil     time.Time
	CommittedAt    *time.Time
	WorkerToken    string
	LastError      string
}

type ReserveRequest struct {
	OrganizationID uuid.UUID
	Channel        string
	MessageKey     string
	DraftID        *uuid.UUID
	WorkerToken    string
	// CapOverride, when >0, tightens the hourly cap for this reserve to min(SendsPerHour, CapOverride).
	// Used so campaign_policy max_rate_per_hour cannot be exceeded by adaptive ramp.
	CapOverride int
}

type ReserveResult struct {
	Allowed          bool
	AlreadyCommitted bool
	Reservation      *Reservation
	Reason           string
	NextSlot         time.Time
	SentLastHour     int
	Cap              int
}

type QueueItem struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	Channel        string
	DraftID        uuid.UUID
	MessageKey     string
	RecipientRef   string // email or E.164 for DNC/opt-out cancel before reserve
	DueAt          time.Time
	Priority       int
	Status         string
	CancelReason   string
	LastError      string
	CreatedAt      time.Time
}

type EnqueueRequest struct {
	OrganizationID uuid.UUID
	Channel        string
	DraftID        uuid.UUID
	MessageKey     string
	RecipientRef   string
	DueAt          time.Time
	Priority       int
}

type ControlState struct {
	Paused      bool
	PauseReason string
	PausedAt    *time.Time
	PausedBy    *uuid.UUID
}

type FailureRecord struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID *uuid.UUID `json:"organization_id,omitempty"`
	Channel        string     `json:"channel"`
	MessageKey     string     `json:"message_key"`
	DraftID        *uuid.UUID `json:"draft_id,omitempty"`
	ErrorText      string     `json:"error_text"`
	OccurredAt     time.Time  `json:"occurred_at"`
}

type Status struct {
	SentLastHour   int             `json:"sent_last_hour"`
	Cap            int             `json:"cap"`
	MinGapSeconds  int             `json:"min_gap_seconds"`
	NextSlotAt     *time.Time      `json:"next_slot_at,omitempty"`
	QueuedApproved int             `json:"queued_approved"`
	Paused         bool            `json:"paused"`
	PauseReason    string          `json:"pause_reason,omitempty"`
	InSendWindow   bool            `json:"in_send_window"`
	Timezone       string          `json:"timezone"`
	WindowStart    string          `json:"window_start"`
	WindowEnd      string          `json:"window_end"`
	ActiveLeases   int             `json:"active_leases"`
	RecentFailures []FailureRecord `json:"recent_failures,omitempty"`
}
