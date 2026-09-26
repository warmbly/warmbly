package models

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
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
	Enabled             bool     `json:"enabled"`
	PositiveKeywords    []string `json:"positive_keywords"`
	NegativeKeywords    []string `json:"negative_keywords"`
	OutOfOfficeKeywords []string `json:"out_of_office_keywords"`
	QuestionKeywords    []string `json:"question_keywords"`
	AutoCreateCRMTask   bool     `json:"auto_create_crm_task"`
	// CRMTaskIntents narrows the switch above to the intents worth a
	// follow-up. Absent means DefaultCRMTaskIntents (human replies only); an
	// explicit empty list means none, same as turning the switch off.
	CRMTaskIntents          []ReplyIntentType `json:"crm_task_intents"`
	AutoPauseOnNegative     bool              `json:"auto_pause_on_negative"`
	AutoSuppressOnUnsubWord bool              `json:"auto_suppress_on_unsubscribe_keyword"`
	// HoldOnOutOfOffice parks the contact's next step when an auto-reply says
	// they are away, and resumes it when they are back. Without it the
	// follow-up goes out on schedule to an empty desk and the sequence is over
	// before the person reads any of it (issue #470).
	HoldOnOutOfOffice bool `json:"hold_on_out_of_office"`
	// OutOfOfficeHoldDays is the hold applied when the auto-reply carries no
	// return date we can read. Clamped to OOOHoldDaysMin..OOOHoldDaysMax.
	OutOfOfficeHoldDays int `json:"out_of_office_hold_days"`
}

// InboxTaggingSettings are the actions a workspace lets a classified reply
// take. The three reversible ones default on; the suppression defaults off,
// because it is the one the system cannot undo.
type InboxTaggingSettings struct {
	// HoldOnNotNow parks the contact's sequences when they answer "not now",
	// for NotNowHoldDays, so the follow-up lands after the timing they named
	// rather than three days later.
	HoldOnNotNow   bool `json:"hold_on_not_now"`
	NotNowHoldDays int  `json:"not_now_hold_days"`
	// StopOnDeclined parks a contact with no end when they decline or say they
	// are the wrong person. The hold is visible on the lead and a member lifts
	// it; nothing is unsubscribed and nothing is deleted.
	StopOnDeclined bool `json:"stop_on_declined"`
	// TaskOnCallRequest opens a CRM task for the mailbox owner when a reply
	// asks for a call or proposes a time.
	TaskOnCallRequest bool `json:"task_on_call_request"`
	// SuppressOnRemovalRequest adds the sender to the suppression list when
	// the reply asks to be removed and the classifier is strongly sure of it.
	// Phase 3: the one irreversible action, and the one with the highest floor.
	SuppressOnRemovalRequest bool `json:"suppress_on_removal_request"`
	// Questions are the workspace's own, asked alongside the built-in set in
	// the same call.
	Questions []InboxTagQuestion `json:"questions"`
	// Languages are the languages the workspace's mail is written in, as
	// MailLanguageNames codes. Tagging reads each with its vocabulary and names
	// it to the classifier; empty uses the default set only.
	Languages []string `json:"languages"`
}

// InboxTagQuestion is one workspace-defined tagging question. A yes/no
// question applies Label on yes; a choice question applies the label of the
// option it picked.
type InboxTagQuestion struct {
	// ID keys the answer in stored verdicts. Minted on save when empty.
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Question string                 `json:"question"`
	Label    string                 `json:"label,omitempty"`
	Action   InboxTagQuestionAction `json:"action"`
	Choices  []InboxTagChoice       `json:"choices,omitempty"`
}

// InboxTagChoice is one option of a choice question.
type InboxTagChoice struct {
	Label       string                 `json:"label"`
	Description string                 `json:"description"`
	Action      InboxTagQuestionAction `json:"action"`
}

// InboxTagQuestionAction is what a matching reply may do, drawn from the same
// primitives as the built-in switches.
type InboxTagQuestionAction struct {
	Type     string `json:"type"`
	HoldDays int    `json:"hold_days,omitempty"`
}

const (
	InboxTagQuestionYesNo  = "yes_no"
	InboxTagQuestionChoice = "choice"

	InboxTagActionNone = ""
	InboxTagActionHold = "hold"
	InboxTagActionStop = "stop"
	InboxTagActionTask = "task"
)

// Bounds on workspace questions. Every question adds input tokens to every
// classified message, so the count is capped.
const (
	InboxTagQuestionsMax        = 10
	InboxTagQuestionMaxLen      = 300
	InboxTagChoiceDescMaxLen    = 200
	InboxTagLabelMaxLen         = 40
	InboxTagChoicesMin          = 2
	InboxTagChoicesMax          = 8
	InboxTagQuestionHoldMin     = 1
	InboxTagQuestionHoldMax     = 365
	InboxTagQuestionHoldDefault = 30
)

// MailLanguageNames are the tagging languages a workspace may choose, by code.
// Choosing one adds its vocabulary to tagging's offline reading where
// replyclassify has any, and names it to the classifier.
var MailLanguageNames = map[string]string{
	"ar":  "Arabic",
	"bg":  "Bulgarian",
	"bn":  "Bengali",
	"ca":  "Catalan",
	"cs":  "Czech",
	"da":  "Danish",
	"de":  "German",
	"el":  "Greek",
	"en":  "English",
	"es":  "Spanish",
	"et":  "Estonian",
	"fa":  "Persian",
	"fi":  "Finnish",
	"fil": "Filipino",
	"fr":  "French",
	"he":  "Hebrew",
	"hi":  "Hindi",
	"hr":  "Croatian",
	"hu":  "Hungarian",
	"id":  "Indonesian",
	"it":  "Italian",
	"ja":  "Japanese",
	"ko":  "Korean",
	"lt":  "Lithuanian",
	"lv":  "Latvian",
	"ms":  "Malay",
	"nb":  "Norwegian",
	"nl":  "Dutch",
	"pl":  "Polish",
	"pt":  "Portuguese",
	"ro":  "Romanian",
	"ru":  "Russian",
	"sk":  "Slovak",
	"sl":  "Slovenian",
	"sr":  "Serbian",
	"sv":  "Swedish",
	"sw":  "Swahili",
	"ta":  "Tamil",
	"th":  "Thai",
	"tr":  "Turkish",
	"uk":  "Ukrainian",
	"ur":  "Urdu",
	"vi":  "Vietnamese",
	"zh":  "Chinese",
}

var inboxTagIDPattern = regexp.MustCompile(`^[a-z0-9]{1,16}$`)

// InboxTagLabelName is a label name as it is filed: plain words like the
// built-in labels ("Later maybe"), letters and digits in any script with
// single spaces or hyphens between them, anything else dropped. NFC first, so
// "später" keeps its letter rather than a combining mark.
func InboxTagLabelName(name string) string {
	var b strings.Builder
	pending := rune(0)
	for _, r := range norm.NFC.String(name) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if pending != 0 && b.Len() > 0 {
				b.WriteRune(pending)
			}
			pending = 0
			b.WriteRune(r)
		case r == '-' && pending != ' ':
			pending = '-'
		default:
			pending = ' '
		}
	}
	return b.String()
}

func (a *InboxTagQuestionAction) normalize() {
	a.Type = strings.ToLower(strings.TrimSpace(a.Type))
	if a.Type != InboxTagActionHold {
		a.HoldDays = 0
		return
	}
	if a.HoldDays == 0 {
		a.HoldDays = InboxTagQuestionHoldDefault
	}
	a.HoldDays = min(max(a.HoldDays, InboxTagQuestionHoldMin), InboxTagQuestionHoldMax)
}

func (a InboxTagQuestionAction) validate() error {
	switch a.Type {
	case InboxTagActionNone, InboxTagActionHold, InboxTagActionStop, InboxTagActionTask:
		return nil
	}
	return fmt.Errorf("%q is not a tagging action; use hold, stop or task", a.Type)
}

// normalizeInboxTagQuestions trims and clamps what can be clamped and mints
// missing ids, so validation only refuses what cannot be repaired.
func normalizeInboxTagQuestions(qs []InboxTagQuestion) {
	for i := range qs {
		q := &qs[i]
		q.ID = strings.ToLower(strings.TrimSpace(q.ID))
		if q.ID == "" {
			q.ID = strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
		}
		q.Type = strings.ToLower(strings.TrimSpace(q.Type))
		q.Question = ClampLine(q.Question, InboxTagQuestionMaxLen)
		q.Action.normalize()
		if q.Type == InboxTagQuestionChoice {
			q.Label = ""
			q.Action = InboxTagQuestionAction{}
		} else {
			q.Label = InboxTagLabelName(q.Label)
			q.Choices = nil
		}
		for j := range q.Choices {
			c := &q.Choices[j]
			c.Label = InboxTagLabelName(c.Label)
			c.Description = ClampLine(c.Description, InboxTagChoiceDescMaxLen)
			c.Action.normalize()
		}
	}
}

func validateInboxTagLabel(label string) error {
	if label == "" {
		return errors.New("every tagging question needs a label")
	}
	if len([]rune(label)) > InboxTagLabelMaxLen {
		return fmt.Errorf("label %q is longer than %d characters", label, InboxTagLabelMaxLen)
	}
	if InboxTagLabelName(label) != label {
		return fmt.Errorf("label %q may use letters, digits, and single spaces or hyphens only", label)
	}
	return nil
}

func validateInboxTagQuestions(qs []InboxTagQuestion) error {
	if len(qs) > InboxTagQuestionsMax {
		return fmt.Errorf("at most %d tagging questions", InboxTagQuestionsMax)
	}
	ids := map[string]bool{}
	labels := map[string]bool{}
	claim := func(label string) error {
		if err := validateInboxTagLabel(label); err != nil {
			return err
		}
		key := strings.ToLower(label)
		if labels[key] {
			return fmt.Errorf("label %q is used by more than one question or option", label)
		}
		labels[key] = true
		return nil
	}
	for _, q := range qs {
		if !inboxTagIDPattern.MatchString(q.ID) {
			return fmt.Errorf("question id %q may use lower-case letters and digits only, up to 16", q.ID)
		}
		if ids[q.ID] {
			return fmt.Errorf("question id %q is used twice", q.ID)
		}
		ids[q.ID] = true
		if strings.TrimSpace(q.Question) == "" {
			return errors.New("every tagging question needs its question text")
		}
		switch q.Type {
		case InboxTagQuestionYesNo:
			if err := claim(q.Label); err != nil {
				return err
			}
			if err := q.Action.validate(); err != nil {
				return err
			}
		case InboxTagQuestionChoice:
			if len(q.Choices) < InboxTagChoicesMin || len(q.Choices) > InboxTagChoicesMax {
				return fmt.Errorf("a choice question needs between %d and %d options", InboxTagChoicesMin, InboxTagChoicesMax)
			}
			for _, c := range q.Choices {
				if err := claim(c.Label); err != nil {
					return err
				}
				if strings.TrimSpace(c.Description) == "" {
					return fmt.Errorf("option %q needs a description", c.Label)
				}
				if err := c.Action.validate(); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("%q is not a question type; use yes_no or choice", q.Type)
		}
	}
	return nil
}

// Bounds on the not-now hold.
const (
	NotNowHoldDaysMin     = 1
	NotNowHoldDaysMax     = 90
	NotNowHoldDaysDefault = 30
)

// DefaultCRMTaskIntents is the task-worthy set a workspace gets when it has
// never chosen one: every human intent, and no automated one. A vacation
// notice or a bounce is not follow-up work, and one week of sending makes
// enough of them to bury the real replies.
func DefaultCRMTaskIntents() []ReplyIntentType {
	return []ReplyIntentType{
		ReplyIntentPositive,
		ReplyIntentQuestion,
		ReplyIntentNeutral,
		ReplyIntentNegative,
	}
}

// TaskIntents resolves the configured set, falling back to the default when
// the workspace has never set one.
func (s ReplyIntentSettings) TaskIntents() []ReplyIntentType {
	if s.CRMTaskIntents == nil {
		return DefaultCRMTaskIntents()
	}
	return s.CRMTaskIntents
}

// CreatesTaskFor reports whether a reply classified as intent should open a
// CRM follow-up task.
func (s ReplyIntentSettings) CreatesTaskFor(intent ReplyIntentType) bool {
	if !s.AutoCreateCRMTask {
		return false
	}
	for _, want := range s.TaskIntents() {
		if want == intent {
			return true
		}
	}
	return false
}

// Bounds on the fallback out-of-office hold. A hold of zero days would send
// into the away message it was triggered by; one of months would silently
// abandon a lead nobody thinks to check on.
const (
	OOOHoldDaysMin     = 1
	OOOHoldDaysMax     = 90
	OOOHoldDaysDefault = 7
)

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
	// CheckContentScore scores each step's copy for spam signals, at preflight
	// and again per send against the rendered text. Advisory: it warns, it
	// never blocks a send.
	CheckContentScore bool `json:"check_content_score"`
	// MinContentScore is the 1-100 floor below which copy is flagged.
	MinContentScore int `json:"min_content_score"`
}

// Normalize clamps the settings an API caller can put out of range, so a stored
// value can never make every campaign fail the check or none of them.
func (s *AdvancedOutreachSettings) Normalize() {
	if s == nil {
		return
	}
	if s.Preflight.MinContentScore > 100 {
		s.Preflight.MinContentScore = 100
	}
	if s.Preflight.MinContentScore < 1 {
		s.Preflight.MinContentScore = 1
	}
	// A workspace that has never seen this setting stores a zero here; read it
	// as "the default", not as "resume the instant the auto-reply lands".
	if s.ReplyIntent.OutOfOfficeHoldDays == 0 {
		s.ReplyIntent.OutOfOfficeHoldDays = OOOHoldDaysDefault
	}
	if s.ReplyIntent.OutOfOfficeHoldDays < OOOHoldDaysMin {
		s.ReplyIntent.OutOfOfficeHoldDays = OOOHoldDaysMin
	}
	if s.ReplyIntent.OutOfOfficeHoldDays > OOOHoldDaysMax {
		s.ReplyIntent.OutOfOfficeHoldDays = OOOHoldDaysMax
	}
	if s.InboxTagging.NotNowHoldDays == 0 {
		s.InboxTagging.NotNowHoldDays = NotNowHoldDaysDefault
	}
	s.InboxTagging.NotNowHoldDays = min(max(s.InboxTagging.NotNowHoldDays, NotNowHoldDaysMin), NotNowHoldDaysMax)
	s.InboxTagging.Languages = normalizeCodes(s.InboxTagging.Languages)
	normalizeInboxTagQuestions(s.InboxTagging.Questions)
	if !ValidUnsubscribeMode(string(s.Unsubscribe.Mode)) || s.Unsubscribe.Mode == UnsubscribeModeInherit {
		s.Unsubscribe.Mode = UnsubscribeModeText
	}
	s.Unsubscribe.Text = clampLine(s.Unsubscribe.Text)
	s.Unsubscribe.LinkIntro = clampLine(s.Unsubscribe.LinkIntro)
	s.Unsubscribe.LinkText = clampLine(s.Unsubscribe.LinkText)
	s.ReplyIntent.CRMTaskIntents = normalizeIntents(s.ReplyIntent.CRMTaskIntents)
}

// Validate reports the settings a caller may not store. Normalize handles what
// can be clamped; this covers what can only be refused, so a bad value is a
// 400 rather than a silently different setting.
func (s *AdvancedOutreachSettings) Validate() error {
	if s == nil {
		return nil
	}
	for _, intent := range s.ReplyIntent.CRMTaskIntents {
		if !ValidReplyIntent(intent) {
			return fmt.Errorf("%q is not a reply intent", intent)
		}
	}
	for _, lang := range s.InboxTagging.Languages {
		if _, ok := MailLanguageNames[lang]; !ok {
			return fmt.Errorf("%q is not a supported tagging language", lang)
		}
	}
	return validateInboxTagQuestions(s.InboxTagging.Questions)
}

// normalizeCodes lower-cases, trims and de-duplicates a code list, keeping
// the caller's order.
func normalizeCodes(in []string) []string {
	var out []string
	for _, v := range in {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

// normalizeIntents lower-cases, trims and de-duplicates an intent list while
// keeping the caller's order. A nil list stays nil: absent means "the default
// set", which an empty list would not.
func normalizeIntents(in []ReplyIntentType) []ReplyIntentType {
	if in == nil {
		return nil
	}
	out := make([]ReplyIntentType, 0, len(in))
	seen := make(map[ReplyIntentType]struct{}, len(in))
	for _, v := range in {
		v = ReplyIntentType(strings.ToLower(strings.TrimSpace(string(v))))
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// clampLine trims a one-line copy field and caps it; the email footer is not
// the place for a paragraph or for line breaks.
func clampLine(v string) string { return ClampLine(v, UnsubscribeCopyMaxLen) }

// ClampLine collapses a one-line user string to single spaces and caps it at
// max runes. Shared by every field that is a single line of copy.
func ClampLine(v string, max int) string {
	v = strings.Join(strings.Fields(v), " ")
	if r := []rune(v); len(r) > max {
		v = strings.TrimSpace(string(r[:max]))
	}
	return v
}

// UnsubscribeMode is how a campaign email carries its opt-out. "text" appends
// a plain sentence inviting a reply (the default: it reads as a personal
// email and the reply is honoured automatically), "link" appends a sentence
// with a real unsubscribe link, "off" appends nothing. A campaign's own
// column may also hold "inherit", which follows the workspace setting.
type UnsubscribeMode string

const (
	UnsubscribeModeInherit UnsubscribeMode = "inherit"
	UnsubscribeModeText    UnsubscribeMode = "text"
	UnsubscribeModeLink    UnsubscribeMode = "link"
	UnsubscribeModeOff     UnsubscribeMode = "off"
)

// ValidUnsubscribeMode reports whether m is a value a campaign may store.
func ValidUnsubscribeMode(m string) bool {
	switch UnsubscribeMode(m) {
	case UnsubscribeModeInherit, UnsubscribeModeText, UnsubscribeModeLink, UnsubscribeModeOff:
		return true
	}
	return false
}

const (
	DefaultUnsubscribeText      = "If this isn't relevant, just reply and let me know and I won't email you again."
	DefaultUnsubscribeLinkIntro = "Not the right person, or not interested?"
	DefaultUnsubscribeLinkText  = "Unsubscribe"
	UnsubscribeCopyMaxLen       = 300
)

// UnsubscribeLinkToken is the template token a step places by hand to put the
// recipient's own opt-out link in its copy. The send path renders it as an
// anchor in HTML, but plain text has nowhere to hide the address, so preflight
// looks for it on a plain-text campaign.
const UnsubscribeLinkToken = "{{.UnsubscribeLink}}"

// UnsubscribeSettings is the workspace default for the in-body opt-out. The
// List-Unsubscribe header is a per-campaign flag and is not part of this.
type UnsubscribeSettings struct {
	Mode UnsubscribeMode `json:"mode"`
	// Text is the sentence appended in "text" mode.
	Text string `json:"text"`
	// LinkIntro and LinkText make up the "link" mode line: "<intro> <a>text</a>".
	LinkIntro string `json:"link_intro"`
	LinkText  string `json:"link_text"`
}

// Effective resolves a campaign's stored mode against the workspace default
// and fills any blank copy with the defaults, so the send path never has to
// think about settings written before this block existed.
func (u UnsubscribeSettings) Effective(campaignMode string) UnsubscribeSettings {
	out := u
	if m := UnsubscribeMode(campaignMode); m != "" && m != UnsubscribeModeInherit && ValidUnsubscribeMode(campaignMode) {
		out.Mode = m
	}
	if !ValidUnsubscribeMode(string(out.Mode)) || out.Mode == UnsubscribeModeInherit {
		out.Mode = UnsubscribeModeText
	}
	if strings.TrimSpace(out.Text) == "" {
		out.Text = DefaultUnsubscribeText
	}
	if strings.TrimSpace(out.LinkIntro) == "" {
		out.LinkIntro = DefaultUnsubscribeLinkIntro
	}
	if strings.TrimSpace(out.LinkText) == "" {
		out.LinkText = DefaultUnsubscribeLinkText
	}
	return out
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
	InboxTagging         InboxTaggingSettings            `json:"inbox_tagging"`
	SendTimeOptimization SendTimeOptimizationSettings    `json:"send_time_optimization"`
	Preflight            PreflightValidationSettings     `json:"preflight"`
	Dashboard            DeliverabilityDashboardSettings `json:"dashboard"`
	Unsubscribe          UnsubscribeSettings             `json:"unsubscribe"`
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

	// Suppression-only sources: never ingested as deliverability events, but
	// they share the column so one list explains why every entry is there.
	SuppressionSourceManual DeliverabilityEventType = "manual"
	SuppressionSourceImport DeliverabilityEventType = "import"
)

// SuppressionKind says what a suppression row matches: one address, or every
// address at a domain.
type SuppressionKind string

const (
	SuppressionKindEmail  SuppressionKind = "email"
	SuppressionKindDomain SuppressionKind = "domain"
)

// SuppressionListResult is the GET /suppressions page.
type SuppressionListResult struct {
	Data       []SuppressedRecipient `json:"data"`
	Pagination CPagination           `json:"pagination"`
}

// AddSuppressionsRequest adds addresses and domains by hand or from a pasted
// list. A value without "@" (or with a leading "@") is a domain.
type AddSuppressionsRequest struct {
	Entries []SuppressionEntry `json:"entries"`
	// Reason applies to every entry that does not carry its own.
	Reason string `json:"reason"`
}

type SuppressionEntry struct {
	Value  string `json:"value"`
	Reason string `json:"reason,omitempty"`
}

// AddSuppressionsResult reports what the request did. Skipped lists the
// values that were neither a valid address nor a valid domain.
type AddSuppressionsResult struct {
	Added   int      `json:"added"`
	Skipped []string `json:"skipped"`
}

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
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	// Email holds the address, or the bare domain when Kind is "domain".
	Email      string                  `json:"email"`
	Kind       SuppressionKind         `json:"kind"`
	Reason     string                  `json:"reason"`
	Source     DeliverabilityEventType `json:"source"`
	CampaignID *uuid.UUID              `json:"campaign_id,omitempty"`
	ExpiresAt  *time.Time              `json:"expires_at,omitempty"`
	Metadata   map[string]interface{}  `json:"metadata,omitempty"`
	CreatedAt  time.Time               `json:"created_at"`
	UpdatedAt  time.Time               `json:"updated_at"`
}

type CampaignABVariant struct {
	ID         uuid.UUID `json:"id"`
	CampaignID uuid.UUID `json:"campaign_id"`
	// SequenceID scopes the variant to one step. nil = campaign-level (applies
	// to every step, legacy behavior).
	SequenceID *uuid.UUID             `json:"step_id,omitempty"`
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
	Name       string                 `json:"name" binding:"required"`
	SequenceID *uuid.UUID             `json:"step_id,omitempty"`
	Weight     int                    `json:"weight"`
	Subject    string                 `json:"subject,omitempty"`
	BodyHTML   string                 `json:"body_html,omitempty"`
	BodyPlain  string                 `json:"body_plain,omitempty"`
	IsControl  bool                   `json:"is_control"`
	IsActive   *bool                  `json:"is_active,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
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
	// ReplyIntentAutomated is a machine reply that is not a vacation notice:
	// an autoresponder, a ticket acknowledgement, a bounce or a delivery
	// report. Recorded from the header layer of the reply classifier, which
	// sees markers the keyword lists never could.
	ReplyIntentAutomated ReplyIntentType = "automated"
)

// ValidReplyIntent reports whether v is an intent the classifier can record
// and a setting may name.
func ValidReplyIntent(v ReplyIntentType) bool {
	switch v {
	case ReplyIntentPositive, ReplyIntentNegative, ReplyIntentOutOfOffice,
		ReplyIntentQuestion, ReplyIntentNeutral, ReplyIntentAutomated:
		return true
	}
	return false
}

// IsAutomatedIntent reports whether an intent describes a machine reply.
func IsAutomatedIntent(v ReplyIntentType) bool {
	return v == ReplyIntentOutOfOffice || v == ReplyIntentAutomated
}

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
	IntentAutomated      int       `json:"intent_automated"`

	// Computed rates (percent, 0-100; 0 when no sends in the window). EmailsSent
	// is the count of completed campaign sends in the window (rate denominator).
	EmailsSent    int     `json:"emails_sent"`
	BounceRate    float64 `json:"bounce_rate"`
	ComplaintRate float64 `json:"complaint_rate"`
	OpenRate      float64 `json:"open_rate"`
	ClickRate     float64 `json:"click_rate"`
	ReplyRate     float64 `json:"reply_rate"`

	// Seed inbox-placement (nil when there are no seed samples in the window).
	SpamPlacementRate  *float64 `json:"spam_placement_rate,omitempty"`
	InboxPlacementRate *float64 `json:"inbox_placement_rate,omitempty"`
	PlacementSamples   int      `json:"placement_samples"`

	// Overall health band for the org window (from the documented thresholds).
	Band string `json:"band"`
	// Score is the 0-100 composite deliverability score for the window.
	Score int `json:"score"`

	Timeseries []DeliverabilityDailyPoint `json:"timeseries"`
	ByMailbox  []MailboxDeliverability    `json:"by_mailbox"`
	ByCampaign []CampaignDeliverability   `json:"by_campaign"`
	// ByProvider breaks seed placement results down per recipient provider.
	ByProvider []ProviderPlacement `json:"by_provider"`
	// WarmupPlacement is the continuous warmup-derived placement signal per
	// recipient domain (inbox = verified arrivals not flagged spam).
	WarmupPlacement []WarmupDomainPlacement `json:"warmup_placement"`
}

// DeliverabilityDailyPoint is one UTC day in the deliverability timeseries.
type DeliverabilityDailyPoint struct {
	Date         string `json:"date"` // YYYY-MM-DD (UTC)
	Sent         int    `json:"sent"`
	Bounces      int    `json:"bounces"`
	Complaints   int    `json:"complaints"`
	Opens        int    `json:"opens"`
	Clicks       int    `json:"clicks"`
	Replies      int    `json:"replies"`
	Unsubscribes int    `json:"unsubscribes"`
}

// MailboxDeliverability is one mailbox's bounce/complaint posture in the window.
type MailboxDeliverability struct {
	EmailAccountID uuid.UUID `json:"email_account_id"`
	Email          string    `json:"email"`
	Sent           int       `json:"sent"`
	Bounces        int       `json:"bounces"`
	Complaints     int       `json:"complaints"`
	BounceRate     float64   `json:"bounce_rate"`
	ComplaintRate  float64   `json:"complaint_rate"`
	Band           string    `json:"band"`
}

// CampaignDeliverability is one campaign's bounce/complaint posture in the window.
type CampaignDeliverability struct {
	CampaignID    uuid.UUID `json:"campaign_id"`
	Name          string    `json:"name"`
	Sent          int       `json:"sent"`
	Bounces       int       `json:"bounces"`
	Complaints    int       `json:"complaints"`
	BounceRate    float64   `json:"bounce_rate"`
	ComplaintRate float64   `json:"complaint_rate"`
	Band          string    `json:"band"`
}

// ProviderPlacement is one recipient provider's seed placement rollup in the
// window (from placement_results; folders mirror the table CHECK constraint).
type ProviderPlacement struct {
	// Provider is the seed's host family (a mailhost value), Label its name.
	Provider   string `json:"provider"`
	Label      string `json:"label"`
	Samples    int    `json:"samples"`
	Inbox      int    `json:"inbox"`
	Promotions int    `json:"promotions"`
	Spam       int    `json:"spam"`
	Other      int    `json:"other"`
	// Missing is copies that never arrived.
	Missing   int     `json:"missing"`
	InboxRate float64 `json:"inbox_rate"`
	SpamRate  float64 `json:"spam_rate"`
}

// WarmupDomainPlacement is one recipient domain's warmup placement rollup:
// Delivered counts verified warmup arrivals, Spam the ones flagged into junk.
type WarmupDomainPlacement struct {
	Provider  string  `json:"provider"`
	Domain    string  `json:"domain"`
	Delivered int     `json:"delivered"`
	Spam      int     `json:"spam"`
	InboxRate float64 `json:"inbox_rate"`
	SpamRate  float64 `json:"spam_rate"`
}

// DeliverabilityBand maps bounce%/complaint%/spam-placement% to a health band
// using the documented shared-pool thresholds (see CLAUDE.md). Rates are on the
// percent scale (0-100), so complaintRate>=0.03 means 0.03% (3 basis points),
// bounceRate>=10 means 10%. Worst band wins.
func DeliverabilityBand(bounceRate, complaintRate, spamRate float64) string {
	switch {
	case complaintRate >= 0.30 || bounceRate >= 10 || spamRate >= 40:
		return "blocked"
	case complaintRate >= 0.10 || bounceRate >= 5 || spamRate >= 20:
		return "quarantine"
	case complaintRate >= 0.03 || spamRate >= 10:
		return "warning"
	default:
		return "healthy"
	}
}

// DeliverabilityScore folds the same three rates the band uses into a 0-100
// score: each penalty saturates at the "blocked" threshold of its metric, so
// any single metric in the blocked band alone costs its full weight.
func DeliverabilityScore(bounceRate, complaintRate, spamRate float64) int {
	penalty := math.Min(40, bounceRate*4) + // 10% bounce = full 40
		math.Min(30, complaintRate*100) + // 0.30% complaints = full 30
		math.Min(40, spamRate) // 40% spam placement = full 40
	score := int(math.Round(100 - penalty))
	if score < 0 {
		return 0
	}
	return score
}

// Rate is a divide-by-zero-safe percentage (count/total*100).
func Rate(count, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(count) / float64(total) * 100
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
			Enabled:          true,
			PositiveKeywords: []string{"interested", "sounds good", "let's talk", "book", "demo", "pricing"},
			NegativeKeywords: []string{"not interested", "unsubscribe", "remove me", "stop", "no thanks"},
			// Not English-only: a German or French auto-reply is the common
			// case on a European list, and matching only English left it to be
			// caught by headers alone (issue #470). The layered classifier in
			// internal/app/replyclassify carries the same vocabulary, so
			// detection does not depend on a workspace having refreshed this
			// list.
			OutOfOfficeKeywords: []string{
				"out of office", "ooo", "vacation", "automatic reply", "annual leave",
				"abwesenheitsnotiz", "abwesend", "außer haus", "nicht im büro",
				"im urlaub", "zurück am", "automatische antwort",
				"réponse automatique", "absence du bureau",
				"respuesta automática", "risposta automatica", "automatisch antwoord",
			},
			QuestionKeywords:        []string{"?", "how", "what", "when", "price"},
			AutoCreateCRMTask:       true,
			AutoPauseOnNegative:     false,
			AutoSuppressOnUnsubWord: true,
			HoldOnOutOfOffice:       true,
			OutOfOfficeHoldDays:     OOOHoldDaysDefault,
		},
		InboxTagging: InboxTaggingSettings{
			// The reversible actions are on from the start, so a classified
			// reply does something useful on day one: a hold and a stop show
			// on the Leads tab and lift with a click, and a task is a task.
			// Suppression is the one that cannot be undone by the system, so
			// it waits for the workspace to turn it on.
			HoldOnNotNow:      true,
			NotNowHoldDays:    NotNowHoldDaysDefault,
			StopOnDeclined:    true,
			TaskOnCallRequest: true,
		},
		SendTimeOptimization: SendTimeOptimizationSettings{
			// Off by default: turning it on delays sends to reach the
			// recipient's business hours, which must be a choice the workspace
			// makes rather than one that changes its sending overnight.
			Enabled:                 false,
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
			CheckContentScore:        true,
			MinContentScore:          60,
		},
		Dashboard: DeliverabilityDashboardSettings{
			Enabled:            true,
			ShowSuppressionLog: true,
			ShowIntentSummary:  true,
			ShowDLQStats:       true,
		},
		// A plain reply-to-opt-out sentence by default: it satisfies CAN-SPAM,
		// CASL and the Spam Act (all accept a reply mechanism), and it reads
		// as a personal email where a formal link reads as bulk mail.
		Unsubscribe: UnsubscribeSettings{
			Mode:      UnsubscribeModeText,
			Text:      DefaultUnsubscribeText,
			LinkIntro: DefaultUnsubscribeLinkIntro,
			LinkText:  DefaultUnsubscribeLinkText,
		},
		Custom: map[string]interface{}{},
	}
}
