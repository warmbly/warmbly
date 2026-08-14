package confenge

import (
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func baseCand() *models.OutreachContactCandidate {
	return &models.OutreachContactCandidate{Email: "a@example.com", VerificationStatus: models.OutreachVerifyOfficialSource}
}

func TestValidateDraftRejectsEmDash(t *testing.T) {
	acc := &models.OutreachAccount{ServiceCode: "ADDITIVE_REVIEW", FactToMention: "contrato prorrogado", MomentEvidenceIDs: []string{"e1"}}
	out := &DraftOutput{Subject: "Oi sobre contrato", BodyText: "Texto com travessão — aqui", FactUsed: "contrato prorrogado", ServiceCode: "ADDITIVE_REVIEW", EvidenceIDs: []string{"e1"}, Claims: []DraftClaim{{Phrase: "contrato prorrogado", EvidenceIDs: []string{"e1"}}}}
	if ValidateDraft(out, acc, baseCand(), ValidateOpts{MaxWords: 120, Evidence: []models.OutreachEvidence{{SourceEvidenceID: "e1"}}}).OK {
		t.Fatal("em dash must fail")
	}
}

func TestValidateDraftRejectsBannedPhrase(t *testing.T) {
	acc := &models.OutreachAccount{ServiceCode: "X", FactToMention: "fato", MomentEvidenceIDs: []string{"e1"}}
	out := &DraftOutput{Subject: "Sobre o fato", BodyText: "Identificamos dinheiro a receber na conta.", FactUsed: "fato", ServiceCode: "X", EvidenceIDs: []string{"e1"}, Claims: []DraftClaim{{Phrase: "fato", EvidenceIDs: []string{"e1"}}}}
	if ValidateDraft(out, acc, baseCand(), ValidateOpts{MaxWords: 120, Evidence: []models.OutreachEvidence{{SourceEvidenceID: "e1"}}}).OK {
		t.Fatal("banned phrase must fail")
	}
}

func TestValidateDraftRejectsUnverified(t *testing.T) {
	acc := &models.OutreachAccount{ServiceCode: "X", FactToMention: "fato publico", MomentEvidenceIDs: []string{"e1"}}
	cand := &models.OutreachContactCandidate{Email: "a@example.com", VerificationStatus: models.OutreachVerifyCandidateUnverified}
	out := &DraftOutput{Subject: "Sobre fato publico", BodyText: "Mensagem curta com fato publico. Faz sentido?", FactUsed: "fato publico", ServiceCode: "X", EvidenceIDs: []string{"e1"}, Claims: []DraftClaim{{Phrase: "fato publico", EvidenceIDs: []string{"e1"}}}}
	if ValidateDraft(out, acc, cand, ValidateOpts{MaxWords: 120, Evidence: []models.OutreachEvidence{{SourceEvidenceID: "e1"}}}).OK {
		t.Fatal("unverified must not enroll")
	}
}

func TestValidateDraftAcceptsClean(t *testing.T) {
	acc := &models.OutreachAccount{ServiceCode: "ADDITIVE_REVIEW", FactToMention: "prorrogacao do contrato 001", MomentEvidenceIDs: []string{"e1"}}
	out := &DraftOutput{Subject: "Sobre a prorrogacao do contrato", BodyText: "Ola Ana,\n\nNotei a prorrogacao do contrato 001. Faz sentido conversarmos sobre aditivos?\n\nPosso enviar um checklist?", FactUsed: "prorrogacao do contrato 001", ServiceCode: "ADDITIVE_REVIEW", EvidenceIDs: []string{"e1"}, Claims: []DraftClaim{{Phrase: "prorrogacao do contrato 001", Fact: "prorrogacao do contrato 001", EvidenceIDs: []string{"e1"}}}, Question: "Faz sentido?", CTA: "Posso enviar?", Channel: ChannelEmailInitial}
	res := ValidateDraft(out, acc, baseCand(), ValidateOpts{MaxWords: 120, Evidence: []models.OutreachEvidence{{SourceEvidenceID: "e1"}}, Channel: ChannelEmailInitial})
	if !res.OK {
		t.Fatalf("expected ok, got %v", res.Errors)
	}
}

func TestValidateDraftRejectsUnknownEvidenceID(t *testing.T) {
	acc := &models.OutreachAccount{ServiceCode: "X", FactToMention: "fato", MomentEvidenceIDs: []string{"e1"}}
	out := &DraftOutput{Subject: "Sobre fato", BodyText: "Notei o fato no portal. Faz sentido?", FactUsed: "fato", ServiceCode: "X", EvidenceIDs: []string{"does-not-exist"}, Claims: []DraftClaim{{Phrase: "fato", EvidenceIDs: []string{"does-not-exist"}}}}
	res := ValidateDraft(out, acc, baseCand(), ValidateOpts{MaxWords: 120, Evidence: []models.OutreachEvidence{{SourceEvidenceID: "e1"}}})
	if res.OK {
		t.Fatal("unknown evidence id must fail")
	}
	found := false
	for _, e := range res.Errors {
		if strings.Contains(e, "unknown evidence_id") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want unknown evidence_id, got %v", res.Errors)
	}
}

func TestValidateDraftRejectsServiceMismatchWithoutOverride(t *testing.T) {
	acc := &models.OutreachAccount{ServiceCode: "ADDITIVE_REVIEW", FactToMention: "contrato 1", MomentEvidenceIDs: []string{"e1"}}
	out := &DraftOutput{Subject: "Sobre contrato", BodyText: "Notei o contrato 1. Faz sentido?", FactUsed: "contrato 1", ServiceCode: "OTHER_SERVICE", EvidenceIDs: []string{"e1"}, Claims: []DraftClaim{{Phrase: "contrato 1", EvidenceIDs: []string{"e1"}}}}
	if ValidateDraft(out, acc, baseCand(), ValidateOpts{MaxWords: 120, Evidence: []models.OutreachEvidence{{SourceEvidenceID: "e1"}}}).OK {
		t.Fatal("service mismatch must fail")
	}
	if !ValidateDraft(out, acc, baseCand(), ValidateOpts{MaxWords: 120, Evidence: []models.OutreachEvidence{{SourceEvidenceID: "e1"}}, ServiceOverrideAudited: true}).OK {
		t.Fatal("audited override should pass")
	}
}

func TestValidateDraftRejectsHypothesisAsFact(t *testing.T) {
	acc := &models.OutreachAccount{ServiceCode: "DIAG", FactToMention: "portal sem organograma", MomentEvidenceIDs: []string{"h1"}}
	out := &DraftOutput{Subject: "Sobre a equipe", BodyText: "Sei que vocês não têm equipe de contratos. Faz sentido?", FactUsed: "portal sem organograma", ServiceCode: "DIAG", EvidenceIDs: []string{"h1"}, Claims: []DraftClaim{{Phrase: "vocês não têm equipe", Fact: "nao tem equipe", EvidenceIDs: []string{"h1"}}}}
	res := ValidateDraft(out, acc, baseCand(), ValidateOpts{MaxWords: 120, Evidence: []models.OutreachEvidence{{SourceEvidenceID: "h1", EpistemicClass: models.OutreachEpistemicCommercialHypothesis}}})
	if res.OK {
		t.Fatalf("hypothesis as fact must fail: %v", res.Errors)
	}
}

func TestClassifyRiskRedForCredit(t *testing.T) {
	class, _ := ClassifyRisk(&models.OutreachAccount{MomentCode: "CREDIT_CLAIM", FactToMention: "x"}, baseCand(), &DraftOutput{BodyText: "ola"}, ValidationResult{OK: true})
	if class != "RED" {
		t.Fatalf("want RED got %s", class)
	}
}

func TestClassifyRiskGreenOfficial(t *testing.T) {
	acc := &models.OutreachAccount{MomentCode: "WARM_INTRO", FactToMention: "publicacao no portal", ServiceCode: "DIAGNOSTIC"}
	cand := &models.OutreachContactCandidate{Email: "a@example.com", VerificationStatus: models.OutreachVerifyOfficialSource, Role: "Analista"}
	class, _ := ClassifyRisk(acc, cand, &DraftOutput{BodyText: "ola", Subject: "oi"}, ValidationResult{OK: true})
	if class != "GREEN" {
		t.Fatalf("want GREEN got %s", class)
	}
}

func TestTemplateDraftNoEmDash(t *testing.T) {
	acc := &models.OutreachAccount{RazaoSocial: "ACME", FactToMention: "termo aditivo 1 ao contrato X publicado", ServiceName: "revisao", QuestionToAsk: "Faz sentido?", CTA: "Posso enviar?", ServiceCode: "ADITIVOS", MomentEvidenceIDs: []string{"e1"}}
	cand := &models.OutreachContactCandidate{Name: "Ana Silva", Email: "a@example.com", VerificationStatus: models.OutreachVerifyOfficialSource}
	out := TemplateDraftChannel(ChannelEmailInitial, acc, cand, []models.OutreachEvidence{{SourceEvidenceID: "e1"}})
	if emDashRe.MatchString(out.BodyText+out.Subject) || out.BodyText == "" {
		t.Fatal("template invalid")
	}
}

func TestParseDraftJSONStripsFence(t *testing.T) {
	raw := "```json\n{\"subject\":\"Oi sobre tema\",\"body_text\":\"Corpo\",\"fact_used\":\"f\",\"service_code\":\"S\",\"evidence_ids\":[\"1\"],\"claims\":[{\"phrase\":\"f\",\"evidence_ids\":[\"1\"]}],\"followups\":[],\"question\":\"?\",\"cta\":\"c\",\"risk_flags\":[],\"rationale\":\"r\"}\n```"
	out, err := parseDraftJSON(raw)
	if err != nil || out.Subject != "Oi sobre tema" {
		t.Fatalf("parse failed: %v %q", err, out.Subject)
	}
}

func TestLintCopyRejectsAntiTemplate(t *testing.T) {
	if LintCopy(ChannelEmailInitial, "Parceria", "Identificamos uma oportunidade de sinergia entre nossas empresas.", "ACME").OK {
		t.Fatal("anti-template must fail")
	}
}

func TestNearDuplicateJaccard(t *testing.T) {
	a := "Sou da CONFENGE. Notei a prorrogacao do contrato 001 no PNCP. Faz sentido conversarmos?"
	if _, hit := NearDuplicate(a, []string{a}); !hit {
		t.Fatal("identical bodies must near-dup")
	}
}
