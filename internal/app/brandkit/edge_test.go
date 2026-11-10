package brandkit

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/customhost"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/domainproof"
	"github.com/warmbly/warmbly/internal/pkg/trackdns"
	"github.com/warmbly/warmbly/internal/repository"
)

type brandRow struct {
	repository.BrandRepository
	brand *models.AgencyBrand
}

func (b *brandRow) AgencyRole(context.Context, uuid.UUID, uuid.UUID) (models.AgencyRole, error) {
	return models.AgencyRoleOwner, nil
}
func (b *brandRow) GetAgencyBrand(context.Context, uuid.UUID) (*models.AgencyBrand, error) {
	return b.brand, nil
}
func (b *brandRow) PortalHostHeldElsewhere(context.Context, uuid.UUID, string) (bool, error) {
	return false, nil
}
func (b *brandRow) SetAgencyPortalDomainVerified(_ context.Context, _ uuid.UUID, _ string, v bool, at *time.Time) error {
	b.brand.PortalDomainVerified, b.brand.PortalDomainVerifiedAt = v, at
	return nil
}

type portalEdge struct {
	registered []string
	pending    bool
}

func (e *portalEdge) Register(_ context.Context, hosts ...string) {
	e.registered = append(e.registered, hosts...)
}
func (e *portalEdge) Records(_ context.Context, hosts ...string) []models.DNSRecord {
	if !e.pending {
		return nil
	}
	return []models.DNSRecord{{Purpose: "edge", Type: "TXT", Name: "_cf-custom-hostname." + hosts[0], Value: "proof"}}
}

type noOwners struct{}

func (noOwners) Owners(context.Context, string) ([]uuid.UUID, error) { return nil, nil }

type noTXT struct{}

func (noTXT) LookupTXT(context.Context, string) ([]string, error) { return nil, nil }

func TestVerifiedPortalDomainIsRegisteredAtTheEdgeAndListsItsRecord(t *testing.T) {
	t.Setenv("BILLING_PROVIDER", "none")
	t.Setenv("PORTAL_DOMAIN", "portal.warmbly.test")
	agency, user := uuid.New(), uuid.New()
	newSvc := func(row *brandRow, verified bool) *Service {
		s := NewService(row, nil, nil, customhost.New(noOwners{}, domainproof.New("secret"), noTXT{}))
		s.verify = func(_ context.Context, domain, target string) trackdns.Result {
			if !verified {
				return trackdns.Result{Domain: domain, Target: target, Code: trackdns.CodeNotFound}
			}
			return trackdns.Result{Domain: domain, Target: target, Verified: true, Code: trackdns.CodeVerified, Method: trackdns.MethodCNAME}
		}
		return s
	}

	// A domain that does not resolve is not registered.
	edge := &portalEdge{pending: true}
	row := &brandRow{brand: &models.AgencyBrand{AgencyID: agency, PortalDomain: "portal.northwind.test"}}
	s := newSvc(row, false)
	s.WireEdge(edge)
	if view, xerr := s.VerifyPortalDomain(context.Background(), agency, user); xerr != nil || len(edge.registered) != 0 || len(view.PortalDomain.EdgeRecords) != 0 {
		t.Fatalf("an unverified portal domain reached the edge: %v %+v %v", edge.registered, view, xerr)
	}

	// A held domain re-verifies on its CNAME and is registered, listing the edge's TXT until it activates.
	row.brand.PortalDomainVerified = true
	s = newSvc(row, true)
	s.WireEdge(edge)
	view, xerr := s.VerifyPortalDomain(context.Background(), agency, user)
	if xerr != nil || len(edge.registered) != 1 || edge.registered[0] != "portal.northwind.test" {
		t.Fatalf("registered %v, %v", edge.registered, xerr)
	}
	if recs := view.PortalDomain.EdgeRecords; len(recs) != 1 || recs[0].Name != "_cf-custom-hostname.portal.northwind.test" {
		t.Fatalf("verify response records = %+v", recs)
	}
	if got, _ := s.Agency(context.Background(), agency, user); len(got.PortalDomain.EdgeRecords) != 1 {
		t.Fatalf("status records = %+v", got.PortalDomain.EdgeRecords)
	}
	edge.pending = false
	if got, _ := s.Agency(context.Background(), agency, user); len(got.PortalDomain.EdgeRecords) != 0 {
		t.Fatalf("an active portal domain still lists %+v", got.PortalDomain.EdgeRecords)
	}

	// Without an edge the status carries no edge record.
	s = newSvc(row, true)
	if got, _ := s.Agency(context.Background(), agency, user); got.PortalDomain.EdgeRecords != nil {
		t.Fatalf("no edge: %+v", got.PortalDomain.EdgeRecords)
	}
}
