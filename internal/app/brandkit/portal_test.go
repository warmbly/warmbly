package brandkit

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

type portalRows struct {
	rows  map[string]*models.AgencyBrand
	calls int
}

func (p *portalRows) VerifiedPortal(_ context.Context, host string) (*models.AgencyBrand, string, error) {
	p.calls++
	return p.rows[host], "Northwind Growth", nil
}

func TestPortalsAnswerOnlyVerifiedHostsAndCache(t *testing.T) {
	t.Setenv("FORMS_DOMAIN", "forms.warmbly.test")
	t.Setenv("PORTAL_DOMAIN", "portal.warmbly.test")
	agency := uuid.New()
	rows := &portalRows{rows: map[string]*models.AgencyBrand{
		"portal.northwind.test":  {AgencyID: agency, PortalDomain: "portal.northwind.test", PortalDomainVerified: true},
		"pending.northwind.test": {AgencyID: agency, PortalDomain: "pending.northwind.test"},
	}}
	p := NewPortals(rows)
	ctx := context.Background()
	got, err := p.Lookup(ctx, "Portal.Northwind.test.")
	if err != nil || got == nil || got.AgencyID != agency || got.AgencyName != "Northwind Growth" {
		t.Fatalf("verified portal = %+v, %v", got, err)
	}
	if _, _ = p.Lookup(ctx, "portal.northwind.test"); rows.calls != 1 {
		t.Fatalf("a cached answer read the database again (%d calls)", rows.calls)
	}
	for _, host := range []string{"pending.northwind.test", "unknown.test", "portal.warmbly.test", "forms.warmbly.test", "localhost", ""} {
		if p.Verified(host) {
			t.Fatalf("%q verified", host)
		}
	}
	p.Invalidate()
	if !p.Verified("portal.northwind.test") || rows.calls < 3 {
		t.Fatal("invalidate must drop cached answers")
	}
}

// Connect links and reports are built on the agency's verified portal domain, else the shared portal address,
// never on a forms host, and nowhere at all without PORTAL_DOMAIN.
func TestClientPagesAreBuiltOnThePortalService(t *testing.T) {
	t.Setenv("FORMS_DOMAIN", "forms.warmbly.test")
	agency := Resolution{PortalHost: "portal.northwind.test"}
	t.Setenv("PORTAL_DOMAIN", "portal.warmbly.test")
	for _, tc := range []struct {
		res        Resolution
		host, from string
	}{
		{agency, "portal.northwind.test", "agency"},
		{Resolution{}, "portal.warmbly.test", "shared"},
	} {
		if host, from := portalOf(tc.res); host != tc.host || from != tc.from {
			t.Fatalf("portalOf(%+v) = %q, %q; want %q, %q", tc.res, host, from, tc.host, tc.from)
		}
	}
	t.Setenv("PORTAL_DOMAIN", "")
	if host, _ := portalOf(agency); host != "" {
		t.Fatalf("built on %q without a portal service", host)
	}
	var none *Service
	if got := none.PortalBaseURL(context.Background(), uuid.New()); got != "" {
		t.Fatalf("nil service base = %q", got)
	}
}
