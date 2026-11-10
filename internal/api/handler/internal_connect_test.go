package handler

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/customhost"
	"github.com/warmbly/warmbly/internal/models"
)

// A sign-in returns to the connect page on the shared portal address or on the link's own agency portal domain, never on a forms host.
func TestConnectSigninReturnsOnlyToThePortalService(t *testing.T) {
	t.Setenv("FORMS_DOMAIN", "forms.warmbly.test")
	t.Setenv("PORTAL_DOMAIN", "portal.warmbly.test")
	acme, other := uuid.New(), uuid.New()
	h := &Handler{CustomHosts: customhost.New(stubOwners{"portal.agency.test": {acme}, "portal.rival.test": {other}}, nil, nil)}
	link := &models.ClientConnectLink{OrganizationID: acme}
	for host, want := range map[string]string{
		"portal.warmbly.test": "https://portal.warmbly.test/connect/tok",
		"Portal.Agency.test":  "https://portal.agency.test/connect/tok",
		"portal.rival.test":   "",
		"forms.warmbly.test":  "",
		"unknown.test":        "",
		"":                    "",
	} {
		if got := h.connectReturnURL(context.Background(), host, link, "tok"); got != want {
			t.Errorf("return on %q = %q, want %q", host, got, want)
		}
	}
}
