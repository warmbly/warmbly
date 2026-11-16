package auth

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/app/partners"
	"github.com/warmbly/warmbly/internal/models"
)

type recordedAcquisition struct{ rows []models.OrgAcquisition }

func (r *recordedAcquisition) RecordOrganizationAcquisition(_ context.Context, acq *models.OrgAcquisition) error {
	r.rows = append(r.rows, *acq)
	return nil
}

type recordedReferral struct {
	org      uuid.UUID
	click    string
	customer partners.Customer
	calls    int
}

func (r *recordedReferral) RecordSignup(_ context.Context, orgID uuid.UUID, clickID string, c partners.Customer) {
	r.org, r.click, r.customer = orgID, clickID, c
	r.calls++
}

// Both signup paths (password and federated) end in attributeWorkspace.
func TestAttributeWorkspaceSplitsChannelAndPartnerClick(t *testing.T) {
	acqStore, ref := &recordedAcquisition{}, &recordedReferral{}
	s := &authService{acquisition: acqStore, partners: ref}
	u := &models.User{ID: uuid.New(), Email: "owner@example.com", FirstName: "Ada", LastName: "Lovelace"}
	org := &models.Organization{ID: uuid.New()}

	s.attributeWorkspace(context.Background(), u, org, models.OrgAcquisition{UTMSource: "x", PartnerClickID: "cl1ck"})

	if len(acqStore.rows) != 1 || acqStore.rows[0].UTMSource != "x" || acqStore.rows[0].PartnerClickID != "" || acqStore.rows[0].OrganizationID != org.ID {
		t.Fatalf("channel row = %+v", acqStore.rows)
	}
	if ref.calls != 1 || ref.org != org.ID || ref.click != "cl1ck" ||
		ref.customer != (partners.Customer{Email: "owner@example.com", Name: "Ada Lovelace"}) {
		t.Fatalf("partner signup = %+v", ref)
	}
}

func TestAttributeWorkspaceWithOnlyAClickWritesNoChannelRow(t *testing.T) {
	acqStore, ref := &recordedAcquisition{}, &recordedReferral{}
	s := &authService{acquisition: acqStore, partners: ref}
	s.attributeWorkspace(context.Background(), &models.User{}, &models.Organization{ID: uuid.New()}, models.OrgAcquisition{PartnerClickID: "cl1ck"})
	if len(acqStore.rows) != 0 || ref.calls != 1 {
		t.Fatalf("rows=%d partner calls=%d", len(acqStore.rows), ref.calls)
	}
}

// With the program off (self-host) a click is dropped and nothing else changes.
func TestAttributeWorkspaceWithoutProgramIgnoresTheClick(t *testing.T) {
	acqStore := &recordedAcquisition{}
	s := &authService{acquisition: acqStore}
	s.attributeWorkspace(context.Background(), &models.User{}, &models.Organization{ID: uuid.New()}, models.OrgAcquisition{PartnerClickID: "cl1ck"})
	if len(acqStore.rows) != 0 {
		t.Fatalf("rows = %+v", acqStore.rows)
	}
}
