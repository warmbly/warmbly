package mailboxconnect

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func intp(v int) *int { return &v }

func TestCursorRoundTrips(t *testing.T) {
	l := &models.MailboxConnectLink{ID: uuid.New(), CreatedAt: time.Date(2026, 11, 9, 12, 0, 0, 123456789, time.UTC)}
	c, ok := decodeCursor(encodeCursor(l))
	if !ok || c.ID != l.ID || !c.CreatedAt.Equal(l.CreatedAt) {
		t.Fatalf("cursor round trip = %+v, %v", c, ok)
	}
	for _, bad := range []string{"", "!!", "bm90LWEtY3Vyc29y"} {
		if _, ok := decodeCursor(bad); ok {
			t.Errorf("decodeCursor(%q) accepted", bad)
		}
	}
}

func TestValidateWarmupRefusesWhatSettingsWouldRefuse(t *testing.T) {
	ok := []*models.MailboxConnectWarmup{nil, {Enabled: true}, {Enabled: true, Start: intp(5), Max: intp(40), Increase: intp(1), ReplyRate: intp(100)}}
	for _, w := range ok {
		if xerr := ValidateWarmup(w); xerr != nil {
			t.Errorf("ValidateWarmup(%+v) = %v", w, xerr)
		}
	}
	bad := []*models.MailboxConnectWarmup{{Start: intp(0)}, {Max: intp(-1)}, {Increase: intp(0)}, {ReplyRate: intp(0)}, {ReplyRate: intp(101)}}
	for _, w := range bad {
		if xerr := ValidateWarmup(w); xerr != ErrWarmup {
			t.Errorf("ValidateWarmup(%+v) = %v, want ErrWarmup", w, xerr)
		}
	}
}

func TestReturnToCarriesTheOutcome(t *testing.T) {
	s := &Service{now: time.Now}
	id, acc := uuid.New(), uuid.New()
	l := &models.MailboxConnectLink{ID: id, ReturnURL: "https://app.acme.com/done?tab=mail", EmailAccountID: &acc}
	u, err := url.Parse(s.returnTo(l, "connected", ""))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "app.acme.com" || q.Get("tab") != "mail" || q.Get("warmbly_link") != id.String() ||
		q.Get("status") != "connected" || q.Get("email_account_id") != acc.String() || q.Has("code") {
		t.Fatalf("connected return = %s", u)
	}
	l.EmailAccountID = nil
	u, _ = url.Parse(s.returnTo(l, "failed", "connect_link_wrong_mailbox"))
	if u.Query().Get("code") != "connect_link_wrong_mailbox" || u.Query().Has("email_account_id") {
		t.Fatalf("failed return = %s", u)
	}
	if got := s.returnTo(&models.MailboxConnectLink{ID: id}, "connected", ""); got != "" {
		t.Fatalf("a link without a return URL returned %q", got)
	}
}

func TestEffectiveStatusExpiresOnlyUnfinishedLinks(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Minute)
	for status, want := range map[string]string{
		models.ConnectLinkPending:    models.ConnectLinkExpired,
		models.ConnectLinkConnecting: models.ConnectLinkExpired,
		models.ConnectLinkConnected:  models.ConnectLinkConnected,
		models.ConnectLinkRevoked:    models.ConnectLinkRevoked,
	} {
		l := &models.MailboxConnectLink{Status: status, ExpiresAt: past}
		if got := l.EffectiveStatus(now); got != want {
			t.Errorf("%s past expiry reads %s, want %s", status, got, want)
		}
	}
}

func TestBindingHashIsStableAndDistinct(t *testing.T) {
	if bindingHash("abc") != bindingHash("abc") || bindingHash("abc") == bindingHash("abd") {
		t.Fatal("binding hash is not a function of the cookie")
	}
	if !IsState(StatePrefix+"x") || IsState("w.x") || IsState("vc.x") {
		t.Fatal("state prefix routing")
	}
}

// refuseRepo fails the test if a refused request reaches the store.
type refuseRepo struct {
	repository.MailboxConnectLinkRepository
	t *testing.T
}

func (r refuseRepo) CreateConnectLink(context.Context, *models.MailboxConnectLink, []byte) (*models.MailboxConnectLink, error) {
	r.t.Fatal("a refused link reached the store")
	return nil, nil
}

type noMailboxes struct{ Mailboxes }

func TestCreateLinkValidatesBeforeStoring(t *testing.T) {
	s := New(refuseRepo{t: t}, noMailboxes{}, nil, nil, nil, nil, "https://api.example.com")
	org, user := uuid.New(), uuid.New()
	cases := map[string]struct {
		req  models.NewMailboxConnectLink
		want *errx.Error
	}{
		"smtp provider":   {models.NewMailboxConnectLink{Provider: models.InboxProviderSMTPIMAP}, ErrLinkProvider},
		"two addresses":   {models.NewMailboxConnectLink{Provider: models.InboxProviderGoogle, Email: "a@x.com, b@x.com"}, ErrLinkEmail},
		"named address":   {models.NewMailboxConnectLink{Provider: models.InboxProviderGoogle, Email: "Ann <a@x.com>"}, ErrLinkEmail},
		"http return":     {models.NewMailboxConnectLink{Provider: models.InboxProviderGoogle, ReturnURL: "http://acme.com/x"}, ErrLinkReturnURL},
		"too long":        {models.NewMailboxConnectLink{Provider: models.InboxProviderOutlook, ExpiresInHours: MaxLinkHours + 1}, ErrLinkExpiry},
		"negative expiry": {models.NewMailboxConnectLink{Provider: models.InboxProviderOutlook, ExpiresInHours: -1}, ErrLinkExpiry},
		"bad warmup":      {models.NewMailboxConnectLink{Provider: models.InboxProviderGoogle, Warmup: &models.MailboxConnectWarmup{ReplyRate: intp(200)}}, ErrWarmup},
	}
	for name, c := range cases {
		if _, xerr := s.CreateLink(context.Background(), org, user, c.req); xerr != c.want {
			t.Errorf("%s: %v, want %v", name, xerr, c.want)
		}
	}
}

func TestLiveRefusesUnusableSecrets(t *testing.T) {
	s := New(refuseRepo{t: t}, nil, nil, nil, nil, nil, "")
	for _, token := range []string{"", "short", string(make([]byte, 200))} {
		if _, xerr := s.live(context.Background(), token); xerr != ErrLinkGone {
			t.Errorf("live(%d bytes) = %v", len(token), xerr)
		}
	}
}
