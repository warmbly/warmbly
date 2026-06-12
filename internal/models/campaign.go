package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// TimeInterval is a sending window within a single day, expressed in minutes
// since local midnight. End is exclusive-ish and must be > Start, <= 1440.
type TimeInterval struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// ScheduleWindows is a campaign's per-day sending schedule, indexed by
// time.Weekday (0=Sunday .. 6=Saturday). A nil/empty day means "no sending that
// day". When non-empty it is the authoritative schedule and supersedes the
// legacy Days/StartTime/EndTime fields. Persisted as a jsonb array-of-7.
type ScheduleWindows [7][]TimeInterval

// IsEmpty reports whether no day carries any interval (treated as "unset").
func (w ScheduleWindows) IsEmpty() bool {
	for _, day := range w {
		if len(day) > 0 {
			return false
		}
	}
	return true
}

// DaySpan returns the earliest start and latest end across a weekday's
// intervals (ok=false when that day has none). Used for even send distribution.
func (w ScheduleWindows) DaySpan(weekday int) (start, end int, ok bool) {
	if weekday < 0 || weekday > 6 || len(w[weekday]) == 0 {
		return 0, 0, false
	}
	start, end = w[weekday][0].Start, w[weekday][0].End
	for _, iv := range w[weekday][1:] {
		if iv.Start < start {
			start = iv.Start
		}
		if iv.End > end {
			end = iv.End
		}
	}
	return start, end, true
}

// Value implements driver.Valuer — marshals to jsonb, or NULL when empty (so an
// empty schedule reverts to the legacy day/time derivation).
func (w ScheduleWindows) Value() (driver.Value, error) {
	if w.IsEmpty() {
		return nil, nil
	}
	return json.Marshal(w)
}

// Scan implements sql.Scanner — reads the jsonb column (NULL → empty).
func (w *ScheduleWindows) Scan(src any) error {
	if src == nil {
		*w = ScheduleWindows{}
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("ScheduleWindows: unsupported scan type %T", src)
	}
	if len(b) == 0 {
		*w = ScheduleWindows{}
		return nil
	}
	var parsed ScheduleWindows
	if err := json.Unmarshal(b, &parsed); err != nil {
		return err
	}
	*w = parsed
	return nil
}

type Campaign struct {
	ID             uuid.UUID  `json:"id"`
	UserID         string     `json:"user_id"`
	OrganizationID *uuid.UUID `json:"organization_id,omitempty"`

	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`

	StopOnReply       bool `json:"stop_on_reply"`
	OpenTracking      bool `json:"open_tracking"`
	LinkTracking      bool `json:"link_tracking"`
	TextOnly          bool `json:"text_only"`
	DailyLimit        int  `json:"daily_limit"`
	UnsubscribeHeader bool `json:"unsubscribe_header"`
	RiskyEmails       bool `json:"risky_emails"`

	CC  []string `json:"cc"`
	BCC []string `json:"bcc"`

	StartDate *time.Time `json:"start_date"`
	EndDate   *time.Time `json:"end_date"`
	Timezone  string     `json:"timezone"`
	Days      uint8      `json:"days"`
	StartTime string     `json:"start_time"`
	EndTime   string     `json:"end_time"`

	// ScheduleWindows, when non-empty, is the authoritative per-day sending
	// schedule (supersedes Days/StartTime/EndTime). Indexed by time.Weekday.
	ScheduleWindows ScheduleWindows `json:"schedule_windows"`

	EmailTags []string `json:"email_tags"`
	Folders   []string `json:"folders"`

	ContactOrderBy    string  `json:"contact_order_by"`
	ContactOrderDir   string  `json:"contact_order_dir"`
	ContactOrderField *string `json:"contact_order_field,omitempty"`

	// Sending-account selection. SenderStrategy is "tags" (default — accounts
	// resolved from EmailTags) or "explicit" (the campaign_senders list).
	// RotationMode picks how volume spreads across the chosen mailboxes.
	SenderStrategy string           `json:"sender_strategy"`
	RotationMode   string           `json:"rotation_mode"`
	Senders        []CampaignSender `json:"senders,omitempty"` // loaded on demand, not in the base SELECT

	// Per-campaign daily ramp-up. Applied only via min() against the per-mailbox
	// cap, so it can never raise volume above the cold cap. RampLevel/RampLevelDate
	// are server-managed (persisted across pause/resume).
	RampEnabled   bool       `json:"ramp_enabled"`
	RampStart     int        `json:"ramp_start"`
	RampIncrement int        `json:"ramp_increment"`
	RampCeiling   int        `json:"ramp_ceiling"`
	RampLevel     int        `json:"ramp_level"`
	RampLevelDate *time.Time `json:"ramp_level_date,omitempty"`

	// ESP/provider matching: off | prefer | strict.
	ESPMatchMode string `json:"esp_match_mode"`

	// New-lead throttle. MaxNewLeadsPerDay 0 = unlimited (current behavior).
	MaxNewLeadsPerDay  int  `json:"max_new_leads_per_day"`
	PrioritizeNewLeads bool `json:"prioritize_new_leads"`

	// Campaign-scoped tracking-domain override. Honored only when verified;
	// otherwise falls back to the mailbox/default domain.
	TrackingDomain           string     `json:"tracking_domain"`
	TrackingDomainVerified   bool       `json:"tracking_domain_verified"`
	TrackingDomainVerifiedAt *time.Time `json:"tracking_domain_verified_at,omitempty"`

	LastStatusChangeAt *time.Time `json:"last_status_change_at,omitempty"`

	UpdatedAt time.Time `json:"updated_at"`
	CreatedAt time.Time `json:"created_at"`
}

// CampaignSender is one mailbox in an explicit-strategy campaign's sender pool.
type CampaignSender struct {
	EmailAccountID uuid.UUID  `json:"email_account_id"`
	Weight         int        `json:"weight"`
	LastSentAt     *time.Time `json:"last_sent_at,omitempty"`
	Enabled        bool       `json:"enabled"`
}

// CampaignSenderInput is the write shape for PUT /campaigns/:id/senders.
type CampaignSenderInput struct {
	EmailAccountID uuid.UUID `json:"email_account_id"`
	Weight         *int      `json:"weight,omitempty"`
	Enabled        *bool     `json:"enabled,omitempty"`
}

type MiniCampaign struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type CampaignsResult struct {
	Data       []Campaign `json:"data"`
	Pagination Pagination `json:"pagination"`
}

type UpdateCampaign struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Status      *string `json:"status,omitempty"`

	StopOnReply       *bool `json:"stop_on_reply"`
	OpenTracking      *bool `json:"open_tracking"`
	LinkTracking      *bool `json:"link_tracking"`
	TextOnly          *bool `json:"text_only"`
	DailyLimit        *int  `json:"daily_limit"`
	UnsubscribeHeader *bool `json:"unsubscribe_header"`
	RiskyEmails       *bool `json:"risky_emails"`

	CC  []string `json:"cc"`
	BCC []string `json:"bcc"`

	StartDate *time.Time `json:"start_date"`
	EndDate   *time.Time `json:"end_date"`
	Timezone  *string    `json:"timezone"`
	Days      *uint8     `json:"days"`
	StartTime *string    `json:"start_time"`
	EndTime   *string    `json:"end_time"`

	// Authoritative per-day schedule. When sent, supersedes Days/StartTime/EndTime.
	ScheduleWindows *ScheduleWindows `json:"schedule_windows,omitempty"`

	EmailTags []string `json:"email_tags"`
	Folders   []string `json:"folders"`

	ContactOrderBy    *string `json:"contact_order_by"`
	ContactOrderDir   *string `json:"contact_order_dir"`
	ContactOrderField *string `json:"contact_order_field"`

	// Net-new send controls. The explicit sender LIST is edited via
	// PUT /campaigns/:id/senders; only the strategy/mode toggles ride PATCH.
	SenderStrategy *string `json:"sender_strategy,omitempty"`
	RotationMode   *string `json:"rotation_mode,omitempty"`

	RampEnabled   *bool `json:"ramp_enabled,omitempty"`
	RampStart     *int  `json:"ramp_start,omitempty"`
	RampIncrement *int  `json:"ramp_increment,omitempty"`
	RampCeiling   *int  `json:"ramp_ceiling,omitempty"`

	ESPMatchMode       *string `json:"esp_match_mode,omitempty"`
	MaxNewLeadsPerDay  *int    `json:"max_new_leads_per_day,omitempty"`
	PrioritizeNewLeads *bool   `json:"prioritize_new_leads,omitempty"`
	TrackingDomain     *string `json:"tracking_domain,omitempty"`
}

// CreateCampaign is the payload accepted by POST /campaigns. Name is required;
// every other field is optional and only applied if the caller sent a non-nil
// value. The wizard sends everything at once; the simple modal can still send
// just {name, description} and get sane defaults.
type CreateCampaign struct {
	Name        string `json:"name"`
	Description string `json:"description"`

	// Sending rules / tracking
	StopOnReply       *bool `json:"stop_on_reply,omitempty"`
	OpenTracking      *bool `json:"open_tracking,omitempty"`
	LinkTracking      *bool `json:"link_tracking,omitempty"`
	TextOnly          *bool `json:"text_only,omitempty"`
	DailyLimit        *int  `json:"daily_limit,omitempty"`
	UnsubscribeHeader *bool `json:"unsubscribe_header,omitempty"`
	RiskyEmails       *bool `json:"risky_emails,omitempty"`

	CC  []string `json:"cc,omitempty"`
	BCC []string `json:"bcc,omitempty"`

	// Schedule
	StartDate *time.Time `json:"start_date,omitempty"`
	EndDate   *time.Time `json:"end_date,omitempty"`
	Timezone  *string    `json:"timezone,omitempty"`
	Days      *uint8     `json:"days,omitempty"`
	StartTime *string    `json:"start_time,omitempty"`
	EndTime   *string    `json:"end_time,omitempty"`

	// Sender pool — accepts UUIDs already created by the user.
	EmailTagIDs []string `json:"email_tag_ids,omitempty"`
	FolderIDs   []string `json:"folder_ids,omitempty"`

	// Sending-account selection + rotation (net-new). When sender_strategy is
	// "explicit", Senders is the mailbox pool; otherwise EmailTagIDs are used.
	SenderStrategy *string               `json:"sender_strategy,omitempty"`
	RotationMode   *string               `json:"rotation_mode,omitempty"`
	Senders        []CampaignSenderInput `json:"senders,omitempty"`

	// Per-campaign daily ramp-up (net-new). ramp_level is server-owned.
	RampEnabled   *bool `json:"ramp_enabled,omitempty"`
	RampStart     *int  `json:"ramp_start,omitempty"`
	RampIncrement *int  `json:"ramp_increment,omitempty"`
	RampCeiling   *int  `json:"ramp_ceiling,omitempty"`

	// ESP/provider matching + new-lead throttle + tracking-domain override.
	ESPMatchMode       *string `json:"esp_match_mode,omitempty"`
	MaxNewLeadsPerDay  *int    `json:"max_new_leads_per_day,omitempty"`
	PrioritizeNewLeads *bool   `json:"prioritize_new_leads,omitempty"`
	TrackingDomain     *string `json:"tracking_domain,omitempty"`

	// Initial sequences (in order) — caller can also create them after.
	Sequences []CreateSequenceInput `json:"steps,omitempty"`

	// A/B variants for the first sequence — useful for "create + test" in one shot.
	Variants []CreateCampaignABVariantRequest `json:"variants,omitempty"`

	// Advanced overrides (bounce/intent/dashboard/etc) — see AdvancedOutreachSettings.
	AdvancedOverrides *AdvancedOutreachSettings `json:"advanced_overrides,omitempty"`
}

// CreateSequenceInput is one step in a sequence. Used during initial campaign
// creation; matches UpdateSequence shape so the wizard can reuse the editor.
type CreateSequenceInput struct {
	Name      string `json:"name"`
	Subject   string `json:"subject"`
	BodyPlain string `json:"body_plain"`
	BodyHTML  string `json:"body_html"`
	BodySync  *bool  `json:"body_sync,omitempty"`
	BodyCode  *bool  `json:"body_code,omitempty"`
	WaitAfter *int   `json:"wait_after,omitempty"`
}
