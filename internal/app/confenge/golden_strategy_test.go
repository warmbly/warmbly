package confenge

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/warmbly/warmbly/internal/models"
)

// models used for TouchpointPurpose* in purposeForPos.

// Golden fixtures (~20 scenarios) assert strategy properties, not one universal template.
func TestGoldenStrategyFixtures(t *testing.T) {
	pb := MustPlaybook()
	type fix struct {
		id, svc, moment, fact, role string
		pos                         int
		wantOffer                   string
		wantFlag                    string
		wantRole                    string
		mustNotBody                 []string
	}
	fixtures := []fix{
		{"01_strong_reajuste", "REAJUSTE", "ANUALIDADE", "contrato 1149/2022 aniversário de reajuste publicado", "Sócio", 1, "REAJUSTE_CHECK", "annualidade_verify_only", "OWNER_PARTNER", []string{"reajuste a receber"}},
		{"02_annualidade_no_proof", "REAJUSTE", "ANUALIDADE", "marco temporal de reajuste sem publicação de pagamento", "Diretor", 1, "REAJUSTE_CHECK", "annualidade_verify_only", "DIRECTOR", []string{"deixou de pagar"}},
		{"03_aditivo", "ADDITIVE_REVIEW", "ADITIVO_RECENTE", "termo aditivo 2 ao contrato 88/2021", "Engenheiro", 1, "ADITIVO_RISK_CHECK", "", "ENGINEERING", []string{"aditivo irregular"}},
		{"04_closeout", "ENCERRAMENTO_CONTRATUAL", "ENCERRAMENTO", "vigência encerra em 60 dias", "Planejamento", 1, "CLOSEOUT_CHECK", "", "PLANNING", nil},
		{"05_medicao", "MEDICOES", "MEDICAO", "medição lote 3 no DER-PR", "Analista Financeiro", 1, "MEDICAO_CHECK", "", "FINANCE", []string{"glosa indevida"}},
		{"06_regional", "REAJUSTE", "ANUALIDADE", "contrato regional pequeno porte aniversário", "Proprietário", 1, "REAJUSTE_CHECK", "annualidade_verify_only", "OWNER_PARTNER", []string{"não tem estrutura", "nao tem estrutura"}},
		{"07_robust", "MONITORAMENTO_CONTRATUAL", "PORTFOLIO", "carteira corporativa robusta com evento PNCP", "Diretor de Contratos", 1, "", "", "DIRECTOR", []string{"não têm equipe", "nao tem equipe"}},
		{"08_owner", "REAJUSTE", "DOCUMENTACAO_PUBLICADA", "publicação PNCP do contrato 12", "Sócio-administrador", 1, "", "", "OWNER_PARTNER", nil},
		{"09_engineering", "PLANILHAS", "MEDICAO", "quantitativos do trecho sul", "Engenheira responsável", 1, "", "", "ENGINEERING", nil},
		{"10_finance", "REAJUSTE", "PRORROGACAO", "prorrogação de prazo publicada", "Controller", 1, "", "", "FINANCE", nil},
		{"11_legal", "REEQUILIBRIO", "DIVERGENCIA", "possível divergência em aditivo (hipótese)", "Advogado interno", 1, "CLAIM_READINESS_CHECK", "hypothesis_language_required", "LEGAL", []string{"o contrato está desequilibrado"}},
		{"12_unknown_role", "REAJUSTE", "ANUALIDADE", "aniversário contratual", "", 1, "REAJUSTE_CHECK", "annualidade_verify_only", "UNKNOWN", nil},
		{"13_routing_touch", "REAJUSTE", "ANUALIDADE", "aniversário contratual", "Assistente", 4, "", "", "UNKNOWN", nil},
		{"14_no_social_proof", "ADITIVOS", "ADITIVO", "aditivo 1", "Comercial", 1, "", "", "COMMERCIAL", []string{"líderes de mercado", "lideres de mercado"}},
		{"15_no_name", "MEDICOES", "MEDICAO", "medição publicada", "Engenheiro", 1, "", "", "ENGINEERING", nil},
		{"16_low_confidence", "REAJUSTE", "ANUALIDADE", "possível janela de reajuste", "Sócio", 1, "REAJUSTE_CHECK", "", "OWNER_PARTNER", []string{"comprovamos que"}},
		{"17_contradictory", "REEQUILIBRIO", "DIVERGENCIA", "publicações conflitantes sobre aditivo", "Jurídico", 1, "", "hypothesis_language_required", "LEGAL", []string{"irregularidade confirmada"}},
		{"18_no_hook", "REAJUSTE", "GENERIC", "", "Sócio", 1, "", "no_safe_factual_hook", "OWNER_PARTNER", []string{"somos especialistas líderes"}},
		{"19_touch2", "ADITIVOS", "ADITIVO_RECENTE", "aditivo 3", "Engenheiro", 2, "", "", "ENGINEERING", []string{"só acompanhando"}},
		{"20_graceful_close", "REAJUSTE", "ANUALIDADE", "aniversário", "Sócio", 5, "", "", "OWNER_PARTNER", []string{"última tentativa", "ultima tentativa"}},
	}

	for _, f := range fixtures {
		t.Run(f.id, func(t *testing.T) {
			acc := testAccount(f.svc, f.moment, f.fact)
			if strings.Contains(f.fact, "robusta") || strings.Contains(f.fact, "corporativa") {
				acc.OfferRationale = "conta corporativa robusta"
			}
			if strings.Contains(f.fact, "regional") || strings.Contains(f.fact, "pequeno porte") {
				acc.MomentSummary = "empresa regional enxuta " + acc.MomentSummary
			}
			cand := testCand(f.role)
			if f.id == "15_no_name" {
				cand.Name = ""
			}
			var ev []models.OutreachEvidence
			if f.id == "16_low_confidence" {
				ev = []models.OutreachEvidence{{SourceEvidenceID: "ev-1", EpistemicClass: models.OutreachEpistemicWeakInference, Synthesis: f.fact}}
			} else if f.id == "17_contradictory" {
				ev = []models.OutreachEvidence{{SourceEvidenceID: "ev-1", EpistemicClass: models.OutreachEpistemicContradictoryEvidence, Synthesis: f.fact}}
			} else if f.fact != "" {
				ev = []models.OutreachEvidence{{SourceEvidenceID: "ev-1", EpistemicClass: models.OutreachEpistemicConfirmedFact, Synthesis: f.fact}}
			}
			st := PlanOutreachStrategy(pb, acc, cand, ev, f.pos)
			if st.DoctrineVersion != OutreachDoctrineVersion {
				t.Fatal("doctrine")
			}
			if st.WhyNow == "" && f.pos == 1 {
				t.Fatal("why_now")
			}
			if f.wantRole != "" && st.BuyerRole != f.wantRole {
				t.Fatalf("role got %s want %s", st.BuyerRole, f.wantRole)
			}
			if f.wantOffer != "" && st.MicroOfferCode != f.wantOffer {
				t.Fatalf("offer got %s want %s", st.MicroOfferCode, f.wantOffer)
			}
			if f.wantFlag != "" && !containsStr(st.RiskFlags, f.wantFlag) {
				t.Fatalf("flag %s missing in %v", f.wantFlag, st.RiskFlags)
			}
			// No commercial scores in strategy JSON
			raw, _ := json.Marshal(st)
			if strings.Contains(string(raw), "lead_score") || strings.Contains(string(raw), "conversion_score") {
				t.Fatal("scores leaked")
			}
			// Production channel mapping: pos>=2 uses EMAIL_FOLLOWUP so legitimate Re: is allowed.
			genCh := GenerationChannelForTouch(f.pos, purposeForPos(f.pos))
			if f.pos >= 2 && genCh != ChannelEmailFollowup {
				t.Fatalf("pos %d must map to EMAIL_FOLLOWUP, got %s", f.pos, genCh)
			}
			if f.pos <= 1 && genCh != ChannelEmailInitial {
				t.Fatalf("pos %d must map to EMAIL_INITIAL, got %s", f.pos, genCh)
			}
			out := ComposeFromStrategy(st, acc, cand, genCh)
			low := strings.ToLower(out.BodyText + " " + out.Subject)
			for _, bad := range f.mustNotBody {
				if bad != "" && strings.Contains(low, strings.ToLower(bad)) {
					t.Fatalf("body contains %q: %s", bad, out.BodyText)
				}
			}
			dqa := ValidateDoctrineCopy(&out, &st, pb, genCh)
			for _, e := range dqa.Errors {
				if strings.Contains(e, "annualidade must not") || strings.Contains(e, "empty follow-up") {
					t.Fatalf("doctrine: %s", e)
				}
				// Legitimate in-thread Re: on follow-up must not fail as fake first-touch Re.
				if strings.Contains(e, "fake Re") {
					t.Fatalf("production channel mapping should allow Re: on follow-up: %s subj=%q", e, out.Subject)
				}
			}
			if f.pos >= 2 && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(out.Subject)), "re:") {
				t.Fatalf("follow-up subject should be Re: got %q", out.Subject)
			}
			ex := ExplainStrategy(st, cand.Email)
			if ex.Doctrine == "" || (f.pos == 1 && ex.WhyNow == "") {
				t.Fatalf("explain incomplete: %+v", ex)
			}
		})
	}
}

func purposeForPos(pos int) string {
	switch {
	case pos >= 5:
		return models.TouchpointPurposeClose
	case pos >= 2:
		return models.TouchpointPurposeFollowUp
	default:
		return models.TouchpointPurposeInitial
	}
}

// Production path: GenerateTouchpointDraft validation channel for ordinal>=2 must accept Re: subjects.
func TestFollowUpChannelAllowsReSubject(t *testing.T) {
	pb := MustPlaybook()
	acc := testAccount("REAJUSTE", "ANUALIDADE", "contrato 1149 aniversário de reajuste")
	cand := testCand("Sócio")
	ev := []models.OutreachEvidence{{SourceEvidenceID: "ev-1", EpistemicClass: models.OutreachEpistemicConfirmedFact, Synthesis: acc.FactToMention}}

	for _, pos := range []int{2, 3, 4, 5} {
		genCh := GenerationChannelForTouch(pos, purposeForPos(pos))
		if genCh != ChannelEmailFollowup {
			t.Fatalf("pos %d channel %s", pos, genCh)
		}
		st := PlanOutreachStrategy(pb, acc, cand, ev, SequencePositionForTouch(pos, purposeForPos(pos)))
		out := ComposeFromStrategy(st, acc, cand, genCh)
		out.Channel = genCh
		val := ValidateDraft(&out, acc, cand, ValidateOpts{
			MaxWords: 200, Evidence: ev, Channel: genCh, Strategy: &st, Playbook: pb,
		})
		for _, e := range val.Errors {
			if strings.Contains(e, "fake Re") {
				t.Fatalf("pos %d: Re: subject wrongly rejected: %s subj=%q", pos, e, out.Subject)
			}
		}
		// Implication body must not leak English mash from annualidade strategy.
		if strings.Contains(out.BodyText, "reconstituting") {
			t.Fatalf("pos %d: English leak in body: %s", pos, out.BodyText)
		}
	}
}
