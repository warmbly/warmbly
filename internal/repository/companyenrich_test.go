package repository

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func TestCompanyEnrichProfileWriteCarriesExactSnapshot(t *testing.T) {
	org, id := uuid.New(), uuid.New()
	at := time.Now()
	name := "New Name"
	calls := 0
	r := &CompanyEnrichContactRepository{exec: func(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
		calls++
		if len(args) != 9 || args[0] != org || args[1] != id || args[2] != at || args[7] != "same@example.com" {
			t.Fatalf("snapshot missing: %v", args)
		}
		if got := args[3].(*string); *got != name {
			t.Fatal("profile field missing")
		}
		if args[4].(*string) != nil || args[5].(*string) != nil {
			t.Fatal("absent names overwritten")
		}
		var custom map[string]string
		if json.Unmarshal(args[6].([]byte), &custom) != nil || custom["companyenrich_position"] != "CTO" {
			t.Fatal("custom field payload lost")
		}
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}}
	err := r.ApplyCompanyEnrichProfile(context.Background(), org, id, at, "same@example.com", &models.CompanyEnrichProfilePatch{FirstName: &name, CustomFields: map[string]string{"companyenrich_position": "CTO"}})
	if err != nil || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestCompanyEnrichProfileWriteRefusesStaleSnapshot(t *testing.T) {
	calls := 0
	r := &CompanyEnrichContactRepository{exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		calls++
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}}
	err := r.ApplyCompanyEnrichProfile(context.Background(), uuid.New(), uuid.New(), time.Now(), "same@example.com", &models.CompanyEnrichProfilePatch{})
	if err == nil || err.Code != errx.Conflict || err.Identifier != "companyenrich_contact_changed" || calls != 1 {
		t.Fatalf("stale result: %v", err)
	}
}

func TestCompanyEnrichProfileWriteRejectsMissingSnapshotAndOversize(t *testing.T) {
	r := &CompanyEnrichContactRepository{exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		t.Fatal("invalid update reached DB")
		return pgconn.CommandTag{}, nil
	}}
	if err := r.ApplyCompanyEnrichProfile(context.Background(), uuid.New(), uuid.New(), time.Time{}, "same@example.com", &models.CompanyEnrichProfilePatch{}); err == nil {
		t.Fatal("missing timestamp accepted")
	}
	if err := r.ApplyCompanyEnrichProfile(context.Background(), uuid.New(), uuid.New(), time.Now(), "same@example.com", &models.CompanyEnrichProfilePatch{CustomFields: map[string]string{"companyenrich_position": strings.Repeat("x", 11000)}}); err == nil {
		t.Fatal("oversized custom data accepted")
	}
}

type companyEnrichReadRecorder struct {
	ContactRepository
	wantOrg, wantID uuid.UUID
	called          bool
}

type companyEnrichSnapshotRow struct{ raw []byte }

func (r companyEnrichSnapshotRow) Scan(dst ...any) error {
	*dst[0].(*[]byte) = r.raw
	*dst[1].(*time.Time) = time.Unix(1700000000, 0).UTC()
	*dst[2].(*time.Time) = time.Unix(1600000000, 0).UTC()
	return nil
}

func (r *companyEnrichReadRecorder) GetDetail(_ context.Context, user uuid.UUID, org *uuid.UUID, id uuid.UUID) (*models.ContactDetail, *errx.Error) {
	r.called = true
	if org == nil || *org != r.wantOrg || id != r.wantID {
		return nil, errx.ErrNotFound
	}
	return &models.ContactDetail{Contact: models.Contact{ID: id, VerificationStatus: "invalid", Subscribed: false}}, nil
}

func TestCompanyEnrichReadUsesOrganizationAndPreservesContactState(t *testing.T) {
	org, id := uuid.New(), uuid.New()
	contacts := &companyEnrichReadRecorder{wantOrg: org, wantID: id}
	r := &CompanyEnrichContactRepository{contacts: contacts, queryRow: func(_ context.Context, _ string, args ...any) pgx.Row {
		if len(args) != 2 || args[0] != org || args[1] != id {
			t.Fatal("snapshot must be scoped by organization and contact")
		}
		return companyEnrichSnapshotRow{raw: []byte(`{"id":"` + id.String() + `","verification_status":"invalid","subscribed":false,"internal_owner_id":"private"}`)}
	}}
	rows, err := r.GetByIDsAndOrganization(context.Background(), org, []uuid.UUID{id})
	if err != nil || !contacts.called || len(rows) != 1 || rows[0].VerificationStatus != "invalid" || rows[0].Subscribed || rows[0].UpdatedAt.Unix() != 1700000000 {
		t.Fatalf("read failed %v %v", rows, err)
	}
	contacts.called = false
	if _, err = r.GetByIDsAndOrganization(context.Background(), uuid.Nil, []uuid.UUID{id}); err == nil || contacts.called {
		t.Fatal("unscoped read")
	}
}
