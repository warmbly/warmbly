package confenge

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

func TestStructuralApproveBlockersIncompleteCopyContext(t *testing.T) {
	pb := MustPlaybook()
	acc := testAccount("gestao_monitoramento_contratual", "PORTFOLIO", "")
	acc.FactToMention = ""
	acc.MomentSummary = "momento comercial público"
	cand := testCand("Engenheira de contratos")
	st := PlanOutreachStrategy(pb, acc, cand, nil, 1)
	d := &models.OutreachDraft{
		ServiceCode: acc.ServiceCode,
		RiskClass:   "RED",
		RiskFlags:   []string{"incomplete_copy_context"},
		FactUsed:    "",
	}
	blockers := StructuralApproveBlockers(acc, cand, &st, d, pb)
	if len(blockers) == 0 {
		t.Fatal("expected blockers for incomplete_copy_context / hollow fields")
	}
	joined := strings.Join(blockers, " | ")
	if !strings.Contains(joined, "incomplete_copy_context") &&
		!strings.Contains(joined, "missing_why") &&
		!strings.Contains(joined, "missing_observed") {
		t.Fatalf("expected incompleteness blockers, got %v", blockers)
	}
}

func TestStructuralApproveBlockersUnknownService(t *testing.T) {
	pb := MustPlaybook()
	acc := testAccount("TOTALLY_UNKNOWN_SERVICE_XYZ", "PORTFOLIO", "contrato de pavimentação em 3 municípios do RS")
	cand := testCand("Engenheira de contratos")
	st := PlanOutreachStrategy(pb, acc, cand, nil, 1)
	d := &models.OutreachDraft{ServiceCode: acc.ServiceCode, RiskClass: "YELLOW", RiskFlags: st.RiskFlags, FactUsed: acc.FactToMention}
	blockers := StructuralApproveBlockers(acc, cand, &st, d, pb)
	if len(blockers) == 0 {
		t.Fatal("unknown service must block approve")
	}
	if !containsAnyFlag(stringsSliceKeys(blockers), "unknown_service_code") &&
		!strings.Contains(strings.Join(blockers, " "), "unknown_service_code") {
		t.Fatalf("expected unknown_service_code, got %v", blockers)
	}
}

func TestStructuralApproveBlockersMissingFields(t *testing.T) {
	pb := MustPlaybook()
	acc := testAccount("REAJUSTE", "ANUALIDADE", "contrato 1149/2022 aniversário de reajuste 2024")
	acc.MomentEvidenceIDs = []string{"ev-1"}
	cand := testCand("Engenheira de contratos")
	st := PlanOutreachStrategy(pb, acc, cand, nil, 1)
	// Force missing commercial fields (including account/draft fallbacks).
	st.WhyThisAccount = ""
	st.WhyNow = ""
	st.ObservedFact = ""
	st.MicroOfferCode = ""
	acc.FactToMention = ""
	d := &models.OutreachDraft{ServiceCode: "REAJUSTE", FactUsed: "", RiskClass: "YELLOW"}
	blockers := StructuralApproveBlockers(acc, cand, &st, d, pb)
	need := []string{"missing_why_this_account", "missing_why_now", "missing_observed_fact", "missing_micro_offer"}
	joined := strings.Join(blockers, " ")
	for _, n := range need {
		if !strings.Contains(joined, n) {
			t.Fatalf("expected %s in blockers %v", n, blockers)
		}
	}
}

func TestReviewDraftApproveFailsIncompleteAndUnknown(t *testing.T) {
	rf := newMemRepoWithSettings()
	cfg := Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120, RequireHumanApproval: true, MaxFeedPayloadBytes: DefaultMaxPayloadBytes}
	svc := NewService(cfg, rf, nil).(*service)

	org := uuid.New()
	user := uuid.New()
	acc := &models.OutreachAccount{
		ID: uuid.New(), OrganizationID: org,
		CNPJ14: "11222333000181", RazaoSocial: "ACME Obras LTDA", NomeFantasia: "ACME",
		ServiceCode: "TOTALLY_UNKNOWN_SERVICE_XYZ", ServiceName: "???",
		MomentCode: "PORTFOLIO", MomentSummary: "momento comercial público",
		FactToMention: "", // hollow
		QueueState:    models.OutreachQueueNeedsReview,
	}
	if _, err := rf.UpsertAccount(context.Background(), acc); err != nil {
		t.Fatal(err)
	}
	cand := &models.OutreachContactCandidate{
		ID: uuid.New(), OrganizationID: org, AccountID: acc.ID,
		Name: "Ana", Role: "Engenheira de contratos", Email: "ana@acme.example.com",
		VerificationStatus: models.OutreachVerifyOfficialSource, Recommended: true,
	}
	if _, err := rf.UpsertCandidate(context.Background(), cand); err != nil {
		t.Fatal(err)
	}
	draft, xerr := svc.GenerateDraft(context.Background(), org, user, acc.ID, &cand.ID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	// Approve MUST FAIL for unknown service / incomplete context
	if _, xerr := svc.ReviewDraft(context.Background(), org, user, draft.ID, "approve", nil); xerr == nil {
		t.Fatal("RED/incomplete/unknown approve MUST FAIL")
	} else if !strings.Contains(strings.ToLower(xerr.Message), "not structurally approvable") &&
		!strings.Contains(strings.ToLower(xerr.Message), "validation") &&
		!strings.Contains(strings.ToLower(xerr.Message), "unknown") &&
		!strings.Contains(strings.ToLower(xerr.Message), "incomplete") {
		t.Fatalf("unexpected approve error: %s", xerr.Message)
	}
}

func TestReviewDraftApproveSucceedsAfterCompleteRepair(t *testing.T) {
	rf := newMemRepoWithSettings()
	cfg := Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 160, RequireHumanApproval: true, MaxFeedPayloadBytes: DefaultMaxPayloadBytes}
	svc := NewService(cfg, rf, nil).(*service)

	org := uuid.New()
	user := uuid.New()
	acc := &models.OutreachAccount{
		ID: uuid.New(), OrganizationID: org,
		CNPJ14: "11222333000181", RazaoSocial: "Construtora Regional ACME Ltda", NomeFantasia: "ACME Obras",
		ServiceCode: "ADITIVOS", ServiceName: "Revisão de aditivos",
		MomentCode:        "CONTRACT_EXTENSION",
		MomentSummary:     "Prorrogacao do contrato 001/2025 publicada no PNCP em julho/2026",
		FactToMention:     "Prorrogacao do contrato 001/2025 publicada no PNCP em julho/2026 para obra de pavimentacao",
		MomentEvidenceIDs: []string{"ev-acme-1"},
		QuestionToAsk:     "Faz sentido conversarmos sobre o controle de aditivos desta obra?",
		CTA:               "Posso enviar um checklist de 1 pagina?",
		ClaimsToAvoid:     []string{"garantia de economia"},
		QueueState:        models.OutreachQueueNeedsReview,
		TargetFitSendTier: "TARGET_CONFIRMED",
		EmailSendReady:    true,
	}
	if _, err := rf.UpsertAccount(context.Background(), acc); err != nil {
		t.Fatal(err)
	}
	cand := &models.OutreachContactCandidate{
		ID: uuid.New(), OrganizationID: org, AccountID: acc.ID,
		Name: "Ana Silva", Role: "Engenheira de contratos", Email: "ana.silva@acme.com.br",
		VerificationStatus: models.OutreachVerifyOfficialSource, Recommended: true, Confidence: "HIGH",
	}
	applyValidatedIdentity(cand)
	if _, err := rf.UpsertCandidate(context.Background(), cand); err != nil {
		t.Fatal(err)
	}
	_, _ = rf.UpsertEvidence(context.Background(), &models.OutreachEvidence{
		ID: uuid.New(), OrganizationID: org, AccountID: acc.ID,
		SourceEvidenceID: "ev-acme-1", Title: "PNCP prorrogacao",
		Synthesis:      "Prorrogacao do contrato 001/2025 publicada no PNCP",
		EpistemicClass: models.OutreachEpistemicConfirmedFact,
	})

	draft, xerr := svc.GenerateDraft(context.Background(), org, user, acc.ID, &cand.ID)
	if xerr != nil {
		t.Fatal(xerr)
	}
	// Human edit with safe, specific body then approve.
	subj := "Sobre a prorrogacao do contrato 001/2025"
	body := "Ola Ana,\n\nVi a prorrogacao do contrato 001/2025 publicada no PNCP em julho/2026. Faz sentido conversarmos sobre o controle de aditivos desta obra?\n\nPosso enviar um checklist de 1 pagina?\n\nAbracos"
	if _, xerr = svc.ReviewDraft(context.Background(), org, user, draft.ID, "edit", &DraftEdit{Subject: &subj, BodyText: &body}); xerr != nil {
		t.Fatal(xerr)
	}
	approved, xerr := svc.ReviewDraft(context.Background(), org, user, draft.ID, "approve", nil)
	if xerr != nil {
		t.Fatalf("complete draft approve should succeed: %s", xerr.Message)
	}
	if approved.Status != models.OutreachDraftApproved {
		t.Fatalf("status=%s", approved.Status)
	}
}

func TestEnrollRejectsNonApprovedStructurally(t *testing.T) {
	rf := newMemRepoWithSettings()
	cfg := Config{Enabled: true, DefaultDailyLimit: 10, MaxInitialEmailWords: 120, RequireHumanApproval: true, MaxFeedPayloadBytes: DefaultMaxPayloadBytes}
	svc := NewService(cfg, rf, nil).(*service)
	svc.WireExecution(&mockCampaigns{}, &mockContacts{})

	org := uuid.New()
	user := uuid.New()
	acc := &models.OutreachAccount{
		ID: uuid.New(), OrganizationID: org, CNPJ14: "11222333000181",
		RazaoSocial: "X", ServiceCode: "ADITIVOS", FactToMention: "fato",
		QueueState: models.OutreachQueueNeedsReview,
	}
	_, _ = rf.UpsertAccount(context.Background(), acc)
	cand := &models.OutreachContactCandidate{
		ID: uuid.New(), OrganizationID: org, AccountID: acc.ID,
		Email: "a@b.com", VerificationStatus: models.OutreachVerifyOfficialSource, Recommended: true,
	}
	_, _ = rf.UpsertCandidate(context.Background(), cand)
	d := &models.OutreachDraft{
		ID: uuid.New(), OrganizationID: org, AccountID: acc.ID, ContactCandidateID: &cand.ID,
		Channel: models.OutreachChannelEmail, RecipientEmail: cand.Email,
		Subject: "S", BodyText: "B", Status: models.OutreachDraftNeedsReview,
		ServiceCode: "ADITIVOS", FactUsed: "fato", RiskClass: "YELLOW",
		RiskFlags: []string{"incomplete_copy_context"},
	}
	_ = rf.UpsertDraft(context.Background(), d)
	if _, xerr := svc.EnrollDraft(context.Background(), org, user, d.ID); xerr == nil {
		t.Fatal("enqueue/enroll must reject non-approved draft")
	}
}

func stringsSliceKeys(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if i := strings.Index(s, ":"); i > 0 {
			out = append(out, strings.TrimSpace(s[:i]))
		} else {
			out = append(out, s)
		}
	}
	return out
}

func TestStructuralApproveAllowsAAutomaticTier(t *testing.T) {
	pb := MustPlaybook()
	acc := testAccount("ADITIVOS", "CONTRACT_EXTENSION", "Prorrogacao do contrato 001/2025 publicada no PNCP em julho/2026 para obra de pavimentacao")
	acc.TargetFitSendTier = "A_AUTOMATIC"
	acc.MomentEvidenceIDs = []string{"ev-1"}
	cand := testCand("Engenheira de contratos")
	st := PlanOutreachStrategy(pb, acc, cand, nil, 1)
	// Surface-complete draft so only tier logic is under test.
	d := &models.OutreachDraft{
		ServiceCode: acc.ServiceCode,
		FactUsed:    acc.FactToMention,
		RiskClass:   "YELLOW",
		Subject:     "Sobre a prorrogacao do contrato 001/2025",
		BodyText: "Ola Ana,\n\nVi a prorrogacao do contrato 001/2025 publicada no PNCP em julho/2026. " +
			"Faz sentido conversarmos sobre o controle de aditivos desta obra?\n\n" +
			"Posso enviar um checklist de 1 pagina?\n\nAbracos",
	}
	blockers := StructuralApproveBlockers(acc, cand, &st, d, pb)
	for _, b := range blockers {
		if strings.Contains(b, "target_not_confirmed") {
			t.Fatalf("A_AUTOMATIC must not be target_not_confirmed: %v", blockers)
		}
	}
}

func TestStructuralApproveBlocksHollowHumanEdit(t *testing.T) {
	// Prod no-send case A: complete account strategy + hollow body/subject must fail closed.
	pb := MustPlaybook()
	acc := testAccount("ADITIVOS", "CONTRACT_EXTENSION", "Prorrogacao do contrato 001/2025 publicada no PNCP em julho/2026 para obra de pavimentacao")
	acc.TargetFitSendTier = "A_AUTOMATIC"
	acc.MomentEvidenceIDs = []string{"ev-1"}
	cand := testCand("Engenheira de contratos")
	st := PlanOutreachStrategy(pb, acc, cand, nil, 1)
	d := &models.OutreachDraft{
		ServiceCode: acc.ServiceCode,
		FactUsed:    acc.FactToMention,
		RiskClass:   "GREEN",
		RiskFlags:   []string{"low_send_risk"},
		Subject:     "x",
		BodyText:    "Oi",
		Channel:     models.OutreachChannelEmail,
	}
	blockers := StructuralApproveBlockers(acc, cand, &st, d, pb)
	if len(blockers) == 0 {
		t.Fatal("hollow subject/body must block approve even when account strategy is complete")
	}
	joined := strings.Join(blockers, " ")
	if !strings.Contains(joined, "incomplete_subject") &&
		!strings.Contains(joined, "incomplete_body") &&
		!strings.Contains(joined, "hollow_body") {
		t.Fatalf("expected incomplete/hollow surface blockers, got %v", blockers)
	}
}

func TestStructuralApproveDoesNotStickyPoisonAfterRepair(t *testing.T) {
	// After a hollow edit, draft.RiskFlags may still list incomplete_copy_context.
	// A repaired body must not stay blocked solely by sticky draft flags.
	pb := MustPlaybook()
	acc := testAccount("ADITIVOS", "CONTRACT_EXTENSION", "Prorrogacao do contrato 001/2025 publicada no PNCP em julho/2026 para obra de pavimentacao")
	acc.TargetFitSendTier = "A_AUTOMATIC"
	acc.MomentEvidenceIDs = []string{"ev-1"}
	cand := testCand("Engenheira de contratos")
	st := PlanOutreachStrategy(pb, acc, cand, nil, 1)
	d := &models.OutreachDraft{
		ServiceCode: acc.ServiceCode,
		FactUsed:    acc.FactToMention,
		RiskClass:   "YELLOW",
		RiskFlags:   []string{"low_send_risk", "incomplete_copy_context"},
		Subject:     "Sobre a prorrogacao do contrato 001/2025",
		BodyText: "Ola Ana,\n\nVi a prorrogacao do contrato 001/2025 publicada no PNCP em julho/2026. " +
			"Faz sentido conversarmos sobre o controle de aditivos desta obra?\n\n" +
			"Posso enviar um checklist de 1 pagina?\n\nAbracos",
		Channel: models.OutreachChannelEmail,
	}
	blockers := StructuralApproveBlockers(acc, cand, &st, d, pb)
	for _, b := range blockers {
		if strings.Contains(b, "incomplete_copy_context") {
			t.Fatalf("sticky draft incomplete_copy_context must not block repaired draft: %v", blockers)
		}
		if strings.Contains(b, "incomplete_body") || strings.Contains(b, "hollow_body") || strings.Contains(b, "incomplete_subject") {
			t.Fatalf("repaired surface must not be incomplete: %v", blockers)
		}
	}
}
