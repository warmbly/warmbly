package whatsapp

import (
	"time"

	"github.com/google/uuid"
)

// ContactChannelState is the policy input for one contact's WhatsApp eligibility.
// Public phone numbers must arrive with ConsentStatus UNKNOWN or NO_OPT_IN.
type ContactChannelState struct {
	ContactID              uuid.UUID
	OrganizationID         uuid.UUID
	PhoneE164              string
	PhoneSource            string // e.g. official_company_site, form, user_provided
	PhoneSourceURL         string
	PhoneVerifiedAt        *time.Time
	ConsentStatus          string
	ConsentSource          string
	ConsentAt              *time.Time
	ConsentScope           string // marketing | transactional | service
	ConsentProvenanceOK    bool   // true only when opt-in has recorded provenance
	LastInboundAt          *time.Time
	ServiceWindowUntil     *time.Time
	ChannelStatus          string // operational label; optional
	OptOutAt               *time.Time
	DoNotContact           bool
	LastEmailOutboundAt    *time.Time
	LastWhatsAppOutboundAt *time.Time
	LastAnyOutboundAt      *time.Time
	HasOpenReply           bool // human reply on any channel for active sequence
}

// SendIntent describes what the caller wants to do.
type SendIntent struct {
	// Mode: free_text | template | enroll_sequence
	Mode string
	// TemplateApproved is true only when the template is APPROVED at Meta/BSP.
	TemplateApproved bool
	// TemplateName for audit; empty for free text.
	TemplateName string
	// Automated is true for sequence/scheduler sends (not human-clicked send).
	Automated bool
	// Now is the evaluation time (injectable for tests).
	Now time.Time
	// CrossChannelMin is the minimum gap between any outbound channels.
	// Zero means use a default of 24h when Automated.
	CrossChannelMin time.Duration
	// ServiceWindow is the allowed reply window after user-initiated inbound.
	ServiceWindow time.Duration
	// RequireHumanApproval blocks automated free-text even when consented.
	RequireHumanApproval bool
	// FeatureEnabled master switch.
	FeatureEnabled bool
	// AutoSendEnabled feature flag for automated sends.
	AutoSendEnabled bool
}

// Decision is the policy outcome. Allowed=false means zero provider sends.
type Decision struct {
	Allowed     bool
	Eligibility string
	Reason      string
	// UseTemplate forces template path (e.g. outside service window with opt-in).
	UseTemplate bool
	// OpenServiceWindow is true when free-text is allowed due to 24h window.
	OpenServiceWindow bool
}

const (
	ModeFreeText       = "free_text"
	ModeTemplate       = "template"
	ModeEnrollSequence = "enroll_sequence"
)

// EvaluateEligibility is the deterministic policy gate.
// Critical invariant: public phone + no opt-in → zero automated WhatsApp sends.
func EvaluateEligibility(state ContactChannelState, intent SendIntent) Decision {
	now := intent.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	sw := intent.ServiceWindow
	if sw <= 0 {
		sw = 24 * time.Hour
	}
	cross := intent.CrossChannelMin
	if cross <= 0 && intent.Automated {
		cross = 24 * time.Hour
	}

	if !intent.FeatureEnabled {
		return Decision{Allowed: false, Eligibility: EligBlocked, Reason: "whatsapp_feature_disabled"}
	}

	// Global DNC / sticky opt-out always wins.
	if state.DoNotContact || state.ConsentStatus == ConsentDoNotContact {
		return Decision{Allowed: false, Eligibility: EligBlocked, Reason: "do_not_contact"}
	}
	if state.ConsentStatus == ConsentOptedOut || state.OptOutAt != nil {
		return Decision{Allowed: false, Eligibility: EligBlocked, Reason: "opted_out"}
	}

	if state.PhoneE164 == "" {
		return Decision{Allowed: false, Eligibility: EligBlocked, Reason: "missing_phone"}
	}

	// No inventing consent: UNKNOWN / NO_OPT_IN / empty block automation.
	consent := state.ConsentStatus
	if consent == "" {
		consent = ConsentUnknown
	}

	inServiceWindow := false
	if state.ServiceWindowUntil != nil && state.ServiceWindowUntil.After(now) {
		inServiceWindow = true
	} else if state.LastInboundAt != nil && state.LastInboundAt.Add(sw).After(now) {
		inServiceWindow = true
	}

	// Free-text is only allowed inside an open service window (last inbound + TTL).
	// USER_INITIATED alone does not keep free-text forever after the window closes.
	if inServiceWindow {
		if intent.Mode == ModeFreeText || intent.Mode == ModeTemplate {
			if intent.Automated && !intent.AutoSendEnabled {
				return Decision{Allowed: false, Eligibility: EligManualReview, Reason: "auto_send_disabled", OpenServiceWindow: true}
			}
			if intent.Automated && intent.RequireHumanApproval {
				return Decision{Allowed: false, Eligibility: EligManualReview, Reason: "requires_human_approval", OpenServiceWindow: true}
			}
			if blocked, why := crossChannelBlocked(state, now, cross); blocked {
				return Decision{Allowed: false, Eligibility: EligBlocked, Reason: why, OpenServiceWindow: true}
			}
			elig := EligServiceWindow
			if consent == ConsentOptedIn && state.ConsentProvenanceOK {
				elig = EligAutomatedAllowed
			}
			return Decision{Allowed: true, Eligibility: elig, Reason: "service_window_open", OpenServiceWindow: true}
		}
	}

	switch consent {
	case ConsentUnknown, ConsentNoOptIn:
		// Public number is NOT opt-in. Block all automated and sequence enrollment.
		return Decision{Allowed: false, Eligibility: EligBlocked, Reason: "no_opt_in"}

	case ConsentOptedIn:
		if !state.ConsentProvenanceOK {
			// Refuse bare opted_in=true without provenance.
			return Decision{Allowed: false, Eligibility: EligBlocked, Reason: "opt_in_missing_provenance"}
		}
		if blocked, why := crossChannelBlocked(state, now, cross); blocked {
			return Decision{Allowed: false, Eligibility: EligBlocked, Reason: why}
		}
		if intent.Mode == ModeFreeText {
			if !inServiceWindow {
				// Outside service window: free text blocked; template may still work.
				return Decision{
					Allowed:     false,
					Eligibility: EligTemplateOnly,
					Reason:      "outside_service_window_use_template",
					UseTemplate: true,
				}
			}
		}
		if intent.Mode == ModeTemplate {
			if !intent.TemplateApproved {
				return Decision{Allowed: false, Eligibility: EligBlocked, Reason: "template_not_approved"}
			}
		}
		if intent.Automated && !intent.AutoSendEnabled {
			return Decision{Allowed: false, Eligibility: EligManualReview, Reason: "auto_send_disabled"}
		}
		if intent.Automated && intent.RequireHumanApproval {
			return Decision{Allowed: false, Eligibility: EligManualReview, Reason: "requires_human_approval"}
		}
		if intent.Mode == ModeTemplate {
			return Decision{Allowed: true, Eligibility: EligTemplateOnly, Reason: "opted_in_template", UseTemplate: true}
		}
		return Decision{Allowed: true, Eligibility: EligAutomatedAllowed, Reason: "opted_in"}

	case ConsentUserInitiated:
		// Handled above when window open; if we get here window is closed.
		if intent.Mode == ModeTemplate && intent.TemplateApproved {
			if intent.Automated && !intent.AutoSendEnabled {
				return Decision{Allowed: false, Eligibility: EligManualReview, Reason: "auto_send_disabled"}
			}
			return Decision{Allowed: true, Eligibility: EligTemplateOnly, Reason: "user_initiated_template_outside_window", UseTemplate: true}
		}
		return Decision{Allowed: false, Eligibility: EligTemplateOnly, Reason: "user_initiated_window_closed", UseTemplate: true}

	default:
		return Decision{Allowed: false, Eligibility: EligBlocked, Reason: "unknown_consent_status"}
	}
}

func crossChannelBlocked(state ContactChannelState, now time.Time, minGap time.Duration) (bool, string) {
	if minGap <= 0 {
		return false, ""
	}
	last := state.LastAnyOutboundAt
	if last == nil {
		// Prefer the more recent of email / whatsapp if aggregate not set.
		if state.LastEmailOutboundAt != nil {
			last = state.LastEmailOutboundAt
		}
		if state.LastWhatsAppOutboundAt != nil {
			if last == nil || state.LastWhatsAppOutboundAt.After(*last) {
				last = state.LastWhatsAppOutboundAt
			}
		}
	}
	if last == nil {
		return false, ""
	}
	if last.Add(minGap).After(now) {
		return true, "cross_channel_cooldown"
	}
	return false, ""
}

// OpenServiceWindowFromInbound updates state timestamps after a user message.
func OpenServiceWindowFromInbound(state *ContactChannelState, at time.Time, window time.Duration) {
	if state == nil {
		return
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if window <= 0 {
		window = 24 * time.Hour
	}
	state.LastInboundAt = &at
	until := at.Add(window)
	state.ServiceWindowUntil = &until
	if state.ConsentStatus == ConsentUnknown || state.ConsentStatus == ConsentNoOptIn || state.ConsentStatus == "" {
		state.ConsentStatus = ConsentUserInitiated
		state.ConsentSource = "inbound_whatsapp"
		state.ConsentAt = &at
		state.ConsentProvenanceOK = true
		state.ConsentScope = "service"
	}
}

// ApplyOptOut sets sticky opt-out that survives re-import.
func ApplyOptOut(state *ContactChannelState, at time.Time, source string) {
	if state == nil {
		return
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	state.ConsentStatus = ConsentOptedOut
	state.OptOutAt = &at
	state.ConsentSource = source
	state.ConsentAt = &at
	state.ConsentProvenanceOK = true
}

// ApplyDoNotContact sets permanent DNC.
func ApplyDoNotContact(state *ContactChannelState, at time.Time, source string) {
	if state == nil {
		return
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	state.DoNotContact = true
	state.ConsentStatus = ConsentDoNotContact
	state.OptOutAt = &at
	state.ConsentSource = source
	state.ConsentAt = &at
	state.ConsentProvenanceOK = true
}

// MergeImportConsent never upgrades sticky opt-out/DNC from a later import.
// Public phone imports must not invent OPTED_IN.
func MergeImportConsent(existing ContactChannelState, incomingConsent, incomingSource string, incomingAt *time.Time, provenanceOK bool) ContactChannelState {
	out := existing
	if existing.ConsentStatus == ConsentOptedOut || existing.ConsentStatus == ConsentDoNotContact || existing.DoNotContact || existing.OptOutAt != nil {
		// Sticky: ignore softer incoming status.
		return out
	}
	// Never invent opt-in from public sources.
	switch incomingConsent {
	case ConsentOptedIn:
		if !provenanceOK {
			// Downgrade to NO_OPT_IN rather than accept bare flag.
			if out.ConsentStatus == "" || out.ConsentStatus == ConsentUnknown {
				out.ConsentStatus = ConsentNoOptIn
			}
			return out
		}
		out.ConsentStatus = ConsentOptedIn
		out.ConsentSource = incomingSource
		out.ConsentAt = incomingAt
		out.ConsentProvenanceOK = true
	case ConsentUserInitiated, ConsentOptedOut, ConsentDoNotContact, ConsentNoOptIn, ConsentUnknown:
		if incomingConsent != "" {
			out.ConsentStatus = incomingConsent
			out.ConsentSource = incomingSource
			out.ConsentAt = incomingAt
			out.ConsentProvenanceOK = provenanceOK || incomingConsent == ConsentUserInitiated
		}
	default:
		if out.ConsentStatus == "" {
			out.ConsentStatus = ConsentUnknown
		}
	}
	return out
}
