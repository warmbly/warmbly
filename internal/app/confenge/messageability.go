package confenge

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/warmbly/warmbly/internal/models"
)

// Messageability answers only: can this dossier produce a specific, defensible
// first contact worth interrupting the recipient? It is not a commercial score.
const (
	MessageabilityReady           = "READY"
	MessageabilityNeedsEnrichment = "NEEDS_ENRICHMENT"
	MessageabilityBlocked         = "BLOCKED"
)

// Recipient modes for outbound-safe copy.
const (
	RecipientModeNamed        = "NAMED"
	RecipientModeGenericInbox = "GENERIC_INBOX"
)

// ComposerVersion stamps the outbound-safe composer. Prior-version unsent
// drafts are identifiable and must be regenerated.
const ComposerVersion = "confenge.composer.v2"

// Outbound hook classes from the service playbook.
const (
	HookClassContractEvent = "contract_event"
	HookClassMomentOrEvent = "moment_or_event"
	HookClassNone          = "none"
)

// OutboundMessagePlan is the only structure the renderer may read.
// Internal strategy fields never appear here.
type OutboundMessagePlan struct {
	Messageability   string   `json:"messageability"`
	RecipientMode    string   `json:"recipient_mode"`
	ServiceCode      string   `json:"service_code"`
	Hook             string   `json:"hook"`
	HookLead         string   `json:"hook_lead,omitempty"`
	Relevance        string   `json:"relevance"`
	ValueUnit        string   `json:"value_unit"`
	CTA              string   `json:"cta"`
	EvidenceIDs      []string `json:"evidence_ids,omitempty"`
	CheckPoints      []string `json:"check_points,omitempty"`
	Reason           string   `json:"reason,omitempty"`
	ReasonCodes      []string `json:"reason_codes,omitempty"`
	ComposerVersion  string   `json:"composer_version"`
	SequencePosition int      `json:"sequence_position,omitempty"`
}

// EvaluateMessageability classifies whether the current dossier can support
// a specific outbound-safe first contact. extra-cli remains WHO/WHY NOW;
// this gate only decides HOW TO APPROACH, never lead priority.
func EvaluateMessageability(
	st OutreachStrategy,
	acc *models.OutreachAccount,
	cand *models.OutreachContactCandidate,
	evidence []models.OutreachEvidence,
	pb *Playbook,
) OutboundMessagePlan {
	if pb == nil {
		pb, _ = LoadPlaybook()
	}
	plan := OutboundMessagePlan{
		ComposerVersion:  ComposerVersion,
		ServiceCode:      strings.TrimSpace(st.ServiceCode),
		EvidenceIDs:      append([]string{}, st.EvidenceIDs...),
		RecipientMode:    RecipientModeNamed,
		SequencePosition: st.SequencePosition,
	}
	if isGenericRecipient(cand) {
		plan.RecipientMode = RecipientModeGenericInbox
	}

	add := func(code, reason string) {
		plan.ReasonCodes = appendUnique(plan.ReasonCodes, code)
		if plan.Reason == "" && strings.TrimSpace(reason) != "" {
			plan.Reason = strings.TrimSpace(reason)
		}
	}

	// --- BLOCKED: factual / contact / service / suppression ---
	if acc != nil {
		if acc.DoNotContact {
			plan.Messageability = MessageabilityBlocked
			add("account_dnc", "A conta está marcada como não contatar.")
			return plan
		}
		if acc.Blocked {
			plan.Messageability = MessageabilityBlocked
			add("account_blocked", "A conta está bloqueada.")
			return plan
		}
	}
	if cand != nil {
		if cand.DoNotContact {
			plan.Messageability = MessageabilityBlocked
			add("contact_dnc", "O contato está marcado como não contatar.")
			return plan
		}
		if cand.Bounced {
			plan.Messageability = MessageabilityBlocked
			add("contact_bounced", "O endereço do contato devolveu e-mail.")
			return plan
		}
	}
	if containsStr(st.RiskFlags, "unknown_service_code") || containsStr(st.RiskFlags, "service_unmapped") {
		plan.Messageability = MessageabilityBlocked
		add("unknown_service", "O serviço upstream não tem playbook; não há abordagem defensável.")
		return plan
	}
	if strings.TrimSpace(st.ServiceCode) == "" || containsStr(st.RiskFlags, "missing_service_code") {
		plan.Messageability = MessageabilityBlocked
		add("missing_service", "Falta o código de serviço; não há unidade de valor.")
		return plan
	}

	svc := (*ServicePlaybook)(nil)
	if pb != nil {
		svc = pb.ResolveServicePlaybook(st.ServiceCode)
	}
	if svc == nil {
		plan.Messageability = MessageabilityBlocked
		add("unknown_service", "O serviço não está mapeado no playbook.")
		return plan
	}
	// Keep the upstream code on the plan so validators can match the account.
	if plan.ServiceCode == "" {
		plan.ServiceCode = svc.Code
	}

	// --- Hook: outbound-safe fact, never a metadata dump ---
	rawFact := firstNonEmpty(st.ObservedFact, factFromEvidence(evidence))
	hook, hookCode := outboundSafeHook(rawFact, evidence)
	if hookCode != "" {
		add(hookCode, operatorReasonForHook(hookCode, svc.Code))
	}

	hookClass := strings.TrimSpace(svc.OutboundHookClass)
	if hookClass == "" {
		hookClass = HookClassMomentOrEvent
	}

	// extra-cli MomentCode/MomentSummary are WHO/WHY NOW labels, not a mentionable hook.
	blob := mentionableSurface(hook, rawFact, evidence)
	hasEvent := hasConcreteContractEvent(blob)

	switch hookClass {
	case HookClassNone:
		add("no_outbound_hook_class", "Este serviço não autoriza primeiro contato só com o dossiê atual.")
	case HookClassContractEvent:
		// Mere publication of a public contract (value, órgão, UF) is not enough.
		if !hasEvent {
			add("missing_contract_event",
				"Existe contrato público, mas nenhum evento contratual específico sustenta ainda uma primeira abordagem.")
		}
	default:
		if hook == "" || isGenericPublicFact(hook) || isGenericPublicFact(rawFact) {
			add("no_safe_hook", "Não há fato público específico o bastante para um primeiro contato.")
		}
		if looksLikeMetadataDump(rawFact) && (hook == "" || looksLikeMetadataDump(hook) || countWords(hook) < 8) {
			add("metadata_dump", "O fato público chegou como metadado serializado, não como gancho comercial.")
		}
		if hook != "" && !serviceHookFits(svc.Code, blob) {
			add("hook_service_mismatch",
				"O fato público não é coerente com o serviço oferecido; não sustenta um primeiro contato.")
		}
	}

	// MONITORAMENTO_CONTRATUAL: publication of a contract of value R$ X is
	// normally NEEDS_ENRICHMENT unless a verifiable contract event exists.
	if strings.EqualFold(svc.Code, "MONITORAMENTO_CONTRATUAL") && !hasEvent {
		add("missing_contract_event",
			"Existe contrato público, mas nenhum evento contratual específico sustenta ainda uma primeira abordagem.")
	}

	// --- Value unit + enumerable checkpoints ---
	plan.ValueUnit = outboundValueUnit(svc.Code, st.MicroOfferCode, pb)
	plan.CheckPoints = outboundCheckPoints(svc.Code, blob, hasEvent)
	plan.CTA = outboundCTA(st, pb, svc)
	plan.Relevance = outboundRelevance(svc.Code, hook, hasEvent)
	plan.Hook = hook
	plan.HookLead = hookLeadForService(svc.Code)

	if strings.TrimSpace(plan.ValueUnit) == "" || isAbstractValueUnit(plan.ValueUnit) {
		add("missing_value_unit", "Não há unidade de valor concreta para oferecer neste primeiro contato.")
	}

	if ctaPromisesPoints(plan.CTA) && len(plan.CheckPoints) == 0 {
		add("unfulfillable_cta", "O CTA promete pontos concretos, mas o dossiê não sustenta nenhum.")
	}

	// Weak offer predicates never auto-qualify outbound.
	if pb != nil && st.MicroOfferCode != "" {
		if o := pb.FindOffer(st.MicroOfferCode); o != nil {
			if offerEvidenceTooWeak(o) && !hasEvent && hookClass == HookClassContractEvent {
				add("weak_offer_evidence", "A micro-oferta exige evidência específica; o dossiê só tem fato público genérico.")
			}
			if !pb.OfferApplicable(o, svc.Code, true) {
				add("offer_not_applicable", "A micro-oferta não se aplica a este serviço.")
			}
		}
	}
	if containsStr(st.RiskFlags, "incomplete_strategy") || strings.TrimSpace(st.MicroOfferCode) == "" {
		add("incomplete_strategy", "A estratégia comercial está incompleta para um primeiro contato.")
	}

	// Provenance / suppression style flags stay blocked.
	for _, f := range st.RiskFlags {
		switch f {
		case "account_dnc", "contact_dnc", "suppressed", "provenance_tainted":
			plan.Messageability = MessageabilityBlocked
			add(f, "Há um bloqueio factual, de contato ou de proveniência.")
			return plan
		}
	}

	if len(plan.ReasonCodes) > 0 {
		plan.Messageability = MessageabilityNeedsEnrichment
		if plan.Reason == "" {
			plan.Reason = "O dossiê atual não sustenta ainda um primeiro contato específico e defensável."
		}
		// Fail closed: no sendable fields when not READY.
		plan.Hook, plan.HookLead, plan.Relevance, plan.ValueUnit, plan.CTA = "", "", "", "", ""
		plan.CheckPoints = nil
		return plan
	}

	plan.Messageability = MessageabilityReady
	if plan.CTA == "" {
		plan.CTA = "Posso te mandar o recorte objetivo que eu conferiria?"
	}
	return plan
}

func factFromEvidence(evidence []models.OutreachEvidence) string {
	for _, e := range evidence {
		if s := firstNonEmpty(e.Synthesis, e.Excerpt, e.Title); strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func evidenceBlob(evidence []models.OutreachEvidence) string {
	var b strings.Builder
	for _, e := range evidence {
		b.WriteString(e.Title)
		b.WriteByte(' ')
		b.WriteString(e.Synthesis)
		b.WriteByte(' ')
		b.WriteString(e.Excerpt)
		b.WriteByte(' ')
	}
	return b.String()
}

// mentionableSurface is the only text that may justify READY. extra-cli
// ActivationTrigger / TriggerSummary (MomentCode / MomentSummary) stay out.
func mentionableSurface(hook, rawFact string, evidence []models.OutreachEvidence) string {
	return strings.ToLower(strings.TrimSpace(hook + " " + rawFact + " " + evidenceBlob(evidence)))
}

// outboundSafeHook condenses a fact into natural language or rejects dumps.
func outboundSafeHook(raw string, evidence []models.OutreachEvidence) (hook, code string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = factFromEvidence(evidence)
	}
	if strings.TrimSpace(raw) == "" || isGenericPublicFact(raw) {
		return "", "no_safe_hook"
	}
	if looksLikeMetadataDump(raw) {
		condensed := condenseMetadataFact(raw)
		if condensed == "" || looksLikeMetadataDump(condensed) {
			return "", "metadata_dump"
		}
		return condensed, ""
	}
	if looksMalformedCurrency(raw) && !hasConcreteContractEvent(strings.ToLower(raw)) {
		// Currency dump without a contract event is not a hook.
		condensed := condenseMetadataFact(raw)
		if condensed == "" {
			return "", "malformed_currency"
		}
		return condensed, ""
	}
	return sanitizeHookProse(raw), ""
}

func sanitizeHookProse(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, ".;")
	s = emDashRe.ReplaceAllString(s, ",")
	// Collapse internal whitespace.
	return strings.Join(strings.Fields(s), " ")
}

var (
	metadataLabelRe  = regexp.MustCompile(`(?i)\b(objeto|órgão|orgao|uf|cnpj|modalidade|valor|número|numero|processo|instrumento)\s*:`)
	usCurrencyRe     = regexp.MustCompile(`(?i)r\$\s*\d{1,3}(?:,\d{3})+(?:\.\d+)?`)
	brCurrencyOkRe   = regexp.MustCompile(`(?i)r\$\s*\d{1,3}(?:\.\d{3})+(?:,\d{2})?`)
	semicolonFieldRe = regexp.MustCompile(`(?i)(?:objeto|órgão|orgao|uf|cnpj|modalidade|valor|r\$)[^;]{0,80};`)
)

func looksLikeMetadataDump(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	labels := metadataLabelRe.FindAllString(s, -1)
	if len(labels) >= 2 {
		return true
	}
	if semicolonFieldRe.MatchString(s) && strings.Count(s, ";") >= 2 {
		return true
	}
	// Near-raw: many key: value pairs.
	if strings.Count(s, ":") >= 3 && strings.Count(s, ";") >= 1 {
		return true
	}
	return false
}

func looksMalformedCurrency(s string) bool {
	if usCurrencyRe.MatchString(s) && !brCurrencyOkRe.MatchString(s) {
		return true
	}
	return false
}

func condenseMetadataFact(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Prefer the objeto / subject field when present.
	if obj := extractLabeledField(raw, "objeto"); obj != "" {
		obj = stripTruncation(obj)
		obj = strings.TrimSpace(obj)
		if utf8.RuneCountInString(obj) > 160 {
			obj = string([]rune(obj)[:157]) + "..."
		}
		if obj != "" {
			return "contratação pública: " + ensureLowerStart(obj)
		}
	}
	// Fall back to a short natural clause without labels.
	cleaned := metadataLabelRe.ReplaceAllString(raw, " ")
	cleaned = strings.ReplaceAll(cleaned, ";", " ")
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if utf8.RuneCountInString(cleaned) > 160 {
		cleaned = string([]rune(cleaned)[:157]) + "..."
	}
	if looksLikeMetadataDump(cleaned) || countWords(cleaned) < 4 {
		return ""
	}
	return sanitizeHookProse(cleaned)
}

func extractLabeledField(raw, label string) string {
	low := strings.ToLower(raw)
	idx := strings.Index(low, strings.ToLower(label)+":")
	if idx < 0 {
		return ""
	}
	rest := raw[idx+len(label)+1:]
	if i := strings.Index(rest, ";"); i >= 0 {
		rest = rest[:i]
	}
	return strings.TrimSpace(rest)
}

func stripTruncation(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "(...)")
	s = strings.TrimSuffix(s, "...")
	return strings.TrimSpace(s)
}

func hasConcreteContractEvent(blob string) bool {
	for _, ev := range []string{
		"aditivo",
		"medicao",
		"medição",
		"reajuste",
		"anualidade",
		"aniversario",
		"aniversário",
		"ordem de servico",
		"ordem de serviço",
		"inicio de execucao",
		"início de execução",
		"marco contratual",
		"prorrogacao",
		"prorrogação",
		"reequilibrio",
		"reequilíbrio",
		"encerramento",
		"termino",
		"término",
		"vigencia encerra",
		"vigência encerra",
		"alteracao de escopo",
		"alteração de escopo",
		"reajuste anual",
		"close-out",
		"closeout",
		"glosa",
		"extracontratual",
		"termo aditivo",
	} {
		if containsMentionToken(blob, ev) {
			return true
		}
	}
	return false
}

// containsMentionToken matches mentionable evidence tokens. Short/ambiguous
// tokens (extra, marco) must be whole words so "março" / "extraordinário"
// cannot mint a contract event.
func containsMentionToken(blob, tok string) bool {
	blob = foldASCII(blob)
	tok = foldASCII(strings.TrimSpace(tok))
	if tok == "" || blob == "" {
		return false
	}
	if strings.Contains(tok, " ") || strings.Contains(tok, "-") {
		return strings.Contains(blob, tok)
	}
	if utf8.RuneCountInString(tok) <= 5 {
		return containsWholeWord(blob, tok)
	}
	return strings.Contains(blob, tok)
}

func containsWholeWord(blob, word string) bool {
	if word == "" {
		return false
	}
	for start := 0; start <= len(blob); {
		idx := strings.Index(blob[start:], word)
		if idx < 0 {
			return false
		}
		i := start + idx
		leftOK := i == 0 || !unicode.IsLetter(rune(blob[i-1]))
		right := i + len(word)
		rightOK := right >= len(blob) || !unicode.IsLetter(rune(blob[right]))
		if leftOK && rightOK {
			return true
		}
		start = i + 1
	}
	return false
}

// serviceHookFits reports whether the public fact is coherent with the service.
// A non-empty fact is not enough: REAJUSTE + edital is not READY.
func serviceHookFits(serviceCode, blob string) bool {
	if strings.TrimSpace(blob) == "" {
		return false
	}
	tokens := serviceHookTokens(serviceCode)
	if len(tokens) == 0 {
		return false
	}
	for _, tok := range tokens {
		if tok != "" && containsMentionToken(blob, tok) {
			return true
		}
	}
	return false
}

func serviceHookTokens(serviceCode string) []string {
	switch strings.ToUpper(strings.TrimSpace(serviceCode)) {
	case "REAJUSTE":
		return []string{"reajuste", "anualidade", "aniversario", "aniversário"}
	case "REEQUILIBRIO":
		return []string{"reequilibrio", "reequilíbrio", "desequilibrio", "desequilíbrio"}
	case "MEDICOES":
		return []string{"medicao", "medição", "glosa"}
	case "ADITIVOS":
		return []string{"aditivo", "prorrogacao", "prorrogação", "prorrogado"}
	case "EXTRACONTRATUAIS":
		return []string{"extra", "extracontratual", "ordem de servico", "ordem de serviço"}
	case "PLANILHAS":
		return []string{"planilha", "quantitativo", "quantitativos"}
	case "ENCERRAMENTO_CONTRATUAL":
		return []string{"encerramento", "vigencia encerra", "vigência encerra", "termino", "término", "close-out", "closeout"}
	case "APOIO_LICITACAO":
		return []string{"edital", "licitacao", "licitação", "proposta"}
	case "MONITORAMENTO_CONTRATUAL":
		return []string{"aditivo", "medicao", "medição", "reajuste", "prorrogacao", "prorrogação", "encerramento", "ordem de servico", "ordem de serviço"}
	case "DIAGNOSTICO":
		return []string{"prorrogacao", "prorrogação", "prorrogado", "aditivo", "medicao", "medição", "reajuste", "encerramento"}
	case "INTELIGENCIA_PNCP":
		return []string{"recorte", "orgao", "órgão", "orgaos", "órgãos", "mercado", "pncp"}
	case "BACKOFFICE":
		return []string{"pico", "volume", "carga", "medicoes", "medições"}
	default:
		return nil
	}
}

func hookLeadForService(serviceCode string) string {
	switch strings.ToUpper(strings.TrimSpace(serviceCode)) {
	case "APOIO_LICITACAO":
		return "Pelo edital publicado"
	case "INTELIGENCIA_PNCP":
		return "Pelo recorte público do PNCP"
	case "DIAGNOSTICO", "BACKOFFICE":
		return "Pelo que está publicado"
	default:
		return "Pelo contrato publicado"
	}
}

// ContractFramedOpener is the default first-touch lead for contract-family services.
// Non-contract services must not use this phrase.
const ContractFramedOpener = "pelo contrato publicado"

func serviceUsesContractOpener(serviceCode string) bool {
	switch strings.ToUpper(strings.TrimSpace(serviceCode)) {
	case "APOIO_LICITACAO", "INTELIGENCIA_PNCP", "DIAGNOSTICO", "BACKOFFICE":
		return false
	default:
		return true
	}
}

func foldASCII(s string) string {
	s = strings.ToLower(s)
	repl := strings.NewReplacer(
		"á", "a", "à", "a", "ã", "a", "â", "a",
		"é", "e", "ê", "e",
		"í", "i",
		"ó", "o", "ô", "o", "õ", "o",
		"ú", "u",
		"ç", "c",
	)
	return repl.Replace(s)
}

func operatorReasonForHook(code, service string) string {
	switch code {
	case "metadata_dump":
		return "O fato público chegou como metadado serializado, não como gancho comercial."
	case "malformed_currency":
		return "O valor público está em formato que parece dump, não linguagem natural."
	case "no_safe_hook":
		if strings.EqualFold(service, "MONITORAMENTO_CONTRATUAL") {
			return "Existe contrato público, mas nenhum evento contratual específico sustenta ainda uma primeira abordagem."
		}
		return "Não há fato público específico o bastante para um primeiro contato."
	default:
		return ""
	}
}

func outboundValueUnit(serviceCode, offerCode string, pb *Playbook) string {
	if pb != nil && offerCode != "" {
		if o := pb.FindOffer(offerCode); o != nil {
			if v := strings.TrimSpace(o.BuyerValue); v != "" && !isAbstractValueUnit(v) {
				// Offer buyer_value is operator-facing; map to an outbound unit.
			}
		}
	}
	switch strings.ToUpper(serviceCode) {
	case "REAJUSTE":
		return "um checklist dos documentos e memórias que eu conferiria neste ciclo de reajuste"
	case "REEQUILIBRIO":
		return "o que eu checaria antes de qualquer pedido de reequilíbrio"
	case "MEDICOES":
		return "os pontos de critério e memória que eu validaria nessa medição"
	case "ADITIVOS":
		return "o que eu checaria depois desse aditivo (planilha, prazos e efeitos)"
	case "EXTRACONTRATUAIS":
		return "a lista de documentos que eu reuniria para enquadrar o extra"
	case "PLANILHAS":
		return "os pontos de quantitativo que eu conferiria na planilha publicada"
	case "ENCERRAMENTO_CONTRATUAL":
		return "o checklist do que ainda cabe regularizar antes do encerramento"
	case "APOIO_LICITACAO":
		return "o snapshot do que eu verificaria no edital ou instrumento"
	case "MONITORAMENTO_CONTRATUAL":
		return "o recorte do evento contratual específico que vale acompanhar agora"
	case "DIAGNOSTICO":
		return "o checklist diagnóstico do que eu conferiria primeiro no que está publicado"
	case "INTELIGENCIA_PNCP":
		return "o briefing priorizado do recorte público de mercado"
	case "BACKOFFICE":
		return "o escopo mínimo que eu validaria antes de qualquer reforço pontual"
	default:
		return ""
	}
}

func outboundCheckPoints(serviceCode, blob string, hasEvent bool) []string {
	switch strings.ToUpper(serviceCode) {
	case "REAJUSTE":
		return []string{
			"índice e cláusula aplicáveis ao ciclo",
			"memória de cálculo do período",
			"base de reajuste versus o que está publicado",
		}
	case "REEQUILIBRIO":
		return []string{
			"nexo do evento com o instrumento",
			"memória de quantificação ainda organizada",
			"o que falta antes de um pedido formal",
		}
	case "MEDICOES":
		return []string{
			"critério de medição no trecho publicado",
			"alinhamento entre campo e planilha",
			"memória que costuma nascer a divergência",
		}
	case "ADITIVOS":
		return []string{
			"o que o aditivo alterou na planilha ou no prazo",
			"efeito em medição ou reajuste derivado",
			"o que deixou de ser revisado depois da assinatura",
		}
	case "EXTRACONTRATUAIS":
		return []string{
			"ordem ou evidência contemporânea do extra",
			"enquadramento no instrumento",
			"documentos que ainda faltam reunir",
		}
	case "PLANILHAS":
		return []string{
			"quantitativos versus execução publicada",
			"base usada em reajuste ou medição",
			"itens que costumam ficar desalinhados",
		}
	case "ENCERRAMENTO_CONTRATUAL":
		return []string{
			"pendências de medição ou reajuste perto do término",
			"documentos de close-out ainda em aberto",
			"o que ainda cabe regularizar nesta janela",
		}
	case "APOIO_LICITACAO":
		return []string{
			"premissas frágeis do edital ou instrumento",
			"quantitativos que pressionam margem",
			"inconsistências que eu verificaria primeiro",
		}
	case "MONITORAMENTO_CONTRATUAL":
		if !hasEvent {
			return nil
		}
		return []string{
			"o evento contratual publicado que mudou agora",
			"o que isso altera na execução ou na documentação",
		}
	case "DIAGNOSTICO":
		if !hasEvent && !strings.Contains(blob, "contrato") {
			return nil
		}
		return []string{
			"o trecho público que vale conferir primeiro",
			"se o material sustenta alguma especialidade",
		}
	case "INTELIGENCIA_PNCP":
		return []string{
			"órgãos ou UFs do recorte público",
			"o que eu priorizaria nesse mapa",
		}
	case "BACKOFFICE":
		if !hasEvent {
			return nil
		}
		return []string{
			"o pico de obrigação publicado",
			"o escopo mínimo de reforço pontual",
		}
	default:
		return nil
	}
}

func outboundRelevance(serviceCode, hook string, hasEvent bool) string {
	_ = hook
	switch strings.ToUpper(serviceCode) {
	case "REAJUSTE":
		return "Vale conferir documentos e memórias deste ciclo antes de qualquer pedido formal."
	case "REEQUILIBRIO":
		return "O material público pede nexo e memória antes de qualquer pedido de reequilíbrio."
	case "MEDICOES":
		return "Nessa medição, o risco costuma estar no critério e na memória, não só na planilha."
	case "ADITIVOS":
		return "Depois de um aditivo, o que costuma ficar para trás é a revisão de planilha, prazo e efeitos."
	case "EXTRACONTRATUAIS":
		return "Extra sem trilha contemporânea fica difícil de reconhecer depois."
	case "PLANILHAS":
		return "Divergência silenciosa de quantitativo vira disputa tarde demais."
	case "ENCERRAMENTO_CONTRATUAL":
		return "Perto do término, ainda dá para regularizar o que está em aberto."
	case "APOIO_LICITACAO":
		return "Risco de proposta nasce em premissa e quantitativo, não só no preço final."
	case "MONITORAMENTO_CONTRATUAL":
		if hasEvent {
			return "Esse evento publicado é o que eu olharia com prioridade agora."
		}
		return ""
	case "DIAGNOSTICO":
		return "O caminho honesto é conferir o que está publicado antes de ofertar especialidade."
	case "INTELIGENCIA_PNCP":
		return "O valor está no recorte priorizado, não na lista bruta de editais."
	case "BACKOFFICE":
		return "Se houver pico pontual, o útil é validar o escopo mínimo, não presumir falta de estrutura."
	default:
		return ""
	}
}

func outboundCTA(st OutreachStrategy, pb *Playbook, svc *ServicePlaybook) string {
	if pb != nil && st.MicroOfferCode != "" {
		if o := pb.FindOffer(st.MicroOfferCode); o != nil && len(o.CTAPatterns) > 0 {
			cta := strings.TrimSpace(o.CTAPatterns[0])
			if cta != "" && !ctaPromisesPoints(cta) {
				return cta
			}
		}
	}
	switch svc.Code {
	case "REAJUSTE":
		return "Posso te mandar o checklist do que eu verificaria no reajuste?"
	case "ADITIVOS":
		return "Posso te mostrar o que eu checaria depois desse aditivo?"
	case "MEDICOES":
		return "Posso te mandar o que eu verificaria na medição?"
	case "ENCERRAMENTO_CONTRATUAL":
		return "Quer o checklist do que eu conferiria antes do encerramento?"
	case "APOIO_LICITACAO":
		return "Quer um snapshot do que eu verificaria no edital ou instrumento?"
	case "INTELIGENCIA_PNCP":
		return "Posso te mandar o briefing público que montei sobre o seu recorte de mercado?"
	case "BACKOFFICE":
		return "Posso te mostrar o escopo mínimo que eu validaria antes de qualquer reforço?"
	default:
		return "Posso te mandar o recorte objetivo que eu conferiria?"
	}
}

func ctaPromisesPoints(cta string) bool {
	t := foldASCII(cta)
	for _, p := range []string{
		"pontos que eu confer",
		"pontos que eu verific",
		"os pontos",
		"recorte que encontrei",
		"pontos objetivos",
	} {
		if strings.Contains(t, foldASCII(p)) {
			return true
		}
	}
	return false
}

func isAbstractValueUnit(s string) bool {
	t := foldASCII(s)
	if strings.TrimSpace(t) == "" {
		return true
	}
	abstract := []string{
		"clareza objetiva sobre um ponto contratual publico",
		"valor para o destinatario",
		"ajuda pontual",
		"leitura geral",
		"conversa diagnostica",
	}
	for _, a := range abstract {
		if t == a {
			return true
		}
	}
	return false
}

func offerEvidenceTooWeak(o *MicroOfferDef) bool {
	if o == nil {
		return true
	}
	if len(o.RequiredEvidence) == 0 {
		return true
	}
	for _, req := range o.RequiredEvidence {
		switch strings.ToLower(strings.TrimSpace(req)) {
		case "any", "public_fact", "any_public_contract_fact":
			return true
		}
	}
	return false
}

// CreditVocabAllowed reports whether the word "crédito" may appear in copy.
func CreditVocabAllowed(pb *Playbook, serviceCode string) bool {
	if pb == nil {
		return false
	}
	svc := pb.ResolveServicePlaybook(serviceCode)
	if svc == nil {
		return false
	}
	return svc.CreditVocabAllowed
}

// LooksLikeInternalReasoning is the defensive leak detector (not the architecture).
func LooksLikeInternalReasoning(s string) bool {
	t := foldASCII(s)
	for _, p := range internalReasoningPhrases {
		if p != "" && strings.Contains(t, foldASCII(p)) {
			return true
		}
	}
	return false
}

var internalReasoningPhrases = []string{
	"isso não prova crédito sozinho",
	"isso nao prova credito sozinho",
	"eventos públicos relevantes sem triagem",
	"eventos publicos relevantes sem triagem",
	"sem presumir falta de capacidade interna",
	"hipótese, não crédito comprovado",
	"hipotese, nao credito comprovado",
	"evidence requires hypothesis language",
	"momento comercial indicado pelo extra-cli",
	"como segunda leitura pontual",
	"segunda leitura / validação independente",
	"segunda leitura / validacao independente",
	"hipótese linguistically scoped",
	"hipotese linguistically scoped",
	"know a lot / say little",
	"sem inventar contexto",
	"caminho diagnóstico costuma começar",
	"caminho diagnostico costuma comecar",
}

func creditWordIn(s string) bool {
	t := foldASCII(s)
	// Word-ish match: credito / crédito, not "acreditamos".
	if strings.Contains(t, "nao prova credito") || strings.Contains(t, "prova credito") {
		return true
	}
	fields := strings.FieldsFunc(t, func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	for _, f := range fields {
		if f == "credito" || f == "creditos" {
			return true
		}
	}
	return false
}

// BuildOutboundPlan is the single strategy → messageability entry used by
// AI and template generators.
func BuildOutboundPlan(
	pb *Playbook,
	acc *models.OutreachAccount,
	cand *models.OutreachContactCandidate,
	evidence []models.OutreachEvidence,
	sequencePosition int,
) (OutreachStrategy, OutboundMessagePlan) {
	st := PlanOutreachStrategy(pb, acc, cand, evidence, sequencePosition)
	plan := EvaluateMessageability(st, acc, cand, evidence, pb)
	return st, plan
}

func draftStatusFromMessageability(out DraftOutput, val ValidationResult) string {
	if containsStr(out.RiskFlags, "messageability_blocked") {
		return models.OutreachDraftBlocked
	}
	if containsStr(out.RiskFlags, "messageability_needs_enrichment") || !val.OK || strings.TrimSpace(out.BodyText) == "" {
		return models.OutreachDraftSkipped
	}
	return models.OutreachDraftNeedsReview
}

// FailClosedDraft is the non-sendable output when the gate is not READY.
func FailClosedDraft(plan OutboundMessagePlan, channel string) DraftOutput {
	if channel == "" {
		channel = ChannelEmailInitial
	}
	flags := append([]string{"messageability_" + strings.ToLower(plan.Messageability), "fail_closed"}, plan.ReasonCodes...)
	return DraftOutput{
		Channel:     channel,
		Subject:     "",
		BodyText:    "",
		FactUsed:    "",
		EvidenceIDs: plan.EvidenceIDs,
		ServiceCode: plan.ServiceCode,
		RiskFlags:   flags,
		Rationale:   firstNonEmpty(plan.Reason, "messageability gate: not READY"),
	}
}
