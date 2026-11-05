package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Content sources for warmup message bodies. "static" is the in-code library;
// "ai" is a thread drawn from the offline-generated warmup_conversations bank;
// "template" is an organization-supplied subject + opening; "campaign" is the
// opening step of one of the sender's running campaigns, rendered as it sends.
const (
	WarmupContentSourceStatic   = "static"
	WarmupContentSourceAI       = "ai"
	WarmupContentSourceTemplate = "template"
	WarmupContentSourceCampaign = "campaign"
)

// WarmupOwnCopyMinDays is how long a mailbox warms on ordinary conversations
// before any of its workspace's own copy goes through it.
const WarmupOwnCopyMinDays = 7

// AdminSettingsKeyWarmupGeneration is the admin_settings key under which the
// warmup generation + engagement config document is stored.
const AdminSettingsKeyWarmupGeneration = "warmup_generation"

// WarmupConversation is a cached conversation thread used as warmup content.
// Messages are immutable ordered reply turns; Description is the opening body.
type WarmupConversation struct {
	ID               uuid.UUID  `json:"id"`
	PoolType         string     `json:"pool_type"`
	Segment          string     `json:"segment"`
	Source           string     `json:"source"`
	Theme            string     `json:"theme"`
	Subject          string     `json:"subject"`
	Description      string     `json:"description"`
	Messages         []string   `json:"messages"`
	Status           string     `json:"status"`
	LintPassed       bool       `json:"lint_passed"`
	ReplyEligible    bool       `json:"reply_eligible"`
	UsageCount       int64      `json:"usage_count"`
	GeneratedByJob   *uuid.UUID `json:"generated_by_job_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	ScenarioVersion  *string    `json:"scenario_version,omitempty"`
	RenderingVersion *string    `json:"rendering_version,omitempty"`
	SemanticReview   string     `json:"semantic_review"`
	// OrganizationID is set only on source="template" rows: the workspace that
	// submitted the template. Shared library rows keep it NULL.
	OrganizationID *uuid.UUID `json:"organization_id,omitempty"`
}

// WarmupTemplateSettings is how much of one organization's warmup carries its
// own copy: its templates and, with CampaignCopy, the opening steps of the
// campaigns each sender is running. TemplateShare is the percentage of new
// openings drawn from that copy when some is eligible; TemplatesOnly sends
// nothing else, and an opening with no eligible copy is skipped.
type WarmupTemplateSettings struct {
	TemplateShare int  `json:"template_share"`
	TemplatesOnly bool `json:"templates_only"`
	CampaignCopy  bool `json:"campaign_copy"`
}

// DefaultWarmupTemplateSettings mirrors the mix the warmup pool applies when
// an organization never changed the setting.
func DefaultWarmupTemplateSettings() WarmupTemplateSettings {
	return WarmupTemplateSettings{TemplateShare: 25, CampaignCopy: true}
}

// WarmupCampaignCopyCandidate is one arm of an opening campaign step that a
// sender's warmup may carry: the step's own copy (VariantID nil) or one of
// its active A/B variants, as written, before rendering.
type WarmupCampaignCopyCandidate struct {
	CampaignID uuid.UUID
	SequenceID uuid.UUID
	VariantID  *uuid.UUID
	Subject    string
	BodyHTML   string
	BodyPlain  string
	// LastSent is when this sender last carried this arm in warmup.
	LastSent *time.Time
}

// WarmupCampaignCopyStep is one arm of an opening step of the workspace's
// campaigns as the copy endpoint reports it: what it is, whether warmup can
// carry it, and where its warmup sends landed.
type WarmupCampaignCopyStep struct {
	CampaignID     uuid.UUID  `json:"campaign_id"`
	CampaignName   string     `json:"campaign_name"`
	CampaignStatus string     `json:"campaign_status"`
	SequenceID     uuid.UUID  `json:"sequence_id"`
	StepName       string     `json:"step_name"`
	Position       int        `json:"position"`
	VariantID      *uuid.UUID `json:"variant_id"`
	VariantName    string     `json:"variant_name"`
	Subject        string     `json:"subject"`
	// Eligible is false when warmup cannot carry this copy faithfully;
	// IneligibleReason says why in a sentence.
	Eligible         bool   `json:"eligible"`
	IneligibleReason string `json:"ineligible_reason,omitempty"`
	// Placement counts recipients at Google, Microsoft and Yahoo only; ByGroup has every group.
	Placement WarmupTemplatePlacement        `json:"placement"`
	ByGroup   []WarmupTemplateGroupPlacement `json:"by_group"`
	BodyHTML  string                         `json:"-"`
	BodyPlain string                         `json:"-"`
}

// WarmupTemplatePlacement counts where one template's sends landed, as the
// recipient mailboxes reported them. Sent counts every dispatched token;
// the landing counters cover the receipts that came back.
type WarmupTemplatePlacement struct {
	Sent int `json:"sent"`
	// Reported counts the sends whose recipient filed a receipt; landing rates are read over it.
	Reported int `json:"reported"`
	Inbox    int `json:"inbox"`
	Tabs     int `json:"tabs"`
	Spam     int `json:"spam"`
	Rescued  int `json:"rescued"`
}

// WarmupTemplateGroupPlacement is WarmupTemplatePlacement for one recipient
// provider group (google, microsoft, yahoo, other).
type WarmupTemplateGroupPlacement struct {
	Group string `json:"group"`
	WarmupTemplatePlacement
}

// Warmup generation job modes. "sync" runs every model call inline (the
// original behaviour); "batch" submits one OpenAI Batch API job and ingests its
// results asynchronously when the batch completes.
const (
	WarmupGenerationModeSync   = "sync"
	WarmupGenerationModeBatch  = "batch"
	WarmupConversationActive   = "active"
	WarmupConversationArchived = "archived"
)

// WarmupGenerationJob records one offline generation run for observability.
// Batch runs additionally carry the OpenAI batch/file identifiers and the
// last-observed batch status so the poller can reconcile them.
type WarmupGenerationJob struct {
	ContentVersion       *string    `json:"content_version,omitempty"`
	MaxMessagesPerThread *int       `json:"max_messages_per_thread,omitempty"`
	ID                   uuid.UUID  `json:"id"`
	RequestedBy          *uuid.UUID `json:"requested_by,omitempty"`
	Trigger              string     `json:"trigger"` // "schedule"; older rows may be "manual"
	Mode                 string     `json:"mode"`    // "sync" | "batch"
	PoolType             string     `json:"pool_type"`
	Segment              string     `json:"segment"`
	Theme                string     `json:"theme"`
	Model                string     `json:"model"`
	RequestedCount       int        `json:"requested_count"`
	GeneratedCount       int        `json:"generated_count"`
	LintRejectedCount    int        `json:"lint_rejected_count"`
	FailedCount          int        `json:"failed_count"`
	Status               string     `json:"status"` // pending | running | completed | failed
	Error                string     `json:"error"`
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

// WarmupGenerationSettings is the persisted controller policy for the offline
// AI thread bank and recipient engagement. Stored as JSON in
// admin_settings under AdminSettingsKeyWarmupGeneration.
type WarmupGenerationSettings struct {
	// GenerationEnabled stops new jobs, independently of cached-content use.
	GenerationEnabled bool `json:"generation_enabled"`
	// Enabled controls fresh-content selection; disabled selection waits without sending.
	Enabled bool `json:"enabled"`
	// ScheduleEnabled runs the background generation job on CadenceHours.
	ScheduleEnabled bool `json:"schedule_enabled"`
	CadenceHours    int  `json:"cadence_hours"`
	// RefreshEnabled replaces unused stock even when the supply target is met.
	RefreshEnabled bool `json:"refresh_enabled"`
	// RefreshPerRun is how many threads are recycled per scheduled run.
	RefreshPerRun        int                          `json:"refresh_per_run"`
	Model                string                       `json:"model"`
	MaxMessagesPerThread int                          `json:"max_messages_per_thread"`
	DailyGenerationCap   int                          `json:"daily_generation_cap"`
	AISelectionShare     int                          `json:"ai_selection_share"` // Retained for settings rollback; new openings always use fresh content.
	Pools                []WarmupGenerationPoolConfig `json:"pools"`
	Engagement           WarmupEngagementSettings     `json:"engagement"`
}

// DefaultWarmupGenerationSettings maintains one shared bank of approved plans reused across distinct mailboxes.
func DefaultWarmupGenerationSettings() WarmupGenerationSettings {
	return WarmupGenerationSettings{
		GenerationEnabled:    true,
		Enabled:              true,
		ScheduleEnabled:      true,
		CadenceHours:         6,
		RefreshEnabled:       false,
		RefreshPerRun:        10,
		Model:                "gpt-6-luna",
		MaxMessagesPerThread: 5,
		DailyGenerationCap:   1000,
		AISelectionShare:     100,
		Pools: []WarmupGenerationPoolConfig{
			{PoolType: "premium", Enabled: true, TargetActiveThreads: 200, Segments: []string{""}},
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
	s.CadenceHours = max(1, min(168, s.CadenceHours))
	s.RefreshPerRun = max(0, min(25, s.RefreshPerRun))
	s.DailyGenerationCap = max(0, s.DailyGenerationCap)
	s.AISelectionShare = clampPct(s.AISelectionShare)
	s.Model = strings.TrimSpace(s.Model)
	if s.Model == "" {
		s.Model = DefaultWarmupGenerationSettings().Model
	}
	s.MaxMessagesPerThread = max(1, min(5, s.MaxMessagesPerThread))
	s.Engagement.SpamRescueRate = clampPct(s.Engagement.SpamRescueRate)
	s.Engagement.MarkImportantRate = clampPct(s.Engagement.MarkImportantRate)
	s.Engagement.MarkReadRate = clampPct(s.Engagement.MarkReadRate)
	s.Engagement.StarRate = clampPct(s.Engagement.StarRate)
	s.Engagement.MinDwellSeconds = max(0, min(3600, s.Engagement.MinDwellSeconds))
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
	chosen := WarmupGenerationPoolConfig{PoolType: "premium", Enabled: true, TargetActiveThreads: 200, Segments: []string{""}}
	for _, p := range s.Pools {
		chosen = p
		if p.Enabled {
			break // prefer an enabled entry
		}
	}
	chosen.PoolType = "premium"
	chosen.TargetActiveThreads = max(1, min(5000, chosen.TargetActiveThreads))
	segments := make([]string, 0, len(chosen.Segments))
	seen := map[string]bool{}
	for _, segment := range chosen.Segments {
		segment = strings.TrimSpace(segment)
		if !seen[segment] {
			segments = append(segments, segment)
			seen[segment] = true
		}
	}
	if len(segments) == 0 {
		segments = []string{""}
	}
	chosen.Segments = segments
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

// MajorProviderPlacement totals the google, microsoft and yahoo groups. Those
// filter on sender reputation; a small host's spam folder is its own filter's
// verdict, so it never counts toward a copy's headline.
func MajorProviderPlacement(groups []WarmupTemplateGroupPlacement) WarmupTemplatePlacement {
	var t WarmupTemplatePlacement
	for _, g := range groups {
		if g.Group == "other" {
			continue
		}
		t.Sent += g.Sent
		t.Reported += g.Reported
		t.Inbox += g.Inbox
		t.Tabs += g.Tabs
		t.Spam += g.Spam
		t.Rescued += g.Rescued
	}
	return t
}
