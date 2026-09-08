package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// AdminInsightRepository holds the smaller cross-workspace reads the panel
// shows on existing pages: warmup abuse signals, warmup admin actions, a
// workspace's API keys, workspace transfers and signup acquisition.
type AdminInsightRepository interface {
	// WarmupAbuse ranks mailboxes by invalid warmup-token attempts since the
	// given time, most attempts first.
	WarmupAbuse(ctx context.Context, since time.Time, limit int) ([]models.AdminWarmupAbuseRow, error)
	// WarmupActions lists warmup_admin_actions newest first.
	WarmupActions(ctx context.Context, limit int) ([]models.AdminWarmupAction, error)
	// OrgAPIKeys lists a workspace's API keys, active first, never the hash.
	OrgAPIKeys(ctx context.Context, orgID uuid.UUID) ([]models.AdminOrgAPIKey, error)
	// ListTransfers lists export and import jobs across every workspace,
	// newest first, capped at limit.
	ListTransfers(ctx context.Context, limit int) ([]models.AdminTransferJob, error)
	// Acquisition groups signups from the last days by channel and reports
	// how many converted to a paid subscription.
	Acquisition(ctx context.Context, days int) (*models.AdminAcquisition, error)
}

type adminInsightRepository struct {
	db *db.DB
}

func NewAdminInsightRepository(d *db.DB) AdminInsightRepository {
	return &adminInsightRepository{db: d}
}

func (r *adminInsightRepository) WarmupAbuse(ctx context.Context, since time.Time, limit int) ([]models.AdminWarmupAbuseRow, error) {
	if limit <= 0 {
		limit = 50
	}
	// warmup_pool_participants is unique per mailbox (000097), so the left
	// join adds at most one row.
	const q = `
		SELECT a.email_account_id, ea.email, ea.organization_id, COALESCE(o.name, ''),
		       COUNT(*)::int, MAX(a.created_at),
		       COALESCE(p.blocked_at IS NOT NULL AND (p.blocked_until IS NULL OR p.blocked_until > now()), false),
		       COALESCE(p.spam_score, 0), COALESCE(p.health_state::text, '')
		FROM warmup_invalid_token_attempts a
		JOIN email_accounts ea ON ea.id = a.email_account_id
		LEFT JOIN organizations o ON o.id = ea.organization_id
		LEFT JOIN warmup_pool_participants p ON p.email_account_id = a.email_account_id
		WHERE a.created_at >= $1
		GROUP BY a.email_account_id, ea.email, ea.organization_id, o.name,
		         p.blocked_at, p.blocked_until, p.spam_score, p.health_state
		ORDER BY COUNT(*) DESC, MAX(a.created_at) DESC
		LIMIT $2
	`
	rows, err := r.db.Query(ctx, q, since, limit)
	if err != nil {
		return nil, fmt.Errorf("admin insight: warmup abuse: %w", err)
	}
	defer rows.Close()
	out := []models.AdminWarmupAbuseRow{}
	for rows.Next() {
		var row models.AdminWarmupAbuseRow
		if err := rows.Scan(
			&row.EmailAccountID, &row.Email, &row.OrganizationID, &row.OrganizationName,
			&row.Attempts, &row.LastAttemptAt, &row.Blocked, &row.SpamScore, &row.HealthState,
		); err != nil {
			return nil, fmt.Errorf("admin insight: warmup abuse scan: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *adminInsightRepository) WarmupActions(ctx context.Context, limit int) ([]models.AdminWarmupAction, error) {
	if limit <= 0 {
		limit = 100
	}
	const q = `
		SELECT a.id, a.admin_user_id, COALESCE(u.email, ''), a.email_account_id, COALESCE(ea.email, ''),
		       a.action, a.reason, a.created_at
		FROM warmup_admin_actions a
		LEFT JOIN users u ON u.id = a.admin_user_id
		LEFT JOIN email_accounts ea ON ea.id = a.email_account_id
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT $1
	`
	rows, err := r.db.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("admin insight: warmup actions: %w", err)
	}
	defer rows.Close()
	out := []models.AdminWarmupAction{}
	for rows.Next() {
		var row models.AdminWarmupAction
		if err := rows.Scan(
			&row.ID, &row.AdminUserID, &row.AdminEmail, &row.EmailAccountID, &row.Email,
			&row.Action, &row.Reason, &row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("admin insight: warmup actions scan: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *adminInsightRepository) OrgAPIKeys(ctx context.Context, orgID uuid.UUID) ([]models.AdminOrgAPIKey, error) {
	const q = `
		SELECT k.id, k.name, k.key_prefix, k.key_suffix, k.status, k.permissions,
		       k.user_id, COALESCE(u.email, ''), k.last_used_at, k.expires_at, k.revoked_at, k.created_at,
		       (SELECT COUNT(*) FROM api_key_usage_logs l
		        WHERE l.api_key_id = k.id AND l.created_at >= now() - interval '7 days')
		FROM api_keys k
		LEFT JOIN users u ON u.id = k.user_id
		WHERE k.organization_id = $1
		ORDER BY (k.status = 'active') DESC, k.created_at DESC
	`
	rows, err := r.db.Query(ctx, q, orgID)
	if err != nil {
		return nil, fmt.Errorf("admin insight: org api keys: %w", err)
	}
	defer rows.Close()
	out := []models.AdminOrgAPIKey{}
	for rows.Next() {
		var row models.AdminOrgAPIKey
		if err := rows.Scan(
			&row.ID, &row.Name, &row.KeyPrefix, &row.KeySuffix, &row.Status, &row.Permissions,
			&row.UserID, &row.UserEmail, &row.LastUsedAt, &row.ExpiresAt, &row.RevokedAt, &row.CreatedAt,
			&row.RequestsLast7d,
		); err != nil {
			return nil, fmt.Errorf("admin insight: org api keys scan: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *adminInsightRepository) ListTransfers(ctx context.Context, limit int) ([]models.AdminTransferJob, error) {
	if limit <= 0 {
		limit = 100
	}
	const q = `
		SELECT j.kind, j.id, j.organization_id, COALESCE(o.name, ''), j.requested_by, COALESCE(u.email, ''),
		       j.status, j.groups, j.include_secrets, j.progress_percent, j.progress_stage,
		       j.archive_bytes, j.error_message, j.started_at, j.completed_at, j.expires_at, j.created_at
		FROM (
			SELECT 'export' AS kind, e.id, e.organization_id, e.requested_by, e.status, e.groups,
			       e.include_secrets, e.progress_percent::int AS progress_percent, e.progress_stage,
			       e.archive_bytes, e.error_message, e.started_at, e.completed_at, e.expires_at, e.created_at
			FROM org_export_jobs e
			UNION ALL
			SELECT 'import', i.id, i.organization_id, i.requested_by, i.status, i.groups,
			       false, i.progress_percent::int, i.progress_stage,
			       i.archive_bytes, i.error_message, i.started_at, i.completed_at, NULL::timestamptz, i.created_at
			FROM org_import_jobs i
		) j
		LEFT JOIN organizations o ON o.id = j.organization_id
		LEFT JOIN users u ON u.id = j.requested_by
		ORDER BY j.created_at DESC, j.id DESC
		LIMIT $1
	`
	rows, err := r.db.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("admin insight: transfers: %w", err)
	}
	defer rows.Close()
	out := []models.AdminTransferJob{}
	for rows.Next() {
		var (
			row    models.AdminTransferJob
			status string
			groups []string
		)
		if err := rows.Scan(
			&row.Kind, &row.ID, &row.OrganizationID, &row.OrganizationName, &row.RequestedBy, &row.RequestedByEmail,
			&status, &groups, &row.IncludeSecrets, &row.ProgressPercent, &row.ProgressStage,
			&row.ArchiveBytes, &row.ErrorMessage, &row.StartedAt, &row.CompletedAt, &row.ExpiresAt, &row.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("admin insight: transfers scan: %w", err)
		}
		row.Status = models.OrgTransferStatus(status)
		row.Groups = make([]models.OrgDataGroup, 0, len(groups))
		for _, g := range groups {
			row.Groups = append(row.Groups, models.OrgDataGroup(g))
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *adminInsightRepository) Acquisition(ctx context.Context, days int) (*models.AdminAcquisition, error) {
	if days <= 0 {
		days = 30
	}
	// Paid means a live Stripe subscription; trialing rows carry no Stripe id.
	const cohort = `
		WITH cohort AS (
			SELECT o.id,
			       COALESCE(a.utm_source, '') AS source,
			       COALESCE(a.utm_medium, '') AS medium,
			       COALESCE(a.referrer_host, '') AS referrer,
			       EXISTS (
			           SELECT 1 FROM subscriptions s
			           WHERE s.organization_id = o.id
			             AND s.status IN ('active', 'past_due')
			             AND s.stripe_subscription_id IS NOT NULL
			       ) AS converted
			FROM organizations o
			LEFT JOIN organization_acquisition a ON a.organization_id = o.id
			WHERE o.created_at >= now() - ($1::int * interval '1 day')
		)
	`
	out := &models.AdminAcquisition{
		Days:      days,
		Channels:  []models.AdminAcquisitionChannel{},
		Referrers: []models.AdminAcquisitionReferrer{},
	}

	const totals = cohort + `
		SELECT COUNT(*)::int, COUNT(*) FILTER (WHERE source <> '')::int, COUNT(*) FILTER (WHERE converted)::int
		FROM cohort
	`
	if err := r.db.QueryRow(ctx, totals, days).Scan(&out.Signups, &out.WithChannel, &out.Converted); err != nil {
		return nil, fmt.Errorf("admin insight: acquisition totals: %w", err)
	}

	// Same predicate the trial expiration job uses, looking forward instead.
	const trials = `
		SELECT COUNT(*)::int FROM subscriptions
		WHERE free_trial_ends_at IS NOT NULL
		  AND free_trial_ends_at BETWEEN now() AND now() + interval '7 days'
		  AND stripe_subscription_id IS NULL
		  AND status NOT IN ('canceled', 'incomplete_expired')
	`
	if err := r.db.QueryRow(ctx, trials).Scan(&out.TrialsExpiring7d); err != nil {
		return nil, fmt.Errorf("admin insight: acquisition trials: %w", err)
	}

	const channels = cohort + `
		SELECT source, medium, COUNT(*)::int, COUNT(*) FILTER (WHERE converted)::int
		FROM cohort
		WHERE NOT (source = '' AND medium = '')
		GROUP BY source, medium
		ORDER BY COUNT(*) DESC, source, medium
	`
	rows, err := r.db.Query(ctx, channels, days)
	if err != nil {
		return nil, fmt.Errorf("admin insight: acquisition channels: %w", err)
	}
	for rows.Next() {
		var ch models.AdminAcquisitionChannel
		if err := rows.Scan(&ch.Source, &ch.Medium, &ch.Signups, &ch.Converted); err != nil {
			rows.Close()
			return nil, fmt.Errorf("admin insight: acquisition channels scan: %w", err)
		}
		out.Channels = append(out.Channels, ch)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("admin insight: acquisition channels: %w", err)
	}

	const referrers = cohort + `
		SELECT referrer, COUNT(*)::int
		FROM cohort
		WHERE referrer <> ''
		GROUP BY referrer
		ORDER BY COUNT(*) DESC, referrer
		LIMIT 10
	`
	rows, err = r.db.Query(ctx, referrers, days)
	if err != nil {
		return nil, fmt.Errorf("admin insight: acquisition referrers: %w", err)
	}
	for rows.Next() {
		var ref models.AdminAcquisitionReferrer
		if err := rows.Scan(&ref.Host, &ref.Signups); err != nil {
			rows.Close()
			return nil, fmt.Errorf("admin insight: acquisition referrers scan: %w", err)
		}
		out.Referrers = append(out.Referrers, ref)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("admin insight: acquisition referrers: %w", err)
	}
	return out, nil
}
