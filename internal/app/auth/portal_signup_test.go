package auth

import (
	"context"
	"testing"

	"github.com/warmbly/warmbly/internal/app/organization"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

type previewOrgs struct {
	organization.OrganizationService
	email string
}

func (o previewOrgs) PreviewInvitation(context.Context, string) (*models.InvitationPreview, *errx.Error) {
	return &models.InvitationPreview{Email: o.email}, nil
}

// A signup sent from a portal needs an invitation for that address; one from the main address does not.
func TestPortalSignupNeedsAnInvitation(t *testing.T) {
	t.Setenv("APP_URL", "https://app.example.test")
	const portalHost = "portal.agency.test"
	config.SetPortalHosts(func(host string) bool { return host == portalHost })
	t.Cleanup(func() { config.SetPortalHosts(nil) })

	s := &authService{policy: &config.AuthPolicy{Registration: config.RegistrationOpen}, organizationService: previewOrgs{email: "invited@client.test"}}
	onPortal := config.WithDashboardOrigin(context.Background(), "https://"+portalHost)
	onMain := config.WithDashboardOrigin(context.Background(), "https://app.example.test")

	if _, xerr := s.RegistrationStart(onPortal, &AuthData{Email: "someone@client.test"}, SignupOrigin{}); xerr != ErrPortalRegistrationClosed {
		t.Fatalf("plain portal signup = %v, want %v", xerr, ErrPortalRegistrationClosed)
	}
	if xerr := s.portalSignupAllowed(onPortal, "someone@client.test", "tok"); xerr != errx.ErrInvitationInvalid {
		t.Fatalf("invitation for another address = %v, want %v", xerr, errx.ErrInvitationInvalid)
	}
	if xerr := s.portalSignupAllowed(onPortal, "invited@client.test", "tok"); xerr != nil {
		t.Fatalf("invited portal signup refused: %v", xerr)
	}
	if xerr := s.portalSignupAllowed(onMain, "someone@client.test", ""); xerr != nil {
		t.Fatalf("main-address signup refused: %v", xerr)
	}
}
