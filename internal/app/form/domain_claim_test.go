package form

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/customhost"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/domainproof"
	"github.com/warmbly/warmbly/internal/pkg/trackdns"
	"github.com/warmbly/warmbly/internal/repository"
)

type fakeOrgDomains struct {
	domain    string
	verified  bool
	heldBy    map[string]bool
	verifyErr error
	sets      int
}

func (f *fakeOrgDomains) GetByID(context.Context, uuid.UUID) (*models.Organization, error) {
	return nil, nil
}
func (f *fakeOrgDomains) GetFormsDomain(context.Context, uuid.UUID) (string, bool, error) {
	return f.domain, f.verified, nil
}
func (f *fakeOrgDomains) SetFormsDomain(_ context.Context, _ uuid.UUID, d string) error {
	f.sets++
	if d != f.domain {
		f.verified = false
	}
	f.domain = d
	return nil
}
func (f *fakeOrgDomains) SetFormsDomainVerified(_ context.Context, _ uuid.UUID, v bool, _ *time.Time) error {
	if f.verifyErr != nil {
		return f.verifyErr
	}
	f.verified = v
	return nil
}
func (f *fakeOrgDomains) FormsDomainHeldElsewhere(_ context.Context, _ uuid.UUID, d string) (bool, error) {
	return f.heldBy[d], nil
}

type fakeTXT map[string][]string

func (f fakeTXT) LookupTXT(_ context.Context, name string) ([]string, error) { return f[name], nil }

type noOwners struct{}

func (noOwners) Owners(context.Context, string) ([]uuid.UUID, error) { return nil, nil }

// pointsHere stands in for a CNAME that resolves to the forms host.
func pointsHere(t *testing.T) {
	t.Helper()
	t.Setenv("FORMS_DOMAIN", "forms.warmbly.test")
	prev := verifyCNAME
	verifyCNAME = func(_ context.Context, domain, target string) trackdns.Result {
		return trackdns.Result{Domain: domain, Target: target, Verified: true, Code: trackdns.CodeVerified, Method: trackdns.MethodCNAME}
	}
	t.Cleanup(func() { verifyCNAME = prev })
}

func newDomainService(store *fakeOrgDomains, txt fakeTXT, prover *domainproof.Prover) *service {
	s := &service{domains: store}
	s.SetCustomHosts(customhost.New(noOwners{}, prover, txt))
	return s
}

func TestSetFormsDomainRefusesADomainAnotherWorkspaceHolds(t *testing.T) {
	pointsHere(t)
	store := &fakeOrgDomains{heldBy: map[string]bool{"forms.acme.test": true}}
	s := newDomainService(store, fakeTXT{}, domainproof.New("secret"))

	_, xerr := s.SetFormsDomain(context.Background(), uuid.New(), "forms.acme.test")
	if xerr == nil || xerr.ResponseCode() != errx.ErrFormsDomainTaken.ResponseCode() {
		t.Fatalf("got %v, want forms_domain_taken", xerr)
	}
	if store.sets != 0 || store.domain != "" {
		t.Fatal("a refused claim changed the stored domain")
	}

	// A claim that loses the race at write time answers the same.
	store = &fakeOrgDomains{domain: "forms.mine.test", verifyErr: repository.ErrFormsDomainTaken}
	prover := domainproof.New("secret")
	org := uuid.New()
	s = newDomainService(store, fakeTXT{"_warmbly.forms.mine.test": {prover.Value(org, "forms.mine.test")}}, prover)
	if _, xerr := s.VerifyFormsDomain(context.Background(), org); xerr == nil || xerr.ResponseCode() != "forms_domain_taken" {
		t.Fatalf("got %v, want forms_domain_taken", xerr)
	}
}

func TestVerifyFormsDomainRequiresWorkspaceProof(t *testing.T) {
	pointsHere(t)
	prover := domainproof.New("secret")
	org := uuid.New()

	store := &fakeOrgDomains{domain: "forms.acme.test"}
	out, xerr := newDomainService(store, fakeTXT{}, prover).VerifyFormsDomain(context.Background(), org)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if out.FormsDomainVerified || store.verified || out.Status != domainproof.CodeProofMissing {
		t.Fatalf("verified on the CNAME alone: %+v", out)
	}
	if out.TXTName != "_warmbly.forms.acme.test" || out.TXTValue != prover.Value(org, "forms.acme.test") {
		t.Fatalf("status does not show the TXT record: %q %q", out.TXTName, out.TXTValue)
	}

	// Another workspace's value does not count.
	store = &fakeOrgDomains{domain: "forms.acme.test"}
	foreign := fakeTXT{"_warmbly.forms.acme.test": {prover.Value(uuid.New(), "forms.acme.test")}}
	if out, _ := newDomainService(store, foreign, prover).VerifyFormsDomain(context.Background(), org); out.FormsDomainVerified {
		t.Fatal("another workspace's proof verified the domain")
	}

	for name, txt := range map[string]fakeTXT{
		"on the host":   {"_warmbly.forms.acme.test": {prover.Value(org, "forms.acme.test")}},
		"on the parent": {"_warmbly.acme.test": {prover.Value(org, "acme.test")}},
	} {
		store = &fakeOrgDomains{domain: "forms.acme.test"}
		out, xerr := newDomainService(store, txt, prover).VerifyFormsDomain(context.Background(), org)
		if xerr != nil || !out.FormsDomainVerified || !store.verified {
			t.Fatalf("proof %s did not verify: %+v %v", name, out, xerr)
		}
	}
}

func TestVerifiedFormsDomainKeepsWorkingWithoutProof(t *testing.T) {
	pointsHere(t)
	store := &fakeOrgDomains{domain: "forms.acme.test", verified: true}
	s := newDomainService(store, fakeTXT{}, domainproof.New("secret"))

	out, xerr := s.SetFormsDomain(context.Background(), uuid.New(), "forms.acme.test")
	if xerr != nil || !out.FormsDomainVerified || !store.verified {
		t.Fatalf("a verified domain lost its verdict on re-save: %+v %v", out, xerr)
	}
}

// A portal domain never carries forms: they point at, verify against and link on the forms host.
func TestFormsStayOffThePortalDomain(t *testing.T) {
	pointsHere(t)
	t.Setenv("PORTAL_DOMAIN", "portal.warmbly.test")
	var targets []string
	prev := verifyCNAME
	verifyCNAME = func(ctx context.Context, domain, target string) trackdns.Result {
		targets = append(targets, target)
		return prev(ctx, domain, target)
	}
	prover := domainproof.New("secret")
	org := uuid.New()

	store := &fakeOrgDomains{domain: "forms.acme.test"}
	s := newDomainService(store, fakeTXT{"_warmbly.forms.acme.test": {prover.Value(org, "forms.acme.test")}}, prover)
	if got := s.FormsHost(context.Background(), org); got != "forms.warmbly.test" {
		t.Fatalf("unverified form links on %q", got)
	}
	status, _ := s.FormsDomainStatus(context.Background(), org)
	if status.CNAMETarget != "forms.warmbly.test" {
		t.Fatalf("forms domain told to point at %q", status.CNAMETarget)
	}
	out, xerr := s.VerifyFormsDomain(context.Background(), org)
	if xerr != nil || !out.FormsDomainVerified || len(targets) != 1 || targets[0] != "forms.warmbly.test" {
		t.Fatalf("verified against %v: %+v %v", targets, out, xerr)
	}
	if got := s.FormsHost(context.Background(), org); got != "forms.acme.test" {
		t.Fatalf("verified form links on %q", got)
	}
}
