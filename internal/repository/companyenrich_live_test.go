package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// This only uses a migrated scratch PostgreSQL database (WARMBLY_TEST_DB).
// It never sends a CompanyEnrich request.
func TestCompanyEnrichLiveProfileGuardAndPreservedState(t *testing.T) {
	handle, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 219)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE contacts SET subscribed = false,
 verification_status = 'invalid', verification_reason = 'mailbox_not_found',
 verification_source = 'provider', verification_provider = 'fixture',
 verification_confidence = 99, is_catch_all = true,
 mail_host = 'google_workspace', esp_provider = 'gmail', phone = '+1-555-0100',
 custom_fields = '{"existing":"keep"}'::jsonb WHERE id = $1`, f.contact); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO campaign_leads (campaign_id, contact_id) VALUES ($1, $2)`, f.campaign, f.contact); err != nil {
		t.Fatal(err)
	}
	repo := NewCompanyEnrichContactRepository(handle, NewContactRepostory(handle))
	read := func() models.Contact {
		t.Helper()
		rows, xerr := repo.GetByIDsAndOrganization(ctx, f.org, []uuid.UUID{f.contact})
		if xerr != nil || len(rows) != 1 {
			t.Fatalf("read contact: %v, rows=%d", xerr, len(rows))
		}
		return rows[0]
	}
	before := read()
	name := "Enriched"
	patch := &models.CompanyEnrichProfilePatch{FirstName: &name, CustomFields: map[string]string{"companyenrich_position": "CTO"}}
	if xerr := repo.ApplyCompanyEnrichProfile(ctx, uuid.New(), f.contact, before.UpdatedAt, before.Email, patch); xerr == nil || xerr.Code != errx.Conflict {
		t.Fatalf("wrong organization must refuse update: %v", xerr)
	}
	if _, xerr := repo.GetByIDsAndOrganization(ctx, uuid.New(), []uuid.UUID{f.contact}); xerr == nil || xerr.Code != errx.NotFound {
		t.Fatalf("wrong organization must refuse read: %v", xerr)
	}
	if xerr := repo.ApplyCompanyEnrichProfile(ctx, f.org, f.contact, before.UpdatedAt, before.Email, patch); xerr != nil {
		t.Fatalf("apply current snapshot: %v", xerr)
	}
	after := read()
	if after.FirstName != name || after.CustomFields["companyenrich_position"] != "CTO" || after.CustomFields["existing"] != "keep" {
		t.Fatalf("profile merge did not persist: %+v", after)
	}
	if after.Email != before.Email || after.LastName != before.LastName || after.Company != before.Company ||
		after.Phone != before.Phone || after.Subscribed || after.VerificationStatus != "invalid" ||
		after.VerificationReason != "mailbox_not_found" || after.VerificationSource != "provider" ||
		after.VerificationProvider != "fixture" || after.VerificationConfidence != 99 || !after.IsCatchAll ||
		after.MailHost != "google_workspace" || after.ESPProvider != "gmail" ||
		len(after.Campaigns) != 1 || after.Campaigns[0].ID != f.campaign.String() {
		t.Fatalf("protected state or membership changed/lost: %+v", after)
	}
	name = "Must not overwrite"
	if xerr := repo.ApplyCompanyEnrichProfile(ctx, f.org, f.contact, before.UpdatedAt, before.Email, patch); xerr == nil || xerr.Code != errx.Conflict {
		t.Fatalf("stale timestamp must refuse update: %v", xerr)
	}
	// Even a writer that fails to bump updated_at cannot replace the identity
	// whose paid lookup produced the enrichment result.
	if _, err := pool.Exec(ctx, `UPDATE contacts SET email = 'changed@example.com' WHERE id = $1`, f.contact); err != nil {
		t.Fatal(err)
	}
	if xerr := repo.ApplyCompanyEnrichProfile(ctx, f.org, f.contact, after.UpdatedAt, after.Email, patch); xerr == nil || xerr.Code != errx.Conflict {
		t.Fatalf("changed email must refuse update: %v", xerr)
	}
	if current := read(); current.FirstName != "Enriched" || current.Email != "changed@example.com" {
		t.Fatalf("refused update mutated contact: %+v", current)
	}
}
