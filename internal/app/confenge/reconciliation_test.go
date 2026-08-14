package confenge

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

func TestTargetFitReconciliationIsRetroactiveAndIdempotent(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	svc := NewService(Config{Enabled: true}, repo, nil).(*service)
	org := uuid.New()
	acc := &models.OutreachAccount{
		ID: uuid.New(), OrganizationID: org, CNPJ14: "10000000000011",
		RazaoSocial:  "EMPRESA SINTETICA ALFA LTDA",
		SourceSystem: "extra-cli", QueueState: models.OutreachQueueApproved,
		TargetFitEligible: true, EmailSendReady: true,
	}
	if _, err := repo.UpsertAccount(ctx, acc); err != nil {
		t.Fatal(err)
	}
	runID := uuid.New()
	cand := &models.OutreachContactCandidate{
		ID: uuid.New(), OrganizationID: org, AccountID: acc.ID,
		Email: "contato@prevencao.example", VerificationStatus: models.OutreachVerifyVerified,
		EmailSendReady: true, LastImportRunID: &runID,
	}
	if _, err := repo.UpsertCandidate(ctx, cand); err != nil {
		t.Fatal(err)
	}
	draft := &models.OutreachDraft{
		ID: uuid.New(), OrganizationID: org, AccountID: acc.ID,
		ContactCandidateID: &cand.ID, Status: models.OutreachDraftApproved,
	}
	if err := repo.UpsertDraft(ctx, draft); err != nil {
		t.Fatal(err)
	}
	tp := &models.OutreachTouchpoint{
		ID: uuid.New(), OrganizationID: org, AccountID: acc.ID,
		ContactCandidateID: &cand.ID, DraftID: &draft.ID,
		Ordinal: 1, Channel: models.OutreachChannelEmail, Purpose: models.TouchpointPurposeInitial,
		State: models.TouchpointNeedsReview, Recipient: cand.Email, Subject: "Assunto", BodyText: "Corpo",
	}
	if err := repo.InsertTouchpoint(ctx, tp); err != nil {
		t.Fatal(err)
	}

	dry, xerr := svc.ReconcileTargetFit(ctx, org, true)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if dry.BeforeOperational != 1 || dry.AfterOperational != 0 || dry.AccountsChanged != 1 || dry.SuppressedByReason[TargetFitReasonMissing] != 1 {
		t.Fatalf("unexpected dry-run report: %+v", dry)
	}
	if repo.byID[acc.ID].QueueState != models.OutreachQueueApproved || repo.drafts[draft.ID].Status != models.OutreachDraftApproved {
		t.Fatal("dry-run mutated persisted state")
	}

	first, xerr := svc.ReconcileTargetFit(ctx, org, false)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if first.AccountsChanged != 1 || first.BlockedDrafts != 1 || first.CancelledTouchpoints != 1 {
		t.Fatalf("unexpected apply report: %+v", first)
	}
	got := repo.byID[acc.ID]
	if got.TargetFitEligible || got.TargetFitSuppressionReason != TargetFitReasonMissing || got.QueueState != models.OutreachQueueTargetFitSuppressed {
		t.Fatalf("historical account not suppressed: %+v", got)
	}
	if got.TargetFitReconciledAt == nil || time.Since(*got.TargetFitReconciledAt) > time.Minute {
		t.Fatal("reconciliation timestamp missing")
	}

	second, xerr := svc.ReconcileTargetFit(ctx, org, false)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if second.AccountsChanged != 0 || second.BlockedDrafts != 0 || second.CancelledTouchpoints != 0 {
		t.Fatalf("second reconciliation was not idempotent: %+v", second)
	}
}
