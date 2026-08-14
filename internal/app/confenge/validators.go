package confenge

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/warmbly/warmbly/internal/models"
)

// PromptVersion tags generation prompt revisions (see docs/confenge/copy-generation.md).
// v4: messageability gate + outbound-safe plan (doctrine confenge-outreach-v2).
const PromptVersion = "confenge.draft.v4"

// DraftClaim is one auditable fact/phrase anchored to evidence ids.
type DraftClaim struct {
	Phrase      string   `json:"phrase"`
	Fact        string   `json:"fact,omitempty"`
	EvidenceIDs []string `json:"evidence_ids"`
}

// DraftOutput is the structured generation result (validated before save).
type DraftOutput struct {
	Channel                string          `json:"channel,omitempty"`
	Subject                string          `json:"subject"`
	BodyText               string          `json:"body_text"`
	BodyHTML               string          `json:"body_html"`
	Followups              []DraftFollowup `json:"followups"`
	FactUsed               string          `json:"fact_used"`
	EvidenceIDs            []string        `json:"evidence_ids"`
	Claims                 []DraftClaim    `json:"claims,omitempty"`
	ServiceCode            string          `json:"service_code"`
	Question               string          `json:"question"`
	CTA                    string          `json:"cta"`
	RiskFlags              []string        `json:"risk_flags"`
	Rationale              string          `json:"rationale,omitempty"`
	ServiceOverrideAudited bool            `json:"service_override_audited,omitempty"`
}

// DraftFollowup is one cadence follow-up in the same thread.
type DraftFollowup struct {
	DelayDays   int    `json:"delay_days"`
	SubjectMode string `json:"subject_mode"`
	BodyText    string `json:"body_text"`
	BodyHTML    string `json:"body_html"`
}

// ValidationResult is deterministic pre-send / pre-approve checks.
type ValidationResult struct {
	OK                   bool                 `json:"ok"`
	Errors               []string             `json:"errors,omitempty"`
	Warnings             []string             `json:"warnings,omitempty"`
	Claims               []DraftClaim         `json:"claims,omitempty"`
	Rationale            string               `json:"rationale,omitempty"`
	Channel              string               `json:"channel,omitempty"`
	NearDupScore         float64              `json:"near_dup_score,omitempty"`
	DoctrineVersion      string               `json:"doctrine_version,omitempty"`
	Strategy             *OutreachStrategy    `json:"strategy,omitempty"`
	StrategyExplain      *StrategyExplain     `json:"strategy_explain,omitempty"`
	DoctrineAlerts       []string             `json:"doctrine_alerts,omitempty"`
	OperatorEdit         *OperatorEditSignal  `json:"operator_edit,omitempty"`
	OperatorReject       *OperatorRejection   `json:"operator_reject,omitempty"`
	Messageability       string               `json:"messageability,omitempty"`
	MessageabilityReason string               `json:"messageability_reason,omitempty"`
	MessagePlan          *OutboundMessagePlan `json:"message_plan,omitempty"`
	Recipient            *RecipientResolution `json:"recipient,omitempty"`
	HumanCorrection      *HumanCorrection     `json:"human_correction,omitempty"`
}

// ValidateOpts configures deterministic validation for a channel.
type ValidateOpts struct {
	MaxWords               int
	Evidence               []models.OutreachEvidence
	Channel                string
	RecentBodies           []string
	ServiceOverrideAudited bool
	SkipEmailRecipient     bool
	Strategy               *OutreachStrategy
	Playbook               *Playbook
}

var bannedPhrases = []string{
	"dinheiro a receber", "crédito identificado", "credito identificado",
	"descobrimos um erro", "há irregularidade", "ha irregularidade",
	"vocês deixaram de receber", "voces deixaram de receber",
	"sua equipe não controla", "sua equipe nao controla", "falta estrutura",
	"lead quente", "alta chance de conversão", "alta chance de conversao",
	"espero que esta mensagem o encontre bem", "espero que esta mensagem a encontre bem",
	"espero que este e-mail o encontre bem", "i hope this email finds you",
	"garantimos", "garantia de", "100% de sucesso",
}

var emDashRe = regexp.MustCompile(`[\x{2014}\x{2013}]`)
var shortURLRe = regexp.MustCompile(`(?i)https?://(bit\.ly|t\.co|goo\.gl|tinyurl\.com|ow\.ly)/`)

// ValidateDraft runs deterministic checks before human approval / enrollment.
func ValidateDraft(out *DraftOutput, acc *models.OutreachAccount, cand *models.OutreachContactCandidate, opts ValidateOpts) ValidationResult {
	var res ValidationResult
	res.OK = true
	if out == nil {
		res.OK = false
		res.Errors = append(res.Errors, "empty draft")
		return res
	}
	channel := strings.TrimSpace(out.Channel)
	if channel == "" {
		channel = strings.TrimSpace(opts.Channel)
	}
	if channel == "" {
		channel = ChannelEmailInitial
	}
	res.Channel = channel
	res.Rationale = strings.TrimSpace(out.Rationale)
	res.Claims = out.Claims

	maxWords := opts.MaxWords
	if maxWords <= 0 {
		if IsWhatsAppChannel(channel) {
			maxWords = DefaultMaxWhatsAppWords
		} else {
			maxWords = DefaultMaxInitialWords
		}
	}
	isWA := IsWhatsAppChannel(channel)
	skipEmail := opts.SkipEmailRecipient || isWA

	if cand != nil {
		if !skipEmail {
			if !cand.CanEnroll() {
				res.OK = false
				res.Errors = append(res.Errors, "contact is not enrollable (verification, DNC, bounce, or missing email)")
			}
			if strings.TrimSpace(cand.Email) == "" {
				res.OK = false
				res.Errors = append(res.Errors, "missing recipient email")
			}
		}
		if cand.DoNotContact {
			res.OK = false
			res.Errors = append(res.Errors, "contact is DO_NOT_CONTACT")
		}
		if cand.Bounced && !isWA {
			res.OK = false
			res.Errors = append(res.Errors, "contact address bounced")
		}
	} else {
		res.OK = false
		res.Errors = append(res.Errors, "no contact candidate")
	}

	body := strings.TrimSpace(out.BodyText)
	if body == "" {
		res.OK = false
		res.Errors = append(res.Errors, "empty body")
	}
	if !isWA && strings.TrimSpace(out.Subject) == "" {
		res.OK = false
		res.Errors = append(res.Errors, "empty subject")
	}

	blob := strings.ToLower(out.Subject + "\n" + body)
	for _, fu := range out.Followups {
		blob += "\n" + strings.ToLower(fu.BodyText)
	}
	if emDashRe.MatchString(out.Subject + body) {
		res.OK = false
		res.Errors = append(res.Errors, "em dash / en dash not allowed in outreach copy")
	}
	for _, fu := range out.Followups {
		if emDashRe.MatchString(fu.BodyText) {
			res.OK = false
			res.Errors = append(res.Errors, "em dash / en dash not allowed in follow-up copy")
			break
		}
	}
	for _, p := range bannedPhrases {
		if strings.Contains(blob, p) {
			res.OK = false
			res.Errors = append(res.Errors, "banned phrase: "+p)
		}
	}
	if shortURLRe.MatchString(body) {
		res.OK = false
		res.Errors = append(res.Errors, "shortened URLs are not allowed")
	}
	if strings.Contains(strings.ToLower(out.BodyHTML), "<script") {
		res.OK = false
		res.Errors = append(res.Errors, "unsafe HTML (script) not allowed")
	}
	words := countWords(body)
	if words > maxWords {
		res.OK = false
		res.Errors = append(res.Errors, fmt.Sprintf("body exceeds %d words (%d)", maxWords, words))
	}

	company := ""
	if acc != nil {
		company = firstNonEmpty(acc.NomeFantasia, acc.RazaoSocial)
	}
	lint := LintCopy(channel, out.Subject, body, company)
	for _, e := range lint.Errors {
		res.OK = false
		res.Errors = append(res.Errors, e)
	}
	res.Warnings = append(res.Warnings, lint.Warnings...)

	knownIDs := evidenceIDSet(opts.Evidence, acc)
	allClaimIDs := collectEvidenceIDs(out)
	if len(allClaimIDs) == 0 && strings.TrimSpace(out.FactUsed) != "" {
		if len(knownIDs) == 0 {
			res.Warnings = append(res.Warnings, "fact used without evidence_ids")
		} else {
			res.OK = false
			res.Errors = append(res.Errors, "fact_used present but no evidence_ids anchored")
		}
	}
	for _, id := range allClaimIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if !knownIDs[id] {
			res.OK = false
			res.Errors = append(res.Errors, "unknown evidence_id: "+id)
		}
	}
	for i, c := range out.Claims {
		if len(c.EvidenceIDs) == 0 && len(knownIDs) > 0 {
			res.OK = false
			res.Errors = append(res.Errors, fmt.Sprintf("claims[%d] missing evidence_ids", i))
		}
		for _, id := range c.EvidenceIDs {
			if id = strings.TrimSpace(id); id != "" && !knownIDs[id] {
				res.OK = false
				res.Errors = append(res.Errors, "unknown evidence_id in claim: "+id)
			}
		}
	}
	if strings.TrimSpace(out.FactUsed) == "" && !isWA {
		if len(out.Claims) == 0 {
			res.OK = false
			res.Errors = append(res.Errors, "fact_used is required")
		}
	}

	override := opts.ServiceOverrideAudited || out.ServiceOverrideAudited
	if acc != nil {
		if sc := strings.TrimSpace(out.ServiceCode); sc != "" && acc.ServiceCode != "" && !strings.EqualFold(sc, acc.ServiceCode) {
			if !override {
				res.OK = false
				res.Errors = append(res.Errors, "service_code does not match account offer")
			} else {
				res.Warnings = append(res.Warnings, "service_code human override audited")
			}
		}
		if countServiceMentions(body) > 1 {
			res.OK = false
			res.Errors = append(res.Errors, "body appears to offer more than one service")
		}
		for _, avoid := range acc.ClaimsToAvoid {
			avoid = strings.TrimSpace(strings.ToLower(avoid))
			if avoid != "" && strings.Contains(blob, avoid) {
				res.OK = false
				res.Errors = append(res.Errors, "claims_to_avoid violated: "+avoid)
			}
		}
	}

	for _, e := range opts.Evidence {
		if e.EpistemicClass != models.OutreachEpistemicCommercialHypothesis &&
			e.EpistemicClass != models.OutreachEpistemicWeakInference &&
			e.EpistemicClass != models.OutreachEpistemicRequiresCompanyConfirm {
			continue
		}
		for _, c := range out.Claims {
			if !containsStr(c.EvidenceIDs, e.SourceEvidenceID) {
				continue
			}
			phrase := strings.ToLower(c.Phrase + " " + c.Fact)
			if looksLikeHardAssertion(phrase) {
				res.OK = false
				res.Errors = append(res.Errors, "hypothesis evidence asserted as hard fact: "+e.SourceEvidenceID)
			}
		}
	}

	qMarks := strings.Count(body, "?")
	if channel == ChannelEmailInitial {
		if qMarks == 0 {
			res.Warnings = append(res.Warnings, "email initial has no question mark")
		}
		if qMarks > 2 {
			res.Warnings = append(res.Warnings, "body has multiple question marks")
		}
	}
	if isWA && qMarks == 0 {
		res.Warnings = append(res.Warnings, "whatsapp body has no question")
	}
	if score, hit := NearDuplicate(body, opts.RecentBodies); hit {
		res.NearDupScore = score
		res.Warnings = append(res.Warnings, fmt.Sprintf("near-duplicate of recent draft (jaccard=%.2f)", score))
	} else if score > 0 {
		res.NearDupScore = score
	}
	if r := strings.TrimSpace(out.Rationale); r != "" && len(r) > 20 {
		sample := r
		if utf8.RuneCountInString(sample) > 24 {
			sample = string([]rune(sample)[:24])
		}
		if sample != "" && strings.Contains(body, sample) {
			res.OK = false
			res.Errors = append(res.Errors, "internal rationale leaked into body_text")
		}
	}

	// Doctrine QA (strategy-first commercial constraints).
	st := opts.Strategy
	pb := opts.Playbook
	dqa := ValidateDoctrineCopy(out, st, pb, channel)
	MergeDoctrineIntoValidation(&res, dqa)
	res.DoctrineAlerts = append(res.DoctrineAlerts, dqa.Alerts...)
	if st != nil {
		res.DoctrineVersion = st.DoctrineVersion
		res.Strategy = st
	} else {
		res.DoctrineVersion = OutreachDoctrineVersion
	}

	if !res.OK && len(res.Errors) == 0 {
		res.Errors = append(res.Errors, "validation failed")
	}
	return res
}

func evidenceIDSet(evidence []models.OutreachEvidence, acc *models.OutreachAccount) map[string]bool {
	m := make(map[string]bool)
	for _, e := range evidence {
		if id := strings.TrimSpace(e.SourceEvidenceID); id != "" {
			m[id] = true
		}
		if e.ID.String() != "00000000-0000-0000-0000-000000000000" {
			m[e.ID.String()] = true
		}
	}
	if acc != nil {
		for _, id := range acc.MomentEvidenceIDs {
			if id = strings.TrimSpace(id); id != "" {
				m[id] = true
			}
		}
	}
	return m
}

func collectEvidenceIDs(out *DraftOutput) []string {
	if out == nil {
		return nil
	}
	seen := map[string]bool{}
	var outIDs []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		outIDs = append(outIDs, id)
	}
	for _, id := range out.EvidenceIDs {
		add(id)
	}
	for _, c := range out.Claims {
		for _, id := range c.EvidenceIDs {
			add(id)
		}
	}
	return outIDs
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func looksLikeHardAssertion(phrase string) bool {
	phrase = strings.ToLower(phrase)
	if strings.Contains(phrase, "?") ||
		strings.Contains(phrase, "faz sentido") ||
		strings.Contains(phrase, "hipótese") ||
		strings.Contains(phrase, "hipotese") ||
		strings.Contains(phrase, "parece que") ||
		strings.Contains(phrase, "talvez") ||
		strings.Contains(phrase, "seria o caso") ||
		strings.Contains(phrase, "gostaria de entender") ||
		strings.Contains(phrase, "confirmar se") {
		return false
	}
	for _, h := range []string{"não têm", "nao tem", "não possui", "nao possui", "falta de", "sem equipe", "não controla", "nao controla", "é certo que", "e certo que"} {
		if strings.Contains(phrase, h) {
			return true
		}
	}
	return false
}

func countWords(s string) int { return len(strings.Fields(s)) }

func firstWords(s string, n int) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	if len(f) < n {
		n = len(f)
	}
	return strings.ToLower(strings.Join(f[:n], " "))
}

func countServiceMentions(body string) int {
	patterns := []string{"além disso oferecemos", "alem disso oferecemos", "também fazemos", "tambem fazemos", "nossos serviços incluem", "nossos servicos incluem"}
	n := 0
	low := strings.ToLower(body)
	for _, p := range patterns {
		if strings.Contains(low, p) {
			n++
		}
	}
	return n
}

// ClassifyRisk returns GREEN/YELLOW/RED send-risk (not lead value).
func ClassifyRisk(acc *models.OutreachAccount, cand *models.OutreachContactCandidate, out *DraftOutput, val ValidationResult) (class string, flags []string) {
	flags = []string{}
	class = "GREEN"
	raise := func(to string, flag string) {
		flags = append(flags, flag)
		if rank(to) > rank(class) {
			class = to
		}
	}
	if !val.OK {
		raise("RED", "validation_failed")
	}
	if val.NearDupScore >= NearDupThreshold {
		raise("YELLOW", "near_duplicate_draft")
	}
	if cand != nil {
		switch cand.VerificationStatus {
		case models.OutreachVerifyOfficialSource, models.OutreachVerifyMultipleSources,
			models.OutreachVerifyPublicDocumentRecent, models.OutreachVerifyVerified,
			models.OutreachVerifyHumanConfirmed:
			// Enrollable verified contacts stay GREEN-eligible.
		case models.OutreachVerifyInstitutionalGeneric:
			raise("YELLOW", "institutional_generic_recipient")
		case models.OutreachVerifyPublicPossiblyStale:
			raise("YELLOW", "possibly_stale_contact")
		default:
			raise("RED", "weak_or_blocked_verification")
		}
		role := strings.ToLower(cand.Role)
		for _, k := range []string{"ceo", "cfo", "presidente", "sócio", "socio", "diretor geral"} {
			if strings.Contains(role, k) {
				raise("RED", "senior_executive_recipient")
				break
			}
		}
	}
	if acc != nil {
		code := strings.ToUpper(acc.MomentCode + " " + acc.ServiceCode + " " + acc.MomentSummary)
		for _, k := range []string{"CREDIT", "CRÉDITO", "CREDITO", "REEQUILIB", "SANCTION", "SANÇÃO", "SANCAO", "LITIG", "CONSÓRCIO", "CONSORCIO", "CONSORTIUM"} {
			if strings.Contains(code, k) {
				raise("RED", "sensitive_moment_or_service")
				break
			}
		}
		// REAJUSTE/ADITIVO/PRORROG are core CONFENGE product moments — informational
		// only, not automatic YELLOW. Human review of generic templates still uses
		// template_fallback demotion when policy does not authorize template GREEN.
		if acc.FactToMention == "" {
			raise("YELLOW", "missing_public_fact")
		}
	}
	if out != nil {
		blob := strings.ToLower(out.BodyText + " " + out.Subject)
		for _, k := range []string{"crédito", "credito", "reequilíbrio", "reequilibrio", "litígio", "litigio", "sanção", "sancao"} {
			if strings.Contains(blob, k) {
				raise("RED", "economic_or_legal_claim_language")
				break
			}
		}
		for _, f := range out.RiskFlags {
			if f != "" {
				flags = append(flags, f)
			}
		}
	}
	if class == "GREEN" && len(flags) == 0 {
		flags = []string{"low_send_risk"}
	}
	return class, flags
}

func rank(c string) int {
	switch c {
	case "RED":
		return 3
	case "YELLOW":
		return 2
	default:
		return 1
	}
}

// TemplateDraft builds a deterministic safe draft when AI is unavailable.
func TemplateDraft(acc *models.OutreachAccount, cand *models.OutreachContactCandidate) DraftOutput {
	return TemplateDraftChannel(ChannelEmailInitial, acc, cand, nil)
}

// TemplateDraftChannel builds channel-aware deterministic copy from the
// outbound-safe plan only. Not READY means fail-closed, no sendable body.
func TemplateDraftChannel(channel string, acc *models.OutreachAccount, cand *models.OutreachContactCandidate, evidence []models.OutreachEvidence) DraftOutput {
	if channel == "" {
		channel = ChannelEmailInitial
	}
	pb, _ := LoadPlaybook()
	_, plan := BuildOutboundPlan(pb, acc, cand, evidence, sequencePosFromChannel(channel, nil))
	if plan.Messageability != MessageabilityReady {
		return FailClosedDraft(plan, channel)
	}
	return ComposeFromPlan(plan, acc, cand, channel)
}

func evidenceIDsFrom(evidence []models.OutreachEvidence, acc *models.OutreachAccount) []string {
	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	for _, e := range evidence {
		add(e.SourceEvidenceID)
	}
	if acc != nil {
		for _, id := range acc.MomentEvidenceIDs {
			add(id)
		}
	}
	return ids
}

func claimsFromFact(fact string, ids []string) []DraftClaim {
	fact = strings.TrimSpace(fact)
	if fact == "" {
		return nil
	}
	return []DraftClaim{{Phrase: fact, Fact: fact, EvidenceIDs: ids}}
}

func trimSubject(s string) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= 80 {
		return s
	}
	return string([]rune(s)[:77]) + "..."
}

func defaultFollowups(question string) []DraftFollowup {
	return []DraftFollowup{
		{DelayDays: 3, SubjectMode: "same_thread", BodyText: "Reenvio com uma pergunta mais objetiva: " + question},
		{DelayDays: 7, SubjectMode: "same_thread", BodyText: "Se não for com você, pode me indicar a pessoa certa de contratos ou engenharia?"},
		{DelayDays: 14, SubjectMode: "same_thread", BodyText: "Encerro por aqui para não ocupar sua caixa. Se fizer sentido no futuro, é só responder este fio."},
	}
}

func firstName(full string) string {
	parts := strings.Fields(full)
	if len(parts) == 0 {
		return full
	}
	return parts[0]
}

// CanBatchApprove reports whether a draft may be approved in bulk.
func CanBatchApprove(d *models.OutreachDraft, cand *models.OutreachContactCandidate) bool {
	if d == nil || cand == nil {
		return false
	}
	if d.Status != models.OutreachDraftNeedsReview && d.Status != models.OutreachDraftApproved {
		return false
	}
	if d.RiskClass != "GREEN" {
		return false
	}
	if !cand.CanEnroll() {
		return false
	}
	if d.ValidationOK != nil && !*d.ValidationOK {
		return false
	}
	return true
}
