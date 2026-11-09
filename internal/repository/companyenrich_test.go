package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

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
