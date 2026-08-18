package wmail

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// fakeMessageMap reports every message as unseen so the "new email" branch is
// the one under test, and records nothing else.
type fakeMessageMap struct{}

func (fakeMessageMap) Add(context.Context, repository.EmailMessageData) error { return nil }
func (fakeMessageMap) Get(context.Context, uuid.UUID, uuid.UUID, string) (*repository.EmailMessageData, error) {
	return nil, nil
}
func (fakeMessageMap) Del(context.Context, uuid.UUID, uuid.UUID, string, uuid.UUID) error {
	return nil
}

type fakeStore struct{}

func (fakeStore) Get(context.Context, string) (io.ReadCloser, error) { return nil, nil }
func (fakeStore) Put(context.Context, string, io.Reader, string) error {
	return nil
}
func (fakeStore) PutPublic(context.Context, string, io.Reader, string) (string, error) {
	return "", nil
}
func (fakeStore) Delete(context.Context, string) error      { return nil }
func (fakeStore) Has(context.Context, string) (bool, error) { return false, nil }
func (fakeStore) Name() string                              { return "fake" }
func (fakeStore) PresignedGetURL(context.Context, string, time.Duration) (string, error) {
	return "", nil
}

type captured struct {
	eventType models.JobEventType
	body      any
}

// newTestWMail builds a WMail with no Redis (which makes the sync governor
// admit everything) and fakes for the two dependencies the store paths touch.
func newTestWMail(t *testing.T, provider models.InboxProvider, got *[]captured) *WMail {
	t.Helper()
	w := &WMail{
		ID:                        uuid.New(),
		UserID:                    uuid.New(),
		Email:                     "box@example.test",
		EmailType:                 provider,
		Storage:                   fakeStore{},
		EmailMessageMapRepository: fakeMessageMap{},
		SmtpImapData:              &SmtpImapData{},
	}
	w.onEvent = func(jobType models.JobEventType, body any) error {
		*got = append(*got, captured{eventType: jobType, body: body})
		return nil
	}
	return w
}

// TestNewEmailEventShape pins the worker -> consumer contract for NEW_EMAIL
// across all three providers' store paths (which share storeNew). The consumer
// decodes it as JobEventNewEmail; each provider previously emitted the bare
// message, which left Message nil on the far side and panicked the consumer on
// the first inbound mail.
func TestNewEmailEventShape(t *testing.T) {
	msg := func() *models.EmailMessageData {
		return &models.EmailMessageData{
			MessageID: "<abc@example.test>",
			GmailID:   "provider-id-1",
			Subject:   "hello",
			From:      []string{"someone@example.test"},
		}
	}

	cases := []struct {
		name     string
		provider models.InboxProvider
		invoke   func(w *WMail) error
	}{
		{"imap", models.InboxProviderSMTPIMAP, func(w *WMail) error {
			return w.imapStore(context.Background(), msg())
		}},
		{"google", models.InboxProviderGoogle, func(w *WMail) error {
			return w.googleStore(context.Background(), msg())
		}},
		{"graph", models.InboxProviderOutlook, func(w *WMail) error {
			return w.graphStore(context.Background(), msg())
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []captured
			w := newTestWMail(t, tc.provider, &got)

			if err := tc.invoke(w); err != nil {
				t.Fatalf("handler returned error: %v", err)
			}

			var found *captured
			for i := range got {
				if got[i].eventType == models.JobEventTypeNewEmail {
					found = &got[i]
					break
				}
			}
			if found == nil {
				t.Fatalf("no NEW_EMAIL event emitted (got %d events)", len(got))
			}

			ev, ok := found.body.(*models.JobEventNewEmail)
			if !ok {
				t.Fatalf("NEW_EMAIL body is %T, want *models.JobEventNewEmail — "+
					"the consumer decodes this type and would see a nil Message", found.body)
			}
			if ev.Message == nil {
				t.Fatal("NEW_EMAIL carried a nil Message")
			}
			if ev.UserID != w.UserID {
				t.Errorf("UserID = %s, want %s", ev.UserID, w.UserID)
			}
			if ev.Message.EmailID != w.ID {
				t.Errorf("Message.EmailID = %s, want %s", ev.Message.EmailID, w.ID)
			}
		})
	}
}
