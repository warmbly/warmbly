package email

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"golang.org/x/oauth2"
)

type fakeApps struct {
	cfg *oauth2.Config
	id  uuid.UUID
}

func (f fakeApps) ConfigFor(context.Context, uuid.UUID, models.InboxProvider) (*oauth2.Config, *uuid.UUID, *errx.Error) {
	if f.cfg == nil {
		return nil, nil, nil
	}
	id := f.id
	return f.cfg, &id, nil
}

func (f fakeApps) ConfigByID(_ context.Context, id uuid.UUID) (*oauth2.Config, *errx.Error) {
	if f.cfg == nil || id != f.id {
		return nil, errx.ErrNotFound
	}
	return f.cfg, nil
}

type fakePartnership bool

func (p fakePartnership) HasPartnership(context.Context, uuid.UUID) (bool, *errx.Error) {
	return bool(p), nil
}

func deploymentInbox() *config.Oauth2Inbox {
	g := config.GoogleOauth2Inbox("https://api.example.com")
	g.ClientID, g.ClientSecret = "warmbly.apps.googleusercontent.com", "warmbly-secret"
	o := config.OutlookOauth2Inbox("https://api.example.com")
	o.ClientID, o.ClientSecret = uuid.NewString(), "warmbly-secret"
	return &config.Oauth2Inbox{Google: g, Outlook: o}
}

// On the Partnership plan a sign-in goes through the workspace's own app or not at all.
func TestSigninConfigFollowsThePartnershipRule(t *testing.T) {
	ctx := context.Background()
	org := uuid.New()
	own := &oauth2.Config{ClientID: "acme.apps.googleusercontent.com", ClientSecret: "acme"}
	ownID := uuid.New()

	s := &emailService{oauthInbox: deploymentInbox()}
	s.WireOAuthApps(fakeApps{}, fakePartnership(true))
	if _, _, xerr := s.signinConfig(ctx, &org, models.InboxProviderGoogle); xerr != ErrPartnershipOAuthAppRequired {
		t.Fatalf("partnership without its own app: %v", xerr)
	}
	if xerr := s.RequireOwnOAuthApp(ctx, org); xerr != ErrPartnershipOAuthAppRequired {
		t.Fatalf("RequireOwnOAuthApp on partnership: %v", xerr)
	}

	s.WireOAuthApps(fakeApps{cfg: own, id: ownID}, fakePartnership(true))
	cfg, id, xerr := s.signinConfig(ctx, &org, models.InboxProviderGoogle)
	if xerr != nil || cfg != own || id == nil || *id != ownID {
		t.Fatalf("partnership with its own app: cfg=%v id=%v err=%v", cfg, id, xerr)
	}

	s.WireOAuthApps(fakeApps{}, fakePartnership(false))
	cfg, id, xerr = s.signinConfig(ctx, &org, models.InboxProviderGoogle)
	if xerr != nil || id != nil || cfg.ClientID != "warmbly.apps.googleusercontent.com" {
		t.Fatalf("other plans keep the deployment's app: cfg=%v id=%v err=%v", cfg, id, xerr)
	}
	if xerr := s.RequireOwnOAuthApp(ctx, org); xerr != nil {
		t.Fatalf("RequireOwnOAuthApp off partnership: %v", xerr)
	}
}

// A stored grant refreshes with the client that issued it, whatever the workspace uses now.
func TestGrantConfigUsesTheIssuingClient(t *testing.T) {
	ctx := context.Background()
	own := &oauth2.Config{ClientID: "acme.apps.googleusercontent.com"}
	ownID := uuid.New()
	s := &emailService{oauthInbox: deploymentInbox()}
	s.WireOAuthApps(fakeApps{cfg: own, id: ownID}, fakePartnership(true))

	if cfg, xerr := s.grantConfig(ctx, nil, models.InboxProviderGoogle); xerr != nil || cfg.ClientID != "warmbly.apps.googleusercontent.com" {
		t.Fatalf("a grant from the deployment's app: cfg=%v err=%v", cfg, xerr)
	}
	if cfg, xerr := s.grantConfig(ctx, &ownID, models.InboxProviderGoogle); xerr != nil || cfg != own {
		t.Fatalf("a grant from the workspace's app: cfg=%v err=%v", cfg, xerr)
	}
	other := uuid.New()
	if _, xerr := s.grantConfig(ctx, &other, models.InboxProviderGoogle); xerr == nil {
		t.Fatal("a grant from an unknown app resolved to some client")
	}
}
