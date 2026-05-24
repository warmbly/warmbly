package models

import (
	"time"

	"github.com/google/uuid"
)

type BouncePipelineSettings struct {
	Enabled                     bool    `json:"enabled"`
	AutoSuppressOnBounce        bool    `json:"auto_suppress_on_bounce"`
	AutoSuppressOnComplaint     bool    `json:"auto_suppress_on_complaint"`
	AutoSuppressOnUnsubscribe   bool    `json:"auto_suppress_on_unsubscribe"`
	AutoPauseCampaignOnSpike    bool    `json:"auto_pause_campaign_on_spike"`
	PauseBounceRateThreshold    float64 `json:"pause_bounce_rate_threshold"`
	PauseComplaintRateThreshold float64 `json:"pause_complaint_rate_threshold"`
}

type TaskReliabilitySettings struct {
	Enabled                bool `json:"enabled"`
	DLQEnabled             bool `json:"dlq_enabled"`
	MaxAttempts            int  `json:"max_attempts"`
	ExecutionWindowSeconds int  `json:"execution_window_seconds"`
}

type ABTestingSettings struct {
	Enabled            bool   `json:"enabled"`
	DefaultWinningRule string `json:"default_winning_rule"`
	AutoPromoteWinner  bool   `json:"auto_promote_winner"`
	MinSampleSize      int    `json:"min_sample_size"`
}

type ReplyIntentSettings struct {
	Enabled                 bool     `json:"enabled"`
	PositiveKeywords        []string `json:"positive_keywords"`
	NegativeKeywords        []string `json:"negative_keywords"`
	OutOfOfficeKeywords     []string `json:"out_of_office_keywords"`
	QuestionKeywords        []string `json:"question_keywords"`
	AutoCreateCRMTask       bool     `json:"auto_create_crm_task"`
	AutoPauseOnNegative     bool     `json:"auto_pause_on_negative"`
	AutoSuppressOnUnsubWord bool     `json:"auto_suppress_on_unsubscribe_keyword"`
}

type SendTimeOptimizationSettings struct {
	Enabled                 bool    `json:"enabled"`
	UseContactTimezone      bool    `json:"use_contact_timezone"`
	DefaultContactTimezone  string  `json:"default_contact_timezone"`
	PreferredHours          []int   `json:"preferred_hours"`
	WeekendWeightMultiplier float64 `json:"weekend_weight_multiplier"`
}

type PreflightValidationSettings struct {
	Enabled                  bool `json:"enabled"`
	CheckTrackingDomain      bool `json:"check_tracking_domain"`
	CheckUnsubscribeHeader   bool `json:"check_unsubscribe_header"`
	CheckABVariantConfigured bool `json:"check_ab_variant_configured"`
	CheckDailyLimit          bool `json:"check_daily_limit"`
	CheckScheduleWindow      bool `json:"check_schedule_window"`
}

type DeliverabilityDashboardSettings struct {
	Enabled            bool `json:"enabled"`
	ShowSuppressionLog bool `json:"show_suppression_log"`
	ShowIntentSummary  bool `json:"show_intent_summary"`
	ShowDLQStats       bool `json:"show_dlq_stats"`
}

type AdvancedOutreachSettings struct {
	BouncePipeline       BouncePipelineSettings          `json:"bounce_pipeline"`
	TaskReliability      TaskReliabilitySettings         `json:"task_reliability"`
	ABTesting            ABTestingSettings               `json:"ab_testing"`
	ReplyIntent          ReplyIntentSettings             `json:"reply_intent"`
	SendTimeOptimization SendTimeOptimizationSettings    `json:"send_time_optimization"`
	Preflight            PreflightValidationSettings     `json:"preflight"`
	Dashboard            DeliverabilityDashboardSettings `json:"dashboard"`
	Custom               map[string]interface{}          `json:"custom,omitempty"`
}

type CampaignAdvancedSettings struct {
	CampaignID uuid.UUID                `json:"campaign_id"`
	Overrides  AdvancedOutreachSettings `json:"overrides"`
	UpdatedAt  time.Time                `json:"updated_at"`
}

type UpsertOutreachSettingsRequest struct {
	Settings AdvancedOutreachSettings `json:"settings"`
}

type DeliverabilityEventType string

const (
	DeliverabilityEventBounce      DeliverabilityEventType = "bounce"
	DeliverabilityEventComplaint   DeliverabilityEventType = "complaint"
	DeliverabilityEventUnsubscribe DeliverabilityEventType = "unsubscribe"
	DeliverabilityEventOpen        DeliverabilityEventType = "open"
	DeliverabilityEventClick       DeliverabilityEventType = "click"
	DeliverabilityEventReply       DeliverabilityEventType = "reply"
)

type DeliverabilityEvent struct {
	ID             uuid.UUID               `json:"id"`
	OrganizationID uuid.UUID               `json:"organization_id"`
	CampaignID     *uuid.UUID              `json:"campaign_id,omitempty"`
	TaskID         *uuid.UUID              `json:"task_id,omitempty"`
	ContactID      *uuid.UUID              `json:"contact_id,omitempty"`
	EventType      DeliverabilityEventType `json:"event_type"`
	Provider       string                  `json:"provider"`
	RecipientEmail string                  `json:"recipient_email"`
	Reason         string                  `json:"reason"`
	IdempotencyKey string                  `json:"idempotency_key"`
	Metadata       map[string]interface{}  `json:"metadata"`
	CreatedAt      time.Time               `json:"created_at"`
}

type IngestDeliverabilityEventRequest struct {
	CampaignID     *uuid.UUID              `json:"campaign_id,omitempty"`
	TaskID         *uuid.UUID              `json:"task_id,omitempty"`
	ContactID      *uuid.UUID              `json:"contact_id,omitempty"`
	EventType      DeliverabilityEventType `json:"event_type" binding:"required"`
	Provider       string                  `json:"provider,omitempty"`
	RecipientEmail string                  `json:"recipient_email" binding:"required"`
	Reason         string                  `json:"reason,omitempty"`
	IdempotencyKey string                  `json:"idempotency_key,omitempty"`
	Metadata       map[string]interface{}  `json:"metadata,omitempty"`
}

type SuppressedRecipient struct {
	ID             uuid.UUID               `json:"id"`
	OrganizationID uuid.UUID               `json:"organization_id"`
	Email          string                  `json:"email"`
	Reason         string                  `json:"reason"`
	Source         DeliverabilityEventType `json:"source"`
	CampaignID     *uuid.UUID              `json:"campaign_id,omitempty"`
	ExpiresAt      *time.Time              `json:"expires_at,omitempty"`
	Metadata       map[string]interface{}  `json:"metadata,omitempty"`
	CreatedAt      time.Time               `json:"created_at"`
	UpdatedAt      time.Time               `json:"updated_at"`
}

type CampaignABVariant struct {
	ID         uuid.UUID              `json:"id"`
	CampaignID uuid.UUID              `json:"campaign_id"`
	Name       string                 `json:"name"`
	Weight     int                    `json:"weight"`
	Subject    string                 `json:"subject"`
	BodyHTML   string                 `json:"body_html"`
	BodyPlain  string                 `json:"body_plain"`
	IsControl  bool                   `json:"is_control"`
	IsActive   bool                   `json:"is_active"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
}

type CreateCampaignABVariantRequest struct {
	Name      string                 `json:"name" binding:"required"`
	Weight    int                    `json:"weight"`
	Subject   string                 `json:"subject,omitempty"`
	BodyHTML  string                 `json:"body_html,omitempty"`
	BodyPlain string                 `json:"body_plain,omitempty"`
	IsControl bool                   `json:"is_control"`
	IsActive  *bool                  `json:"is_active,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type UpdateCampaignABVariantRequest struct {
	Name      *string                `json:"name,omitempty"`
	Weight    *int                   `json:"weight,omitempty"`
	Subject   *string                `json:"subject,omitempty"`
	BodyHTML  *string                `json:"body_html,omitempty"`
	BodyPlain *string                `json:"body_plain,omitempty"`
	IsControl *bool                  `json:"is_control,omitempty"`
	IsActive  *bool                  `json:"is_active,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

type VariantSelection struct {
	VariantID *uuid.UUID `json:"variant_id,omitempty"`
	Subject   string     `json:"subject"`
	BodyHTML  string     `json:"body_html"`
	BodyPlain string     `json:"body_plain"`
}

type TaskDeadLetter struct {
	ID          uuid.UUID              `json:"id"`
	TaskID      uuid.UUID              `json:"task_id"`
	TaskType    string                 `json:"task_type"`
	Payload     map[string]interface{} `json:"payload"`
	LastError   string                 `json:"last_error"`
	Attempts    int                    `json:"attempts"`
	MaxAttempts int                    `json:"max_attempts"`
	Status      string                 `json:"status"`
	NextRetryAt *time.Time             `json:"next_retry_at,omitempty"`
	ReplayedAt  *time.Time             `json:"replayed_at,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

type ReplyIntentType string

const (
	ReplyIntentPositive    ReplyIntentType = "positive"
	ReplyIntentNegative    ReplyIntentType = "negative"
	ReplyIntentOutOfOffice ReplyIntentType = "out_of_office"
	ReplyIntentQuestion    ReplyIntentType = "question"
	ReplyIntentNeutral     ReplyIntentType = "neutral"
)

type ReplyIntentRecord struct {
	ID             uuid.UUID              `json:"id"`
	OrganizationID uuid.UUID              `json:"organization_id"`
	ContactEmail   string                 `json:"contact_email"`
	CampaignID     *uuid.UUID             `json:"campaign_id,omitempty"`
	TaskID         *uuid.UUID             `json:"task_id,omitempty"`
	Intent         ReplyIntentType        `json:"intent"`
	Confidence     float64                `json:"confidence"`
	ActionTaken    string                 `json:"action_taken"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt      time.Time              `json:"created_at"`
}

type PreflightCheckResult struct {
	Key         string `json:"key"`
	Passed      bool   `json:"passed"`
	Severity    string `json:"severity"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

type PreflightReport struct {
	ID              uuid.UUID              `json:"id"`
	OrganizationID  uuid.UUID              `json:"organization_id"`
	CampaignID      uuid.UUID              `json:"campaign_id"`
	Passed          bool                   `json:"passed"`
	Score           int                    `json:"score"`
	Checks          []PreflightCheckResult `json:"checks"`
	Recommendations []string               `json:"recommendations"`
	CreatedAt       time.Time              `json:"created_at"`
}

type DeliverabilityDashboard struct {
	From                 time.Time `json:"from"`
	To                   time.Time `json:"to"`
	EventsTotal          int       `json:"events_total"`
	BounceCount          int       `json:"bounce_count"`
	ComplaintCount       int       `json:"complaint_count"`
	UnsubscribeCount     int       `json:"unsubscribe_count"`
	ReplyCount           int       `json:"reply_count"`
	OpenCount            int       `json:"open_count"`
	ClickCount           int       `json:"click_count"`
	SuppressedRecipients int       `json:"suppressed_recipients"`
	DLQPending           int       `json:"dlq_pending"`
	IntentPositive       int       `json:"intent_positive"`
	IntentNegative       int       `json:"intent_negative"`
	IntentOOO            int       `json:"intent_out_of_office"`
	IntentQuestion       int       `json:"intent_question"`
	IntentNeutral        int       `json:"intent_neutral"`
}

type ABVariantStats struct {
	VariantID   uuid.UUID `json:"variant_id"`
	VariantName string    `json:"variant_name"`
	TotalSent   int       `json:"total_sent"`
	Opened      int       `json:"opened"`
	Clicked     int       `json:"clicked"`
	Replied     int       `json:"replied"`
	Bounced     int       `json:"bounced"`
	OpenRate    float64   `json:"open_rate"`
	ClickRate   float64   `json:"click_rate"`
	ReplyRate   float64   `json:"reply_rate"`
	BounceRate  float64   `json:"bounce_rate"`
}

type ABWinnerAnalysis struct {
	CampaignID  uuid.UUID        `json:"campaign_id"`
	Variants    []ABVariantStats `json:"variants"`
	WinnerID    *uuid.UUID       `json:"winner_id,omitempty"`
	WinnerName  string           `json:"winner_name,omitempty"`
	WinningRule string           `json:"winning_rule"`
	Confidence  string           `json:"confidence"`
}

func DefaultAdvancedOutreachSettings() AdvancedOutreachSettings {
	return AdvancedOutreachSettings{
		BouncePipeline: BouncePipelineSettings{
			Enabled:                     true,
			AutoSuppressOnBounce:        true,
			AutoSuppressOnComplaint:     true,
			AutoSuppressOnUnsubscribe:   true,
			AutoPauseCampaignOnSpike:    true,
			PauseBounceRateThreshold:    8,
			PauseComplaintRateThreshold: 1.5,
		},
		TaskReliability: TaskReliabilitySettings{
			Enabled:                true,
			DLQEnabled:             true,
			MaxAttempts:            5,
			ExecutionWindowSeconds: 300,
		},
		ABTesting: ABTestingSettings{
			Enabled:            true,
			DefaultWinningRule: "reply_rate",
			AutoPromoteWinner:  false,
			MinSampleSize:      30,
		},
		ReplyIntent: ReplyIntentSettings{
			Enabled:                 true,
			PositiveKeywords:        []string{"interested", "sounds good", "let's talk", "book", "demo", "pricing"},
			NegativeKeywords:        []string{"not interested", "unsubscribe", "remove me", "stop", "no thanks"},
			OutOfOfficeKeywords:     []string{"out of office", "ooo", "vacation", "automatic reply"},
			QuestionKeywords:        []string{"?", "how", "what", "when", "price"},
			AutoCreateCRMTask:       true,
			AutoPauseOnNegative:     false,
			AutoSuppressOnUnsubWord: true,
		},
		SendTimeOptimization: SendTimeOptimizationSettings{
			Enabled:                 true,
			UseContactTimezone:      true,
			DefaultContactTimezone:  "UTC",
			PreferredHours:          []int{9, 10, 11, 14, 15, 16},
			WeekendWeightMultiplier: 0.5,
		},
		Preflight: PreflightValidationSettings{
			Enabled:                  true,
			CheckTrackingDomain:      true,
			CheckUnsubscribeHeader:   true,
			CheckABVariantConfigured: false,
			CheckDailyLimit:          true,
			CheckScheduleWindow:      true,
		},
		Dashboard: DeliverabilityDashboardSettings{
			Enabled:            true,
			ShowSuppressionLog: true,
			ShowIntentSummary:  true,
			ShowDLQStats:       true,
		},
		Custom: map[string]interface{}{},
	}
}
