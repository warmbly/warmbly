package confenge

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

func TestCurrentComposerPrompt(t *testing.T) {
	if !CurrentComposerPrompt(PromptVersion) {
		t.Fatal("current")
	}
	if !CurrentComposerPrompt(PromptVersion + "+touch") {
		t.Fatal("suffix")
	}
	if CurrentComposerPrompt("confenge.draft.v3") {
		t.Fatal("v3 is prior")
	}
	if CurrentComposerPrompt("") {
		t.Fatal("empty is prior")
	}
}

func TestInvalidatePriorComposerDrafts(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	org, actor := uuid.New(), uuid.New()
	svc := NewService(Config{Enabled: true, RequireHumanApproval: true, MaxInitialEmailWords: 120}, repo, nil).(*service)

	acc := &models.OutreachAccount{
		ID: uuid.New(), OrganizationID: org, RazaoSocial: "Exemplo", CNPJ14: "12345678000199",
		QueueState: models.OutreachQueueNeedsReview,
	}
	if _, err := repo.UpsertAccount(ctx, acc); err != nil {
		t.Fatal(err)
	}

	approvedAt := time.Now().UTC()
	oldApproved := &models.OutreachDraft{
		ID: uuid.New(), OrganizationID: org, AccountID: acc.ID,
		Subject: "old", BodyText: "Isso não prova crédito sozinho, mas eventos públicos relevantes sem triagem.",
		Status: models.OutreachDraftApproved, PromptVersion: "confenge.draft.v3",
		ApprovedBy: &actor, ApprovedAt: &approvedAt,
	}
	oldReview := &models.OutreachDraft{
		ID: uuid.New(), OrganizationID: org, AccountID: acc.ID,
		Subject: "old2", BodyText: "objeto: x; órgão: y",
		Status: models.OutreachDraftNeedsReview, PromptVersion: "confenge.draft.v3",
	}
	current := &models.OutreachDraft{
		ID: uuid.New(), OrganizationID: org, AccountID: acc.ID,
		Subject: "new", BodyText: "Pelo contrato publicado, o contrato 1149 atingiu aniversário de reajuste.",
		Status: models.OutreachDraftNeedsReview, PromptVersion: PromptVersion,
	}
	sent := &models.OutreachDraft{
		ID: uuid.New(), OrganizationID: org, AccountID: acc.ID,
		Subject: "sent", BodyText: "já enviado",
		Status: models.OutreachDraftSent, PromptVersion: "confenge.draft.v3",
	}
	for _, d := range []*models.OutreachDraft{oldApproved, oldReview, current, sent} {
		if err := repo.UpsertDraft(ctx, d); err != nil {
			t.Fatal(err)
		}
	}

	tpApproved := &models.OutreachTouchpoint{
		ID: uuid.New(), OrganizationID: org, AccountID: acc.ID, Ordinal: 1,
		Channel: models.OutreachChannelEmail, State: models.TouchpointApproved,
		DraftID: &oldApproved.ID, Recipient: "ana@exemplo.com", Subject: "old", BodyText: oldApproved.BodyText,
		ApprovedBy: &actor, ApprovedAt: &approvedAt, ApprovedContentHash: "abc",
	}
	tpSent := &models.OutreachTouchpoint{
		ID: uuid.New(), OrganizationID: org, AccountID: acc.ID, Ordinal: 2,
		Channel: models.OutreachChannelEmail, State: models.TouchpointSent,
		DraftID: &sent.ID, Recipient: "ana@exemplo.com", Subject: "sent", BodyText: "já enviado",
	}
	if err := repo.InsertTouchpoint(ctx, tpApproved); err != nil {
		t.Fatal(err)
	}
	if err := repo.InsertTouchpoint(ctx, tpSent); err != nil {
		t.Fatal(err)
	}

	rep, xerr := svc.InvalidatePriorComposerDrafts(ctx, org, actor)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if rep.DraftsInvalidated < 2 {
		t.Fatalf("expected >=2 drafts invalidated: %+v", rep)
	}
	if rep.ApprovalsRevoked < 1 {
		t.Fatalf("expected approval revoke: %+v", rep)
	}
	if gotSent, _ := repo.GetDraft(ctx, org, sent.ID); gotSent.Status != models.OutreachDraftSent || gotSent.BodyText != "já enviado" {
		t.Fatalf("SENT draft mutated: %+v", gotSent)
	}

	gotApproved, _ := repo.GetDraft(ctx, org, oldApproved.ID)
	if gotApproved.Status != models.OutreachDraftSkipped || gotApproved.ApprovedBy != nil {
		t.Fatalf("approved-unsent not revoked: %+v", gotApproved)
	}
	if !containsStr(gotApproved.RiskFlags, FlagComposerStale) || !containsStr(gotApproved.RiskFlags, FlagRequiresRegen) {
		t.Fatalf("flags %v", gotApproved.RiskFlags)
	}
	gotReview, _ := repo.GetDraft(ctx, org, oldReview.ID)
	if gotReview.Status != models.OutreachDraftSkipped {
		t.Fatalf("old review status %s", gotReview.Status)
	}
	gotCurrent, _ := repo.GetDraft(ctx, org, current.ID)
	if gotCurrent.Status != models.OutreachDraftNeedsReview {
		t.Fatalf("current draft mutated: %s", gotCurrent.Status)
	}
	gotSent, _ := repo.GetDraft(ctx, org, sent.ID)
	if gotSent.Status != models.OutreachDraftSent || gotSent.BodyText != "já enviado" {
		t.Fatalf("SENT mutated: %+v", gotSent)
	}

	gotTP, _ := repo.GetTouchpoint(ctx, org, tpApproved.ID)
	if gotTP.State != models.TouchpointCancelled || gotTP.StopReason != StopComposerStale {
		t.Fatalf("approved-unsent touchpoint: %+v", gotTP)
	}
	if gotTP.ApprovedBy != nil || gotTP.ApprovedContentHash != "" {
		t.Fatalf("approval not cleared: %+v", gotTP)
	}
	gotSentTP, _ := repo.GetTouchpoint(ctx, org, tpSent.ID)
	if gotSentTP.State != models.TouchpointSent || gotSentTP.BodyText != "já enviado" {
		t.Fatalf("SENT touchpoint mutated: %+v", gotSentTP)
	}

	// Idempotent second pass.
	rep2, xerr := svc.InvalidatePriorComposerDrafts(ctx, org, actor)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if rep2.DraftsInvalidated != 0 {
		t.Fatalf("second pass must not re-invalidate skipped rows as prior NEEDS_REVIEW: %+v", rep2)
	}

	if strings.Contains(gotSent.BodyText, "fake") {
		t.Fatal("no invented content")
	}
}

func TestInvalidatePriorComposerSkipsSuppressedReservoir(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	org, actor := uuid.New(), uuid.New()
	svc := NewService(Config{Enabled: true, RequireHumanApproval: true, MaxInitialEmailWords: 120}, repo, nil).(*service)
	for i := 0; i < 20; i++ {
		acc := &models.OutreachAccount{
			ID: uuid.New(), OrganizationID: org, CNPJ14: fmt.Sprintf("40000000000%03d", i),
			QueueState: models.OutreachQueueTargetFitSuppressed, RazaoSocial: "Suppressed",
		}
		if _, err := repo.UpsertAccount(ctx, acc); err != nil {
			t.Fatal(err)
		}
	}
	ready := &models.OutreachAccount{
		ID: uuid.New(), OrganizationID: org, CNPJ14: "50000000000191",
		QueueState: models.OutreachQueueReadyToGenerate, RazaoSocial: "Ready",
	}
	if _, err := repo.UpsertAccount(ctx, ready); err != nil {
		t.Fatal(err)
	}
	old := &models.OutreachDraft{
		ID: uuid.New(), OrganizationID: org, AccountID: ready.ID,
		Status: models.OutreachDraftSkipped, PromptVersion: "confenge.draft.v3+touch",
		Subject: "old", BodyText: "objeto: dump",
	}
	if err := repo.UpsertDraft(ctx, old); err != nil {
		t.Fatal(err)
	}
	tp := &models.OutreachTouchpoint{
		ID: uuid.New(), OrganizationID: org, AccountID: ready.ID, Ordinal: 1,
		Channel: models.OutreachChannelEmail, State: models.TouchpointNeedsReview,
		DraftID: &old.ID, Recipient: "ana@horizontesul.com.br",
	}
	if err := repo.InsertTouchpoint(ctx, tp); err != nil {
		t.Fatal(err)
	}
	rep, xerr := svc.InvalidatePriorComposerDrafts(ctx, org, actor)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if rep.TouchpointsInvalidated != 1 {
		t.Fatalf("ready-queue stale touchpoint must be cancelled: %+v", rep)
	}
	got, _ := repo.GetTouchpoint(ctx, org, tp.ID)
	if got.State != models.TouchpointCancelled || got.StopReason != StopComposerStale {
		t.Fatalf("stale review touchpoint left: %+v", got)
	}
	rep2, xerr := svc.InvalidatePriorComposerDrafts(ctx, org, actor)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if rep2.TouchpointsInvalidated != 0 {
		t.Fatalf("second pass must be a no-op: %+v", rep2)
	}
}
