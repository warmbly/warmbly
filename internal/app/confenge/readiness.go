package confenge

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/confenge/dispatch"
	"github.com/warmbly/warmbly/internal/app/whatsapp"
	"github.com/warmbly/warmbly/internal/models"
)

// Channel readiness codes for the operator panel.
const (
	ReadyOK            = "ready"
	ReadyNotReady      = "not_ready"
	ReadyBlockedPolicy = "blocked_by_policy"
	ReadyNotConfigured = "not_configured"
	ReadyFallback      = "fallback_template"
)

// Readiness is the discrete operator status panel payload.
type Readiness struct {
	Email             string     `json:"email"`
	WhatsApp          string     `json:"whatsapp"`
	FeedConfigured    bool       `json:"feed_configured"`
	FeedAgeSeconds    *int64     `json:"feed_age_seconds,omitempty"`
	FeedAgeLabel      string     `json:"feed_age"`
	FeedState         string     `json:"feed_state"`
	FeedSnapshot      string     `json:"feed_snapshot_hash,omitempty"`
	FeedLastSyncAt    *time.Time `json:"feed_last_success_at,omitempty"`
	FeedSourceAt      *time.Time `json:"feed_source_generated_at,omitempty"`
	FeedSyncedAt      *time.Time `json:"feed_synced_at,omitempty"`
	FeedMaxAgeSeconds int64      `json:"feed_max_age_seconds"`
	OutcomeLoop       string     `json:"outcome_loop"`
	AI                string     `json:"ai"`
	// GovernorCap is the global rolling-hour outbound cap (email+WhatsApp).
	// Primary CONFENGE pacing control (~10/h). Not the campaign daily limit.
	GovernorCap int `json:"governor_cap"`
	// CampaignDailyLimit is the campaign-shell daily ceiling (secondary).
	CampaignDailyLimit int `json:"campaign_daily_limit"`
	// EffectiveDailyCap is min(campaign daily, hourly*window hours) when computable.
	EffectiveDailyCap int    `json:"effective_daily_cap"`
	QueueCount        int    `json:"queue_count"`
	KillSwitch        bool   `json:"kill_switch"`
	SendingAllowed    bool   `json:"sending_allowed"`
	OutreachEnabled   bool   `json:"outreach_enabled"`
	RequireHuman      bool   `json:"require_human_approval"`
	AutoSendEnabled   bool   `json:"auto_send_enabled"`
	WhatsAppEnabled   bool   `json:"whatsapp_enabled"`
	WhatsAppProvider  string `json:"whatsapp_provider,omitempty"`
	PilotCohortState  string `json:"pilot_cohort_state"`
	PilotPrepared     int    `json:"pilot_cohort_prepared"`
	PilotNeedsReview  int    `json:"pilot_cohort_needs_review"`
	PilotApproved     int    `json:"pilot_cohort_approved"`
	PilotSent         int    `json:"pilot_cohort_sent"`
}

// ReadinessInputs are optional live signals for BuildReadiness.
type ReadinessInputs struct {
	EmailReady            bool
	WhatsAppReady         bool
	WhatsAppPolicyBlocked bool
	LastImportAt          *time.Time
	LastSyncAt            *time.Time
	FeedSnapshot          string
	Queue                 *models.OutreachQueueSummary
	AIConfigured          bool
	WA                    *whatsapp.Config
	Now                   time.Time
}

// BuildReadiness aggregates operator-facing readiness without side effects.
func BuildReadiness(cfg Config, in ReadinessInputs) Readiness {
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	waCfg := whatsapp.LoadConfig()
	if in.WA != nil {
		waCfg = *in.WA
	}

	dcfg := dispatch.LoadConfig()
	hourly := dcfg.SendsPerHour
	if hourly <= 0 {
		hourly = dispatch.DefaultSendsPerHour
	}
	windowHours := SendWindowHours(dcfg.WindowStart, dcfg.WindowEnd)
	if windowHours <= 0 {
		windowHours = 9 // 09:00–18:00 default
	}
	daily := cfg.DefaultDailyLimit
	if daily < 1 {
		daily = DefaultCampaignDailyLimit
	}
	hourlyDay := hourly * windowHours
	effective := daily
	if hourlyDay > 0 && hourlyDay < effective {
		effective = hourlyDay
	}

	r := Readiness{
		OutreachEnabled:    cfg.Enabled,
		RequireHuman:       cfg.RequireHumanApproval,
		AutoSendEnabled:    cfg.AutoSendEnabled,
		GovernorCap:        hourly,
		CampaignDailyLimit: daily,
		EffectiveDailyCap:  effective,
		FeedConfigured:     strings.TrimSpace(cfg.FeedURL) != "" || strings.TrimSpace(cfg.ManifestURL) != "",
		KillSwitch:         !cfg.SendingAllowed(),
		SendingAllowed:     cfg.SendingAllowed(),
		WhatsAppEnabled:    cfg.WhatsAppEnabled || waCfg.Enabled,
		WhatsAppProvider:   waCfg.Provider,
	}
	maxAge := cfg.FeedMaxAge
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}
	r.FeedMaxAgeSeconds = int64(maxAge.Seconds())

	if in.EmailReady {
		r.Email = ReadyOK
	} else {
		r.Email = ReadyNotReady
	}

	switch {
	case !r.WhatsAppEnabled:
		r.WhatsApp = ReadyNotConfigured
	case in.WhatsAppPolicyBlocked:
		r.WhatsApp = ReadyBlockedPolicy
	case in.WhatsAppReady:
		r.WhatsApp = ReadyOK
	default:
		r.WhatsApp = ReadyNotReady
	}

	if cfg.OutcomeWebhookURL != "" && cfg.OutcomeWebhookSecret != "" {
		r.OutcomeLoop = ReadyOK
	} else {
		r.OutcomeLoop = ReadyNotConfigured
	}

	if in.AIConfigured {
		r.AI = ReadyOK
	} else {
		r.AI = ReadyFallback
	}

	if in.LastImportAt != nil {
		age := int64(now.Sub(in.LastImportAt.UTC()).Seconds())
		if age < 0 {
			age = 0
		}
		r.FeedAgeSeconds = &age
		r.FeedAgeLabel = formatAge(age)
		r.FeedLastSyncAt = in.LastImportAt
		r.FeedSourceAt = in.LastImportAt
		r.FeedSyncedAt = in.LastSyncAt
		r.FeedSnapshot = in.FeedSnapshot
		if time.Duration(age)*time.Second > maxAge {
			r.FeedState = "stale"
		} else {
			r.FeedState = "fresh"
		}
	} else {
		r.FeedAgeLabel = "unknown"
		r.FeedState = "missing"
	}

	if in.Queue != nil {
		r.QueueCount = in.Queue.NeedsReview + in.Queue.ReadyToGenerate + in.Queue.Approved
	}
	return r
}

func formatAge(seconds int64) string {
	if seconds < 60 {
		return "just now"
	}
	if seconds < 3600 {
		return formatInt(seconds/60) + "m"
	}
	if seconds < 86400 {
		return formatInt(seconds/3600) + "h"
	}
	return formatInt(seconds/86400) + "d"
}

func formatInt(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// CollectReadiness loads org signals and builds Readiness for the API.
func (s *service) CollectReadiness(ctx context.Context, orgID uuid.UUID, emailReady bool) Readiness {
	in := ReadinessInputs{
		EmailReady:   emailReady,
		AIConfigured: s.ai != nil,
	}
	if s.cfg.WhatsAppEnabled && s.wa != nil {
		in.WhatsAppReady = true
	}
	state, stateErr := s.repo.GetFeedSyncState(ctx, orgID)
	if stateErr == nil && state != nil {
		if state.LastStatus == "completed" && state.SourceGeneratedAt != nil && state.LastSnapshotHash != "" && state.LastRunID != "" {
			in.LastImportAt = state.SourceGeneratedAt
			in.LastSyncAt = state.LastSuccessAt
			in.FeedSnapshot = state.LastSnapshotHash
		}
	} else if stateErr == nil && state == nil {
		runs, err := s.repo.ListImportRuns(ctx, orgID, 1)
		if err == nil && len(runs) > 0 && runs[0].Status == models.OutreachImportCompleted {
			in.LastImportAt = runs[0].SourceGeneratedAt
			in.LastSyncAt = runs[0].FinishedAt
			in.FeedSnapshot = runs[0].SnapshotHash
		}
	}
	if sum, err := s.repo.CountByQueueState(ctx, orgID); err == nil {
		in.Queue = sum
	}
	readiness := BuildReadiness(s.cfg, in)
	readiness.PilotCohortState = "unavailable"
	cohortID := uuid.NewSHA1(orgID, []byte("confenge-pilot-v1")).String()
	memberships, err := s.repo.ListPilotMemberships(ctx, orgID, cohortID)
	if err != nil {
		return readiness
	}
	readiness.PilotCohortState = "ready"
	readiness.PilotPrepared = len(memberships)
	for i := range memberships {
		touchpoint, touchpointErr := s.repo.GetTouchpoint(ctx, orgID, memberships[i].TouchpointID)
		if touchpointErr != nil || touchpoint == nil {
			readiness.PilotCohortState = "unavailable"
			readiness.PilotNeedsReview = 0
			readiness.PilotApproved = 0
			readiness.PilotSent = 0
			return readiness
		}
		switch touchpoint.State {
		case models.TouchpointNeedsReview:
			readiness.PilotNeedsReview++
		case models.TouchpointApproved, models.TouchpointQueued:
			readiness.PilotApproved++
		case models.TouchpointSent, models.TouchpointReplied:
			readiness.PilotSent++
		}
	}
	return readiness
}

// RedactSecret returns a non-empty secret marker without leaking the value.
func RedactSecret(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
}

// AIProviderName reports the configured AI_PROVIDER env (empty = template fallback).
func AIProviderName() string {
	return strings.TrimSpace(os.Getenv("AI_PROVIDER"))
}
