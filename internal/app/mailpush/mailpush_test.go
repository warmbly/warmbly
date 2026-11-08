package mailpush

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestGraphClientStateIsPerMailboxAndKeyed(t *testing.T) {
	a := NewSettings("an-auth-secret-of-at-least-32-bytes!!", nil)
	b := NewSettings("another-auth-secret-of-32-bytes-or-more", nil)
	one, two := uuid.New(), uuid.New()
	if !a.VerifyGraphClientState(one, a.GraphClientState(one)) {
		t.Fatal("a mailbox's own client state did not verify")
	}
	if a.VerifyGraphClientState(two, a.GraphClientState(one)) {
		t.Fatal("one mailbox's client state verified for another")
	}
	if a.VerifyGraphClientState(one, b.GraphClientState(one)) {
		t.Fatal("a client state from another secret verified")
	}
	if len(a.GraphClientState(one)) > 128 {
		t.Fatal("Graph caps clientState at 128 characters")
	}
}

func TestParseGraphIgnoresForgedItemsAndFlagsRenewal(t *testing.T) {
	s := NewSettings("an-auth-secret-of-at-least-32-bytes!!", nil)
	id := uuid.New()
	good := s.GraphClientState(id)
	for _, tc := range []struct {
		name string
		body string
		want GraphPost
	}{
		{"change", `{"value":[{"clientState":"` + good + `","changeType":"created"}]}`, GraphPost{Authentic: true}},
		{"forged", `{"value":[{"clientState":"guess","changeType":"created"}]}`, GraphPost{}},
		{"reauthorize", `{"value":[{"clientState":"` + good + `","lifecycleEvent":"reauthorizationRequired"}]}`, GraphPost{Authentic: true, Renew: true}},
		{"removed", `{"value":[{"clientState":"` + good + `","lifecycleEvent":"subscriptionRemoved"}]}`, GraphPost{Authentic: true, Renew: true}},
		{"missed", `{"value":[{"clientState":"` + good + `","lifecycleEvent":"missed"}]}`, GraphPost{Authentic: true}},
		{"forged renewal", `{"value":[{"clientState":"x","lifecycleEvent":"subscriptionRemoved"}]}`, GraphPost{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.ParseGraph(id, []byte(tc.body))
			if err != nil || got != tc.want {
				t.Fatalf("got %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
	if _, err := s.ParseGraph(id, []byte("not json")); err == nil {
		t.Fatal("a malformed body parsed")
	}
}

type fakeStates struct {
	repository.MailboxPushRepository
	state   *models.MailboxPushState
	targets []repository.SyncWakeTarget
}

func (f *fakeStates) GetPushState(context.Context, uuid.UUID) (*models.MailboxPushState, error) {
	return f.state, nil
}

func (f *fakeStates) WakeTargets(context.Context, []uuid.UUID) ([]repository.SyncWakeTarget, error) {
	return f.targets, nil
}

func (f *fakeStates) GmailWakeTargets(context.Context, string) ([]repository.SyncWakeTarget, error) {
	return f.targets, nil
}

func TestForCarriesOnlyWhatTheProviderUses(t *testing.T) {
	t.Setenv("GRAPH_PUSH", "true")
	t.Setenv("BACKEND_PUBLIC_URL", "https://api.example.com")
	t.Setenv("GMAIL_PUSH_TOPIC", "projects/p/topics/gmail")
	s := NewSettings("an-auth-secret-of-at-least-32-bytes!!", &fakeStates{})
	id := uuid.New()

	graph := s.For(t.Context(), id, models.InboxProviderOutlook, models.MailAuthOAuth)
	if graph == nil || graph.GraphNotificationURL != "https://api.example.com"+GraphNotificationPath+id.String() || graph.GmailTopic != "" {
		t.Fatalf("outlook push = %+v", graph)
	}
	gmail := s.For(t.Context(), id, models.InboxProviderGoogle, models.MailAuthOAuth)
	if gmail == nil || gmail.GmailTopic != "projects/p/topics/gmail" || gmail.GraphNotificationURL != "" {
		t.Fatalf("gmail push = %+v", gmail)
	}
	if s.For(t.Context(), id, models.InboxProviderSMTPIMAP, models.MailAuthPassword) != nil {
		t.Fatal("IMAP mailboxes watch with IDLE and carry no provider push")
	}
}

func TestGmailWatchNeedsTheTopicInTheCallersProject(t *testing.T) {
	t.Setenv("GMAIL_PUSH_TOPIC", "projects/mailboxes/topics/gmail")
	id := uuid.New()
	for _, c := range []struct {
		key       string
		delegated bool
	}{
		{"", false},
		{`{"type":"service_account","project_id":"grants"}`, false},
		{`{"type":"service_account","project_id":"mailboxes"}`, true},
	} {
		t.Setenv("GOOGLE_WORKSPACE_DELEGATION_KEY", c.key)
		s := NewSettings("an-auth-secret-of-at-least-32-bytes!!", &fakeStates{})
		if got := s.For(t.Context(), id, models.InboxProviderGoogle, models.MailAuthOAuth); got == nil || got.GmailTopic == "" {
			t.Fatalf("a sign-in mailbox must watch the topic, got %+v", got)
		}
		got := s.For(t.Context(), id, models.InboxProviderGoogle, models.MailAuthDelegated)
		if watches := got != nil && got.GmailTopic != ""; watches != c.delegated {
			t.Fatalf("key %q: a granted mailbox watches=%v, want %v", c.key, watches, c.delegated)
		}
	}
}

func TestGmailWatchSkipsGrantsOnAMalformedTopicOrKey(t *testing.T) {
	id := uuid.New()
	for _, c := range []struct{ topic, key string }{
		{"mailboxes", `{"project_id":"mailboxes"}`},
		{"projects/mailboxes/topics/", `{"project_id":"mailboxes"}`},
		{"projects/mailboxes/subscriptions/gmail", `{"project_id":"mailboxes"}`},
		{"projects/mailboxes/topics/gmail", `not json`},
		{"projects/mailboxes/topics/gmail", `{"type":"service_account"}`},
	} {
		t.Setenv("GMAIL_PUSH_TOPIC", c.topic)
		t.Setenv("GOOGLE_WORKSPACE_DELEGATION_KEY", c.key)
		got := NewSettings("an-auth-secret-of-at-least-32-bytes!!", &fakeStates{}).For(t.Context(), id, models.InboxProviderGoogle, models.MailAuthDelegated)
		if got != nil && got.GmailTopic != "" {
			t.Fatalf("topic %q key %q: a granted mailbox was asked to watch", c.topic, c.key)
		}
	}
}

func TestForHandsBackAStaleChannelSoTheWorkerClosesIt(t *testing.T) {
	t.Setenv("GRAPH_PUSH", "")
	t.Setenv("GMAIL_PUSH_TOPIC", "")
	stale := &models.MailboxPushState{Provider: models.MailboxPushGraph, SubscriptionID: "sub", ExpiresAt: time.Now().Add(time.Hour)}
	got := NewSettings("an-auth-secret-of-at-least-32-bytes!!", &fakeStates{state: stale}).For(t.Context(), uuid.New(), models.InboxProviderOutlook, models.MailAuthOAuth)
	if got == nil || got.GraphNotificationURL != "" || got.State == nil || got.State.SubscriptionID != "sub" {
		t.Fatalf("push turned off must still hand back the open channel, got %+v", got)
	}
}

func TestGraphPushNeedsHTTPS(t *testing.T) {
	t.Setenv("GRAPH_PUSH", "true")
	t.Setenv("BACKEND_PUBLIC_URL", "http://localhost:8080")
	if NewSettings("an-auth-secret-of-at-least-32-bytes!!", nil).GraphEnabled() {
		t.Fatal("Graph only delivers to HTTPS, so a plain-HTTP backend must keep polling")
	}
}

type recordingPublisher struct {
	wakes []models.SyncWake
	err   error
}

func (p *recordingPublisher) PublishSyncWake(_ context.Context, _ uuid.UUID, w *models.SyncWake) error {
	p.wakes = append(p.wakes, *w)
	return p.err
}

func TestWakerWakesEveryTargetAndReportsFailures(t *testing.T) {
	targets := []repository.SyncWakeTarget{{EmailID: uuid.New(), WorkerID: uuid.New()}, {EmailID: uuid.New(), WorkerID: uuid.New()}}
	pub := &recordingPublisher{}
	w := NewWaker(&fakeStates{targets: targets}, pub, nil)
	if err := w.WakeGmail(t.Context(), "someone@example.com"); err != nil || len(pub.wakes) != 2 {
		t.Fatalf("woke %d, err %v", len(pub.wakes), err)
	}
	pub.err = errors.New("bus down")
	if err := w.WakeMailboxes(t.Context(), []uuid.UUID{targets[0].EmailID}, true); err == nil {
		t.Fatal("a failed publish must reach the caller so the provider retries")
	}
	if !pub.wakes[len(pub.wakes)-1].Renew {
		t.Fatal("renew was not carried to the worker")
	}
}

func TestGmailNotificationParsing(t *testing.T) {
	if got, ok := GmailAddress([]byte(`{"emailAddress":"a@b.com","historyId":"1"}`)); !ok || got != "a@b.com" {
		t.Fatalf("got %q %v", got, ok)
	}
	if _, ok := GmailAddress([]byte(`{"historyId":"1"}`)); ok {
		t.Fatal("a notification with no address parsed")
	}
	if _, _, err := ParseGmailSubscription("projects/p/subscriptions/s"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ParseGmailSubscription("projects/p/topics/s"); err == nil {
		t.Fatal("a topic name was taken for a subscription")
	}
}
