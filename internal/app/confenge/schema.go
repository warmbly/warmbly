package confenge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/warmbly/warmbly/internal/models"
)

// Native feed contract: confenge.outreach.v1

// Feed is the top-level document produced by extra-cli (or fixtures).
type Feed struct {
	SchemaVersion string         `json:"schema_version"`
	GeneratedAt   string         `json:"generated_at"`
	Source        FeedSource     `json:"source"`
	Pagination    FeedPagination `json:"pagination"`
	Leads         []FeedLead     `json:"leads"`
	Legacy        bool           `json:"-"`
}

// FeedSource identifies the producing intelligence-plane run.
type FeedSource struct {
	System         string `json:"system"`
	RunID          string `json:"run_id"`
	SnapshotHash   string `json:"snapshot_hash"`
	RepoSHA        string `json:"repo_sha"`
	ProfileID      string `json:"profile_id"`
	ProfileVersion string `json:"profile_version"`
}

// FeedPagination supports paged feeds; all fields optional.
type FeedPagination struct {
	Cursor     *string `json:"cursor"`
	NextCursor *string `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}

// FeedLead is one company opportunity in the feed.
type FeedLead struct {
	SourceLeadID     string            `json:"source_lead_id"`
	Company          FeedCompany       `json:"company"`
	Priority         FeedPriority      `json:"priority"`
	Moment           FeedMoment        `json:"moment"`
	Offer            FeedOffer         `json:"offer"`
	MessagingContext FeedMessaging     `json:"messaging_context"`
	Contacts         []FeedContact     `json:"contacts"`
	Contracts        []json.RawMessage `json:"contracts"`
	Evidence         []FeedEvidence    `json:"evidence"`
	CommercialState  string            `json:"commercial_state"`
	// Activation is optional (additive confenge.outreach.v1). Absent in legacy feeds.
	Activation *FeedActivation `json:"activation,omitempty"`
	// Send-fit imported from extra-cli. Absent in legacy feeds → autorun forbidden.
	// Never derive TargetFitSendTier from Activation.State.
	TargetFitSendTier        string   `json:"target_fit_send_tier,omitempty"`
	TargetFitReasons         []string `json:"target_fit_reasons,omitempty"`
	TargetFitClass           string   `json:"target_fit_class,omitempty"`
	TargetFitConfidence      *float64 `json:"target_fit_confidence,omitempty"`
	TargetFitVersion         string   `json:"target_fit_version,omitempty"`
	TargetFitComputedAt      string   `json:"target_fit_computed_at,omitempty"`
	TargetFitSourceWatermark string   `json:"target_fit_source_watermark,omitempty"`
	TargetFitFresh           *bool    `json:"target_fit_fresh,omitempty"`
	TargetFitEvidenceIDs     []string `json:"target_fit_evidence_ids,omitempty"`
	TargetFitFreshnessReason string   `json:"target_fit_freshness_reason,omitempty"`
	EmailSendReady           *bool    `json:"email_send_ready,omitempty"`
	MailboxPurpose           string   `json:"mailbox_purpose,omitempty"`
	OwnershipStatus          string   `json:"ownership_status,omitempty"`
	// Additive extra-cli Decision-Unit + Reachability (unknown fields tolerated).
	DecisionUnitCandidates []json.RawMessage `json:"decision_unit_candidates,omitempty"`
	ReachabilityRoutes     []json.RawMessage `json:"reachability_routes,omitempty"`
	RecommendedTarget      json.RawMessage   `json:"recommended_target,omitempty"`
	RecommendedRoute       json.RawMessage   `json:"recommended_route,omitempty"`
	RecommendedAction      string            `json:"recommended_action,omitempty"`
}

// FeedActivation is the extra-cli commercial activation planner projection.
// activation score is ordering only — never purchase/conversion probability.
type FeedActivation struct {
	State            string             `json:"state"`
	Score            float64            `json:"score"`
	ReasonCodes      []string           `json:"reason_codes"`
	PolicyVersion    string             `json:"policy_version"`
	EvaluatedAt      string             `json:"evaluated_at"`
	NextBestActionAt string             `json:"next_best_action_at"`
	ExpiresAt        string             `json:"expires_at"`
	SourceHash       string             `json:"source_hash"`
	ScoreComponents  map[string]float64 `json:"score_components"`
}

// Allowed activation states from extra-cli planner.
const (
	ActivationWatch            = "WATCH"
	ActivationResearchRequired = "RESEARCH_REQUIRED"
	ActivationActionableNow    = "ACTIONABLE_NOW"
	ActivationSuppressed       = "SUPPRESSED"
)

// FeedCompany is Brazilian company identity from the datalake.
type FeedCompany struct {
	CNPJ14       string `json:"cnpj14"`
	CNPJRoot     string `json:"cnpj_root"`
	RazaoSocial  string `json:"razao_social"`
	NomeFantasia string `json:"nome_fantasia"`
	Municipio    string `json:"municipio"`
	UF           string `json:"uf"`
	Website      string `json:"website"`
}

// FeedPriority is rank/score from extra-cli (stored, never re-scored here).
type FeedPriority struct {
	Rank       int     `json:"rank"`
	Score      float64 `json:"score"`
	Tier       string  `json:"tier"`
	Confidence string  `json:"confidence"`
}

// FeedMoment is the commercial moment / fato gerador.
type FeedMoment struct {
	Code        string   `json:"code"`
	Summary     string   `json:"summary"`
	ObservedAt  string   `json:"observed_at"`
	Confidence  string   `json:"confidence"`
	EvidenceIDs []string `json:"evidence_ids"`
}

// FeedOffer is the suggested service entry offer.
type FeedOffer struct {
	ServiceCode string `json:"service_code"`
	ServiceName string `json:"service_name"`
	EntryOffer  string `json:"entry_offer"`
	Rationale   string `json:"rationale"`
}

// FeedMessaging is structured messaging context for generation.
type FeedMessaging struct {
	FactToMention string   `json:"fact_to_mention"`
	QuestionToAsk string   `json:"question_to_ask"`
	CTA           string   `json:"cta"`
	ClaimsToAvoid []string `json:"claims_to_avoid"`
}

// FeedContact is a candidate recipient (may lack email).
// Phone remains a legacy string; PhoneObj + WhatsApp are optional additive
// fields for structured provenance (backward compatible with older feeds).
type FeedContact struct {
	SourceContactID    string        `json:"source_contact_id"`
	Name               string        `json:"name"`
	Role               string        `json:"role"`
	Email              string        `json:"email"`
	Phone              string        `json:"phone"`
	PhoneObj           *FeedPhone    `json:"phone_detail,omitempty"`
	WhatsApp           *FeedWhatsApp `json:"whatsapp,omitempty"`
	LinkedInURL        string        `json:"linkedin_url"`
	SourceURL          string        `json:"source_url"`
	SourceDocument     string        `json:"source_document"`
	SourceDate         string        `json:"source_date"`
	VerificationStatus string        `json:"verification_status"`
	Confidence         string        `json:"confidence"`
	Recommended        bool          `json:"recommended"`
	// Imported readiness — never inferred by Warmbly from email syntax.
	EmailSendReady                 *bool  `json:"email_send_ready,omitempty"`
	MailboxPurpose                 string `json:"mailbox_purpose,omitempty"`
	MailboxPurposeSendBlocked      *bool  `json:"mailbox_purpose_send_blocked,omitempty"`
	OwnershipStatus                string `json:"ownership_status,omitempty"`
	RecipientCommercialSuitability string `json:"recipient_commercial_suitability,omitempty"`
	// Provenance trust fields (extra-cli). Optional; absence + demo patterns still fail closed.
	ProvenanceChainValid *bool  `json:"provenance_chain_valid,omitempty"`
	ProvenanceTrust      string `json:"provenance_trust,omitempty"`
	RootSourceType       string `json:"root_source_type,omitempty"`
	DerivedFromFixture   *bool  `json:"derived_from_fixture,omitempty"`
	ContactTier          string `json:"contact_tier,omitempty"`
	Channel              string `json:"channel,omitempty"`
	// Additive Decision-Unit / Reachability boundary. Unknown extra-cli
	// strings are tolerated; Warmbly never invents a class when absent.
	ReachabilityClass string `json:"reachability_class,omitempty"`
	RouteType         string `json:"route_type,omitempty"`
	RouteRelation     string `json:"route_relation,omitempty"`
	ChannelValue      string `json:"channel_value,omitempty"`
	ChannelDisplay    string `json:"channel_display,omitempty"`
	RecommendedAction string `json:"recommended_action,omitempty"`
	InferredEmail     *bool  `json:"inferred_email,omitempty"`
	PersonID          string `json:"person_id,omitempty"`
	ActionMode        string `json:"action_mode,omitempty"`
}

// FeedEvidence is one evidence item (text only; HTML stripped on import).
type FeedEvidence struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	Title          string `json:"title"`
	URL            string `json:"url"`
	Document       string `json:"document"`
	Date           string `json:"date"`
	Location       string `json:"location"`
	Excerpt        string `json:"excerpt"`
	Synthesis      string `json:"synthesis"`
	EpistemicClass string `json:"epistemic_class"`
	Reliability    string `json:"reliability"`
	ConsultedAt    string `json:"consulted_at"`
}

var (
	cnpj14Re   = regexp.MustCompile(`^\d{14}$`)
	cnpjRootRe = regexp.MustCompile(`^\d{8}$`)
)

var allowedVerification = map[string]bool{
	models.OutreachVerifyOfficialSource:       true,
	models.OutreachVerifyPublicDocumentRecent: true,
	models.OutreachVerifyMultipleSources:      true,
	models.OutreachVerifyInstitutionalGeneric: true,
	models.OutreachVerifyPublicPossiblyStale:  true,
	models.OutreachVerifyCandidateUnverified:  true,
	models.OutreachVerifyNotFound:             true,
	models.OutreachVerifyInvalid:              true,
	models.OutreachVerifyBounced:              true,
	models.OutreachVerifyDoNotContact:         true,
	models.OutreachVerifyHumanConfirmed:       true,
	models.OutreachVerifyVerified:             true,
}

var allowedEpistemic = map[string]bool{
	models.OutreachEpistemicConfirmedFact:          true,
	models.OutreachEpistemicStrongInference:        true,
	models.OutreachEpistemicWeakInference:          true,
	models.OutreachEpistemicCommercialHypothesis:   true,
	models.OutreachEpistemicNotFound:               true,
	models.OutreachEpistemicRequiresCompanyConfirm: true,
	models.OutreachEpistemicContradictoryEvidence:  true,
}

// ParseFeed unmarshals JSON bytes into a Feed. Does not invent missing fields.
func ParseFeed(raw []byte) (*Feed, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty payload")
	}
	var f Feed
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return &f, nil
}

// ValidateFeed checks schema_version and required identity fields.
// Per-lead errors are returned separately so a run can continue.
func ValidateFeed(f *Feed) error {
	if f == nil {
		return fmt.Errorf("nil feed")
	}
	if f.SchemaVersion != models.OutreachSchemaV1 {
		return fmt.Errorf("unsupported schema_version %q (want %s)", f.SchemaVersion, models.OutreachSchemaV1)
	}
	if strings.TrimSpace(f.Source.System) == "" {
		return fmt.Errorf("source.system is required")
	}
	if strings.TrimSpace(f.GeneratedAt) == "" && f.Legacy {
		return nil
	}
	generatedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(f.GeneratedAt))
	if err != nil {
		return fmt.Errorf("generated_at must be an RFC3339 timestamp")
	}
	if generatedAt.After(time.Now().UTC().Add(5 * time.Minute)) {
		return fmt.Errorf("generated_at cannot be in the future")
	}
	return nil
}

// LeadValidationError is a non-fatal per-lead problem.
type LeadValidationError struct {
	Index        int
	SourceLeadID string
	CNPJ14       string
	Message      string
}

// ValidateLead checks one lead. Missing contact is allowed (NEEDS_CONTACT).
// Missing activation is allowed (legacy feeds). Invalid activation fails closed.
func ValidateLead(i int, lead FeedLead) *LeadValidationError {
	cnpj := digitsOnly(lead.Company.CNPJ14)
	sid := strings.TrimSpace(lead.SourceLeadID)
	if cnpj == "" {
		return &LeadValidationError{Index: i, SourceLeadID: sid, Message: "company.cnpj14 is required"}
	}
	if !cnpj14Re.MatchString(cnpj) {
		return &LeadValidationError{Index: i, SourceLeadID: sid, CNPJ14: cnpj, Message: "company.cnpj14 must be 14 digits"}
	}
	if root := digitsOnly(lead.Company.CNPJRoot); root != "" && !cnpjRootRe.MatchString(root) {
		return &LeadValidationError{Index: i, SourceLeadID: sid, CNPJ14: cnpj, Message: "company.cnpj_root must be 8 digits when set"}
	}
	if strings.TrimSpace(lead.Company.RazaoSocial) == "" && strings.TrimSpace(lead.Company.NomeFantasia) == "" {
		return &LeadValidationError{Index: i, SourceLeadID: sid, CNPJ14: cnpj, Message: "company needs razao_social or nome_fantasia"}
	}
	for j, c := range lead.Contacts {
		vs := strings.TrimSpace(c.VerificationStatus)
		if vs != "" && !allowedVerification[vs] {
			return &LeadValidationError{
				Index: i, SourceLeadID: sid, CNPJ14: cnpj,
				Message: fmt.Sprintf("contacts[%d].verification_status %q is not allowed", j, vs),
			}
		}
	}
	for j, e := range lead.Evidence {
		ec := strings.TrimSpace(e.EpistemicClass)
		if ec != "" && !allowedEpistemic[ec] {
			return &LeadValidationError{
				Index: i, SourceLeadID: sid, CNPJ14: cnpj,
				Message: fmt.Sprintf("evidence[%d].epistemic_class %q is not allowed", j, ec),
			}
		}
		if strings.TrimSpace(e.ID) == "" {
			return &LeadValidationError{
				Index: i, SourceLeadID: sid, CNPJ14: cnpj,
				Message: fmt.Sprintf("evidence[%d].id is required", j),
			}
		}
	}
	if lead.Activation != nil {
		if err := validateActivation(lead.Activation); err != "" {
			return &LeadValidationError{Index: i, SourceLeadID: sid, CNPJ14: cnpj, Message: err}
		}
	}
	return nil
}

func validateActivation(a *FeedActivation) string {
	if a == nil {
		return ""
	}
	st := strings.ToUpper(strings.TrimSpace(a.State))
	switch st {
	case ActivationWatch, ActivationResearchRequired, ActivationActionableNow, ActivationSuppressed:
	default:
		return fmt.Sprintf("activation.state %q is not allowed", a.State)
	}
	if a.Score < 0 || a.Score > 100 {
		return fmt.Sprintf("activation.score %.4f out of range 0–100", a.Score)
	}
	if st == ActivationActionableNow {
		if len(a.ReasonCodes) == 0 {
			return "activation ACTIONABLE_NOW requires reason_codes"
		}
		if strings.TrimSpace(a.NextBestActionAt) == "" {
			return "activation ACTIONABLE_NOW requires next_best_action_at"
		}
	}
	return ""
}

// CanonicalPayloadHash is a stable SHA-256 of the raw payload bytes (hex).
// Callers that re-serialize should use the original bytes for idempotency.
func CanonicalPayloadHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// LeadContentHash hashes the lead's machine-owned fields for change detection.
func LeadContentHash(lead FeedLead) string {
	// Exclude human-only outcomes; include messaging, priority, moment, offer, contacts, evidence ids.
	type slim struct {
		SourceLeadID             string          `json:"source_lead_id"`
		Company                  FeedCompany     `json:"company"`
		Priority                 FeedPriority    `json:"priority"`
		Moment                   FeedMoment      `json:"moment"`
		Offer                    FeedOffer       `json:"offer"`
		Messaging                FeedMessaging   `json:"messaging_context"`
		Contacts                 []FeedContact   `json:"contacts"`
		Evidence                 []FeedEvidence  `json:"evidence"`
		State                    string          `json:"commercial_state"`
		Activation               *FeedActivation `json:"activation,omitempty"`
		TargetFitClass           string          `json:"target_fit_class,omitempty"`
		TargetFitVersion         string          `json:"target_fit_version,omitempty"`
		TargetFitComputedAt      string          `json:"target_fit_computed_at,omitempty"`
		TargetFitSourceWatermark string          `json:"target_fit_source_watermark,omitempty"`
		TargetFitFresh           *bool           `json:"target_fit_fresh,omitempty"`
		TargetFitSendTier        string          `json:"target_fit_send_tier,omitempty"`
		TargetFitEvidenceIDs     []string        `json:"target_fit_evidence_ids,omitempty"`
		EmailSendReady           *bool           `json:"email_send_ready,omitempty"`
	}
	b, _ := json.Marshal(slim{
		SourceLeadID:   lead.SourceLeadID,
		Company:        lead.Company,
		Priority:       lead.Priority,
		Moment:         lead.Moment,
		Offer:          lead.Offer,
		Messaging:      lead.MessagingContext,
		Contacts:       lead.Contacts,
		Evidence:       lead.Evidence,
		State:          lead.CommercialState,
		Activation:     lead.Activation,
		TargetFitClass: lead.TargetFitClass, TargetFitVersion: lead.TargetFitVersion,
		TargetFitComputedAt: lead.TargetFitComputedAt, TargetFitSourceWatermark: lead.TargetFitSourceWatermark,
		TargetFitFresh: lead.TargetFitFresh, TargetFitSendTier: lead.TargetFitSendTier,
		TargetFitEvidenceIDs: lead.TargetFitEvidenceIDs, EmailSendReady: lead.EmailSendReady,
	})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// MessageContextHash hashes only fields that change message truthfulness.
// Rank / activation score alone must NOT change this hash (no false stale invalidation).
func MessageContextHash(lead FeedLead) string {
	type contactSlim struct {
		Email           string `json:"email"`
		Name            string `json:"name"`
		Role            string `json:"role"`
		Phone           string `json:"phone"`
		Verify          string `json:"verification_status"`
		Rec             bool   `json:"recommended"`
		EmailReady      *bool  `json:"email_send_ready"`
		MailboxPurpose  string `json:"mailbox_purpose"`
		PurposeBlocked  *bool  `json:"mailbox_purpose_send_blocked"`
		Ownership       string `json:"ownership_status"`
		Suitability     string `json:"recipient_commercial_suitability"`
		SourceURL       string `json:"source_url"`
		SourceDocument  string `json:"source_document"`
		SourceDate      string `json:"source_date"`
		ProvenanceValid *bool  `json:"provenance_chain_valid"`
		ProvenanceTrust string `json:"provenance_trust"`
		RootSourceType  string `json:"root_source_type"`
		FixtureDerived  *bool  `json:"derived_from_fixture"`
	}
	type evidSlim struct {
		ID             string `json:"id"`
		Type           string `json:"type"`
		Title          string `json:"title"`
		URL            string `json:"url"`
		Document       string `json:"document"`
		Date           string `json:"date"`
		Location       string `json:"location"`
		Excerpt        string `json:"excerpt"`
		Synthesis      string `json:"synthesis"`
		EpistemicClass string `json:"epistemic_class"`
		Reliability    string `json:"reliability"`
		ConsultedAt    string `json:"consulted_at"`
	}
	contacts := make([]contactSlim, 0, len(lead.Contacts))
	for _, c := range lead.Contacts {
		contacts = append(contacts, contactSlim{
			Email: strings.TrimSpace(strings.ToLower(c.Email)), Name: strings.TrimSpace(c.Name),
			Role: strings.TrimSpace(c.Role), Phone: strings.TrimSpace(c.Phone), Verify: strings.TrimSpace(c.VerificationStatus),
			Rec: c.Recommended, EmailReady: c.EmailSendReady, MailboxPurpose: strings.TrimSpace(c.MailboxPurpose),
			PurposeBlocked: c.MailboxPurposeSendBlocked, Ownership: strings.TrimSpace(c.OwnershipStatus),
			Suitability: strings.TrimSpace(c.RecipientCommercialSuitability), SourceURL: strings.TrimSpace(c.SourceURL),
			SourceDocument: strings.TrimSpace(c.SourceDocument), SourceDate: strings.TrimSpace(c.SourceDate),
			ProvenanceValid: c.ProvenanceChainValid, ProvenanceTrust: strings.TrimSpace(c.ProvenanceTrust),
			RootSourceType: strings.TrimSpace(c.RootSourceType), FixtureDerived: c.DerivedFromFixture,
		})
	}
	evid := make([]evidSlim, 0, len(lead.Evidence))
	for _, e := range lead.Evidence {
		evid = append(evid, evidSlim{
			ID: e.ID, Type: e.Type, Title: e.Title, URL: e.URL, Document: e.Document,
			Date: e.Date, Location: e.Location, Excerpt: e.Excerpt, Synthesis: e.Synthesis,
			EpistemicClass: e.EpistemicClass, Reliability: e.Reliability, ConsultedAt: e.ConsultedAt,
		})
	}
	// Material activation trigger identity only (not score/rank).
	var actSrc, actReasons string
	if lead.Activation != nil {
		actSrc = strings.TrimSpace(lead.Activation.SourceHash)
		actReasons = strings.Join(lead.Activation.ReasonCodes, ",")
	}
	type slim struct {
		Company                  FeedCompany       `json:"company"`
		Moment                   FeedMoment        `json:"moment"`
		Offer                    FeedOffer         `json:"offer"`
		Messaging                FeedMessaging     `json:"messaging_context"`
		Contacts                 []contactSlim     `json:"contacts"`
		Contracts                []json.RawMessage `json:"contracts"`
		Evidence                 []evidSlim        `json:"evidence"`
		ActSrc                   string            `json:"activation_source_hash"`
		ActCodes                 string            `json:"activation_reason_codes"`
		TargetFitClass           string            `json:"target_fit_class"`
		TargetFitConfidence      *float64          `json:"target_fit_confidence"`
		TargetFitVersion         string            `json:"target_fit_version"`
		TargetFitComputedAt      string            `json:"target_fit_computed_at"`
		TargetFitSourceWatermark string            `json:"target_fit_source_watermark"`
		TargetFitFresh           *bool             `json:"target_fit_fresh"`
		TargetFitFreshnessReason string            `json:"target_fit_freshness_reason"`
		TargetFitEvidenceIDs     []string          `json:"target_fit_evidence_ids"`
		TargetFitSendTier        string            `json:"target_fit_send_tier"`
		TargetFitReasons         []string          `json:"target_fit_reasons"`
		EmailSendReady           *bool             `json:"email_send_ready"`
		MailboxPurpose           string            `json:"mailbox_purpose"`
		OwnershipStatus          string            `json:"ownership_status"`
	}
	b, _ := json.Marshal(slim{
		Company:        lead.Company,
		Moment:         lead.Moment,
		Offer:          lead.Offer,
		Messaging:      lead.MessagingContext,
		Contacts:       contacts,
		Contracts:      lead.Contracts,
		Evidence:       evid,
		ActSrc:         actSrc,
		ActCodes:       actReasons,
		TargetFitClass: lead.TargetFitClass, TargetFitConfidence: lead.TargetFitConfidence,
		TargetFitVersion: lead.TargetFitVersion, TargetFitComputedAt: lead.TargetFitComputedAt,
		TargetFitSourceWatermark: lead.TargetFitSourceWatermark, TargetFitFresh: lead.TargetFitFresh,
		TargetFitFreshnessReason: lead.TargetFitFreshnessReason, TargetFitEvidenceIDs: lead.TargetFitEvidenceIDs,
		TargetFitSendTier: lead.TargetFitSendTier, TargetFitReasons: lead.TargetFitReasons,
		EmailSendReady: lead.EmailSendReady, MailboxPurpose: lead.MailboxPurpose, OwnershipStatus: lead.OwnershipStatus,
	})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// NormalizeCNPJ14 returns digits-only 14-char CNPJ or empty.
func NormalizeCNPJ14(s string) string {
	d := digitsOnly(s)
	if !cnpj14Re.MatchString(d) {
		return ""
	}
	return d
}

// SanitizeText strips control chars and truncates; never invents content.
func SanitizeText(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Drop HTML tags roughly (no script execution surface).
	s = stripTags(s)
	s = strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, s)
	if maxRunes > 0 && utf8.RuneCountInString(s) > maxRunes {
		runes := []rune(s)
		s = string(runes[:maxRunes])
	}
	return s
}

func stripTags(s string) string {
	// Simple angle-bracket strip; feed must not carry executable HTML.
	var b strings.Builder
	b.Grow(len(s))
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func digitsOnly(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ParseDate accepts YYYY-MM-DD or RFC3339 date portion; empty -> nil.
func ParseDate(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return &t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		return &d
	}
	// Date-only prefix of datetime
	if len(s) >= 10 {
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
			return &t
		}
	}
	return nil
}

// DefaultQueueState for a lead after import (no invention of contacts).
func DefaultQueueState(lead FeedLead, existing *models.OutreachAccount) string {
	// Preserve human terminal states.
	if existing != nil {
		if existing.DoNotContact || existing.QueueState == models.OutreachQueueDoNotContact {
			return models.OutreachQueueDoNotContact
		}
		if existing.Blocked || existing.QueueState == models.OutreachQueueBlocked {
			return models.OutreachQueueBlocked
		}
		if existing.QueueState == models.OutreachQueueTargetFitSuppressed {
			if LeadTargetFitDecision(lead).Eligible {
				if hasEnrollableContact(lead) {
					return models.OutreachQueueReadyToGenerate
				}
				return models.OutreachQueueNeedsContact
			}
			return models.OutreachQueueTargetFitSuppressed
		}
		// Do not silently restart post-send states.
		switch existing.QueueState {
		case models.OutreachQueueEnrolled, models.OutreachQueueSent, models.OutreachQueueReplied,
			models.OutreachQueueMeeting, models.OutreachQueueProposal, models.OutreachQueueWon,
			models.OutreachQueueLost, models.OutreachQueueSkipped, models.OutreachQueueBounced,
			models.OutreachQueueApproved, models.OutreachQueueNeedsReview:
			return existing.QueueState
		}
	}
	if !LeadTargetFitDecision(lead).Eligible {
		// Target-fit still blocks email. Manual/routed commercial work stays visible.
		if leadHasManualCommercialRoute(lead) {
			return models.OutreachQueueNeedsContact
		}
		return models.OutreachQueueTargetFitSuppressed
	}
	if hasEnrollableContact(lead) {
		return models.OutreachQueueReadyToGenerate
	}
	return models.OutreachQueueNeedsContact
}

func leadHasManualCommercialRoute(lead FeedLead) bool {
	for _, c := range lead.Contacts {
		class := MapReachability(c.ReachabilityClass)
		if class == "" {
			class = MapActionMode(c.ActionMode)
		}
		switch class {
		case models.ReachabilityR3Routed, models.ReachabilityR4Role, models.ReachabilityR5Corporate:
			return true
		}
		switch strings.ToUpper(strings.TrimSpace(c.ActionMode)) {
		case ActionModeManualRoutedCall, ActionModeNamedHumanManual, ActionModeManualCall,
			ActionModeManualWhatsApp, ActionModeManualSocial, ActionModeRoleEmail, ActionModeContactForm:
			return true
		}
		switch strings.ToUpper(strings.TrimSpace(c.ContactTier)) {
		case ContactTierB, ContactTierC, ContactTierD:
			return true
		}
	}
	return false
}

func hasEnrollableContact(lead FeedLead) bool {
	for _, c := range lead.Contacts {
		email := strings.TrimSpace(c.Email)
		if email == "" {
			continue
		}
		vs := strings.TrimSpace(c.VerificationStatus)
		if vs == "" {
			vs = models.OutreachVerifyCandidateUnverified
		}
		if models.OutreachUnenrollableVerification[vs] {
			continue
		}
		if vs == models.OutreachVerifyDoNotContact {
			continue
		}
		return true
	}
	return false
}

// NormalizeVerification returns a known status or CANDIDATE_UNVERIFIED when
// email is present without status; NOT_FOUND when empty.
func NormalizeVerification(status, email string) string {
	status = strings.TrimSpace(status)
	email = strings.TrimSpace(email)
	if status != "" && allowedVerification[status] {
		return status
	}
	if email == "" {
		return models.OutreachVerifyNotFound
	}
	return models.OutreachVerifyCandidateUnverified
}

// NormalizeEpistemic defaults missing class to COMMERCIAL_HYPOTHESIS.
func NormalizeEpistemic(class string) string {
	class = strings.TrimSpace(class)
	if class != "" && allowedEpistemic[class] {
		return class
	}
	return models.OutreachEpistemicCommercialHypothesis
}
