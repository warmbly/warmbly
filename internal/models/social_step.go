package models

import (
	"fmt"
	"hash/fnv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Social step bounds beyond the channel catalog.
const (
	SocialMaxVariants     = 4
	SocialMaxComments     = 10
	SocialVariantIDMax    = 16
	SocialIfMissingSkip   = "skip"
	SocialIfMissingStop   = "stop"
	SocialVoiceRecording  = "recording"
	SocialVoiceWritten    = "written"
	SocialBaseVariantID   = "a"
	socialInviteNoteSlack = 2
)

// SocialVoice is a voice note step's audio: an uploaded recording or a script read in the account's voice.
type SocialVoice struct {
	Mode            string `json:"mode"`
	AudioID         string `json:"audio_id,omitempty"`
	DurationSeconds int    `json:"duration_seconds,omitempty"`
	Script          string `json:"script,omitempty"`
	FallbackScript  string `json:"fallback_script,omitempty"`
}

// SocialVariant is one A/B alternative of an invite, message or InMail's copy.
type SocialVariant struct {
	ID      string `json:"id"`
	Weight  int    `json:"weight"`
	Note    string `json:"note,omitempty"`
	Subject string `json:"subject,omitempty"`
	Body    string `json:"body,omitempty"`
}

// SocialStep is the config of a social sequence step.
type SocialStep struct {
	Channel         string          `json:"channel"`
	Action          string          `json:"action"`
	Note            string          `json:"note,omitempty"`
	FallbackNote    string          `json:"fallback_note,omitempty"`
	Body            string          `json:"body,omitempty"`
	FallbackBody    string          `json:"fallback_body,omitempty"`
	Subject         string          `json:"subject,omitempty"`
	FallbackSubject string          `json:"fallback_subject,omitempty"`
	Reaction        string          `json:"reaction,omitempty"`
	PostAgeDays     int             `json:"post_age_days,omitempty"`
	Comments        []string        `json:"comments,omitempty"`
	Voice           *SocialVoice    `json:"voice,omitempty"`
	MaxSkills       int             `json:"max_skills,omitempty"`
	Variants        []SocialVariant `json:"variants,omitempty"`
	Winner          string          `json:"winner,omitempty"`
	IfMissing       string          `json:"if_missing,omitempty"`
}

// StopsWhenMissing reports a step that takes the contact out of the campaign when it cannot reach them.
func (s *SocialStep) StopsWhenMissing() bool {
	return s.IfMissing == SocialIfMissingStop
}

func socialHasVariants(action string) bool {
	return action == SocialActionInvite || action == SocialActionMessage || action == SocialActionInmail
}

func tooLong(text string, max int) bool {
	return max > 0 && utf8.RuneCountInString(text) > max
}

// Validate checks the step's shape and lengths against the channel; copy is checked as written, since variables
// usually render shorter. Missing content is reported by Incomplete, so a step can be saved while it is edited.
func (s *SocialStep) Validate() error {
	spec, ok := SocialChannelSpecs[s.Channel]
	if !ok {
		return fmt.Errorf("unknown channel %q", s.Channel)
	}
	if !SocialActionOn(s.Channel, s.Action) {
		return fmt.Errorf("%s does not offer %q", spec.Label, s.Action)
	}
	switch s.IfMissing {
	case "", SocialIfMissingSkip, SocialIfMissingStop:
	default:
		return fmt.Errorf("if_missing must be skip or stop")
	}
	noteMax := spec.InviteNoteMaxChars * socialInviteNoteSlack
	for _, t := range []string{s.Note, s.FallbackNote} {
		if tooLong(t, noteMax) {
			return fmt.Errorf("the invitation note is longer than %d characters", spec.InviteNoteMaxChars)
		}
	}
	bodyMax := spec.MessageMaxChars
	if s.Action == SocialActionInmail {
		bodyMax = spec.InmailBodyMax
	}
	for _, t := range []string{s.Body, s.FallbackBody} {
		if tooLong(t, bodyMax) {
			return fmt.Errorf("the message is longer than %d characters, %s's limit", bodyMax, spec.Label)
		}
	}
	for _, t := range []string{s.Subject, s.FallbackSubject} {
		if tooLong(t, spec.InmailSubjectMax) {
			return fmt.Errorf("the subject is longer than %d characters", spec.InmailSubjectMax)
		}
	}
	if s.Reaction != "" && (s.Action != SocialActionLikePost || !contains(spec.Reactions, s.Reaction)) {
		return fmt.Errorf("%s does not offer the %q reaction", spec.Label, s.Reaction)
	}
	if s.PostAgeDays < 0 || s.PostAgeDays > spec.MaxPostAgeDays {
		return fmt.Errorf("post_age_days must be between 0 and %d", spec.MaxPostAgeDays)
	}
	if len(s.Comments) > SocialMaxComments {
		return fmt.Errorf("a comment step takes at most %d comments", SocialMaxComments)
	}
	for _, c := range s.Comments {
		if tooLong(c, spec.CommentMaxChars) {
			return fmt.Errorf("a comment is longer than %d characters, %s's limit", spec.CommentMaxChars, spec.Label)
		}
	}
	if s.MaxSkills != 0 && (s.Action != SocialActionEndorse || s.MaxSkills < 1 || s.MaxSkills > 3) {
		return fmt.Errorf("max_skills must be between 1 and 3")
	}
	if v := s.Voice; v != nil {
		if s.Action != SocialActionVoiceNote {
			return fmt.Errorf("voice is only for a voice note step")
		}
		switch v.Mode {
		case SocialVoiceRecording, SocialVoiceWritten:
		default:
			return fmt.Errorf("voice mode must be recording or written")
		}
		if v.DurationSeconds < 0 || v.DurationSeconds > spec.VoiceNoteMaxSeconds {
			return fmt.Errorf("a %s voice note is at most %d seconds", spec.Label, spec.VoiceNoteMaxSeconds)
		}
		if len(v.AudioID) > 100 || tooLong(v.Script, spec.VoiceScriptMaxChars) || tooLong(v.FallbackScript, spec.VoiceScriptMaxChars) {
			return fmt.Errorf("the voice note is too long")
		}
	}
	if len(s.Variants) > 0 && !socialHasVariants(s.Action) {
		return fmt.Errorf("only invitations, messages and InMails take variants")
	}
	if len(s.Variants) > SocialMaxVariants {
		return fmt.Errorf("a step takes at most %d variants", SocialMaxVariants)
	}
	seen := map[string]bool{SocialBaseVariantID: true}
	for _, v := range s.Variants {
		id := strings.TrimSpace(v.ID)
		if id == "" || len(id) > SocialVariantIDMax || seen[id] {
			return fmt.Errorf("each variant needs its own id")
		}
		seen[id] = true
		if v.Weight < 0 || v.Weight > 100 {
			return fmt.Errorf("a variant's weight is between 0 and 100")
		}
		if tooLong(v.Note, noteMax) || tooLong(v.Body, bodyMax) || tooLong(v.Subject, spec.InmailSubjectMax) {
			return fmt.Errorf("variant %s is longer than %s allows", id, spec.Label)
		}
	}
	if s.Winner != "" && !seen[s.Winner] {
		return fmt.Errorf("the winner names no variant")
	}
	return nil
}

// Incomplete is why the step cannot run yet, "" when it can.
func (s *SocialStep) Incomplete() string {
	switch s.Action {
	case SocialActionMessage, SocialActionInmail:
		if strings.TrimSpace(s.Body) == "" {
			return "has no message"
		}
	case SocialActionCommentPost:
		for _, c := range s.Comments {
			if strings.TrimSpace(c) != "" {
				return ""
			}
		}
		return "has no comment"
	case SocialActionVoiceNote:
		v := s.Voice
		switch {
		case v == nil:
			return "has no voice note"
		case v.Mode == SocialVoiceRecording && v.AudioID == "":
			return "has no recording"
		case v.Mode == SocialVoiceWritten && strings.TrimSpace(v.Script) == "":
			return "has no script"
		}
	}
	return ""
}

// SocialCopy is the copy one contact gets from a step: the base or one variant.
type SocialCopy struct {
	Variant string
	Note    string
	Subject string
	Body    string
}

// CopyFor picks the step's copy for a contact: the winner when one is set, otherwise a weighted draw that is stable per
// contact and step. The base copy takes whatever weight the variants leave of 100.
func (s *SocialStep) CopyFor(contactID, stepID uuid.UUID) SocialCopy {
	base := SocialCopy{Variant: SocialBaseVariantID, Note: s.Note, Subject: s.Subject, Body: s.Body}
	if len(s.Variants) == 0 || !socialHasVariants(s.Action) {
		return base
	}
	pick := func(v SocialVariant) SocialCopy {
		c := SocialCopy{Variant: v.ID, Note: v.Note, Subject: v.Subject, Body: v.Body}
		// A variant only replaces the parts it writes.
		if strings.TrimSpace(c.Note) == "" {
			c.Note = s.Note
		}
		if strings.TrimSpace(c.Subject) == "" {
			c.Subject = s.Subject
		}
		if strings.TrimSpace(c.Body) == "" {
			c.Body = s.Body
		}
		return c
	}
	if s.Winner != "" {
		for _, v := range s.Variants {
			if v.ID == s.Winner {
				return pick(v)
			}
		}
		return base
	}
	total := 0
	for _, v := range s.Variants {
		total += v.Weight
	}
	baseWeight := max(0, 100-total)
	total += baseWeight
	if total == 0 {
		return base
	}
	h := fnv.New64a()
	_, _ = h.Write(contactID[:])
	_, _ = h.Write(stepID[:])
	n := int(h.Sum64() % uint64(total))
	if n < baseWeight {
		return base
	}
	n -= baseWeight
	for _, v := range s.Variants {
		if n < v.Weight {
			return pick(v)
		}
		n -= v.Weight
	}
	return base
}
