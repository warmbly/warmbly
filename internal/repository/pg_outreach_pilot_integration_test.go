package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

func TestPostgresPilotMembershipCapacityAndIdempotency(t *testing.T) {
	dsn := os.Getenv("WARMBLY_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_POSTGRES_DSN is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}

	userID, orgID := uuid.New(), uuid.New()
	email := fmt.Sprintf("pilot-pg-%s@example.test", userID)
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, first_name, last_name, email) VALUES ($1,'Pilot','Test',$2)`, userID, email); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1,'Pilot PG Test',$2,$3)`, orgID, "pilot-pg-"+orgID.String(), userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer pool.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM organizations WHERE id=$1`, orgID)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM users WHERE id=$1`, userID)
	})

	repo := NewOutreachRepository(pool)
	now := time.Now().UTC()
	generatedAt := now.Add(-time.Hour)
	run := &models.OutreachImportRun{
		OrganizationID: orgID, SourceSystem: "extra-cli", SourceRunID: "pg-concurrent-run",
		SchemaVersion: models.OutreachSchemaV1, SnapshotHash: "pg-concurrent-snapshot",
		PayloadHash: uuid.NewString(), Status: models.OutreachImportCompleted,
		SourceGeneratedAt: &generatedAt,
	}
	if err := repo.CreateImportRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertFeedSyncState(ctx, &models.OutreachFeedSyncState{
		OrganizationID: orgID, LastSnapshotHash: run.SnapshotHash, LastRunID: run.SourceRunID,
		LastStatus: "completed", LastSuccessAt: &now, SourceGeneratedAt: &generatedAt,
	}); err != nil {
		t.Fatal(err)
	}

	const attempts = 40
	memberships := make([]models.OutreachPilotMembership, 0, attempts)
	for i := 0; i < attempts; i++ {
		contextHash := fmt.Sprintf("context-%02d", i)
		cnpj := fmt.Sprintf("%014d", 90000000000000+i)
		account := &models.OutreachAccount{
			OrganizationID: orgID, CNPJ14: cnpj, QueueState: models.OutreachQueueNeedsReview,
			SourceRunID: run.SourceRunID, LastImportRunID: &run.ID, MessageContextHash: contextHash,
			TargetFitClass: "TARGET_CONFIRMED", TargetFitVersion: "v1", TargetFitFresh: true,
			TargetFitEligible: true, TargetFitSourceWatermark: generatedAt.Format(time.RFC3339),
			TargetFitObservedAt: &generatedAt, EmailSendReady: true,
		}
		if _, err := repo.UpsertAccount(ctx, account); err != nil {
			t.Fatal(err)
		}
		recipient := fmt.Sprintf("pilot%02d@company%02d.example.test", i, i)
		candidate := &models.OutreachContactCandidate{
			OrganizationID: orgID, AccountID: account.ID, Email: recipient,
			VerificationStatus: models.OutreachVerifyOfficialSource, EmailSendReady: true,
			OwnershipStatus: "COMPANY_OWNED", SourceURL: fmt.Sprintf("https://company%02d.example.test/contact", i),
			SourceDate: &generatedAt, LastImportRunID: &run.ID,
		}
		if _, err := repo.UpsertCandidate(ctx, candidate); err != nil {
			t.Fatal(err)
		}
		draft := &models.OutreachDraft{
			OrganizationID: orgID, AccountID: account.ID, ContactCandidateID: &candidate.ID,
			RecipientEmail: recipient, Subject: "Pilot subject", BodyText: "Pilot body",
			RiskClass: "YELLOW", Status: models.OutreachDraftNeedsReview,
		}
		if err := repo.UpsertDraft(ctx, draft); err != nil {
			t.Fatal(err)
		}
		touchpoint := &models.OutreachTouchpoint{
			OrganizationID: orgID, AccountID: account.ID, ContactCandidateID: &candidate.ID,
			DraftID: &draft.ID, Ordinal: 1, Channel: models.OutreachChannelEmail,
			State: models.TouchpointNeedsReview, Recipient: recipient, Subject: draft.Subject,
			BodyText: draft.BodyText, GeneratedContextHash: contextHash,
			IdempotencyKey: "pg-pilot-" + account.ID.String(),
		}
		if err := repo.InsertTouchpoint(ctx, touchpoint); err != nil {
			t.Fatal(err)
		}
		memberships = append(memberships, models.OutreachPilotMembership{
			OrganizationID: orgID, CohortID: "pg-concurrency", AccountID: account.ID,
			CNPJ14: cnpj, ContactCandidateID: candidate.ID, TouchpointID: touchpoint.ID,
			DraftID: draft.ID, SnapshotHash: run.SnapshotHash, SourceRunID: run.SourceRunID,
			ContextHash: contextHash, OperationKey: "pg-operation", RequestHash: fmt.Sprintf("%064x", i+1),
			FeedGeneratedAt: *run.SourceGeneratedAt, CandidateUpdatedAt: candidate.UpdatedAt,
		})
	}

	for i := 0; i < 30; i++ {
		membership := memberships[i]
		if _, err := repo.ReservePilotSlot(ctx, orgID, "pg-release", membership.AccountID, membership.CNPJ14, 30); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.ReservePilotSlot(ctx, orgID, "pg-release", memberships[30].AccountID, memberships[30].CNPJ14, 30); !errors.Is(err, ErrPilotCapacityReached) {
		t.Fatalf("31st reservation must be blocked: %v", err)
	}
	if err := repo.ReleasePilotSlot(ctx, orgID, "pg-release", memberships[0].AccountID); err != nil {
		t.Fatal(err)
	}
	if count, err := repo.ReservePilotSlot(ctx, orgID, "pg-release", memberships[30].AccountID, memberships[30].CNPJ14, 30); err != nil || count != 30 {
		t.Fatalf("released capacity must be reusable: count=%d err=%v", count, err)
	}

	for i := 0; i < 30; i++ {
		membership := memberships[i]
		if _, err := repo.ReservePilotSlot(ctx, orgID, "pg-stale", membership.AccountID, membership.CNPJ14, 30); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE outreach_pilot_slots SET created_at=now() - interval '31 minutes' WHERE organization_id=$1 AND cohort_id='pg-stale'`, orgID); err != nil {
		t.Fatal(err)
	}
	if count, err := repo.ReservePilotSlot(ctx, orgID, "pg-stale", memberships[30].AccountID, memberships[30].CNPJ14, 30); err != nil || count != 1 {
		t.Fatalf("stale orphan reservations must be reclaimed: count=%d err=%v", count, err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	succeeded, capacityBlocked, unexpected := 0, 0, []error{}
	for i := range memberships {
		membership := memberships[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, claimErr := repo.ReservePilotSlot(ctx, membership.OrganizationID, membership.CohortID, membership.AccountID, membership.CNPJ14, 30)
			if claimErr == nil {
				_, _, claimErr = repo.ClaimPilotMembership(ctx, &membership, 30)
			}
			mu.Lock()
			defer mu.Unlock()
			switch {
			case claimErr == nil:
				succeeded++
			case errors.Is(claimErr, ErrPilotCapacityReached):
				capacityBlocked++
			default:
				unexpected = append(unexpected, claimErr)
			}
		}()
	}
	wg.Wait()
	if len(unexpected) != 0 || succeeded != 30 || capacityBlocked != 10 {
		t.Fatalf("concurrent claims: succeeded=%d capacity_blocked=%d errors=%v", succeeded, capacityBlocked, unexpected)
	}
	stored, err := repo.ListPilotMemberships(ctx, orgID, "pg-concurrency")
	if err != nil || len(stored) != 30 {
		t.Fatalf("stored memberships=%d err=%v", len(stored), err)
	}
	first := stored[0]
	for i := range memberships {
		if memberships[i].AccountID == first.AccountID {
			first.FeedGeneratedAt = memberships[i].FeedGeneratedAt
			first.CandidateUpdatedAt = memberships[i].CandidateUpdatedAt
			break
		}
	}
	if _, count, err := repo.ClaimPilotMembership(ctx, &first, 30); err != nil || count != 30 {
		t.Fatalf("idempotent retry count=%d err=%v", count, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE outreach_contact_candidates SET updated_at=updated_at + interval '1 second' WHERE id=$1`, first.ContactCandidateID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.ClaimPilotMembership(ctx, &first, 30); err == nil {
		t.Fatal("candidate changed after resolution must fail the atomic membership claim")
	}
	if _, err := pool.Exec(ctx, `UPDATE outreach_contact_candidates SET updated_at=$2 WHERE id=$1`, first.ContactCandidateID, first.CandidateUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClaimPilotOperation(ctx, orgID, "same-key", fmt.Sprintf("%064x", 1)); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClaimPilotOperation(ctx, orgID, "same-key", fmt.Sprintf("%064x", 2)); !errors.Is(err, ErrPilotIdempotencyConflict) {
		t.Fatalf("operation key reuse must conflict: %v", err)
	}

	// A changed authoritative context must atomically revoke approval and any
	// queued governor item. This exercises the real Postgres table names and
	// status vocabulary rather than a mock implementation.
	first = stored[0]
	if _, err := pool.Exec(ctx, `
		UPDATE outreach_touchpoints
		SET state='APPROVED', approved_content_hash='old-content', approved_by=$2, approved_at=now()
		WHERE id=$1`, first.TouchpointID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE outreach_drafts SET status='APPROVED', approved_by=$2, approved_at=now()
		WHERE id=$1`, first.DraftID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO confenge_dispatch_queue (organization_id, channel, draft_id, message_key, status)
		VALUES ($1, 'EMAIL', $2, $3, 'queued')`, orgID, first.DraftID, "context-invalidation-"+first.DraftID.String()); err != nil {
		t.Fatal(err)
	}
	if err := repo.InvalidateAccountApprovalsForContext(ctx, orgID, first.AccountID, "new-context"); err != nil {
		t.Fatal(err)
	}
	var touchpointState, draftState, dispatchState string
	if err := pool.QueryRow(ctx, `SELECT state FROM outreach_touchpoints WHERE id=$1`, first.TouchpointID).Scan(&touchpointState); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM outreach_drafts WHERE id=$1`, first.DraftID).Scan(&draftState); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM confenge_dispatch_queue WHERE draft_id=$1`, first.DraftID).Scan(&dispatchState); err != nil {
		t.Fatal(err)
	}
	if touchpointState != models.TouchpointNeedsReview || draftState != models.OutreachDraftNeedsReview || dispatchState != "cancelled" {
		t.Fatalf("context invalidation touchpoint=%s draft=%s dispatch=%s", touchpointState, draftState, dispatchState)
	}

	// Concurrent generators converge on one active draft without leaking a unique-constraint error.
	draftA := &models.OutreachDraft{OrganizationID: orgID, AccountID: first.AccountID, Subject: "Concurrent A", BodyText: "Body", RiskClass: "YELLOW", Status: models.OutreachDraftNeedsReview}
	draftB := &models.OutreachDraft{OrganizationID: orgID, AccountID: first.AccountID, Subject: "Concurrent B", BodyText: "Body", RiskClass: "YELLOW", Status: models.OutreachDraftNeedsReview}
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, draft := range []*models.OutreachDraft{draftA, draftB} {
		go func(value *models.OutreachDraft) {
			<-start
			errs <- repo.UpsertDraft(ctx, value)
		}(draft)
	}
	close(start)
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if draftA.ID != first.DraftID || draftB.ID != first.DraftID {
		t.Fatalf("concurrent active drafts did not converge: a=%s b=%s want=%s", draftA.ID, draftB.ID, first.DraftID)
	}
}
