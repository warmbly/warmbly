package connectlink

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type fakePartnerLinks struct {
	link      uuid.UUID
	hint      string
	connected []uuid.UUID
}

func (p *fakePartnerLinks) LoginHint(_ context.Context, id uuid.UUID) string {
	if id != p.link {
		return ""
	}
	return p.hint
}

func (p *fakePartnerLinks) Connected(_ context.Context, id uuid.UUID, acc *models.Email) {
	if id == p.link {
		p.connected = append(p.connected, acc.ID)
	}
}

func (p *fakePartnerLinks) ReturnURL(_ context.Context, id uuid.UUID, status string) (string, bool) {
	if id != p.link {
		return "", false
	}
	return "https://app.partner.test/back?status=" + status, true
}

type creatingRepo struct {
	*fakeRepo
	created *models.ClientConnectLink
}

func (r *creatingRepo) Create(_ context.Context, l *models.ClientConnectLink) (*models.ClientConnectLink, *errx.Error) {
	l.ID = uuid.New()
	r.created = l
	cp := *l
	return &cp, nil
}

// A partner's link connects exactly one mailbox, within the hour, and only through the provider it names.
func TestCreateSingleIsOneMailboxForAnHour(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	repo := &creatingRepo{fakeRepo: &fakeRepo{}}
	s := New(Deps{Repo: repo, Mailboxes: &fakeMailboxes{}, Workspaces: &fakeWorkspaces{canManage: true},
		BaseURL: func(context.Context, uuid.UUID) string { return "https://portal.example.test" }, Now: func() time.Time { return now }})
	out, xerr := s.CreateSingle(context.Background(), uuid.New(), uuid.New(), "Connect link from Partner", models.InboxProviderOutlook, time.Hour)
	if xerr != nil {
		t.Fatal(xerr)
	}
	l := repo.created
	if l.MaxMailboxes != 1 || !l.ExpiresAt.Equal(now.Add(time.Hour)) || len(l.AllowedProviders) != 1 || l.AllowedProviders[0] != "outlook" || l.Warmup {
		t.Fatalf("link = %+v", l)
	}
	if !strings.HasPrefix(out.URL, "https://portal.example.test"+PagePath) {
		t.Fatalf("url = %q", out.URL)
	}
	if _, xerr := s.CreateSingle(context.Background(), uuid.New(), uuid.New(), "x", models.InboxProviderSMTPIMAP, time.Hour); xerr != ErrProviderNotAllowed {
		t.Fatalf("smtp link = %v", xerr)
	}
}

func TestPartnerLinkIsSingleUseAndReturnsToThePartner(t *testing.T) {
	s, repo, mb, _, token := newFixture(t)
	repo.link.MaxMailboxes = 1
	p := &fakePartnerLinks{link: repo.link.ID, hint: "bo@client.test"}
	s.WirePartnerLinks(p)

	link, xerr := s.Resolve(context.Background(), token)
	if xerr != nil {
		t.Fatal(xerr)
	}
	started, xerr := s.StartOAuth(context.Background(), link, models.InboxProviderOutlook, returnURL, "203.0.113.9")
	if xerr != nil {
		t.Fatal(xerr)
	}
	u, _ := url.Parse(started.URL)
	if u.Query().Get("login_hint") != "bo@client.test" {
		t.Fatalf("sign-in url %q carries no login hint", started.URL)
	}
	res, back, xerr := s.FinishOAuth(context.Background(), "outlook", "code", u.Query().Get("state"), "")
	if xerr != nil {
		t.Fatal(xerr)
	}
	if back != "https://app.partner.test/back?status=connected" || len(p.connected) != 1 || p.connected[0] != res.Account.ID {
		t.Fatalf("returned to %q, partner told of %v", back, p.connected)
	}
	// The one mailbox is spent: the link is exhausted for every later visit.
	if _, xerr := s.Resolve(context.Background(), token); xerr == nil || xerr.ResponseCode() != "client_link_exhausted" {
		t.Fatalf("second visit = %v", xerr)
	}
	if mb.calls != 1 {
		t.Fatalf("connected %d times", mb.calls)
	}
}

func TestPartnerLinkDeclinedReturnsCancelled(t *testing.T) {
	s, repo, _, _, token := newFixture(t)
	p := &fakePartnerLinks{link: repo.link.ID}
	s.WirePartnerLinks(p)
	state := startOutlook(t, s, token)
	_, back, xerr := s.FinishOAuth(context.Background(), "outlook", "", state, "access_denied")
	if xerr != ErrSigninDeclined || back != "https://app.partner.test/back?status=cancelled" || len(p.connected) != 0 {
		t.Fatalf("declined = %q %v %v", back, xerr, p.connected)
	}
}
