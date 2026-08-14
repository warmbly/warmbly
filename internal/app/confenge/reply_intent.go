package confenge

import (
	"regexp"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/app/replyclassify"
)

const (
	IntentPositiveInterest = "POSITIVE_INTEREST"
	IntentReferral         = "REFERRAL_TO_OTHER_PERSON"
	IntentQuestion         = "QUESTION"
	IntentObjection        = "OBJECTION"
	IntentNotNow           = "NOT_NOW"
	IntentNegative         = "NEGATIVE"
	IntentDoNotContact     = "DO_NOT_CONTACT"
	IntentOutOfOffice      = "OUT_OF_OFFICE"
	IntentUnknown          = "UNKNOWN"

	// Fine-grained commercial reply classes (doctrine mapping; next strategy, not auto-send).
	ReplyClassPositiveInterest     = "POSITIVE_INTEREST"
	ReplyClassOfferAccepted        = "OFFER_ACCEPTED"
	ReplyClassSendMoreInfo         = "SEND_MORE_INFO"
	ReplyClassWrongPerson          = "WRONG_PERSON"
	ReplyClassReferral             = "REFERRAL"
	ReplyClassAlreadyHandled       = "ALREADY_HANDLED_INTERNAL"
	ReplyClassAlreadyHasConsultant = "ALREADY_HAS_CONSULTANT"
	ReplyClassNotAPriority         = "NOT_A_PRIORITY"
	ReplyClassNotInterested        = "NOT_INTERESTED"
	ReplyClassContactLater         = "CONTACT_LATER"
	ReplyClassPriceQuestion        = "PRICE_QUESTION"
	ReplyClassCredibilityQuestion  = "CREDIBILITY_QUESTION"
	ReplyClassTechnicalQuestion    = "TECHNICAL_QUESTION"
	ReplyClassProcurementConcern   = "PROCUREMENT_CONCERN"
	ReplyClassDNC                  = "DNC"
	ReplyClassOutOfOffice          = "OUT_OF_OFFICE"
	ReplyClassAutoReply            = "AUTO_REPLY"
	ReplyClassOther                = "OTHER"
)

type CommercialIntent struct {
	Intent          string     `json:"intent"`
	Confidence      float64    `json:"confidence"`
	Source          string     `json:"source"`
	OOOReturnDate   *time.Time `json:"ooo_return_date,omitempty"`
	ReferralHint    string     `json:"referral_hint,omitempty"`
	SuggestedAction string     `json:"suggested_action"`
}

func ClassifyCommercialIntent(subject, body, preClass string, headers map[string][]string) CommercialIntent {
	text := strings.ToLower(strings.TrimSpace(subject + "\n" + body))
	if DetectTextualDNC(subject, body) {
		return CommercialIntent{Intent: IntentDoNotContact, Confidence: 0.95, Source: "lexicon", SuggestedAction: SuggestNextAction(IntentDoNotContact, nil, "")}
	}
	if r, ok := classifyViaHeaders(headers, subject, body); ok {
		intent := MapReplyClassToIntent(r.Class)
		var ooo *time.Time
		if intent == IntentOutOfOffice {
			ooo = ExtractOOODate(subject + "\n" + body)
		}
		return CommercialIntent{Intent: intent, Confidence: r.Confidence, Source: "header", OOOReturnDate: ooo, SuggestedAction: SuggestNextAction(intent, ooo, "")}
	}
	if intent, conf, hint := classifyCommercialLexicon(text); intent != "" {
		return CommercialIntent{Intent: intent, Confidence: conf, Source: "lexicon", ReferralHint: hint, SuggestedAction: SuggestNextAction(intent, nil, hint)}
	}
	if preClass != "" {
		intent := MapReplyClassToIntent(preClass)
		var ooo *time.Time
		if intent == IntentOutOfOffice {
			ooo = ExtractOOODate(subject + "\n" + body)
		}
		src := "class_map"
		if preClass == replyclassify.ClassUnknown || preClass == "" {
			src = "unknown"
		}
		return CommercialIntent{Intent: intent, Confidence: confidenceForMappedClass(preClass), Source: src, OOOReturnDate: ooo, SuggestedAction: SuggestNextAction(intent, ooo, "")}
	}
	return CommercialIntent{Intent: IntentUnknown, Confidence: 0, Source: "unknown", SuggestedAction: SuggestNextAction(IntentUnknown, nil, "")}
}

func MapReplyClassToIntent(class string) string {
	switch strings.ToLower(strings.TrimSpace(class)) {
	case replyclassify.ClassPositive, "positive_interest", "interested":
		return IntentPositiveInterest
	case replyclassify.ClassNegative, "no_interest":
		return IntentNegative
	case replyclassify.ClassUnsubscribe, "do_not_contact", "dnc", "opt_out":
		return IntentDoNotContact
	case replyclassify.ClassOutOfOffice, "ooo":
		return IntentOutOfOffice
	case replyclassify.ClassAutoReply, "automated_reply":
		return IntentOutOfOffice
	case replyclassify.ClassNeutral, "question":
		return IntentQuestion
	case "referral", "referral_to_other_person", "wrong_contact":
		return IntentReferral
	case "not_now", "later":
		return IntentNotNow
	case "objection":
		return IntentObjection
	case replyclassify.ClassUnknown, "":
		return IntentUnknown
	default:
		return IntentUnknown
	}
}

func DetectTextualDNC(subject, body string) bool {
	text := strings.ToLower(strings.TrimSpace(subject + "\n" + body))
	if text == "" {
		return false
	}
	for _, kw := range dncKeywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

func ExtractOOODate(text string) *time.Time {
	t := strings.TrimSpace(text)
	if t == "" {
		return nil
	}
	if m := reISODate.FindString(t); m != "" {
		if d, err := time.ParseInLocation("2006-01-02", m, time.UTC); err == nil {
			return &d
		}
	}
	if m := reBRDate.FindStringSubmatch(t); len(m) == 4 {
		if d, err := time.ParseInLocation("02/01/2006", m[1]+"/"+m[2]+"/"+m[3], time.UTC); err == nil {
			return &d
		}
	}
	return nil
}

func SuggestNextAction(intent string, oooDate *time.Time, referralHint string) string {
	switch intent {
	case IntentPositiveInterest: // same string as ReplyClassPositiveInterest
		return "Priorizar: redigir resposta consultiva e marcar follow-up humano (nao marcar Ganho)."
	case ReplyClassOfferAccepted:
		return "Cumprir micro-oferta prometida (fulfillment draft) antes de pedir reuniao; aprovacao humana."
	case ReplyClassSendMoreInfo:
		return "Enviar material especifico ao problema do fio (nao brochure generico); aprovacao humana."
	case IntentReferral, ReplyClassReferral, ReplyClassWrongPerson:
		if referralHint != "" {
			return "Atualizar destinatario para o contato indicado (" + referralHint + ") sem perder a timeline; gerar novo toque com aprovacao."
		}
		return "Pedir o contato indicado e trocar o destinatario sem perder a timeline da conta."
	case IntentQuestion, ReplyClassTechnicalQuestion, ReplyClassCredibilityQuestion:
		return "Responder a pergunta com fatos do dossie; sem inventar numeros ou prazos."
	case ReplyClassPriceQuestion:
		return "Responder faixa/escopo conforme offer/service; nao esconder preco artificialmente; aprovacao humana."
	case IntentObjection, ReplyClassAlreadyHandled, ReplyClassAlreadyHasConsultant, ReplyClassProcurementConcern:
		return "Reconhecer a objecao, posicionar complementaridade; nao discutir juridicamente nem inventar fatos."
	case IntentNotNow, ReplyClassContactLater, ReplyClassNotAPriority:
		return "Oferecer retomar em data X (toque futuro explicito, sujeito a aprovacao). Nao reabrir cadencia automaticamente."
	case IntentNegative, ReplyClassNotInterested:
		return "Registrar desinteresse, interromper cadencia. Nao insistir."
	case IntentDoNotContact, ReplyClassDNC:
		return "Bloqueio sticky DO_NOT_CONTACT: parar todos os toques e suprimir contato."
	case IntentOutOfOffice, ReplyClassAutoReply: // OUT_OF_OFFICE shared with ReplyClassOutOfOffice
		if oooDate != nil {
			return "OOO com data clara: sugerir retomar apos " + oooDate.UTC().Format("2006-01-02") + " (toque futuro com aprovacao)."
		}
		return "OOO sem data clara: nao inventar data; deixar para o operador decidir quando retomar."
	default:
		return "Classificacao manual necessaria; IA opcional desligada ou inconclusiva."
	}
}

// MapCommercialReplyClass refines intent into doctrine commercial reply classes.
// Positive reply is distinct from any human reply (e.g. DNC is reply but not success).
func MapCommercialReplyClass(intent string, subject, body string) string {
	text := strings.ToLower(strings.TrimSpace(subject + "\n" + body))
	switch intent {
	case IntentDoNotContact:
		return ReplyClassDNC
	case IntentOutOfOffice:
		return ReplyClassOutOfOffice
	case IntentReferral:
		return ReplyClassReferral
	case IntentNotNow:
		return ReplyClassContactLater
	case IntentNegative:
		return ReplyClassNotInterested
	case IntentObjection:
		if strings.Contains(text, "jurídico") || strings.Contains(text, "juridico") {
			return ReplyClassAlreadyHandled
		}
		if strings.Contains(text, "consultor") || strings.Contains(text, "já temos") || strings.Contains(text, "ja temos") {
			return ReplyClassAlreadyHasConsultant
		}
		return ReplyClassNotAPriority
	case IntentQuestion:
		if strings.Contains(text, "quanto custa") || strings.Contains(text, "preço") || strings.Contains(text, "preco") {
			return ReplyClassPriceQuestion
		}
		if strings.Contains(text, "quem é") || strings.Contains(text, "quem e") || strings.Contains(text, "confenge") {
			return ReplyClassCredibilityQuestion
		}
		return ReplyClassTechnicalQuestion
	case IntentPositiveInterest:
		for _, kw := range offerAcceptedKeywords {
			if strings.Contains(text, kw) {
				return ReplyClassOfferAccepted
			}
		}
		for _, kw := range sendMoreInfoKeywords {
			if strings.Contains(text, kw) {
				return ReplyClassSendMoreInfo
			}
		}
		return ReplyClassPositiveInterest
	default:
		if strings.Contains(text, "não sou a pessoa") || strings.Contains(text, "nao sou a pessoa") {
			return ReplyClassWrongPerson
		}
		return ReplyClassOther
	}
}

// IsPositiveCommercialReply reports success-class replies (not mere human reply).
func IsPositiveCommercialReply(class string) bool {
	switch class {
	case ReplyClassPositiveInterest, ReplyClassOfferAccepted, ReplyClassSendMoreInfo, ReplyClassReferral:
		return true
	default:
		return false
	}
}

// NextCommercialStrategy returns the playbook next step label for a reply class.
func NextCommercialStrategy(class string) string {
	return SuggestNextAction(class, nil, "")
}

func ObjectionReplyGuardrails() []string {
	return []string{"nao discutir juridicamente com o lead", "nao inventar fatos, numeros, contratos ou prazos", "nao pressionar apos objecao clara", "ancorar apenas evidencias e fatos do dossie da conta"}
}

const (
	FilterNeedsAttention   = "needs_attention"
	FilterAwaitingApproval = "awaiting_approval"
	FilterScheduled        = "scheduled"
	FilterSent             = "sent"
	FilterReplied          = "replied"
	FilterDNC              = "dnc"
)

func MapCockpitFilterToQueueState(filter string) string {
	switch strings.ToLower(strings.TrimSpace(filter)) {
	case FilterNeedsAttention, "needs-attention", "attention", FilterReplied:
		return "REPLIED"
	case FilterAwaitingApproval, "awaiting-approval", "review":
		return "NEEDS_REVIEW"
	case FilterScheduled, "enrolled":
		return "ENROLLED"
	case FilterSent:
		return "SENT"
	case FilterDNC, "do_not_contact":
		return "DO_NOT_CONTACT"
	default:
		return strings.ToUpper(strings.TrimSpace(filter))
	}
}

func confidenceForMappedClass(class string) float64 {
	switch strings.ToLower(strings.TrimSpace(class)) {
	case replyclassify.ClassUnsubscribe:
		return 0.9
	case replyclassify.ClassPositive, replyclassify.ClassNegative:
		return 0.8
	case replyclassify.ClassOutOfOffice, replyclassify.ClassAutoReply:
		return 0.85
	case replyclassify.ClassUnknown, "":
		return 0
	default:
		return 0.6
	}
}

func classifyViaHeaders(headers map[string][]string, subject, body string) (replyclassify.Result, bool) {
	r := replyclassify.Classify(replyclassify.Input{Headers: headers, Subject: subject, BodyText: body})
	if r.Source == replyclassify.SourceHeader {
		return r, true
	}
	return replyclassify.Result{}, false
}

func classifyCommercialLexicon(text string) (intent string, conf float64, referralHint string) {
	if text == "" {
		return "", 0, ""
	}
	for _, kw := range referralKeywords {
		if strings.Contains(text, kw) {
			return IntentReferral, 0.85, extractReferralHint(text)
		}
	}
	for _, kw := range notNowKeywords {
		if strings.Contains(text, kw) {
			return IntentNotNow, 0.8, ""
		}
	}
	for _, kw := range objectionKeywords {
		if strings.Contains(text, kw) {
			return IntentObjection, 0.75, ""
		}
	}
	if strings.Contains(text, "?") {
		for _, kw := range questionKeywords {
			if strings.Contains(text, kw) {
				return IntentQuestion, 0.7, ""
			}
		}
		if len([]rune(text)) >= 12 {
			return IntentQuestion, 0.55, ""
		}
	}
	for _, kw := range positiveCommercialKeywords {
		if strings.Contains(text, kw) {
			return IntentPositiveInterest, 0.8, ""
		}
	}
	for _, kw := range negativeCommercialKeywords {
		if strings.Contains(text, kw) {
			return IntentNegative, 0.8, ""
		}
	}
	return "", 0, ""
}

func extractReferralHint(text string) string {
	if m := reReferralName.FindStringSubmatch(text); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	if m := reEmail.FindString(text); m != "" {
		return m
	}
	return ""
}

var (
	reISODate      = regexp.MustCompile(`\b(20\d{2}-\d{2}-\d{2})\b`)
	reBRDate       = regexp.MustCompile(`\b(\d{1,2})[/-](\d{1,2})[/-](20\d{2})\b`)
	reReferralName = regexp.MustCompile(`(?i)(?:fale com|falar com|contact|speak (?:to|with)|procure)\s+([A-Za-zÀ-ú][A-Za-zÀ-ú\s.]{1,40})`)
	reEmail        = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
)

var dncKeywords = []string{"unsubscribe", "opt out", "opt-out", "remove me", "take me off", "stop emailing", "stop contacting", "do not contact", "don't contact", "do not email", "don't email", "please stop", "pare de me contatar", "nao me contate", "não me contate", "remover da lista", "sair da lista", "nao quero mais receber", "não quero mais receber", "cancele o envio", "stop sending", "deixe de me enviar"}
var referralKeywords = []string{"fale com", "falar com", "nao sou a pessoa", "não sou a pessoa", "pessoa certa", "wrong person", "wrong contact", "not the right person", "contact my colleague", "speak to", "speak with", "encaminho para", "encaminhar para", "outra pessoa", "meu colega", "minha colega"}
var notNowKeywords = []string{"not now", "nao agora", "não agora", "maybe later", "talvez depois", "next quarter", "proximo trimestre", "próximo trimestre", "next year", "ano que vem", "retomar em", "voltar a falar", "mais para frente", "in a few months", "daqui a alguns meses", "sem tempo agora"}
var objectionKeywords = []string{"muito caro", "too expensive", "sem orcamento", "sem orçamento", "no budget", "ja temos", "já temos", "we already have", "contrato vigente", "nao vejo valor", "não vejo valor", "risco juridico", "risco jurídico", "compliance block"}
var questionKeywords = []string{"quanto custa", "how much", "qual o prazo", "como funciona", "pode enviar", "pode detalhar", "what does", "could you", "voce pode", "você pode", "tem case", "tem material"}
var positiveCommercialKeywords = []string{"tenho interesse", "interessado", "interessada", "vamos agendar", "pode ligar", "marcamos", "bora conversar", "sounds good", "interested", "let's chat", "lets chat", "agendar uma call", "enviar proposta"}
var negativeCommercialKeywords = []string{"not interested", "sem interesse", "nao tenho interesse", "não tenho interesse", "no thanks", "no thank you", "dispenso", "nao precisamos", "não precisamos"}
var offerAcceptedKeywords = []string{"manda", "pode enviar", "pode mandar", "quero ver", "envia o checklist", "envia o recorte", "sim, pode", "sim pode", "pode sim", "send it", "please send"}
var sendMoreInfoKeywords = []string{"manda material", "mais informações", "mais informacoes", "send more", "mais detalhes", "tem material"}
