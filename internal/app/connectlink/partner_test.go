package connectlink

import (
	"context"
	"errors"
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
	required  string
	lookupErr error
	connected []uuid.UUID
}

func (p *fakePartnerLinks) RequiredAddress(_ context.Context, id uuid.UUID) (string, error) {
	if id != p.link {
		return "", nil
	}
	return p.required, p.lookupErr
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

// A store sign-in link is for one mailbox: signing in as another account (a tenant's own
// admin@….onmicrosoft.com) connects nothing, tells the person who to sign in as, and leaves the
// link usable for the right account.
func TestRequiredAddressRefusesAnotherAccountAndKeepsTheLink(t *testing.T) {
	s, repo, mb, _, token := newFixture(t)
	repo.link.MaxMailboxes = 1
	p := &fakePartnerLinks{link: repo.link.ID, hint: "jordan@client.test", required: "jordan@client.test"}
	s.WirePartnerLinks(p)

	mb.owner = "admin@client.onmicrosoft.com"
	state := startHinted(t, s, token)
	if _, _, xerr := s.FinishOAuth(context.Background(), "outlook", "code", state, ""); xerr == nil || xerr.ResponseCode() != "signed_in_as_other" {
		t.Fatalf("wrong account = %v", xerr)
	}
	if mb.gotAddress != "jordan@client.test" || len(p.connected) != 0 || len(repo.uses) != 0 {
		t.Fatalf("address %q, partner told of %v, uses %v", mb.gotAddress, p.connected, repo.uses)
	}
	if _, xerr := s.Resolve(context.Background(), token); xerr != nil {
		t.Fatalf("link after a refused sign-in = %v", xerr)
	}

	mb.owner = "Jordan@Client.test"
	res, _, xerr := s.FinishOAuth(context.Background(), "outlook", "code", startHinted(t, s, token), "")
	if xerr != nil || len(p.connected) != 1 || p.connected[0] != res.Account.ID {
		t.Fatalf("right account = %v, partner told of %v", xerr, p.connected)
	}
}

// A partner API link's hint only pre-fills: any account may still sign in.
func TestPartnerHintAloneAcceptsAnyAccount(t *testing.T) {
	s, repo, mb, _, token := newFixture(t)
	p := &fakePartnerLinks{link: repo.link.ID, hint: "bo@client.test"}
	s.WirePartnerLinks(p)
	mb.owner = "someone@else.test"
	if _, _, xerr := s.FinishOAuth(context.Background(), "outlook", "code", startHinted(t, s, token), ""); xerr != nil || mb.gotAddress != "" {
		t.Fatalf("hint-only link = %v (address %q)", xerr, mb.gotAddress)
	}
}

// startHinted starts an Outlook sign-in on a link whose sign-in URL carries a login hint.
func startHinted(t *testing.T, s *Service, token string) string {
	t.Helper()
	link, xerr := s.Resolve(context.Background(), token)
	if xerr != nil {
		t.Fatal(xerr)
	}
	started, xerr := s.StartOAuth(context.Background(), link, models.InboxProviderOutlook, returnURL, "203.0.113.9")
	if xerr != nil {
		t.Fatal(xerr)
	}
	u, _ := url.Parse(started.URL)
	return u.Query().Get("state")
}

// When the link's mailbox cannot be looked up, nobody is let in: nothing is stored and the link stays usable.
func TestRequiredAddressLookupFailureRefusesTheSignIn(t *testing.T) {
	s, repo, mb, _, token := newFixture(t)
	repo.link.MaxMailboxes = 1
	p := &fakePartnerLinks{link: repo.link.ID, hint: "jordan@client.test", lookupErr: errors.New("db down")}
	s.WirePartnerLinks(p)
	if _, _, xerr := s.FinishOAuth(context.Background(), "outlook", "code", startHinted(t, s, token), ""); xerr == nil {
		t.Fatal("a failed lookup let the sign-in through")
	}
	if mb.calls != 0 || len(p.connected) != 0 || repo.link.MailboxesConnected != 0 {
		t.Fatalf("calls %d, partner told of %v, used %d", mb.calls, p.connected, repo.link.MailboxesConnected)
	}
}
