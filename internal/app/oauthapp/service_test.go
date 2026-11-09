package oauthapp

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"golang.org/x/oauth2"
)

type memRepo struct {
	apps map[models.InboxProvider]*models.MailboxOAuthApp
	used bool
}

func (m *memRepo) ListMailboxOAuthApps(context.Context, uuid.UUID) ([]*models.MailboxOAuthApp, error) {
	out := []*models.MailboxOAuthApp{}
	for _, a := range m.apps {
		c := *a
		out = append(out, &c)
	}
	return out, nil
}

func (m *memRepo) GetMailboxOAuthApp(_ context.Context, _ uuid.UUID, p models.InboxProvider) (*models.MailboxOAuthApp, error) {
	if a, ok := m.apps[p]; ok {
		c := *a
		return &c, nil
	}
	return nil, nil
}

func (m *memRepo) GetMailboxOAuthAppByID(_ context.Context, id uuid.UUID) (*models.MailboxOAuthApp, error) {
	for _, a := range m.apps {
		if a.ID == id {
			c := *a
			return &c, nil
		}
	}
	return nil, nil
}

func (m *memRepo) UpsertMailboxOAuthApp(_ context.Context, app *models.MailboxOAuthApp) (*models.MailboxOAuthApp, error) {
	if cur, ok := m.apps[app.Provider]; ok && m.used && cur.ClientID != app.ClientID {
		return nil, repository.ErrMailboxOAuthAppInUse
	}
	c := *app
	if cur, ok := m.apps[app.Provider]; ok {
		c.ID = cur.ID
	} else {
		c.ID = uuid.New()
	}
	c.CustomRedirect = c.RedirectURL != ""
	m.apps[app.Provider] = &c
	out := c
	return &out, nil
}

func (m *memRepo) DeleteMailboxOAuthApp(_ context.Context, _ uuid.UUID, p models.InboxProvider) (bool, error) {
	if m.used {
		return false, repository.ErrMailboxOAuthAppInUse
	}
	_, ok := m.apps[p]
	delete(m.apps, p)
	return ok, nil
}

// testService has no cipher, so a save that reaches sealing fails; every test here stops before it.
type testService struct {
	*service
	repo *memRepo
}

func newTestService(t *testing.T) *testService {
	t.Helper()
	repo := &memRepo{apps: map[models.InboxProvider]*models.MailboxOAuthApp{}}
	defaults := &config.Oauth2Inbox{
		Google:  config.GoogleOauth2Inbox("https://api.example.com"),
		Outlook: config.OutlookOauth2Inbox("https://api.example.com"),
	}
	s := &service{repo: repo, defaults: defaults, probe: func(context.Context, *oauth2.Config) error { return nil }}
	return &testService{service: s, repo: repo}
}

func TestValidClientIDFollowsTheProvider(t *testing.T) {
	cases := []struct {
		provider models.InboxProvider
		id       string
		want     bool
	}{
		{models.InboxProviderGoogle, "123-abc.apps.googleusercontent.com", true},
		{models.InboxProviderGoogle, "123-abc", false},
		{models.InboxProviderGoogle, "123 abc.apps.googleusercontent.com", false},
		{models.InboxProviderOutlook, uuid.NewString(), true},
		{models.InboxProviderOutlook, "123-abc.apps.googleusercontent.com", false},
		{models.InboxProviderOutlook, "", false},
	}
	for _, c := range cases {
		if got := validClientID(c.provider, c.id); got != c.want {
			t.Errorf("validClientID(%s, %q) = %v, want %v", c.provider, c.id, got, c.want)
		}
	}
}

func TestValidRedirectNeedsHTTPSAndAPath(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://acme.com/oauth/callback":        true,
		"https://acme.com/oauth/callback?x=1":    true,
		"http://acme.com/oauth/callback":         false,
		"https://acme.com/":                      false,
		"https://acme.com":                       false,
		"https://user@acme.com/oauth/callback":   false,
		"https://acme.com/oauth/callback#frag":   false,
		"javascript:alert(1)//acme.com/callback": false,
	} {
		if got := validRedirect(raw); got != want {
			t.Errorf("validRedirect(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestConfigFromKeepsScopesAndOverridesOnlyTheClient(t *testing.T) {
	s := newTestService(t)
	base := s.base(models.InboxProviderGoogle)
	cfg := s.configFrom(base, "id.apps.googleusercontent.com", "secret", "")
	if cfg.ClientID != "id.apps.googleusercontent.com" || cfg.ClientSecret != "secret" {
		t.Fatalf("client not applied: %+v", cfg)
	}
	if cfg.RedirectURL != base.RedirectURL || len(cfg.Scopes) != len(base.Scopes) || cfg.Endpoint != base.Endpoint {
		t.Fatalf("deployment callback, scopes or endpoint changed: %+v", cfg)
	}
	cfg.Scopes[0] = "changed"
	if base.Scopes[0] == "changed" {
		t.Fatal("the workspace config shares its scope slice with the deployment's")
	}
	if got := s.configFrom(base, "x", "y", "https://acme.com/cb").RedirectURL; got != "https://acme.com/cb" {
		t.Fatalf("custom redirect = %q", got)
	}
}

func TestSaveRefusesBeforeTouchingTheStore(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	org, user := uuid.New(), uuid.New()
	good := models.MailboxOAuthAppInput{ClientID: "1-a.apps.googleusercontent.com", ClientSecret: "s", AppName: "Acme Mail"}

	if _, xerr := s.Save(ctx, org, user, models.InboxProviderSMTPIMAP, good); xerr != ErrProvider {
		t.Fatalf("smtp provider: %v", xerr)
	}
	bad := good
	bad.ClientID = "nope"
	if _, xerr := s.Save(ctx, org, user, models.InboxProviderGoogle, bad); xerr != ErrClientID {
		t.Fatalf("bad client id: %v", xerr)
	}
	noSecret := good
	noSecret.ClientSecret = ""
	if _, xerr := s.Save(ctx, org, user, models.InboxProviderGoogle, noSecret); xerr != ErrSecret {
		t.Fatalf("a new app without a secret: %v", xerr)
	}
	link := good
	link.AppName = "acme.com"
	if _, xerr := s.Save(ctx, org, user, models.InboxProviderGoogle, link); xerr == nil {
		t.Fatal("an app name that reads as a web address was accepted")
	}
	redirect := good
	redirect.RedirectURL = "http://acme.com/cb"
	if _, xerr := s.Save(ctx, org, user, models.InboxProviderGoogle, redirect); xerr != ErrRedirect {
		t.Fatalf("plain-http redirect: %v", xerr)
	}
	if len(s.repo.apps) != 0 {
		t.Fatalf("a refused save wrote %d apps", len(s.repo.apps))
	}
}

func TestSaveRefusesAClientTheProviderRejects(t *testing.T) {
	s := newTestService(t)
	s.probe = func(context.Context, *oauth2.Config) error { return errClientRejected }
	_, xerr := s.Save(context.Background(), uuid.New(), uuid.New(), models.InboxProviderGoogle,
		models.MailboxOAuthAppInput{ClientID: "1-a.apps.googleusercontent.com", ClientSecret: "s", AppName: "Acme Mail"})
	if xerr != ErrRejected {
		t.Fatalf("rejected client: %v", xerr)
	}
}

func TestDeleteRefusesWhileMailboxesHoldGrants(t *testing.T) {
	s := newTestService(t)
	s.repo.used = true
	if xerr := s.Delete(context.Background(), uuid.New(), models.InboxProviderGoogle); xerr != ErrInUse {
		t.Fatalf("delete in use: %v", xerr)
	}
}

func TestCallbacksAreTheDeploymentsOwn(t *testing.T) {
	s := newTestService(t)
	cb := s.Callbacks()
	if cb[models.InboxProviderGoogle] != "https://api.example.com/addresses/google/callback" ||
		cb[models.InboxProviderOutlook] != "https://api.example.com/addresses/outlook/callback" {
		t.Fatalf("callbacks = %v", cb)
	}
}

// An app imported without credentials carries no secret: it refuses sign-ins and a secret-less save until one is entered.
func TestImportedAppWithoutSecret(t *testing.T) {
	s := newTestService(t)
	org := uuid.New()
	s.repo.apps[models.InboxProviderGoogle] = &models.MailboxOAuthApp{
		ID: uuid.New(), OrganizationID: org, Provider: models.InboxProviderGoogle,
		ClientID: "1-a.apps.googleusercontent.com", AppName: "Acme Mail",
	}
	if _, _, xerr := s.ConfigFor(context.Background(), org, models.InboxProviderGoogle); xerr != ErrNoSecret {
		t.Fatalf("ConfigFor without a secret = %v", xerr)
	}
	_, xerr := s.Save(context.Background(), org, uuid.New(), models.InboxProviderGoogle,
		models.MailboxOAuthAppInput{ClientID: "1-a.apps.googleusercontent.com", AppName: "Acme Mail"})
	if xerr != ErrSecret {
		t.Fatalf("keeping a secret that is not there = %v", xerr)
	}
}
