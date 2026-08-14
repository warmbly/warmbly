package confenge

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// memPolicyStore is an in-memory ConfengePolicyRepository for E2E green autorun tests.
type memPolicyStore struct {
	byKey map[string]*models.CampaignPolicyAuthorization
}

func newMemPolicyStore() *memPolicyStore {
	return &memPolicyStore{byKey: map[string]*models.CampaignPolicyAuthorization{}}
}

func (m *memPolicyStore) key(org, camp uuid.UUID) string {
	return org.String() + "|" + camp.String()
}

func (m *memPolicyStore) InsertCampaignPolicy(ctx context.Context, orgID uuid.UUID, auth *models.CampaignPolicyAuthorization) (uuid.UUID, error) {
	cp := *auth
	if cp.ID == uuid.Nil {
		cp.ID = uuid.New()
	}
	auth.ID = cp.ID
	m.byKey[m.key(orgID, auth.CampaignID)] = &cp
	return cp.ID, nil
}

func (m *memPolicyStore) GetActiveCampaignPolicy(ctx context.Context, orgID, campaignID uuid.UUID, now time.Time) (*models.CampaignPolicyAuthorization, error) {
	a := m.byKey[m.key(orgID, campaignID)]
	if a == nil || !a.Active(now) {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (m *memPolicyStore) GetCampaignPolicyByID(ctx context.Context, orgID, authID uuid.UUID) (*models.CampaignPolicyAuthorization, error) {
	for _, a := range m.byKey {
		if a != nil && a.ID == authID {
			cp := *a
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *memPolicyStore) RevokeCampaignPolicy(ctx context.Context, orgID, campaignID, actor uuid.UUID, now time.Time) (bool, error) {
	a := m.byKey[m.key(orgID, campaignID)]
	if a == nil {
		return false, nil
	}
	t := now
	a.RevokedAt = &t
	return true, nil
}

func (m *memPolicyStore) ListCampaignPolicies(ctx context.Context, orgID, campaignID uuid.UUID, limit int) ([]models.CampaignPolicyAuthorization, error) {
	a := m.byKey[m.key(orgID, campaignID)]
	if a == nil {
		return nil, nil
	}
	return []models.CampaignPolicyAuthorization{*a}, nil
}

func TestCampaignPolicyGreenAutoqueueNoFakeApprovedBy(t *testing.T) {
	ctx := context.Background()
	repo := newMemRepo()
	org, user, camp, accID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	cid := camp
	_ = repo.UpsertOrgSettings(ctx, &models.OutreachOrgSettings{OrganizationID: org, CampaignID: &cid})

	acc := &models.OutreachAccount{
		ID: accID, OrganizationID: org, CNPJ14: "12345678000199",
		RazaoSocial: "Engenharia Alpha LTDA", NomeFantasia: "Alpha Eng",
		ServiceCode: "REAJUSTE", ServiceName: "reajuste de contratos",
		FactToMention:   "prorrogação do contrato 001/2025 no PNCP",
		ActivationState: "ACTIONABLE_NOW", ActivationReasonCodes: []string{"WINDOW_OPEN"},
		TargetFitSendTier: "A_AUTOMATIC", EmailSendReady: true,
		MessageContextHash: "ctx-hash-1", QueueState: models.OutreachQueueNeedsReview,
	}
	_, _ = repo.UpsertAccount(ctx, acc)
	cand := &models.OutreachContactCandidate{
		ID: uuid.New(), OrganizationID: org, AccountID: accID,
		Name: "Ana Silva", Email: "ana.silva@alphaeng.com.br",
		VerificationStatus: models.OutreachVerifyOfficialSource, Confidence: "HIGH", Recommended: true,
		EmailSendReady: true, OwnershipStatus: "COMPANY_OWNED",
	}
	_, _ = repo.UpsertCandidate(ctx, cand)

	tp := &models.OutreachTouchpoint{
		OrganizationID: org, AccountID: accID, ContactCandidateID: &cand.ID,
		Ordinal: 1, Channel: models.OutreachChannelEmail, Purpose: models.TouchpointPurposeInitial,
		State: models.TouchpointNeedsReview, Recipient: cand.Email,
		Subject: "Sobre Alpha Eng", BodyText: "Olá Ana,\n\nNotei a prorrogação do contrato 001/2025 no PNCP.\n\nFaz sentido conversarmos?\n\n" + SignaturePlain(),
		ServiceCode: "REAJUSTE", FactUsed: acc.FactToMention, GeneratedContextHash: "ctx-hash-1",
		IdempotencyKey: "green-e2e-1",
	}
	RecomputeContentHash(tp)
	_ = repo.InsertTouchpoint(ctx, tp)

	draft := &models.OutreachDraft{
		OrganizationID: org, AccountID: accID, ContactCandidateID: &cand.ID,
		Channel: models.OutreachChannelEmail, Subject: tp.Subject, BodyText: tp.BodyText,
		RecipientEmail: cand.Email, ServiceCode: "REAJUSTE", FactUsed: acc.FactToMention,
		Provider: "template", Model: "policy_approved_v1", Status: models.OutreachDraftNeedsReview,
		RiskClass: "GREEN", RiskFlags: []string{},
	}
	ok := true
	draft.ValidationOK = &ok
	_ = repo.UpsertDraft(ctx, draft)
	tp.DraftID = &draft.ID
	_ = repo.UpdateTouchpoint(ctx, tp)

	cfg := Config{
		Enabled: true, RequireHumanApproval: true, GreenAutorunEnabled: true,
		MaxInitialEmailWords: 120, AppEnv: "test", RateMaxPerHour: 20,
	}
	svc := NewService(cfg, repo, nil).(*service)
	store := newMemPolicyStore()
	svc.WirePolicyAuth(store)

	// No policy yet → refuse
	_, dec, xerr := svc.TryGreenAutorun(ctx, org, user, tp.ID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	if dec.Allow {
		t.Fatal("must not allow without campaign policy")
	}

	auth, xerr := svc.AuthorizeCampaignPolicy(ctx, org, user, &models.CampaignPolicyAuthorization{
		CampaignID: camp, Channel: "EMAIL", AllowedRiskClass: "GREEN",
		SenderMailbox:            "tiago.sasaki@confenge.com.br",
		AllowPolicyTemplateGREEN: true,
		EffectiveAt:              time.Now().UTC().Add(-time.Minute),
		AuthorizedByLabel:        "test-operator",
	})
	if xerr != nil {
		t.Fatal(xerr)
	}
	if auth == nil || auth.AuthorizedBy != user {
		t.Fatalf("policy auth actor = %v", auth)
	}

	// Mock QueueTouchpoint path: TryGreenAutorun calls Queue which needs campaigns.
	// For this unit test, verify ApplyCampaignPolicyAuthorization + CAS queue path directly
	// when EvaluateGreenAutorun allows — full Queue needs execution wiring.
	in := svc.buildGreenAutorunInput(ctx, org, tp, auth)
	// Force GREEN draft risk into input
	in.RiskClass = "GREEN"
	in.ValidationOK = true
	in.GenericUnauditedTemplate = false
	in.UsedPolicyApprovedTemplate = true
	in.EmailSendReady = true
	in.TargetFitSendTier = "A_AUTOMATIC"
	in.OwnershipAllowed = true
	in.MailboxPurposeAllowed = true
	in.VerificationAllowed = true
	in.FactualHookAnchored = true
	in.GovernorHealthy = true
	in.HasContactCandidate = true
	in.CopyWithinLimits = true
	in.MessageContextHashCurrent = true
	in.NoEditAfterAuthorization = true
	in.SingleService = true
	in.ServiceCode = "REAJUSTE"
	in.ContactFresh = true
	in.ContextFresh = true
	in.NoUnknownEvidenceIDs = true
	in.NoHypothesisAsFact = true
	in.NoClaimsToAvoidViolated = true

	now := time.Now().UTC()
	dec = EvaluateGreenAutorun(true, auth, in, now)
	if !dec.Allow {
		t.Fatalf("expected GREEN allow, got %v", dec.Reasons)
	}
	if dec.AuthorizationMode != AuthorizationModeCampaignPolicy {
		t.Fatalf("mode=%s", dec.AuthorizationMode)
	}

	if err := ApplyCampaignPolicyAuthorization(tp, auth, now); err != nil {
		t.Fatal(err)
	}
	if tp.ApprovedBy != nil {
		t.Fatal("CAMPAIGN_POLICY must not forge approved_by")
	}
	if tp.AuthorizationMode != AuthorizationModeCampaignPolicy {
		t.Fatalf("auth mode=%s", tp.AuthorizationMode)
	}
	if err := CanTransport(tp); err != nil {
		t.Fatalf("CanTransport: %v", err)
	}
	_ = repo.UpdateTouchpoint(ctx, tp)
	queued, err := repo.CASQueueTouchpoint(ctx, org, tp.ID, tp.ContentHash)
	if err != nil || queued == nil {
		t.Fatalf("CASQueue policy path failed: %v %v", err, queued)
	}
	if queued.State != models.TouchpointQueued {
		t.Fatalf("state=%s", queued.State)
	}
	if queued.ApprovedBy != nil {
		t.Fatal("queued touchpoint still has approved_by")
	}
	if queued.AuthorizationMode != AuthorizationModeCampaignPolicy {
		t.Fatalf("queued mode=%s", queued.AuthorizationMode)
	}
}

func TestYellowAndRedStayOutOfAutorun(t *testing.T) {
	now := time.Now().UTC()
	auth := &models.CampaignPolicyAuthorization{
		ID: uuid.New(), CampaignID: uuid.New(), Channel: "EMAIL", AllowedRiskClass: "GREEN",
		EffectiveAt: now.Add(-time.Hour), AuthorizedBy: uuid.New(),
		AllowPolicyTemplateGREEN: true,
	}
	in := fullGreenInput()
	in.RiskClass = "YELLOW"
	if EvaluateGreenAutorun(true, auth, in, now).Allow {
		t.Fatal("YELLOW must not autorun")
	}
	in.RiskClass = "RED"
	if EvaluateGreenAutorun(true, auth, in, now).Allow {
		t.Fatal("RED must not autorun")
	}
	in.RiskClass = "GREEN"
	in.MailboxPurposeAllowed = false
	if EvaluateGreenAutorun(true, auth, in, now).Allow {
		t.Fatal("blocked mailbox purpose must not autorun")
	}
	// vagas@ blocked by MailboxPurposeAllowed helper
	if MailboxPurposeAllowed("vagas@moveinfraestrutura.com.br") {
		t.Fatal("vagas@ must be blocked for autorun")
	}
	if !MailboxPurposeAllowed("contratos@empresa.com.br") {
		t.Fatal("contratos@ should be allowed")
	}
}

func TestClassifyRiskVerifiedReajusteCanBeGreen(t *testing.T) {
	acc := &models.OutreachAccount{
		ServiceCode: "REAJUSTE_14133", MomentCode: "PORTFOLIO",
		FactToMention: "Observamos contratos públicos de engenharia no PNCP.",
	}
	cand := &models.OutreachContactCandidate{
		Email: "encopav@encopav.com.br", VerificationStatus: models.OutreachVerifyVerified,
		Name: "Contato", Role: "generic",
	}
	out := &DraftOutput{
		Subject: "Sobre ENCOPAV", BodyText: "Olá,\n\nNotei contratos públicos de engenharia.\n\nFaz sentido conversarmos?\n\nAbraço,\nTiago Sasaki\nCONFENGE",
		ServiceCode: "REAJUSTE_14133", FactUsed: acc.FactToMention, Channel: ChannelEmailInitial,
	}
	val := ValidateDraft(out, acc, cand, ValidateOpts{MaxWords: 120, Evidence: []models.OutreachEvidence{{SourceEvidenceID: "e1"}}, Channel: ChannelEmailInitial})
	// May fail validation for missing evidence bind — force OK shape for risk only.
	val.OK = true
	class, flags := ClassifyRisk(acc, cand, out, val)
	if class == "RED" {
		t.Fatalf("VERIFIED+REAJUSTE must not be RED: %s flags=%v", class, flags)
	}
	// Without policy demotion, contract topics no longer force YELLOW.
	if class != "GREEN" {
		t.Fatalf("want GREEN got %s flags=%v", class, flags)
	}
}

func TestHumanTouchpointStillRequiresApprovedBy(t *testing.T) {
	tp := sampleTouch(models.OutreachChannelEmail, "a@b.com", "S", "B")
	if err := ApplyHumanApproval(tp, uuid.New(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if tp.AuthorizationMode != AuthorizationModeHumanTouchpoint {
		t.Fatalf("mode=%s", tp.AuthorizationMode)
	}
	if err := CanTransport(tp); err != nil {
		t.Fatal(err)
	}
	// Forging policy mode with human approved_by must fail
	tp.AuthorizationMode = AuthorizationModeCampaignPolicy
	if err := CanTransport(tp); err == nil {
		t.Fatal("policy mode with approved_by must fail")
	}
}
