package confenge

import (
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

const (
	ContactTierA = "DIRECT_EMAIL_VALIDATED"
	ContactTierB = "NAMED_HUMAN_MANUAL_CHANNEL"
	ContactTierC = "ROLE_MAILBOX_VALIDATED"
	ContactTierD = "GENERIC_CORPORATE_CONTACT"
	ContactTierE = "EXHAUSTED"
)

const (
	LaneNeedsReviewEmail     = "NEEDS_REVIEW"
	LaneManualOutreach       = "MANUAL_OUTREACH"
	LaneRoleMailboxException = "ROLE_MAILBOX_EXCEPTION"
	LaneLowConfidenceManual  = "LOW_CONFIDENCE_MANUAL"
	LaneBlockedExhausted     = "BLOCKED"
)

const (
	ManualOpenSource           = "OPEN_SOURCE"
	ManualCopyText             = "COPY_TEXT"
	ManualMarkContacted        = "MARK_CONTACTED"
	ManualSkip                 = "SKIP"
	ManualNeedsEnrichment      = "NEEDS_ENRICHMENT"
	ManualCorrectContact       = "CORRECT_CONTACT"
	ManualPromoteAfterEvidence = "PROMOTE_AFTER_NEW_EVIDENCE"
)

type ContactClass struct {
	Tier            string `json:"contact_tier"`
	PublishedTier   string `json:"published_tier,omitempty"`
	Lane            string `json:"lane"`
	RecipientState  string `json:"recipient_state"`
	Messageability  string `json:"messageability,omitempty"`
	Channel         string `json:"channel,omitempty"`
	RecommendedNext string `json:"recommended_action,omitempty"`
	Warning         string `json:"blocking_warning,omitempty"`
	EmailValidated  bool   `json:"email_validated"`
	NamedHuman      bool   `json:"named_human"`
}

type ManualQueueItem struct {
	Company           string   `json:"company"`
	Person            string   `json:"person,omitempty"`
	Role              string   `json:"role,omitempty"`
	ContactTier       string   `json:"contact_tier"`
	Lane              string   `json:"lane"`
	Channel           string   `json:"channel,omitempty"`
	Source            string   `json:"source,omitempty"`
	Service           string   `json:"service,omitempty"`
	WhyNow            string   `json:"why_now,omitempty"`
	FactualHook       string   `json:"factual_hook,omitempty"`
	RecommendedAction string   `json:"recommended_action,omitempty"`
	SuggestedText     string   `json:"suggested_text,omitempty"`
	Confidence        string   `json:"confidence,omitempty"`
	Warning           string   `json:"blocking_warning,omitempty"`
	Actions           []string `json:"actions"`
	CanonicalTargetID string   `json:"canonical_target_id,omitempty"`
}

type ContactFunnel struct {
	Imported            int `json:"imported"`
	TierA               int `json:"tier_a"`
	TierB               int `json:"tier_b"`
	TierC               int `json:"tier_c"`
	TierD               int `json:"tier_d"`
	BlockedExhausted    int `json:"blocked_exhausted"`
	MessageabilityReady int `json:"messageability_ready"`
	NeedsReview         int `json:"needs_review"`
	ManualOutreachReady int `json:"manual_outreach_ready"`
	Approved            int `json:"approved"`
	Contacted           int `json:"contacted"`
	Replied             int `json:"replied"`
	Meeting             int `json:"meeting"`
	Actionable          int `json:"actionable"`
	ActionPlanned       int `json:"action_planned"`
	Touched             int `json:"touched"`
	TargetReached       int `json:"target_reached"`
	Conversation        int `json:"conversation"`
	Interested          int `json:"interested"`
}

func ClassifyContactTier(acc *models.OutreachAccount, c *models.OutreachContactCandidate, now time.Time) ContactClass {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	out := ContactClass{Channel: "email"}
	if acc != nil && (acc.DoNotContact || acc.Blocked) {
		out.Tier, out.Lane, out.RecipientState = ContactTierE, LaneBlockedExhausted, RecipientBlocked
		out.RecommendedNext, out.Warning = "Nao abordar automaticamente.", "Conta bloqueada ou marcada como nao contatar."
		return out
	}
	if c == nil {
		out.Tier, out.Lane, out.RecipientState = ContactTierE, LaneBlockedExhausted, RecipientBlocked
		out.RecommendedNext, out.Warning = "Publique um contato no extra-cli.", "Nenhum destinatario publicado."
		return out
	}
	if c.DoNotContact || c.Bounced || c.Blocked {
		out.Tier, out.Lane, out.RecipientState = ContactTierE, LaneBlockedExhausted, RecipientBlocked
		out.RecommendedNext, out.Warning = "Mantenha a supressao.", firstNonEmpty(c.BlockReason, "Destinatario bloqueado, bounce ou opt-out.")
		return out
	}
	named := provenPersonName(c)
	roleOK := provenRole(c)
	out.NamedHuman = named && roleOK
	generic := isGenericRecipient(c)
	roleBox := isRoleMailbox(c)
	genericBox := isGenericCorporateMailbox(c)
	email := strings.TrimSpace(c.Email)
	sendReady := c.EmailSendReady && email != "" && !generic && !roleBox
	if sendReady && named && roleOK && validatePilotRecipient(c, now) == nil {
		// TIER A is identity only. NEEDS_REVIEW is decided later by VALIDATED+READY+body.
		out.Tier, out.EmailValidated, out.RecipientState = ContactTierA, true, RecipientValidated
		out.RecommendedNext = "Gerar mensagem so se messageability for READY; autorizacao humana obrigatoria."
		return out
	}
	if named && roleOK && !generic && !roleBox && (email == "" || !c.EmailSendReady) {
		out.Tier, out.RecipientState, out.Lane = ContactTierB, RecipientException, LaneManualOutreach
		out.Channel = firstNonEmpty(channelFromCandidate(c), "manual")
		out.RecommendedNext, out.Warning = "Abordar no canal publicado. Nao enviar e-mail automatico.", "Pessoa nomeada sem e-mail direto validado."
		return out
	}
	if roleBox && !named {
		out.Tier, out.RecipientState, out.Lane = ContactTierC, RecipientException, LaneRoleMailboxException
		out.RecommendedNext, out.Warning = "Decidir manualmente se a caixa funcional deve ser usada. Nao tratar como humano validado.", "Caixa funcional oficial; nao e uma pessoa nomeada."
		return out
	}
	if genericBox || generic {
		out.Tier, out.RecipientState, out.Lane = ContactTierD, RecipientException, LaneLowConfidenceManual
		out.RecommendedNext, out.Warning = "So abordagem manual de baixa confianca. Nunca mascarar como pessoa.", "Contato corporativo generico."
		return out
	}
	out.Tier, out.RecipientState, out.Lane = ContactTierE, RecipientBlocked, LaneBlockedExhausted
	out.RecommendedNext, out.Warning = "Sem acao automatica.", "Sem fundamento suficiente para um proximo passo."
	return out
}

func ClassifyActionLane(cls ContactClass, rec RecipientResolution, plan OutboundMessagePlan, body string) string {
	// NEEDS_REVIEW is only sendable copy awaiting human authorization.
	if rec.State == RecipientValidated && cls.Tier == ContactTierA &&
		plan.Messageability == MessageabilityReady && strings.TrimSpace(body) != "" {
		return LaneNeedsReviewEmail
	}
	if cls.Tier == ContactTierE || rec.State == RecipientBlocked {
		return LaneBlockedExhausted
	}
	switch cls.Tier {
	case ContactTierB:
		return LaneManualOutreach
	case ContactTierC:
		return LaneRoleMailboxException
	case ContactTierD:
		return LaneLowConfidenceManual
	}
	if rec.State == RecipientException {
		// Conflict / ambiguous identity is an operator decision, never authorize.
		return LaneManualOutreach
	}
	return LaneBlockedExhausted
}

func BuildManualItem(acc *models.OutreachAccount, c *models.OutreachContactCandidate, rec RecipientResolution, cls ContactClass, plan OutboundMessagePlan) ManualQueueItem {
	item := ManualQueueItem{
		Company: firstNonEmpty(accName(acc), rec.Company), ContactTier: cls.Tier, Lane: cls.Lane,
		Channel: cls.Channel, Service: firstNonEmpty(plan.ServiceCode, accService(acc)),
		WhyNow: rec.Reason, FactualHook: firstNonEmpty(plan.Hook, accFact(acc)),
		RecommendedAction: firstNonEmpty(cls.RecommendedNext, rec.NextAction),
		Warning:           cls.Warning, Confidence: rec.Confidence, CanonicalTargetID: rec.CanonicalTargetID,
		Actions: manualActionsFor(cls.Tier),
	}
	if rec.State == RecipientValidated || cls.NamedHuman {
		item.Person = firstNonEmpty(rec.Name, candidateName(c))
		item.Role = firstNonEmpty(rec.Role, candidateRole(c))
	}
	if c != nil {
		item.Source = firstNonEmpty(c.SourceURL, c.SourceDocument)
		if item.Confidence == "" {
			item.Confidence = c.Confidence
		}
		if item.Channel == "" || item.Channel == "email" {
			item.Channel = channelFromCandidate(c)
		}
	}
	if acc != nil {
		item.WhyNow = firstNonEmpty(acc.MomentSummary, item.WhyNow)
	}
	if cls.Lane != LaneNeedsReviewEmail {
		item.SuggestedText = manualSuggestedText(cls, item)
	}
	return item
}

func SummarizeContactFunnel(items []ContactClass, extras ContactFunnel) ContactFunnel {
	out := extras
	out.Imported = len(items)
	for _, it := range items {
		switch it.Tier {
		case ContactTierA:
			out.TierA++
		case ContactTierB:
			out.TierB++
		case ContactTierC:
			out.TierC++
		case ContactTierD:
			out.TierD++
		default:
			out.BlockedExhausted++
		}
		if it.Lane == LaneNeedsReviewEmail {
			out.NeedsReview++
			out.MessageabilityReady++
		}
		if it.Lane == LaneManualOutreach || it.Lane == LaneRoleMailboxException || it.Lane == LaneLowConfidenceManual {
			out.ManualOutreachReady++
		}
	}
	return out
}

func manualActionsFor(tier string) []string {
	base := []string{ManualOpenSource, ManualCopyText, ManualSkip, ManualNeedsEnrichment, ManualCorrectContact}
	switch tier {
	case ContactTierB, ContactTierC, ContactTierD:
		return append(base, ManualMarkContacted, ManualPromoteAfterEvidence)
	default:
		return []string{ManualSkip}
	}
}

func manualSuggestedText(cls ContactClass, item ManualQueueItem) string {
	company := strings.TrimSpace(item.Company)
	hook := strings.TrimSpace(item.FactualHook)
	switch cls.Tier {
	case ContactTierB:
		name := strings.TrimSpace(strings.Split(item.Person, " ")[0])
		greet := "Ola"
		if name != "" {
			greet = "Ola, " + name
		}
		if hook == "" {
			return greet + ". Posso te mandar um recorte curto do que conferiria neste contrato?"
		}
		return greet + ". Pelo que esta publico, " + hook + ". Posso te mandar o recorte do que eu conferiria?"
	case ContactTierC:
		if hook == "" {
			return "Ola. Escrevo para a area responsavel pelos contratos de " + firstNonEmpty(company, "voces") + ". Posso enviar um recorte objetivo para a equipe?"
		}
		return "Ola. Escrevo para a caixa da area de contratos de " + firstNonEmpty(company, "voces") + ". Pelo que esta publico, " + hook + ". Posso enviar um recorte para a equipe?"
	case ContactTierD:
		return "Nao abordar como pessoa. Se for o caso, use o canal geral sem fingir destinatario nominal."
	default:
		return ""
	}
}

func isRoleMailbox(c *models.OutreachContactCandidate) bool {
	if c == nil {
		return false
	}
	switch strings.ToUpper(strings.TrimSpace(c.MailboxPurpose)) {
	case "ROLE", "ROLE_MAILBOX", "FUNCTIONAL", "INSTITUTIONAL":
		return true
	}
	return isRoleMailboxLocal(c.Email)
}

func isGenericCorporateMailbox(c *models.OutreachContactCandidate) bool {
	if c == nil {
		return false
	}
	if isGenericMailboxPurpose(c.MailboxPurpose) && !isRoleMailbox(c) {
		return true
	}
	return isGenericCorporateLocal(c.Email)
}

func isRoleMailboxLocal(email string) bool {
	switch emailLocal(email) {
	case "licitacao", "licitacoes", "comercial", "sales", "vendas", "contratos", "compras":
		return true
	}
	return false
}

func isGenericCorporateLocal(email string) bool {
	switch emailLocal(email) {
	case "contato", "contact", "info", "atendimento", "sac", "financeiro", "administrativo", "ouvidoria", "rh", "vagas":
		return true
	}
	return false
}

func emailLocal(email string) string {
	parts := strings.Split(canonicalPilotEmail(email), "@")
	if len(parts) != 2 {
		return ""
	}
	return strings.Trim(parts[0], "._-+")
}

func channelFromCandidate(c *models.OutreachContactCandidate) string {
	if c == nil {
		return ""
	}
	if strings.TrimSpace(c.Email) != "" && c.EmailSendReady && !isGenericRecipient(c) {
		return "email"
	}
	if strings.TrimSpace(firstNonEmpty(c.PhoneE164, c.Phone)) != "" {
		return "phone"
	}
	if strings.TrimSpace(c.LinkedInURL) != "" {
		return "linkedin"
	}
	if strings.TrimSpace(c.Email) != "" {
		return "email_unvalidated"
	}
	return "manual"
}

func normalizePublishedTier(mailboxPurpose, _ string) string {
	switch strings.ToUpper(strings.TrimSpace(mailboxPurpose)) {
	case ContactTierA, "TIER_A", "A":
		return ContactTierA
	case ContactTierB, "TIER_B", "B", "MANUAL_CHANNEL":
		return ContactTierB
	case ContactTierC, "TIER_C", "C", "ROLE", "ROLE_MAILBOX", "FUNCTIONAL":
		return ContactTierC
	case ContactTierD, "TIER_D", "D", "GENERIC", "GENERIC_CONTACT":
		return ContactTierD
	case ContactTierE, "TIER_E", "E", "BLOCKED":
		return ContactTierE
	}
	return ""
}

func applyPublishedContactTier(c *models.OutreachContactCandidate, published string) {
	if c == nil {
		return
	}
	switch normalizePublishedTier(published, "") {
	case ContactTierB:
		c.EmailSendReady = false
		if strings.TrimSpace(c.MailboxPurpose) == "" {
			c.MailboxPurpose = "MANUAL_CHANNEL"
		}
	case ContactTierC:
		if strings.TrimSpace(c.MailboxPurpose) == "" {
			c.MailboxPurpose = "ROLE_MAILBOX"
		}
	case ContactTierD:
		if strings.TrimSpace(c.MailboxPurpose) == "" {
			c.MailboxPurpose = "GENERIC_CONTACT"
		}
	case ContactTierE:
		if !c.Blocked && !c.DoNotContact && !c.Bounced {
			c.Blocked = true
			if c.BlockReason == "" {
				c.BlockReason = "published_exhausted"
			}
		}
	}
}

func candidateName(c *models.OutreachContactCandidate) string {
	if c == nil || !provenPersonName(c) {
		return ""
	}
	return strings.TrimSpace(c.Name)
}

func candidateRole(c *models.OutreachContactCandidate) string {
	if c == nil || !provenRole(c) {
		return ""
	}
	return strings.TrimSpace(c.Role)
}

func accFact(acc *models.OutreachAccount) string {
	if acc == nil {
		return ""
	}
	return acc.FactToMention
}
