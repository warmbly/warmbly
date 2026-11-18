package contact

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type snapshotImportRepository struct {
	repository.ContactRepository
	existing map[string]uuid.UUID
	updates  []repository.ContactImportUpdate
	found    bool
}

func (r *snapshotImportRepository) ImportLookup(context.Context, uuid.UUID, uuid.UUID, []string) (map[string]uuid.UUID, map[string]bool, *errx.Error) {
	return r.existing, nil, nil
}

func (r *snapshotImportRepository) ImportUpdate(_ context.Context, _ uuid.UUID, rows []repository.ContactImportUpdate) ([]bool, *errx.Error) {
	r.updates = append(r.updates, rows...)
	result := make([]bool, len(rows))
	for i := range result {
		result[i] = r.found
	}
	return result, nil
}

type snapshotImportSyncer struct{ orgs []uuid.UUID }

func (s *snapshotImportSyncer) SyncOrgLinkedCampaigns(_ context.Context, orgID uuid.UUID) {
	s.orgs = append(s.orgs, orgID)
}

func TestGuardedImportUpdatesExistingContactAndSyncsSegments(t *testing.T) {
	id, orgID := uuid.New(), uuid.New()
	expected := &repository.ContactImportSnapshot{ID: id, Email: "person@example.com", UpdatedAt: time.Now()}
	repo := &snapshotImportRepository{existing: map[string]uuid.UUID{expected.Email: id}, found: true}
	syncer := &snapshotImportSyncer{}
	svc := &contactService{contactRepository: repo, segmentSyncer: syncer}
	rows := []ImportRow{{Line: 1, Cells: []string{expected.Email, "Booking.com"}, Expected: expected}, {Line: 2, Cells: []string{expected.Email, "Other"}}}
	result, xerr := svc.RunImport(context.Background(), uuid.New().String(), orgID, rows, snapshotImportOptions(), nil, nil)
	if xerr != nil || result.Updated != 1 || result.Skipped != 1 || result.Imported != 0 || result.Failed != 0 {
		t.Fatalf("guarded import: result=%+v, error=%v", result, xerr)
	}
	if len(repo.updates) != 1 || repo.updates[0].Expected != expected || repo.updates[0].Contact.Company != "Booking.com" {
		t.Fatalf("snapshot or original profile lost: %+v", repo.updates)
	}
	if len(syncer.orgs) != 1 || syncer.orgs[0] != orgID {
		t.Fatalf("normal segment/campaign sync did not run: %v", syncer.orgs)
	}
	if repo.updates[0].Contact.Subscribed != nil || repo.updates[0].Contact.VerificationStatus != "" {
		t.Fatal("profile-only import must not change subscription or verification")
	}
}

func TestGuardedImportNeverCreatesOrSwitchesTarget(t *testing.T) {
	for _, scenario := range []string{"missing", "different_id", "stale", "missing_id", "missing_timestamp", "different_email", "wrong_dedup"} {
		t.Run(scenario, func(t *testing.T) {
			id := uuid.New()
			expected := &repository.ContactImportSnapshot{ID: id, Email: "person@example.com", UpdatedAt: time.Now()}
			repo := &snapshotImportRepository{existing: map[string]uuid.UUID{expected.Email: id}, found: true}
			opts := snapshotImportOptions()
			switch scenario {
			case "missing":
				repo.existing = nil
			case "different_id":
				repo.existing[expected.Email] = uuid.New()
			case "stale":
				repo.found = false
			case "missing_id":
				expected.ID = uuid.Nil
			case "missing_timestamp":
				expected.UpdatedAt = time.Time{}
			case "different_email":
				expected.Email = "changed@example.com"
			case "wrong_dedup":
				opts.Dedup = models.ContactImportDedupSkip
			}
			syncer := &snapshotImportSyncer{}
			svc := &contactService{contactRepository: repo, segmentSyncer: syncer}
			result, xerr := svc.RunImport(context.Background(), uuid.New().String(), uuid.New(), []ImportRow{{Line: 1, Cells: []string{"person@example.com", "New"}, Expected: expected}}, opts, nil, nil)
			if xerr != nil || result.Failed != 1 || result.Updated != 0 || result.Imported != 0 || len(result.Errors) != 1 || result.Errors[0].Reason != ImportReasonContactChanged {
				t.Fatalf("guarded import: result=%+v, error=%v", result, xerr)
			}
			if len(syncer.orgs) != 0 {
				t.Fatal("rejected row must not trigger campaign synchronization")
			}
			if scenario != "stale" && len(repo.updates) != 0 {
				t.Fatal("invalid target must not reach the write")
			}
		})
	}
}

func snapshotImportOptions() *models.ContactImportCommit {
	return &models.ContactImportCommit{Dedup: models.ContactImportDedupUpdate, SkipMissingSegments: true, Mapping: []models.ContactImportColumnMapping{
		{Index: 0, Target: models.ContactImportTargetEmail}, {Index: 1, Target: models.ContactImportTargetCompany},
	}}
}
