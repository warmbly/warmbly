package confenge

import (
	"encoding/json"
	"strings"

	"github.com/warmbly/warmbly/internal/models"
)

// ComposeFromStrategy plans messageability then renders only from the
// outbound-safe plan. Internal strategy fields are never interpolated.
func ComposeFromStrategy(st OutreachStrategy, acc *models.OutreachAccount, cand *models.OutreachContactCandidate, channel string) DraftOutput {
	pb, _ := LoadPlaybook()
	plan := EvaluateMessageability(st, acc, cand, nil, pb)
	return ComposeFromPlan(plan, acc, cand, channel)
}

// ComposeFromPlan builds copy exclusively from outbound-safe plan fields.
// If the plan is not READY, it fail-closes with no sendable body.
func ComposeFromPlan(plan OutboundMessagePlan, acc *models.OutreachAccount, cand *models.OutreachContactCandidate, channel string) DraftOutput {
	if channel == "" {
		channel = ChannelEmailInitial
	}
	if plan.SequencePosition >= 2 && (channel == "" || channel == ChannelEmailInitial) {
		channel = ChannelEmailFollowup
	}
	if plan.Messageability != MessageabilityReady {
		return FailClosedDraft(plan, channel)
	}

	greeting := planGreeting(cand)
	hook := strings.TrimSpace(plan.Hook)
	relevance := strings.TrimSpace(plan.Relevance)
	cta := strings.TrimSpace(plan.CTA)
	if cta == "" {
		cta = "Posso te mandar o recorte objetivo que eu conferiria?"
	}

	company := ""
	if acc != nil {
		company = firstNonEmpty(acc.NomeFantasia, acc.RazaoSocial)
	}

	var body string
	switch {
	case channel == ChannelEmailFollowup || sequenceFromChannel(channel) >= 2:
		body = composeFollowup(greeting, hook, relevance, cta, plan, channel)
	case channel == ChannelReplyDraft:
		body = greeting + ",\n\nObrigado pela resposta. "
		if hook != "" {
			body += "Sobre " + ensureLowerStart(hook) + ", "
		}
		if relevance != "" {
			body += ensureLowerStart(relevance) + " "
		}
		body += cta
	case IsWhatsAppChannel(channel):
		body = composeWhatsApp(greeting, hook, cta)
	default:
		body = greeting + ",\n\n"
		if hook != "" {
			lead := strings.TrimSpace(plan.HookLead)
			if lead == "" {
				lead = hookLeadForService(plan.ServiceCode)
			}
			body += lead + ", " + ensureLowerStart(hook)
			if !strings.HasSuffix(strings.TrimSpace(hook), ".") {
				body += "."
			}
			body += "\n\n"
		}
		if relevance != "" {
			body += relevance
			if !strings.HasSuffix(strings.TrimSpace(relevance), ".") {
				body += "."
			}
			body += "\n\n"
		}
		body += cta
	}

	body = emDashRe.ReplaceAllString(body, ",")
	subj := planSubject(plan, company, hook, channel)

	return DraftOutput{
		Channel:     channel,
		Subject:     subj,
		BodyText:    body,
		FactUsed:    hook,
		EvidenceIDs: plan.EvidenceIDs,
		Claims:      claimsFromFact(hook, plan.EvidenceIDs),
		ServiceCode: plan.ServiceCode,
		Question:    cta,
		CTA:         cta,
		RiskFlags:   []string{"strategy_compose", "messageability_ready"},
		Rationale:   "composed from OutboundMessagePlan " + plan.ComposerVersion + " service=" + plan.ServiceCode,
	}
}

func planGreeting(cand *models.OutreachContactCandidate) string {
	if isGenericRecipient(cand) {
		return "Olá, equipe"
	}
	if cand != nil {
		if name := strings.TrimSpace(cand.Name); name != "" {
			return "Olá, " + firstName(name)
		}
	}
	return "Olá"
}

func sequenceFromChannel(channel string) int {
	if channel == ChannelEmailFollowup {
		return 2
	}
	return 1
}

func composeFollowup(greeting, hook, relevance, cta string, plan OutboundMessagePlan, channel string) string {
	_ = channel
	if plan.SequencePosition >= 5 {
		return greeting + ",\n\nParece que não é prioridade agora; encerro por aqui para não ocupar sua caixa.\n\nSe fizer sentido no futuro, é só responder este fio."
	}
	if plan.SequencePosition == 4 {
		return greeting + ",\n\nSobre " + firstNonEmpty(ensureLowerStart(hook), "este tema") + ", quem na equipe acompanha isso no dia a dia?\n\nSe não for você, um encaminhamento curto já ajuda."
	}
	body := greeting + ",\n\n"
	switch {
	case len(plan.CheckPoints) > 0 && strings.Contains(strings.ToLower(cta+relevance), "checklist"):
		body += "Se for útil, um mini-checklist a partir do que está publicado:\n"
		for i, p := range plan.CheckPoints {
			if i >= 3 {
				break
			}
			body += itoa(i+1) + ") " + p + ";\n"
		}
		body += "\n" + cta
	case hook != "":
		body += "Outro ângulo sobre " + ensureLowerStart(hook)
		if !strings.HasSuffix(strings.TrimSpace(hook), ".") {
			body += "."
		}
		body += " "
		if relevance != "" {
			body += relevance
			if !strings.HasSuffix(strings.TrimSpace(relevance), ".") {
				body += "."
			}
		}
		body += "\n\n" + cta
	default:
		body += cta
	}
	return body
}

func composeWhatsApp(greeting, hook, cta string) string {
	body := greeting + ", "
	if hook != "" {
		body += ensureLowerStart(hook)
		if !strings.HasSuffix(strings.TrimSpace(hook), ".") {
			body += "."
		}
		body += " "
	}
	body += cta
	return body
}

func canonicalServiceForSubject(code string) string {
	if pb, _ := LoadPlaybook(); pb != nil {
		if s := pb.ResolveServicePlaybook(code); s != nil {
			return s.Code
		}
	}
	return strings.ToUpper(strings.TrimSpace(code))
}

func planSubject(plan OutboundMessagePlan, company, hook, channel string) string {
	if IsWhatsAppChannel(channel) {
		return ""
	}
	if channel == ChannelEmailFollowup || channel == ChannelReplyDraft {
		if company != "" {
			return trimSubject("Re: " + company)
		}
		return "Re: conversa anterior"
	}
	switch strings.ToUpper(canonicalServiceForSubject(plan.ServiceCode)) {
	case "REAJUSTE":
		return "Reajuste contratual"
	case "MEDICOES":
		return trimSubject("Medições " + firstNonEmpty(company, ""))
	case "ADITIVOS":
		return trimSubject("Aditivo " + firstNonEmpty(company, ""))
	case "ENCERRAMENTO_CONTRATUAL":
		return "Encerramento contratual"
	case "APOIO_LICITACAO":
		return trimSubject("Edital " + firstNonEmpty(company, ""))
	case "INTELIGENCIA_PNCP":
		return trimSubject("Recorte PNCP " + firstNonEmpty(company, ""))
	}
	if hook != "" && hasConcreteToken(hook) {
		return trimSubject(firstWords(hook, 4))
	}
	if company != "" {
		return trimSubject("Contrato " + company)
	}
	return "Contrato público"
}

func ensureLowerStart(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	r := []rune(s)
	// Avoid double-capital awkwardness mid-sentence; keep if looks like proper noun acronym
	if len(r) > 0 && r[0] >= 'A' && r[0] <= 'Z' {
		if len(r) > 1 && r[1] >= 'a' && r[1] <= 'z' {
			r[0] = rune(strings.ToLower(string(r[0]))[0])
		}
	}
	return string(r)
}

// draftSystemPromptDoctrine extends the system prompt with doctrine constraints.
// The model receives the outbound-safe plan, never internal strategy fields.
func draftSystemPromptDoctrine(channel string, plan OutboundMessagePlan) string {
	base := draftSystemPrompt(channel)
	base += "\n\nDOUTRINA " + OutreachDoctrineVersion + " / " + ComposerVersion + ":\n"
	base += "- Gere copy SOMENTE a partir do PLANO DE MENSAGEM (hook_lead, hook, relevance, value_unit, cta).\n"
	base += "- Use hook_lead como abertura; não troque por 'Pelo contrato publicado' se o plano trouxe outro lead.\n"
	base += "- NÃO use hipóteses internas, scores, metadados crus ou raciocínio de playbook.\n"
	base += "- NÃO escreva 'crédito' salvo se o serviço do plano autorizar explicitamente.\n"
	base += "- NÃO despeje campos como objeto:/órgão:/UF:/CNPJ:.\n"
	base += "- Primeiro email: micro-oferta / permissão; NÃO peça reunião por padrão.\n"
	base += "- Sem pitch institucional, sem lista de serviços, sem mini-CV.\n"
	base += "- CTA alinhado ao plano: " + plan.CTA + "\n"
	if plan.RecipientMode == RecipientModeGenericInbox {
		base += "- DESTINATÁRIO GENÉRICO: trate como equipe; nunca use nome, cargo ou saudação pessoal.\n"
	}
	return base
}

// draftUserPromptWithPlan feeds the model only outbound-safe plan fields plus
// identity needed for greeting. It must not include fact_to_mention dumps,
// offer rationale, or internal hypotheses.
func draftUserPromptWithPlan(in GenerateInput, plan OutboundMessagePlan) string {
	channel := in.Channel
	if channel == "" {
		channel = ChannelEmailInitial
	}
	identity := map[string]any{
		"channel":        channel,
		"recipient_mode": plan.RecipientMode,
	}
	if in.Account != nil {
		identity["company"] = firstNonEmpty(in.Account.NomeFantasia, in.Account.RazaoSocial)
	}
	if plan.RecipientMode == RecipientModeNamed && in.Contact != nil {
		if name := strings.TrimSpace(in.Contact.Name); name != "" && !isGenericRecipient(in.Contact) {
			identity["first_name"] = firstName(name)
		}
	}
	if in.PriorSubject != "" {
		identity["prior_subject"] = in.PriorSubject
	}
	if in.PriorBody != "" {
		identity["prior_body"] = in.PriorBody
	}
	if in.InboundReply != "" {
		identity["inbound_reply"] = in.InboundReply
	}
	safe := struct {
		Messageability string         `json:"messageability"`
		RecipientMode  string         `json:"recipient_mode"`
		ServiceCode    string         `json:"service_code"`
		HookLead       string         `json:"hook_lead"`
		Hook           string         `json:"hook"`
		Relevance      string         `json:"relevance"`
		ValueUnit      string         `json:"value_unit"`
		CTA            string         `json:"cta"`
		EvidenceIDs    []string       `json:"evidence_ids"`
		CheckPoints    []string       `json:"check_points"`
		Identity       map[string]any `json:"identity"`
	}{
		Messageability: plan.Messageability,
		RecipientMode:  plan.RecipientMode,
		ServiceCode:    plan.ServiceCode,
		HookLead:       plan.HookLead,
		Hook:           plan.Hook,
		Relevance:      plan.Relevance,
		ValueUnit:      plan.ValueUnit,
		CTA:            plan.CTA,
		EvidenceIDs:    plan.EvidenceIDs,
		CheckPoints:    plan.CheckPoints,
		Identity:       identity,
	}
	b, _ := json.MarshalIndent(safe, "", "  ")
	return "PLANO DE MENSAGEM (único material autorizado na copy; não interpolar raciocínio interno, dumps ou hipóteses):\n" + string(b)
}

// StrategyCodeFor stamps doctrine + offer + CTA for audit.
func StrategyCodeFor(st OutreachStrategy) string {
	parts := []string{st.DoctrineVersion, st.MicroOfferCode, st.CTAType}
	if st.SequenceTouchName != "" {
		parts = append(parts, st.SequenceTouchName)
	}
	return strings.Join(parts, "/")
}

// PackValidationWithStrategy marshals validation including strategy explain for UI.
func PackValidationWithStrategy(val ValidationResult, st OutreachStrategy, recipient string) []byte {
	return PackValidationWithPlan(val, st, OutboundMessagePlan{}, recipient)
}

// PackValidationWithPlan includes the outbound-safe plan for the operator cockpit.
func PackValidationWithPlan(val ValidationResult, st OutreachStrategy, plan OutboundMessagePlan, recipient string) []byte {
	val.DoctrineVersion = st.DoctrineVersion
	val.Strategy = &st
	ex := ExplainStrategy(st, recipient)
	if plan.Messageability != "" {
		ex.Messageability = plan.Messageability
		ex.MessageabilityReason = plan.Reason
		val.Messageability = plan.Messageability
		val.MessageabilityReason = plan.Reason
		cp := plan
		val.MessagePlan = &cp
	}
	val.StrategyExplain = &ex
	b, _ := json.Marshal(val)
	return b
}
