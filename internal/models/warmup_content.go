package models

import (
	"time"

	"github.com/google/uuid"
)

// Content sources for warmup message bodies. "static" is the in-code library;
// "ai" is a thread drawn from the offline-generated warmup_conversations bank.
const (
	WarmupContentSourceStatic = "static"
	WarmupContentSourceAI     = "ai"
)

// AdminSettingsKeyWarmupGeneration is the admin_settings key under which the
// warmup generation + engagement config document is stored.
const AdminSettingsKeyWarmupGeneration = "warmup_generation"

// WarmupConversation is a cached conversation thread used as warmup content.
// Messages are the follow-up question lines the sender can pick from; the
// Description is the opening body line. Both may contain {a|b|c} spintax,
// which is expanded at render time.
type WarmupConversation struct {
	ID             uuid.UUID  `json:"id"`
	PoolType       string     `json:"pool_type"`
	Segment        string     `json:"segment"`
	Source         string     `json:"source"`
	Theme          string     `json:"theme"`
	Subject        string     `json:"subject"`
	Description    string     `json:"description"`
	Messages       []string   `json:"messages"`
	Status         string     `json:"status"`
	LintPassed     bool       `json:"lint_passed"`
	UsageCount     int64      `json:"usage_count"`
	GeneratedByJob *uuid.UUID `json:"generated_by_job_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// Warmup generation job modes. "sync" runs every model call inline (the
// original behaviour); "batch" submits one OpenAI Batch API job and ingests its
// results asynchronously when the batch completes.
const (
	WarmupGenerationModeSync  = "sync"
	WarmupGenerationModeBatch = "batch"
)

// WarmupGenerationJob records one offline generation run for observability.
// Batch runs additionally carry the OpenAI batch/file identifiers and the
// last-observed batch status so the poller can reconcile them.
type WarmupGenerationJob struct {
	ID                uuid.UUID  `json:"id"`
	RequestedBy       *uuid.UUID `json:"requested_by,omitempty"`
	Trigger           string     `json:"trigger"` // "manual" | "schedule"
	Mode              string     `json:"mode"`    // "sync" | "batch"
	PoolType          string     `json:"pool_type"`
	Segment           string     `json:"segment"`
	Theme             string     `json:"theme"`
	Model             string     `json:"model"`
	RequestedCount    int        `json:"requested_count"`
	GeneratedCount    int        `json:"generated_count"`
	LintRejectedCount int        `json:"lint_rejected_count"`
	FailedCount       int        `json:"failed_count"`
	Status            string     `json:"status"` // pending | running | completed | failed
	Error             string     `json:"error"`
	// Batch-only fields. BatchStatus is the last status reported by OpenAI
	// (validating | in_progress | finalizing | completed | failed | expired |
	// cancelling | cancelled); empty for sync jobs.
	BatchID           string     `json:"batch_id,omitempty"`
	BatchInputFileID  string     `json:"batch_input_file_id,omitempty"`
	BatchOutputFileID string     `json:"batch_output_file_id,omitempty"`
	BatchStatus       string     `json:"batch_status,omitempty"`
	CompletionWindow  string     `json:"completion_window,omitempty"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	FinishedAt        *time.Time `json:"finished_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// WarmupGenerationPoolConfig is per-pool generation policy.
type WarmupGenerationPoolConfig struct {
	PoolType            string   `json:"pool_type"`
	Enabled             bool     `json:"enabled"`
	TargetActiveThreads int      `json:"target_active_threads"`
	Segments            []string `json:"segments"`
}

// WarmupEngagementSettings controls how recipient-side warmup engagement
// actions are selected. Rates are percentages (0-100). Defaults preserve the
// previous always-on rescue behaviour but break the uniform "every account
// marks important, instantly" bot signature with per-mailbox probability and
// a randomised dwell delay.
type WarmupEngagementSettings struct {
	SpamRescueRate    int `json:"spam_rescue_rate"`
	MarkImportantRate int `json:"mark_important_rate"`
	MarkReadRate      int `json:"mark_read_rate"`
	// StarRate is the chance a warmup message is starred (Gmail STARRED). A
	// star is a deliberate positive signal distinct from "important"; kept
	// lower than read/important so the pool doesn't star in lockstep.
	StarRate        int `json:"star_rate"`
	MinDwellSeconds int `json:"min_dwell_seconds"`
	MaxDwellSeconds int `json:"max_dwell_seconds"`
}

// WarmupGenerationSettings is the admin-controlled config document for the
// offline AI thread-bank and recipient engagement. Stored as JSON in
// admin_settings under AdminSettingsKeyWarmupGeneration.
type WarmupGenerationSettings struct {
	// Enabled is the master switch for using AI-generated content in the
	// live send selection. When false the static library is used exclusively.
	Enabled bool `json:"enabled"`
	// ScheduleEnabled runs the background generation job on CadenceHours.
	ScheduleEnabled      bool                         `json:"schedule_enabled"`
	CadenceHours         int                          `json:"cadence_hours"`
	Model                string                       `json:"model"`
	MaxMessagesPerThread int                          `json:"max_messages_per_thread"`
	DailyGenerationCap   int                          `json:"daily_generation_cap"`
	AISelectionShare     int                          `json:"ai_selection_share"` // 0-100
	Pools                []WarmupGenerationPoolConfig `json:"pools"`
	Engagement           WarmupEngagementSettings     `json:"engagement"`
}

// DefaultWarmupGenerationSettings returns conservative defaults: AI off (static
// library only) and engagement rates that keep the strong "not spam" rescue
// signal while adding variation and dwell. Content is ONE shared library
// (free/premium pools only isolate mailbox reputation, not content), so there's
// a single library config; "premium" is just its canonical bucket label.
func DefaultWarmupGenerationSettings() WarmupGenerationSettings {
	return WarmupGenerationSettings{
		Enabled:              false,
		ScheduleEnabled:      false,
		CadenceHours:         24,
		Model:                "gpt-4o-mini",
		MaxMessagesPerThread: 6,
		DailyGenerationCap:   200,
		AISelectionShare:     50,
		Pools: []WarmupGenerationPoolConfig{
			{PoolType: "premium", Enabled: true, TargetActiveThreads: 60, Segments: []string{""}},
		},
		Engagement: WarmupEngagementSettings{
			SpamRescueRate:    85,
			MarkImportantRate: 30,
			MarkReadRate:      95,
			StarRate:          20,
			// Heavy-tailed sample (see dwellSeconds): most reads land within
			// minutes, the tail waits up to an hour — "always read within 4
			// minutes, around the clock" was a lockstep signature.
			MinDwellSeconds: 45,
			MaxDwellSeconds: 3600,
		},
	}
}

// Normalize clamps settings into safe ranges so a bad admin payload can't
// produce nonsense (negative counts, percentages over 100, inverted dwell).
func (s *WarmupGenerationSettings) Normalize() {
	if s.CadenceHours < 1 {
		s.CadenceHours = 1
	}
	if s.Model == "" {
		s.Model = "gpt-4o-mini"
	}
	if s.MaxMessagesPerThread < 1 {
		s.MaxMessagesPerThread = 1
	}
	if s.MaxMessagesPerThread > 20 {
		s.MaxMessagesPerThread = 20
	}
	if s.DailyGenerationCap < 0 {
		s.DailyGenerationCap = 0
	}
	s.AISelectionShare = clampPct(s.AISelectionShare)
	s.Engagement.SpamRescueRate = clampPct(s.Engagement.SpamRescueRate)
	s.Engagement.MarkImportantRate = clampPct(s.Engagement.MarkImportantRate)
	s.Engagement.MarkReadRate = clampPct(s.Engagement.MarkReadRate)
	s.Engagement.StarRate = clampPct(s.Engagement.StarRate)
	if s.Engagement.MinDwellSeconds < 0 {
		s.Engagement.MinDwellSeconds = 0
	}
	if s.Engagement.MaxDwellSeconds < s.Engagement.MinDwellSeconds {
		s.Engagement.MaxDwellSeconds = s.Engagement.MinDwellSeconds
	}
	if s.Engagement.MaxDwellSeconds > 3600 {
		s.Engagement.MaxDwellSeconds = 3600
	}

	// Collapse to a single shared content library. Content isn't split by tier
	// (PickConversation ignores pool_type), so a multi-pool config would leave a
	// dead, never-topped-up branch and contradict the single-library admin UI.
	// Keep one entry under the canonical "premium" bucket, preferring an enabled
	// one from any legacy multi-pool doc.
	s.collapsePools()
}

func (s *WarmupGenerationSettings) collapsePools() {
	chosen := WarmupGenerationPoolConfig{PoolType: "premium", Enabled: true, TargetActiveThreads: 60, Segments: []string{""}}
	for _, p := range s.Pools {
		chosen = p
		if p.Enabled {
			break // prefer an enabled entry
		}
	}
	chosen.PoolType = "premium"
	if len(chosen.Segments) == 0 {
		chosen.Segments = []string{""}
	}
	s.Pools = []WarmupGenerationPoolConfig{chosen}
}

func clampPct(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// PoolConfig returns the config for a pool type, or false when absent/disabled.
func (s *WarmupGenerationSettings) PoolConfig(poolType string) (WarmupGenerationPoolConfig, bool) {
	for _, p := range s.Pools {
		if p.PoolType == poolType {
			return p, true
		}
	}
	return WarmupGenerationPoolConfig{}, false
}
