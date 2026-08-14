package confenge

import (
	"strings"

	"github.com/warmbly/warmbly/internal/models"
)

// CommercialActionContent is channel-appropriate copy. It is not a
// subject-less email: calls get a short script, role mailboxes address a
// function, WhatsApp/social/form stay short and human-executed.
type CommercialActionContent struct {
	Kind             string   `json:"kind"`
	Subject          string   `json:"subject,omitempty"`
	Body             string   `json:"body,omitempty"`
	CTA              string   `json:"cta,omitempty"`
	Opening          string   `json:"opening,omitempty"`
	ReasonForCall    string   `json:"reason_for_call,omitempty"`
	ValueProposition string   `json:"value_proposition,omitempty"`
	Ask              string   `json:"ask,omitempty"`
	ObjectionNotes   string   `json:"objection_notes,omitempty"`
	DoNotClaim       []string `json:"do_not_claim,omitempty"`
	PersonID         string   `json:"person_id,omitempty"`
}

// ComposeActionContent builds executable copy from the planned action.
func ComposeActionContent(a models.OutreachCommercialAction) CommercialActionContent {
	company := firstNonEmpty(a.CompanyName, "a empresa")
	hook := strings.TrimSpace(a.FactualHook)
	offer := firstNonEmpty(a.ServiceContext, a.ServiceCode)
	ask := firstNonEmpty(a.RecommendedAction, "Posso enviar um recorte objetivo?")
	var c CommercialActionContent
	switch a.ActionType {
	case models.ActionDirectEmail:
		c = composeDirectEmail(a, company, hook, offer)
	case models.ActionInferredEmailReview:
		c = composeDirectEmail(a, company, hook, offer)
		c.Kind = "INFERRED_EMAIL"
		c.DoNotClaim = append(c.DoNotClaim, "Nao tratar este endereco como e-mail validado para envio.")
	case models.ActionRoleEmail:
		c = composeRoleEmail(a, company, hook, offer)
	case models.ActionGenericEmail, models.ActionOtherManual:
		c = CommercialActionContent{
			Kind: "MANUAL",
			Body: "Nao abordar como pessoa. Se for o caso, use o canal geral sem fingir destinatario nominal.",
			DoNotClaim: []string{
				"Nao inventar um nome.",
				"Nao tratar contato generico como destinatario pessoal.",
			},
		}
	case models.ActionDirectCall:
		c = composeCall(a, company, hook, offer, ask, false)
	case models.ActionRoutedCall:
		c = composeCall(a, company, hook, offer, ask, true)
	case models.ActionWhatsApp:
		c = composeWhatsAppAction(a, hook)
	case models.ActionProfessionalSocial:
		c = composeSocial(a, company, hook)
	case models.ActionContactForm:
		c = composeForm(a, company, hook, offer)
	default:
		c = CommercialActionContent{Kind: "MANUAL", Body: strings.TrimSpace(a.RecommendedAction)}
	}
	c.PersonID = firstNonEmpty(a.PersonID, c.PersonID)
	if len(a.Warnings) > 0 {
		for _, w := range a.Warnings {
			if strings.Contains(strings.ToLower(w), "nao alegar") || strings.Contains(strings.ToLower(w), "não alegar") {
				c.DoNotClaim = appendUnique(c.DoNotClaim, w)
			}
		}
	}
	return c
}

func composeDirectEmail(a models.OutreachCommercialAction, company, hook, offer string) CommercialActionContent {
	name := givenName(a.PersonName)
	greet := "Ola"
	if name != "" {
		greet = "Ola, " + name
	}
	body := greet + "."
	if hook != "" {
		body += " Pelo que esta publico, " + hook + "."
	}
	cta := firstNonEmpty(a.RecommendedAction, "Posso te mandar o recorte do que eu conferiria?")
	body += " " + cta
	subj := ""
	if hook != "" {
		subj = "Sobre " + clipRunes(hook, 80)
	} else if offer != "" {
		subj = offer + ": " + company
	}
	return CommercialActionContent{
		Kind:    "EMAIL",
		Subject: subj,
		Body:    strings.TrimSpace(body),
		CTA:     cta,
	}
}

func composeRoleEmail(a models.OutreachCommercialAction, company, hook, offer string) CommercialActionContent {
	team := firstNonEmpty(a.TargetRole, "a equipe responsavel")
	body := "Ola. Escrevo para " + team + " de " + company + "."
	if hook != "" {
		body += " Pelo que esta publico, " + hook + "."
	}
	cta := "Posso enviar um recorte objetivo para a equipe?"
	body += " " + cta
	return CommercialActionContent{
		Kind:       "ROLE_EMAIL",
		Subject:    firstNonEmpty(offer, "Contrato publicado") + ": " + company,
		Body:       body,
		CTA:        cta,
		DoNotClaim: []string{"Nao tratar esta caixa como o e-mail pessoal de uma pessoa inventada."},
	}
}

func composeCall(a models.OutreachCommercialAction, company, hook, offer string, ask string, routed bool) CommercialActionContent {
	person := firstNonEmpty(a.PersonName, firstNonEmpty(a.TargetRole, "quem trata do contrato"))
	opening := "Ola, aqui e da CONFENGE. Eu liguei para falar com " + person + "."
	reason := "Vi um fato publico de " + company
	if hook != "" {
		reason += ": " + hook
	}
	reason += "."
	value := "Consigo mandar um recorte curto"
	if offer != "" {
		value += " sobre " + offer
	}
	value += " para a pessoa certa."
	callAsk := "Consegue me passar para " + person + "?"
	if !routed && a.PersonName != "" {
		opening = "Ola, " + givenName(a.PersonName) + ". Aqui e da CONFENGE."
		callAsk = firstNonEmpty(ask, "Faz sentido um recorte de um minuto?")
	}
	doNot := []string{}
	if routed {
		doNot = append(doNot,
			"Nao alegar que este telefone pertence diretamente a "+person+".",
			"Este e o telefone oficial da empresa (switchboard), nao o ramal pessoal.",
		)
	}
	return CommercialActionContent{
		Kind:             "CALL",
		Opening:          opening,
		ReasonForCall:    reason,
		ValueProposition: value,
		Ask:              callAsk,
		ObjectionNotes:   "Se a recepcao pedir assunto: contrato publico / " + firstNonEmpty(a.ServiceCode, "conferencia contratual") + ".",
		DoNotClaim:       doNot,
	}
}

func composeWhatsAppAction(a models.OutreachCommercialAction, hook string) CommercialActionContent {
	name := givenName(a.PersonName)
	greet := "Ola"
	if name != "" {
		greet = "Ola, " + name
	}
	body := greet + ". Posso te mandar um recorte curto do que eu conferiria?"
	if hook != "" {
		body = greet + ". Pelo que esta publico, " + hook + ". Posso te mandar o recorte?"
	}
	return CommercialActionContent{
		Kind: "WHATSAPP",
		Body: body,
		Ask:  "Posso te mandar o recorte?",
		DoNotClaim: []string{
			"Nao enviar automaticamente.",
			"So executar se o numero publicado tiver consentimento.",
		},
	}
}

func composeSocial(a models.OutreachCommercialAction, company, hook string) CommercialActionContent {
	name := givenName(a.PersonName)
	body := "Ola"
	if name != "" {
		body = "Ola, " + name
	}
	body += ". Vi um fato publico de " + company
	if hook != "" {
		body += " (" + clipRunes(hook, 90) + ")"
	}
	body += ". Posso te mandar um recorte de uma pagina?"
	return CommercialActionContent{
		Kind: "PROFESSIONAL_SOCIAL",
		Body: body,
		Ask:  "Posso te mandar um recorte de uma pagina?",
		DoNotClaim: []string{
			"Nao automatizar scraping nem envio.",
			"Nao fingir relacao previa.",
		},
	}
}

func composeForm(a models.OutreachCommercialAction, company, hook, offer string) CommercialActionContent {
	body := "Escrevo para a equipe de " + company + "."
	if hook != "" {
		body += " Pelo que esta publico, " + hook + "."
	}
	if offer != "" {
		body += " Gostaria de enviar um recorte sobre " + offer + "."
	}
	return CommercialActionContent{
		Kind: "CONTACT_FORM",
		Body: body,
		Ask:  "Podem encaminhar ao time de contratos?",
		DoNotClaim: []string{
			"Nao submeter automaticamente.",
			"Nao inventar destinatario nominal no formulario.",
		},
	}
}

func givenName(full string) string {
	full = strings.TrimSpace(full)
	if full == "" {
		return ""
	}
	return strings.Split(full, " ")[0]
}

func clipRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "..."
}
