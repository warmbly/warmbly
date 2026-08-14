package confenge

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// DoctrineQAResult is deterministic copy QA against doctrine + strategy.
type DoctrineQAResult struct {
	OK       bool     `json:"ok"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Alerts   []string `json:"alerts,omitempty"` // operator-facing short codes
}

var (
	fakeReFwdRe = regexp.MustCompile(`(?i)^\s*(re|fw|fwd)\s*:`)
	linkRe      = regexp.MustCompile(`(?i)https?://[^\s]+`)
)

// ValidateDoctrineCopy enforces world-class outreach constraints after structural ValidateDraft.
func ValidateDoctrineCopy(out *DraftOutput, st *OutreachStrategy, pb *Playbook, channel string) DoctrineQAResult {
	res := DoctrineQAResult{OK: true}
	if out == nil {
		res.OK = false
		res.Errors = append(res.Errors, "empty draft")
		return res
	}
	if pb == nil {
		pb, _ = LoadPlaybook()
	}
	body := strings.TrimSpace(out.BodyText)
	subj := strings.TrimSpace(out.Subject)
	blob := strings.ToLower(subj + "\n" + body)
	isInitial := channel == ChannelEmailInitial || channel == ""

	// Empty follow-up phrases (any touch)
	if pb != nil {
		for _, p := range pb.Sequence.EmptyFollowupPhrases {
			if p != "" && strings.Contains(blob, strings.ToLower(p)) {
				res.OK = false
				res.Errors = append(res.Errors, "empty follow-up phrase: "+p)
				res.Alerts = append(res.Alerts, "empty_followup")
			}
		}
		for _, p := range pb.CopyRules.BannedPhrases {
			if p != "" && strings.Contains(blob, strings.ToLower(p)) {
				res.OK = false
				res.Errors = append(res.Errors, "doctrine banned phrase: "+p)
				res.Alerts = append(res.Alerts, "banned_phrase")
			}
		}
		for _, p := range pb.CopyRules.CreepyPhrasing {
			if p != "" && strings.Contains(blob, strings.ToLower(p)) {
				res.OK = false
				res.Errors = append(res.Errors, "creepy phrasing: "+p)
				res.Alerts = append(res.Alerts, "creepy")
			}
		}
	}

	// Meeting-first CTA on first touch
	if isInitial && st != nil && st.SequencePosition <= 1 {
		if pb != nil {
			for _, p := range pb.CopyRules.MeetingCTAPhrases {
				if p != "" && strings.Contains(blob, strings.ToLower(p)) {
					res.OK = false
					res.Errors = append(res.Errors, "meeting CTA not allowed as first-touch default: "+p)
					res.Alerts = append(res.Alerts, "meeting_default_cta")
				}
			}
		}
		// Multiple CTAs (question marks as proxy + explicit calendar)
		if strings.Count(body, "?") > 2 {
			res.Warnings = append(res.Warnings, "multiple question marks / possible multi-CTA")
			res.Alerts = append(res.Alerts, "multiple_ctas")
		}
	}

	// Fake Re:/Fwd: on first touch subject
	if isInitial && subj != "" && fakeReFwdRe.MatchString(subj) {
		res.OK = false
		res.Errors = append(res.Errors, "fake Re:/Fwd: subject not allowed on first touch")
		res.Alerts = append(res.Alerts, "fake_re_fwd")
	}

	// Annualidade unpaid claim
	if st != nil && containsStr(st.RiskFlags, "annualidade_verify_only") {
		for _, bad := range annualidadeBadFromPB(pb) {
			if strings.Contains(blob, strings.ToLower(bad)) {
				res.OK = false
				res.Errors = append(res.Errors, "annualidade must not claim unpaid reajuste: "+bad)
				res.Alerts = append(res.Alerts, "unsupported_claim")
			}
		}
	}

	// Strategy claims_to_avoid
	if st != nil {
		for _, avoid := range st.ClaimsToAvoid {
			avoid = strings.TrimSpace(strings.ToLower(avoid))
			if avoid != "" && strings.Contains(blob, avoid) {
				res.OK = false
				res.Errors = append(res.Errors, "claims_to_avoid: "+avoid)
				res.Alerts = append(res.Alerts, "unsupported_claim")
			}
		}
		// WHY NOW required for first touch (strategy field, not necessarily in body)
		if st.SequencePosition <= 1 && strings.TrimSpace(st.WhyNow) == "" {
			res.OK = false
			res.Errors = append(res.Errors, "strategy missing why_now")
			res.Alerts = append(res.Alerts, "missing_why_now")
		}
		if st.SequencePosition <= 1 && strings.TrimSpace(st.WhyThisAccount) == "" {
			res.OK = false
			res.Errors = append(res.Errors, "strategy missing why_this_account")
		}
		// Offer fulfillable
		if st.MicroOfferCode != "" && pb != nil {
			o := pb.FindOffer(st.MicroOfferCode)
			if o == nil {
				res.OK = false
				res.Errors = append(res.Errors, "unknown micro_offer: "+st.MicroOfferCode)
				res.Alerts = append(res.Alerts, "offer_cannot_be_fulfilled")
			} else if strings.ToUpper(o.FulfillmentCost) != "LOW" && st.SequencePosition <= 1 {
				res.OK = false
				res.Errors = append(res.Errors, "cold path may only offer LOW fulfillment offers")
				res.Alerts = append(res.Alerts, "offer_cannot_be_fulfilled")
			}
		}
		if containsStr(st.RiskFlags, "no_safe_factual_hook") && isInitial {
			res.Warnings = append(res.Warnings, "weak factual hook; prefer needs-review over generic pitch")
			res.Alerts = append(res.Alerts, "evidence_weak")
		}
		if containsStr(st.RiskFlags, "evidence_requires_hypothesis_language") {
			if looksLikeHardMonetaryOrCertainty(blob) {
				res.OK = false
				res.Errors = append(res.Errors, "hypothesis-grade evidence phrased as certainty")
				res.Alerts = append(res.Alerts, "hypothesis_as_fact")
			}
		}
	}

	// Word count doctrine defaults for first email
	words := countWords(body)
	if isInitial && pb != nil {
		minW := pb.Doctrine.FirstEmailDefaults.MinWords
		maxW := pb.Doctrine.FirstEmailDefaults.MaxWords
		if minW > 0 && words > 0 && words < minW {
			res.Warnings = append(res.Warnings, fmt.Sprintf("shorter than doctrine min %d words (%d)", minW, words))
			res.Alerts = append(res.Alerts, "short_email")
		}
		if maxW > 0 && words > maxW {
			// Soft doctrine cap as warning if under hard MaxInitial; hard fail if way over
			if words > maxW+40 {
				res.OK = false
				res.Errors = append(res.Errors, fmt.Sprintf("body exceeds doctrine max %d words (%d)", maxW, words))
			} else {
				res.Warnings = append(res.Warnings, fmt.Sprintf("body above doctrine preferred max %d words (%d)", maxW, words))
			}
			res.Alerts = append(res.Alerts, "long_email")
		}
	}

	// Links on first email
	if isInitial {
		links := linkRe.FindAllString(body, -1)
		if len(links) > 1 {
			res.OK = false
			res.Errors = append(res.Errors, fmt.Sprintf("too many links on first email (%d)", len(links)))
		}
	}

	// Generic opening
	if isInitial && isGenericOpening(body) {
		res.Warnings = append(res.Warnings, "generic opening")
		res.Alerts = append(res.Alerts, "generic_opening")
	}

	// SO WHAT: value_to_recipient must exist on strategy for first touch
	if isInitial && st != nil && strings.TrimSpace(st.ValueToRecipient) == "" {
		res.OK = false
		res.Errors = append(res.Errors, "strategy missing value_to_recipient (so-what test)")
	}

	// Hard commercial QA: leak, dump, vocab, empty value, unfulfillable CTA.
	ApplyHardCommercialQA(&res, out, st, pb, channel)

	// Subject: no ALL CAPS shouting, no emoji
	if subj != "" {
		if subj == strings.ToUpper(subj) && utf8.RuneCountInString(subj) > 4 && hasLetter(subj) {
			res.OK = false
			res.Errors = append(res.Errors, "subject is ALL CAPS")
		}
		if containsEmoji(subj) {
			res.OK = false
			res.Errors = append(res.Errors, "emoji in subject not allowed")
		}
	}

	if !res.OK && len(res.Errors) == 0 {
		res.Errors = append(res.Errors, "doctrine validation failed")
	}
	return res
}

func annualidadeBadFromPB(pb *Playbook) []string {
	if pb != nil && len(pb.CopyRules.AnnualidadeBadClaims) > 0 {
		return pb.CopyRules.AnnualidadeBadClaims
	}
	return []string{
		"reajuste a receber", "reajuste não pago", "reajuste nao pago",
		"o órgão deixou de pagar", "o orgao deixou de pagar",
		"crédito de reajuste", "credito de reajuste",
	}
}

func looksLikeHardMonetaryOrCertainty(blob string) bool {
	for _, p := range []string{
		"vocês têm r$", "voces tem r$", "têm r$ a receber", "tem r$ a receber",
		"é certo que", "e certo que", "comprovamos que", "identificamos perda de r$",
	} {
		if strings.Contains(blob, p) {
			return true
		}
	}
	return false
}

func isGenericOpening(body string) bool {
	low := strings.ToLower(strings.TrimSpace(body))
	// First ~80 chars
	if utf8.RuneCountInString(low) > 80 {
		low = string([]rune(low)[:80])
	}
	for _, g := range []string{
		"vi que sua empresa atua",
		"notei que vocês atuam",
		"notei que voces atuam",
		"parabéns pela trajetória",
		"parabens pela trajetoria",
		"empresa incrível",
		"empresa incrivel",
		"vi seu linkedin",
		"vi o site de vocês",
		"vi o site de voces",
	} {
		if strings.Contains(low, g) {
			return true
		}
	}
	return false
}

func hasLetter(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= 'á' && r <= 'ž') {
			return true
		}
	}
	return false
}

func containsEmoji(s string) bool {
	for _, r := range s {
		if r >= 0x1F300 && r <= 0x1FAFF {
			return true
		}
		if r >= 0x2600 && r <= 0x27BF {
			return true
		}
	}
	return false
}

// ApplyHardCommercialQA blocks sendable-looking junk that formal doctrine
// checks used to miss: reasoning leak, metadata dump, service/vocab mismatch,
// empty value, unsupported micro-offer, unfulfillable CTA, fact-then-generic-CTA.
func ApplyHardCommercialQA(res *DoctrineQAResult, out *DraftOutput, st *OutreachStrategy, pb *Playbook, channel string) {
	if res == nil || out == nil {
		return
	}
	body := strings.TrimSpace(out.BodyText)
	subj := strings.TrimSpace(out.Subject)
	blob := subj + "\n" + body
	isInitial := channel == ChannelEmailInitial || channel == ""

	if LooksLikeInternalReasoning(blob) {
		res.OK = false
		res.Errors = append(res.Errors, "internal reasoning leaked into copy")
		res.Alerts = append(res.Alerts, "reasoning_leak")
	}
	if looksLikeMetadataDump(blob) {
		res.OK = false
		res.Errors = append(res.Errors, "metadata dump in copy")
		res.Alerts = append(res.Alerts, "metadata_dump")
	}
	if looksMalformedCurrency(blob) {
		res.OK = false
		res.Errors = append(res.Errors, "malformed currency in copy")
		res.Alerts = append(res.Alerts, "malformed_currency")
	}

	svcCode := ""
	if st != nil {
		svcCode = st.ServiceCode
	}
	if out.ServiceCode != "" {
		svcCode = out.ServiceCode
	}
	if svcCode != "" && creditWordIn(blob) && !CreditVocabAllowed(pb, svcCode) {
		res.OK = false
		res.Errors = append(res.Errors, "service/vocabulary mismatch: crédito")
		res.Alerts = append(res.Alerts, "vocab_mismatch")
	}
	canon := svcCode
	if pb != nil {
		if s := pb.ResolveServicePlaybook(svcCode); s != nil {
			canon = s.Code
		}
	}
	if canon != "" && !serviceUsesContractOpener(canon) && strings.Contains(foldASCII(blob), ContractFramedOpener) {
		res.OK = false
		res.Errors = append(res.Errors, "contract-framed opener on a non-contract service")
		res.Alerts = append(res.Alerts, "hook_frame_mismatch")
	}

	if isInitial && body != "" {
		if isFactThenGenericCTA(body) {
			res.OK = false
			res.Errors = append(res.Errors, "copy only restates the public fact then adds a generic CTA")
			res.Alerts = append(res.Alerts, "empty_value_proposition")
		}
		if isUnnecessaryDisclaimer(blob) {
			res.OK = false
			res.Errors = append(res.Errors, "unnecessary defensive disclaimer")
			res.Alerts = append(res.Alerts, "defensive_disclaimer")
		}
	}

	if st != nil && ctaPromisesPoints(firstNonEmpty(out.CTA, st.CTASuggested)) {
		// A points CTA is only legal when the composer can enumerate checkpoints.
		// Strategy hypotheses are not checkpoints.
		if LooksLikeInternalReasoning(st.ProblemHypothesis) || strings.TrimSpace(st.ProblemHypothesis) == "" {
			if !hasConcreteContractEvent(strings.ToLower(st.ObservedFact + " " + body)) {
				res.OK = false
				res.Errors = append(res.Errors, "CTA promises points the dossier cannot produce")
				res.Alerts = append(res.Alerts, "unfulfillable_cta")
			}
		}
	}
}

func isFactThenGenericCTA(body string) bool {
	// Two short paragraphs: dumped fact + generic permission CTA, no so-what.
	parts := strings.Split(strings.TrimSpace(body), "\n\n")
	if len(parts) < 2 {
		return false
	}
	last := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
	genericCTA := strings.Contains(last, "posso te mandar") || strings.Contains(last, "faz sentido")
	if !genericCTA {
		return false
	}
	middle := strings.Join(parts[1:len(parts)-1], " ")
	if strings.TrimSpace(middle) == "" {
		fact := parts[0]
		if looksLikeMetadataDump(fact) || looksMalformedCurrency(fact) {
			return true
		}
	}
	return false
}

func isUnnecessaryDisclaimer(blob string) bool {
	t := foldASCII(blob)
	for _, p := range []string{
		"isso nao prova credito sozinho",
		"isso nao prova",
		"hipotese, nao credito comprovado",
		"sem afirmar credito",
		"nao e credito comprovado",
	} {
		if strings.Contains(t, p) {
			return true
		}
	}
	return false
}

// MergeDoctrineIntoValidation folds doctrine QA into ValidationResult.
func MergeDoctrineIntoValidation(val *ValidationResult, dqa DoctrineQAResult) {
	if val == nil {
		return
	}
	for _, e := range dqa.Errors {
		val.OK = false
		val.Errors = append(val.Errors, e)
	}
	val.Warnings = append(val.Warnings, dqa.Warnings...)
}
