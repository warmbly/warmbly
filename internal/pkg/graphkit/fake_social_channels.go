package graphkit

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The sandbox's channel catalog mirrors GraphKit's campaign domain (channel.go): which actions run where, the
// per-channel limits, and the consent rule on WhatsApp, Telegram and Facebook.

var fakeConsentBases = []string{"opt_in", "existing_conversation", "imported_with_consent"}

func fakeSocialCatalog() []SocialChannel {
	return []SocialChannel{
		{Channel: ChannelLinkedIn, Label: "LinkedIn", AuthMethods: []string{SignInPassword, SignInCookie},
			CookieNames: &SocialCookieNames{Required: []string{"li_at"}, Optional: []string{"li_a"}},
			Actions: []string{ActionInvite, ActionMessage, ActionInmail, ActionVoiceNote, ActionProfileVisit, ActionFollow, ActionLikePost,
				ActionCommentPost, ActionEndorse, ActionWithdrawInvite},
			Signals: []string{"replied", "connected", "invite_pending", "open_profile"}, ConsentBases: []string{},
			MessageMaxChars: 8000, CommentMaxChars: 1250, VoiceNoteMaxSeconds: 60, VoiceScriptMaxChars: 840, InviteNoteMaxChars: 300,
			InmailSubjectMax: 200, InmailBodyMax: 1900, MaxPostAgeDays: 365,
			Reactions: []string{"like", "celebrate", "support", "love", "insightful", "funny"}},
		{Channel: ChannelInstagram, Label: "Instagram", AuthMethods: []string{SignInPassword, SignInCookie},
			CookieNames: &SocialCookieNames{Required: []string{"sessionid"}, Optional: []string{}},
			Actions:     []string{ActionMessage, ActionVoiceNote, ActionFollow, ActionLikePost, ActionCommentPost},
			Signals:     []string{"replied"}, ConsentBases: []string{},
			MessageMaxChars: 1000, CommentMaxChars: 2200, VoiceNoteMaxSeconds: 60, VoiceScriptMaxChars: 840, MaxPostAgeDays: 365, Reactions: []string{"like"}},
		{Channel: ChannelWhatsApp, Label: "WhatsApp", AuthMethods: []string{SignInQR, SignInPairingCode},
			Actions: []string{ActionMessage, ActionVoiceNote}, Signals: []string{"replied"},
			ConsentRequired: true, ConsentBases: fakeConsentBases, MessageMaxChars: 4096, VoiceNoteMaxSeconds: 300, VoiceScriptMaxChars: 4200, Reactions: []string{}},
		{Channel: ChannelTelegram, Label: "Telegram", AuthMethods: []string{SignInQR},
			Actions: []string{ActionMessage, ActionVoiceNote}, Signals: []string{"replied"},
			ConsentRequired: true, ConsentBases: fakeConsentBases, MessageMaxChars: 4096, VoiceNoteMaxSeconds: 300, VoiceScriptMaxChars: 4200, Reactions: []string{}},
		{Channel: ChannelFacebook, Label: "Facebook", AuthMethods: []string{SignInPassword, SignInCookie},
			CookieNames: &SocialCookieNames{Required: []string{"xs", "c_user", "datr"}, Optional: []string{"sb", "fr"}},
			Actions:     []string{ActionMessage, ActionFollow, ActionLikePost, ActionCommentPost}, Signals: []string{"replied"},
			ConsentRequired: true, ConsentBases: append(append([]string{}, fakeConsentBases...), "friend"),
			MessageMaxChars: 2000, CommentMaxChars: 8000, MaxPostAgeDays: 365, Reactions: []string{"like"}},
	}
}

func fakeChannel(ch string) (SocialChannel, bool) {
	for _, c := range fakeSocialCatalog() {
		if c.Channel == ch {
			return c, true
		}
	}
	return SocialChannel{}, false
}

// fakeDefaultLimits are a new account's daily limits, weekly limits and their ceilings per channel.
func fakeDefaultLimits(ch string) (daily, weekly, ceilings, weeklyCeilings SocialLimits) {
	switch ch {
	case ChannelLinkedIn:
		return SocialLimits{ActionInvite: 15, ActionMessage: 50, ActionInmail: 20, ActionProfileVisit: 40, ActionFollow: 30, ActionLikePost: 30,
				ActionVoiceNote: 10, ActionEndorse: 15, ActionCommentPost: 20, ActionWithdrawInvite: 30, "search": 1000},
			SocialLimits{ActionInvite: 80},
			SocialLimits{ActionInvite: 25, ActionMessage: 100, ActionInmail: 40, ActionProfileVisit: 80, ActionFollow: 60, ActionLikePost: 60,
				ActionVoiceNote: 20, ActionEndorse: 50, ActionCommentPost: 50, ActionWithdrawInvite: 100, "search": 2500},
			SocialLimits{ActionInvite: 100}
	case ChannelInstagram:
		return SocialLimits{ActionMessage: 20, ActionFollow: 15, ActionLikePost: 20, ActionCommentPost: 10, ActionVoiceNote: 10},
			SocialLimits{},
			SocialLimits{ActionMessage: 50, ActionFollow: 40, ActionLikePost: 50, ActionCommentPost: 25, ActionVoiceNote: 20},
			SocialLimits{}
	case ChannelFacebook:
		return SocialLimits{ActionMessage: 15, ActionLikePost: 15, ActionCommentPost: 10, ActionFollow: 0},
			SocialLimits{},
			SocialLimits{ActionMessage: 40, ActionLikePost: 40, ActionCommentPost: 25, ActionFollow: 20},
			SocialLimits{}
	}
	return SocialLimits{ActionMessage: 20, ActionVoiceNote: 10}, SocialLimits{},
		SocialLimits{ActionMessage: 50, ActionVoiceNote: 20}, SocialLimits{}
}

func fakeCapabilities(ch string) map[string]SocialCapability {
	out := map[string]SocialCapability{}
	c, _ := fakeChannel(ch)
	for _, a := range c.Actions {
		out[a] = SocialCapability{Supported: true}
	}
	if ch == ChannelFacebook {
		reason := "following people is not enabled for this Facebook account by the provider"
		out[ActionFollow] = SocialCapability{Supported: false, Reason: &reason}
	}
	return out
}

var (
	fakeE164      = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)
	fakeUsername  = regexp.MustCompile(`^[a-z0-9._]{1,50}$`)
	fakeTelegram  = regexp.MustCompile(`^[a-z][a-z0-9_]{4,31}$`)
	fakeNumericID = regexp.MustCompile(`^\d{1,20}$`)
	fakePhoneJunk = regexp.MustCompile(`[\s().\-]`)
)

// fakeRecipient is the key a recipient names on a channel, "" when it is not a form the channel takes.
func fakeRecipient(ch, raw string) string {
	s := strings.TrimSpace(raw)
	switch ch {
	case ChannelLinkedIn:
		return fakePublicID(s)
	case ChannelWhatsApp:
		if p := fakePhoneJunk.ReplaceAllString(s, ""); fakeE164.MatchString(p) {
			return p
		}
		return ""
	case ChannelTelegram:
		// An @username, a t.me link or a numeric id; never a phone number.
		if fakeNumericID.MatchString(s) {
			return s
		}
		name := s
		if u, err := url.Parse(s); err == nil && u.Host != "" {
			if h := strings.ToLower(u.Hostname()); h != "t.me" && h != "telegram.me" {
				return ""
			}
			name = "@" + strings.Trim(u.Path, "/")
		}
		if n := strings.ToLower(strings.TrimPrefix(name, "@")); strings.HasPrefix(name, "@") && fakeTelegram.MatchString(n) {
			return "@" + n
		}
		return ""
	case ChannelFacebook:
		// Numeric ids only: profile.php?id=, /people/<name>/<id>, or the id itself.
		if fakeNumericID.MatchString(s) {
			return s
		}
		u, err := url.Parse(s)
		if err != nil || !strings.HasSuffix(strings.ToLower(u.Hostname()), "facebook.com") {
			return ""
		}
		if id := u.Query().Get("id"); strings.Trim(u.Path, "/") == "profile.php" && fakeNumericID.MatchString(id) {
			return id
		}
		if parts := strings.Split(strings.Trim(u.Path, "/"), "/"); len(parts) == 3 && parts[0] == "people" && fakeNumericID.MatchString(parts[2]) {
			return parts[2]
		}
		return ""
	}
	if fakeNumericID.MatchString(s) {
		return s
	}
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		s = strings.Trim(u.Path, "/")
	}
	name := strings.ToLower(strings.TrimPrefix(s, "@"))
	if fakeUsername.MatchString(name) {
		return name
	}
	return ""
}

// fakeProfileURL is where a recipient's profile lives on a channel.
func fakeProfileURL(ch, key string) string {
	switch ch {
	case ChannelLinkedIn:
		return "https://www.linkedin.com/in/" + key
	case ChannelInstagram:
		return "https://www.instagram.com/" + key
	case ChannelFacebook:
		return "https://www.facebook.com/profile.php?id=" + key
	case ChannelTelegram:
		if strings.HasPrefix(key, "@") {
			return "https://t.me/" + key[1:]
		}
	}
	return ""
}

// fakePersonOn is the sandbox person behind a recipient key on a channel.
func fakePersonOn(ch, key string) Person {
	if ch == ChannelLinkedIn {
		return fakePersonFor(key)
	}
	p := fakePerson(key)
	p.PublicID, p.ProfileURL, p.ProviderID = key, fakeProfileURL(ch, key), ch+":"+key
	return p
}

// fakeReplies reports a recipient the sandbox answers: one naming "reply", or a phone or numeric id ending in 99.
func fakeReplies(key string) bool {
	digits := strings.TrimPrefix(key, "+")
	return strings.Contains(key, "reply") || (fakeNumericID.MatchString(digits) && strings.HasSuffix(digits, "99"))
}

type fakeAudio struct {
	external string
	a        Audio
}

func (f *Fake) UploadAudio(_ context.Context, externalID string, in AudioInput, key string) (*Audio, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return replay(f, key, "social:audio:"+externalID, in.Filename+"|"+in.ContentType+"|"+strconv.Itoa(len(in.Data))+"|"+strconv.Itoa(in.DurationSeconds), func() (*Audio, error) {
		if err := f.socialAccountLocked(externalID); err != nil {
			return nil, err
		}
		if !strings.HasPrefix(in.ContentType, "audio/") {
			return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "file must be audio", map[string]any{"fields": []string{"file"}})
		}
		if len(in.Data) == 0 || len(in.Data) > AudioMaxBytes {
			return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "file must be at most 16 MiB", map[string]any{"fields": []string{"file"}})
		}
		if in.DurationSeconds < 1 || in.DurationSeconds > AudioMaxSeconds {
			return nil, fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, "duration_seconds must be 1 to 300", map[string]any{"fields": []string{"duration_seconds"}})
		}
		a := &fakeAudio{external: externalID, a: Audio{ID: f.id("aud"), DurationSeconds: in.DurationSeconds, ContentType: in.ContentType, CreatedAt: f.now().UTC()}}
		f.social.audio[a.a.ID] = a
		return cloneJSON(a.a), nil
	})
}

func (f *Fake) DeleteAudio(_ context.Context, externalID, id, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, err := replay(f, key, "social:audio-delete:"+id, nil, func() (*struct{}, error) {
		if err := f.socialAccountLocked(externalID); err != nil {
			return nil, err
		}
		a := f.social.audio[id]
		if a == nil || a.external != externalID {
			return nil, fakeErr(http.StatusNotFound, CodeNotFound, "no such audio")
		}
		for _, act := range f.social.actions {
			if act.audioID == id && (act.a.Status == ActionQueued || act.a.Status == ActionRunning) {
				return nil, fakeErr(http.StatusConflict, CodeStateConflict, "a queued action sends this recording")
			}
		}
		delete(f.social.audio, id)
		return &struct{}{}, nil
	})
	return err
}

// checkActionLocked refuses what GraphKit refuses for an action on the account's channel.
func (f *Fake) checkActionLocked(acct *fakeSocialAccount, in ActionInput) error {
	ch, _ := fakeChannel(acct.a.Channel)
	field := func(name, msg string) error {
		return fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, msg, map[string]any{"fields": []string{name}})
	}
	if !slices.Contains(ch.Actions, in.Type) {
		return field("type", in.Type+" is not an action on "+ch.Label)
	}
	if fakeRecipient(ch.Channel, in.Recipient) == "" {
		return fakeErr(http.StatusUnprocessableEntity, CodeRecipientInvalid, "the recipient is not a form "+ch.Label+" takes")
	}
	// A field the type does not use is refused.
	for _, u := range []struct {
		name  string
		set   bool
		types []string
	}{
		{"note", in.Note != nil, []string{ActionInvite}},
		{"body", in.Body != nil, []string{ActionMessage, ActionInmail}},
		{"subject", in.Subject != nil, []string{ActionInmail}},
		{"voice", in.Voice != nil, []string{ActionVoiceNote}},
		{"post", in.Post != nil, []string{ActionLikePost, ActionCommentPost}},
		{"reaction", in.Reaction != nil, []string{ActionLikePost}},
		{"max_post_age_days", in.MaxPostAgeDays != nil, []string{ActionLikePost, ActionCommentPost}},
		{"comment", in.Comment != nil, []string{ActionCommentPost}},
		{"max_skills", in.MaxSkills != nil, []string{ActionEndorse}},
	} {
		if u.set && !slices.Contains(u.types, in.Type) {
			return field(u.name, u.name+" is only for "+strings.Join(u.types, " and "))
		}
	}
	if ch.ConsentRequired {
		if in.Consent == nil || !slices.Contains(ch.ConsentBases, in.Consent.Basis) {
			return fakeErrDetails(http.StatusUnprocessableEntity, CodeConsentRequired, ch.Label+" outreach needs a consent basis for the contact",
				map[string]any{"field": "consent.basis"})
		}
	} else if in.Consent != nil {
		return field("consent", ch.Label+" takes no consent basis")
	}
	if c, ok := acct.a.Capabilities[in.Type]; ok && !c.Supported {
		reason := "the account cannot do this"
		if c.Reason != nil {
			reason = *c.Reason
		}
		return fakeErrDetails(http.StatusConflict, CodeCapabilityUnavailable, reason, map[string]any{"reason": reason})
	}
	runes := func(p *string) int {
		if p == nil {
			return 0
		}
		return len([]rune(*p))
	}
	switch in.Type {
	case ActionInvite:
		if runes(in.Note) > ch.InviteNoteMaxChars {
			return field("note", "note is too long")
		}
	case ActionMessage, ActionInmail:
		limit := ch.MessageMaxChars
		if in.Type == ActionInmail {
			limit = ch.InmailBodyMax
			if runes(in.Subject) > ch.InmailSubjectMax {
				return field("subject", "subject is too long")
			}
		}
		if in.Body == nil || strings.TrimSpace(*in.Body) == "" {
			return field("body", "body is required")
		}
		if runes(in.Body) > limit {
			return field("body", "body is too long")
		}
	case ActionVoiceNote:
		if err := f.checkVoiceLocked(acct, ch, in.Voice); err != nil {
			return err
		}
	case ActionLikePost, ActionCommentPost:
		if in.Reaction != nil && !slices.Contains(ch.Reactions, *in.Reaction) {
			return field("reaction", "unknown reaction")
		}
		if in.MaxPostAgeDays != nil && (*in.MaxPostAgeDays < 0 || *in.MaxPostAgeDays > ch.MaxPostAgeDays) {
			return field("max_post_age_days", "max_post_age_days is outside what the channel takes")
		}
		if in.Type == ActionCommentPost && (in.Comment == nil || strings.TrimSpace(*in.Comment) == "" || runes(in.Comment) > ch.CommentMaxChars) {
			return field("comment", "comment is required and at most the channel's limit")
		}
	case ActionEndorse:
		if in.MaxSkills != nil && (*in.MaxSkills < 1 || *in.MaxSkills > 3) {
			return field("max_skills", "max_skills is 1 to 3")
		}
	}
	return nil
}

// checkVoiceLocked refuses a voice note the channel or account cannot send.
func (f *Fake) checkVoiceLocked(acct *fakeSocialAccount, ch SocialChannel, v *VoiceInput) error {
	field := func(name, msg string) error {
		return fakeErrDetails(http.StatusBadRequest, CodeInvalidRequest, msg, map[string]any{"fields": []string{name}})
	}
	switch {
	case v == nil:
		return field("voice", "voice is required")
	case v.Mode == "recording":
		if a := f.social.audio[v.AudioID]; a == nil || a.external != acct.external {
			return field("voice.audio_id", "no such audio")
		} else if a.a.DurationSeconds > ch.VoiceNoteMaxSeconds {
			return field("voice.audio_id", "the recording is longer than "+ch.Label+" takes")
		}
	case v.Mode == "written":
		if strings.TrimSpace(v.Script) == "" || len([]rune(v.Script)) > ch.VoiceScriptMaxChars {
			return field("voice.script", "script is required and at most the channel's limit")
		}
		if !acct.a.Voice.Available {
			return fakeErrDetails(http.StatusConflict, CodeCapabilityUnavailable, "the account has no voice for written voice notes",
				map[string]any{"reason": "the account has no voice for written voice notes"})
		}
	default:
		return field("voice.mode", "mode must be recording or written")
	}
	return nil
}

// fakeQRPayload is the rotating payload a QR checkpoint shows.
func fakeQRPayload(session string, n int, now time.Time) string {
	return "graphkit-sandbox-qr:" + session + ":" + now.Format("150405") + ":" + string(rune('a'+n%26))
}
