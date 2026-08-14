package confenge

import (
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func TestDoctrineQA_BadAnnualidadeFails(t *testing.T) {
	pb := MustPlaybook()
	acc := testAccount("REAJUSTE", "ANUALIDADE", "aniversário contratual 2024")
	st := PlanOutreachStrategy(pb, acc, testCand("Sócio"), []models.OutreachEvidence{{
		SourceEvidenceID: "ev-1", EpistemicClass: models.OutreachEpistemicConfirmedFact,
	}}, 1)
	bad := &DraftOutput{
		Channel: ChannelEmailInitial,
		Subject: "Oportunidade",
		BodyText: "Olá, identificamos que vocês têm reajuste a receber no contrato. " +
			"O órgão deixou de pagar o reajuste. Podemos marcar uma call de 30 minutos amanhã?",
		FactUsed: st.ObservedFact, EvidenceIDs: []string{"ev-1"}, ServiceCode: "REAJUSTE",
		CTA: "Podemos marcar uma call?",
	}
	dqa := ValidateDoctrineCopy(bad, &st, pb, ChannelEmailInitial)
	if dqa.OK {
		t.Fatalf("expected fail: %+v", dqa)
	}
	joined := strings.Join(dqa.Errors, " ")
	if !strings.Contains(joined, "annualidade") && !strings.Contains(joined, "meeting") && !strings.Contains(joined, "reajuste") {
		t.Fatalf("errors should cite annualidade/meeting/claim: %v", dqa.Errors)
	}
}

func TestDoctrineQA_GoodVerifyPasses(t *testing.T) {
	pb := MustPlaybook()
	acc := testAccount("REAJUSTE", "ANUALIDADE", "contrato 1149 atingiu aniversário de reajuste")
	cand := testCand("Sócio")
	ev := []models.OutreachEvidence{{SourceEvidenceID: "ev-1", EpistemicClass: models.OutreachEpistemicConfirmedFact}}
	st := PlanOutreachStrategy(pb, acc, cand, ev, 1)
	good := ComposeFromStrategy(st, acc, cand, ChannelEmailInitial)
	// Ensure body long enough for contact enroll path later
	val := ValidateDraft(&good, acc, cand, ValidateOpts{
		MaxWords: 200, Evidence: ev, Channel: ChannelEmailInitial, Strategy: &st, Playbook: pb,
	})
	// Compose may still warn on short email; hard errors on annualidade claim must be empty
	for _, e := range val.Errors {
		if strings.Contains(e, "annualidade") || strings.Contains(e, "reajuste a receber") {
			t.Fatalf("good path annualidade error: %s body=%s", e, good.BodyText)
		}
		if strings.Contains(e, "meeting CTA") {
			t.Fatalf("good path meeting: %s", e)
		}
	}
	dqa := ValidateDoctrineCopy(&good, &st, pb, ChannelEmailInitial)
	for _, e := range dqa.Errors {
		if strings.Contains(e, "annualidade") {
			t.Fatal(e)
		}
	}
}

func TestDoctrineQA_EmptyFollowUpFails(t *testing.T) {
	pb := MustPlaybook()
	st := PlanOutreachStrategy(pb, testAccount("REAJUSTE", "PORTFOLIO", "fato X"), testCand(""), nil, 2)
	out := &DraftOutput{
		Subject: "Re: X", BodyText: "Olá, só acompanhando e subindo no seu inbox. Teve chance de ver?",
		FactUsed: "fato X", EvidenceIDs: []string{"ev-1"},
	}
	dqa := ValidateDoctrineCopy(out, &st, pb, ChannelEmailFollowup)
	if dqa.OK {
		t.Fatal("empty follow-up must fail")
	}
}

func TestDoctrineQA_FakeReFirstTouch(t *testing.T) {
	pb := MustPlaybook()
	st := PlanOutreachStrategy(pb, testAccount("ADITIVOS", "ADITIVO", "aditivo 2"), testCand("Engenheiro"), nil, 1)
	out := &DraftOutput{
		Subject: "Re: aditivo", BodyText: strings.Repeat("ponto relevante sobre o aditivo publicado. ", 8) + "Posso te mandar os pontos?",
		FactUsed: "aditivo 2", EvidenceIDs: []string{"ev-1"},
	}
	dqa := ValidateDoctrineCopy(out, &st, pb, ChannelEmailInitial)
	if dqa.OK {
		t.Fatal("fake Re must fail on first touch")
	}
}

func TestContrastBadVsGood(t *testing.T) {
	pb := MustPlaybook()
	acc := testAccount("REAJUSTE", "ANUALIDADE", "contrato DER-PR 88/2021 aniversário de reajuste")
	cand := testCand("Diretor de Contratos")
	ev := []models.OutreachEvidence{{SourceEvidenceID: "ev-1", Title: "PNCP", EpistemicClass: models.OutreachEpistemicConfirmedFact}}
	st := PlanOutreachStrategy(pb, acc, cand, ev, 1)

	bad := DraftOutput{
		Subject: "PARCERIA IMPERDÍVEL",
		BodyText: "A CONFENGE é especializada em contratos públicos e pode ajudar sua empresa a recuperar valores. " +
			"Gostaria de agendar 30 minutos? Somos líderes de mercado.",
		FactUsed: "x", EvidenceIDs: []string{"ev-1"},
	}
	good := ComposeFromStrategy(st, acc, cand, ChannelEmailInitial)

	badQA := ValidateDoctrineCopy(&bad, &st, pb, ChannelEmailInitial)
	goodQA := ValidateDoctrineCopy(&good, &st, pb, ChannelEmailInitial)
	if badQA.OK {
		t.Fatalf("BAD should fail: %v", badQA)
	}
	// GOOD may have soft warnings but must not claim unpaid reajuste
	for _, e := range goodQA.Errors {
		if strings.Contains(e, "annualidade") || strings.Contains(e, "meeting CTA") {
			t.Fatalf("GOOD hard fail: %s\n%s", e, good.BodyText)
		}
	}
}

func TestAntiTemplateDiversity(t *testing.T) {
	pb := MustPlaybook()
	bodies := map[string]bool{}
	fixtures := []struct {
		svc, moment, fact, role string
		pos                     int
	}{
		{"REAJUSTE", "ANUALIDADE", "contrato 1149 aniversário reajuste", "Sócio", 1},
		{"ADITIVOS", "ADITIVO_RECENTE", "termo aditivo 3 ao contrato 220/2023", "Engenheiro", 1},
		{"MEDICOES", "MEDICAO", "medição do lote 2 publicada", "Analista Financeiro", 1},
		{"ENCERRAMENTO_CONTRATUAL", "ENCERRAMENTO", "vigência encerra em 90 dias", "Diretor", 1},
		{"REAJUSTE", "ANUALIDADE", "contrato 1149 aniversário reajuste", "Sócio", 5},
	}
	for _, f := range fixtures {
		acc := testAccount(f.svc, f.moment, f.fact)
		st := PlanOutreachStrategy(pb, acc, testCand(f.role), nil, f.pos)
		out := ComposeFromStrategy(st, acc, testCand(f.role), ChannelEmailInitial)
		// Normalize names
		key := strings.TrimSpace(out.BodyText)
		bodies[key] = true
	}
	if len(bodies) < 4 {
		t.Fatalf("expected diverse bodies, got %d unique", len(bodies))
	}
}
