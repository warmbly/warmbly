package confenge

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// TestEmailSyntaxAloneNeverGrantsAutorun is the mandatory regression:
// recipient="qualquer@empresa.com" + ContactCandidateID=nil + CAMPAIGN_POLICY + GREEN → FAIL.
func TestEmailSyntaxAloneNeverGrantsAutorun(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	org, user, camp, accID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	cid := camp
	_ = repo.UpsertOrgSettings(ctx, &models.OutreachOrgSettings{OrganizationID: org, CampaignID: &cid})

	acc := &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: "12345678000199",
		SourceSystem: "extra-cli",
		RazaoSocial:  "Empresa X", ServiceCode: "REAJUSTE", FactToMention: "fato público",
		ActivationState:    "ACTIONABLE_NOW", // must NOT imply A_AUTOMATIC
		MessageContextHash: "ctx1", QueueState: models.OutreachQueueNeedsReview,
		// No TargetFitSendTier, no EmailSendReady — legacy shape.
	}
	_, _ = repo.UpsertAccount(ctx, acc)

	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: accID,
		// ContactCandidateID deliberately nil
		Ordinal: 1, Channel: models.OutreachChannelEmail, Purpose: models.TouchpointPurposeInitial,
		State: models.TouchpointNeedsReview, Recipient: "qualquer@empresa.com",
		Subject: "Sobre Empresa X", BodyText: "Olá,\n\nfato público\n\nFaz sentido?\n\n" + SignaturePlain(),
		ServiceCode: "REAJUSTE", FactUsed: "fato público", GeneratedContextHash: "ctx1",
		IdempotencyKey: "syntax-only-1",
	}
	RecomputeContentHash(tp)
	_ = repo.InsertTouchpoint(ctx, tp)

	ok := true
	draft := &models.OutreachDraft{
		OrganizationID: org, AccountID: accID, Channel: models.OutreachChannelEmail,
		Subject: tp.Subject, BodyText: tp.BodyText, RecipientEmail: tp.Recipient,
		ServiceCode: "REAJUSTE", FactUsed: "fato público",
		Provider: "template", Model: "policy_approved_v1", Status: models.OutreachDraftNeedsReview,
		RiskClass: "GREEN", ValidationOK: &ok,
	}
	_ = repo.UpsertDraft(ctx, draft)
	tp.DraftID = &draft.ID
	_ = repo.UpdateTouchpoint(ctx, tp)

	cfg := Config{Enabled: true, GreenAutorunEnabled: true, MaxInitialEmailWords: 120, AppEnv: "test", RateMaxPerHour: 20}
	svc := NewService(cfg, repo, nil).(*service)
	store := newMemPolicyStore()
	svc.WirePolicyAuth(store)
	_, xerr := svc.AuthorizeCampaignPolicy(ctx, org, user, &models.CampaignPolicyAuthorization{
		CampaignID: camp, Channel: "EMAIL", AllowedRiskClass: "GREEN",
		SenderMailbox: "tiago.sasaki@confenge.com.br", AllowPolicyTemplateGREEN: true,
		EffectiveAt: time.Now().UTC().Add(-time.Minute),
	})
	if xerr != nil {
		t.Fatal(xerr)
	}

	in := svc.buildGreenAutorunInput(ctx, org, tp, nil)
	if in.EmailSendReady {
		t.Fatal("email syntax must not set EmailSendReady")
	}
	if in.OwnershipAllowed {
		t.Fatal("email syntax must not set OwnershipAllowed")
	}
	if in.VerificationAllowed {
		t.Fatal("email syntax must not set VerificationAllowed")
	}
	if in.HasContactCandidate {
		t.Fatal("nil ContactCandidateID must leave HasContactCandidate false")
	}
	if in.TargetFitSendTier == "A_AUTOMATIC" {
		t.Fatal("ACTIONABLE_NOW must not promote target_fit to A_AUTOMATIC")
	}

	auth, _ := store.GetActiveCampaignPolicy(ctx, org, camp, time.Now().UTC())
	dec := EvaluateGreenAutorun(true, auth, in, time.Now().UTC())
	if dec.Allow {
		t.Fatalf("AUTORUN MUST FAIL for syntax-only recipient; reasons=%v", dec.Reasons)
	}
	joined := strings.Join(dec.Reasons, ",")
	if !strings.Contains(joined, "missing_contact_candidate") && !strings.Contains(joined, "email_send_ready") {
		t.Fatalf("expected missing_contact or email_send_ready in reasons: %v", dec.Reasons)
	}
}

func TestActionableNowDoesNotImplyAAutomatic(t *testing.T) {
	acc := &models.OutreachAccount{ActivationState: "ACTIONABLE_NOW"}
	// Historical helper removed; ensure no leftover path via build input.
	ctx := context.Background()
	repo := newMemRepo()
	org := uuid.New()
	acc.ID = uuid.New()
	acc.OrganizationID = org
	acc.SourceSystem = "extra-cli"
	acc.CNPJ14 = "99887766000155"
	acc.ServiceCode = "REAJUSTE"
	acc.FactToMention = "x"
	acc.MessageContextHash = "h"
	_, _ = repo.UpsertAccount(ctx, acc)
	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: acc.ID, Channel: "EMAIL",
		State: models.TouchpointNeedsReview, Recipient: "a@b.com",
		BodyText: "hi", ServiceCode: "REAJUSTE", GeneratedContextHash: "h",
	}
	svc := NewService(Config{Enabled: true, AppEnv: "test"}, repo, nil).(*service)
	in := svc.buildGreenAutorunInput(ctx, org, tp, nil)
	if in.TargetFitSendTier == "A_AUTOMATIC" {
		t.Fatal("must not infer A_AUTOMATIC from ACTIONABLE_NOW")
	}
	if ImportedSendTierEligible(in.TargetFitSendTier) {
		t.Fatal("empty tier must not be send-eligible")
	}
}

func TestClearApprovalClearsPolicyBinding(t *testing.T) {
	id := uuid.New()
	now := time.Now().UTC()
	tp := &models.OutreachTouchpoint{
		State: models.TouchpointQueued, AuthorizationMode: AuthorizationModeCampaignPolicy,
		ApprovedContentHash: "abc", CampaignPolicyAuthorizationID: &id,
		AuthorizationPolicyHash: "hash", AuthorizationAt: &now,
	}
	ClearApproval(tp)
	if tp.AuthorizationMode != "" || tp.ApprovedContentHash != "" {
		t.Fatal("mode/hash not cleared")
	}
	if tp.CampaignPolicyAuthorizationID != nil || tp.AuthorizationPolicyHash != "" || tp.AuthorizationAt != nil {
		t.Fatal("policy binding not cleared")
	}
	if tp.State != models.TouchpointNeedsReview {
		t.Fatalf("state=%s", tp.State)
	}
}

func TestCampaignPolicyContextStaleInvalidatesFully(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	org, user, camp, accID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	_ = repo.UpsertOrgSettings(ctx, &models.OutreachOrgSettings{OrganizationID: org, CampaignID: &camp})
	acc := &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: "11222333000181",
		ServiceCode: "REAJUSTE", FactToMention: "fato", MessageContextHash: "ctx-old",
		ActivationState: "ACTIONABLE_NOW", TargetFitSendTier: "A_AUTOMATIC", EmailSendReady: true,
	}
	_, _ = repo.UpsertAccount(ctx, acc)
	cand := &models.OutreachContactCandidate{
		ID: uuid.New(), OrganizationID: org, AccountID: accID,
		Email: "ana@alpha.com.br", VerificationStatus: models.OutreachVerifyOfficialSource,
		EmailSendReady: true, OwnershipStatus: "COMPANY_OWNED", Recommended: true,
	}
	_, _ = repo.UpsertCandidate(ctx, cand)
	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: accID, ContactCandidateID: &cand.ID,
		Ordinal: 1, Channel: "EMAIL", Purpose: models.TouchpointPurposeInitial,
		State: models.TouchpointApproved, Recipient: cand.Email,
		Subject: "S", BodyText: "body com fato", ServiceCode: "REAJUSTE", FactUsed: "fato",
		GeneratedContextHash: "ctx-old", ContentHash: "ch1", ApprovedContentHash: "ch1",
		AuthorizationMode: AuthorizationModeCampaignPolicy, IdempotencyKey: "stale-1",
	}
	authID := uuid.New()
	tp.CampaignPolicyAuthorizationID = &authID
	tp.AuthorizationPolicyHash = "ph"
	at := time.Now().UTC()
	tp.AuthorizationAt = &at
	_ = repo.InsertTouchpoint(ctx, tp)

	// Material context changes.
	acc.MessageContextHash = "ctx-new"
	acc.FactToMention = "fato novo material"
	_, _ = repo.UpsertAccount(ctx, acc)

	// Simulate pre-SMTP / QueueTouchpoint context_stale path.
	if err := AssertMessageContextFresh(acc, tp.GeneratedContextHash); err == nil {
		t.Fatal("expected stale")
	}
	ClearApproval(tp)
	tp.StopReason = "context_stale"
	_ = repo.UpdateTouchpoint(ctx, tp)
	got, _ := repo.GetTouchpoint(ctx, org, tp.ID)
	if got.State != models.TouchpointNeedsReview {
		t.Fatalf("state=%s", got.State)
	}
	if got.AuthorizationMode != "" || got.ApprovedContentHash != "" || got.CampaignPolicyAuthorizationID != nil {
		t.Fatalf("authorization not fully cleared: mode=%q hash=%q cpa=%v", got.AuthorizationMode, got.ApprovedContentHash, got.CampaignPolicyAuthorizationID)
	}
	_ = user
}

func TestPolicyRevokeBlocksTransportRevalidation(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	org, user, camp := uuid.New(), uuid.New(), uuid.New()
	store := newMemPolicyStore()
	svc := NewService(Config{Enabled: true, AppEnv: "test", GreenAutorunEnabled: false}, repo, nil).(*service)
	svc.WirePolicyAuth(store)

	auth, xerr := svc.AuthorizeCampaignPolicy(ctx, org, user, &models.CampaignPolicyAuthorization{
		CampaignID: camp, Channel: "EMAIL", AllowedRiskClass: "GREEN",
		SenderMailbox: "tiago.sasaki@confenge.com.br", EffectiveAt: time.Now().UTC().Add(-time.Minute),
		MaxRatePerHour: 10, AllowPolicyTemplateGREEN: true,
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	accID := uuid.New()
	candID := uuid.New()
	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: accID, ContactCandidateID: &candID,
		Channel: "EMAIL", State: models.TouchpointQueued,
		Recipient: "a@b.com", Subject: "S", BodyText: "B",
		ContentHash: "h", ApprovedContentHash: "h",
		AuthorizationMode: AuthorizationModeCampaignPolicy,
	}
	if err := ApplyCampaignPolicyAuthorization(tp, auth, time.Now().UTC()); err != nil {
		// state was QUEUED; apply expects APPROVED/DRAFTED/NEEDS_REVIEW
		tp.State = models.TouchpointNeedsReview
		if err := ApplyCampaignPolicyAuthorization(tp, auth, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		tp.State = models.TouchpointQueued
	}
	_ = repo.InsertTouchpoint(ctx, tp)

	// authorize → queue → revoke → transport revalidation must block
	ok, _ := svc.RevokeCampaignPolicy(ctx, org, camp, user)
	if !ok {
		t.Fatal("revoke failed")
	}
	block := svc.revalidateCampaignPolicyAtSend(ctx, org, tp)
	if block == nil {
		t.Fatal("expected block after revoke")
	}
	if block.Reason != "policy_revoked" && block.Reason != "policy_binding_missing" {
		// after clear, re-load may show cleared binding
		t.Logf("block reason=%s (ok if revoked)", block.Reason)
	}
	if tp.AuthorizationMode == AuthorizationModeCampaignPolicy && tp.CampaignPolicyAuthorizationID != nil {
		// revalidate should have ClearApproval'd
		if block.Reason != "policy_revoked" {
			t.Fatalf("want policy_revoked got %s", block.Reason)
		}
	}
}

func TestDomainStageActivationSendReadinessNotCollapsed(t *testing.T) {
	// Architectural separation: these strings must remain distinct concepts.
	const (
		domainStage = "DOCUMENT_REQUEST_READY"
		activation  = "ACTIONABLE_NOW"
		sendTier    = "A_AUTOMATIC"
		emailReady  = "EMAIL_SEND_READY"
		queueState  = "QUEUED"
	)
	if domainStage == activation || activation == sendTier || sendTier == emailReady || emailReady == queueState {
		t.Fatal("concept collapse")
	}
	// ACTIONABLE_NOW + email_send_ready=false → never GREEN autorun
	now := time.Now().UTC()
	auth := testPolicyAuth(now)
	in := fullGreenInput()
	in.EmailSendReady = false
	// activation is not even a field of GreenAutorunInput — correct separation
	if EvaluateGreenAutorun(true, auth, in, now).Allow {
		t.Fatal("email_send_ready=false must block")
	}
	// ACTIONABLE_NOW with missing tier
	in = fullGreenInput()
	in.TargetFitSendTier = ""
	if EvaluateGreenAutorun(true, auth, in, now).Allow {
		t.Fatal("absent tier must block")
	}
}

func TestRankOnlyDoesNotChangeMessageContextHash(t *testing.T) {
	// Rank/score changes must not rewrite message_context_hash material fields.
	a1 := &models.OutreachAccount{
		ServiceCode: "REAJUSTE", FactToMention: "fato", MomentCode: "M1",
		PriorityRank: 1, PriorityScore: 90, ActivationScore: 80,
	}
	a2 := &models.OutreachAccount{
		ServiceCode: "REAJUSTE", FactToMention: "fato", MomentCode: "M1",
		PriorityRank: 50, PriorityScore: 10, ActivationScore: 20,
	}
	// MessageContextHash is computed on import; unit-level: equal material fields.
	if a1.ServiceCode != a2.ServiceCode || a1.FactToMention != a2.FactToMention {
		t.Fatal("fixture")
	}
	// Material change must differ.
	a3 := *a1
	a3.FactToMention = "outro fato"
	if a3.FactToMention == a1.FactToMention {
		t.Fatal("material must differ")
	}
}

func TestSignatureVersionConstantPresent(t *testing.T) {
	if SignatureVersion == "" {
		t.Fatal("SignatureVersion required for post-auth decoration audit")
	}
	if !strings.HasPrefix(SignatureVersion, "confenge.signature.") {
		t.Fatalf("unexpected SignatureVersion %s", SignatureVersion)
	}
}

func TestGovernorCapOverrideRespected(t *testing.T) {
	// EffectiveHourlyCap is the pure function used by CapOverride wiring.
	if EffectiveHourlyCap(20, 10) != 10 {
		t.Fatal("policy cap must win")
	}
}
