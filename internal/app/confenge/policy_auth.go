package confenge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// AuthorizationMode distinguishes per-touch human approval from campaign policy.
const (
	AuthorizationModeHumanTouchpoint = "HUMAN_TOUCHPOINT_APPROVAL"
	AuthorizationModeCampaignPolicy  = "CAMPAIGN_POLICY"
)

// Canonical version tags used when minting / matching campaign policy grants.
const (
	ValidatorVersionV1      = "confenge.validators.v1"
	ContactPolicyVersionV1  = "confenge.contact.v1"
	TemplatePolicyVersionV1 = "policy_approved_v1"
)

// CampaignPolicyAuthorization is an alias of the models type for package-local use.
type CampaignPolicyAuthorization = models.CampaignPolicyAuthorization

// GreenAutorunInput is the deterministic predicate set for policy autoqueue.
// Critical booleans must be proven true from persisted/verified state; absence is deny.
type GreenAutorunInput struct {
	Channel                   string
	EmailSendReady            bool
	TargetFitSendTier         string // must be imported: A_AUTOMATIC | B_EVIDENCE_SUPPORTED
	OwnershipAllowed          bool
	MailboxPurposeAllowed     bool
	VerificationAllowed       bool
	DNC                       bool
	Bounce                    bool
	Replied                   bool
	Blocked                   bool
	ContactFresh              bool
	ContextFresh              bool
	ServiceCode               string
	SingleService             bool
	FactualHookAnchored       bool
	NoUnknownEvidenceIDs      bool
	NoHypothesisAsFact        bool
	NoClaimsToAvoidViolated   bool
	ValidationOK              bool
	RiskClass                 string
	MessageContextHashCurrent bool
	NoEditAfterAuthorization  bool
	CopyWithinLimits          bool
	GovernorHealthy           bool
	// HasContactCandidate: CAMPAIGN_POLICY requires an identifiable contact row.
	HasContactCandidate bool
	// Runtime versions compared to the policy grant (mismatch = deny).
	RuntimePromptVersion    string
	RuntimeValidatorVersion string
	RuntimeContactPolicy    string
	RuntimeTemplateVersion  string
	RuntimeSenderMailbox    string
	// Template path flags.
	UsedPolicyApprovedTemplate bool
	GenericUnauditedTemplate   bool
}

// GreenAutorunDecision is the fail-closed evaluation result.
type GreenAutorunDecision struct {
	Allow             bool
	AuthorizationMode string
	Reasons           []string
}

// PolicyAuthorizationHash is a stable audit fingerprint of an active grant.
func PolicyAuthorizationHash(auth *CampaignPolicyAuthorization) string {
	if auth == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(auth.ID.String())
	b.WriteByte(0)
	b.WriteString(auth.CampaignID.String())
	b.WriteByte(0)
	b.WriteString(strings.TrimSpace(auth.PromptPolicyVersion))
	b.WriteByte(0)
	b.WriteString(strings.TrimSpace(auth.ValidatorVersion))
	b.WriteByte(0)
	b.WriteString(strings.TrimSpace(auth.ContactPolicyVersion))
	b.WriteByte(0)
	b.WriteString(strings.TrimSpace(auth.TemplatePolicyVersion))
	b.WriteByte(0)
	b.WriteString(strings.ToLower(strings.TrimSpace(auth.SenderMailbox)))
	b.WriteByte(0)
	b.WriteString(strings.ToUpper(strings.TrimSpace(auth.Channel)))
	b.WriteByte(0)
	b.WriteString(strings.ToUpper(strings.TrimSpace(auth.AllowedRiskClass)))
	b.WriteByte(0)
	b.WriteString(fmt.Sprintf("%d", auth.MaxRatePerHour))
	b.WriteByte(0)
	if auth.AllowPolicyTemplateGREEN {
		b.WriteString("1")
	} else {
		b.WriteString("0")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// EffectiveHourlyCap is min of adaptive runtime, campaign policy cap, and any
// additional positive operational caps. Zero/negative extra caps are ignored.
func EffectiveHourlyCap(adaptiveCap, policyCap int, extraCaps ...int) int {
	capN := adaptiveCap
	if capN < 1 {
		capN = 1
	}
	if policyCap > 0 && policyCap < capN {
		capN = policyCap
	}
	for _, e := range extraCaps {
		if e > 0 && e < capN {
			capN = e
		}
	}
	return capN
}

// EvaluateGreenAutorun returns allow=true only when ALL predicates pass and
// a valid campaign policy authorization exists with GreenAutorunEnabled.
// Does not set approved_by to a human identity.
// InSendWindow and ProviderHealthy are intentionally NOT gates here: queue may
// form outside the window; final transport + governor enforce window/provider.
func EvaluateGreenAutorun(
	enabled bool,
	auth *CampaignPolicyAuthorization,
	in GreenAutorunInput,
	now time.Time,
) GreenAutorunDecision {
	reasons := make([]string, 0, 12)
	if !enabled {
		return GreenAutorunDecision{Allow: false, Reasons: []string{"green_autorun_disabled"}}
	}
	if auth == nil || !auth.Active(now) {
		return GreenAutorunDecision{Allow: false, Reasons: []string{"no_active_campaign_policy_authorization"}}
	}
	if auth.ID == uuid.Nil {
		reasons = append(reasons, "policy_authorization_id_missing")
	}
	if ch := strings.ToUpper(strings.TrimSpace(in.Channel)); ch != "EMAIL" {
		reasons = append(reasons, "channel_not_email")
	}
	if authCh := strings.ToUpper(strings.TrimSpace(auth.Channel)); authCh != "" && authCh != "EMAIL" {
		reasons = append(reasons, "auth_channel_not_email")
	}
	if !in.HasContactCandidate {
		reasons = append(reasons, "missing_contact_candidate")
	}
	if !in.EmailSendReady {
		reasons = append(reasons, "email_send_ready_false")
	}
	tier := strings.ToUpper(strings.TrimSpace(in.TargetFitSendTier))
	if tier == "" {
		reasons = append(reasons, "target_fit_tier_absent")
	} else if tier != "A_AUTOMATIC" && tier != "B_EVIDENCE_SUPPORTED" {
		reasons = append(reasons, "target_fit_not_send_tier")
	}
	if !in.OwnershipAllowed {
		reasons = append(reasons, "ownership_not_allowed")
	}
	if !in.MailboxPurposeAllowed {
		reasons = append(reasons, "mailbox_purpose_blocked")
	}
	if !in.VerificationAllowed {
		reasons = append(reasons, "verification_not_allowed")
	}
	if in.DNC {
		reasons = append(reasons, "dnc")
	}
	if in.Bounce {
		reasons = append(reasons, "bounce")
	}
	if in.Replied {
		reasons = append(reasons, "replied")
	}
	if in.Blocked {
		reasons = append(reasons, "blocked")
	}
	if !in.ContactFresh || !in.ContextFresh {
		reasons = append(reasons, "stale_contact_or_context")
	}
	if strings.TrimSpace(in.ServiceCode) == "" {
		reasons = append(reasons, "service_code_missing")
	}
	if !in.SingleService {
		reasons = append(reasons, "not_exactly_one_service")
	}
	if !in.FactualHookAnchored {
		reasons = append(reasons, "factual_hook_not_anchored")
	}
	if !in.NoUnknownEvidenceIDs {
		reasons = append(reasons, "unknown_evidence_ids")
	}
	if !in.NoHypothesisAsFact {
		reasons = append(reasons, "hypothesis_as_fact")
	}
	if !in.NoClaimsToAvoidViolated {
		reasons = append(reasons, "claims_to_avoid_violated")
	}
	if !in.ValidationOK {
		reasons = append(reasons, "validation_not_ok")
	}
	rc := strings.ToUpper(strings.TrimSpace(in.RiskClass))
	allowedRC := strings.ToUpper(strings.TrimSpace(auth.AllowedRiskClass))
	if allowedRC == "" {
		allowedRC = "GREEN"
	}
	if rc != "GREEN" || (allowedRC != "GREEN" && rc != allowedRC) {
		reasons = append(reasons, "risk_class_not_green")
	}
	if !in.MessageContextHashCurrent {
		reasons = append(reasons, "message_context_stale")
	}
	if !in.NoEditAfterAuthorization {
		reasons = append(reasons, "edited_after_authorization")
	}
	if !in.CopyWithinLimits {
		reasons = append(reasons, "copy_outside_limits")
	}
	if !in.GovernorHealthy {
		reasons = append(reasons, "governor_unhealthy")
	}
	// Generic unaudited template stays YELLOW — never autorun.
	if in.GenericUnauditedTemplate {
		reasons = append(reasons, "generic_unaudited_template")
	}
	// Policy-approved template is allowed when flag set; AI path needs no template flag.
	if in.UsedPolicyApprovedTemplate && !auth.AllowPolicyTemplateGREEN {
		reasons = append(reasons, "policy_template_not_authorized")
	}

	// Version / sender mismatch closes autorun (stale grant must not authorize new code paths).
	if pv := strings.TrimSpace(auth.PromptPolicyVersion); pv != "" && strings.TrimSpace(in.RuntimePromptVersion) != "" {
		if !promptVersionsCompatible(pv, in.RuntimePromptVersion) {
			reasons = append(reasons, "prompt_policy_version_mismatch")
		}
	}
	if vv := strings.TrimSpace(auth.ValidatorVersion); vv != "" && strings.TrimSpace(in.RuntimeValidatorVersion) != "" {
		if vv != strings.TrimSpace(in.RuntimeValidatorVersion) {
			reasons = append(reasons, "validator_version_mismatch")
		}
	}
	if cv := strings.TrimSpace(auth.ContactPolicyVersion); cv != "" && strings.TrimSpace(in.RuntimeContactPolicy) != "" {
		if cv != strings.TrimSpace(in.RuntimeContactPolicy) {
			reasons = append(reasons, "contact_policy_version_mismatch")
		}
	}
	if in.UsedPolicyApprovedTemplate {
		tv := strings.TrimSpace(auth.TemplatePolicyVersion)
		if tv == "" {
			tv = TemplatePolicyVersionV1
		}
		rt := strings.TrimSpace(in.RuntimeTemplateVersion)
		if rt != "" && rt != tv {
			reasons = append(reasons, "template_policy_version_mismatch")
		}
	}
	if sm := strings.TrimSpace(auth.SenderMailbox); sm != "" && strings.TrimSpace(in.RuntimeSenderMailbox) != "" {
		if !strings.EqualFold(sm, strings.TrimSpace(in.RuntimeSenderMailbox)) {
			reasons = append(reasons, "sender_mailbox_mismatch")
		}
	}

	if len(reasons) > 0 {
		return GreenAutorunDecision{Allow: false, AuthorizationMode: AuthorizationModeCampaignPolicy, Reasons: reasons}
	}
	return GreenAutorunDecision{
		Allow:             true,
		AuthorizationMode: AuthorizationModeCampaignPolicy,
		Reasons:           []string{"all_green_predicates_pass"},
	}
}

// promptVersionsCompatible allows exact match or draft.vN matching policy that
// stores the base PromptVersion (runtime often appends "+touch").
func promptVersionsCompatible(policyVer, runtimeVer string) bool {
	p := strings.TrimSpace(policyVer)
	r := strings.TrimSpace(runtimeVer)
	if p == r {
		return true
	}
	// Runtime may be "confenge.draft.v2+touch" while policy stores "confenge.draft.v2".
	if strings.HasPrefix(r, p) {
		return true
	}
	if strings.HasPrefix(p, r) {
		return true
	}
	return false
}
