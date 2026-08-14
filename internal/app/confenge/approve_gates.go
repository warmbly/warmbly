package confenge

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/warmbly/warmbly/internal/models"
)

// Minimum commercial surface for human approve (email). WhatsApp uses a lower floor.
const (
	minApproveSubjectRunes  = 8
	minApproveEmailWords    = 25
	minApproveWhatsAppWords = 12
)

// StructuralApproveBlockers reconstructs commercial completeness from persisted
// account/candidate/strategy data. Human approve must not promote incomplete
// strategy to send-ready solely because body text passed superficial checks.
//
// Returns empty slice when the draft may be approved; otherwise explicit reasons.
func StructuralApproveBlockers(
	acc *models.OutreachAccount,
	cand *models.OutreachContactCandidate,
	st *OutreachStrategy,
	d *models.OutreachDraft,
	pb *Playbook,
) []string {
	var blockers []string
	add := func(code, msg string) {
		blockers = append(blockers, code+": "+msg)
	}

	if d == nil {
		add("missing_draft", "draft is required")
		return blockers
	}
	if acc == nil {
		add("missing_account", "account is required")
		return blockers
	}
	if acc.DoNotContact {
		add("account_dnc", "account is DO_NOT_CONTACT")
	}
	if acc.Blocked {
		add("account_blocked", "account is blocked")
	}

	if err := RequireTargetFit(acc); err != nil {
		add("target_not_current", err.Error())
	}

	// Contact send-ready / DNC / bounce / block.
	if cand == nil {
		add("missing_contact", "contact candidate is required")
	} else {
		if cand.DoNotContact {
			add("contact_dnc", "contact is DO_NOT_CONTACT")
		}
		if cand.Bounced {
			add("contact_bounce", "contact address bounced")
		}
		if cand.Blocked {
			add("contact_blocked", "contact is blocked")
		}
		if !cand.CanEnroll() {
			add("contact_not_send_ready", "contact is not enrollable")
		}
		if err := RequireEmailOutbound(acc, cand); err != nil {
			add("contact_not_send_ready", err.Error())
		}
	}

	// Prefer reconstructed strategy; fall back to draft validation JSON.
	if st == nil {
		st = strategyFromDraft(d)
	}
	if st == nil {
		// Last resort: replan from account/evidence-less candidate.
		tmp := PlanOutreachStrategy(pb, acc, cand, nil, 1)
		st = &tmp
	}

	// Use strategy risk flags only. Draft.RiskFlags are outputs of prior
	// classify/structural runs and stick across human repairs (e.g. hollow
	// body → incomplete_copy_context must not permanently poison approve after
	// a valid edit). Recompute incompleteness from strategy + draft surface.
	flags := append([]string{}, st.RiskFlags...)

	serviceCode := strings.TrimSpace(firstNonEmpty(st.ServiceCode, d.ServiceCode, acc.ServiceCode))
	if serviceCode == "" {
		add("missing_service_code", "service code is empty")
	} else if pb != nil && pb.ResolveServicePlaybook(serviceCode) == nil {
		add("unknown_service_code", "service has no canonical playbook mapping: "+serviceCode)
	}

	if containsAnyFlag(flags, "unknown_service_code", "service_unmapped") {
		add("unknown_service_code", "strategy marked unknown_service_code")
	}
	if containsAnyFlag(flags, "missing_service_code") {
		add("missing_service_code", "strategy marked missing_service_code")
	}
	if containsAnyFlag(flags, "incomplete_strategy") {
		add("incomplete_strategy", "strategy is incomplete")
	}
	if plan := messagePlanFromDraft(d); plan != nil && plan.Messageability != "" && plan.Messageability != MessageabilityReady {
		add("not_messageable", "messageability is "+plan.Messageability+": "+firstNonEmpty(plan.Reason, "dossier cannot support a first contact"))
	}
	rec := recipientFromDraft(d)
	if rec == nil && acc != nil {
		var cands []models.OutreachContactCandidate
		if cand != nil {
			cands = []models.OutreachContactCandidate{*cand}
		}
		resolved := ResolveRecipient(acc, cands, time.Now().UTC())
		rec = &resolved
	}
	if rec == nil || rec.State != RecipientValidated {
		reason := "recipient identity is not VALIDATED"
		if rec != nil {
			reason = "recipient is " + rec.State
			if rec.Reason != "" {
				reason += ": " + rec.Reason
			}
		}
		add("recipient_not_validated", reason)
	}
	if cand != nil && isGenericRecipient(cand) {
		add("generic_mailbox", "generic mailbox cannot be approved")
	}
	if containsAnyFlag(flags, "incomplete_copy_context") {
		add("incomplete_copy_context", "copy context is incomplete")
	}

	micro := strings.TrimSpace(st.MicroOfferCode)
	if micro == "" {
		// Draft may not store micro_offer; strategy is authoritative.
		add("missing_micro_offer", "MicroOfferCode is empty")
	} else if pb != nil && serviceCode != "" {
		if o := pb.FindOffer(micro); o != nil {
			if !pb.OfferApplicable(o, serviceCode, true) {
				add("service_offer_mismatch", "micro-offer "+micro+" not applicable to service "+serviceCode)
			}
		}
	}

	whyYou := strings.TrimSpace(st.WhyThisAccount)
	whyNow := strings.TrimSpace(st.WhyNow)
	obs := strings.TrimSpace(firstNonEmpty(st.ObservedFact, d.FactUsed, acc.FactToMention))
	if whyYou == "" || isGenericWhyThisAccount(whyYou) {
		add("missing_why_this_account", "WhyThisAccount empty or generic")
	}
	if whyNow == "" || isGenericWhyNow(whyNow) {
		add("missing_why_now", "WhyNow empty or generic")
	}
	if obs == "" || isGenericPublicFact(obs) {
		add("missing_observed_fact", "ObservedFact empty or generic")
	}

	// Evidence required when service/playbook expects it (fact used without anchors).
	if obs != "" && len(st.EvidenceIDs) == 0 && len(acc.MomentEvidenceIDs) == 0 && len(d.EvidenceIDs) == 0 {
		add("missing_evidence", "service fact lacks referenced evidence ids")
	}

	// RiskClass alone is not authority, but RED with incomplete flags is hard-block.
	if strings.EqualFold(d.RiskClass, "RED") && containsAnyFlag(flags,
		"incomplete_copy_context", "incomplete_strategy", "unknown_service_code", "missing_service_code") {
		add("red_incomplete", "RED draft with structural incompleteness cannot be approved")
	}

	// Draft surface completeness: a human edit that hollows subject/body must
	// not become APPROVED merely because account strategy fields remain rich.
	// Case A (prod no-send): subject "x" + body "Oi" must fail closed.
	subj := strings.TrimSpace(d.Subject)
	body := strings.TrimSpace(d.BodyText)
	isWA := d.Channel == models.OutreachChannelWhatsApp || strings.EqualFold(strings.TrimSpace(d.Channel), "whatsapp")
	if !isWA {
		if subj == "" {
			add("incomplete_subject", "subject is empty")
		} else if utf8.RuneCountInString(subj) < minApproveSubjectRunes {
			add("incomplete_subject", "subject too short for commercial outreach")
		}
	}
	if body == "" {
		add("incomplete_body", "body is empty")
	} else {
		minWords := minApproveEmailWords
		if isWA {
			minWords = minApproveWhatsAppWords
		}
		if countWords(body) < minWords {
			add("incomplete_body", fmt.Sprintf("body too short (%d words; need >=%d)", countWords(body), minWords))
		}
		if isHollowDraftBody(body) {
			add("hollow_body", "body is greeting-only or commercially hollow")
		}
		// If we have a concrete observed fact, body should anchor at least one
		// non-trivial token so approve cannot rubber-stamp unrelated fluff.
		if obs != "" && !isGenericPublicFact(obs) && !bodyAnchorsFact(body, obs) {
			add("body_missing_fact_anchor", "body does not reference the observed public fact")
		}
	}

	return blockers
}

// isHollowDraftBody detects greeting-only / placeholder human edits.
func isHollowDraftBody(body string) bool {
	b := strings.TrimSpace(strings.ToLower(body))
	if b == "" {
		return true
	}
	// Strip common punctuation and collapse whitespace.
	repl := strings.NewReplacer(
		",", " ", ".", " ", "!", " ", "?", " ", ";", " ", ":", " ",
		"\n", " ", "\r", " ", "\t", " ",
	)
	b = strings.Join(strings.Fields(repl.Replace(b)), " ")
	hollowExact := map[string]bool{
		"oi": true, "olá": true, "ola": true, "bom dia": true, "boa tarde": true,
		"boa noite": true, "hello": true, "hi": true, "hey": true, "test": true,
		"teste": true, "ok": true, "obrigado": true, "obrigada": true,
	}
	if hollowExact[b] {
		return true
	}
	// Extremely short after stripping (e.g. "Oi", "x", "abc")
	if countWords(b) <= 2 && utf8.RuneCountInString(b) < 12 {
		return true
	}
	return false
}

// bodyAnchorsFact reports whether body shares a non-trivial token with the fact.
func bodyAnchorsFact(body, fact string) bool {
	bodyL := strings.ToLower(body)
	// Prefer multi-digit contract-like tokens and significant words (>=5 runes).
	for _, tok := range strings.FieldsFunc(strings.ToLower(fact), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '/' && r != '-'
	}) {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		// digits / contract numbers
		hasDigit := false
		for _, r := range tok {
			if r >= '0' && r <= '9' {
				hasDigit = true
				break
			}
		}
		if hasDigit && len(tok) >= 3 && strings.Contains(bodyL, tok) {
			return true
		}
		if utf8.RuneCountInString(tok) >= 6 && strings.Contains(bodyL, tok) {
			return true
		}
	}
	return false
}

func containsAnyFlag(flags []string, want ...string) bool {
	for _, f := range flags {
		for _, w := range want {
			if strings.EqualFold(strings.TrimSpace(f), w) {
				return true
			}
		}
	}
	return false
}

// strategyFromDraft reconstructs strategy from persisted ValidationJSON when present.
func strategyFromDraft(d *models.OutreachDraft) *OutreachStrategy {
	if d == nil || len(d.ValidationJSON) == 0 {
		return nil
	}
	var val ValidationResult
	if err := json.Unmarshal(d.ValidationJSON, &val); err != nil {
		return nil
	}
	if val.Strategy == nil {
		return nil
	}
	return val.Strategy
}

func messagePlanFromDraft(d *models.OutreachDraft) *OutboundMessagePlan {
	if d == nil || len(d.ValidationJSON) == 0 {
		return nil
	}
	var val ValidationResult
	if err := json.Unmarshal(d.ValidationJSON, &val); err != nil {
		return nil
	}
	return val.MessagePlan
}

func recipientFromDraft(d *models.OutreachDraft) *RecipientResolution {
	if d == nil || len(d.ValidationJSON) == 0 {
		return nil
	}
	var val ValidationResult
	if err := json.Unmarshal(d.ValidationJSON, &val); err != nil {
		return nil
	}
	return val.Recipient
}

// FormatApproveBlockers joins structural blockers for errx messages.
func FormatApproveBlockers(blockers []string) string {
	if len(blockers) == 0 {
		return ""
	}
	return fmt.Sprintf("draft not structurally approvable: %s", strings.Join(blockers, "; "))
}
