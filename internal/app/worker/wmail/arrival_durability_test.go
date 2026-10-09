package wmail

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type durableTestMap struct {
	recoveryMessageMap
	pending         map[string]*repository.PendingArrival
	failAfterCommit bool
	unsupported     bool
}

func (m *durableTestMap) AdmitArrival(ctx context.Context, data repository.EmailMessageData, p *repository.PendingArrival) error {
	if m.unsupported {
		return repository.ErrArrivalOutboxUnsupported
	}
	if _, exists := m.data[data.MessageID]; !exists {
		m.data[data.MessageID] = data
		m.pending[data.MessageID] = p
	}
	if m.failAfterCommit {
		return errors.New("admission response lost")
	}
	return nil
}

func TestArrivalDurabilityRestartPreservesIDAcrossProviders(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider models.InboxProvider
		store    func(*WMail, context.Context, *models.EmailMessageData) error
		key      string
	}{
		{"google", models.InboxProviderGoogle, (*WMail).googleStore, "provider-id"},
		{"graph", models.InboxProviderOutlook, (*WMail).graphStore, "provider-id"},
		{"imap", models.InboxProviderSMTPIMAP, (*WMail).imapStore, "<arrival@test.local>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			m := &durableTestMap{recoveryMessageMap: recoveryMessageMap{data: map[string]repository.EmailMessageData{}}, pending: map[string]*repository.PendingArrival{}, failAfterCommit: true}
			var events []captured
			w := newTestWMail(t, tc.provider, &events)
			w.EmailMessageMapRepository = m
			msg := &models.EmailMessageData{MessageID: "<arrival@test.local>", GmailID: "provider-id", Subject: "arrival", BodyPlain: "complete body", InReplyTo: []string{"<send@test.local>"}}
			if err := tc.store(w, ctx, msg); err == nil {
				t.Fatal("expected ambiguous response")
			}
			canonical, err := m.Get(ctx, w.UserID, w.ID, tc.key)
			if err != nil || canonical == nil {
				t.Fatalf("durable mapping missing: %v", err)
			}
			original := canonical.ID
			if len(events) != 0 || len(m.pending) != 1 {
				t.Fatal("durable arrival must not depend on worker publication")
			}
			fresh := newTestWMail(t, tc.provider, &events)
			fresh.UserID = w.UserID
			fresh.ID = w.ID
			fresh.EmailMessageMapRepository = m
			m.failAfterCommit = false
			if !fresh.retryUnmap(ctx) {
				t.Fatal("fresh worker failed")
			}
			// Re-offering after a lost response cannot replace the queued canonical ID.
			if err := tc.store(fresh, ctx, msg); err != nil {
				t.Fatal(err)
			}
			p := m.pending[tc.key]
			if len(m.pending) != 1 || p.Arrival.Message.ID.String() != original || m.data[tc.key].ID != original || len(events) != 0 {
				t.Fatal("restart reminted the arrival or published outside durable delivery")
			}
			if p.Arrival.UserID != w.UserID || p.Arrival.Message.EmailID != w.ID || p.Arrival.Message.ID == uuid.Nil {
				t.Fatal("ownership not retained")
			}
		})
	}
}

func TestArrivalOldBackendKeepsLegacyPublication(t *testing.T) {
	var events []captured
	w := newTestWMail(t, models.InboxProviderGoogle, &events)
	m := &durableTestMap{recoveryMessageMap: recoveryMessageMap{data: map[string]repository.EmailMessageData{}}, unsupported: true}
	w.EmailMessageMapRepository = m
	if err := w.googleStore(context.Background(), &models.EmailMessageData{MessageID: "legacy", GmailID: "legacy"}); err != nil {
		t.Fatal(err)
	}
	if len(newEmails(events)) != 1 || len(m.data) != 1 {
		t.Fatal("old backend lost existing sync functionality")
	}
}
