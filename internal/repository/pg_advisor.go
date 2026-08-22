package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// AdvisorMailbox is one mailbox's configuration plus the rolling behaviour the
// detectors reason about. Rates are left to the detectors: the repository
// returns raw counts so a detector can apply its own sample floor.
type AdvisorMailbox struct {
	ID       uuid.UUID
	Email    string
	Name     string
	Status   string
	Provider string
	// AgeDays is how long the mailbox has been connected. New mailboxes get
	// stricter volume advice than proven ones.
	AgeDays int

	CampaignLimit int
	MinWaitTime   int

	TrackingDomain         string
	TrackingDomainVerified bool

	AuthState       string
	AuthSPF         bool
	AuthDKIM        bool
	AuthDMARC       bool
	AuthDMARCPolicy string
	// AuthFailingSince is when the sending domain entered "failing". The send
	// gate measures its grace window from here, so the advisor uses it to say
	// whether sending has already stopped or is only about to.
	AuthFailingSince *time.Time

	WarmupActive    bool
	WarmupPaused    bool
	WarmupBase      int
	WarmupMax       int
	WarmupIncrease  int
	WarmupReplyRate int
	WarmupPoolType  string

	RiskBand string

	// ColdSent7d / ColdSent1d count completed campaign sends. Bounces and
	// complaints are 30-day windows, matching the documented complaint sample
	// floor.
	ColdSent7d     int
	ColdSent1d     int
	ColdSent30d    int
	Bounces30d     int
	Complaints30d  int
	WarmupSent7d   int
	WarmupSpam7d   int
	WarmupRecv7d   int
	PoolHealth     string
	PoolSpamScore  int
	PoolBlocked    bool
	UnresolvedErrs int
	// InActiveCampaign is true when at least one running campaign can send
	// through this mailbox (tag match or explicit sender).
	InActiveCampaign bool
}

// AdvisorCampaign is one campaign's configuration plus its window performance.
type AdvisorCampaign struct {
	ID     uuid.UUID
	Name   string
	Status string

	DailyLimit        int
	OpenTracking      bool
	LinkTracking      bool
	UnsubscribeHeader bool
	StopOnReply       bool
	TextOnly          bool
	Timezone          string
	Days              int
	StartTime         string
	EndTime           string
	ScheduleWindows   json.RawMessage

	SenderStrategy string
	RotationMode   string
	ESPMatchMode   string

	RampEnabled            bool
	RampStart              int
	RampCeiling            int
	TrackingDomain         string
	TrackingDomainVerified bool

	CreatedAt          time.Time
	LastStatusChangeAt *time.Time

	// SenderCount is how many mailboxes actually resolve for this campaign, and
	// SenderCapacity is the sum of their own daily caps — the real ceiling on
	// what this campaign can send, since the per-mailbox cap always wins.
	SenderCount    int
	SenderCapacity int
	StepCount      int
	// EmailStepCount excludes wait/action nodes.
	EmailStepCount int
	VariantCount   int

	// Window metrics (30 days).
	Sent       int
	Opened     int
	Clicked    int
	Replied    int
	Bounced    int
	Complaints int

	// LeadsTotal / LeadsRemaining drive the list-exhaustion detector.
	LeadsTotal     int
	LeadsRemaining int
}

// AdvisorStep is one sequence step with its copy and its own funnel, so the
// engine can spot a single follow-up that kills the sequence.
type AdvisorStep struct {
	ID         uuid.UUID
	CampaignID uuid.UUID
	Name       string
	Position   int
	Kind       string
	WaitAfter  int
	Subject    string
	BodyPlain  string
	BodyHTML   string

	Sent    int
	Opened  int
	Replied int
	Bounced int
}

// AdvisorListStats are the hygiene signals for one campaign's audience.
type AdvisorListStats struct {
	CampaignID uuid.UUID
	Total      int
	// RoleAddresses counts info@/sales@/support@-style recipients, which reply
	// rarely and complain disproportionately.
	RoleAddresses int
	// FreeMail counts consumer-domain recipients (gmail/yahoo/outlook.com).
	FreeMail int
	// Suppressed counts leads already on the org suppression list: they will be
	// skipped at send time, so a large share means the campaign will underdeliver.
	Suppressed int
	// Unsubscribed counts contacts with subscribed=false.
	Unsubscribed int
	// MissingFirstName counts contacts that would render an empty {{first_name}}.
	MissingFirstName int
}

// AdvisorOrgStats are the org-wide counters the cross-cutting detectors need.
type AdvisorOrgStats struct {
	Mailboxes        int
	ActiveMailboxes  int
	RunningCampaigns int
	SuppressedTotal  int
	// DuplicateContacts counts contacts enrolled in more than one running
	// campaign, which reads to a recipient as two unrelated strangers pitching
	// on the same week.
	DuplicateContacts int
}

// AdvisorSnapshot is everything one evaluation pass reads. It is loaded once
// per org per run so the detectors are pure functions over a consistent view.
type AdvisorSnapshot struct {
	OrganizationID uuid.UUID
	Now            time.Time
	Mailboxes      []AdvisorMailbox
	Campaigns      []AdvisorCampaign
	Steps          []AdvisorStep
	Lists          map[uuid.UUID]AdvisorListStats
	Org            AdvisorOrgStats
	// TrackingHost is this install's tracking host, set by the service rather
	// than loaded from SQL, so a detector can render a complete CNAME.
	TrackingHost string
	// DomainAuthEnforced / DomainAuthGrace mirror the operator's sending-domain
	// authentication gate, also set by the service rather than loaded from SQL.
	// Without them the domain-auth finding could not tell an owner whether
	// their mail has already stopped going out or is only about to.
	DomainAuthEnforced bool
	DomainAuthGrace    time.Duration
}

// AdvisorRepository persists findings and loads the evaluation snapshot.
type AdvisorRepository interface {
	// LoadSnapshot reads one org's full advisor view in a handful of queries.
	LoadSnapshot(ctx context.Context, orgID uuid.UUID, now time.Time) (*AdvisorSnapshot, error)

	// UpsertFinding inserts a new finding or refreshes an existing one by
	// fingerprint, returning the stored row and whether it was newly created.
	// A finding that had been resolved and now fires again is reopened; a
	// dismissed one stays dismissed until it resolves first.
	UpsertFinding(ctx context.Context, f *models.AdvisorFinding) (*models.AdvisorFinding, bool, error)
	// ResolveMissing closes every open/snoozed/applied finding for the org whose
	// fingerprint is not in keep, returning how many it closed.
	ResolveMissing(ctx context.Context, orgID uuid.UUID, keep []string) (int, error)
	// ExpireSnoozes flips snoozed findings whose window has passed back to open.
	ExpireSnoozes(ctx context.Context) (int, error)

	GetFinding(ctx context.Context, orgID, id uuid.UUID) (*models.AdvisorFinding, error)
	// ListFindings returns findings filtered by surface, entity, and status.
	// Empty filters mean "any".
	ListFindings(ctx context.Context, orgID uuid.UUID, f AdvisorFindingFilter) ([]models.AdvisorFinding, error)
	Summary(ctx context.Context, orgID uuid.UUID) (*models.AdvisorSummary, error)

	SetStatus(ctx context.Context, orgID, id uuid.UUID, status models.AdvisorStatus) error
	Snooze(ctx context.Context, orgID, id uuid.UUID, until time.Time) error
	Dismiss(ctx context.Context, orgID, id, userID uuid.UUID, reason string) error
	MarkApplied(ctx context.Context, orgID, id, userID uuid.UUID, result string) error
	// UpdateNarration writes the AI-authored copy onto a finding.
	UpdateNarration(ctx context.Context, orgID, id uuid.UUID, title, detail, remedy string) error

	GetNarration(ctx context.Context, orgID uuid.UUID, cacheKey string) (title, detail, remedy string, ok bool)
	PutNarration(ctx context.Context, orgID uuid.UUID, cacheKey, title, detail, remedy, model string) error

	RecordFeedback(ctx context.Context, orgID, findingID uuid.UUID, userID *uuid.UUID, detectorKey string, helpful bool, reason string) error

	GetSettings(ctx context.Context, orgID uuid.UUID) (*models.AdvisorSettings, error)
	UpdateSettings(ctx context.Context, s *models.AdvisorSettings) error

	StartRun(ctx context.Context, orgID uuid.UUID, trigger string) (uuid.UUID, error)
	FinishRun(ctx context.Context, runID uuid.UUID, newCount, openCount, closedCount, narrated int, runErr string) error
	LastRunAt(ctx context.Context, orgID uuid.UUID) (*time.Time, error)

	// ListOrgsDue returns orgs whose last run is older than staleness (or that
	// have never run), oldest first, bounded by limit.
	ListOrgsDue(ctx context.Context, staleness time.Duration, limit int) ([]uuid.UUID, error)
}

// AdvisorFindingFilter narrows a finding list query.
type AdvisorFindingFilter struct {
	Surface    models.AdvisorSurface
	Category   models.AdvisorCategory
	EntityType string
	EntityID   *uuid.UUID
	// Statuses defaults to open + snoozed when empty.
	Statuses []models.AdvisorStatus
	Limit    int
}

type advisorRepository struct {
	db *db.DB
}

func NewAdvisorRepository(database *db.DB) AdvisorRepository {
	return &advisorRepository{db: database}
}

// findingColumns is the shared SELECT list, kept in one place so scanFinding
// stays in sync with every query.
const findingColumns = `
	id, organization_id, fingerprint, detector_key, category, severity, surface,
	entity_type, entity_id, entity_label, parent_type, parent_id, status, impact,
	title, group_title, detail, remedy, steps, snippets, narrated, evidence, action,
	first_seen_at, last_seen_at, resolved_at, snoozed_until,
	dismissed_at, dismiss_reason, applied_at, applied_by, applied_result`

func scanFinding(row db.Scannable) (*models.AdvisorFinding, error) {
	var f models.AdvisorFinding
	var evidence, action, snippets []byte
	if err := row.Scan(
		&f.ID, &f.OrganizationID, &f.Fingerprint, &f.DetectorKey, &f.Category, &f.Severity, &f.Surface,
		&f.EntityType, &f.EntityID, &f.EntityLabel, &f.ParentType, &f.ParentID, &f.Status, &f.Impact,
		&f.Title, &f.GroupTitle, &f.Detail, &f.Remedy, &f.Steps, &snippets, &f.Narrated, &evidence, &action,
		&f.FirstSeenAt, &f.LastSeenAt, &f.ResolvedAt, &f.SnoozedUntil,
		&f.DismissedAt, &f.DismissReason, &f.AppliedAt, &f.AppliedBy, &f.AppliedResult,
	); err != nil {
		return nil, err
	}
	if len(evidence) > 0 {
		f.Evidence = json.RawMessage(evidence)
	}
	if len(snippets) > 0 {
		_ = json.Unmarshal(snippets, &f.Snippets)
	}
	if len(action) > 0 {
		var a models.AdvisorAction
		if err := json.Unmarshal(action, &a); err == nil && a.Tool != "" {
			f.Action = &a
		}
	}
	return &f, nil
}

func (r *advisorRepository) UpsertFinding(ctx context.Context, f *models.AdvisorFinding) (*models.AdvisorFinding, bool, error) {
	var actionJSON []byte
	if f.Action != nil {
		b, err := json.Marshal(f.Action)
		if err != nil {
			return nil, false, err
		}
		actionJSON = b
	}
	evidence := f.Evidence
	if len(evidence) == 0 {
		evidence = json.RawMessage(`{}`)
	}
	// A nil slice would bind as NULL against a NOT NULL column.
	steps := f.Steps
	if steps == nil {
		steps = []string{}
	}
	snippetsJSON := []byte(`[]`)
	if len(f.Snippets) > 0 {
		if b, err := json.Marshal(f.Snippets); err == nil {
			snippetsJSON = b
		}
	}

	// On conflict the detector's fresh severity/evidence/action always win, but
	// the human decisions do not: a dismissed finding stays dismissed, and a
	// snooze keeps running. Anything else (open, applied, resolved) goes back to
	// open, which is what reopens a finding the applied fix did not actually fix.
	//
	// Narration is only cleared when the evidence hash moved, so stable findings
	// keep their copy across runs for free.
	query := `
		INSERT INTO advisor_findings (
			organization_id, fingerprint, detector_key, category, severity, surface,
			entity_type, entity_id, entity_label, parent_type, parent_id, status, impact,
			title, group_title, detail, remedy, steps, snippets, narrated, evidence, evidence_hash, action
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'open',$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
		ON CONFLICT (organization_id, fingerprint) DO UPDATE SET
			severity      = EXCLUDED.severity,
			category      = EXCLUDED.category,
			surface       = EXCLUDED.surface,
			entity_label  = EXCLUDED.entity_label,
			parent_type   = EXCLUDED.parent_type,
			parent_id     = EXCLUDED.parent_id,
			impact        = EXCLUDED.impact,
			group_title   = EXCLUDED.group_title,
			-- Steps are fixed per-detector copy rather than narration, so they
			-- always take the current build's wording.
			steps         = EXCLUDED.steps,
			snippets      = EXCLUDED.snippets,
			evidence      = EXCLUDED.evidence,
			action        = EXCLUDED.action,
			last_seen_at  = NOW(),
			resolved_at   = NULL,
			title    = CASE WHEN advisor_findings.evidence_hash IS DISTINCT FROM EXCLUDED.evidence_hash
			                THEN EXCLUDED.title ELSE advisor_findings.title END,
			detail   = CASE WHEN advisor_findings.evidence_hash IS DISTINCT FROM EXCLUDED.evidence_hash
			                THEN EXCLUDED.detail ELSE advisor_findings.detail END,
			remedy   = CASE WHEN advisor_findings.evidence_hash IS DISTINCT FROM EXCLUDED.evidence_hash
			                THEN EXCLUDED.remedy ELSE advisor_findings.remedy END,
			narrated = CASE WHEN advisor_findings.evidence_hash IS DISTINCT FROM EXCLUDED.evidence_hash
			                THEN false ELSE advisor_findings.narrated END,
			evidence_hash = EXCLUDED.evidence_hash,
			status = CASE
				WHEN advisor_findings.status = 'dismissed' THEN 'dismissed'
				WHEN advisor_findings.status = 'snoozed'
				     AND advisor_findings.snoozed_until > NOW() THEN 'snoozed'
				ELSE 'open'
			END
		RETURNING ` + findingColumns + `, (xmax::text::bigint = 0) AS inserted`

	var out models.AdvisorFinding
	var evidenceOut, actionOut, snippetsOut []byte
	var inserted bool
	err := r.db.QueryRow(ctx, query,
		f.OrganizationID, f.Fingerprint, f.DetectorKey, f.Category, f.Severity, f.Surface,
		f.EntityType, f.EntityID, f.EntityLabel, f.ParentType, f.ParentID, f.Impact,
		f.Title, f.GroupTitle, f.Detail, f.Remedy, steps, snippetsJSON, f.Narrated, evidence, evidenceHashOf(f), actionJSON,
	).Scan(
		&out.ID, &out.OrganizationID, &out.Fingerprint, &out.DetectorKey, &out.Category, &out.Severity, &out.Surface,
		&out.EntityType, &out.EntityID, &out.EntityLabel, &out.ParentType, &out.ParentID, &out.Status, &out.Impact,
		&out.Title, &out.GroupTitle, &out.Detail, &out.Remedy, &out.Steps, &snippetsOut, &out.Narrated, &evidenceOut, &actionOut,
		&out.FirstSeenAt, &out.LastSeenAt, &out.ResolvedAt, &out.SnoozedUntil,
		&out.DismissedAt, &out.DismissReason, &out.AppliedAt, &out.AppliedBy, &out.AppliedResult,
		&inserted,
	)
	if err != nil {
		return nil, false, err
	}
	if len(evidenceOut) > 0 {
		out.Evidence = json.RawMessage(evidenceOut)
	}
	if len(snippetsOut) > 0 {
		_ = json.Unmarshal(snippetsOut, &out.Snippets)
	}
	if len(actionOut) > 0 {
		var a models.AdvisorAction
		if json.Unmarshal(actionOut, &a) == nil && a.Tool != "" {
			out.Action = &a
		}
	}
	return &out, inserted, nil
}

// evidenceHashOf is set by the service before the upsert; kept as a helper so
// the column can never be written from a stale field.
func evidenceHashOf(f *models.AdvisorFinding) string {
	return f.EvidenceHash()
}

func (r *advisorRepository) ResolveMissing(ctx context.Context, orgID uuid.UUID, keep []string) (int, error) {
	query := `
		UPDATE advisor_findings
		SET status = 'resolved', resolved_at = NOW()
		WHERE organization_id = $1
		  AND status IN ('open', 'snoozed', 'applied', 'dismissed')
		  AND NOT (fingerprint = ANY($2))`
	tag, err := r.db.Exec(ctx, query, orgID, keep)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (r *advisorRepository) ExpireSnoozes(ctx context.Context) (int, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE advisor_findings SET status = 'open', snoozed_until = NULL
		WHERE status = 'snoozed' AND snoozed_until IS NOT NULL AND snoozed_until <= NOW()`)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (r *advisorRepository) GetFinding(ctx context.Context, orgID, id uuid.UUID) (*models.AdvisorFinding, error) {
	row := r.db.QueryRow(ctx, `SELECT `+findingColumns+` FROM advisor_findings WHERE organization_id = $1 AND id = $2`, orgID, id)
	f, err := scanFinding(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return f, err
}

func (r *advisorRepository) ListFindings(ctx context.Context, orgID uuid.UUID, f AdvisorFindingFilter) ([]models.AdvisorFinding, error) {
	statuses := f.Statuses
	if len(statuses) == 0 {
		statuses = []models.AdvisorStatus{models.AdvisorStatusOpen, models.AdvisorStatusApplied}
	}
	strs := make([]string, 0, len(statuses))
	for _, s := range statuses {
		strs = append(strs, string(s))
	}
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	// Ordering is the product's priority model: severity first, then the
	// detector's own impact weight, then most-recently-confirmed.
	query := `
		SELECT ` + findingColumns + `
		FROM advisor_findings
		WHERE organization_id = $1
		  AND status = ANY($2)
		  AND ($3 = '' OR surface = $3)
		  AND ($4 = '' OR category = $4)
		  -- An entity-scoped read matches the subject OR its parent, so a
		  -- campaign's strip includes the copy findings on its own steps.
		  AND ($5 = '' OR entity_type = $5 OR parent_type = $5)
		  AND ($6::uuid IS NULL OR entity_id = $6 OR parent_id = $6)
		ORDER BY
			CASE severity WHEN 'critical' THEN 4 WHEN 'high' THEN 3 WHEN 'medium' THEN 2 ELSE 1 END DESC,
			impact DESC,
			last_seen_at DESC
		LIMIT $7`

	rows, err := r.db.Query(ctx, query, orgID, strs, string(f.Surface), string(f.Category), f.EntityType, f.EntityID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.AdvisorFinding, 0, 16)
	for rows.Next() {
		found, err := scanFinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *found)
	}
	return out, rows.Err()
}

func (r *advisorRepository) Summary(ctx context.Context, orgID uuid.UUID) (*models.AdvisorSummary, error) {
	out := &models.AdvisorSummary{Surfaces: []models.AdvisorSurfaceCount{}}

	rows, err := r.db.Query(ctx, `
		SELECT surface, severity, COUNT(*)
		FROM advisor_findings
		WHERE organization_id = $1 AND status = 'open'
		GROUP BY surface, severity`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	bySurface := map[string]*models.AdvisorSurfaceCount{}
	for rows.Next() {
		var surface, severity string
		var n int
		if err := rows.Scan(&surface, &severity, &n); err != nil {
			return nil, err
		}
		sc := bySurface[surface]
		if sc == nil {
			sc = &models.AdvisorSurfaceCount{Surface: models.AdvisorSurface(surface)}
			bySurface[surface] = sc
		}
		sc.Total += n
		out.Total += n
		switch models.AdvisorSeverity(severity) {
		case models.AdvisorCritical:
			out.Critical += n
			sc.Critical += n
		case models.AdvisorHigh:
			out.High += n
			sc.High += n
		case models.AdvisorMedium:
			out.Medium += n
		default:
			out.Low += n
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, sc := range bySurface {
		out.Surfaces = append(out.Surfaces, *sc)
	}
	out.Score = models.AdvisorScore(out.Critical, out.High, out.Medium, out.Low)

	if last, err := r.LastRunAt(ctx, orgID); err == nil {
		out.LastRunAt = last
	}
	return out, nil
}

func (r *advisorRepository) SetStatus(ctx context.Context, orgID, id uuid.UUID, status models.AdvisorStatus) error {
	_, err := r.db.Exec(ctx, `UPDATE advisor_findings SET status = $3 WHERE organization_id = $1 AND id = $2`, orgID, id, string(status))
	return err
}

func (r *advisorRepository) Snooze(ctx context.Context, orgID, id uuid.UUID, until time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE advisor_findings SET status = 'snoozed', snoozed_until = $3
		WHERE organization_id = $1 AND id = $2`, orgID, id, until)
	return err
}

func (r *advisorRepository) Dismiss(ctx context.Context, orgID, id, userID uuid.UUID, reason string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE advisor_findings
		SET status = 'dismissed', dismissed_at = NOW(), dismissed_by = $3, dismiss_reason = $4
		WHERE organization_id = $1 AND id = $2`, orgID, id, userID, reason)
	return err
}

func (r *advisorRepository) MarkApplied(ctx context.Context, orgID, id, userID uuid.UUID, result string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE advisor_findings
		SET status = 'applied', applied_at = NOW(), applied_by = $3, applied_result = $4
		WHERE organization_id = $1 AND id = $2`, orgID, id, userID, result)
	return err
}

func (r *advisorRepository) UpdateNarration(ctx context.Context, orgID, id uuid.UUID, title, detail, remedy string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE advisor_findings SET title = $3, detail = $4, remedy = $5, narrated = true
		WHERE organization_id = $1 AND id = $2`, orgID, id, title, detail, remedy)
	return err
}

func (r *advisorRepository) GetNarration(ctx context.Context, orgID uuid.UUID, cacheKey string) (string, string, string, bool) {
	var title, detail, remedy string
	err := r.db.QueryRow(ctx, `
		SELECT title, detail, remedy FROM advisor_narrations
		WHERE organization_id = $1 AND cache_key = $2`, orgID, cacheKey).Scan(&title, &detail, &remedy)
	if err != nil {
		return "", "", "", false
	}
	return title, detail, remedy, true
}

func (r *advisorRepository) PutNarration(ctx context.Context, orgID uuid.UUID, cacheKey, title, detail, remedy, model string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO advisor_narrations (organization_id, cache_key, title, detail, remedy, model)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (organization_id, cache_key) DO UPDATE SET
			title = EXCLUDED.title, detail = EXCLUDED.detail,
			remedy = EXCLUDED.remedy, model = EXCLUDED.model, created_at = NOW()`,
		orgID, cacheKey, title, detail, remedy, model)
	return err
}

func (r *advisorRepository) RecordFeedback(ctx context.Context, orgID, findingID uuid.UUID, userID *uuid.UUID, detectorKey string, helpful bool, reason string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO advisor_feedback (organization_id, finding_id, detector_key, user_id, helpful, reason)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (finding_id, user_id) DO UPDATE SET
			helpful = EXCLUDED.helpful, reason = EXCLUDED.reason, created_at = NOW()`,
		orgID, findingID, detectorKey, userID, helpful, reason)
	return err
}

func (r *advisorRepository) GetSettings(ctx context.Context, orgID uuid.UUID) (*models.AdvisorSettings, error) {
	s := models.DefaultAdvisorSettings(orgID)
	err := r.db.QueryRow(ctx, `
		SELECT enabled, muted_categories, muted_detectors, min_severity, autopilot, autopilot_actor_id, updated_at
		FROM advisor_settings WHERE organization_id = $1`, orgID).
		Scan(&s.Enabled, &s.MutedCategories, &s.MutedDetectors, &s.MinSeverity,
			&s.Autopilot, &s.AutopilotActorID, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (r *advisorRepository) UpdateSettings(ctx context.Context, s *models.AdvisorSettings) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO advisor_settings (organization_id, enabled, muted_categories, muted_detectors, min_severity,
			autopilot, autopilot_actor_id, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())
		ON CONFLICT (organization_id) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			muted_categories = EXCLUDED.muted_categories,
			muted_detectors = EXCLUDED.muted_detectors,
			min_severity = EXCLUDED.min_severity,
			autopilot = EXCLUDED.autopilot,
			autopilot_actor_id = EXCLUDED.autopilot_actor_id,
			updated_at = NOW()`,
		s.OrganizationID, s.Enabled, s.MutedCategories, s.MutedDetectors, string(s.MinSeverity),
		s.Autopilot, s.AutopilotActorID)
	return err
}

func (r *advisorRepository) StartRun(ctx context.Context, orgID uuid.UUID, trigger string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.QueryRow(ctx, `
		INSERT INTO advisor_runs (organization_id, trigger) VALUES ($1,$2) RETURNING id`, orgID, trigger).Scan(&id)
	return id, err
}

func (r *advisorRepository) FinishRun(ctx context.Context, runID uuid.UUID, newCount, openCount, closedCount, narrated int, runErr string) error {
	_, err := r.db.Exec(ctx, `
		UPDATE advisor_runs
		SET finished_at = NOW(), findings_new = $2, findings_open = $3,
		    findings_closed = $4, narrated = $5, error = $6
		WHERE id = $1`, runID, newCount, openCount, closedCount, narrated, runErr)
	return err
}

func (r *advisorRepository) LastRunAt(ctx context.Context, orgID uuid.UUID) (*time.Time, error) {
	var t *time.Time
	err := r.db.QueryRow(ctx, `
		SELECT MAX(finished_at) FROM advisor_runs WHERE organization_id = $1`, orgID).Scan(&t)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (r *advisorRepository) ListOrgsDue(ctx context.Context, staleness time.Duration, limit int) ([]uuid.UUID, error) {
	// Only orgs with something to advise on: at least one connected mailbox.
	// A brand-new org with nothing set up has no useful advice to receive and
	// should not cost an evaluation pass.
	query := `
		SELECT o.id
		FROM organizations o
		JOIN LATERAL (
			SELECT 1 FROM email_accounts ea WHERE ea.organization_id = o.id LIMIT 1
		) has_mailbox ON true
		LEFT JOIN LATERAL (
			SELECT MAX(started_at) AS last_run FROM advisor_runs ar WHERE ar.organization_id = o.id
		) r ON true
		LEFT JOIN advisor_settings s ON s.organization_id = o.id
		WHERE COALESCE(s.enabled, true)
		  AND (r.last_run IS NULL OR r.last_run < NOW() - make_interval(secs => $1))
		ORDER BY r.last_run ASC NULLS FIRST
		LIMIT $2`
	rows, err := r.db.Query(ctx, query, staleness.Seconds(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
