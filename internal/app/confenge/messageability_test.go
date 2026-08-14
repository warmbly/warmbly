package confenge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

func encopavAccount() *models.OutreachAccount {
	return &models.OutreachAccount{
		RazaoSocial:       "ENCOPAV Engenharia de Pavimentacao Ltda",
		NomeFantasia:      "ENCOPAV",
		CNPJ14:            "11222333000181",
		UF:                "RS",
		ServiceCode:       "MONITORAMENTO_CONTRATUAL",
		ServiceName:       "Monitoramento contratual",
		MomentCode:        "PORTFOLIO",
		MomentSummary:     "Contratação pública de pavimentação publicada",
		FactToMention:     "objeto: Contratação de empresa, através de empreitada global (...) ; órgão: DER-RS; UF RS; R$ 2,839,000",
		MomentEvidenceIDs: []string{"ev-encopav-1"},
	}
}

func encopavEvidence() []models.OutreachEvidence {
	return []models.OutreachEvidence{{
		SourceEvidenceID: "ev-encopav-1",
		Title:            "Contrato pavimentação RS",
		Synthesis:        "objeto: Contratação de empresa, através de empreitada global (...) ; órgão: DER-RS; UF RS; R$ 2,839,000",
		Excerpt:          "objeto: Contratação de empresa; órgão: DER-RS; UF: RS; valor: R$ 2,839,000",
		EpistemicClass:   models.OutreachEpistemicConfirmedFact,
	}}
}

func leakPatterns() []string {
	return []string{
		"isso não prova crédito sozinho",
		"isso nao prova credito sozinho",
		"eventos públicos relevantes sem triagem",
		"eventos publicos relevantes sem triagem",
		"objeto:",
		"órgão:",
		"orgao:",
		"UF:",
		"sem presumir falta de capacidade interna",
	}
}

func assertNoLeaks(t *testing.T, body string) {
	t.Helper()
	low := strings.ToLower(body)
	for _, p := range leakPatterns() {
		if p != "" && strings.Contains(low, strings.ToLower(p)) {
			t.Fatalf("leak/metadata pattern %q in body:\n%s", p, body)
		}
	}
}

func TestEncopavSemanticRegressionNeedsEnrichment(t *testing.T) {
	pb := MustPlaybook()
	acc := encopavAccount()
	cand := testCand("Diretor de Contratos")
	ev := encopavEvidence()
	_, plan := BuildOutboundPlan(pb, acc, cand, ev, 1)
	if plan.Messageability != MessageabilityNeedsEnrichment {
		t.Fatalf("ENCOPAV must be NEEDS_ENRICHMENT, got %s codes=%v reason=%q", plan.Messageability, plan.ReasonCodes, plan.Reason)
	}
	if !containsStr(plan.ReasonCodes, "missing_contract_event") && !containsStr(plan.ReasonCodes, "metadata_dump") {
		t.Fatalf("expected missing_contract_event or metadata_dump, got %v", plan.ReasonCodes)
	}
	if plan.Hook != "" || plan.CTA != "" {
		t.Fatalf("non-READY plan must not carry sendable fields: %+v", plan)
	}
	if !strings.Contains(strings.ToLower(plan.Reason), "evento contratual") && !strings.Contains(strings.ToLower(plan.Reason), "metadado") {
		t.Fatalf("operator reason should be readable, got %q", plan.Reason)
	}

	out := ComposeFromPlan(plan, acc, cand, ChannelEmailInitial)
	if out.BodyText != "" {
		t.Fatalf("must not fabricate sendable copy: %q", out.BodyText)
	}
	assertNoLeaks(t, out.BodyText+" "+out.Subject)
	// Strategy may still hold internal hypothesis; renderer must not interpolate it.
	if strings.Contains(strings.ToLower(out.BodyText), "crédito") || strings.Contains(strings.ToLower(out.BodyText), "credito") {
		t.Fatalf("crédito leaked into copy: %s", out.BodyText)
	}

	ai := &AIDraftGenerator{} // nil provider; Generate still gates first
	genOut, _, model, err := TemplateGenerator{}.Generate(context.Background(), BuildGenerateInput(ChannelEmailInitial, acc, cand, ev, nil))
	if err != nil {
		t.Fatal(err)
	}
	if model != "messageability_gate" {
		t.Fatalf("template path must fail closed at the gate, model=%s", model)
	}
	if genOut.BodyText != "" {
		t.Fatalf("template fabricated body: %s", genOut.BodyText)
	}
	_ = ai
	assertNoLeaks(t, genOut.BodyText)
}

func TestEncopavDoesNotEmitOldText(t *testing.T) {
	// Drive the same compose path that produced the production junk.
	pb := MustPlaybook()
	acc := encopavAccount()
	cand := testCand("Diretor")
	st := PlanOutreachStrategy(pb, acc, cand, encopavEvidence(), 1)
	out := ComposeFromStrategy(st, acc, cand, ChannelEmailInitial)
	old := []string{
		"Isso não prova crédito sozinho",
		"eventos públicos relevantes sem triagem",
		"Pelo que está público, objeto:",
		"Como segunda leitura pontual",
	}
	blob := out.BodyText + " " + out.Subject
	for _, p := range old {
		if strings.Contains(blob, p) {
			t.Fatalf("old production text still emitted: %q\n%s", p, out.BodyText)
		}
	}
	if out.BodyText != "" {
		t.Fatalf("ENCOPAV compose must be empty, got %q", out.BodyText)
	}
}

func TestMessageabilityNotCommercialScore(t *testing.T) {
	pb := MustPlaybook()
	acc := encopavAccount()
	st, plan := BuildOutboundPlan(pb, acc, testCand("Sócio"), encopavEvidence(), 1)
	raw, _ := json.Marshal(st)
	for _, banned := range []string{"lead_score", "priority_score", "commercial_score", "conversion_score"} {
		if strings.Contains(string(raw), banned) {
			t.Fatalf("strategy must not emit %s", banned)
		}
	}
	if plan.Messageability == MessageabilityReady {
		t.Fatal("target-fit-like public fact must not be READY")
	}
}

func TestGoldenServiceMatrix(t *testing.T) {
	pb := MustPlaybook()
	services := []string{
		"REAJUSTE", "REEQUILIBRIO", "MEDICOES", "ADITIVOS", "EXTRACONTRATUAIS",
		"PLANILHAS", "ENCERRAMENTO_CONTRATUAL", "APOIO_LICITACAO",
		"MONITORAMENTO_CONTRATUAL", "DIAGNOSTICO", "INTELIGENCIA_PNCP", "BACKOFFICE",
	}
	strongFact := map[string]string{
		"REAJUSTE":                 "contrato 1149/2022 atingiu aniversário de reajuste em 2024",
		"REEQUILIBRIO":             "aditivo publicado aponta possível desequilíbrio a documentar",
		"MEDICOES":                 "medição do lote 3 publicada no DER-PR",
		"ADITIVOS":                 "termo aditivo 2 ao contrato 88/2021 publicado",
		"EXTRACONTRATUAIS":         "ordem de serviço de extra publicada no contrato 12",
		"PLANILHAS":                "quantitativos do trecho sul publicados após medição",
		"ENCERRAMENTO_CONTRATUAL":  "vigência encerra em 60 dias no contrato 220/2023",
		"APOIO_LICITACAO":          "edital 45/2026 publicado com quantitativos a conferir",
		"MONITORAMENTO_CONTRATUAL": "aditivo 1 ao contrato de pavimentação publicado no PNCP",
		"DIAGNOSTICO":              "prorrogação do contrato 001/2025 publicada no PNCP",
		"INTELIGENCIA_PNCP":        "novos órgãos em SC no recorte PNCP do último trimestre",
		"BACKOFFICE":               "pico de medições publicadas no último trimestre no contrato 9",
	}
	weakFact := "empresa possui contrato público"
	metaFact := "objeto: Contratação de empresa; órgão: Prefeitura; UF: RS; valor: R$ 2,839,000"

	for _, svc := range services {
		t.Run(svc, func(t *testing.T) {
			// named + strong
			acc := testAccount(svc, "EVENT", strongFact[svc])
			named := testCand("Engenheira de contratos")
			ev := []models.OutreachEvidence{{SourceEvidenceID: "ev-1", EpistemicClass: models.OutreachEpistemicConfirmedFact, Synthesis: strongFact[svc]}}
			_, plan := BuildOutboundPlan(pb, acc, named, ev, 1)
			out := ComposeFromPlan(plan, acc, named, ChannelEmailInitial)
			if plan.Messageability != MessageabilityReady {
				t.Fatalf("strong named: want READY got %s %v %q", plan.Messageability, plan.ReasonCodes, plan.Reason)
			}
			if out.BodyText == "" {
				t.Fatal("READY must produce copy")
			}
			assertNoLeaks(t, out.BodyText)
			assertServiceCoherentCopy(t, svc, out.BodyText, plan)
			if !CreditVocabAllowed(pb, svc) && creditWordIn(out.BodyText) {
				t.Fatalf("crédito in non-authorized service copy: %s", out.BodyText)
			}
			stReady := PlanOutreachStrategy(pb, acc, named, ev, 1)
			dqa := ValidateDoctrineCopy(&out, &stReady, pb, ChannelEmailInitial)
			for _, e := range dqa.Errors {
				if strings.Contains(e, "reasoning") || strings.Contains(e, "metadata") || strings.Contains(e, "crédito") || strings.Contains(e, "contract-framed") {
					t.Fatalf("READY copy failed hard QA: %s\n%s", e, out.BodyText)
				}
			}

			// generic inbox + strong
			gen := testCand("Diretora")
			gen.Name = "Pessoa Histórica"
			gen.Email = "contato@exemplo.com.br"
			gen.MailboxPurpose = "GENERIC_CONTACT"
			_, gplan := BuildOutboundPlan(pb, acc, gen, ev, 1)
			gout := ComposeFromPlan(gplan, acc, gen, ChannelEmailInitial)
			if gplan.RecipientMode != RecipientModeGenericInbox {
				t.Fatalf("generic mode=%s", gplan.RecipientMode)
			}
			if gplan.Messageability != MessageabilityReady {
				t.Fatalf("generic strong: want READY got %s %v", gplan.Messageability, gplan.ReasonCodes)
			}
			if !strings.HasPrefix(gout.BodyText, "Olá, equipe") || strings.Contains(gout.BodyText, gen.Name) {
				t.Fatalf("generic inbox personalized: %q", gout.BodyText)
			}
			assertNoLeaks(t, gout.BodyText)

			// weak evidence
			wacc := testAccount(svc, "PORTFOLIO", weakFact)
			_, wplan := BuildOutboundPlan(pb, wacc, named, nil, 1)
			wout := ComposeFromPlan(wplan, wacc, named, ChannelEmailInitial)
			if wplan.Messageability == MessageabilityReady {
				t.Fatalf("weak evidence must not be READY: hook=%q", wplan.Hook)
			}
			if wout.BodyText != "" {
				t.Fatalf("weak evidence fabricated copy: %s", wout.BodyText)
			}

			// metadata-like fact
			macc := testAccount(svc, "PORTFOLIO", metaFact)
			_, mplan := BuildOutboundPlan(pb, macc, named, []models.OutreachEvidence{{
				SourceEvidenceID: "ev-1", EpistemicClass: models.OutreachEpistemicConfirmedFact, Synthesis: metaFact,
			}}, 1)
			mout := ComposeFromPlan(mplan, macc, named, ChannelEmailInitial)
			if svc == "MONITORAMENTO_CONTRATUAL" && mplan.Messageability == MessageabilityReady {
				t.Fatal("metadata contract dump must not READY monitoramento")
			}
			if mout.BodyText != "" {
				assertNoLeaks(t, mout.BodyText)
				if looksLikeMetadataDump(mout.BodyText) {
					t.Fatalf("metadata dump reached copy: %s", mout.BodyText)
				}
			}

			// mismatched vocabulary on a forced body
			bad := &DraftOutput{Subject: "X", BodyText: "Olá, isso não prova crédito sozinho, mas " + metaFact, ServiceCode: svc}
			stBad := PlanOutreachStrategy(pb, acc, named, ev, 1)
			qa := ValidateDoctrineCopy(bad, &stBad, pb, ChannelEmailInitial)
			if qa.OK {
				t.Fatalf("mismatched leak body must fail QA for %s: %+v", svc, qa)
			}

			// missing value unit / unsupported offer
			if wplan.Messageability != MessageabilityNeedsEnrichment && wplan.Messageability != MessageabilityBlocked {
				t.Fatalf("weak must be enrichment or blocked, got %s", wplan.Messageability)
			}
		})
	}
}

func TestHardCommercialQA(t *testing.T) {
	pb := MustPlaybook()
	acc := testAccount("REAJUSTE", "ANUALIDADE", "contrato 1149/2022 aniversário de reajuste")
	cand := testCand("Sócio")
	ev := []models.OutreachEvidence{{SourceEvidenceID: "ev-1", EpistemicClass: models.OutreachEpistemicConfirmedFact, Synthesis: acc.FactToMention}}
	st := PlanOutreachStrategy(pb, acc, cand, ev, 1)

	cases := []struct {
		name string
		body string
		want string
	}{
		{"metadata", "Olá,\n\nobjeto: Contratação; órgão: DER; UF: RS; R$ 2,839,000.\n\nPosso te mandar os pontos?", "metadata"},
		{"leak", "Olá,\n\nIsso não prova crédito sozinho, mas eventos públicos relevantes sem triagem.\n\nPosso te mandar os pontos?", "reasoning"},
		{"empty_value", "Olá,\n\nobjeto: x; órgão: y; UF: z.\n\nPosso te mandar os pontos que eu conferiria?", "metadata"},
		{"vocab", "Olá,\n\nPelo contrato publicado, o edital 45 saiu.\n\nIsso não prova crédito sozinho.", "crédito"},
		{"disclaimer", "Olá,\n\nAniversário contratual.\n\nIsso não prova crédito sozinho, mas vale conferir.", "disclaimer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := &DraftOutput{Subject: "Contrato", BodyText: tc.body, ServiceCode: "MONITORAMENTO_CONTRATUAL", CTA: "Posso te mandar os pontos?"}
			stMon := st
			stMon.ServiceCode = "MONITORAMENTO_CONTRATUAL"
			dqa := ValidateDoctrineCopy(out, &stMon, pb, ChannelEmailInitial)
			if dqa.OK {
				t.Fatalf("expected fail (%s): %+v", tc.want, dqa)
			}
		})
	}

	good := ComposeFromPlan(EvaluateMessageability(st, acc, cand, ev, pb), acc, cand, ChannelEmailInitial)
	if good.BodyText == "" {
		t.Fatal("expected READY compose for strong reajuste")
	}
	dqa := ValidateDoctrineCopy(&good, &st, pb, ChannelEmailInitial)
	for _, e := range dqa.Errors {
		if strings.Contains(e, "reasoning") || strings.Contains(e, "metadata") || strings.Contains(e, "crédito") {
			t.Fatalf("good READY failed hard QA: %s\n%s", e, good.BodyText)
		}
	}
}

func TestCreditVocabBlockedForNonCreditServices(t *testing.T) {
	pb := MustPlaybook()
	for _, svc := range []string{"MONITORAMENTO_CONTRATUAL", "APOIO_LICITACAO", "INTELIGENCIA_PNCP"} {
		if CreditVocabAllowed(pb, svc) {
			t.Fatalf("%s must not allow crédito vocab", svc)
		}
		body := "Olá,\n\nPelo contrato publicado, há um edital novo.\n\nIsso não prova crédito sozinho, mas vale olhar."
		out := &DraftOutput{Subject: "Edital", BodyText: body, ServiceCode: svc}
		acc := testAccount(svc, "EVENT", "edital 45/2026 publicado")
		st := PlanOutreachStrategy(pb, acc, testCand("Sócio"), nil, 1)
		dqa := ValidateDoctrineCopy(out, &st, pb, ChannelEmailInitial)
		if dqa.OK {
			t.Fatalf("%s must fail on crédito: %+v", svc, dqa)
		}
	}
}

func TestLooksLikeMetadataDump(t *testing.T) {
	dump := "objeto: Contratação de empresa; órgão: DER-RS; UF: RS; R$ 2,839,000"
	if !looksLikeMetadataDump(dump) {
		t.Fatal("expected dump")
	}
	if !looksMalformedCurrency("contrato de R$ 2,839,000") {
		t.Fatal("US-style thousands should be malformed")
	}
	natural := "contrato 1149/2022 atingiu aniversário de reajuste em 2024"
	if looksLikeMetadataDump(natural) {
		t.Fatal("natural fact is not a dump")
	}
}

func TestReajusteEditalIsNotReady(t *testing.T) {
	pb := MustPlaybook()
	// extra-cli ANUALIDADE must not make an edital fact service-coherent.
	acc := testAccount("REAJUSTE", "ANUALIDADE", "edital 45/2026 publicado com quantitativos a conferir")
	cand := testCand("Sócio")
	ev := []models.OutreachEvidence{{SourceEvidenceID: "ev-1", EpistemicClass: models.OutreachEpistemicConfirmedFact, Synthesis: acc.FactToMention}}
	_, plan := BuildOutboundPlan(pb, acc, cand, ev, 1)
	if plan.Messageability == MessageabilityReady {
		t.Fatalf("REAJUSTE + ANUALIDADE moment + edital fact must not be READY: %+v", plan)
	}
	if !containsStr(plan.ReasonCodes, "hook_service_mismatch") {
		t.Fatalf("expected hook_service_mismatch, got %v", plan.ReasonCodes)
	}
	out := ComposeFromPlan(plan, acc, cand, ChannelEmailInitial)
	if out.BodyText != "" {
		t.Fatalf("fabricated: %s", out.BodyText)
	}
}

func TestMonitoramentoAditivoMomentNeedsEventInFact(t *testing.T) {
	pb := MustPlaybook()
	// extra-cli ADITIVO must not READY a mere published-contract dump.
	acc := testAccount("MONITORAMENTO_CONTRATUAL", "ADITIVO",
		"objeto: Contratação de empresa, através de empreitada global (...) ; órgão: DER-RS; UF RS; R$ 2,839,000")
	cand := testCand("Diretor de Contratos")
	ev := []models.OutreachEvidence{{
		SourceEvidenceID: "ev-1",
		EpistemicClass:   models.OutreachEpistemicConfirmedFact,
		Title:            "Contrato pavimentação RS",
		Synthesis:        acc.FactToMention,
	}}
	_, plan := BuildOutboundPlan(pb, acc, cand, ev, 1)
	if plan.Messageability == MessageabilityReady {
		t.Fatalf("MONITORAMENTO + ADITIVO moment + no aditivo in fact must not be READY: %+v", plan)
	}
	if !containsStr(plan.ReasonCodes, "missing_contract_event") && !containsStr(plan.ReasonCodes, "metadata_dump") {
		t.Fatalf("expected missing_contract_event, got %v", plan.ReasonCodes)
	}
	out := ComposeFromPlan(plan, acc, cand, ChannelEmailInitial)
	if out.BodyText != "" {
		t.Fatalf("fabricated: %s", out.BodyText)
	}
	assertNoLeaks(t, out.BodyText+" "+out.Subject)
}

func TestLicitationAndPNCPOpenersAreNotContractFramed(t *testing.T) {
	pb := MustPlaybook()
	cases := []struct {
		svc, fact, forbid, want string
	}{
		{"APOIO_LICITACAO", "edital 45/2026 publicado com quantitativos a conferir", ContractFramedOpener, "pelo edital publicado"},
		{"INTELIGENCIA_PNCP", "novos órgãos em SC no recorte PNCP do último trimestre", ContractFramedOpener, "pelo recorte publico do pncp"},
	}
	for _, tc := range cases {
		acc := testAccount(tc.svc, "EVENT", tc.fact)
		cand := testCand("Diretor")
		ev := []models.OutreachEvidence{{SourceEvidenceID: "ev-1", EpistemicClass: models.OutreachEpistemicConfirmedFact, Synthesis: tc.fact}}
		_, plan := BuildOutboundPlan(pb, acc, cand, ev, 1)
		out := ComposeFromPlan(plan, acc, cand, ChannelEmailInitial)
		if plan.Messageability != MessageabilityReady {
			t.Fatalf("%s: want READY got %s %v", tc.svc, plan.Messageability, plan.ReasonCodes)
		}
		low := foldASCII(out.BodyText)
		if strings.Contains(low, tc.forbid) {
			t.Fatalf("%s contract-framed opener:\n%s", tc.svc, out.BodyText)
		}
		if !strings.Contains(low, tc.want) {
			t.Fatalf("%s missing opener %q:\n%s", tc.svc, tc.want, out.BodyText)
		}
	}
}

func TestAIPromptIsPlanOnly(t *testing.T) {
	pb := MustPlaybook()
	acc := testAccount("REAJUSTE", "ANUALIDADE", "contrato 1149/2022 atingiu aniversário de reajuste em 2024")
	acc.OfferRationale = "momento comercial indicado pelo extra-cli"
	cand := testCand("Sócio")
	ev := []models.OutreachEvidence{{
		SourceEvidenceID: "ev-1", EpistemicClass: models.OutreachEpistemicCommercialHypothesis,
		Synthesis: "eventos públicos relevantes sem triagem",
	}}
	st, plan := BuildOutboundPlan(pb, acc, cand, ev, 1)
	if plan.Messageability != MessageabilityReady {
		t.Fatalf("expected READY for strong reajuste, got %s %v", plan.Messageability, plan.ReasonCodes)
	}
	user := draftUserPromptWithPlan(BuildGenerateInput(ChannelEmailInitial, acc, cand, ev, nil), plan)
	for _, bad := range []string{
		"fact_to_mention", "internal_structure_hypothesis", "offer rationale",
		"objeto:", "órgão:", "eventos públicos relevantes sem triagem",
		"momento comercial indicado pelo extra-cli", "ProblemHypothesis",
	} {
		if strings.Contains(user, bad) {
			t.Fatalf("AI prompt leaked %q:\n%s", bad, user)
		}
	}
	if !strings.Contains(user, `"hook"`) || !strings.Contains(user, plan.Hook) {
		t.Fatalf("AI prompt must carry outbound-safe hook: %s", user)
	}
	_ = st
}

func TestAIAndTemplateShareGate(t *testing.T) {
	pb := MustPlaybook()
	acc := encopavAccount()
	cand := testCand("Sócio")
	ev := encopavEvidence()
	in := BuildGenerateInput(ChannelEmailInitial, acc, cand, ev, nil)
	tOut, _, tModel, err := TemplateGenerator{}.Generate(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if tModel != "messageability_gate" || tOut.BodyText != "" {
		t.Fatalf("template: model=%s body=%q", tModel, tOut.BodyText)
	}
	// AI with nil provider still evaluates the gate before Complete.
	g := &AIDraftGenerator{Provider: nil}
	aiOut, _, aiModel, aiErr := g.Generate(context.Background(), in)
	if aiErr != nil {
		t.Fatalf("not-READY AI path must not error, got %v", aiErr)
	}
	if aiModel != "messageability_gate" || aiOut.BodyText != "" {
		t.Fatalf("AI: model=%s body=%q", aiModel, aiOut.BodyText)
	}
	st, plan := BuildOutboundPlan(pb, acc, cand, ev, 1)
	if plan.Messageability == MessageabilityReady {
		t.Fatal("gate must not READY ENCOPAV")
	}
	_ = st
}
