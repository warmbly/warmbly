package confenge

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/confenge/dispatch"
	"github.com/warmbly/warmbly/internal/models"
)

// TestAuthorizeQueueRevokeGateCampaignEmailBlocks is the real final-send path:
// authorize → bind CAMPAIGN_POLICY → queue (APPROVED/QUEUED) → revoke → GateCampaignEmail
// must return non-Proceed (policy_revoked / deferred), not GateProceed.
func TestAuthorizeQueueRevokeGateCampaignEmailBlocks(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	org, user, camp := uuid.New(), uuid.New(), uuid.New()
	accID := uuid.New()
	candID := uuid.New()

	// Wire service with policy store + governor (required for GateCampaignEmail).
	svc := NewService(Config{
		Enabled: true, AppEnv: "test", GreenAutorunEnabled: false,
		RateMaxPerHour: 20,
	}, repo, nil).(*service)
	store := newMemPolicyStore()
	svc.WirePolicyAuth(store)
	svc.WireDispatchGovernor(dispatch.NewGovernor(dispatch.LoadConfig(), dispatch.NewMemoryStore(), nil))

	acc := &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: "99887766000111",
		RazaoSocial: "Revoke Gate Co", ServiceCode: "REAJUSTE",
		FactToMention: "fato", MessageContextHash: "ctx-r1",
		TargetFitSendTier: "A_AUTOMATIC", EmailSendReady: true,
		QueueState: models.OutreachQueueNeedsReview,
	}
	_, _ = repo.UpsertAccount(ctx, acc)
	cand := &models.OutreachContactCandidate{
		ID: candID, OrganizationID: org, AccountID: accID,
		Name: "Rev", Email: "revoke.gate@example.com",
		VerificationStatus: models.OutreachVerifyOfficialSource,
		EmailSendReady:     true, OwnershipStatus: "COMPANY_OWNED", Recommended: true,
	}
	_, _ = repo.UpsertCandidate(ctx, cand)

	auth, xerr := svc.AuthorizeCampaignPolicy(ctx, org, user, &models.CampaignPolicyAuthorization{
		CampaignID: camp, Channel: "EMAIL", AllowedRiskClass: "GREEN",
		SenderMailbox: "tiago.sasaki@confenge.com.br", AllowPolicyTemplateGREEN: true,
		EffectiveAt: time.Now().UTC().Add(-time.Minute), MaxRatePerHour: 10,
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if auth.ID == uuid.Nil {
		t.Fatal("auth id required")
	}

	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: accID, ContactCandidateID: &candID,
		Ordinal: 1, Channel: models.OutreachChannelEmail, Purpose: models.TouchpointPurposeInitial,
		State: models.TouchpointNeedsReview, Recipient: cand.Email,
		Subject: "S", BodyText: "body com fato", ServiceCode: "REAJUSTE", FactUsed: "fato",
		GeneratedContextHash: "ctx-r1", IdempotencyKey: "revoke-gate-e2e-1",
	}
	RecomputeContentHash(tp)
	if err := ApplyCampaignPolicyAuthorization(tp, auth, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	// Simulate CAS-queue: state QUEUED (as after queue, before worker SMTP).
	tp.State = models.TouchpointQueued
	now := time.Now().UTC()
	tp.QueuedAt = &now
	if err := repo.InsertTouchpoint(ctx, tp); err != nil {
		t.Fatal(err)
	}

	// Revoke the campaign policy while message is still unsent.
	ok, xerr := svc.RevokeCampaignPolicy(ctx, org, camp, user)
	if xerr != nil || !ok {
		t.Fatalf("revoke ok=%v err=%v", ok, xerr)
	}

	// Real final-send path: GateCampaignEmail must not Proceed.
	res := svc.GateCampaignEmail(ctx, org, "CONFENGE National", cand.Email, camp, candID, uuid.New())
	if res.Kind == GateProceed {
		t.Fatalf("GateCampaignEmail MUST block after revoke; got Proceed reason=%s", res.Reason)
	}
	if res.Reason != "policy_revoked" && res.Reason != "policy_binding_missing" && res.Reason != "transport_invalid" {
		// After ClearApproval, mode is empty so subsequent list may skip;
		// first hit must have been policy_revoked.
		t.Logf("gate kind=%d reason=%s (acceptable if revoke cleared binding)", res.Kind, res.Reason)
	}
	if res.Kind == GateProceed {
		t.Fatal("unreachable")
	}

	// Touchpoint must no longer be transportable under CAMPAIGN_POLICY.
	got, _ := repo.GetTouchpoint(ctx, org, tp.ID)
	if got == nil {
		t.Fatal("touchpoint missing")
	}
	// After revalidate, ClearApproval should have fired.
	if got.AuthorizationMode == AuthorizationModeCampaignPolicy && got.CampaignPolicyAuthorizationID != nil {
		// Gate may have deferred with ClearApproval — re-read after gate
		if block := svc.revalidateCampaignPolicyAtSend(ctx, org, got); block == nil {
			t.Fatal("live revalidate must still fail for revoked grant")
		}
	}
	if err := svc.AssertTransportable(ctx, org, got); err == nil {
		// If state still QUEUED with cleared mode, CanTransport may require human approved_by
		// which is also a block. Either way must not be transportable as CAMPAIGN_POLICY.
		if got.AuthorizationMode == AuthorizationModeCampaignPolicy {
			t.Fatal("AssertTransportable must fail after revoke for policy mode")
		}
	}
}

// TestQueueTouchpointAfterRevokeBlocks drives QueueTouchpoint (not only revalidate helper).
func TestQueueTouchpointAfterRevokeBlocks(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	org, user, camp := uuid.New(), uuid.New(), uuid.New()
	accID, candID := uuid.New(), uuid.New()

	svc := NewService(Config{Enabled: true, AppEnv: "test"}, repo, nil).(*service)
	store := newMemPolicyStore()
	svc.WirePolicyAuth(store)
	svc.WireDispatchGovernor(dispatch.NewGovernor(dispatch.LoadConfig(), dispatch.NewMemoryStore(), nil))

	acc := &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: "88776655000122",
		ServiceCode: "REAJUSTE", FactToMention: "f", MessageContextHash: "ctx-q",
	}
	_, _ = repo.UpsertAccount(ctx, acc)
	cand := &models.OutreachContactCandidate{
		ID: candID, OrganizationID: org, AccountID: accID,
		Email: "queue.revoke@example.com", VerificationStatus: models.OutreachVerifyOfficialSource,
		EmailSendReady: true, OwnershipStatus: "COMPANY_OWNED",
	}
	_, _ = repo.UpsertCandidate(ctx, cand)

	auth, xerr := svc.AuthorizeCampaignPolicy(ctx, org, user, &models.CampaignPolicyAuthorization{
		CampaignID: camp, Channel: "EMAIL", AllowedRiskClass: "GREEN",
		EffectiveAt: time.Now().UTC().Add(-time.Minute), SenderMailbox: "tiago.sasaki@confenge.com.br",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}

	// Minimal draft for Enroll path if queue gets that far (must not).
	draft := &models.OutreachDraft{
		OrganizationID: org, AccountID: accID, ContactCandidateID: &candID,
		Channel: models.OutreachChannelEmail, Subject: "S", BodyText: "body",
		RecipientEmail: cand.Email, Status: models.OutreachDraftApproved,
		ServiceCode: "REAJUSTE", RiskClass: "GREEN",
	}
	_ = repo.UpsertDraft(ctx, draft)

	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: accID, ContactCandidateID: &candID,
		DraftID: &draft.ID, Ordinal: 1, Channel: "EMAIL", Purpose: models.TouchpointPurposeInitial,
		State: models.TouchpointNeedsReview, Recipient: cand.Email,
		Subject: "S", BodyText: "body", ServiceCode: "REAJUSTE", FactUsed: "f",
		GeneratedContextHash: "ctx-q", IdempotencyKey: "queue-revoke-1",
	}
	RecomputeContentHash(tp)
	if err := ApplyCampaignPolicyAuthorization(tp, auth, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	_ = repo.InsertTouchpoint(ctx, tp)

	// Revoke before queue.
	if ok, _ := svc.RevokeCampaignPolicy(ctx, org, camp, user); !ok {
		t.Fatal("revoke")
	}

	// QueueTouchpoint must fail via AssertTransportable → revalidate.
	_, xerr = svc.QueueTouchpoint(ctx, org, user, tp.ID)
	if xerr == nil {
		t.Fatal("QueueTouchpoint MUST fail after policy revoke")
	}
	msg := strings.ToLower(xerr.Message)
	if !strings.Contains(msg, "policy") && !strings.Contains(msg, "revok") && !strings.Contains(msg, "blocked") && !strings.Contains(msg, "send blocked") {
		t.Fatalf("unexpected error: %s", xerr.Message)
	}
}

// TestGateCampaignEmailRevokeWithoutMessageContextHash still blocks.
// Proves revalidation is not nested under MessageContextHash != "".
func TestGateCampaignEmailRevokeWithoutMessageContextHash(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	org, user, camp := uuid.New(), uuid.New(), uuid.New()
	accID, candID := uuid.New(), uuid.New()

	svc := NewService(Config{Enabled: true, AppEnv: "test"}, repo, nil).(*service)
	store := newMemPolicyStore()
	svc.WirePolicyAuth(store)
	svc.WireDispatchGovernor(dispatch.NewGovernor(dispatch.LoadConfig(), dispatch.NewMemoryStore(), nil))

	// Empty MessageContextHash — old bug skipped revalidation entirely.
	acc := &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: "77665544000133",
		ServiceCode: "REAJUSTE", MessageContextHash: "",
	}
	_, _ = repo.UpsertAccount(ctx, acc)
	cand := &models.OutreachContactCandidate{
		ID: candID, OrganizationID: org, AccountID: accID,
		Email: "noctx@example.com", VerificationStatus: models.OutreachVerifyOfficialSource,
		EmailSendReady: true, OwnershipStatus: "COMPANY_OWNED",
	}
	_, _ = repo.UpsertCandidate(ctx, cand)

	auth, xerr := svc.AuthorizeCampaignPolicy(ctx, org, user, &models.CampaignPolicyAuthorization{
		CampaignID: camp, Channel: "EMAIL", AllowedRiskClass: "GREEN",
		EffectiveAt: time.Now().UTC().Add(-time.Minute), SenderMailbox: "tiago.sasaki@confenge.com.br",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}

	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: accID, ContactCandidateID: &candID,
		Channel: "EMAIL", State: models.TouchpointQueued, Recipient: cand.Email,
		Subject: "S", BodyText: "B", GeneratedContextHash: "", // empty
		IdempotencyKey: "noctx-1",
	}
	RecomputeContentHash(tp)
	if err := ApplyCampaignPolicyAuthorization(tp, auth, time.Now().UTC()); err != nil {
		// Apply requires certain states; force binding
		tp.State = models.TouchpointNeedsReview
		if err := ApplyCampaignPolicyAuthorization(tp, auth, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	tp.State = models.TouchpointQueued
	_ = repo.InsertTouchpoint(ctx, tp)

	if ok, _ := svc.RevokeCampaignPolicy(ctx, org, camp, user); !ok {
		t.Fatal("revoke")
	}

	res := svc.GateCampaignEmail(ctx, org, "CONFENGE Pilot", cand.Email, camp, candID, uuid.New())
	if res.Kind == GateProceed {
		t.Fatal("empty MessageContextHash must not skip policy revoke check")
	}
}

// TestAssertTransportableAfterRevokeFails proves CanTransport alone is not enough;
// AssertTransportable loads the live grant.
func TestAssertTransportableAfterRevokeFails(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	org, user, camp := uuid.New(), uuid.New(), uuid.New()
	svc := NewService(Config{Enabled: true, AppEnv: "test"}, repo, nil).(*service)
	store := newMemPolicyStore()
	svc.WirePolicyAuth(store)

	auth, xerr := svc.AuthorizeCampaignPolicy(ctx, org, user, &models.CampaignPolicyAuthorization{
		CampaignID: camp, Channel: "EMAIL", AllowedRiskClass: "GREEN",
		EffectiveAt: time.Now().UTC().Add(-time.Minute), SenderMailbox: "tiago.sasaki@confenge.com.br",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	candID := uuid.New()
	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: uuid.New(), ContactCandidateID: &candID,
		Channel: "EMAIL", State: models.TouchpointApproved, Recipient: "a@b.com",
		Subject: "S", BodyText: "body",
	}
	RecomputeContentHash(tp)
	if err := ApplyCampaignPolicyAuthorization(tp, auth, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	// Structural CanTransport still passes (binding fields present).
	if err := CanTransport(tp); err != nil {
		t.Fatalf("structural CanTransport: %v", err)
	}
	// Revoke live grant.
	if ok, _ := svc.RevokeCampaignPolicy(ctx, org, camp, user); !ok {
		t.Fatal("revoke")
	}
	// AssertTransportable must fail by loading revoked grant.
	if err := svc.AssertTransportable(ctx, org, tp); err == nil {
		t.Fatal("AssertTransportable must fail after revoke")
	}
	// CanTransport may still pass until ClearApproval — that is why callers MUST use AssertTransportable.
	_ = CanTransport(tp)
}

// TestRequireTouchTransportAfterRevokeBlocks is the EnrollDraft/API path:
// requireTouchTransport must use AssertTransportable, not structural CanTransport only.
func TestRequireTouchTransportAfterRevokeBlocks(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	org, user, camp := uuid.New(), uuid.New(), uuid.New()
	accID, candID := uuid.New(), uuid.New()

	svc := NewService(Config{Enabled: true, AppEnv: "test", RequireHumanApproval: true}, repo, nil).(*service)
	store := newMemPolicyStore()
	svc.WirePolicyAuth(store)

	acc := &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: "55443322000155",
		ServiceCode: "REAJUSTE", FactToMention: "f", MessageContextHash: "ctx-e",
	}
	_, _ = repo.UpsertAccount(ctx, acc)
	cand := &models.OutreachContactCandidate{
		ID: candID, OrganizationID: org, AccountID: accID,
		Email: "enroll.revoke@example.com", VerificationStatus: models.OutreachVerifyOfficialSource,
		EmailSendReady: true, OwnershipStatus: "COMPANY_OWNED", Recommended: true,
	}
	_, _ = repo.UpsertCandidate(ctx, cand)

	auth, xerr := svc.AuthorizeCampaignPolicy(ctx, org, user, &models.CampaignPolicyAuthorization{
		CampaignID: camp, Channel: "EMAIL", AllowedRiskClass: "GREEN",
		EffectiveAt: time.Now().UTC().Add(-time.Minute), SenderMailbox: "tiago.sasaki@confenge.com.br",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}

	draft := &models.OutreachDraft{
		OrganizationID: org, AccountID: accID, ContactCandidateID: &candID,
		Channel: models.OutreachChannelEmail, Subject: "S", BodyText: "body content here",
		RecipientEmail: cand.Email, Status: models.OutreachDraftApproved,
		ServiceCode: "REAJUSTE", RiskClass: "GREEN", FactUsed: "f",
	}
	_ = repo.UpsertDraft(ctx, draft)

	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: accID, ContactCandidateID: &candID,
		DraftID: &draft.ID, Ordinal: 1, Channel: "EMAIL", Purpose: models.TouchpointPurposeInitial,
		State: models.TouchpointNeedsReview, Recipient: cand.Email,
		Subject: draft.Subject, BodyText: draft.BodyText, ServiceCode: "REAJUSTE", FactUsed: "f",
		GeneratedContextHash: "ctx-e", IdempotencyKey: "enroll-revoke-1",
	}
	RecomputeContentHash(tp)
	if err := ApplyCampaignPolicyAuthorization(tp, auth, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	_ = repo.InsertTouchpoint(ctx, tp)

	if err := CanTransport(tp); err != nil {
		t.Fatalf("structural CanTransport: %v", err)
	}
	if xerr := AssertTransportAllowed(tp); xerr != nil {
		t.Fatalf("structural AssertTransportAllowed: %v", xerr)
	}

	if ok, _ := svc.RevokeCampaignPolicy(ctx, org, camp, user); !ok {
		t.Fatal("revoke")
	}

	_, xerr = svc.requireTouchTransport(ctx, org, draft.ID)
	if xerr == nil {
		t.Fatal("requireTouchTransport MUST fail after policy revoke")
	}
	msg := strings.ToLower(xerr.Message)
	if !strings.Contains(msg, "policy") && !strings.Contains(msg, "revok") && !strings.Contains(msg, "blocked") {
		t.Fatalf("unexpected requireTouchTransport error: %s", xerr.Message)
	}

	_, xerr = svc.EnrollDraft(ctx, org, user, draft.ID)
	if xerr == nil {
		t.Fatal("EnrollDraft MUST fail after policy revoke")
	}
}

// TestGateCampaignEmailBlocksRevokedPolicyAfterSentResidual covers residual SENT
// touchpoints still bearing CAMPAIGN_POLICY binding after QueueTouchpoint→SENT.
func TestGateCampaignEmailBlocksRevokedPolicyAfterSentResidual(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	org, user, camp := uuid.New(), uuid.New(), uuid.New()
	accID, candID := uuid.New(), uuid.New()

	svc := NewService(Config{Enabled: true, AppEnv: "test"}, repo, nil).(*service)
	store := newMemPolicyStore()
	svc.WirePolicyAuth(store)
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	fixed := time.Date(2026, 8, 10, 12, 0, 0, 0, loc) // Monday business hours
	clock := &dispatch.FixedClock{T: fixed.UTC()}
	cfg := dispatch.LoadConfig()
	cfg.BusinessDaysOnly = true
	svc.WireDispatchGovernor(dispatch.NewGovernor(cfg, dispatch.NewMemoryStore(), clock))

	acc := &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: "44332211000166",
		ServiceCode: "REAJUSTE", MessageContextHash: "ctx-s",
	}
	_, _ = repo.UpsertAccount(ctx, acc)
	cand := &models.OutreachContactCandidate{
		ID: candID, OrganizationID: org, AccountID: accID,
		Email: "sent.residual@example.com", VerificationStatus: models.OutreachVerifyOfficialSource,
		EmailSendReady: true, OwnershipStatus: "COMPANY_OWNED",
	}
	_, _ = repo.UpsertCandidate(ctx, cand)

	auth, xerr := svc.AuthorizeCampaignPolicy(ctx, org, user, &models.CampaignPolicyAuthorization{
		CampaignID: camp, Channel: "EMAIL", AllowedRiskClass: "GREEN",
		EffectiveAt: time.Now().UTC().Add(-time.Minute), SenderMailbox: "tiago.sasaki@confenge.com.br",
		MaxRatePerHour: 10,
	})
	if xerr != nil {
		t.Fatal(xerr)
	}

	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: accID, ContactCandidateID: &candID,
		Channel: "EMAIL", State: models.TouchpointNeedsReview, Recipient: cand.Email,
		Subject: "S", BodyText: "body", GeneratedContextHash: "ctx-s",
		IdempotencyKey: "sent-residual-1",
	}
	RecomputeContentHash(tp)
	if err := ApplyCampaignPolicyAuthorization(tp, auth, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	tp.State = models.TouchpointSent
	now := time.Now().UTC()
	tp.SentAt = &now
	_ = repo.InsertTouchpoint(ctx, tp)

	if ok, _ := svc.RevokeCampaignPolicy(ctx, org, camp, user); !ok {
		t.Fatal("revoke")
	}

	res := svc.GateCampaignEmail(ctx, org, "CONFENGE Residual", cand.Email, camp, candID, uuid.New())
	if res.Kind == GateProceed {
		t.Fatalf("GateCampaignEmail must not Proceed after revoke with residual SENT policy TP; reason=%s", res.Reason)
	}
	if res.Reason == "outside_business_day" || res.Reason == "outside_send_window" {
		t.Fatalf("got window deferral %q instead of policy_revoked — revoke not enforced on residual SENT", res.Reason)
	}
	if res.Reason != "policy_revoked" && res.Reason != "policy_binding_missing" && res.Reason != "policy_hash_mismatch" {
		t.Logf("gate kind=%d reason=%s (must not be Proceed)", res.Kind, res.Reason)
	}
}

// TestNoEditAfterAuthorizationDerivedFromHash proves the predicate is not a constant true.
func TestNoEditAfterAuthorizationDerivedFromHash(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	org := uuid.New()
	accID := uuid.New()
	candID := uuid.New()
	svc := NewService(Config{Enabled: true, AppEnv: "test"}, repo, nil).(*service)

	acc := &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: "66554433000144",
		ServiceCode: "REAJUSTE", FactToMention: "f", MessageContextHash: "c",
		TargetFitSendTier: "A_AUTOMATIC", ActivationState: "ACTIONABLE_NOW",
	}
	_, _ = repo.UpsertAccount(ctx, acc)
	cand := &models.OutreachContactCandidate{
		ID: candID, OrganizationID: org, AccountID: accID,
		Email: "edit@example.com", VerificationStatus: models.OutreachVerifyOfficialSource,
		EmailSendReady: true, OwnershipStatus: "COMPANY_OWNED",
	}
	_, _ = repo.UpsertCandidate(ctx, cand)

	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: accID, ContactCandidateID: &candID,
		Channel: "EMAIL", State: models.TouchpointNeedsReview, Recipient: cand.Email,
		Subject: "S", BodyText: "original body", ServiceCode: "REAJUSTE", FactUsed: "f",
		GeneratedContextHash: "c",
	}
	RecomputeContentHash(tp)
	// Unapproved: no edit-after-auth possible.
	in := svc.buildGreenAutorunInput(ctx, org, tp, nil)
	if !in.NoEditAfterAuthorization {
		t.Fatal("unapproved touchpoint should set NoEditAfterAuthorization true")
	}

	// Simulate approval then silent body edit without ClearApproval.
	tp.ApprovedContentHash = tp.ContentHash
	tp.AuthorizationMode = AuthorizationModeCampaignPolicy
	tp.BodyText = "EDITED after approval"
	RecomputeContentHash(tp) // content hash changes, approved hash stale
	in2 := svc.buildGreenAutorunInput(ctx, org, tp, nil)
	if in2.NoEditAfterAuthorization {
		t.Fatal("content hash mismatch after approval must set NoEditAfterAuthorization false")
	}
}
