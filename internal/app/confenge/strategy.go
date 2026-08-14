package confenge

import (
	"strings"

	"github.com/warmbly/warmbly/internal/models"
)

// CTA types for first-touch strategy.
const (
	CTATypePermissionOffer = "PERMISSION_OFFER"
	CTATypeInterest        = "INTEREST"
	CTATypeRouting         = "ROUTING"
	CTATypeGracefulClose   = "GRACEFUL_CLOSE"
	CTATypeMeeting         = "MEETING" // only after sufficient interest
	CTATypeFulfillment     = "FULFILLMENT"
)

// OutreachStrategy is the commercial plan produced BEFORE any copy.
// It never contains activation/lead/commercial scores.
type OutreachStrategy struct {
	DoctrineVersion string `json:"doctrine_version"`
	AccountID       string `json:"account_id,omitempty"`
	ContactID       string `json:"contact_id,omitempty"`

	BuyerRole   string `json:"buyer_role"`
	ServiceCode string `json:"service_code"`
	ServiceName string `json:"service_name,omitempty"`

	ActivationTrigger string   `json:"activation_trigger"`
	TriggerSummary    string   `json:"trigger_summary"`
	EvidenceIDs       []string `json:"evidence_ids"`

	WhyThisAccount string `json:"why_this_account"`
	WhyNow         string `json:"why_now"`

	ObservedFact          string `json:"observed_fact"`
	ProblemHypothesis     string `json:"problem_hypothesis"`
	ImplicationHypothesis string `json:"implication_hypothesis"`
	CommercialReframe     string `json:"commercial_reframe"`
	MicroOfferCode        string `json:"micro_offer_code"`
	MicroOfferDescription string `json:"micro_offer_description,omitempty"`
	CredibilityBasis      string `json:"credibility_basis"`
	CTAType               string `json:"cta_type"`
	CTASuggested          string `json:"cta_suggested,omitempty"`
	SequencePosition      int    `json:"sequence_position"`
	SequenceTouchName     string `json:"sequence_touch_name,omitempty"`

	ClaimsAllowed        []string `json:"claims_allowed,omitempty"`
	ClaimsToAvoid        []string `json:"claims_to_avoid,omitempty"`
	PersonalizationBasis string   `json:"personalization_basis"`
	RiskFlags            []string `json:"risk_flags,omitempty"`

	ValueToRecipient string `json:"value_to_recipient,omitempty"`
	AccountArchetype string `json:"account_archetype,omitempty"` // robust | regional | unknown

	// Experiment metadata (assignment only; not a score).
	Experiment *ExperimentAssignment `json:"experiment,omitempty"`
}

// StrategyExplain is the compact operator cockpit projection.
type StrategyExplain struct {
	WhyThisAccount       string   `json:"why_this_account"`
	WhyNow               string   `json:"why_now"`
	FactUsed             string   `json:"fact_used"`
	Hypothesis           string   `json:"hypothesis"`
	Service              string   `json:"service"`
	Offer                string   `json:"offer"`
	Recipient            string   `json:"recipient"`
	Sources              []string `json:"sources,omitempty"`
	Touch                string   `json:"touch"`
	Experiment           string   `json:"experiment,omitempty"`
	Doctrine             string   `json:"doctrine_version"`
	Messageability       string   `json:"messageability,omitempty"`
	MessageabilityReason string   `json:"messageability_reason,omitempty"`
}

// PlanOutreachStrategy builds strategy from dossier only (no re-scoring).
func PlanOutreachStrategy(
	pb *Playbook,
	acc *models.OutreachAccount,
	cand *models.OutreachContactCandidate,
	evidence []models.OutreachEvidence,
	sequencePosition int,
) OutreachStrategy {
	if pb == nil {
		pb, _ = LoadPlaybook()
	}
	st := OutreachStrategy{
		DoctrineVersion:  OutreachDoctrineVersion,
		SequencePosition: sequencePosition,
		BuyerRole:        "UNKNOWN",
		CTAType:          CTATypePermissionOffer,
		CredibilityBasis: "quality_of_public_observation",
	}
	if sequencePosition <= 0 {
		st.SequencePosition = 1
	}
	if pb != nil {
		for _, t := range pb.Sequence.Sequence.Touches {
			if t.Position == st.SequencePosition {
				st.SequenceTouchName = t.Name
				break
			}
		}
	}

	if acc != nil {
		st.AccountID = acc.ID.String()
		st.ServiceCode = strings.TrimSpace(acc.ServiceCode)
		st.ServiceName = strings.TrimSpace(acc.ServiceName)
		st.ActivationTrigger = strings.TrimSpace(acc.MomentCode)
		st.TriggerSummary = strings.TrimSpace(acc.MomentSummary)
		st.EvidenceIDs = append([]string{}, acc.MomentEvidenceIDs...)
		st.ObservedFact = strings.TrimSpace(acc.FactToMention)
		st.ClaimsToAvoid = append([]string{}, acc.ClaimsToAvoid...)
		company := firstNonEmpty(acc.NomeFantasia, acc.RazaoSocial)
		// Prefer concrete public fact over hollow "momento comercial público" boilerplate.
		st.WhyThisAccount = buildWhyThisAccount(company, acc.CNPJ14, st.ObservedFact, st.TriggerSummary)
		st.AccountArchetype = inferArchetype(acc, evidence)
	}
	if cand != nil {
		st.ContactID = cand.ID.String()
		if isGenericRecipient(cand) {
			st.RiskFlags = appendUnique(st.RiskFlags, "generic_recipient")
		} else if pb != nil {
			st.BuyerRole = pb.MapBuyerRole(cand.Role)
		}
	}

	// Evidence IDs from rows
	for _, e := range evidence {
		id := strings.TrimSpace(e.SourceEvidenceID)
		if id == "" {
			continue
		}
		if !containsStr(st.EvidenceIDs, id) {
			st.EvidenceIDs = append(st.EvidenceIDs, id)
		}
		if st.ObservedFact == "" {
			st.ObservedFact = firstNonEmpty(e.Synthesis, e.Excerpt, e.Title)
		}
	}

	var svc *ServicePlaybook
	var trig *TriggerRule
	if pb != nil {
		svc = pb.ResolveServicePlaybook(st.ServiceCode)
		trig = pb.ResolveTrigger(st.ActivationTrigger)
	}

	if trig != nil {
		st.WhyNow = strings.TrimSpace(trig.WhyNowTemplate)
		if st.TriggerSummary != "" {
			st.WhyNow = st.TriggerSummary
		}
		st.ClaimsToAvoid = appendUnique(st.ClaimsToAvoid, trig.ClaimsToAvoid...)
		st.RiskFlags = appendUnique(st.RiskFlags, trig.RiskFlags...)
	} else if st.TriggerSummary != "" {
		st.WhyNow = st.TriggerSummary
	} else {
		st.WhyNow = "momento comercial indicado pelo extra-cli"
		st.RiskFlags = appendUnique(st.RiskFlags, "weak_trigger")
	}

	if svc != nil {
		if st.ServiceName == "" {
			st.ServiceName = svc.Name
		}
		// Normalize service code to playbook canonical (never invent REAJUSTE for unknown).
		if st.ServiceCode != "" && !strings.EqualFold(st.ServiceCode, svc.Code) {
			// Keep upstream code in strategy but resolve playbook via svc.Code for offers.
			st.ServiceName = svc.Name
		}
		st.CommercialReframe = strings.TrimSpace(svc.CommercialInsight)
		if len(svc.ProblemHypotheses) > 0 {
			st.ProblemHypothesis = svc.ProblemHypotheses[0]
		}
		if len(svc.Implications) > 0 {
			st.ImplicationHypothesis = svc.Implications[0]
		}
		st.ClaimsToAvoid = appendUnique(st.ClaimsToAvoid, svc.DisallowedClaims...)
		st.MicroOfferCode = svc.DefaultMicroOffer
	} else if strings.TrimSpace(st.ServiceCode) != "" {
		// Unknown service from extra-cli: fail closed — never map to REAJUSTE.
		st.RiskFlags = appendUnique(st.RiskFlags, "unknown_service_code", "needs_review", "service_unmapped")
		st.MicroOfferCode = ""
		st.ProblemHypothesis = ""
		st.CommercialReframe = "Serviço upstream não mapeado no playbook; não inventar especialidade."
	} else {
		st.RiskFlags = appendUnique(st.RiskFlags, "missing_service_code", "needs_review")
	}

	// Annualidade special case: never claim unpaid reajuste.
	// Only when service is already reajuste-family — do not force REAJUSTE onto other services.
	reajusteFamily := (svc != nil && strings.EqualFold(svc.Code, "REAJUSTE")) ||
		strings.Contains(strings.ToUpper(st.ServiceCode), "REAJUSTE")
	if isAnnualidadeContext(st.ActivationTrigger, st.TriggerSummary, st.ObservedFact) && reajusteFamily {
		st.RiskFlags = appendUnique(st.RiskFlags, "annualidade_verify_only")
		st.ProblemHypothesis = "pode haver documentos e memórias de reajuste a conferir neste ciclo (hipótese, não crédito comprovado)"
		st.ImplicationHypothesis = "sem verificação, a equipe pode perder tempo reconstituindo a memória documental depois"
		st.CommercialReframe = "Anualidade é sinal de verificação, não prova de reajuste devido."
		st.MicroOfferCode = "REAJUSTE_CHECK"
		st.ClaimsToAvoid = appendUnique(st.ClaimsToAvoid,
			"reajuste a receber", "o órgão deixou de pagar", "crédito de reajuste", "identificamos perda")
		st.ClaimsAllowed = appendUnique(st.ClaimsAllowed,
			"janela de verificação documental", "checklist de reajuste", "dados públicos sugerem conferência")
	}

	// Prefer trigger offers when set; keep service default if it is already preferred.
	if pb != nil && trig != nil && len(trig.PreferredOffers) > 0 {
		keepDefault := false
		if st.MicroOfferCode != "" {
			for _, oc := range trig.PreferredOffers {
				if strings.EqualFold(oc, st.MicroOfferCode) {
					keepDefault = true
					break
				}
			}
		}
		if !keepDefault {
			for _, oc := range trig.PreferredOffers {
				if o := pb.FindOffer(oc); o != nil && pb.OfferApplicable(o, st.ServiceCode, true) {
					st.MicroOfferCode = o.Code
					break
				}
			}
		}
	}

	if pb != nil && st.MicroOfferCode != "" {
		if o := pb.FindOffer(st.MicroOfferCode); o != nil {
			if !pb.OfferApplicable(o, st.ServiceCode, true) {
				st.RiskFlags = appendUnique(st.RiskFlags, "offer_not_applicable")
				st.MicroOfferCode = fallbackLOWOffer(pb, st.ServiceCode)
			} else {
				st.MicroOfferDescription = o.Description
				st.ValueToRecipient = o.BuyerValue
				st.ClaimsToAvoid = appendUnique(st.ClaimsToAvoid, o.ProhibitedClaims...)
				if len(o.CTAPatterns) > 0 {
					st.CTASuggested = o.CTAPatterns[0]
				}
			}
		}
	}
	// Only apply generic LOW fallback when service is known; unknown must stay empty (NEEDS_REVIEW).
	if st.MicroOfferCode == "" && pb != nil && svc != nil {
		st.MicroOfferCode = fallbackLOWOffer(pb, st.ServiceCode)
		if o := pb.FindOffer(st.MicroOfferCode); o != nil {
			st.MicroOfferDescription = o.Description
			st.ValueToRecipient = o.BuyerValue
			if len(o.CTAPatterns) > 0 {
				st.CTASuggested = o.CTAPatterns[0]
			}
		}
	}
	if st.MicroOfferCode == "" {
		st.RiskFlags = appendUnique(st.RiskFlags, "incomplete_strategy", "needs_review")
	}
	if st.WhyThisAccount == "" || st.WhyNow == "" || st.ObservedFact == "" ||
		isGenericWhyThisAccount(st.WhyThisAccount) || isGenericWhyNow(st.WhyNow) {
		st.RiskFlags = appendUnique(st.RiskFlags, "incomplete_copy_context", "needs_review")
	}

	// Sequence-specific CTA
	switch st.SequencePosition {
	case 4:
		st.CTAType = CTATypeRouting
		if st.CTASuggested == "" {
			st.CTASuggested = "Quem na equipe acompanha este tema no dia a dia?"
		}
	case 5:
		st.CTAType = CTATypeGracefulClose
		st.CTASuggested = "Encerro por aqui para não ocupar sua caixa."
	default:
		st.CTAType = CTATypePermissionOffer
	}

	// Tailor hypothesis language by buyer role (still hypothesis)
	st.ProblemHypothesis = tailorHypothesis(st.BuyerRole, st.ProblemHypothesis, st.AccountArchetype)

	if st.ObservedFact == "" {
		st.RiskFlags = appendUnique(st.RiskFlags, "no_safe_factual_hook")
		st.PersonalizationBasis = "insufficient_public_fact"
	} else {
		st.PersonalizationBasis = "public_contract_fact"
	}

	// Low-confidence evidence
	for _, e := range evidence {
		switch e.EpistemicClass {
		case models.OutreachEpistemicWeakInference, models.OutreachEpistemicCommercialHypothesis,
			models.OutreachEpistemicRequiresCompanyConfirm, models.OutreachEpistemicContradictoryEvidence:
			st.RiskFlags = appendUnique(st.RiskFlags, "evidence_requires_hypothesis_language")
		}
	}

	if st.ValueToRecipient == "" {
		st.ValueToRecipient = "clareza objetiva sobre um ponto contratual público"
	}
	if st.ClaimsAllowed == nil {
		st.ClaimsAllowed = []string{"fato público citado", "hipótese linguistically scoped", "micro-oferta de verificação"}
	}

	// Account-level experiment (single dimension, no scores)
	if acc != nil {
		st.Experiment = AssignExperiment(acc.ID.String(), st.ServiceCode, st.MicroOfferCode)
	}

	return st
}

// ExplainStrategy projects operator cockpit fields.
func ExplainStrategy(st OutreachStrategy, recipient string) StrategyExplain {
	touch := ""
	if st.SequenceTouchName != "" {
		touch = st.SequenceTouchName
	}
	if st.SequencePosition > 0 {
		if touch != "" {
			touch = touch + " "
		}
		touch += "#" + itoa(st.SequencePosition)
	}
	exp := ""
	if st.Experiment != nil {
		exp = st.Experiment.VariantID
		if st.Experiment.Dimension != "" {
			exp = st.Experiment.Dimension + ":" + exp
		}
	}
	return StrategyExplain{
		WhyThisAccount: st.WhyThisAccount,
		WhyNow:         st.WhyNow,
		FactUsed:       st.ObservedFact,
		Hypothesis:     firstNonEmpty(st.ProblemHypothesis, st.ImplicationHypothesis),
		Service:        firstNonEmpty(st.ServiceName, st.ServiceCode),
		Offer:          firstNonEmpty(st.MicroOfferDescription, st.MicroOfferCode),
		Recipient:      recipient,
		Sources:        st.EvidenceIDs,
		Touch:          touch,
		Experiment:     exp,
		Doctrine:       st.DoctrineVersion,
	}
}

func buildWhyThisAccount(company, cnpj, fact, momentSummary string) string {
	fact = strings.TrimSpace(fact)
	company = strings.TrimSpace(company)
	momentSummary = strings.TrimSpace(momentSummary)
	if fact != "" && len(fact) >= 24 && !isGenericPublicFact(fact) {
		if company != "" {
			return company + " — fato público: " + truncateRunes(fact, 180)
		}
		return "fato público: " + truncateRunes(fact, 180)
	}
	if momentSummary != "" && !isGenericWhyNow(momentSummary) && len(momentSummary) >= 24 {
		if company != "" {
			return company + " — " + truncateRunes(momentSummary, 160)
		}
		return truncateRunes(momentSummary, 160)
	}
	// Hollow fallback is intentionally weak so incomplete_copy_context fires.
	if company != "" {
		return "empresa com momento comercial público: " + company
	}
	return "empresa com momento comercial público: " + strings.TrimSpace(cnpj)
}

func isGenericWhyThisAccount(s string) bool {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" || len([]rune(t)) < 40 {
		return true
	}
	hollow := []string{
		"empresa com momento comercial público",
		"portfólio público observável",
		"portfolio publico observavel",
		"empresa com portfólio público",
	}
	for _, h := range hollow {
		if strings.Contains(t, h) {
			// Still hollow unless a concrete contractual token appears.
			if !hasConcreteContractToken(t) {
				return true
			}
		}
	}
	return false
}

func isGenericWhyNow(s string) bool {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" {
		return true
	}
	hollow := []string{
		"momento comercial indicado pelo extra-cli",
		"portfólio público de contratos de engenharia/construção observado",
		"portfolio publico de contratos",
		"há portfólio público observável sem dor contratual",
		"ha portfolio publico observavel sem dor contratual",
	}
	for _, h := range hollow {
		if strings.Contains(t, h) && !hasConcreteContractToken(t) {
			return true
		}
	}
	return false
}

func isGenericPublicFact(s string) bool {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" {
		return true
	}
	// Portfolio-count boilerplate from intelligence layers.
	if strings.Contains(t, "portfólio público observado com") && strings.Contains(t, "contrato") {
		return true
	}
	if strings.Contains(t, "ufs observadas nos contratos") {
		return true
	}
	hollow := []string{
		"empresa possui contrato público",
		"empresa possui contrato publico",
		"há contratos públicos",
		"ha contratos publicos",
		"há portfólio público observável",
		"ha portfolio publico observavel",
		"portfólio público observável",
		"portfolio publico observavel",
		"empresa com site institucional",
		"contratos públicos esporádicos",
		"contratos publicos esporadicos",
	}
	for _, h := range hollow {
		if t == h || strings.HasPrefix(t, h) {
			if !hasConcreteContractEvent(t) {
				return true
			}
		}
	}
	return false
}

func hasConcreteContractToken(t string) bool {
	for _, c := range []string{
		"objeto", "paviment", "obra", "engenharia", "aditivo", "medição", "medicao",
		"orgão", "orgao", "prefeitura", "dnit", "empreitada", "saneamento", "reabilitação",
		"reabilitacao", "contrato ", "fato público:",
	} {
		if strings.Contains(t, c) {
			return true
		}
	}
	return false
}

func isAnnualidadeContext(trigger, summary, fact string) bool {
	blob := strings.ToLower(trigger + " " + summary + " " + fact)
	for _, k := range []string{"anualidade", "aniversário", "aniversario", "annuality", "marco temporal de reajuste", "janela de reajuste"} {
		if strings.Contains(blob, k) {
			return true
		}
	}
	return strings.EqualFold(strings.TrimSpace(trigger), "ANUALIDADE")
}

func inferArchetype(acc *models.OutreachAccount, evidence []models.OutreachEvidence) string {
	// Prefer explicit hints in moment/summary; never invent from email domain alone.
	blob := ""
	if acc != nil {
		blob = strings.ToLower(acc.MomentSummary + " " + acc.OfferRationale + " " + acc.PriorityTier)
	}
	for _, e := range evidence {
		blob += " " + strings.ToLower(e.Synthesis)
	}
	if strings.Contains(blob, "robusta") || strings.Contains(blob, "corporativ") ||
		strings.Contains(blob, "grande porte") || strings.Contains(blob, "strategic") {
		return "robust"
	}
	if strings.Contains(blob, "regional") || strings.Contains(blob, "enxut") ||
		strings.Contains(blob, "pequeno porte") || strings.Contains(blob, "mei ") {
		return "regional"
	}
	return "unknown"
}

func tailorHypothesis(role, hyp, archetype string) string {
	hyp = strings.TrimSpace(hyp)
	if hyp == "" {
		return hyp
	}
	if archetype == "robust" {
		return hyp + " (como segunda leitura / validação independente, sem presumir falta de capacidade interna)"
	}
	switch role {
	case "FINANCE":
		return hyp + " com impacto potencial em previsibilidade de recebimento (hipótese)"
	case "LEGAL":
		return hyp + " no plano técnico-documental que costuma apoiar o jurídico (hipótese)"
	case "ENGINEERING", "CONTRACTS":
		return hyp + " em documentação e memória de cálculo (hipótese)"
	case "OWNER_PARTNER", "DIRECTOR":
		return hyp + " com leitura de risco/margem, não de acusação (hipótese)"
	default:
		return hyp
	}
}

func fallbackLOWOffer(pb *Playbook, serviceCode string) string {
	if pb == nil {
		return "DOCUMENT_CHECKLIST"
	}
	for _, o := range pb.Offers.Offers {
		if pb.OfferApplicable(&o, serviceCode, true) {
			return o.Code
		}
	}
	return "DOCUMENT_CHECKLIST"
}

func appendUnique(dst []string, extra ...string) []string {
	seen := map[string]bool{}
	for _, d := range dst {
		seen[strings.ToLower(strings.TrimSpace(d))] = true
	}
	for _, e := range extra {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		k := strings.ToLower(e)
		if seen[k] {
			continue
		}
		seen[k] = true
		dst = append(dst, e)
	}
	return dst
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// SequencePositionForTouch maps touchpoint ordinal/purpose to doctrine sequence position (1–5).
func SequencePositionForTouch(ordinal int, purpose string) int {
	pos := ordinal
	if pos <= 0 {
		pos = 1
	}
	switch purpose {
	case models.TouchpointPurposeFollowUp:
		if pos < 2 {
			pos = 2
		}
	case models.TouchpointPurposeClose:
		pos = 5
	}
	if pos > 5 {
		pos = 5
	}
	return pos
}

// GenerationChannelForTouch maps sequence position to generation/validation channel.
// Follow-ups and close must use EMAIL_FOLLOWUP so legitimate in-thread "Re:" subjects
// are not treated as fake first-touch Re/Fwd.
func GenerationChannelForTouch(ordinal int, purpose string) string {
	if SequencePositionForTouch(ordinal, purpose) >= 2 {
		return ChannelEmailFollowup
	}
	return ChannelEmailInitial
}
