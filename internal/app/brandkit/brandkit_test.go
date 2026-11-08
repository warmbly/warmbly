package brandkit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

var deployment = Brand{Name: "Warmbly", WebsiteURL: "https://warmbly.com", PoweredBy: &PoweredBy{Name: "Warmbly", URL: "https://warmbly.com"}}

func TestComposePrecedence(t *testing.T) {
	ws := &models.BrandSettings{DisplayName: "Client Co", AccentColor: "#112233"}
	agency := &models.AgencyBrand{AgencyID: uuid.New(), BrandSettings: models.BrandSettings{DisplayName: "Agency", LogoURL: "https://cdn/x.png"}}

	if got := Compose(deployment, nil, nil, false); got.Source != SourceDeployment || got.Brand.Name != "Warmbly" || got.Custom() {
		t.Fatalf("no brands: %+v", got)
	}
	if got := Compose(deployment, nil, agency, false); got.Source != SourceAgency || got.Brand.Name != "Agency" || got.Brand.LogoURL != "https://cdn/x.png" {
		t.Fatalf("agency only: %+v", got)
	}
	got := Compose(deployment, ws, agency, false)
	if got.Source != SourceWorkspace || got.Brand.Name != "Client Co" || got.Brand.AccentColor != "#112233" {
		t.Fatalf("workspace over agency: %+v", got)
	}
	// A workspace brand replaces the agency's whole: no logo bleeds through.
	if got.Brand.LogoURL != "" {
		t.Fatalf("agency logo leaked into workspace brand: %q", got.Brand.LogoURL)
	}
	if got.AgencyID == nil || *got.AgencyID != agency.AgencyID {
		t.Fatalf("agency id not carried: %+v", got.AgencyID)
	}
	// A stored row that names nothing does not replace the agency's.
	if got := Compose(deployment, &models.BrandSettings{HidePoweredBy: true}, agency, true); got.Source != SourceAgency {
		t.Fatalf("empty workspace row should inherit: %+v", got)
	}
}

func TestComposePortalHostOnlyWhenVerified(t *testing.T) {
	agency := &models.AgencyBrand{AgencyID: uuid.New(), PortalDomain: "portal.agency.com"}
	if got := Compose(deployment, nil, agency, false); got.PortalHost != "" {
		t.Fatalf("unverified portal host used: %q", got.PortalHost)
	}
	agency.PortalDomainVerified = true
	if got := Compose(deployment, nil, agency, false); got.PortalHost != "portal.agency.com" {
		t.Fatalf("verified portal host missing: %q", got.PortalHost)
	}
}

func TestHidePoweredByNeedsEntitlement(t *testing.T) {
	ws := &models.BrandSettings{DisplayName: "Client Co", HidePoweredBy: true}
	if got := Compose(deployment, ws, nil, false); got.Brand.PoweredBy == nil {
		t.Fatal("powered by removed without entitlement")
	}
	if got := Compose(deployment, ws, nil, true); got.Brand.PoweredBy != nil {
		t.Fatal("powered by kept despite entitlement")
	}
	agency := &models.AgencyBrand{BrandSettings: models.BrandSettings{DisplayName: "Agency", HidePoweredBy: true}}
	if got := Compose(deployment, nil, agency, false); got.Brand.PoweredBy == nil {
		t.Fatal("inherited hide honoured without entitlement")
	}

	if err := CheckHidePoweredBy(true, false, false); err == nil || err.Identifier != "white_label_not_entitled" {
		t.Fatalf("turning it off unentitled: %v", err)
	}
	if err := CheckHidePoweredBy(true, true, false); err != nil {
		t.Fatalf("keeping a stored value after a downgrade must save: %v", err)
	}
	if err := CheckHidePoweredBy(true, false, true); err != nil {
		t.Fatalf("entitled: %v", err)
	}
}

func TestFullWhiteLabelPlans(t *testing.T) {
	t.Setenv("BILLING_PROVIDER", "stripe")
	future := time.Now().Add(24 * time.Hour)
	stripeID := "sub_1"
	active := &models.Subscription{Status: models.SubscriptionStatusActive, PlanID: uuid.New(), CurrentPeriodEnd: &future, StripeSubscriptionID: &stripeID}
	agency := &models.Plan{ID: models.AgencyPlanID, DedicatedWorkers: 0}
	pro := &models.Plan{ID: models.AgencyProPlanID, DedicatedWorkers: 1}

	if FullWhiteLabel(active, agency) {
		t.Fatal("Agency plan must not remove powered by")
	}
	if !FullWhiteLabel(active, pro) {
		t.Fatal("Agency Pro must remove powered by")
	}
	enterprise := *active
	enterprise.IsEnterprise = true
	if !FullWhiteLabel(&enterprise, agency) {
		t.Fatal("enterprise must remove powered by")
	}
	if FullWhiteLabel(nil, pro) {
		t.Fatal("no subscription is not entitled")
	}
	canceled := *active
	canceled.Status = models.SubscriptionStatusCanceled
	if FullWhiteLabel(&canceled, pro) {
		t.Fatal("canceled subscription is not entitled")
	}

	t.Setenv("BILLING_PROVIDER", "none")
	if !FullWhiteLabel(nil, nil) {
		t.Fatal("self-host is always entitled")
	}
}

func TestNormalizeAccent(t *testing.T) {
	for in, want := range map[string]string{"": "", "#0284C7": "#0284c7", " #abcdef ": "#abcdef"} {
		if got, ok := NormalizeAccent(in); !ok || got != want {
			t.Errorf("NormalizeAccent(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"0284c7", "#fff", "#0284c7ff", "red", "#gggggg", "#0284c7;}", "url(x)"} {
		if _, ok := NormalizeAccent(in); ok {
			t.Errorf("NormalizeAccent(%q) accepted", in)
		}
	}
}

func TestNormalizeWebsite(t *testing.T) {
	for in, want := range map[string]string{"": "", "https://agency.com": "https://agency.com", " https://agency.com/about ": "https://agency.com/about"} {
		if got, ok := NormalizeWebsite(in); !ok || got != want {
			t.Errorf("NormalizeWebsite(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"http://agency.com", "agency.com", "javascript:alert(1)", "https://user:pw@agency.com", "https://localhost", "https://", "ftp://agency.com"} {
		if _, ok := NormalizeWebsite(in); ok {
			t.Errorf("NormalizeWebsite(%q) accepted", in)
		}
	}
}

func TestNormalizeInputRefusesLinkNames(t *testing.T) {
	if _, err := NormalizeInput(models.BrandInput{DisplayName: "visit evil.com"}); err == nil || err.Identifier != "invalid_name" {
		t.Fatalf("link in name: %v", err)
	}
	out, err := NormalizeInput(models.BrandInput{DisplayName: "  Acme   Growth ", AccentColor: "#ABCDEF", WebsiteURL: "https://acme.io"})
	if err != nil || out.DisplayName != "Acme Growth" || out.AccentColor != "#abcdef" {
		t.Fatalf("normalize: %+v %v", out, err)
	}
}

type fakeStore struct {
	ws     *models.BrandSettings
	agency *models.AgencyBrand
	reads  int
	err    error
}

func (f *fakeStore) GetWorkspaceBrand(context.Context, uuid.UUID) (*models.BrandSettings, error) {
	f.reads++
	return f.ws, f.err
}

func (f *fakeStore) AgencyBrandForOrganization(context.Context, uuid.UUID) (*models.AgencyBrand, error) {
	return f.agency, nil
}

type fixedEntitlement bool

func (e fixedEntitlement) FullWhiteLabel(context.Context, uuid.UUID) (bool, error) {
	return bool(e), nil
}

func TestDBResolverCachesAndInvalidates(t *testing.T) {
	t.Setenv("BILLING_PROVIDER", "stripe")
	store := &fakeStore{ws: &models.BrandSettings{DisplayName: "One"}}
	r := NewDBResolver(store, fixedEntitlement(false))
	now := time.Now()
	r.now = func() time.Time { return now }
	org := uuid.New()

	b, _ := r.ForOrganization(context.Background(), org)
	store.ws = &models.BrandSettings{DisplayName: "Two"}
	if b2, _ := r.ForOrganization(context.Background(), org); b.Name != "One" || b2.Name != "One" || store.reads != 1 {
		t.Fatalf("cache miss: %q %q reads=%d", b.Name, b2.Name, store.reads)
	}
	r.Invalidate(org)
	if b, _ := r.ForOrganization(context.Background(), org); b.Name != "Two" {
		t.Fatalf("invalidate: %q", b.Name)
	}
	store.ws = &models.BrandSettings{DisplayName: "Three"}
	now = now.Add(resolveTTL + time.Second)
	if b, _ := r.ForOrganization(context.Background(), org); b.Name != "Three" {
		t.Fatalf("expiry: %q", b.Name)
	}
}

func TestDBResolverFailureShowsDeploymentBrand(t *testing.T) {
	store := &fakeStore{err: errors.New("down")}
	r := NewDBResolver(store, nil)
	res, err := r.Resolve(context.Background(), uuid.New())
	if err == nil || res.Source != SourceDeployment {
		t.Fatalf("failure: %+v %v", res, err)
	}
	var nilResolver *DBResolver
	if res, err := nilResolver.Resolve(context.Background(), uuid.New()); err != nil || res.Source != SourceDeployment {
		t.Fatalf("nil resolver: %+v %v", res, err)
	}
}
