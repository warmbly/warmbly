package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// ClientReportToken is what a public report token resolves to.
type ClientReportToken struct {
	Report *models.ClientReport
	// Range is the period the link was minted for; empty means the report's own default.
	Range models.ClientReportRange
}

// ClientReportRepository stores client reports, their schedules and the deliveries of scheduled emails.
type ClientReportRepository interface {
	Create(ctx context.Context, r *models.ClientReport) error
	// Get is nil, nil when the workspace has no such report.
	Get(ctx context.Context, orgID, id uuid.UUID) (*models.ClientReport, error)
	// List is a keyset page newest first, after (createdAt, id) when set.
	List(ctx context.Context, orgID uuid.UUID, afterAt *time.Time, afterID uuid.UUID, limit int) ([]models.ClientReport, error)
	Count(ctx context.Context, orgID uuid.UUID) (int, error)
	// Update writes the editable fields; false when the report is gone.
	Update(ctx context.Context, r *models.ClientReport) (bool, error)
	Delete(ctx context.Context, orgID, id uuid.UUID) (bool, error)

	// SetLink replaces the share link with a new token hash.
	SetLink(ctx context.Context, orgID, id uuid.UUID, hash []byte, hint string, expiresAt time.Time) (bool, error)
	// RevokeLinks turns off the share link and every link sent in a scheduled email.
	RevokeLinks(ctx context.Context, orgID, id uuid.UUID) (bool, error)
	// ResolveToken finds the live report a token hash opens, nil when none.
	ResolveToken(ctx context.Context, hash []byte, now time.Time) (*ClientReportToken, error)

	// ResolveScope is what access reaches inside the workspace right now: folders follow their current contents.
	ResolveScope(ctx context.Context, orgID uuid.UUID, access *models.MemberAccess) (*models.ResourceScope, error)
	// OwnedCounts counts how many of the ids belong to the workspace, per kind.
	OwnedCounts(ctx context.Context, orgID uuid.UUID, a *models.MemberAccess) (folders, campaigns, mailboxes int, err error)

	UpsertSchedule(ctx context.Context, orgID, reportID uuid.UUID, s *models.ClientReportSchedule) error
	DeleteSchedule(ctx context.Context, orgID, reportID uuid.UUID) (bool, error)
	// DueSchedules are unpaused schedules whose next run is at or before now.
	DueSchedules(ctx context.Context, now time.Time, limit int) ([]DueClientReportSchedule, error)
	// RecordRun advances a schedule from runAt to next and records the run's delivery, once per (report, runAt).
	RecordRun(ctx context.Context, due DueClientReportSchedule, next time.Time, d *models.ClientReportDelivery, hash []byte) (bool, error)
	// ClaimDeliveries takes unsent deliveries for one attempt each.
	ClaimDeliveries(ctx context.Context, now time.Time, limit, maxAttempts int) ([]models.ClientReportDelivery, error)
	MarkDeliveredTo(ctx context.Context, id uuid.UUID, address string) error
	// SetDeliveryToken replaces a delivery's token hash, for a link rebuilt under a new key.
	SetDeliveryToken(ctx context.Context, id uuid.UUID, hash []byte) error
	FinishDelivery(ctx context.Context, id uuid.UUID, sentAt *time.Time, lastError string) error
}

// DueClientReportSchedule is a schedule ready to run.
type DueClientReportSchedule struct {
	ReportID       uuid.UUID
	OrganizationID uuid.UUID
	Schedule       models.ClientReportSchedule
}

type clientReportRepository struct {
	db *pgxpool.Pool
}

func NewClientReportRepository(pool *pgxpool.Pool) ClientReportRepository {
	return &clientReportRepository{db: pool}
}

const clientReportCols = `r.id, r.organization_id, r.created_by, r.name, r.access_scope,
	r.campaign_folder_ids, r.campaign_ids, r.email_account_ids,
	r.show_overview, r.show_campaigns, r.show_replies, r.show_positive_replies, r.show_mailboxes, r.show_daily_chart,
	r.show_reply_snippets, r.date_range, r.token_hash IS NOT NULL AND r.revoked_at IS NULL, r.token_hint,
	r.link_created_at, r.link_expires_at, r.created_at, r.updated_at,
	s.report_id IS NOT NULL, COALESCE(s.recipients, '{}'), COALESCE(s.cadence, ''), COALESCE(s.weekday, 0),
	COALESCE(s.month_day, 1), COALESCE(s.hour, 0), COALESCE(s.timezone, ''), COALESCE(s.paused, false),
	s.last_sent_at, s.next_run_at`

const clientReportFrom = ` FROM client_reports r LEFT JOIN client_report_schedules s ON s.report_id = r.id`

func scanClientReport(row pgx.Row, now time.Time) (*models.ClientReport, error) {
	var r models.ClientReport
	var linked, scheduled bool
	var hint, cadence, tz string
	var linkCreated, linkExpires, lastSent, nextRun *time.Time
	var weekday, monthDay, hour int16
	var paused bool
	var recipients []string
	if err := row.Scan(&r.ID, &r.OrganizationID, &r.CreatedBy, &r.Name, &r.Access.Scope,
		&r.Access.FolderIDs, &r.Access.CampaignIDs, &r.Access.EmailAccountIDs,
		&r.Sections.Overview, &r.Sections.Campaigns, &r.Sections.Replies, &r.Sections.PositiveReplies, &r.Sections.Mailboxes, &r.Sections.DailyChart,
		&r.ShowReplySnippets, &r.DateRange, &linked, &hint,
		&linkCreated, &linkExpires, &r.CreatedAt, &r.UpdatedAt,
		&scheduled, &recipients, &cadence, &weekday, &monthDay, &hour, &tz, &paused, &lastSent, &nextRun); err != nil {
		return nil, err
	}
	r.Access.Normalize()
	if linked && linkCreated != nil {
		r.Link = &models.ClientReportLink{Hint: hint, CreatedAt: *linkCreated, ExpiresAt: linkExpires,
			Expired: linkExpires != nil && !now.Before(*linkExpires)}
	}
	if scheduled && nextRun != nil {
		r.Schedule = &models.ClientReportSchedule{Recipients: recipients, Cadence: models.ClientReportCadence(cadence),
			Weekday: int(weekday), MonthDay: int(monthDay), Hour: int(hour), Timezone: tz, Paused: paused,
			LastSentAt: lastSent, NextRunAt: *nextRun}
	}
	return &r, nil
}

func (r *clientReportRepository) Create(ctx context.Context, rep *models.ClientReport) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO client_reports (organization_id, created_by, name, access_scope, campaign_folder_ids, campaign_ids, email_account_ids,
			show_overview, show_campaigns, show_replies, show_positive_replies, show_mailboxes, show_daily_chart, show_reply_snippets, date_range)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id, created_at, updated_at`,
		rep.OrganizationID, rep.CreatedBy, rep.Name, rep.Access.Scope,
		nonNilUUIDs(rep.Access.FolderIDs), nonNilUUIDs(rep.Access.CampaignIDs), nonNilUUIDs(rep.Access.EmailAccountIDs),
		rep.Sections.Overview, rep.Sections.Campaigns, rep.Sections.Replies, rep.Sections.PositiveReplies, rep.Sections.Mailboxes, rep.Sections.DailyChart,
		rep.ShowReplySnippets, rep.DateRange,
	).Scan(&rep.ID, &rep.CreatedAt, &rep.UpdatedAt)
}

func (r *clientReportRepository) Get(ctx context.Context, orgID, id uuid.UUID) (*models.ClientReport, error) {
	rep, err := scanClientReport(r.db.QueryRow(ctx, `SELECT `+clientReportCols+clientReportFrom+`
		WHERE r.organization_id = $1 AND r.id = $2`, orgID, id), time.Now())
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return rep, err
}

func (r *clientReportRepository) List(ctx context.Context, orgID uuid.UUID, afterAt *time.Time, afterID uuid.UUID, limit int) ([]models.ClientReport, error) {
	rows, err := r.db.Query(ctx, `SELECT `+clientReportCols+clientReportFrom+`
		WHERE r.organization_id = $1 AND ($2::timestamptz IS NULL OR (r.created_at, r.id) < ($2, $3))
		ORDER BY r.created_at DESC, r.id DESC LIMIT $4`, orgID, afterAt, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now()
	out := []models.ClientReport{}
	for rows.Next() {
		rep, err := scanClientReport(rows, now)
		if err != nil {
			return nil, err
		}
		out = append(out, *rep)
	}
	return out, rows.Err()
}

func (r *clientReportRepository) Count(ctx context.Context, orgID uuid.UUID) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM client_reports WHERE organization_id = $1`, orgID).Scan(&n)
	return n, err
}

func (r *clientReportRepository) Update(ctx context.Context, rep *models.ClientReport) (bool, error) {
	err := r.db.QueryRow(ctx, `
		UPDATE client_reports SET name = $3, access_scope = $4, campaign_folder_ids = $5, campaign_ids = $6, email_account_ids = $7,
			show_overview = $8, show_campaigns = $9, show_replies = $10, show_positive_replies = $11, show_mailboxes = $12,
			show_daily_chart = $13, show_reply_snippets = $14, date_range = $15, updated_at = now()
		WHERE organization_id = $1 AND id = $2
		RETURNING updated_at`,
		rep.OrganizationID, rep.ID, rep.Name, rep.Access.Scope,
		nonNilUUIDs(rep.Access.FolderIDs), nonNilUUIDs(rep.Access.CampaignIDs), nonNilUUIDs(rep.Access.EmailAccountIDs),
		rep.Sections.Overview, rep.Sections.Campaigns, rep.Sections.Replies, rep.Sections.PositiveReplies, rep.Sections.Mailboxes, rep.Sections.DailyChart,
		rep.ShowReplySnippets, rep.DateRange,
	).Scan(&rep.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (r *clientReportRepository) Delete(ctx context.Context, orgID, id uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM client_reports WHERE organization_id = $1 AND id = $2`, orgID, id)
	return tag.RowsAffected() == 1, err
}

func (r *clientReportRepository) SetLink(ctx context.Context, orgID, id uuid.UUID, hash []byte, hint string, expiresAt time.Time) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE client_reports SET token_hash = $3, token_hint = $4, link_created_at = now(), link_expires_at = $5,
			revoked_at = NULL, updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id, hash, hint, expiresAt)
	return tag.RowsAffected() == 1, err
}

func (r *clientReportRepository) RevokeLinks(ctx context.Context, orgID, id uuid.UUID) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	tag, err := tx.Exec(ctx, `
		UPDATE client_reports SET token_hash = NULL, token_hint = '', link_created_at = NULL, link_expires_at = NULL,
			revoked_at = now(), updated_at = now()
		WHERE organization_id = $1 AND id = $2`, orgID, id)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE client_report_deliveries SET revoked_at = now()
		WHERE organization_id = $1 AND report_id = $2 AND revoked_at IS NULL`, orgID, id); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (r *clientReportRepository) ResolveToken(ctx context.Context, hash []byte, now time.Time) (*ClientReportToken, error) {
	rep, err := scanClientReport(r.db.QueryRow(ctx, `SELECT `+clientReportCols+clientReportFrom+`
		WHERE r.token_hash = $1 AND r.revoked_at IS NULL AND (r.link_expires_at IS NULL OR r.link_expires_at > $2)`, hash, now), now)
	if err == nil {
		return &ClientReportToken{Report: rep}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	var reportID, orgID uuid.UUID
	var dateRange models.ClientReportRange
	err = r.db.QueryRow(ctx, `SELECT report_id, organization_id, date_range FROM client_report_deliveries
		WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > $2`, hash, now).Scan(&reportID, &orgID, &dateRange)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rep, err = r.Get(ctx, orgID, reportID)
	if err != nil || rep == nil {
		return nil, err
	}
	return &ClientReportToken{Report: rep, Range: dateRange}, nil
}

func (r *clientReportRepository) ResolveScope(ctx context.Context, orgID uuid.UUID, a *models.MemberAccess) (*models.ResourceScope, error) {
	var campaigns, folders, mailboxes, tags []string
	err := r.db.QueryRow(ctx, `
		WITH granted_folders AS (
			SELECT f.id FROM folders f WHERE f.organization_id = $1 AND f.id = ANY($2::uuid[])
		), in_scope AS (
			SELECT c.id FROM campaigns c
			WHERE c.organization_id = $1 AND (
				c.id = ANY($3::uuid[])
				OR c.id IN (SELECT cf.campaign_id FROM campaign_folders cf WHERE cf.folder_id IN (SELECT id FROM granted_folders)))
		), boxes AS (
			SELECT ea.id FROM email_accounts ea WHERE ea.organization_id = $1 AND ea.id = ANY($4::uuid[])
		)
		SELECT
			COALESCE((SELECT array_agg(id::text) FROM in_scope), '{}'),
			COALESCE((SELECT array_agg(DISTINCT id::text) FROM (
				SELECT id FROM granted_folders
				UNION SELECT cf.folder_id FROM campaign_folders cf WHERE cf.campaign_id IN (SELECT id FROM in_scope)) v), '{}'),
			COALESCE((SELECT array_agg(id::text) FROM boxes), '{}'),
			COALESCE((SELECT array_agg(DISTINCT et.tag_id::text) FROM email_tags et WHERE et.email_id IN (SELECT id FROM boxes)), '{}')
	`, orgID, nonNilUUIDs(a.FolderIDs), nonNilUUIDs(a.CampaignIDs), nonNilUUIDs(a.EmailAccountIDs)).Scan(&campaigns, &folders, &mailboxes, &tags)
	if err != nil {
		return nil, err
	}
	return &models.ResourceScope{Campaigns: textUUIDs(campaigns), Folders: textUUIDs(folders), Mailboxes: textUUIDs(mailboxes), MailboxTags: textUUIDs(tags)}, nil
}

func (r *clientReportRepository) OwnedCounts(ctx context.Context, orgID uuid.UUID, a *models.MemberAccess) (folders, campaigns, mailboxes int, err error) {
	err = r.db.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM folders WHERE organization_id = $1 AND id = ANY($2::uuid[])),
			(SELECT COUNT(*) FROM campaigns WHERE organization_id = $1 AND id = ANY($3::uuid[])),
			(SELECT COUNT(*) FROM email_accounts WHERE organization_id = $1 AND id = ANY($4::uuid[]))
	`, orgID, nonNilUUIDs(a.FolderIDs), nonNilUUIDs(a.CampaignIDs), nonNilUUIDs(a.EmailAccountIDs)).Scan(&folders, &campaigns, &mailboxes)
	return folders, campaigns, mailboxes, err
}

func (r *clientReportRepository) UpsertSchedule(ctx context.Context, orgID, reportID uuid.UUID, s *models.ClientReportSchedule) error {
	tag, err := r.db.Exec(ctx, `
		INSERT INTO client_report_schedules (report_id, organization_id, recipients, cadence, weekday, month_day, hour, timezone, paused, next_run_at)
		SELECT r.id, r.organization_id, $3, $4, $5, $6, $7, $8, $9, $10 FROM client_reports r
		WHERE r.organization_id = $1 AND r.id = $2
		ON CONFLICT (report_id) DO UPDATE SET recipients = EXCLUDED.recipients, cadence = EXCLUDED.cadence,
			weekday = EXCLUDED.weekday, month_day = EXCLUDED.month_day, hour = EXCLUDED.hour, timezone = EXCLUDED.timezone,
			paused = EXCLUDED.paused, next_run_at = EXCLUDED.next_run_at, updated_at = now()`,
		orgID, reportID, s.Recipients, s.Cadence, s.Weekday, s.MonthDay, s.Hour, s.Timezone, s.Paused, s.NextRunAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (r *clientReportRepository) DeleteSchedule(ctx context.Context, orgID, reportID uuid.UUID) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM client_report_schedules WHERE organization_id = $1 AND report_id = $2`, orgID, reportID)
	return tag.RowsAffected() == 1, err
}

func (r *clientReportRepository) DueSchedules(ctx context.Context, now time.Time, limit int) ([]DueClientReportSchedule, error) {
	rows, err := r.db.Query(ctx, `
		SELECT report_id, organization_id, recipients, cadence, weekday, month_day, hour, timezone, last_sent_at, next_run_at
		FROM client_report_schedules WHERE NOT paused AND next_run_at <= $1
		ORDER BY next_run_at LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DueClientReportSchedule
	for rows.Next() {
		var d DueClientReportSchedule
		var weekday, monthDay, hour int16
		if err := rows.Scan(&d.ReportID, &d.OrganizationID, &d.Schedule.Recipients, &d.Schedule.Cadence, &weekday, &monthDay, &hour,
			&d.Schedule.Timezone, &d.Schedule.LastSentAt, &d.Schedule.NextRunAt); err != nil {
			return nil, err
		}
		d.Schedule.Weekday, d.Schedule.MonthDay, d.Schedule.Hour = int(weekday), int(monthDay), int(hour)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *clientReportRepository) RecordRun(ctx context.Context, due DueClientReportSchedule, next time.Time, d *models.ClientReportDelivery, hash []byte) (bool, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	// Compare-and-swap on the slot, so two replicas reading the same due row run it once.
	tag, err := tx.Exec(ctx, `UPDATE client_report_schedules SET next_run_at = $3, updated_at = now()
		WHERE report_id = $1 AND next_run_at = $2 AND NOT paused`, due.ReportID, due.Schedule.NextRunAt, next)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO client_report_deliveries (id, report_id, organization_id, run_at, date_range, recipients, token_hash, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (report_id, run_at) DO NOTHING`,
		d.ID, due.ReportID, due.OrganizationID, d.RunAt, d.DateRange, d.Recipients, hash, d.ExpiresAt); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (r *clientReportRepository) ClaimDeliveries(ctx context.Context, now time.Time, limit, maxAttempts int) ([]models.ClientReportDelivery, error) {
	rows, err := r.db.Query(ctx, `
		UPDATE client_report_deliveries d SET attempts = d.attempts + 1, claimed_at = $1
		WHERE d.id IN (
			SELECT id FROM client_report_deliveries
			WHERE sent_at IS NULL AND revoked_at IS NULL AND attempts < $3 AND expires_at > $1
			  AND (claimed_at IS NULL OR claimed_at < $1 - interval '10 minutes')
			ORDER BY created_at LIMIT $2 FOR UPDATE SKIP LOCKED)
		RETURNING d.id, d.report_id, d.organization_id, d.run_at, d.date_range, d.recipients, d.sent_to, d.token_hash, d.expires_at, d.attempts`,
		now, limit, maxAttempts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.ClientReportDelivery
	for rows.Next() {
		var d models.ClientReportDelivery
		if err := rows.Scan(&d.ID, &d.ReportID, &d.OrganizationID, &d.RunAt, &d.DateRange, &d.Recipients, &d.SentTo, &d.TokenHash, &d.ExpiresAt, &d.Attempts); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *clientReportRepository) MarkDeliveredTo(ctx context.Context, id uuid.UUID, address string) error {
	_, err := r.db.Exec(ctx, `UPDATE client_report_deliveries SET sent_to = array_append(sent_to, $2)
		WHERE id = $1 AND NOT ($2 = ANY(sent_to))`, id, address)
	return err
}

func (r *clientReportRepository) SetDeliveryToken(ctx context.Context, id uuid.UUID, hash []byte) error {
	_, err := r.db.Exec(ctx, `UPDATE client_report_deliveries SET token_hash = $2 WHERE id = $1`, id, hash)
	return err
}

func (r *clientReportRepository) FinishDelivery(ctx context.Context, id uuid.UUID, sentAt *time.Time, lastError string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var reportID uuid.UUID
	if err := tx.QueryRow(ctx, `UPDATE client_report_deliveries SET sent_at = $2, last_error = $3
		WHERE id = $1 RETURNING report_id`, id, sentAt, lastError).Scan(&reportID); err != nil {
		return err
	}
	if sentAt != nil {
		if _, err := tx.Exec(ctx, `UPDATE client_report_schedules SET last_sent_at = $2 WHERE report_id = $1`, reportID, *sentAt); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
