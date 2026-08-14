package confenge

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/confenge/dispatch"
	"github.com/warmbly/warmbly/internal/models"
)

func adversarialLead(cnpj, name, class, watermark string, includeTargetFit bool) FeedLead {
	ready := true
	lead := sampleLeadWithActivation(90, ActivationActionableNow)
	lead.SourceLeadID = "lead-" + cnpj
	lead.Company.CNPJ14 = cnpj
	lead.Company.CNPJRoot = cnpj[:8]
	lead.Company.RazaoSocial = name
	lead.Contacts[0].SourceContactID = "contact-" + cnpj
	lead.Contacts[0].Email = cnpj + "@company.example"
	lead.Contacts[0].OwnershipStatus = "COMPANY_OWNED"
	lead.Contacts[0].EmailSendReady = &ready
	lead.EmailSendReady = &ready
	if !includeTargetFit {
		lead.TargetFitClass = ""
		lead.TargetFitVersion = ""
		lead.TargetFitSourceWatermark = ""
		lead.TargetFitComputedAt = ""
		lead.TargetFitFresh = nil
		lead.TargetFitSendTier = ""
		return lead
	}
	lead.TargetFitClass = class
	lead.TargetFitVersion = "confenge-target-fit-v1"
	lead.TargetFitSourceWatermark = watermark
	lead.TargetFitComputedAt = watermark
	lead.TargetFitFresh = &ready
	if class == TargetFitConfirmed {
		lead.TargetFitSendTier = "A_AUTOMATIC"
	} else {
		lead.TargetFitSendTier = "OUT_OF_SCOPE"
	}
	return lead
}

func importAdversarialLead(t *testing.T, svc *service, org uuid.UUID, lead FeedLead, run string) *models.OutreachAccount {
	t.Helper()
	feed := Feed{
		SchemaVersion: models.OutreachSchemaV1,
		GeneratedAt:   "2026-08-11T12:00:00Z",
		Source:        FeedSource{System: "extra-cli", RunID: run, SnapshotHash: run, ProfileID: "confenge", ProfileVersion: "1"},
		Leads:         []FeedLead{lead},
	}
	raw, _ := json.Marshal(feed)
	if _, xerr := svc.ImportFromBytes(context.Background(), org, nil, raw, ImportOptions{IdempotencyKey: run}); xerr != nil {
		t.Fatal(xerr)
	}
	acc, err := svc.repo.GetAccountByCNPJ(context.Background(), org, lead.Company.CNPJ14)
	if err != nil || acc == nil {
		t.Fatalf("account missing: %v", err)
	}
	return acc
}

func TestTargetFitOutCompaniesNeverEnterOperationalSurface(t *testing.T) {
	cases := []struct{ cnpj, name string }{
		{"10000000000011", "EMPRESA SINTETICA ALFA LTDA"},
		{"10000000000012", "EMPRESA SINTETICA BETA LTDA"},
		{"10000000000013", "EMPRESA SINTETICA GAMA LTDA"},
	}
	for _, tc := range cases {
		t.Run(tc.cnpj, func(t *testing.T) {
			repo := newMemRepo()
			svc := NewService(Config{Enabled: true, DynamicPriorityEnabled: true}, repo, nil).(*service)
			svc.WireExecution(&mockCampaigns{}, &mockContacts{})
			svc.WireDispatchGovernor(dispatch.NewGovernor(dispatch.Config{SendsPerHour: 10, MinGap: time.Second}, dispatch.NewMemoryStore(), nil))
			org := uuid.New()
			acc := importAdversarialLead(t, svc, org, adversarialLead(tc.cnpj, tc.name, TargetFitOutOfScope, "2026-08-11T12:00:00Z", true), "out-"+tc.cnpj)
			if acc.TargetFitEligible || acc.QueueState != models.OutreachQueueTargetFitSuppressed || acc.TargetFitSuppressionReason != TargetFitReasonOut {
				t.Fatalf("not suppressed: %+v", acc)
			}
			items, xerr := svc.ListWorkingQueue(context.Background(), org, LaneNow, 20)
			if xerr != nil || len(items) != 0 {
				t.Fatalf("Agora leaked target OUT: items=%d err=%v", len(items), xerr)
			}
			if _, xerr := svc.PlanAccountCadence(context.Background(), org, uuid.New(), acc.ID, nil, models.OutreachChannelEmail); xerr == nil {
				t.Fatal("Plan must fail closed")
			}
			cand := repo.cands[acc.ID][0]
			draftID := uuid.New()
			tp := &models.OutreachTouchpoint{
				ID: uuid.New(), OrganizationID: org, AccountID: acc.ID, ContactCandidateID: &cand.ID,
				DraftID: &draftID, Ordinal: 1, Channel: models.OutreachChannelEmail,
				Purpose: models.TouchpointPurposeInitial, State: models.TouchpointNeedsReview,
				Recipient: cand.Email, Subject: "Assunto", BodyText: "Corpo comercial",
			}
			RecomputeContentHash(tp)
			if err := repo.InsertTouchpoint(context.Background(), tp); err != nil {
				t.Fatal(err)
			}
			if _, xerr := svc.ApproveTouchpoint(context.Background(), org, uuid.New(), tp.ID, ApprovalOptions{}); xerr == nil {
				t.Fatal("Approve must fail closed")
			}
			if err := ApplyHumanApproval(tp, uuid.New(), time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			if err := repo.UpdateTouchpoint(context.Background(), tp); err != nil {
				t.Fatal(err)
			}
			draft := &models.OutreachDraft{
				ID: draftID, OrganizationID: org, AccountID: acc.ID, ContactCandidateID: &cand.ID,
				Channel: models.OutreachChannelEmail, RecipientEmail: cand.Email,
				Subject: tp.Subject, BodyText: tp.BodyText, Status: models.OutreachDraftApproved,
			}
			if err := repo.UpsertDraft(context.Background(), draft); err != nil {
				t.Fatal(err)
			}
			if _, xerr := svc.EnrollDraft(context.Background(), org, uuid.New(), draft.ID); xerr == nil {
				t.Fatal("Enroll must fail closed")
			}
			gate := svc.GateCampaignEmail(context.Background(), org, DefaultCampaignName, cand.Email, uuid.New(), uuid.New(), uuid.New())
			if gate.Kind != GateCommercialBlock {
				t.Fatalf("final send gate kind=%v reason=%s", gate.Kind, gate.Reason)
			}
		})
	}
}

func TestTargetFitConfirmedFreshRemainsOperational(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(Config{Enabled: true, DynamicPriorityEnabled: true}, repo, nil).(*service)
	org := uuid.New()
	acc := importAdversarialLead(t, svc, org, adversarialLead("11222333000181", "ACME ENGENHARIA LTDA", TargetFitConfirmed, "2026-08-11T12:00:00Z", true), "confirmed")
	items, xerr := svc.ListWorkingQueue(context.Background(), org, LaneNow, 20)
	if xerr != nil || len(items) != 1 || items[0].Account.ID != acc.ID {
		t.Fatalf("confirmed company missing from Agora: items=%d err=%v", len(items), xerr)
	}
	if _, xerr := svc.PlanAccountCadence(context.Background(), org, uuid.New(), acc.ID, nil, models.OutreachChannelEmail); xerr != nil {
		t.Fatalf("confirmed company cannot plan: %v", xerr)
	}
}

func TestTargetFitDowngradeCancelsPendingAndOldWatermarkCannotResurrect(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(Config{Enabled: true, DynamicPriorityEnabled: true}, repo, nil).(*service)
	org := uuid.New()
	acc := importAdversarialLead(t, svc, org, adversarialLead("11222333000181", "ACME ENGENHARIA LTDA", TargetFitConfirmed, "2026-08-10T12:00:00Z", true), "confirmed-old")
	touches, xerr := svc.PlanAccountCadence(context.Background(), org, uuid.New(), acc.ID, nil, models.OutreachChannelEmail)
	if xerr != nil || len(touches) == 0 {
		t.Fatalf("plan setup: %v", xerr)
	}
	draft := &models.OutreachDraft{ID: uuid.New(), OrganizationID: org, AccountID: acc.ID, Status: models.OutreachDraftApproved}
	_ = repo.UpsertDraft(context.Background(), draft)

	acc = importAdversarialLead(t, svc, org, adversarialLead("11222333000181", "ACME ENGENHARIA LTDA", TargetFitOutOfScope, "2026-08-11T12:00:00Z", true), "downgrade-new")
	if acc.TargetFitEligible || acc.TargetFitClass != TargetFitOutOfScope {
		t.Fatalf("downgrade not applied: %+v", acc)
	}
	if got := repo.touchpoints[touches[0].ID]; got == nil || got.State != models.TouchpointCancelled {
		t.Fatalf("pending touchpoint not cancelled: %+v", got)
	}
	if got := repo.drafts[draft.ID]; got == nil || got.Status != models.OutreachDraftBlocked {
		t.Fatalf("approved draft not blocked: %+v", got)
	}

	acc = importAdversarialLead(t, svc, org, adversarialLead("11222333000181", "ACME ENGENHARIA LTDA", TargetFitConfirmed, "2026-08-09T12:00:00Z", true), "confirmed-stale-arrival")
	if acc.TargetFitClass != TargetFitOutOfScope || acc.TargetFitEligible {
		t.Fatalf("old confirmed decision resurrected account: %+v", acc)
	}
	acc = importAdversarialLead(t, svc, org, adversarialLead("11222333000181", "ACME ENGENHARIA LTDA", TargetFitConfirmed, "2026-08-11T12:00:00Z", true), "confirmed-equal-watermark")
	if acc.TargetFitClass != TargetFitOutOfScope || acc.TargetFitEligible {
		t.Fatalf("equal-watermark confirmed decision resurrected account: %+v", acc)
	}
}

func TestOlderTargetFitOutCannotSuppressNewerConfirmedDecision(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(Config{Enabled: true, DynamicPriorityEnabled: true}, repo, nil).(*service)
	org := uuid.New()
	acc := importAdversarialLead(t, svc, org, adversarialLead("11222333000181", "ACME ENGENHARIA LTDA", TargetFitConfirmed, "2026-08-11T12:00:00Z", true), "confirmed-new")
	acc = importAdversarialLead(t, svc, org, adversarialLead("11222333000181", "ACME ENGENHARIA LTDA", TargetFitOutOfScope, "2026-08-10T12:00:00Z", true), "out-stale-arrival")
	if acc.TargetFitClass != TargetFitConfirmed || !acc.TargetFitEligible || acc.QueueState == models.OutreachQueueTargetFitSuppressed {
		t.Fatalf("old OUT decision suppressed current confirmed account: %+v", acc)
	}
}

func TestTargetFitMissingAndContradictoryEmailReadyFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		lead   FeedLead
		reason string
	}{
		{"historical missing", adversarialLead("11222333000181", "LEGACY", "", "", false), TargetFitReasonMissing},
		{"EMAIL_SEND_READY with TARGET_OUT", adversarialLead("11222333000182", "CONTRADICTORY", TargetFitOutOfScope, "2026-08-11T12:00:00Z", true), TargetFitReasonOut},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMemRepo()
			svc := NewService(Config{Enabled: true, DynamicPriorityEnabled: true}, repo, nil).(*service)
			acc := importAdversarialLead(t, svc, uuid.New(), tc.lead, "case-"+tc.lead.Company.CNPJ14)
			if acc.TargetFitEligible || acc.TargetFitSuppressionReason != tc.reason {
				t.Fatalf("fail-open: %+v", acc)
			}
		})
	}
}
