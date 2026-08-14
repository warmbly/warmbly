package confenge

import (
	"fmt"
	"strings"

	"github.com/warmbly/warmbly/internal/models"
)

// FulfillmentDraft is the promised micro-deliverable after offer acceptance.
type FulfillmentDraft struct {
	OfferCode       string       `json:"offer_code"`
	Subject         string       `json:"subject"`
	BodyText        string       `json:"body_text"`
	EvidenceIDs     []string     `json:"evidence_ids"`
	Claims          []DraftClaim `json:"claims,omitempty"`
	ProhibitedHit   bool         `json:"prohibited_hit"`
	RiskFlags       []string     `json:"risk_flags,omitempty"`
	DoctrineVersion string       `json:"doctrine_version"`
	StrategyRef     string       `json:"strategy_ref,omitempty"`
}

// BuildFulfillmentDraft generates the promised micro-deliverable from strategy + evidence.
// Never pivots to "let's book a call" without delivering value first.
func BuildFulfillmentDraft(pb *Playbook, st OutreachStrategy, acc *models.OutreachAccount, evidence []models.OutreachEvidence) (FulfillmentDraft, error) {
	if pb == nil {
		var err error
		pb, err = LoadPlaybook()
		if err != nil {
			return FulfillmentDraft{}, err
		}
	}
	offer := pb.FindOffer(st.MicroOfferCode)
	if offer == nil {
		return FulfillmentDraft{}, fmt.Errorf("unknown offer %q", st.MicroOfferCode)
	}
	if strings.ToUpper(offer.FulfillmentCost) == "HIGH" {
		return FulfillmentDraft{}, fmt.Errorf("offer %s is HIGH cost; not auto-fulfillable", offer.Code)
	}

	company := ""
	if acc != nil {
		company = firstNonEmpty(acc.NomeFantasia, acc.RazaoSocial)
	}
	fact := st.ObservedFact
	if fact == "" && acc != nil {
		fact = acc.FactToMention
	}
	ids := st.EvidenceIDs
	if len(ids) == 0 {
		ids = evidenceIDsFrom(evidence, acc)
	}

	points := fulfillmentPoints(offer.Code, st, fact, company)
	var b strings.Builder
	b.WriteString("Segue o que prometi, de forma objetiva.\n\n")
	if fact != "" {
		b.WriteString("Contexto público: ")
		b.WriteString(fact)
		b.WriteString(".\n\n")
	}
	b.WriteString("O que eu conferiria:\n")
	for i, p := range points {
		b.WriteString(fmt.Sprintf("%d) %s\n", i+1, p))
	}
	b.WriteString("\nIsso não conclui mérito econômico sozinho; é só a checagem inicial.\n")
	b.WriteString("Se fizer sentido aprofundar depois, me diga.")

	body := strings.TrimSpace(b.String())
	// Guard prohibited claims
	low := strings.ToLower(body)
	hit := false
	for _, p := range offer.ProhibitedClaims {
		if p != "" && strings.Contains(low, strings.ToLower(p)) {
			hit = true
		}
	}
	for _, p := range st.ClaimsToAvoid {
		if p != "" && strings.Contains(low, strings.ToLower(p)) {
			hit = true
		}
	}

	subj := "Checklist: " + firstNonEmpty(offer.Code, "pontos a conferir")
	if company != "" {
		subj = trimSubject("Checklist · " + company)
	}

	fd := FulfillmentDraft{
		OfferCode:       offer.Code,
		Subject:         subj,
		BodyText:        body,
		EvidenceIDs:     ids,
		Claims:          claimsFromFact(fact, ids),
		ProhibitedHit:   hit,
		DoctrineVersion: OutreachDoctrineVersion,
		StrategyRef:     st.DoctrineVersion + "/" + st.MicroOfferCode,
	}
	if hit {
		fd.RiskFlags = append(fd.RiskFlags, "fulfillment_prohibited_claim")
	}
	// Must not be meeting-first
	if strings.Contains(low, "marcar uma call") || strings.Contains(low, "calendly") {
		fd.RiskFlags = append(fd.RiskFlags, "fulfillment_pivoted_to_meeting")
		return fd, fmt.Errorf("fulfillment must deliver value before meeting")
	}
	if hit {
		return fd, fmt.Errorf("fulfillment draft hit prohibited claim")
	}
	return fd, nil
}

func fulfillmentPoints(offerCode string, st OutreachStrategy, fact, company string) []string {
	base := []string{
		"Confirmar o instrumento e o trecho público relevante" + when(fact != "", " ("+truncateRunesOffer(fact, 80)+")"),
		"Separar fato publicado de hipótese ainda não validada",
		"Listar documentos/memórias que sustentariam qualquer pedido formal",
	}
	switch strings.ToUpper(offerCode) {
	case "REAJUSTE_CHECK":
		return []string{
			"Cláusula de reajuste e índice referenciado no instrumento público",
			"Marco temporal / anualidade (sinal de verificação, não de crédito automático)",
			"Memória de cálculo e base de incidência a reunir internamente",
			"Publicações que confirmem ou não formalização recente",
		}
	case "ADITIVO_RISK_CHECK":
		return []string{
			"Escopo e valores alterados no aditivo publicado",
			"Efeito em planilha base, prazos e reajuste derivado",
			"Consistência entre aditivo e medições subsequentes",
		}
	case "MEDICAO_CHECK":
		return []string{
			"Critério de medição no trecho crítico",
			"Alinhamento entre campo, planilha e publicação",
			"Documentos que fecham glosa/divergência",
		}
	case "CLOSEOUT_CHECK":
		return []string{
			"Pendências de medição e reajuste perto do término",
			"Documentos de quitação / termo de recebimento",
			"Janela operacional restante para regularização",
		}
	case "CONTRACT_TIMELINE":
		return []string{
			"Marcos públicos conhecidos (assinatura, aditivos, vigência)",
			"Eventos recentes ligados a " + firstNonEmpty(company, "o contrato"),
			"Próximas datas que mudam o que deve ser conferido",
		}
	case "PUBLIC_DATA_SNAPSHOT":
		return []string{
			"Recorte público usado: " + firstNonEmpty(fact, "publicação contratual"),
			"O que o recorte permite afirmar com segurança",
			"O que ainda exige confirmação da empresa",
		}
	case "CLAIM_READINESS_CHECK":
		return []string{
			"Nexo e fatos públicos disponíveis",
			"Lacunas documentais antes de qualquer pleito",
			"Ordem sugerida de reconstituição de memória",
		}
	default:
		if st.CommercialReframe != "" {
			base = append(base, "Perspectiva: "+truncateRunesOffer(st.CommercialReframe, 120))
		}
		return base
	}
}

func when(cond bool, s string) string {
	if cond {
		return s
	}
	return ""
}

func truncateRunesOffer(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "..."
}
