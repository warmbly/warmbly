package confenge

import (
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/app/whatsapp"
	"github.com/warmbly/warmbly/internal/models"
)

// ChannelAction is the deterministic next action for multichannel outreach.
const (
	ChannelActionNone             = "NONE"
	ChannelActionEmailOnly        = "EMAIL_ONLY"
	ChannelActionWhatsAppBlocked  = "WHATSAPP_BLOCKED"
	ChannelActionWhatsAppEligible = "WHATSAPP_ELIGIBLE"
	ChannelActionWhatsAppTemplate = "WHATSAPP_TEMPLATE_ONLY"
	ChannelActionWhatsAppReview   = "WHATSAPP_MANUAL_REVIEW"
	ChannelActionStopAll          = "STOP_ALL"
	ChannelActionAwaitCooldown    = "AWAIT_CROSS_CHANNEL_COOLDOWN"
)

// ChannelDecision is the orchestrator output (policy engine, not LLM).
type ChannelDecision struct {
	Action      string `json:"action"`
	Channel     string `json:"channel,omitempty"` // EMAIL | WHATSAPP
	Eligibility string `json:"eligibility,omitempty"`
	Reason      string `json:"reason"`
	// CaseID maps to product cases A–E for tests/docs.
	CaseID string `json:"case_id,omitempty"`
}

// OrchestrateChannel decides the next commercial channel action.
// The LLM may suggest a channel; this function is the authority.
func OrchestrateChannel(
	waEnabled bool,
	emailAvailable bool,
	phoneE164 string,
	state whatsapp.ContactChannelState,
	intent whatsapp.SendIntent,
) ChannelDecision {
	// Global DNC / opt-out stops everything WhatsApp and blocks new WA automation.
	if state.DoNotContact || state.ConsentStatus == whatsapp.ConsentDoNotContact {
		return ChannelDecision{Action: ChannelActionStopAll, Reason: "do_not_contact", CaseID: "E"}
	}
	if state.ConsentStatus == whatsapp.ConsentOptedOut || state.OptOutAt != nil {
		return ChannelDecision{Action: ChannelActionStopAll, Reason: "opted_out", CaseID: "E"}
	}
	if state.HasOpenReply {
		return ChannelDecision{Action: ChannelActionStopAll, Reason: "human_reply_stop_sequences"}
	}

	// Case A: cold lead, public phone, no opt-in → email may proceed; WA blocked.
	consent := state.ConsentStatus
	if consent == "" {
		consent = whatsapp.ConsentUnknown
	}
	publicNoOptIn := phoneE164 != "" && (consent == whatsapp.ConsentUnknown || consent == whatsapp.ConsentNoOptIn)

	if !waEnabled || phoneE164 == "" {
		if emailAvailable {
			return ChannelDecision{Action: ChannelActionEmailOnly, Channel: whatsapp.ChannelEmail, Reason: "whatsapp_unavailable_or_disabled", CaseID: "A"}
		}
		return ChannelDecision{Action: ChannelActionNone, Reason: "no_channel"}
	}

	if publicNoOptIn {
		// Phone stays stored; zero automated WhatsApp.
		if emailAvailable {
			return ChannelDecision{
				Action:      ChannelActionWhatsAppBlocked,
				Channel:     whatsapp.ChannelEmail,
				Eligibility: whatsapp.EligBlocked,
				Reason:      "public_phone_no_opt_in",
				CaseID:      "A",
			}
		}
		return ChannelDecision{
			Action:      ChannelActionWhatsAppBlocked,
			Eligibility: whatsapp.EligBlocked,
			Reason:      "public_phone_no_opt_in",
			CaseID:      "A",
		}
	}

	// Evaluate WhatsApp policy for intentional WA send.
	d := whatsapp.EvaluateEligibility(state, intent)
	if !d.Allowed {
		if d.Reason == "cross_channel_cooldown" {
			return ChannelDecision{Action: ChannelActionAwaitCooldown, Eligibility: d.Eligibility, Reason: d.Reason}
		}
		if d.Eligibility == whatsapp.EligManualReview {
			return ChannelDecision{Action: ChannelActionWhatsAppReview, Channel: whatsapp.ChannelWhatsApp, Eligibility: d.Eligibility, Reason: d.Reason, CaseID: caseForConsent(consent)}
		}
		if d.Eligibility == whatsapp.EligTemplateOnly || d.UseTemplate {
			return ChannelDecision{Action: ChannelActionWhatsAppTemplate, Channel: whatsapp.ChannelWhatsApp, Eligibility: d.Eligibility, Reason: d.Reason, CaseID: caseForConsent(consent)}
		}
		if emailAvailable {
			return ChannelDecision{Action: ChannelActionEmailOnly, Channel: whatsapp.ChannelEmail, Eligibility: d.Eligibility, Reason: d.Reason, CaseID: caseForConsent(consent)}
		}
		return ChannelDecision{Action: ChannelActionWhatsAppBlocked, Eligibility: d.Eligibility, Reason: d.Reason, CaseID: caseForConsent(consent)}
	}

	return ChannelDecision{
		Action:      ChannelActionWhatsAppEligible,
		Channel:     whatsapp.ChannelWhatsApp,
		Eligibility: d.Eligibility,
		Reason:      d.Reason,
		CaseID:      caseForConsent(consent),
	}
}

func caseForConsent(consent string) string {
	switch consent {
	case whatsapp.ConsentUserInitiated:
		return "C"
	case whatsapp.ConsentOptedIn:
		return "D"
	case whatsapp.ConsentOptedOut, whatsapp.ConsentDoNotContact:
		return "E"
	default:
		return "B"
	}
}

// FeedPhone is an optional structured phone object from extra-cli (backward compatible).
// Legacy string field FeedContact.Phone remains supported.
type FeedPhone struct {
	Raw        string `json:"raw"`
	E164       string `json:"e164"`
	Type       string `json:"type"` // mobile | landline | unknown
	SourceURL  string `json:"source_url"`
	SourceKind string `json:"source_kind"`
	Confidence string `json:"confidence"`
}

// FeedWhatsApp is optional consent facts from extra-cli. Warmbly applies policy.
type FeedWhatsApp struct {
	ConsentStatus string  `json:"consent_status"` // UNKNOWN default
	ConsentSource *string `json:"consent_source"`
	ConsentAt     *string `json:"consent_at"`
	ConsentScope  *string `json:"consent_scope"`
	// ProvenanceOK must be true with source/at for OPTED_IN to stick.
	ProvenanceOK bool `json:"provenance_ok"`
}

// ContactPhoneFacts is the normalized phone+consent extracted from a feed contact.
type ContactPhoneFacts struct {
	Raw           string
	E164          string
	Source        string
	SourceURL     string
	ConsentStatus string
	ConsentSource string
	ConsentAt     *time.Time
	ConsentScope  string
	ProvenanceOK  bool
}

// ExtractPhoneFacts merges legacy phone string + optional structured objects.
// Never invents OPTED_IN from public phone sources.
func ExtractPhoneFacts(c FeedContact) ContactPhoneFacts {
	out := ContactPhoneFacts{
		Raw:           strings.TrimSpace(c.Phone),
		SourceURL:     strings.TrimSpace(c.SourceURL),
		ConsentStatus: whatsapp.ConsentUnknown,
	}
	if c.PhoneObj != nil {
		if c.PhoneObj.Raw != "" {
			out.Raw = c.PhoneObj.Raw
		}
		if c.PhoneObj.E164 != "" {
			out.E164 = c.PhoneObj.E164
		}
		out.Source = c.PhoneObj.SourceKind
		if c.PhoneObj.SourceURL != "" {
			out.SourceURL = c.PhoneObj.SourceURL
		}
	}
	if out.E164 == "" && out.Raw != "" {
		norm := whatsapp.NormalizePhone(out.Raw, "BR")
		if norm.Valid {
			out.E164 = norm.E164
		}
	} else if out.E164 != "" && !strings.HasPrefix(out.E164, "+") {
		out.E164 = "+" + whatsapp.DigitsOnly(out.E164)
	}
	if out.Source == "" {
		// Public sources are not opt-in.
		out.Source = "import_public"
	}
	if c.WhatsApp != nil {
		st := strings.ToUpper(strings.TrimSpace(c.WhatsApp.ConsentStatus))
		if st != "" {
			out.ConsentStatus = st
		}
		if c.WhatsApp.ConsentSource != nil {
			out.ConsentSource = *c.WhatsApp.ConsentSource
		}
		if c.WhatsApp.ConsentScope != nil {
			out.ConsentScope = *c.WhatsApp.ConsentScope
		}
		out.ProvenanceOK = c.WhatsApp.ProvenanceOK
		if c.WhatsApp.ConsentAt != nil && *c.WhatsApp.ConsentAt != "" {
			if t, err := time.Parse(time.RFC3339, *c.WhatsApp.ConsentAt); err == nil {
				out.ConsentAt = &t
			}
		}
		// Refuse bare OPTED_IN without provenance.
		if out.ConsentStatus == whatsapp.ConsentOptedIn && !out.ProvenanceOK {
			out.ConsentStatus = whatsapp.ConsentNoOptIn
			out.ProvenanceOK = false
		}
	} else if out.E164 != "" {
		// Phone present without whatsapp block → UNKNOWN (not opted in).
		out.ConsentStatus = whatsapp.ConsentUnknown
	}
	return out
}

// BuildWhatsAppCopy creates short WhatsApp body from commercial context (not email paste).
// Target: ~25–70 words, 1 fact, 1 offer, 1 question. No em dash, no lists.
func BuildWhatsAppCopy(acc *models.OutreachAccount, cand *models.OutreachContactCandidate) string {
	name := "tudo bem"
	if cand != nil && strings.TrimSpace(cand.Name) != "" {
		// first token only
		parts := strings.Fields(strings.TrimSpace(cand.Name))
		name = parts[0]
	}
	fact := ""
	offer := ""
	question := ""
	if acc != nil {
		fact = strings.TrimSpace(acc.FactToMention)
		if fact == "" {
			fact = strings.TrimSpace(acc.MomentSummary)
		}
		offer = strings.TrimSpace(acc.EntryOffer)
		if offer == "" {
			offer = strings.TrimSpace(acc.ServiceName)
		}
		question = strings.TrimSpace(acc.QuestionToAsk)
	}
	if fact == "" {
		fact = "um momento recente da empresa"
	}
	if offer == "" {
		offer = "revisão técnica consultiva"
	}
	if question == "" {
		question = "Posso te explicar em duas linhas como fazemos essa revisão?"
	}
	// Strip em dashes if present in inputs.
	fact = strings.ReplaceAll(fact, "—", ",")
	fact = strings.ReplaceAll(fact, "–", ",")
	offer = strings.ReplaceAll(offer, "—", ",")
	question = strings.ReplaceAll(question, "—", ",")

	body := "Olá, " + name + ". Aqui é o Tiago, da CONFENGE.\n\n" +
		"Vi " + truncateRunes(fact, 120) + ". Trabalho especificamente com " + truncateRunes(offer, 80) + ".\n\n" +
		truncateRunes(question, 140)
	return strings.TrimSpace(body)
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// MapReplyClassToOutcome reuses the existing commercial taxonomy for WhatsApp.
// No parallel taxonomy.
func MapWhatsAppOptOutToReplyClass() string {
	return "do_not_contact"
}
