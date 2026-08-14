package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// Outreach staging models: intelligence-plane feed import into a multi-tenant
// company staging queue. Companies may lack email; they are not forced into
// contacts until a candidate is promoted.

// Schema versions for the confenge/extra-cli integration contracts.
const (
	OutreachSchemaV1        = "confenge.outreach.v1"
	OutreachOutcomeSchemaV1 = "confenge.outcome.v1"
)

// Contact verification statuses allowed on outreach contact candidates.
const (
	OutreachVerifyOfficialSource       = "OFFICIAL_SOURCE"
	OutreachVerifyPublicDocumentRecent = "PUBLIC_DOCUMENT_RECENT"
	OutreachVerifyMultipleSources      = "MULTIPLE_PUBLIC_SOURCES"
	OutreachVerifyInstitutionalGeneric = "INSTITUTIONAL_GENERIC"
	OutreachVerifyPublicPossiblyStale  = "PUBLIC_POSSIBLY_STALE"
	OutreachVerifyCandidateUnverified  = "CANDIDATE_UNVERIFIED"
	OutreachVerifyNotFound             = "NOT_FOUND"
	OutreachVerifyInvalid              = "INVALID"
	OutreachVerifyBounced              = "BOUNCED"
	OutreachVerifyDoNotContact         = "DO_NOT_CONTACT"
	// Human/operator-confirmed (pilot list, sink, CRM) and verified-channel statuses.
	OutreachVerifyHumanConfirmed = "HUMAN_CONFIRMED"
	OutreachVerifyVerified       = "VERIFIED"
)

// Epistemic classifications for evidence.
const (
	OutreachEpistemicConfirmedFact          = "CONFIRMED_FACT"
	OutreachEpistemicStrongInference        = "STRONG_INFERENCE"
	OutreachEpistemicWeakInference          = "WEAK_INFERENCE"
	OutreachEpistemicCommercialHypothesis   = "COMMERCIAL_HYPOTHESIS"
	OutreachEpistemicNotFound               = "NOT_FOUND"
	OutreachEpistemicRequiresCompanyConfirm = "REQUIRES_COMPANY_CONFIRMATION"
	OutreachEpistemicContradictoryEvidence  = "CONTRADICTORY_EVIDENCE"
)

// Queue states for outreach accounts (dashboard pipeline).
const (
	OutreachQueueNeedsContact        = "NEEDS_CONTACT"
	OutreachQueueReadyToGenerate     = "READY_TO_GENERATE"
	OutreachQueueNeedsReview         = "NEEDS_REVIEW"
	OutreachQueueApproved            = "APPROVED"
	OutreachQueueEnrolled            = "ENROLLED"
	OutreachQueueSent                = "SENT"
	OutreachQueueReplied             = "REPLIED"
	OutreachQueueMeeting             = "MEETING"
	OutreachQueueProposal            = "PROPOSAL"
	OutreachQueueWon                 = "WON"
	OutreachQueueLost                = "LOST"
	OutreachQueueBlocked             = "BLOCKED"
	OutreachQueueBounced             = "BOUNCED"
	OutreachQueueDoNotContact        = "DO_NOT_CONTACT"
	OutreachQueueSkipped             = "SKIPPED"
	OutreachQueueTargetFitSuppressed = "TARGET_FIT_SUPPRESSED"
)

// Import run statuses.
const (
	OutreachImportPending   = "pending"
	OutreachImportRunning   = "running"
	OutreachImportCompleted = "completed"
	OutreachImportFailed    = "failed"
	OutreachImportPartial   = "partial"
)

// Verification statuses that must never be enrolled into a campaign.
var OutreachUnenrollableVerification = map[string]bool{
	OutreachVerifyCandidateUnverified: true,
	OutreachVerifyNotFound:            true,
	OutreachVerifyInvalid:             true,
	OutreachVerifyBounced:             true,
	OutreachVerifyDoNotContact:        true,
}

// OutreachImportCounts is the dry-run / apply summary for one import run.
type OutreachImportCounts struct {
	Creates           int `json:"creates"`
	Updates           int `json:"updates"`
	Unchanged         int `json:"unchanged"`
	MissingContact    int `json:"missing_contact"`
	Invalid           int `json:"invalid"`
	Blocked           int `json:"blocked"`
	Conflicts         int `json:"conflicts"`
	EvidenceAdded     int `json:"evidence_added"`
	ContactsPromoted  int `json:"contacts_promoted"`
	Warnings          int `json:"warnings"`
	LeadsProcessed    int `json:"leads_processed"`
	LeadsSkippedError int `json:"leads_skipped_error"`
	// Operator briefing (additive). Omitted on older import rows.
	Actionable         int            `json:"actionable,omitempty"`
	ManualCall         int            `json:"manual_call,omitempty"`
	EmailSafe          int            `json:"email_safe,omitempty"`
	UnresolvedBlockers int            `json:"unresolved_blockers,omitempty"`
	RouteDistribution  map[string]int `json:"route_distribution,omitempty"`
	NextHumanActions   []string       `json:"next_human_actions,omitempty"`
}

// OutreachImportError is a per-lead error recorded without aborting the run.
type OutreachImportError struct {
	SourceLeadID string `json:"source_lead_id,omitempty"`
	CNPJ14       string `json:"cnpj14,omitempty"`
	Message      string `json:"message"`
}

// OutreachImportRun is one feed import attempt (dry-run or apply).
type OutreachImportRun struct {
	ID                uuid.UUID             `json:"id"`
	OrganizationID    uuid.UUID             `json:"organization_id"`
	SourceSystem      string                `json:"source_system"`
	SourceRunID       string                `json:"source_run_id"`
	SchemaVersion     string                `json:"schema_version"`
	SnapshotHash      string                `json:"snapshot_hash"`
	RepoSHA           string                `json:"repo_sha"`
	PayloadHash       string                `json:"payload_hash"`
	ProfileID         string                `json:"profile_id"`
	ProfileVersion    string                `json:"profile_version"`
	Status            string                `json:"status"`
	DryRun            bool                  `json:"dry_run"`
	StartedAt         time.Time             `json:"started_at"`
	FinishedAt        *time.Time            `json:"finished_at,omitempty"`
	CursorIn          string                `json:"cursor_in,omitempty"`
	CursorOut         string                `json:"cursor_out,omitempty"`
	Counts            OutreachImportCounts  `json:"counts"`
	Errors            []OutreachImportError `json:"errors,omitempty"`
	Warnings          []string              `json:"warnings,omitempty"`
	CreatedByUserID   *uuid.UUID            `json:"created_by_user_id,omitempty"`
	IdempotencyKey    string                `json:"idempotency_key,omitempty"`
	SourceURI         string                `json:"source_uri,omitempty"`
	SourceGeneratedAt *time.Time            `json:"source_generated_at,omitempty"`
	CreatedAt         time.Time             `json:"created_at"`
	UpdatedAt         time.Time             `json:"updated_at"`
}

// OutreachAccount is a staged company from the intelligence feed.
type OutreachAccount struct {
	ID                 uuid.UUID  `json:"id"`
	OrganizationID     uuid.UUID  `json:"organization_id"`
	SourceLeadID       string     `json:"source_lead_id"`
	CNPJ14             string     `json:"cnpj14"`
	CNPJRoot           string     `json:"cnpj_root"`
	RazaoSocial        string     `json:"razao_social"`
	NomeFantasia       string     `json:"nome_fantasia"`
	Municipio          string     `json:"municipio"`
	UF                 string     `json:"uf"`
	Website            string     `json:"website"`
	PriorityRank       int        `json:"priority_rank"`
	PriorityScore      float64    `json:"priority_score"`
	PriorityTier       string     `json:"priority_tier"`
	PriorityConfidence string     `json:"priority_confidence"`
	MomentCode         string     `json:"moment_code"`
	MomentSummary      string     `json:"moment_summary"`
	MomentObservedAt   *time.Time `json:"moment_observed_at,omitempty"`
	MomentConfidence   string     `json:"moment_confidence"`
	MomentEvidenceIDs  []string   `json:"moment_evidence_ids,omitempty"`
	ServiceCode        string     `json:"service_code"`
	ServiceName        string     `json:"service_name"`
	EntryOffer         string     `json:"entry_offer"`
	OfferRationale     string     `json:"offer_rationale"`
	FactToMention      string     `json:"fact_to_mention"`
	QuestionToAsk      string     `json:"question_to_ask"`
	CTA                string     `json:"cta"`
	ClaimsToAvoid      []string   `json:"claims_to_avoid,omitempty"`
	CommercialState    string     `json:"commercial_state"`
	QueueState         string     `json:"queue_state"`
	HumanOverride      bool       `json:"human_override"`
	Blocked            bool       `json:"blocked"`
	BlockReason        string     `json:"block_reason,omitempty"`
	DoNotContact       bool       `json:"do_not_contact"`
	SourceSystem       string     `json:"source_system"`
	SourceRunID        string     `json:"source_run_id"`
	LastImportRunID    *uuid.UUID `json:"last_import_run_id,omitempty"`
	LastPayloadHash    string     `json:"last_payload_hash,omitempty"`
	ContractsJSON      []byte     `json:"contracts,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`

	// Activation projection from extra-cli (not local commercial scoring).
	ActivationState         string     `json:"activation_state,omitempty"`
	ActivationScore         float64    `json:"activation_score,omitempty"`
	ActivationReasonCodes   []string   `json:"activation_reason_codes,omitempty"`
	ActivationPolicyVersion string     `json:"activation_policy_version,omitempty"`
	ActivationEvaluatedAt   *time.Time `json:"activation_evaluated_at,omitempty"`
	NextBestActionAt        *time.Time `json:"next_best_action_at,omitempty"`
	ActivationExpiresAt     *time.Time `json:"activation_expires_at,omitempty"`
	ActivationSourceHash    string     `json:"activation_source_hash,omitempty"`
	MessageContextHash      string     `json:"message_context_hash,omitempty"`
	ScoreComponentsJSON     []byte     `json:"score_components,omitempty"`

	// Send-fit imported from extra-cli. Never derived from activation_state.
	// Empty TargetFitSendTier = legacy feed: import OK, GREEN autorun forbidden.
	TargetFitSendTier          string     `json:"target_fit_send_tier,omitempty"`
	TargetFitReasons           []string   `json:"target_fit_reasons,omitempty"`
	TargetFitClass             string     `json:"target_fit_class,omitempty"`
	TargetFitConfidence        *float64   `json:"target_fit_confidence,omitempty"`
	TargetFitVersion           string     `json:"target_fit_version,omitempty"`
	TargetFitComputedAt        *time.Time `json:"target_fit_computed_at,omitempty"`
	TargetFitSourceWatermark   string     `json:"target_fit_source_watermark,omitempty"`
	TargetFitObservedAt        *time.Time `json:"target_fit_observed_at,omitempty"`
	TargetFitFresh             bool       `json:"target_fit_fresh"`
	TargetFitEvidenceIDs       []string   `json:"target_fit_evidence_ids,omitempty"`
	TargetFitFreshnessReason   string     `json:"target_fit_freshness_reason,omitempty"`
	TargetFitEligible          bool       `json:"target_fit_eligible"`
	TargetFitSuppressionReason string     `json:"target_fit_suppression_reason,omitempty"`
	TargetFitReconciledAt      *time.Time `json:"target_fit_reconciled_at,omitempty"`
	// Company-level rollup of best-contact email_send_ready from extra-cli.
	EmailSendReady bool `json:"email_send_ready,omitempty"`

	// Joined / computed (not always filled).
	Contacts  []OutreachContactCandidate `json:"contacts,omitempty"`
	Evidence  []OutreachEvidence         `json:"evidence,omitempty"`
	ContactN  int                        `json:"contact_count,omitempty"`
	EvidenceN int                        `json:"evidence_count,omitempty"`
	// ContextStale is set when generated content no longer matches message_context_hash.
	ContextStale bool `json:"context_stale,omitempty"`
}

// OutreachContactCandidate is a possible recipient before promotion to contacts.
type OutreachContactCandidate struct {
	ID              uuid.UUID `json:"id"`
	OrganizationID  uuid.UUID `json:"organization_id"`
	AccountID       uuid.UUID `json:"account_id"`
	SourceContactID string    `json:"source_contact_id"`
	PersonID        string    `json:"person_id,omitempty"`
	Name            string    `json:"name"`
	Role            string    `json:"role"`
	Email           string    `json:"email"`
	Phone           string    `json:"phone"`
	// Structured phone + WhatsApp consent (additive; public phone is not opt-in).
	PhoneE164                   string     `json:"phone_e164,omitempty"`
	PhoneSource                 string     `json:"phone_source,omitempty"`
	PhoneSourceURL              string     `json:"phone_source_url,omitempty"`
	WhatsAppConsentStatus       string     `json:"whatsapp_consent_status,omitempty"`
	WhatsAppConsentSource       string     `json:"whatsapp_consent_source,omitempty"`
	WhatsAppConsentAt           *time.Time `json:"whatsapp_consent_at,omitempty"`
	WhatsAppConsentProvenanceOK bool       `json:"whatsapp_consent_provenance_ok,omitempty"`
	LinkedInURL                 string     `json:"linkedin_url,omitempty"`
	SourceURL                   string     `json:"source_url,omitempty"`
	SourceDocument              string     `json:"source_document,omitempty"`
	SourceDate                  *time.Time `json:"source_date,omitempty"`
	VerificationStatus          string     `json:"verification_status"`
	Confidence                  string     `json:"confidence"`
	Recommended                 bool       `json:"recommended"`
	WarmblyContactID            *uuid.UUID `json:"warmbly_contact_id,omitempty"`
	PromotedAt                  *time.Time `json:"promoted_at,omitempty"`
	Blocked                     bool       `json:"blocked"`
	BlockReason                 string     `json:"block_reason,omitempty"`
	DoNotContact                bool       `json:"do_not_contact"`
	Bounced                     bool       `json:"bounced"`
	// Imported readiness (extra-cli). Never inferred from email syntax alone.
	EmailSendReady                 bool       `json:"email_send_ready,omitempty"`
	MailboxPurpose                 string     `json:"mailbox_purpose,omitempty"`
	MailboxPurposeSendBlocked      bool       `json:"mailbox_purpose_send_blocked,omitempty"`
	OwnershipStatus                string     `json:"ownership_status,omitempty"`
	RecipientCommercialSuitability string     `json:"recipient_commercial_suitability,omitempty"`
	LastImportRunID                *uuid.UUID `json:"last_import_run_id,omitempty"`
	// Additive extra-cli reachability boundary. Empty means the current
	// contact-tier contract applies; Warmbly never invents a class.
	ReachabilityClass string    `json:"reachability_class,omitempty"`
	RouteType         string    `json:"route_type,omitempty"`
	RouteRelation     string    `json:"route_relation,omitempty"`
	ChannelValue      string    `json:"channel_value,omitempty"`
	ChannelDisplay    string    `json:"channel_display,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// CanEnroll reports whether this candidate may be put into a campaign.
func (c *OutreachContactCandidate) CanEnroll() bool {
	if c == nil {
		return false
	}
	if c.Blocked || c.DoNotContact || c.Bounced {
		return false
	}
	if c.Email == "" {
		return false
	}
	if OutreachUnenrollableVerification[c.VerificationStatus] {
		return false
	}
	// Defense-in-depth: known synthetic demo00Xobra channels never enroll
	// even if labeled VERIFIED/COMPANY_OWNED. Broader fixture heuristics
	// (example.com) are enforced at import (leadToCandidate taint gate), not
	// here — unit fixtures legitimately use @example.com.
	email := strings.ToLower(strings.TrimSpace(c.Email))
	if strings.Contains(email, "@demo") && strings.Contains(email, "obra.com.br") {
		return false
	}
	if strings.HasPrefix(email, "fixture@") || strings.HasPrefix(email, "synthetic@") {
		return false
	}
	if strings.Contains(c.BlockReason, "provenance_taint") || strings.Contains(c.BlockReason, "provenance_chain") {
		return false
	}
	if strings.Contains(c.BlockReason, "PROVENANCE_CONTAMINATION") {
		return false
	}
	return true
}

// OutreachEvidence is one sanitized evidence row for an account.
type OutreachEvidence struct {
	ID               uuid.UUID  `json:"id"`
	OrganizationID   uuid.UUID  `json:"organization_id"`
	AccountID        uuid.UUID  `json:"account_id"`
	SourceEvidenceID string     `json:"source_evidence_id"`
	EvidenceType     string     `json:"evidence_type"`
	Title            string     `json:"title"`
	URL              string     `json:"url"`
	Document         string     `json:"document"`
	EvidenceDate     *time.Time `json:"evidence_date,omitempty"`
	Location         string     `json:"location"`
	Excerpt          string     `json:"excerpt"`
	Synthesis        string     `json:"synthesis"`
	EpistemicClass   string     `json:"epistemic_class"`
	Reliability      string     `json:"reliability"`
	ConsultedAt      *time.Time `json:"consulted_at,omitempty"`
	LastImportRunID  *uuid.UUID `json:"last_import_run_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// OutreachQueueSummary is the dashboard overview counters.
type OutreachQueueSummary struct {
	NeedsContact    int `json:"needs_contact"`
	ReadyToGenerate int `json:"ready_to_generate"`
	NeedsReview     int `json:"needs_review"`
	Approved        int `json:"approved"`
	Enrolled        int `json:"enrolled"`
	Sent            int `json:"sent"`
	Replied         int `json:"replied"`
	Meeting         int `json:"meeting"`
	Proposal        int `json:"proposal"`
	Won             int `json:"won"`
	Blocked         int `json:"blocked"`
	Bounced         int `json:"bounced"`
	DoNotContact    int `json:"do_not_contact"`
	Total           int `json:"total"`
}

// OutreachAccountListResult is a cursor-paginated account list.
type OutreachAccountListResult struct {
	Data       []OutreachAccount `json:"data"`
	Pagination Pagination        `json:"pagination"`
}

// OutreachImportRunListResult lists recent import runs.
type OutreachImportRunListResult struct {
	Data       []OutreachImportRun `json:"data"`
	Pagination Pagination          `json:"pagination"`
}

// Draft statuses.
const (
	OutreachDraftNotGenerated = "NOT_GENERATED"
	OutreachDraftGenerating   = "GENERATING"
	OutreachDraftNeedsReview  = "NEEDS_REVIEW"
	OutreachDraftApproved     = "APPROVED"
	OutreachDraftRejected     = "REJECTED"
	OutreachDraftSkipped      = "SKIPPED"
	OutreachDraftBlocked      = "BLOCKED"
	OutreachDraftEnrolled     = "ENROLLED"
	OutreachDraftSent         = "SENT"
	OutreachDraftReplied      = "REPLIED"
)

// Outreach draft channels.
const (
	OutreachChannelEmail    = "EMAIL"
	OutreachChannelWhatsApp = "WHATSAPP"
)

// OutreachDraft is one generated/reviewed message for a staged account.
type OutreachDraft struct {
	ID                 uuid.UUID  `json:"id"`
	OrganizationID     uuid.UUID  `json:"organization_id"`
	AccountID          uuid.UUID  `json:"account_id"`
	ContactCandidateID *uuid.UUID `json:"contact_candidate_id,omitempty"`

	// Channel is EMAIL (default) or WHATSAPP. Threads stay separate per channel.
	Channel            string `json:"channel"`
	RecipientName      string `json:"recipient_name"`
	RecipientRole      string `json:"recipient_role"`
	RecipientEmail     string `json:"recipient_email"`
	RecipientPhoneE164 string `json:"recipient_phone_e164,omitempty"`
	VerificationStatus string `json:"verification_status"`

	Subject       string `json:"subject"`
	BodyText      string `json:"body_text"`
	BodyHTML      string `json:"body_html"`
	FollowupsJSON []byte `json:"followups,omitempty"`

	ServiceCode  string   `json:"service_code"`
	StrategyCode string   `json:"strategy_code"`
	FactUsed     string   `json:"fact_used"`
	EvidenceIDs  []string `json:"evidence_ids,omitempty"`
	Question     string   `json:"question"`
	CTA          string   `json:"cta"`

	Provider      string `json:"provider"`
	Model         string `json:"model"`
	PromptVersion string `json:"prompt_version"`
	Generation    int    `json:"generation"`

	ValidationJSON []byte   `json:"validation,omitempty"`
	ValidationOK   *bool    `json:"validation_ok,omitempty"`
	RiskClass      string   `json:"risk_class"`
	RiskFlags      []string `json:"risk_flags,omitempty"`
	RedTeamResult  string   `json:"red_team_result,omitempty"`
	RedTeamReasons []string `json:"red_team_reasons,omitempty"`

	Status        string     `json:"status"`
	HumanEdited   bool       `json:"human_edited"`
	ApprovedBy    *uuid.UUID `json:"approved_by,omitempty"`
	ApprovedAt    *time.Time `json:"approved_at,omitempty"`
	ReviewSeconds int        `json:"review_seconds"`

	CampaignID          *uuid.UUID `json:"campaign_id,omitempty"`
	EnrollmentContactID *uuid.UUID `json:"enrollment_contact_id,omitempty"`
	EnrolledAt          *time.Time `json:"enrolled_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Joined for review UI
	Account *OutreachAccount `json:"account,omitempty"`
}

// OutreachOrgSettings stores confenge campaign bootstrap pointer.
type OutreachOrgSettings struct {
	OrganizationID uuid.UUID  `json:"organization_id"`
	CampaignID     *uuid.UUID `json:"campaign_id,omitempty"`
	CampaignName   string     `json:"campaign_name"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// OutreachOutcome is one outbox row for confenge.outcome.v1 delivery.
type OutreachOutcome struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	EventID        uuid.UUID  `json:"event_id"`
	IdempotencyKey string     `json:"idempotency_key"`
	SourceLeadID   string     `json:"source_lead_id"`
	CNPJ14         string     `json:"cnpj14"`
	ContactEmail   string     `json:"contact_email"`
	EventType      string     `json:"event_type"`
	Payload        []byte     `json:"payload,omitempty"`
	OccurredAt     time.Time  `json:"occurred_at"`
	Attempts       int        `json:"attempts"`
	NextAttemptAt  time.Time  `json:"next_attempt_at"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	DeadLetter     bool       `json:"dead_letter"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// OutreachFeedSyncState is durable manifest sync progress per org.
type OutreachFeedSyncState struct {
	OrganizationID    uuid.UUID  `json:"organization_id"`
	LastSnapshotHash  string     `json:"last_snapshot_hash"`
	LastRunID         string     `json:"last_run_id"`
	LastManifestURI   string     `json:"last_manifest_uri"`
	LastSuccessAt     *time.Time `json:"last_success_at,omitempty"`
	SourceGeneratedAt *time.Time `json:"source_generated_at,omitempty"`
	LastAttemptAt     *time.Time `json:"last_attempt_at,omitempty"`
	LastError         string     `json:"last_error,omitempty"`
	LastStatus        string     `json:"last_status"`
	CountsJSON        []byte     `json:"counts,omitempty"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// OutreachPilotMembership is the durable definition of one prepared pilot account.
// It is inserted only after the current touchpoint and draft have been validated.
type OutreachPilotMembership struct {
	ID                 uuid.UUID `json:"id"`
	OrganizationID     uuid.UUID `json:"organization_id"`
	CohortID           string    `json:"cohort_id"`
	AccountID          uuid.UUID `json:"account_id"`
	CNPJ14             string    `json:"cnpj14"`
	ContactCandidateID uuid.UUID `json:"contact_candidate_id"`
	TouchpointID       uuid.UUID `json:"touchpoint_id"`
	DraftID            uuid.UUID `json:"draft_id"`
	SnapshotHash       string    `json:"snapshot_hash"`
	SourceRunID        string    `json:"source_run_id"`
	ContextHash        string    `json:"context_hash"`
	OperationKey       string    `json:"operation_key,omitempty"`
	RequestHash        string    `json:"request_hash,omitempty"`
	FeedGeneratedAt    time.Time `json:"-"`
	CandidateUpdatedAt time.Time `json:"-"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// Touchpoint states (per-message human approval authority).
const (
	TouchpointPlanned     = "PLANNED"
	TouchpointDue         = "DUE"
	TouchpointDrafted     = "DRAFTED"
	TouchpointNeedsReview = "NEEDS_REVIEW"
	TouchpointApproved    = "APPROVED"
	TouchpointQueued      = "QUEUED"
	TouchpointSent        = "SENT"
	TouchpointSkipped     = "SKIPPED"
	TouchpointRejected    = "REJECTED"
	TouchpointReplied     = "REPLIED"
	TouchpointDNC         = "DNC"
	TouchpointBounced     = "BOUNCED"
	TouchpointCancelled   = "CANCELLED"
	TouchpointFailed      = "FAILED"
)

const (
	TouchpointPurposeInitial  = "INITIAL"
	TouchpointPurposeFollowUp = "FOLLOW_UP"
	TouchpointPurposeClose    = "CLOSE"
	CadencePolicyVersionV1    = "confenge.cadence.v1"
)

var TouchpointOpenStates = map[string]bool{
	TouchpointPlanned: true, TouchpointDue: true, TouchpointDrafted: true,
	TouchpointNeedsReview: true, TouchpointApproved: true, TouchpointQueued: true,
}

var TouchpointTerminalStates = map[string]bool{
	TouchpointSent: true, TouchpointSkipped: true, TouchpointRejected: true,
	TouchpointReplied: true, TouchpointDNC: true, TouchpointBounced: true,
	TouchpointCancelled: true, TouchpointFailed: true,
}

// CampaignPolicyAuthorization is an explicit, auditable campaign/policy grant.
// After this authorization, GREEN messages may autoqueue when GreenAutorunEnabled.
// Never forges approved_by=<human> for messages the human did not review.
type CampaignPolicyAuthorization struct {
	ID                       uuid.UUID  `json:"id,omitempty"`
	CampaignID               uuid.UUID  `json:"campaign_id"`
	PromptPolicyVersion      string     `json:"prompt_policy_version"`
	ValidatorVersion         string     `json:"validator_version"`
	ContactPolicyVersion     string     `json:"contact_policy_version"`
	SenderMailbox            string     `json:"sender_mailbox"`
	Channel                  string     `json:"channel"`            // EMAIL
	AllowedRiskClass         string     `json:"allowed_risk_class"` // GREEN
	MaxRatePerHour           int        `json:"max_rate_per_hour"`
	EffectiveAt              time.Time  `json:"effective_at"`
	AuthorizedBy             uuid.UUID  `json:"authorized_by"`
	AuthorizedByLabel        string     `json:"authorized_by_label,omitempty"`
	RevokedAt                *time.Time `json:"revoked_at,omitempty"`
	TemplatePolicyVersion    string     `json:"template_policy_version,omitempty"`
	AllowPolicyTemplateGREEN bool       `json:"allow_policy_template_green"`
}

// Active reports whether the authorization is currently valid at now.
func (a *CampaignPolicyAuthorization) Active(now time.Time) bool {
	if a == nil {
		return false
	}
	if a.RevokedAt != nil && !a.RevokedAt.After(now) {
		return false
	}
	if a.EffectiveAt.IsZero() {
		return false
	}
	return !a.EffectiveAt.After(now)
}

// OutreachTouchpoint is one human-gated message in a CONFENGE cadence.
type OutreachTouchpoint struct {
	ID                  uuid.UUID  `json:"id"`
	OrganizationID      uuid.UUID  `json:"organization_id"`
	AccountID           uuid.UUID  `json:"account_id"`
	ContactCandidateID  *uuid.UUID `json:"contact_candidate_id,omitempty"`
	Ordinal             int        `json:"ordinal"`
	CadenceStep         string     `json:"cadence_step"`
	Channel             string     `json:"channel"`
	Purpose             string     `json:"purpose"`
	DueAt               time.Time  `json:"due_at"`
	State               string     `json:"state"`
	DraftID             *uuid.UUID `json:"draft_id,omitempty"`
	Recipient           string     `json:"recipient"`
	Subject             string     `json:"subject"`
	BodyText            string     `json:"body_text"`
	ContentHash         string     `json:"content_hash"`
	ApprovedContentHash string     `json:"approved_content_hash"`
	ApprovedBy          *uuid.UUID `json:"approved_by,omitempty"`
	ApprovedAt          *time.Time `json:"approved_at,omitempty"`
	// AuthorizationMode: HUMAN_TOUCHPOINT_APPROVAL | CAMPAIGN_POLICY (empty = legacy human).
	AuthorizationMode string `json:"authorization_mode,omitempty"`
	// Campaign policy binding (CAMPAIGN_POLICY path only). Cleared with ClearApproval.
	CampaignPolicyAuthorizationID *uuid.UUID `json:"campaign_policy_authorization_id,omitempty"`
	AuthorizationPolicyHash       string     `json:"authorization_policy_hash,omitempty"`
	AuthorizationAt               *time.Time `json:"authorization_at,omitempty"`
	// SignatureVersion is post-auth decoration id (deterministic CID/text close).
	SignatureVersion     string           `json:"signature_version,omitempty"`
	QueuedAt             *time.Time       `json:"queued_at,omitempty"`
	SentAt               *time.Time       `json:"sent_at,omitempty"`
	ProviderMessageID    string           `json:"provider_message_id,omitempty"`
	StopReason           string           `json:"stop_reason,omitempty"`
	PreviousTouchpointID *uuid.UUID       `json:"previous_touchpoint_id,omitempty"`
	IdempotencyKey       string           `json:"idempotency_key"`
	PolicyVersion        string           `json:"policy_version"`
	ServiceCode          string           `json:"service_code"`
	FactUsed             string           `json:"fact_used"`
	EvidenceIDs          []string         `json:"evidence_ids,omitempty"`
	GeneratedContextHash string           `json:"generated_context_hash,omitempty"`
	CreatedAt            time.Time        `json:"created_at"`
	UpdatedAt            time.Time        `json:"updated_at"`
	Account              *OutreachAccount `json:"account,omitempty"`
	Draft                *OutreachDraft   `json:"draft,omitempty"`
	// ContextStale when GeneratedContextHash != account.MessageContextHash.
	ContextStale bool `json:"context_stale,omitempty"`
	// StrategyExplain is operator cockpit metadata (not prospect-facing; not a DB column).
	StrategyExplain         map[string]any `json:"strategy_explain,omitempty"`
	DoctrineAlerts          []string       `json:"doctrine_alerts,omitempty"`
	RecipientMailboxPurpose string         `json:"recipient_mailbox_purpose,omitempty"`
	RecipientGeneric        bool           `json:"recipient_generic,omitempty"`
	RecipientState          string         `json:"recipient_state,omitempty"`
	RecipientReason         string         `json:"recipient_reason,omitempty"`
}

// Commercial action types. Semantic differences are load-bearing.
const (
	ActionDirectEmail         = "DIRECT_EMAIL"
	ActionInferredEmailReview = "INFERRED_EMAIL_REVIEW"
	ActionRoleEmail           = "ROLE_EMAIL"
	ActionGenericEmail        = "GENERIC_EMAIL"
	ActionDirectCall          = "DIRECT_CALL"
	ActionRoutedCall          = "ROUTED_CALL"
	ActionWhatsApp            = "WHATSAPP"
	ActionProfessionalSocial  = "PROFESSIONAL_SOCIAL"
	ActionContactForm         = "CONTACT_FORM"
	ActionOtherManual         = "OTHER_MANUAL"
)

// Canonical reachability classes (mapped from extra-cli strings).
const (
	ReachabilityR1Direct    = "R1_DIRECT"
	ReachabilityR2Inferred  = "R2_HIGH_CONFIDENCE_DIRECT"
	ReachabilityR3Routed    = "R3_ROUTED_TO_NAMED_PERSON"
	ReachabilityR4Role      = "R4_ROLE_ROUTE"
	ReachabilityR5Corporate = "R5_CORPORATE_ONLY"
	ReachabilityR0None      = "R0_NO_ACTIONABLE_ROUTE"
	ReachabilityBlocked     = "BLOCKED"
	ReachabilityUnmapped    = "UNMAPPED"
)

// Commercial action execution states. Outcome is stored separately.
const (
	ActionStatePlanned       = "PLANNED"
	ActionStateReady         = "READY"
	ActionStateInProgress    = "IN_PROGRESS"
	ActionStateCompleted     = "COMPLETED"
	ActionStateFailed        = "FAILED"
	ActionStateSkipped       = "SKIPPED"
	ActionStateBlocked       = "BLOCKED"
	ActionStateNeedsFollowup = "NEEDS_FOLLOWUP"
)

// Lean commercial outcome codes. WON is never inferred from these.
const (
	OutcomeNoAnswer              = "NO_ANSWER"
	OutcomeBusy                  = "BUSY"
	OutcomeInvalidChannel        = "INVALID_CHANNEL"
	OutcomeGatekeeperReached     = "GATEKEEPER_REACHED"
	OutcomeReferredToOtherPerson = "REFERRED_TO_OTHER_PERSON"
	OutcomeWrongPerson           = "WRONG_PERSON"
	OutcomeTargetReached         = "TARGET_REACHED"
	OutcomeCallbackRequested     = "CALLBACK_REQUESTED"
	OutcomeNotInterested         = "NOT_INTERESTED"
	OutcomeInterested            = "INTERESTED"
	OutcomeMeetingScheduled      = "MEETING_SCHEDULED"
	OutcomeRepliedCode           = "REPLIED"
	OutcomeBouncedCode           = "BOUNCED"
	OutcomeComplaint             = "COMPLAINT"
	OutcomeDNCCode               = "DNC"
	OutcomeFormSubmitted         = "FORM_SUBMITTED"
	OutcomeSocialMessageSent     = "SOCIAL_MESSAGE_SENT"
	OutcomeSkippedCode           = "SKIPPED"
	OutcomeBlockedCode           = "BLOCKED"
	OutcomeWrongChannel          = "WRONG_CHANNEL"
	OutcomeInvalidRoute          = "INVALID_ROUTE"
	OutcomeAttempted             = "ATTEMPTED"
	OutcomeContactedCode         = "CONTACTED"
	OutcomeFollowUp              = "FOLLOW_UP"
)

// Operational lanes for the commercial-action cockpit.
const (
	LaneEmailNeedsReview        = "EMAIL_NEEDS_REVIEW"
	LaneHumanReviewEmail        = "HUMAN_REVIEW_EMAIL"
	LaneCallQueue               = "CALL_QUEUE"
	LaneRoutedCallQueue         = "ROUTED_CALL_QUEUE"
	LaneWhatsAppQueue           = "WHATSAPP_QUEUE"
	LaneProfessionalSocialQueue = "PROFESSIONAL_SOCIAL_QUEUE"
	LaneRoleEmailQueue          = "ROLE_EMAIL_QUEUE"
	LaneContactFormQueue        = "CONTACT_FORM_QUEUE"
	LaneLowConfidenceManual     = "LOW_CONFIDENCE_MANUAL"
	LaneNeedsEnrichment         = "NEEDS_ENRICHMENT"
	LaneBlockedAction           = "BLOCKED"
	LaneDone                    = "DONE"
)

const (
	RouteRelBelongsToNamedPerson = "BELONGS_TO_NAMED_PERSON"
	RouteRelRoutesToNamedPerson  = "ROUTES_TO_NAMED_PERSON"
	RouteRelRoleMailbox          = "ROLE_MAILBOX"
	RouteRelCorporateGeneric     = "CORPORATE_GENERIC"
	RouteRelUnknown              = "UNKNOWN"
)

const ReachabilityMappingVersionV1 = "confenge.reachability.v1"

// OutreachCommercialAction is recommended next human work on one route.
// Distinct from an email-sendable draft or touchpoint.
type OutreachCommercialAction struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	AccountID      uuid.UUID  `json:"account_id"`
	CandidateID    *uuid.UUID `json:"candidate_id,omitempty"`
	SourceLeadID   string     `json:"source_lead_id,omitempty"`

	PersonName   string `json:"person_name,omitempty"`
	PersonID     string `json:"person_id,omitempty"`
	ObservedRole string `json:"observed_role,omitempty"`
	TargetRole   string `json:"target_role,omitempty"`

	ActionType        string `json:"action_type"`
	ReachabilityClass string `json:"reachability_class,omitempty"`
	MappingVersion    string `json:"mapping_version,omitempty"`
	RouteType         string `json:"route_type,omitempty"`
	RouteRelation     string `json:"route_relation,omitempty"`
	ChannelValue      string `json:"channel_value,omitempty"`
	ChannelDisplay    string `json:"channel_display,omitempty"`

	WhyNow            string   `json:"why_now,omitempty"`
	FactualHook       string   `json:"factual_hook,omitempty"`
	RecommendedAction string   `json:"recommended_action,omitempty"`
	ServiceCode       string   `json:"service_code,omitempty"`
	ServiceContext    string   `json:"service_context,omitempty"`
	Confidence        string   `json:"confidence,omitempty"`
	EvidenceIDs       []string `json:"evidence_ids,omitempty"`
	Warnings          []string `json:"warnings,omitempty"`

	State         string  `json:"state"`
	Lane          string  `json:"lane"`
	PriorityRank  int     `json:"priority_rank"`
	PriorityScore float64 `json:"priority_score"`

	Actionable    bool `json:"actionable"`
	EmailSendable bool `json:"email_sendable"`
	Dispatchable  bool `json:"dispatchable"`

	PersonFingerprint string `json:"person_fingerprint,omitempty"`
	RouteFingerprint  string `json:"route_fingerprint,omitempty"`
	ContentHash       string `json:"content_hash,omitempty"`
	SnapshotHash      string `json:"snapshot_hash,omitempty"`
	IdempotencyKey    string `json:"idempotency_key,omitempty"`

	ParentActionID   *uuid.UUID `json:"parent_action_id,omitempty"`
	FollowupActionID *uuid.UUID `json:"followup_action_id,omitempty"`

	HumanActor string `json:"human_actor,omitempty"`
	HumanNotes string `json:"human_notes,omitempty"`

	OutcomeCode             string     `json:"outcome_code,omitempty"`
	OutcomeNotes            string     `json:"outcome_notes,omitempty"`
	TargetReached           *bool      `json:"target_reached,omitempty"`
	ConversationStarted     bool       `json:"conversation_started"`
	InterestState           string     `json:"interest_state,omitempty"`
	NextActionType          string     `json:"next_action_type,omitempty"`
	NextActionAt            *time.Time `json:"next_action_at,omitempty"`
	RouteQualityFeedback    string     `json:"route_quality_feedback,omitempty"`
	PersonRelevanceFeedback string     `json:"person_relevance_feedback,omitempty"`
	MessageFeedback         string     `json:"message_feedback,omitempty"`

	ContentJSON    []byte `json:"content,omitempty"`
	CorrectionJSON []byte `json:"corrections,omitempty"`

	BlockedPerson bool   `json:"blocked_person,omitempty"`
	BlockedRoute  bool   `json:"blocked_route,omitempty"`
	StaleWarning  string `json:"stale_warning,omitempty"`
	RequiresFresh bool   `json:"requires_freshness,omitempty"`

	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	CompanyName string `json:"company_name,omitempty"`
}
