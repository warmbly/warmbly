package graphkit

import (
	"encoding/json"
	"time"
)

// Social channels.
const (
	ChannelLinkedIn  = "linkedin"
	ChannelInstagram = "instagram"
	ChannelWhatsApp  = "whatsapp"
	ChannelTelegram  = "telegram"
	ChannelFacebook  = "facebook"
)

// Sign-in methods.
const (
	SignInPassword    = "password"
	SignInCookie      = "cookie"
	SignInQR          = "qr"
	SignInPairingCode = "pairing_code"
)

// SocialCookieNames are the session cookies a channel signs in with.
type SocialCookieNames struct {
	Required []string `json:"required"`
	Optional []string `json:"optional"`
}

// SocialChannel is one channel of the catalog: how it signs in, what it does and its limits.
type SocialChannel struct {
	Channel             string             `json:"channel"`
	Label               string             `json:"label"`
	AuthMethods         []string           `json:"auth_methods"`
	CookieNames         *SocialCookieNames `json:"cookie_names"`
	Actions             []string           `json:"actions"`
	Signals             []string           `json:"signals"`
	ConsentRequired     bool               `json:"consent_required"`
	ConsentBases        []string           `json:"consent_bases"`
	MessageMaxChars     int                `json:"message_max_chars"`
	CommentMaxChars     int                `json:"comment_max_chars"`
	VoiceNoteMaxSeconds int                `json:"voice_note_max_seconds"`
	VoiceScriptMaxChars int                `json:"voice_script_max_chars"`
	InviteNoteMaxChars  int                `json:"invite_note_max_chars"`
	InmailSubjectMax    int                `json:"inmail_subject_max_chars"`
	InmailBodyMax       int                `json:"inmail_body_max_chars"`
	Reactions           []string           `json:"reactions"`
	MaxPostAgeDays      int                `json:"max_post_age_days"`
}

// Social workspace modes.
const (
	SocialModeNone    = "none"
	SocialModeManaged = "managed"
	SocialModeLinked  = "linked"
)

// Link session statuses.
const (
	LinkPending   = "pending"
	LinkLinked    = "linked"
	LinkCancelled = "cancelled"
	LinkExpired   = "expired"
)

// Connect session statuses and kinds.
const (
	ConnectSignIn     = "sign_in"
	ConnectCheckpoint = "checkpoint"
	ConnectSucceeded  = "succeeded"
	ConnectFailed     = "failed"
	ConnectCancelled  = "cancelled"
	ConnectExpired    = "expired"

	ConnectKindConnect   = "connect"
	ConnectKindReconnect = "reconnect"
)

// Checkpoint types.
const (
	CheckpointTwoFactor      = "two_factor"
	CheckpointOTP            = "otp"
	CheckpointOTPOrApp       = "otp_or_app"
	CheckpointAppApproval    = "app_approval"
	CheckpointPhoneNumber    = "phone_number"
	CheckpointCaptcha        = "captcha"
	CheckpointChooseContract = "choose_contract"
	CheckpointChooseMethod   = "choose_method"
	CheckpointUnsupported    = "unsupported"
	CheckpointQRCode         = "qr_code"
	CheckpointPairingCode    = "pairing_code"
	CheckpointLoading        = "loading"
	CheckpointEncrypted      = "encrypted_history"
)

// Social account statuses.
const (
	SocialConnecting        = "connecting"
	SocialActive            = "active"
	SocialReconnectRequired = "reconnect_required"
	SocialRestricted        = "restricted"
	SocialPaused            = "paused"
	SocialDisconnected      = "disconnected"
	SocialRemoved           = "removed"
)

// Licences a social account can hold.
const (
	LicencePremium        = "premium"
	LicenceSalesNavigator = "sales_navigator"
	LicenceRecruiter      = "recruiter"
)

// SocialPrices is the social object of GET /pricing.
type SocialPrices struct {
	SeatMonthlyMicros int64           `json:"seat_monthly_micros"`
	Channels          []SocialChannel `json:"channels"`
}

// WorkspaceName names a linked GraphKit workspace.
type WorkspaceName struct {
	Name string `json:"name"`
}

// SocialWorkspace is an account's social workspace.
type SocialWorkspace struct {
	Mode                   string          `json:"mode"`
	Workspace              *WorkspaceName  `json:"workspace"`
	LinkedAt               *time.Time      `json:"linked_at"`
	Accounts               int             `json:"accounts"`
	SeatMonthlyMicros      int64           `json:"seat_monthly_micros"`
	AcknowledgementVersion string          `json:"acknowledgement_version"`
	Channels               []SocialChannel `json:"channels"`
}

// LinkSession is a consent page that links a customer's own GraphKit workspace.
type LinkSession struct {
	ID        string         `json:"id"`
	URL       string         `json:"url,omitempty"`
	Status    string         `json:"status"`
	Workspace *WorkspaceName `json:"workspace"`
	ExpiresAt time.Time      `json:"expires_at"`
}

// Acknowledgement records that the person accepted the terms of automating their account.
type Acknowledgement struct {
	Version          string    `json:"version"`
	AcceptedAt       time.Time `json:"accepted_at"`
	PersonExternalID string    `json:"person_external_id"`
	IP               string    `json:"ip,omitempty"`
	UserAgent        string    `json:"user_agent,omitempty"`
}

// ConnectInput starts a connect or reconnect session.
type ConnectInput struct {
	Channel         string           `json:"channel"`
	AccountID       *string          `json:"account_id"`
	Timezone        string           `json:"timezone,omitempty"`
	AcceptCharge    bool             `json:"accept_charge"`
	Acknowledgement *Acknowledgement `json:"acknowledgement,omitempty"`
}

// SignInInput is one sign-in attempt; credentials are passed once and never stored.
type SignInInput struct {
	Method            string            `json:"method"`
	Username          string            `json:"username,omitempty"`
	Password          string            `json:"password,omitempty"`
	Cookies           map[string]string `json:"cookies,omitempty"`
	PhoneNumber       string            `json:"phone_number,omitempty"`
	TwoFactorPassword string            `json:"two_factor_password,omitempty"`
	UserAgent         string            `json:"user_agent,omitempty"`
	IP                string            `json:"ip,omitempty"`
	Recruiter         bool              `json:"recruiter,omitempty"`
	Country           string            `json:"country,omitempty"`
}

// CheckpointAnswer answers a checkpoint with a code, one choice or several.
type CheckpointAnswer struct {
	Code    string   `json:"code,omitempty"`
	Choice  string   `json:"choice,omitempty"`
	Choices []string `json:"choices,omitempty"`
	// TwoFactorPassword answers Telegram's cloud password checkpoint.
	TwoFactorPassword string `json:"two_factor_password,omitempty"`
	// Secret is the PIN or recovery code an encrypted_history choice takes.
	Secret string `json:"secret,omitempty"`
}

// CheckpointChoice is one option a checkpoint offers; a bare string reads as its id.
type CheckpointChoice struct {
	ID                string `json:"id"`
	Label             string `json:"label,omitempty"`
	Description       string `json:"description,omitempty"`
	AttemptsRemaining *int   `json:"attempts_remaining,omitempty"`
}

// UnmarshalJSON accepts an option as an object or as a bare id.
func (c *CheckpointChoice) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*c = CheckpointChoice{ID: s, Label: s}
		return nil
	}
	type plain CheckpointChoice
	return json.Unmarshal(b, (*plain)(c))
}

// Checkpoint is what a connect session waits for.
type Checkpoint struct {
	Type              string             `json:"type"`
	Delivery          *string            `json:"delivery"`
	Choices           []CheckpointChoice `json:"choices"`
	AttemptsRemaining *int               `json:"attempts_remaining"`
	LastError         *string            `json:"last_error"`
	CaptchaKey        *string            `json:"captcha_key,omitempty"`
	CaptchaData       *string            `json:"captcha_data,omitempty"`
	// QRCode is the payload to render as a QR code, PairingCode the code to enter on the phone; both rotate until ExpiresAt.
	QRCode      *string    `json:"qr_code,omitempty"`
	PairingCode *string    `json:"pairing_code,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

// SocialError is the error a failed session, action or message carries.
type SocialError struct {
	Code      string `json:"code"`
	Message   string `json:"message,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`
}

// ConnectSession is one connect or reconnect of a social account.
type ConnectSession struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Channel    string         `json:"channel"`
	Status     string         `json:"status"`
	Checkpoint *Checkpoint    `json:"checkpoint"`
	CanResend  bool           `json:"can_resend"`
	Account    *SocialAccount `json:"account"`
	Error      *SocialError   `json:"error"`
	ExpiresAt  time.Time      `json:"expires_at"`
	CreatedAt  time.Time      `json:"created_at"`
}

// Ended reports a session that will not change again.
func (s *ConnectSession) Ended() bool {
	switch s.Status {
	case ConnectSucceeded, ConnectFailed, ConnectCancelled, ConnectExpired:
		return true
	}
	return false
}

// SocialLimits are an account's limits keyed by action type, only the channel's actions.
type SocialLimits map[string]int

// SocialCapability says whether the account can run an action, and why not.
type SocialCapability struct {
	Supported bool    `json:"supported"`
	Reason    *string `json:"reason"`
}

// SocialVoiceState says whether the account has a voice for written voice notes.
type SocialVoiceState struct {
	Available bool `json:"available"`
}

// WorkingHours are when an account's actions run, in its timezone; days are ISO weekdays.
type WorkingHours struct {
	Days  []int  `json:"days"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// SocialWarmup is an account's warm-up as read back.
type SocialWarmup struct {
	Enabled bool    `json:"enabled"`
	Percent *int    `json:"percent,omitempty"`
	EndsOn  *string `json:"ends_on,omitempty"`
}

// SocialWarmupInput changes an account's warm-up.
type SocialWarmupInput struct {
	Enabled      bool `json:"enabled"`
	Weeks        *int `json:"weeks,omitempty"`
	StartPercent *int `json:"start_percent,omitempty"`
}

// SocialUsage is one action's use today and this week.
type SocialUsage struct {
	Action      string `json:"action"`
	Today       int    `json:"today"`
	DailyLimit  int    `json:"daily_limit"`
	Week        int    `json:"week"`
	WeeklyLimit *int   `json:"weekly_limit"`
}

// InmailCredits are an account's InMail credits per product; nil when it does not hold one.
type InmailCredits struct {
	Classic        *int `json:"classic"`
	SalesNavigator *int `json:"sales_navigator"`
	Recruiter      *int `json:"recruiter"`
}

// SocialBilling is how an account is billed: managed is the partner's balance, linked the customer's plan.
type SocialBilling struct {
	Mode             string     `json:"mode"`
	MonthlyMicros    *int64     `json:"monthly_micros"`
	CurrentPeriodEnd *time.Time `json:"current_period_end"`
}

// SocialAccount is one connected social account.
type SocialAccount struct {
	ID                       string                      `json:"id"`
	Channel                  string                      `json:"channel"`
	Status                   string                      `json:"status"`
	StatusReason             *string                     `json:"status_reason"`
	DisplayName              string                      `json:"display_name"`
	Headline                 string                      `json:"headline"`
	AvatarURL                string                      `json:"avatar_url"`
	ProfileURL               string                      `json:"profile_url"`
	PublicID                 string                      `json:"public_id"`
	Licences                 []string                    `json:"licences"`
	InmailCredits            *InmailCredits              `json:"inmail_credits"`
	Limits                   SocialLimits                `json:"limits"`
	WeeklyLimits             SocialLimits                `json:"weekly_limits"`
	LimitCeilings            SocialLimits                `json:"limit_ceilings"`
	WeeklyLimitCeilings      SocialLimits                `json:"weekly_limit_ceilings"`
	Capabilities             map[string]SocialCapability `json:"capabilities"`
	Voice                    SocialVoiceState            `json:"voice"`
	WorkingHours             *WorkingHours               `json:"working_hours"`
	Timezone                 string                      `json:"timezone"`
	Warmup                   *SocialWarmup               `json:"warmup"`
	Usage                    []SocialUsage               `json:"usage"`
	PendingInvites           *int                        `json:"pending_invites"`
	AcceptanceRate           *float64                    `json:"acceptance_rate"`
	WithdrawInvitesAfterDays *int                        `json:"withdraw_invites_after_days,omitempty"`
	Billing                  SocialBilling               `json:"billing"`
	ConnectedAt              *time.Time                  `json:"connected_at"`
	UpdatedAt                time.Time                   `json:"updated_at"`
}

// SocialAccountPatch changes an account's settings; nil fields are left as they are.
type SocialAccountPatch struct {
	Limits                   SocialLimits       `json:"limits,omitempty"`
	WeeklyLimits             SocialLimits       `json:"weekly_limits,omitempty"`
	WorkingHours             *WorkingHours      `json:"working_hours,omitempty"`
	Timezone                 *string            `json:"timezone,omitempty"`
	Warmup                   *SocialWarmupInput `json:"warmup,omitempty"`
	WithdrawInvitesAfterDays *int               `json:"withdraw_invites_after_days,omitempty"`
}

// Search sources.
const (
	SourceSalesNavigatorLeads    = "sales_navigator_leads"
	SourceSalesNavigatorAccounts = "sales_navigator_accounts"
	SourceClassicPeople          = "classic_people"
	SourceRecruiterPeople        = "recruiter_people"
)

// QueryValue is one filter value.
type QueryValue struct {
	ID      string `json:"id,omitempty"`
	Text    string `json:"text,omitempty"`
	Exclude bool   `json:"exclude,omitempty"`
}

// Query is a lead search; it travels between Warmbly and GraphKit as raw JSON so no field is lost.
type Query struct {
	Source   string                  `json:"source"`
	Keywords string                  `json:"keywords,omitempty"`
	Filters  map[string][]QueryValue `json:"filters,omitempty"`
	URL      *string                 `json:"url,omitempty"`
}

// SearchInput is one page of a lead search.
type SearchInput struct {
	AccountID *string         `json:"account_id"`
	Query     json.RawMessage `json:"query"`
	Cursor    *string         `json:"cursor"`
	Limit     int             `json:"limit,omitempty"`
}

// Person is a LinkedIn member.
type Person struct {
	ProviderID      string `json:"provider_id"`
	PublicID        string `json:"public_id"`
	FullName        string `json:"full_name"`
	FirstName       string `json:"first_name"`
	LastName        string `json:"last_name"`
	Headline        string `json:"headline"`
	Location        string `json:"location"`
	ProfileURL      string `json:"profile_url"`
	PictureURL      string `json:"picture_url"`
	NetworkDistance string `json:"network_distance"`
	Premium         bool   `json:"premium"`
	OpenProfile     bool   `json:"open_profile"`
	CanInmail       bool   `json:"can_inmail"`
}

// Company is a LinkedIn company page.
type Company struct {
	ProviderID  string          `json:"provider_id"`
	Name        string          `json:"name"`
	Industry    string          `json:"industry"`
	Headcount   json.RawMessage `json:"headcount"`
	Location    string          `json:"location"`
	Website     string          `json:"website"`
	LinkedInURL string          `json:"linkedin_url"`
	LogoURL     string          `json:"logo_url"`
}

// Insights is what a search knows about a hit's current position.
type Insights struct {
	CurrentTitle      string `json:"current_title"`
	CurrentCompany    string `json:"current_company"`
	CurrentCompanyID  string `json:"current_company_id"`
	PositionStartedOn string `json:"position_started_on,omitempty"`
	CompanyStartedOn  string `json:"company_started_on,omitempty"`
	Industry          string `json:"industry"`
	SharedConnections int    `json:"shared_connections"`
	RecentlyHired     bool   `json:"recently_hired"`
	RecentlyPromoted  bool   `json:"recently_promoted"`
	PostedRecently    bool   `json:"posted_recently"`
	ViewedYourProfile bool   `json:"viewed_your_profile"`
	FollowsCompany    bool   `json:"follows_company"`
	PastColleague     bool   `json:"past_colleague"`
	OpenToWork        bool   `json:"open_to_work"`
}

// ReachReason is one reason a hit is easy or hard to reach.
type ReachReason struct {
	Code   string `json:"code"`
	Label  string `json:"label"`
	Weight int    `json:"weight"`
}

// Reach scores how reachable a hit is.
type Reach struct {
	Score   int           `json:"score"`
	Level   string        `json:"level"`
	Reasons []ReachReason `json:"reasons"`
}

// Hit is one search result: a person, a company, or both.
type Hit struct {
	Key      string    `json:"key"`
	Person   *Person   `json:"person"`
	Company  *Company  `json:"company"`
	Insights *Insights `json:"insights"`
	Reach    *Reach    `json:"reach"`
}

// AccountRef names the account a search ran on.
type AccountRef struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// SearchUsage is the account's search budget.
type SearchUsage struct {
	Used      int        `json:"used"`
	Limit     int        `json:"limit"`
	Remaining int        `json:"remaining"`
	ResetsAt  *time.Time `json:"resets_at"`
}

// SearchPage is one page of search results.
type SearchPage struct {
	Data        []Hit       `json:"data"`
	NextCursor  *string     `json:"next_cursor"`
	Total       *int        `json:"total"`
	Cap         int         `json:"cap"`
	EndReason   *string     `json:"end_reason"`
	Account     *AccountRef `json:"account"`
	Usage       SearchUsage `json:"usage"`
	LinkedInURL *string     `json:"linkedin_url"`
}

// TypeaheadItem is one typeahead suggestion.
type TypeaheadItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AIInput asks the AI builder for a query.
type AIInput struct {
	Prompt       string          `json:"prompt"`
	Source       string          `json:"source"`
	CurrentQuery json.RawMessage `json:"current_query"`
}

// AIChange is one filter change the AI builder made.
type AIChange struct {
	Filter string       `json:"filter"`
	Action string       `json:"action"`
	Values []QueryValue `json:"values"`
}

// AIResult is the AI builder's query and what it cost GraphKit.
type AIResult struct {
	Query       json.RawMessage `json:"query"`
	Summary     string          `json:"summary"`
	Changes     []AIChange      `json:"changes"`
	Assumptions []string        `json:"assumptions"`
	Unresolved  json.RawMessage `json:"unresolved"`
	Suggestions []string        `json:"suggestions"`
	CostMicros  int64           `json:"cost_micros"`
}

// ProfileRelation is the account's relation to a profile.
type ProfileRelation struct {
	NetworkDistance   string `json:"network_distance"`
	InvitationPending bool   `json:"invitation_pending"`
	IsFollowing       bool   `json:"is_following"`
}

// Experience is one position on a profile.
type Experience struct {
	Title     string  `json:"title"`
	Company   string  `json:"company"`
	StartedOn string  `json:"started_on,omitempty"`
	EndedOn   *string `json:"ended_on"`
}

// Profile is one LinkedIn profile looked up from an account.
type Profile struct {
	Person         Person          `json:"person"`
	Summary        string          `json:"summary"`
	CurrentTitle   string          `json:"current_title"`
	CurrentCompany string          `json:"current_company"`
	Industry       string          `json:"industry"`
	FollowerCount  int             `json:"follower_count"`
	Relation       ProfileRelation `json:"relation"`
	Experience     []Experience    `json:"experience"`
}

// Action types and statuses.
const (
	ActionInvite         = "invite"
	ActionMessage        = "message"
	ActionInmail         = "inmail"
	ActionVoiceNote      = "voice_note"
	ActionProfileVisit   = "profile_visit"
	ActionFollow         = "follow"
	ActionLikePost       = "like_post"
	ActionCommentPost    = "comment_post"
	ActionEndorse        = "endorse"
	ActionWithdrawInvite = "withdraw_invite"

	ActionQueued    = "queued"
	ActionRunning   = "running"
	ActionSucceeded = "succeeded"
	ActionSkipped   = "skipped"
	ActionFailed    = "failed"
	ActionCancelled = "cancelled"
	ActionExpired   = "expired"
)

// VoiceInput is a voice note: an uploaded recording or a script read in the account's voice.
type VoiceInput struct {
	Mode    string `json:"mode"`
	AudioID string `json:"audio_id,omitempty"`
	Script  string `json:"script,omitempty"`
}

// ConsentInput is the basis outreach on a consent channel rests on.
type ConsentInput struct {
	Basis string `json:"basis"`
}

// ActionInput queues one outreach action; only the fields its type takes are sent.
type ActionInput struct {
	AccountID      string        `json:"account_id"`
	Type           string        `json:"type"`
	Recipient      string        `json:"recipient"`
	Note           *string       `json:"note,omitempty"`
	Body           *string       `json:"body,omitempty"`
	Subject        *string       `json:"subject,omitempty"`
	Voice          *VoiceInput   `json:"voice,omitempty"`
	Post           *string       `json:"post,omitempty"`
	Reaction       *string       `json:"reaction,omitempty"`
	Comment        *string       `json:"comment,omitempty"`
	MaxPostAgeDays *int          `json:"max_post_age_days,omitempty"`
	MaxSkills      *int          `json:"max_skills,omitempty"`
	Consent        *ConsentInput `json:"consent,omitempty"`
	NotBefore      *time.Time    `json:"not_before,omitempty"`
	ExpiresAt      *time.Time    `json:"expires_at,omitempty"`
	Reference      string        `json:"reference,omitempty"`
}

// ActionRecipient is who an action was for, as resolved.
type ActionRecipient struct {
	Input      string `json:"input"`
	ProviderID string `json:"provider_id"`
	PublicID   string `json:"public_id"`
	FullName   string `json:"full_name"`
	ProfileURL string `json:"profile_url"`
}

// ActionResult names what an action produced.
type ActionResult struct {
	InvitationID   *string `json:"invitation_id"`
	ConversationID *string `json:"conversation_id"`
	MessageID      *string `json:"message_id"`
	CommentID      *string `json:"comment_id"`
	PostID         *string `json:"post_id"`
}

// Action is one queued outreach action.
type Action struct {
	ID           string          `json:"id"`
	AccountID    string          `json:"account_id"`
	Channel      string          `json:"channel"`
	Type         string          `json:"type"`
	Status       string          `json:"status"`
	Recipient    ActionRecipient `json:"recipient"`
	ScheduledFor *time.Time      `json:"scheduled_for"`
	AttemptedAt  *time.Time      `json:"attempted_at"`
	CompletedAt  *time.Time      `json:"completed_at"`
	Result       ActionResult    `json:"result"`
	SkipReason   *string         `json:"skip_reason"`
	Error        *SocialError    `json:"error"`
	Reference    *string         `json:"reference"`
	NotBefore    *time.Time      `json:"not_before"`
	ExpiresAt    *time.Time      `json:"expires_at"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// Participant is the other side of a conversation.
type Participant struct {
	ProviderID string `json:"provider_id"`
	PublicID   string `json:"public_id"`
	FullName   string `json:"full_name"`
	Headline   string `json:"headline"`
	ProfileURL string `json:"profile_url"`
	PictureURL string `json:"picture_url"`
}

// Attachment is a file on a message.
type Attachment struct {
	Name string `json:"name"`
	Mime string `json:"mime"`
	Size int64  `json:"size"`
}

// Message directions.
const (
	DirectionInbound  = "inbound"
	DirectionOutbound = "outbound"
)

// SocialMessage is one message of a conversation.
type SocialMessage struct {
	ID             string       `json:"id"`
	ConversationID string       `json:"conversation_id"`
	Channel        string       `json:"channel"`
	Direction      string       `json:"direction"`
	Body           string       `json:"body"`
	SentAt         *time.Time   `json:"sent_at"`
	Status         string       `json:"status"`
	Error          *SocialError `json:"error"`
	Kind           string       `json:"kind"`
	Attachments    []Attachment `json:"attachments"`
	ActionID       *string      `json:"action_id"`
}

// Conversation is one conversation of a social account.
type Conversation struct {
	ID            string         `json:"id"`
	AccountID     string         `json:"account_id"`
	Channel       string         `json:"channel"`
	Participant   Participant    `json:"participant"`
	Kind          string         `json:"kind"`
	UnreadCount   int            `json:"unread_count"`
	LastMessageAt *time.Time     `json:"last_message_at"`
	LastMessage   *SocialMessage `json:"last_message"`
}

// MessageEvent is the data of social.message.received and social.message.sent.
type MessageEvent struct {
	Channel      string        `json:"channel"`
	Conversation Conversation  `json:"conversation"`
	Message      SocialMessage `json:"message"`
}

// InvitationAccepted is the data of social.invitation.accepted.
type InvitationAccepted struct {
	AccountID   string    `json:"account_id"`
	Channel     string    `json:"channel"`
	Person      Person    `json:"person"`
	ConnectedAt time.Time `json:"connected_at"`
	ActionID    *string   `json:"action_id"`
}

// ConversationFilter narrows a conversation list.
type ConversationFilter struct {
	AccountID    string
	Channel      string
	UpdatedAfter *time.Time
	Cursor       string
	Limit        int
}

// ActionFilter narrows an action list.
type ActionFilter struct {
	AccountID string
	Status    string
	Reference string
	Cursor    string
	Limit     int
}

// Audio is an uploaded recording for voice notes.
type Audio struct {
	ID              string    `json:"id"`
	DurationSeconds int       `json:"duration_seconds"`
	ContentType     string    `json:"content_type"`
	CreatedAt       time.Time `json:"created_at"`
}

// AudioMaxBytes is the largest recording GraphKit takes.
const AudioMaxBytes = 16 << 20

// AudioInput is a recording to upload; GraphKit does not measure audio, so the length comes from the uploader.
type AudioInput struct {
	Filename        string
	ContentType     string
	Data            []byte
	DurationSeconds int
}

// AudioMaxSeconds is the longest recording GraphKit takes, the longest voice note of any channel.
const AudioMaxSeconds = 300
