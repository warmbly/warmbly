package models

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Social channels.
const (
	SocialLinkedIn  = "linkedin"
	SocialInstagram = "instagram"
	SocialWhatsApp  = "whatsapp"
	SocialTelegram  = "telegram"
	SocialFacebook  = "facebook"
)

// SocialChannels lists every channel in the order the dashboard shows them.
var SocialChannels = []string{SocialLinkedIn, SocialInstagram, SocialWhatsApp, SocialTelegram, SocialFacebook}

// Consent bases outreach on a consent channel may rest on.
const (
	ConsentOptIn                = "opt_in"
	ConsentExistingConversation = "existing_conversation"
	ConsentImportedWithConsent  = "imported_with_consent"
	ConsentFriend               = "friend"
)

// Social action types, as GraphKit names them.
const (
	SocialActionInvite         = "invite"
	SocialActionMessage        = "message"
	SocialActionInmail         = "inmail"
	SocialActionVoiceNote      = "voice_note"
	SocialActionProfileVisit   = "profile_visit"
	SocialActionFollow         = "follow"
	SocialActionLikePost       = "like_post"
	SocialActionCommentPost    = "comment_post"
	SocialActionEndorse        = "endorse"
	SocialActionWithdrawInvite = "withdraw_invite"
)

// SocialChannelSpec is what a channel offers; it mirrors GraphKit's catalog and is the fallback when the catalog is unread.
type SocialChannelSpec struct {
	Label               string
	AuthMethods         []string
	CookieRequired      []string
	CookieOptional      []string
	Signals             []string
	Actions             []string
	ConsentRequired     bool
	ConsentBases        []string
	MessageMaxChars     int
	CommentMaxChars     int
	VoiceNoteMaxSeconds int
	InviteNoteMaxChars  int
	VoiceScriptMaxChars int
	InmailSubjectMax    int
	InmailBodyMax       int
	MaxPostAgeDays      int
	Reactions           []string
}

var consentBases = []string{ConsentOptIn, ConsentExistingConversation, ConsentImportedWithConsent}

var (
	socialWebAuth     = []string{"password", "cookie"}
	socialRepliedOnly = []string{"replied"}
)

// SocialChannelSpecs is GraphKit's channel matrix (campaign.stepChannels and the per-channel limits).
var SocialChannelSpecs = map[string]SocialChannelSpec{
	SocialLinkedIn: {Label: "LinkedIn", AuthMethods: socialWebAuth, CookieRequired: []string{"li_at"}, CookieOptional: []string{"li_a"},
		Signals: []string{"replied", "connected", "invite_pending", "open_profile"}, Actions: []string{SocialActionInvite, SocialActionMessage, SocialActionInmail, SocialActionVoiceNote,
			SocialActionProfileVisit, SocialActionFollow, SocialActionLikePost, SocialActionCommentPost, SocialActionEndorse, SocialActionWithdrawInvite},
		MessageMaxChars: 8000, CommentMaxChars: 1250, VoiceNoteMaxSeconds: 60, VoiceScriptMaxChars: 840, InviteNoteMaxChars: 300,
		InmailSubjectMax: 200, InmailBodyMax: 1900, MaxPostAgeDays: 365,
		Reactions: []string{"like", "celebrate", "support", "love", "insightful", "funny"}},
	SocialInstagram: {Label: "Instagram", AuthMethods: socialWebAuth, CookieRequired: []string{"sessionid"}, Signals: socialRepliedOnly, Actions: []string{SocialActionMessage, SocialActionVoiceNote, SocialActionFollow, SocialActionLikePost, SocialActionCommentPost},
		MessageMaxChars: 1000, CommentMaxChars: 2200, VoiceNoteMaxSeconds: 60, VoiceScriptMaxChars: 840, MaxPostAgeDays: 365, Reactions: []string{"like"}},
	SocialWhatsApp: {Label: "WhatsApp", AuthMethods: []string{"qr", "pairing_code"}, Signals: socialRepliedOnly, Actions: []string{SocialActionMessage, SocialActionVoiceNote}, ConsentRequired: true, ConsentBases: consentBases,
		MessageMaxChars: 4096, VoiceNoteMaxSeconds: 300, VoiceScriptMaxChars: 4200},
	SocialTelegram: {Label: "Telegram", AuthMethods: []string{"qr"}, Signals: socialRepliedOnly, Actions: []string{SocialActionMessage, SocialActionVoiceNote}, ConsentRequired: true, ConsentBases: consentBases,
		MessageMaxChars: 4096, VoiceNoteMaxSeconds: 300, VoiceScriptMaxChars: 4200},
	SocialFacebook: {Label: "Facebook", AuthMethods: socialWebAuth, CookieRequired: []string{"xs", "c_user", "datr"}, CookieOptional: []string{"sb", "fr"},
		Signals: socialRepliedOnly, Actions: []string{SocialActionMessage, SocialActionFollow, SocialActionLikePost, SocialActionCommentPost},
		ConsentRequired: true, ConsentBases: append(append([]string{}, consentBases...), ConsentFriend),
		MessageMaxChars: 2000, CommentMaxChars: 8000, MaxPostAgeDays: 365, Reactions: []string{"like"}},
}

// IsSocialChannel reports a known channel.
func IsSocialChannel(ch string) bool {
	_, ok := SocialChannelSpecs[ch]
	return ok
}

// SocialChannelLabel is a channel's display name.
func SocialChannelLabel(ch string) string {
	if s, ok := SocialChannelSpecs[ch]; ok {
		return s.Label
	}
	return "social"
}

// SocialActionOn reports whether a channel offers an action.
func SocialActionOn(ch, action string) bool {
	return contains(SocialChannelSpecs[ch].Actions, action)
}

// ValidConsentBasis reports whether a basis is one any channel accepts.
func ValidConsentBasis(b string) bool {
	switch b {
	case ConsentOptIn, ConsentExistingConversation, ConsentImportedWithConsent, ConsentFriend:
		return true
	}
	return false
}

// ConsentBasisFor reports whether a channel accepts a basis for outreach.
func ConsentBasisFor(ch, basis string) bool {
	return contains(SocialChannelSpecs[ch].ConsentBases, basis)
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// SocialBindingAction reports an action that binds the contact to the account that ran it for the rest of the campaign.
func SocialBindingAction(action string) bool {
	switch action {
	case SocialActionInvite, SocialActionMessage, SocialActionInmail, SocialActionVoiceNote:
		return true
	}
	return false
}

var (
	linkedInProfile  = regexp.MustCompile(`(?i)(?:^|[/.])linkedin\.com/in/([^/?#\s"'<>\\]+)`)
	linkedInSlug     = regexp.MustCompile(`^[\p{L}\p{N}_%.-]{1,200}$`)
	instagramProfile = regexp.MustCompile(`(?i)(?:^|[/.])instagram\.com/([A-Za-z0-9._]{1,30})/?(?:[?#].*)?$`)
	instagramName    = regexp.MustCompile(`^[A-Za-z0-9._]{1,30}$`)
	telegramProfile  = regexp.MustCompile(`(?i)(?:^|[/.])(?:t\.me|telegram\.me)/([A-Za-z0-9_]{5,32})/?(?:[?#].*)?$`)
	telegramName     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{4,31}$`)
	numericID        = regexp.MustCompile(`^\d{1,20}$`)
	facebookNumeric  = regexp.MustCompile(`(?i)(?:^|[/.])(?:facebook|fb)\.com/profile\.php\?(?:.*&)?id=(\d{1,20})(?:[&#].*)?$`)
	facebookPeople   = regexp.MustCompile(`(?i)(?:^|[/.])(?:facebook|fb)\.com/people/[^/?#]+/(\d{1,20})/?(?:[?#].*)?$`)
	phoneNoise       = regexp.MustCompile(`[\s().\-/]`)
	e164             = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)
)

// LinkedInPublicID is the lowercase profile slug a LinkedIn URL or bare public id names, "" when it names none.
func LinkedInPublicID(raw string) string {
	raw = strings.TrimSpace(raw)
	if m := linkedInProfile.FindStringSubmatch(raw); m != nil {
		return strings.ToLower(m[1])
	}
	if !strings.Contains(raw, "/") && linkedInSlug.MatchString(raw) {
		return strings.ToLower(raw)
	}
	return ""
}

// LinkedInProfileURL is the canonical profile URL for a public id.
func LinkedInProfileURL(publicID string) string {
	return "https://www.linkedin.com/in/" + publicID
}

// E164 is a phone number in E.164 form, "" when raw is not one; it must carry its country code.
func E164(raw string) string {
	s := phoneNoise.ReplaceAllString(strings.TrimSpace(raw), "")
	if strings.HasPrefix(s, "00") {
		s = "+" + s[2:]
	}
	if !e164.MatchString(s) {
		return ""
	}
	return s
}

// NormalizeSocialHandle is a handle's shown form and the form lookups match on; ok is false when raw names nobody on the channel.
func NormalizeSocialHandle(channel, raw string) (handle, normalized string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 500 {
		return "", "", false
	}
	switch channel {
	case SocialLinkedIn:
		if id := LinkedInPublicID(raw); id != "" {
			return id, id, true
		}
	case SocialInstagram:
		name := strings.TrimPrefix(raw, "@")
		if m := instagramProfile.FindStringSubmatch(raw); m != nil {
			name = m[1]
		}
		if instagramName.MatchString(name) {
			return name, strings.ToLower(name), true
		}
	case SocialWhatsApp:
		if p := E164(raw); p != "" {
			return p, p, true
		}
	case SocialTelegram:
		// GraphKit reaches Telegram by username, t.me link or numeric id; never by phone number.
		if numericID.MatchString(raw) {
			return raw, raw, true
		}
		name := strings.TrimPrefix(raw, "@")
		if m := telegramProfile.FindStringSubmatch(raw); m != nil {
			name = m[1]
		}
		if telegramName.MatchString(name) {
			return "@" + name, "@" + strings.ToLower(name), true
		}
	case SocialFacebook:
		// Facebook profiles are looked up by numeric id only; vanity names name nobody GraphKit can reach.
		if numericID.MatchString(raw) {
			return raw, raw, true
		}
		for _, re := range []*regexp.Regexp{facebookNumeric, facebookPeople} {
			if m := re.FindStringSubmatch(raw); m != nil {
				return m[1], m[1], true
			}
		}
	}
	return "", "", false
}

// SocialRecipient is what GraphKit takes as the recipient for a normalized handle.
func SocialRecipient(channel, normalized string) string {
	switch channel {
	case SocialLinkedIn:
		return LinkedInProfileURL(normalized)
	}
	return normalized
}

// ContactSocialIdentity is a contact's handle on one channel and the consent basis for outreach there.
type ContactSocialIdentity struct {
	Handle       string  `json:"handle"`
	ConsentBasis *string `json:"consent_basis"`
	// ConsentRecordedAt is when the basis was set; read only.
	ConsentRecordedAt *time.Time `json:"consent_recorded_at,omitempty"`
}

// ContactSocial is a contact's identities by channel; absent channels are omitted.
type ContactSocial map[string]ContactSocialIdentity

// ContactSocialPatch sets identities by channel; a nil value clears that channel.
type ContactSocialPatch map[string]*ContactSocialIdentity

// SocialIdentityRow is one stored identity.
type SocialIdentityRow struct {
	ContactID    uuid.UUID
	Channel      string
	Handle       string
	Normalized   string
	ConsentBasis *string
}

// Social workspace modes: none, Warmbly's own GraphKit workspace billed per seat, or the customer's own GraphKit workspace.
const (
	SocialModeNone    = "none"
	SocialModeManaged = "managed"
	SocialModeLinked  = "linked"
)

// Social seat billing modes.
const (
	SocialBillingStore  = "store"
	SocialBillingLinked = "linked"
)

// SocialWorkspace is the workspace's social mode, mirrored from GraphKit.
type SocialWorkspace struct {
	OrganizationID uuid.UUID
	Mode           string
	WorkspaceName  *string
	LinkedAt       *time.Time
	LinkSessionID  *string
	UpdatedAt      time.Time
}

// SocialAccount is one connected social account; Snapshot is GraphKit's SocialAccount as last read.
type SocialAccount struct {
	ID                  uuid.UUID
	OrganizationID      uuid.UUID
	UserID              *uuid.UUID
	Channel             string
	GraphKitID          string
	Status              string
	StatusReason        *string
	DisplayName         string
	Headline            string
	AvatarURL           string
	ProfileURL          string
	PublicID            string
	Licences            []string
	Snapshot            json.RawMessage
	BillingMode         string
	MonthlyRetailMicros Micro
	PaidThrough         *time.Time
	BillingFailures     int
	BillingAttemptedAt  *time.Time
	PendingChargeKey    *string
	PendingChargeMicros Micro
	PaidKey             *string
	PaidMicros          Micro
	UnpaidSince         *time.Time
	UnpaidPaused        bool
	LastNotice          string
	ConnectedAt         *time.Time
	RemovedAt           *time.Time
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// SocialConnectSession is one connect or reconnect at GraphKit and the first month it charged.
type SocialConnectSession struct {
	ID                string
	OrganizationID    uuid.UUID
	UserID            *uuid.UUID
	Channel           string
	Kind              string
	Method            *string
	AccountID         *uuid.UUID
	ChargeKey         *string
	ChargedMicros     Micro
	Status            string
	AccountGraphKitID *string
	RefundedAt        *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// SocialConversation is one synced conversation.
type SocialConversation struct {
	ID                 uuid.UUID       `json:"id"`
	OrganizationID     uuid.UUID       `json:"-"`
	AccountID          uuid.UUID       `json:"account_id"`
	Channel            string          `json:"channel"`
	GraphKitID         string          `json:"-"`
	ContactID          *uuid.UUID      `json:"contact_id"`
	Participant        json.RawMessage `json:"participant"`
	Kind               string          `json:"kind"`
	UnreadCount        int             `json:"unread_count"`
	LastMessageAt      *time.Time      `json:"last_message_at"`
	LastMessagePreview string          `json:"last_message_preview"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

// SocialMessage is one message of a conversation; BodySealed is the body under the workspace's data key.
type SocialMessage struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	ConversationID uuid.UUID
	GraphKitID     string
	Direction      string
	BodySealed     string
	SentAt         *time.Time
	Status         string
	Kind           string
	ActionID       *uuid.UUID
	SentByUserID   *uuid.UUID
	CreatedAt      time.Time
}

// LinkedInSavedSearch is a named LinkedIn lead search; Query is GraphKit's Query verbatim.
type LinkedInSavedSearch struct {
	ID             uuid.UUID       `json:"id"`
	OrganizationID uuid.UUID       `json:"-"`
	Name           string          `json:"name"`
	Query          json.RawMessage `json:"query"`
	CreatedBy      *uuid.UUID      `json:"created_by"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// LinkedInContactMatch is a workspace contact a LinkedIn profile already is, with what Warmbly did with it.
type LinkedInContactMatch struct {
	PublicID  string
	ContactID uuid.UUID
	Company   string
	Campaigns []SocialCampaignRef
	// Contacted is a sent campaign email; Replied a reply from the contact.
	Contacted bool
	Replied   bool
}

// SocialCampaignRef names one campaign a contact is in.
type SocialCampaignRef struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Status string    `json:"status"`
}

// Social action row statuses: pending is reserved here and not yet queued at GraphKit.
const (
	SocialActionPending   = "pending"
	SocialActionQueued    = "queued"
	SocialActionRunning   = "running"
	SocialActionSucceeded = "succeeded"
	SocialActionSkipped   = "skipped"
	SocialActionFailed    = "failed"
	SocialActionCancelled = "cancelled"
	SocialActionExpired   = "expired"
)

// SocialActionRow is one social step queued at GraphKit for one contact.
type SocialActionRow struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"-"`
	GraphKitID     *string    `json:"-"`
	AccountID      *uuid.UUID `json:"account_id"`
	CampaignID     *uuid.UUID `json:"campaign_id"`
	CampaignName   string     `json:"campaign_name,omitempty"`
	ContactID      uuid.UUID  `json:"contact_id"`
	SequenceID     *uuid.UUID `json:"step_id"`
	Channel        string     `json:"channel"`
	Type           string     `json:"type"`
	Status         string     `json:"status"`
	SkipReason     *string    `json:"skip_reason"`
	ErrorCode      *string    `json:"error_code"`
	Reference      string     `json:"-"`
	Attempts       int        `json:"-"`
	QueuedAt       *time.Time `json:"queued_at"`
	CompletedAt    *time.Time `json:"completed_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// SocialStepRequest is one due social step of a campaign for one contact.
type SocialStepRequest struct {
	OrganizationID uuid.UUID
	CampaignID     uuid.UUID
	ContactID      uuid.UUID
	SequenceID     uuid.UUID
	Step           SocialStep
	// Render renders a template for the contact as email copy is, with the sending account's name as the sender;
	// missing is true when the copy prints a contact field the contact has empty.
	Render func(text, part, senderName string) (out string, missing bool)
}

// Social step outcomes: done routes on, retry keeps the step due, stop takes the contact out of the campaign.
const (
	SocialStepQueued  = "queued"
	SocialStepSkipped = "skipped"
	SocialStepFailed  = "failed"
	SocialStepRetry   = "retry"
	SocialStepStop    = "stop"
)

// SocialStepOutcome is what became of a due social step; Reason is a plain sentence for the campaign feed.
type SocialStepOutcome struct {
	State  string
	Reason string
	// Code is the skip reason or error code behind a skipped or failed step.
	Code string
	// RetryAfter is how soon a retry is due; zero is the default.
	RetryAfter time.Duration
}

// SocialReply is an inbound social message for the reply path; the campaign fields are set when it answers a campaign.
type SocialReply struct {
	OrganizationID uuid.UUID
	ConversationID uuid.UUID
	AccountID      uuid.UUID
	Channel        string
	NotifyUserID   *uuid.UUID
	ContactID      *uuid.UUID
	CampaignID     *uuid.UUID
	SequenceID     *uuid.UUID
	Sender         string
	Body           string
}

// SocialChannelStats is one channel's part of a campaign's social analytics.
type SocialChannelStats struct {
	ActionsSent int `json:"actions_sent"`
	Replies     int `json:"replies"`
}

// SocialCampaignStats is the social half of a campaign's analytics.
type SocialCampaignStats struct {
	InvitesSent     int                           `json:"invites_sent"`
	InvitesAccepted int                           `json:"invites_accepted"`
	AcceptanceRate  float64                       `json:"acceptance_rate"`
	MessagesSent    int                           `json:"messages_sent"`
	InmailsSent     int                           `json:"inmails_sent"`
	VoiceNotesSent  int                           `json:"voice_notes_sent"`
	Replies         int                           `json:"replies"`
	ByChannel       map[string]SocialChannelStats `json:"by_channel"`
}

// LinkedIn import statuses.
const (
	LinkedInImportQueued  = "queued"
	LinkedInImportRunning = "running"
	LinkedInImportDone    = "done"
	LinkedInImportFailed  = "failed"
)

// LinkedInImport is a LinkedIn import that looks up work emails in the background.
type LinkedInImport struct {
	ID             uuid.UUID       `json:"id"`
	OrganizationID uuid.UUID       `json:"-"`
	UserID         *uuid.UUID      `json:"-"`
	Status         string          `json:"status"`
	Total          int             `json:"total"`
	Processed      int             `json:"processed"`
	Created        int             `json:"created"`
	Updated        int             `json:"updated"`
	Skipped        int             `json:"skipped"`
	NoEmail        int             `json:"no_email"`
	EmailsFound    int             `json:"emails_found"`
	ContactIDs     []uuid.UUID     `json:"contact_ids"`
	Request        json.RawMessage `json:"-"`
	Error          *string         `json:"error"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// ContactLinkedInField is the custom field imports and integrations carry a LinkedIn profile in.
const ContactLinkedInField = "linkedin_url"

// NormalizeSocialPatch checks a patch and splits it into the identities it sets and the channels it clears.
func NormalizeSocialPatch(p ContactSocialPatch) (set []SocialIdentityRow, clear []string, err error) {
	for _, ch := range SocialChannels {
		id, ok := p[ch]
		if !ok {
			continue
		}
		if id == nil || strings.TrimSpace(id.Handle) == "" {
			clear = append(clear, ch)
			continue
		}
		handle, norm, ok := NormalizeSocialHandle(ch, id.Handle)
		if !ok {
			return nil, nil, fmt.Errorf("social.%s.handle is not a %s handle", ch, SocialChannelLabel(ch))
		}
		row := SocialIdentityRow{Channel: ch, Handle: handle, Normalized: norm}
		if id.ConsentBasis != nil && *id.ConsentBasis != "" {
			if !ConsentBasisFor(ch, *id.ConsentBasis) {
				return nil, nil, fmt.Errorf("social.%s.consent_basis is not a consent basis %s accepts", ch, SocialChannelLabel(ch))
			}
			b := *id.ConsentBasis
			row.ConsentBasis = &b
		}
		set = append(set, row)
	}
	for ch := range p {
		if !IsSocialChannel(ch) {
			return nil, nil, fmt.Errorf("social.%s is not a channel", ch)
		}
	}
	return set, clear, nil
}

// WithLinkedInField adds the LinkedIn profile a contact's linkedin_url custom field names when the patch has none.
func WithLinkedInField(p ContactSocialPatch, fields map[string]string) ContactSocialPatch {
	if _, ok := p[SocialLinkedIn]; ok {
		return p
	}
	id := LinkedInPublicID(fields[ContactLinkedInField])
	if id == "" {
		return p
	}
	out := ContactSocialPatch{SocialLinkedIn: {Handle: id}}
	for k, v := range p {
		out[k] = v
	}
	return out
}
