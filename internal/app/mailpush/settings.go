// Package mailpush lets mail providers announce mailbox changes instead of
// waiting for the next poll: it decides which push channel each mailbox
// holds, authenticates what the providers send, and wakes the worker that
// holds the mailbox.
package mailpush

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// GraphNotificationPath is the backend route Graph posts a mailbox's notifications to.
const GraphNotificationPath = "/api/v1/mailbox-push/graph/"

// Settings decides the push channel each mailbox should hold.
type Settings struct {
	graphBase  string
	gmailTopic string
	key        []byte
	states     repository.MailboxPushRepository
}

// NewSettings reads the push configuration; secret keys the Graph client state.
func NewSettings(secret string, states repository.MailboxPushRepository) *Settings {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("warmbly mailbox push v1"))
	s := &Settings{gmailTopic: config.GmailPushTopic(), key: mac.Sum(nil), states: states}
	if config.GraphPushEnabled() {
		s.graphBase = config.BackendPublicURL() + GraphNotificationPath
	}
	return s
}

// GraphEnabled reports whether Outlook mailboxes subscribe to Graph notifications.
func (s *Settings) GraphEnabled() bool { return s != nil && s.graphBase != "" }

// GraphClientState is the secret Graph echoes on every notification for emailID.
func (s *Settings) GraphClientState(emailID uuid.UUID) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte("graph-client-state:" + emailID.String()))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyGraphClientState reports whether got is the client state issued for emailID.
func (s *Settings) VerifyGraphClientState(emailID uuid.UUID, got string) bool {
	return s != nil && hmac.Equal([]byte(s.GraphClientState(emailID)), []byte(got))
}

// For is the push block of a mailbox's ADD_EMAIL. A mailbox whose provider has
// push turned off still gets its recorded state, so the worker closes it.
func (s *Settings) For(ctx context.Context, emailID uuid.UUID, provider models.InboxProvider) *models.AddWorkerEmailPushData {
	if s == nil {
		return nil
	}
	out := &models.AddWorkerEmailPushData{}
	switch provider {
	case models.InboxProviderOutlook:
		if s.graphBase != "" {
			out.GraphNotificationURL = s.graphBase + emailID.String()
			out.GraphClientState = s.GraphClientState(emailID)
		}
	case models.InboxProviderGoogle:
		out.GmailTopic = s.gmailTopic
	default:
		return nil
	}
	if s.states != nil {
		state, err := s.states.GetPushState(ctx, emailID)
		if err != nil {
			log.Warn().Err(err).Str("email_id", emailID.String()).Msg("mailbox push state lookup failed")
		}
		out.State = state
	}
	if out.GraphNotificationURL == "" && out.GmailTopic == "" && out.State == nil {
		return nil
	}
	return out
}

// GraphMailboxID reads the mailbox id from a notification path suffix.
func GraphMailboxID(raw string) (uuid.UUID, bool) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	return id, err == nil && id != uuid.Nil
}
