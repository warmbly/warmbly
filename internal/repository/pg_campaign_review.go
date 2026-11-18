package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// CampaignReviewRepository holds the review state of campaign leads and their
// per-lead copies of email steps.
type CampaignReviewRepository interface {
	// SetReviewLeads turns review mode on or off. Only a change moves leads:
	// on holds every lead not yet emailed, off approves every waiting lead.
	SetReviewLeads(ctx context.Context, campaignID uuid.UUID, enabled bool) (held, released int, err error)
	// ApproveLeads lifts review holds, on the listed leads or on all of them.
	ApproveLeads(ctx context.Context, campaignID uuid.UUID, contactIDs []uuid.UUID, all bool) (int, error)
	// ReopenLeads puts unheld leads not yet emailed back into review.
	ReopenLeads(ctx context.Context, campaignID uuid.UUID, contactIDs []uuid.UUID) (int, error)

	Counts(ctx context.Context, campaignID uuid.UUID) (*models.CampaignReviewCounts, error)
	// MissingCounts counts unstarted, subscribed leads with each field empty.
	MissingCounts(ctx context.Context, campaignID uuid.UUID, fields []string) (map[string]int, error)
	UnsubscribedCount(ctx context.Context, campaignID uuid.UUID) (int, error)
	ListLeads(ctx context.Context, campaignID uuid.UUID, f models.CampaignReviewLeadsFilter) ([]models.CampaignReviewLead, bool, error)
	GetLead(ctx context.Context, campaignID, contactID uuid.UUID) (*models.CampaignReviewLead, error)

	// AIProgress counts unstarted leads whose copy of the step has every block written, and the rest.
	AIProgress(ctx context.Context, campaignID, stepID uuid.UUID, blockIDs []string) (written, pending int, err error)
	// PendingAILeads lists up to limit unstarted leads still missing a block of the
	// step, within contactIDs when given (else waiting leads only when waitingOnly),
	// after the cursor, and how many such leads there are from the cursor on.
	PendingAILeads(ctx context.Context, campaignID, stepID uuid.UUID, blockIDs []string, contactIDs []uuid.UUID, waitingOnly bool, after *models.CampaignReviewCursor, limit int) ([]models.CampaignReviewCursor, int, error)

	// ActiveAICampaigns lists active campaigns with an AI block in an email step, after the given id.
	ActiveAICampaigns(ctx context.Context, after uuid.UUID, limit int) ([]CampaignOrg, error)

	// SaveLeadAIVariable stores one AI block's copy for a step; ErrLeadStepSent once the step is reserved or sent.
	// Without replace, copy another write stored first is kept.
	SaveLeadAIVariable(ctx context.Context, campaignID, contactID, stepID uuid.UUID, id, text string, replace bool) error

	GetOverride(ctx context.Context, campaignID, contactID, stepID uuid.UUID) (*models.LeadStepOverride, error)
	ListOverrides(ctx context.Context, campaignID, contactID uuid.UUID) ([]models.LeadStepOverride, error)
	// UpsertOverride and DeleteOverride refuse with ErrLeadStepSent once the step is reserved or sent.
	UpsertOverride(ctx context.Context, o models.LeadStepOverride) (*models.LeadStepOverride, error)
	DeleteOverride(ctx context.Context, campaignID, contactID, stepID uuid.UUID) (bool, error)
}

// ErrLeadStepSent is a change to the copy of a step the lead was already sent, or is being sent.
var ErrLeadStepSent = errors.New("the lead was already sent this step")

type campaignReviewRepository struct {
	db *pgxpool.Pool
}

// NewCampaignReviewRepository creates the review repository.
func NewCampaignReviewRepository(pool *pgxpool.Pool) CampaignReviewRepository {
	return &campaignReviewRepository{db: pool}
}

// reviewStatusSQL derives a lead's review state on alias cl; one definition
// for the list, the counts and the filters.
const reviewStatusSQL = `CASE
	WHEN cl.review_pending_at IS NOT NULL THEN 'waiting'
	WHEN campaign_lead_attempted(cl.campaign_id, cl.contact_id) THEN 'started'
	WHEN cl.paused_at IS NOT NULL AND (cl.paused_until IS NULL OR cl.paused_until > NOW()) THEN 'held'
	ELSE 'ready' END`

// blankRunes is every rune unicode.IsSpace accepts, so SQL and strings.TrimSpace agree on a blank field.
const blankRunes = "\t\n\v\f\r \u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"

// missingColumn maps a field name to its contacts column; the map is the whitelist.
var missingColumn = map[string]string{
	"FirstName": "first_name",
	"LastName":  "last_name",
	"Company":   "company",
	"Phone":     "phone",
}

func (r *campaignReviewRepository) SetReviewLeads(ctx context.Context, campaignID uuid.UUID, enabled bool) (int, int, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	// Waits for enrolments in flight, and holds new ones until this commits, so
	// none is inserted against the flag as it was.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(campaign_review_lock_key($1))`, campaignID); err != nil {
		return 0, 0, err
	}
	var changed bool
	err = tx.QueryRow(ctx, `
		UPDATE campaigns SET review_leads = $2, updated_at = NOW()
		WHERE id = $1 AND review_leads IS DISTINCT FROM $2
		RETURNING true`, campaignID, enabled).Scan(&changed)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, tx.Commit(ctx)
	}
	if err != nil {
		return 0, 0, err
	}

	var n int
	if enabled {
		n, err = holdForReview(ctx, tx, campaignID, nil)
	} else {
		n, err = releaseReview(ctx, tx, campaignID, nil, true)
	}
	if err != nil {
		return 0, 0, err
	}
	if enabled {
		return n, 0, tx.Commit(ctx)
	}
	return 0, n, tx.Commit(ctx)
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// holdForReview marks leads not yet emailed as waiting (all of them, or the
// listed ones), and gives each whose hold slot is free the 'review' hold
// older releases read.
func holdForReview(ctx context.Context, q execer, campaignID uuid.UUID, ids []uuid.UUID) (int, error) {
	filter, args := "", []any{campaignID}
	if ids != nil {
		filter, args = " AND cl.contact_id = ANY($2)", append(args, ids)
	}
	cmd, err := q.Exec(ctx, `
		UPDATE campaign_leads cl
		SET review_pending_at = NOW(),
		    paused_at = CASE WHEN `+freeSlotSQL+` THEN NOW() ELSE cl.paused_at END,
		    paused_until = CASE WHEN `+freeSlotSQL+` THEN NULL ELSE cl.paused_until END,
		    pause_reason = CASE WHEN `+freeSlotSQL+` THEN NULL ELSE cl.pause_reason END,
		    pause_source = CASE WHEN `+freeSlotSQL+` THEN 'review' ELSE cl.pause_source END
		WHERE cl.campaign_id = $1 AND cl.review_pending_at IS NULL
		  AND EXISTS (SELECT 1 FROM campaigns c WHERE c.id = $1 AND c.review_leads)
		  AND NOT campaign_lead_attempted(cl.campaign_id, cl.contact_id)`+filter, args...)
	if err != nil {
		return 0, err
	}
	return int(cmd.RowsAffected()), nil
}

// freeSlotSQL is a hold slot holding nothing live, on alias cl.
const freeSlotSQL = `(cl.paused_at IS NULL OR (cl.paused_until IS NOT NULL AND cl.paused_until <= NOW()))`

// releaseReview approves waiting leads (all, or the listed ones) and clears the
// 'review' hold kept for older releases.
func releaseReview(ctx context.Context, q execer, campaignID uuid.UUID, ids []uuid.UUID, all bool) (int, error) {
	filter, args := "", []any{campaignID}
	if !all {
		filter, args = " AND contact_id = ANY($2)", append(args, ids)
	}
	cmd, err := q.Exec(ctx, `
		UPDATE campaign_leads
		SET review_pending_at = NULL,
		    paused_at = CASE WHEN pause_source = 'review' THEN NULL ELSE paused_at END,
		    paused_until = CASE WHEN pause_source = 'review' THEN NULL ELSE paused_until END,
		    pause_reason = CASE WHEN pause_source = 'review' THEN NULL ELSE pause_reason END,
		    pause_source = CASE WHEN pause_source = 'review' THEN NULL ELSE pause_source END
		WHERE campaign_id = $1 AND (review_pending_at IS NOT NULL OR pause_source = 'review')`+filter, args...)
	if err != nil {
		return 0, err
	}
	return int(cmd.RowsAffected()), nil
}

func (r *campaignReviewRepository) ApproveLeads(ctx context.Context, campaignID uuid.UUID, contactIDs []uuid.UUID, all bool) (int, error) {
	if !all && len(contactIDs) == 0 {
		return 0, nil
	}
	return releaseReview(ctx, r.db, campaignID, contactIDs, all)
}

func (r *campaignReviewRepository) ReopenLeads(ctx context.Context, campaignID uuid.UUID, contactIDs []uuid.UUID) (int, error) {
	if len(contactIDs) == 0 {
		return 0, nil
	}
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	// Shared with enrolment, so a reopen sees review turned off before it holds anyone.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(campaign_review_lock_key($1))`, campaignID); err != nil {
		return 0, err
	}
	n, err := holdForReview(ctx, tx, campaignID, contactIDs)
	if err != nil {
		return 0, err
	}
	return n, tx.Commit(ctx)
}

func (r *campaignReviewRepository) Counts(ctx context.Context, campaignID uuid.UUID) (*models.CampaignReviewCounts, error) {
	var out models.CampaignReviewCounts
	query := `
		WITH s AS (SELECT ` + reviewStatusSQL + ` AS status FROM campaign_leads cl WHERE cl.campaign_id = $1)
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE status = 'waiting'),
		       COUNT(*) FILTER (WHERE status = 'ready'),
		       COUNT(*) FILTER (WHERE status = 'held'),
		       COUNT(*) FILTER (WHERE status = 'started'),
		       (SELECT COUNT(DISTINCT o.contact_id) FROM campaign_lead_step_overrides o WHERE o.campaign_id = $1)
		FROM s`
	err := r.db.QueryRow(ctx, query, campaignID).Scan(&out.Total, &out.Waiting, &out.Ready, &out.Held, &out.Started, &out.Personalized)
	if err != nil {
		db.CaptureError(err, query, []any{campaignID}, "queryrow")
		return nil, err
	}
	return &out, nil
}

func (r *campaignReviewRepository) MissingCounts(ctx context.Context, campaignID uuid.UUID, fields []string) (map[string]int, error) {
	out := map[string]int{}
	var cols []string
	var names []string
	for _, f := range fields {
		col, ok := missingColumn[f]
		if !ok {
			continue
		}
		cols = append(cols, fmt.Sprintf("COUNT(*) FILTER (WHERE btrim(c.%s, $2) = '')", col))
		names = append(names, f)
	}
	if len(cols) == 0 {
		return out, nil
	}
	query := `
		SELECT ` + strings.Join(cols, ", ") + `
		FROM campaign_leads cl JOIN contacts c ON c.id = cl.contact_id
		WHERE cl.campaign_id = $1 AND c.subscribed IS NOT FALSE
		  AND NOT campaign_lead_attempted(cl.campaign_id, cl.contact_id)`
	counts := make([]int, len(cols))
	dest := make([]any, len(cols))
	for i := range counts {
		dest[i] = &counts[i]
	}
	if err := r.db.QueryRow(ctx, query, campaignID, blankRunes).Scan(dest...); err != nil {
		db.CaptureError(err, query, []any{campaignID}, "queryrow")
		return nil, err
	}
	for i, n := range names {
		out[n] = counts[i]
	}
	return out, nil
}

func (r *campaignReviewRepository) UnsubscribedCount(ctx context.Context, campaignID uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM campaign_leads cl JOIN contacts c ON c.id = cl.contact_id
		WHERE cl.campaign_id = $1 AND c.subscribed IS FALSE`, campaignID).Scan(&n)
	return n, err
}

const reviewLeadColumns = `cl.contact_id, c.email, c.first_name, c.last_name, c.company, c.phone,
	` + reviewStatusSQL + ` AS status,
	CASE WHEN cl.review_pending_at IS NOT NULL THEN cl.review_pending_at ELSE cl.paused_at END,
	CASE WHEN cl.review_pending_at IS NOT NULL THEN NULL::timestamptz ELSE cl.paused_until END,
	CASE WHEN cl.review_pending_at IS NOT NULL THEN '' ELSE COALESCE(cl.pause_reason, '') END,
	CASE WHEN cl.review_pending_at IS NOT NULL THEN 'review' ELSE COALESCE(cl.pause_source, '') END,
	(SELECT COUNT(*) FROM campaign_lead_step_overrides o WHERE o.campaign_id = cl.campaign_id AND o.contact_id = cl.contact_id),
	cl.added_at`

func scanReviewLead(row pgx.Row, gaps []string, now time.Time) (models.CampaignReviewLead, error) {
	var l models.CampaignReviewLead
	var phone string
	var pausedAt, until *time.Time
	var reason, source string
	if err := row.Scan(&l.ContactID, &l.Email, &l.FirstName, &l.LastName, &l.Company, &phone,
		&l.Status, &pausedAt, &until, &reason, &source, &l.Personalized, &l.AddedAt); err != nil {
		return l, err
	}
	if pausedAt != nil && (until == nil || until.After(now)) {
		l.Hold = &models.LeadHold{Since: *pausedAt, Until: until, Reason: reason, Source: source}
	}
	values := map[string]string{"FirstName": l.FirstName, "LastName": l.LastName, "Company": l.Company, "Phone": phone}
	for _, g := range gaps {
		if v, ok := values[g]; ok && strings.TrimSpace(v) == "" {
			l.Missing = append(l.Missing, g)
		}
	}
	return l, nil
}

func (r *campaignReviewRepository) ListLeads(ctx context.Context, campaignID uuid.UUID, f models.CampaignReviewLeadsFilter) ([]models.CampaignReviewLead, bool, error) {
	where := []string{"cl.campaign_id = $1"}
	args := []any{campaignID}
	add := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.Status != "" && f.Status != "all" {
		where = append(where, "("+reviewStatusSQL+") = "+add(f.Status))
	}
	if col, ok := missingColumn[f.Missing]; ok {
		where = append(where, "btrim(c."+col+", "+add(blankRunes)+") = ''")
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		p := add("%" + escapeLike(q) + "%")
		where = append(where, "(c.email ILIKE "+p+" OR c.first_name ILIKE "+p+" OR c.last_name ILIKE "+p+" OR c.company ILIKE "+p+")")
	}
	if f.Cursor != nil {
		where = append(where, "(c.email, cl.contact_id) > ("+add(f.Cursor.Email)+", "+add(f.Cursor.ContactID)+")")
	}
	limit := f.Limit
	query := `SELECT ` + reviewLeadColumns + `
		FROM campaign_leads cl JOIN contacts c ON c.id = cl.contact_id
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY c.email, cl.contact_id
		LIMIT ` + add(limit+1)
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		db.CaptureError(err, query, nil, "query")
		return nil, false, err
	}
	defer rows.Close()
	now := time.Now()
	out := make([]models.CampaignReviewLead, 0, limit)
	for rows.Next() {
		l, err := scanReviewLead(rows, f.Gaps, now)
		if err != nil {
			return nil, false, err
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(out) > limit
	if more {
		out = out[:limit]
	}
	return out, more, nil
}

func (r *campaignReviewRepository) GetLead(ctx context.Context, campaignID, contactID uuid.UUID) (*models.CampaignReviewLead, error) {
	query := `SELECT ` + reviewLeadColumns + `
		FROM campaign_leads cl JOIN contacts c ON c.id = cl.contact_id
		WHERE cl.campaign_id = $1 AND cl.contact_id = $2`
	l, err := scanReviewLead(r.db.QueryRow(ctx, query, campaignID, contactID), nil, time.Now())
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLeadNotInCampaign
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// pendingAIScope is the unstarted, subscribed leads whose copy of step $2 still
// misses one of blocks $3 and that have no personalized copy of it.
const pendingAIScope = `
	FROM campaign_leads cl
	JOIN contacts c ON c.id = cl.contact_id
	LEFT JOIN campaign_contact_progress p
	  ON p.campaign_id = cl.campaign_id AND p.contact_id = cl.contact_id AND p.sequence_id = $2
	WHERE cl.campaign_id = $1 AND c.subscribed IS NOT FALSE
	  AND NOT campaign_lead_attempted(cl.campaign_id, cl.contact_id)
	  AND NOT EXISTS (SELECT 1 FROM campaign_lead_step_overrides o
	                  WHERE o.campaign_id = cl.campaign_id AND o.contact_id = cl.contact_id AND o.sequence_id = $2)`

func (r *campaignReviewRepository) AIProgress(ctx context.Context, campaignID, stepID uuid.UUID, blockIDs []string) (int, int, error) {
	if len(blockIDs) == 0 {
		return 0, 0, nil
	}
	var written, pending int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE COALESCE(p.ai_variables_resolved, '{}'::jsonb) ?& $3::text[]),
		       COUNT(*) FILTER (WHERE NOT COALESCE(p.ai_variables_resolved, '{}'::jsonb) ?& $3::text[])
		`+pendingAIScope, campaignID, stepID, blockIDs).Scan(&written, &pending)
	return written, pending, err
}

func (r *campaignReviewRepository) PendingAILeads(ctx context.Context, campaignID, stepID uuid.UUID, blockIDs []string, contactIDs []uuid.UUID, waitingOnly bool, after *models.CampaignReviewCursor, limit int) ([]models.CampaignReviewCursor, int, error) {
	if len(blockIDs) == 0 {
		return nil, 0, nil
	}
	scope := pendingAIScope + ` AND NOT COALESCE(p.ai_variables_resolved, '{}'::jsonb) ?& $3::text[]`
	args := []any{campaignID, stepID, blockIDs}
	if len(contactIDs) > 0 {
		args = append(args, contactIDs)
		scope += fmt.Sprintf(` AND cl.contact_id = ANY($%d)`, len(args))
	} else if waitingOnly {
		scope += ` AND cl.review_pending_at IS NOT NULL`
	}
	// The cursor moves a run past leads that failed, so they cannot stall it.
	if after != nil {
		args = append(args, after.Email, after.ContactID)
		scope += fmt.Sprintf(` AND (c.email, cl.contact_id) > ($%d, $%d)`, len(args)-1, len(args))
	}
	// The count rides on the page, so both come from one snapshot.
	rows, err := r.db.Query(ctx, `SELECT c.email, cl.contact_id, COUNT(*) OVER () `+scope+fmt.Sprintf(` ORDER BY c.email, cl.contact_id LIMIT %d`, limit), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var total int
	var out []models.CampaignReviewCursor
	for rows.Next() {
		var c models.CampaignReviewCursor
		if err := rows.Scan(&c.Email, &c.ContactID, &total); err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// CampaignOrg names a campaign and the organization that owns it.
type CampaignOrg struct {
	CampaignID uuid.UUID
	OrgID      uuid.UUID
}

func (r *campaignReviewRepository) ActiveAICampaigns(ctx context.Context, after uuid.UUID, limit int) ([]CampaignOrg, error) {
	rows, err := r.db.Query(ctx, `
		SELECT c.id, c.organization_id FROM campaigns c
		WHERE c.status = 'active' AND c.organization_id IS NOT NULL AND c.id > $1
		  AND EXISTS (SELECT 1 FROM sequences s
		              WHERE s.campaign_id = c.id AND s.kind = 'email'
		                AND s.body_html LIKE '%data-ai-var%')
		ORDER BY c.id LIMIT $2`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CampaignOrg
	for rows.Next() {
		var c CampaignOrg
		if err := rows.Scan(&c.CampaignID, &c.OrgID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

const overrideColumns = `campaign_id, contact_id, sequence_id, subject, body_html, body_plain, edited_by, updated_at`

func scanOverride(row pgx.Row) (*models.LeadStepOverride, error) {
	var o models.LeadStepOverride
	if err := row.Scan(&o.CampaignID, &o.ContactID, &o.StepID, &o.Subject, &o.BodyHTML, &o.BodyPlain, &o.EditedBy, &o.UpdatedAt); err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *campaignReviewRepository) GetOverride(ctx context.Context, campaignID, contactID, stepID uuid.UUID) (*models.LeadStepOverride, error) {
	o, err := scanOverride(r.db.QueryRow(ctx, `SELECT `+overrideColumns+` FROM campaign_lead_step_overrides
		WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3`, campaignID, contactID, stepID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return o, err
}

func (r *campaignReviewRepository) ListOverrides(ctx context.Context, campaignID, contactID uuid.UUID) ([]models.LeadStepOverride, error) {
	rows, err := r.db.Query(ctx, `SELECT `+overrideColumns+` FROM campaign_lead_step_overrides
		WHERE campaign_id = $1 AND contact_id = $2`, campaignID, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.LeadStepOverride
	for rows.Next() {
		o, err := scanOverride(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}

// claimUnsentStep locks the lead's progress row for the step, the row
// ReserveSend claims, so a copy change and a send are ordered either way.
func claimUnsentStep(ctx context.Context, tx pgx.Tx, campaignID, contactID, stepID uuid.UUID) error {
	var ok bool
	err := tx.QueryRow(ctx, `
		INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (campaign_id, contact_id, sequence_id) DO UPDATE SET sequence_id = EXCLUDED.sequence_id
		WHERE campaign_contact_progress.sent_at IS NULL AND campaign_contact_progress.dispatched_at IS NULL
		RETURNING true`, campaignID, contactID, stepID).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrLeadStepSent
	}
	return err
}

func (r *campaignReviewRepository) UpsertOverride(ctx context.Context, o models.LeadStepOverride) (*models.LeadStepOverride, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := claimUnsentStep(ctx, tx, o.CampaignID, o.ContactID, o.StepID); err != nil {
		return nil, err
	}
	out, err := scanOverride(tx.QueryRow(ctx, `
		INSERT INTO campaign_lead_step_overrides (campaign_id, contact_id, sequence_id, subject, body_html, body_plain, edited_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (campaign_id, contact_id, sequence_id) DO UPDATE
		SET subject = EXCLUDED.subject, body_html = EXCLUDED.body_html, body_plain = EXCLUDED.body_plain,
		    edited_by = EXCLUDED.edited_by, updated_at = NOW()
		RETURNING `+overrideColumns,
		o.CampaignID, o.ContactID, o.StepID, o.Subject, o.BodyHTML, o.BodyPlain, o.EditedBy))
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

func (r *campaignReviewRepository) DeleteOverride(ctx context.Context, campaignID, contactID, stepID uuid.UUID) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if err := claimUnsentStep(ctx, tx, campaignID, contactID, stepID); err != nil {
		return false, err
	}
	cmd, err := tx.Exec(ctx, `DELETE FROM campaign_lead_step_overrides
		WHERE campaign_id = $1 AND contact_id = $2 AND sequence_id = $3`, campaignID, contactID, stepID)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, tx.Commit(ctx)
}

func (r *campaignReviewRepository) SaveLeadAIVariable(ctx context.Context, campaignID, contactID, stepID uuid.UUID, id, text string, replace bool) error {
	var ok bool
	err := r.db.QueryRow(ctx, `
		INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, ai_variables_resolved)
		VALUES ($1, $2, $3, jsonb_build_object($4::text, $5::text))
		ON CONFLICT (campaign_id, contact_id, sequence_id) DO UPDATE
		SET ai_variables_resolved = CASE
		        WHEN NOT $6 AND campaign_contact_progress.ai_variables_resolved ? $4::text THEN campaign_contact_progress.ai_variables_resolved
		        ELSE COALESCE(campaign_contact_progress.ai_variables_resolved, '{}'::jsonb) || jsonb_build_object($4::text, $5::text) END
		WHERE campaign_contact_progress.sent_at IS NULL AND campaign_contact_progress.dispatched_at IS NULL
		RETURNING true`, campaignID, contactID, stepID, id, text, replace).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrLeadStepSent
	}
	return err
}
