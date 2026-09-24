package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

type AnalyticsRepository interface {
	// Warmup analytics
	GetWarmupStats(ctx context.Context, orgID uuid.UUID, emailAccountID *uuid.UUID, from, to time.Time) ([]models.WarmupDailyStats, *errx.Error)

	// Campaign analytics
	GetCampaignSummary(ctx context.Context, orgID, campaignID uuid.UUID) (*models.CampaignSummary, *errx.Error)

	// Direct (hand-written) mail analytics.
	GetDirectMailAnalytics(ctx context.Context, orgID uuid.UUID, from, to time.Time) (*models.DirectMailAnalytics, *errx.Error)
	GetCampaignDailyStats(ctx context.Context, campaignID uuid.UUID, from, to time.Time) ([]models.CampaignDailyStats, *errx.Error)
	GetSequenceStats(ctx context.Context, campaignID uuid.UUID) ([]models.SequenceStats, *errx.Error)
	// GetCampaignEngagementBreakdown groups the campaign's human opens and
	// clicks by country, client, device and surface: distinct contacts per bucket, the
	// busiest `limit` buckets of each. A click counts as an open, as it does
	// on the progress row.
	GetCampaignEngagementBreakdown(ctx context.Context, campaignID uuid.UUID, limit int) (*models.CampaignEngagementBreakdown, *errx.Error)

	// Email account status
	GetAccountsWithErrors(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, *errx.Error)
	GetAccountDailyUsage(ctx context.Context, accountID uuid.UUID, date time.Time) (*models.AccountDailyUsage, *errx.Error)

	// Usage overview
	GetEmailAccountCounts(ctx context.Context, orgID uuid.UUID) (*models.AccountsUsage, *errx.Error)
	GetCampaignCounts(ctx context.Context, orgID uuid.UUID, from, to time.Time) (*models.CampaignsUsage, *errx.Error)
	GetContactCounts(ctx context.Context, orgID uuid.UUID) (*models.ContactsUsage, *errx.Error)

	// Dashboard analytics
	GetDashboardOverallStats(ctx context.Context, orgID uuid.UUID, from, to time.Time) (*models.DashboardOverallStats, *errx.Error)
	GetRecentActivity(ctx context.Context, orgID uuid.UUID, limit int) ([]models.RecentActivityItem, *errx.Error)
	GetTopCampaigns(ctx context.Context, orgID uuid.UUID, from, to time.Time, limit int, sortBy string) ([]models.TopCampaignStats, *errx.Error)
	GetDashboardDailyTrend(ctx context.Context, orgID uuid.UUID, from, to time.Time) ([]models.DashboardDailyStats, *errx.Error)
	GetAccountHealthSummary(ctx context.Context, orgID uuid.UUID) (*models.AccountHealthSummary, *errx.Error)

	// Campaign hourly stats
	GetCampaignHourlyStats(ctx context.Context, campaignID uuid.UUID, date time.Time) ([]models.CampaignHourlyStats, *errx.Error)

	// Campaign comparison
	CompareCampaigns(ctx context.Context, orgID uuid.UUID, campaignIDs []uuid.UUID, from, to time.Time) (*models.CampaignComparison, *errx.Error)
}

type analyticsRepository struct {
	DB *db.DB
}

func NewAnalyticsRepository(db *db.DB) AnalyticsRepository {
	return &analyticsRepository{DB: db}
}

func (r *analyticsRepository) GetWarmupStats(ctx context.Context, orgID uuid.UUID, emailAccountID *uuid.UUID, from, to time.Time) ([]models.WarmupDailyStats, *errx.Error) {
	// Sends come from the daily plan rows, arrivals from the verified receipts,
	// joined both ways so a day the mailbox was written to but did not send
	// still shows what came in. Receipts are bucketed on the UTC day, which is
	// the day the plan rows are keyed on, and bounded by the UTC instants the
	// caller passes rather than a session-timezone date cast.
	query := `
		WITH sent AS (
			SELECT
				ws.date,
				SUM(ws.emails_sent) AS emails_sent,
				SUM(ws.emails_replied) AS emails_replied,
				SUM(ws.target_volume) AS target_volume
			FROM warmup_statistics ws
			JOIN email_accounts ea ON ea.id = ws.email_account_id
			WHERE ea.organization_id = $1
			  AND ws.date >= $2
			  AND ws.date <= $3
			  AND ($4::uuid IS NULL OR ws.email_account_id = $4)
			GROUP BY ws.date
		),
		received AS (
			SELECT
				(wr.created_at AT TIME ZONE 'UTC')::date AS date,
				COUNT(*) AS emails_received
			FROM warmup_received wr
			JOIN email_accounts ea ON ea.id = wr.email_account_id
			WHERE ea.organization_id = $1
			  AND wr.created_at >= $2::timestamptz
			  AND wr.created_at < ($3::timestamptz + interval '1 day')
			  AND ($4::uuid IS NULL OR wr.email_account_id = $4)
			GROUP BY 1
		)
		SELECT
			COALESCE(s.date, r.date)::text,
			COALESCE(s.emails_sent, 0),
			COALESCE(s.emails_replied, 0),
			COALESCE(r.emails_received, 0),
			COALESCE(s.target_volume, 0),
			s.date IS NOT NULL
		FROM sent s
		FULL OUTER JOIN received r ON r.date = s.date
		ORDER BY 1 ASC
	`

	params := []any{orgID, from, to, emailAccountID}

	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	stats := make([]models.WarmupDailyStats, 0)
	for rows.Next() {
		var s models.WarmupDailyStats
		if err := rows.Scan(&s.Date, &s.EmailsSent, &s.EmailsReplied, &s.EmailsReceived, &s.TargetVolume, &s.Active); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		stats = append(stats, s)
	}

	return stats, nil
}

// machineClicksCount counts the (step, contact) pairs whose clicks were ALL
// automated: bool_and(machine) is true only when no human click landed on that
// pair. The summary and the per-step stats both join this, so they can never
// disagree about the rule, and rolling the clicks up once runs in about half
// the time of a correlated EXISTS per progress row on a 50k-lead campaign.
// $1 is the campaign id in both queries.
const (
	machineClicksJoin = `
		LEFT JOIN (
			SELECT sequence_id, contact_id, bool_and(machine) AS machine_only
			FROM email_link_clicks
			WHERE campaign_id = $1
			GROUP BY sequence_id, contact_id
		) mc ON mc.sequence_id = ccp.sequence_id AND mc.contact_id = ccp.contact_id`
	machineClicksCount = `COUNT(CASE WHEN ccp.clicked_at IS NULL AND mc.machine_only THEN 1 END) as machine_clicks`
)

func (r *analyticsRepository) GetCampaignSummary(ctx context.Context, orgID, campaignID uuid.UUID) (*models.CampaignSummary, *errx.Error) {
	query := `
		WITH campaign_plan AS (
			SELECT
				(SELECT COUNT(*) FROM campaign_leads WHERE campaign_id = $1) AS total_contacts,
				(SELECT COUNT(*) FROM sequences WHERE campaign_id = $1 AND kind = 'email') AS email_steps
		)
		SELECT
			cp.total_contacts,
			COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) as emails_sent,
			GREATEST(cp.total_contacts * cp.email_steps - COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END), 0) as emails_pending,
			COUNT(CASE WHEN ccp.opened_at IS NOT NULL AND NOT ccp.opened_machine THEN 1 END) as unique_opens,
			COUNT(CASE WHEN ccp.opened_at IS NOT NULL AND ccp.opened_machine THEN 1 END) as machine_opens,
			COUNT(CASE WHEN ccp.clicked_at IS NOT NULL THEN 1 END) as unique_clicks,
			` + machineClicksCount + `,
			COUNT(CASE WHEN ccp.replied_at IS NOT NULL THEN 1 END) as replies,
			COUNT(CASE WHEN ccp.bounced_at IS NOT NULL THEN 1 END) as bounces
		FROM campaigns c
		CROSS JOIN campaign_plan cp
		LEFT JOIN campaign_contact_progress ccp ON ccp.campaign_id = c.id
			AND EXISTS (SELECT 1 FROM sequences s WHERE s.id = ccp.sequence_id AND s.kind = 'email')` + machineClicksJoin + `
		WHERE c.id = $1 AND c.organization_id = $2
		GROUP BY cp.total_contacts, cp.email_steps
	`

	params := []any{campaignID, orgID}

	var summary models.CampaignSummary
	err := r.DB.QueryRow(ctx, query, params...).Scan(
		&summary.TotalContacts,
		&summary.EmailsSent,
		&summary.EmailsPending,
		&summary.UniqueOpens,
		&summary.MachineOpens,
		&summary.UniqueClicks,
		&summary.MachineClicks,
		&summary.Replies,
		&summary.Bounces,
	)
	if err != nil {
		db.CaptureError(err, query, params, "queryrow")
		return nil, errx.InternalError()
	}

	// Calculate rates
	if summary.EmailsSent > 0 {
		summary.OpenRate = float64(summary.UniqueOpens) / float64(summary.EmailsSent) * 100
		summary.ClickRate = float64(summary.UniqueClicks) / float64(summary.EmailsSent) * 100
		summary.ReplyRate = float64(summary.Replies) / float64(summary.EmailsSent) * 100
		summary.BounceRate = float64(summary.Bounces) / float64(summary.EmailsSent) * 100
	}

	return &summary, nil
}

func (r *analyticsRepository) GetCampaignDailyStats(ctx context.Context, campaignID uuid.UUID, from, to time.Time) ([]models.CampaignDailyStats, *errx.Error) {
	query := `
		SELECT
			ccp.sent_at::date::text as date,
			COUNT(*) as sent,
			COUNT(CASE WHEN ccp.opened_at IS NOT NULL AND NOT ccp.opened_machine THEN 1 END) as opens,
			COUNT(CASE WHEN ccp.clicked_at IS NOT NULL THEN 1 END) as clicks,
			COUNT(CASE WHEN ccp.replied_at IS NOT NULL THEN 1 END) as replies
		FROM campaign_contact_progress ccp
		JOIN sequences s ON s.id = ccp.sequence_id AND s.kind = 'email'
		WHERE ccp.campaign_id = $1
		  AND ccp.sent_at IS NOT NULL
		  AND ccp.sent_at::date >= $2
		  AND ccp.sent_at::date <= $3
		GROUP BY ccp.sent_at::date
		ORDER BY ccp.sent_at::date ASC
	`

	params := []any{campaignID, from, to}

	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	stats := make([]models.CampaignDailyStats, 0)
	for rows.Next() {
		var s models.CampaignDailyStats
		if err := rows.Scan(&s.Date, &s.Sent, &s.Opens, &s.Clicks, &s.Replies); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		stats = append(stats, s)
	}

	return stats, nil
}

func (r *analyticsRepository) GetCampaignEngagementBreakdown(ctx context.Context, campaignID uuid.UUID, limit int) (*models.CampaignEngagementBreakdown, *errx.Error) {
	if limit <= 0 {
		limit = 8
	}
	// One query per dimension over the union of both logs; the key
	// expression is the only difference. The client falls back to the
	// browser so a plain webmail open still lands in a named bucket, and
	// unknown stays the empty key.
	bucket := func(keyExpr string) ([]models.EngagementBucket, *errx.Error) {
		// The aggregates are grouped in a subquery and ordered outside it
		// because Postgres only resolves an output column name in ORDER BY
		// when it stands alone: inside `opens + clicks` it looked for a column
		// named `opens` on email_opens and every call failed with 42703.
		query := `
			WITH ev AS (
				SELECT contact_id, 'open' AS kind, client, client_type, device_hidden, browser, device_type, country_code
				FROM email_opens
				WHERE campaign_id = $1 AND NOT machine
				UNION ALL
				SELECT contact_id, 'click' AS kind, client, client_type, device_hidden, browser, device_type, country_code
				FROM email_link_clicks
				WHERE campaign_id = $1 AND NOT machine
			), buckets AS (
				SELECT ` + keyExpr + ` AS key,
				       COUNT(DISTINCT contact_id) FILTER (WHERE kind = 'open') AS opens,
				       COUNT(DISTINCT contact_id) FILTER (WHERE kind = 'click') AS clicks
				FROM ev
				GROUP BY 1
			)
			SELECT key, opens, clicks
			FROM buckets
			ORDER BY opens + clicks DESC, key ASC
			LIMIT $2
		`
		rows, err := r.DB.Query(ctx, query, campaignID, limit)
		if err != nil {
			db.CaptureError(err, query, []any{campaignID, limit}, "GetCampaignEngagementBreakdown")
			return nil, errx.InternalError()
		}
		defer rows.Close()
		out := []models.EngagementBucket{}
		for rows.Next() {
			var b models.EngagementBucket
			if err := rows.Scan(&b.Key, &b.Opens, &b.Clicks); err != nil {
				db.CaptureError(err, "", nil, "GetCampaignEngagementBreakdown scan")
				return nil, errx.InternalError()
			}
			out = append(out, b)
		}
		if err := rows.Err(); err != nil {
			db.CaptureError(err, query, []any{campaignID, limit}, "GetCampaignEngagementBreakdown rows")
			return nil, errx.InternalError()
		}
		return out, nil
	}

	countries, xerr := bucket(`country_code`)
	if xerr != nil {
		return nil, xerr
	}
	// An open with no named client was read in a browser's webmail; a click
	// with none is the browser the link opened in.
	clients, xerr := bucket(`CASE
		WHEN client <> '' THEN client
		WHEN kind = 'open' AND client_type = 'webmail' AND browser <> '' THEN 'Webmail in ' || browser
		ELSE browser END`)
	if xerr != nil {
		return nil, xerr
	}
	devices, xerr := bucket(`CASE WHEN device_type = 'unknown' THEN '' ELSE device_type END`)
	if xerr != nil {
		return nil, xerr
	}
	surfaces, xerr := bucket(`CASE
		WHEN device_hidden THEN '` + models.EngagementSurfaceHidden + `'
		WHEN client_type = 'app' AND device_type IN ('desktop', 'mobile', 'tablet') THEN device_type || '_app'
		WHEN client_type = 'webmail' THEN '` + models.EngagementSurfaceWebmail + `'
		WHEN device_type IN ('desktop', 'mobile', 'tablet') THEN device_type
		ELSE '' END`)
	if xerr != nil {
		return nil, xerr
	}
	return &models.CampaignEngagementBreakdown{Countries: countries, Clients: clients, Devices: devices, Surfaces: surfaces}, nil
}

// GetSequenceStats lists email steps in canvas order; Position is the canvas's "Email N".
func (r *analyticsRepository) GetSequenceStats(ctx context.Context, campaignID uuid.UUID) ([]models.SequenceStats, *errx.Error) {
	query := `
		SELECT
			s.id,
			s.name,
			ROW_NUMBER() OVER (ORDER BY s.position, s.created_at, s.id) as position,
			COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) as emails_sent,
			COUNT(CASE WHEN ccp.opened_at IS NOT NULL AND NOT ccp.opened_machine THEN 1 END) as opens,
			COUNT(CASE WHEN ccp.opened_at IS NOT NULL AND ccp.opened_machine THEN 1 END) as machine_opens,
			COUNT(CASE WHEN ccp.clicked_at IS NOT NULL THEN 1 END) as clicks,
			` + machineClicksCount + `,
			COUNT(CASE WHEN ccp.replied_at IS NOT NULL THEN 1 END) as replies,
			COUNT(CASE WHEN ccp.bounced_at IS NOT NULL THEN 1 END) as bounces
		FROM sequences s
		LEFT JOIN campaign_contact_progress ccp ON ccp.sequence_id = s.id AND ccp.campaign_id = $1` + machineClicksJoin + `
		WHERE s.campaign_id = $1 AND s.kind = 'email'
		GROUP BY s.id, s.name, s.position, s.created_at
		ORDER BY s.position, s.created_at, s.id
	`

	params := []any{campaignID}

	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	stats := make([]models.SequenceStats, 0)
	for rows.Next() {
		var s models.SequenceStats
		if err := rows.Scan(&s.SequenceID, &s.Name, &s.Position, &s.EmailsSent, &s.Opens, &s.MachineOpens, &s.Clicks, &s.MachineClicks, &s.Replies, &s.Bounces); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		// Rates are of the step's own sends, which is the only way one step
		// compares against another that reached fewer contacts.
		s.OpenRate = models.Rate(s.Opens, s.EmailsSent)
		s.ClickRate = models.Rate(s.Clicks, s.EmailsSent)
		s.ReplyRate = models.Rate(s.Replies, s.EmailsSent)
		s.BounceRate = models.Rate(s.Bounces, s.EmailsSent)
		stats = append(stats, s)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, query, params, "rows")
		return nil, errx.InternalError()
	}

	return stats, nil
}

func (r *analyticsRepository) GetAccountsWithErrors(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, *errx.Error) {
	query := `
		SELECT DISTINCT email_account_id
		FROM email_account_errors
		WHERE user_id = $1 AND resolved_at IS NULL
	`

	rows, err := r.DB.Query(ctx, query, userID)
	if err != nil {
		db.CaptureError(err, query, []any{userID}, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	accountIDs := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			continue
		}
		accountIDs = append(accountIDs, id)
	}

	return accountIDs, nil
}

// GetAccountDailyUsage uses the same completed-task ledger as the sending caps.
func (r *analyticsRepository) GetAccountDailyUsage(ctx context.Context, accountID uuid.UUID, date time.Time) (*models.AccountDailyUsage, *errx.Error) {
	query := `
		SELECT
			$2::date::text as date,
			(
				SELECT COUNT(*)
				FROM tasks t
				WHERE t.email_account_id = ea.id
				  AND t.status = 'completed'
				  AND t.task_type = 'campaign'
				  AND t.completed_at >= $2::date
				  AND t.completed_at < $2::date + INTERVAL '1 day'
				  AND ` + taskDispatchedEmail + `
			) as campaign_sent,
			COALESCE(ea.campaign_limit, 50) as campaign_limit,
			(
				SELECT COUNT(*)
				FROM tasks t
				WHERE t.email_account_id = ea.id
				  AND t.status = 'completed'
				  AND t.task_type = 'warmup'
				  AND t.completed_at >= $2::date
				  AND t.completed_at < $2::date + INTERVAL '1 day'
			) as warmup_sent,
			COALESCE(ea.warmup_max, 0) as warmup_limit
		FROM email_accounts ea
		WHERE ea.id = $1
	`

	params := []any{accountID, date}

	var usage models.AccountDailyUsage
	err := r.DB.QueryRow(ctx, query, params...).Scan(
		&usage.Date,
		&usage.CampaignSent,
		&usage.CampaignLimit,
		&usage.WarmupSent,
		&usage.WarmupLimit,
	)
	if err != nil {
		// Every row here hangs off email_accounts, so no row means the mailbox
		// is gone rather than that anything failed. Reported as an incident it
		// filed "no rows in result set" against a query that did exactly what
		// it was asked, and answered the caller 500 for a 404.
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errx.ErrNotFound
		}
		db.CaptureError(err, query, params, "queryrow")
		return nil, errx.InternalError()
	}

	return &usage, nil
}

func (r *analyticsRepository) GetEmailAccountCounts(ctx context.Context, orgID uuid.UUID) (*models.AccountsUsage, *errx.Error) {
	query := `
		SELECT
			COUNT(*) as total,
			COUNT(CASE WHEN status = 'active' THEN 1 END) as active,
			COUNT(CASE WHEN warmup IS NOT NULL THEN 1 END) as in_warmup,
			COUNT(DISTINCT eae.email_account_id) as with_errors
		FROM email_accounts ea
		LEFT JOIN email_account_errors eae ON eae.email_account_id = ea.id AND eae.resolved_at IS NULL
		WHERE ea.organization_id = $1
	`

	var usage models.AccountsUsage
	err := r.DB.QueryRow(ctx, query, orgID).Scan(&usage.Total, &usage.Active, &usage.InWarmup, &usage.WithErrors)
	if err != nil {
		db.CaptureError(err, query, []any{orgID}, "queryrow")
		return nil, errx.InternalError()
	}

	return &usage, nil
}

func (r *analyticsRepository) GetCampaignCounts(ctx context.Context, orgID uuid.UUID, from, to time.Time) (*models.CampaignsUsage, *errx.Error) {
	query := `
		SELECT
			COUNT(*) as total,
			COUNT(CASE WHEN status = 'active' THEN 1 END) as active,
			COUNT(CASE WHEN status = 'paused' THEN 1 END) as paused,
			COUNT(CASE WHEN status = 'draft' THEN 1 END) as draft,
			(SELECT COUNT(*) FROM campaign_contact_progress ccp
			 JOIN campaigns c ON c.id = ccp.campaign_id
			 JOIN sequences s ON s.id = ccp.sequence_id AND s.kind = 'email'
			 WHERE c.organization_id = $1
			   AND ccp.sent_at >= $2 AND ccp.sent_at <= $3) as emails_sent
		FROM campaigns
		WHERE organization_id = $1
	`

	var usage models.CampaignsUsage
	params := []any{orgID, from, to}
	err := r.DB.QueryRow(ctx, query, params...).Scan(&usage.Total, &usage.Active, &usage.Paused, &usage.Draft, &usage.EmailsSent)
	if err != nil {
		db.CaptureError(err, query, params, "queryrow")
		return nil, errx.InternalError()
	}

	return &usage, nil
}

func (r *analyticsRepository) GetContactCounts(ctx context.Context, orgID uuid.UUID) (*models.ContactsUsage, *errx.Error) {
	query := `
		SELECT
			COUNT(*) as total,
			COUNT(CASE WHEN subscribed = true THEN 1 END) as subscribed,
			COUNT(CASE WHEN created_at::date = CURRENT_DATE THEN 1 END) as added_today
		FROM contacts
		WHERE organization_id = $1
	`

	var usage models.ContactsUsage
	err := r.DB.QueryRow(ctx, query, orgID).Scan(&usage.Total, &usage.Subscribed, &usage.AddedToday)
	if err != nil {
		db.CaptureError(err, query, []any{orgID}, "queryrow")
		return nil, errx.InternalError()
	}

	return &usage, nil
}

// Dashboard Analytics Methods

func (r *analyticsRepository) GetDashboardOverallStats(ctx context.Context, orgID uuid.UUID, from, to time.Time) (*models.DashboardOverallStats, *errx.Error) {
	query := `
		SELECT
			COUNT(CASE WHEN ccp.sent_at IS NOT NULL AND ccp.sent_at >= $2 AND ccp.sent_at <= $3 THEN 1 END) as total_sent,
			COUNT(CASE WHEN ccp.opened_at IS NOT NULL AND NOT ccp.opened_machine AND ccp.sent_at >= $2 AND ccp.sent_at <= $3 THEN 1 END) as total_opens,
			COUNT(CASE WHEN ccp.opened_at IS NOT NULL AND ccp.opened_machine AND ccp.sent_at >= $2 AND ccp.sent_at <= $3 THEN 1 END) as machine_opens,
			COUNT(CASE WHEN ccp.clicked_at IS NOT NULL AND ccp.sent_at >= $2 AND ccp.sent_at <= $3 THEN 1 END) as total_clicks,
			COUNT(CASE WHEN ccp.clicked_at IS NULL AND ccp.sent_at >= $2 AND ccp.sent_at <= $3 AND EXISTS (
				SELECT 1 FROM email_link_clicks lc
				WHERE lc.campaign_id = ccp.campaign_id AND lc.contact_id = ccp.contact_id AND lc.sequence_id = ccp.sequence_id AND lc.machine
			) AND NOT EXISTS (
				SELECT 1 FROM email_link_clicks lc
				WHERE lc.campaign_id = ccp.campaign_id AND lc.contact_id = ccp.contact_id AND lc.sequence_id = ccp.sequence_id AND NOT lc.machine
			) THEN 1 END) as machine_clicks,
			COUNT(CASE WHEN ccp.replied_at IS NOT NULL AND ccp.sent_at >= $2 AND ccp.sent_at <= $3 THEN 1 END) as total_replies,
			COUNT(CASE WHEN ccp.bounced_at IS NOT NULL AND ccp.sent_at >= $2 AND ccp.sent_at <= $3 THEN 1 END) as total_bounces,
			(SELECT COUNT(*) FROM campaigns WHERE organization_id = $1 AND status = 'active') as active_campaigns,
			(SELECT COUNT(*) FROM email_accounts WHERE organization_id = $1 AND status = 'active') as active_accounts
		FROM campaign_contact_progress ccp
		JOIN campaigns c ON c.id = ccp.campaign_id
		JOIN sequences s ON s.id = ccp.sequence_id AND s.kind = 'email'
		WHERE c.organization_id = $1
	`

	params := []any{orgID, from, to}

	var stats models.DashboardOverallStats
	err := r.DB.QueryRow(ctx, query, params...).Scan(
		&stats.TotalEmailsSent,
		&stats.TotalOpens,
		&stats.MachineOpens,
		&stats.TotalClicks,
		&stats.MachineClicks,
		&stats.TotalReplies,
		&stats.TotalBounces,
		&stats.ActiveCampaigns,
		&stats.ActiveAccounts,
	)
	if err != nil {
		db.CaptureError(err, query, params, "queryrow")
		return nil, errx.InternalError()
	}

	// Calculate rates
	if stats.TotalEmailsSent > 0 {
		stats.OpenRate = float64(stats.TotalOpens) / float64(stats.TotalEmailsSent) * 100
		stats.ClickRate = float64(stats.TotalClicks) / float64(stats.TotalEmailsSent) * 100
		stats.ReplyRate = float64(stats.TotalReplies) / float64(stats.TotalEmailsSent) * 100
		stats.BounceRate = float64(stats.TotalBounces) / float64(stats.TotalEmailsSent) * 100
	}

	return &stats, nil
}

func (r *analyticsRepository) GetRecentActivity(ctx context.Context, orgID uuid.UUID, limit int) ([]models.RecentActivityItem, *errx.Error) {
	// Union query to get recent opens, clicks, replies, and bounces. The
	// origin of an open or click is looked up only for the rows that make
	// the page, from the person's first logged open or click on the step.
	query := `
		WITH recent_events AS (
			-- Opens
			SELECT 'opened' as type, ccp.campaign_id, c.name as campaign_name,
				   co.email as contact_email, ccp.contact_id, ccp.sequence_id, ccp.opened_at as timestamp, NULL as link
			FROM campaign_contact_progress ccp
			JOIN campaigns c ON c.id = ccp.campaign_id
			JOIN contacts co ON co.id = ccp.contact_id
			WHERE c.organization_id = $1 AND ccp.opened_at IS NOT NULL AND NOT ccp.opened_machine

			UNION ALL

			-- Clicks (the first link a person clicked on the step, when logged per link)
			SELECT 'clicked' as type, ccp.campaign_id, c.name as campaign_name,
				   co.email as contact_email, ccp.contact_id, ccp.sequence_id, ccp.clicked_at as timestamp,
				   (SELECT lc.destination FROM email_link_clicks lc
				    WHERE lc.campaign_id = ccp.campaign_id AND lc.contact_id = ccp.contact_id
				      AND lc.sequence_id = ccp.sequence_id AND lc.machine = false
				    ORDER BY lc.clicked_at LIMIT 1) as link
			FROM campaign_contact_progress ccp
			JOIN campaigns c ON c.id = ccp.campaign_id
			JOIN contacts co ON co.id = ccp.contact_id
			WHERE c.organization_id = $1 AND ccp.clicked_at IS NOT NULL

			UNION ALL

			-- Replies
			SELECT 'replied' as type, ccp.campaign_id, c.name as campaign_name,
				   co.email as contact_email, ccp.contact_id, ccp.sequence_id, ccp.replied_at as timestamp, NULL as link
			FROM campaign_contact_progress ccp
			JOIN campaigns c ON c.id = ccp.campaign_id
			JOIN contacts co ON co.id = ccp.contact_id
			WHERE c.organization_id = $1 AND ccp.replied_at IS NOT NULL

			UNION ALL

			-- Bounces
			SELECT 'bounced' as type, ccp.campaign_id, c.name as campaign_name,
				   co.email as contact_email, ccp.contact_id, ccp.sequence_id, ccp.bounced_at as timestamp, NULL as link
			FROM campaign_contact_progress ccp
			JOIN campaigns c ON c.id = ccp.campaign_id
			JOIN contacts co ON co.id = ccp.contact_id
			WHERE c.organization_id = $1 AND ccp.bounced_at IS NOT NULL
		), page AS (
			SELECT * FROM recent_events
			ORDER BY timestamp DESC
			LIMIT $2
		)
		SELECT p.type, p.campaign_id, p.campaign_name, p.contact_email, p.contact_id, p.timestamp, COALESCE(p.link, '') as link,
		       COALESCE(og.client, ''), COALESCE(og.client_type, ''), COALESCE(og.device_hidden, false),
		       COALESCE(og.device_type, ''), COALESCE(og.os, ''), COALESCE(og.browser, ''),
		       COALESCE(og.country_code, ''), COALESCE(og.region, ''), COALESCE(og.city, '')
		FROM page p
		LEFT JOIN LATERAL (
			SELECT o.opened_at AS at, o.client, o.client_type, o.device_hidden, o.device_type, o.os, o.browser, o.country_code, o.region, o.city
			FROM email_opens o
			WHERE p.type = 'opened'
			  AND o.campaign_id = p.campaign_id AND o.contact_id = p.contact_id AND o.sequence_id = p.sequence_id
			  AND NOT o.machine
			UNION ALL
			SELECT lc.clicked_at, lc.client, lc.client_type, lc.device_hidden, lc.device_type, lc.os, lc.browser, lc.country_code, lc.region, lc.city
			FROM email_link_clicks lc
			WHERE p.type = 'clicked'
			  AND lc.campaign_id = p.campaign_id AND lc.contact_id = p.contact_id AND lc.sequence_id = p.sequence_id
			  AND NOT lc.machine
			ORDER BY 1
			LIMIT 1
		) og ON TRUE
		ORDER BY p.timestamp DESC
	`

	params := []any{orgID, limit}

	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	activities := make([]models.RecentActivityItem, 0)
	for rows.Next() {
		var a models.RecentActivityItem
		var o models.EngagementOrigin
		if err := rows.Scan(&a.Type, &a.CampaignID, &a.CampaignName, &a.ContactEmail, &a.ContactID, &a.Timestamp, &a.Link,
			&o.Client, &o.ClientType, &o.DeviceHidden, &o.DeviceType, &o.OS, &o.Browser,
			&o.CountryCode, &o.Region, &o.City); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		if !o.Empty() {
			a.Origin = &o
		}
		activities = append(activities, a)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, query, params, "rows")
		return nil, errx.InternalError()
	}

	return activities, nil
}

func (r *analyticsRepository) GetTopCampaigns(ctx context.Context, orgID uuid.UUID, from, to time.Time, limit int, sortBy string) ([]models.TopCampaignStats, *errx.Error) {
	// Default sort by emails_sent
	orderClause := "emails_sent DESC"
	switch sortBy {
	case "open_rate":
		orderClause = "open_rate DESC"
	case "click_rate":
		orderClause = "click_rate DESC"
	case "reply_rate":
		orderClause = "reply_rate DESC"
	}

	query := `
		SELECT
			c.id as campaign_id,
			c.name,
			c.status,
			COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) as emails_sent,
			CASE WHEN COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) > 0
				THEN COUNT(CASE WHEN ccp.opened_at IS NOT NULL AND NOT ccp.opened_machine THEN 1 END)::float / COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) * 100
				ELSE 0 END as open_rate,
			CASE WHEN COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) > 0
				THEN COUNT(CASE WHEN ccp.clicked_at IS NOT NULL THEN 1 END)::float / COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) * 100
				ELSE 0 END as click_rate,
			CASE WHEN COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) > 0
				THEN COUNT(CASE WHEN ccp.replied_at IS NOT NULL THEN 1 END)::float / COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) * 100
				ELSE 0 END as reply_rate
		FROM campaigns c
		LEFT JOIN campaign_contact_progress ccp ON ccp.campaign_id = c.id
			AND ccp.sent_at >= $2 AND ccp.sent_at <= $3
			AND EXISTS (SELECT 1 FROM sequences s WHERE s.id = ccp.sequence_id AND s.kind = 'email')
		WHERE c.organization_id = $1
		GROUP BY c.id, c.name, c.status
		HAVING COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) > 0
		ORDER BY ` + orderClause + `
		LIMIT $4
	`

	params := []any{orgID, from, to, limit}

	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	campaigns := make([]models.TopCampaignStats, 0)
	for rows.Next() {
		var c models.TopCampaignStats
		if err := rows.Scan(&c.CampaignID, &c.Name, &c.Status, &c.EmailsSent, &c.OpenRate, &c.ClickRate, &c.ReplyRate); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		campaigns = append(campaigns, c)
	}

	return campaigns, nil
}

func (r *analyticsRepository) GetDashboardDailyTrend(ctx context.Context, orgID uuid.UUID, from, to time.Time) ([]models.DashboardDailyStats, *errx.Error) {
	query := `
		SELECT
			sent_at::date::text as date,
			COUNT(*) as sent,
			COUNT(CASE WHEN opened_at IS NOT NULL AND NOT opened_machine THEN 1 END) as opens,
			COUNT(CASE WHEN clicked_at IS NOT NULL THEN 1 END) as clicks,
			COUNT(CASE WHEN replied_at IS NOT NULL THEN 1 END) as replies
		FROM campaign_contact_progress ccp
		JOIN campaigns c ON c.id = ccp.campaign_id
		JOIN sequences s ON s.id = ccp.sequence_id AND s.kind = 'email'
		WHERE c.organization_id = $1
		  AND ccp.sent_at IS NOT NULL
		  AND ccp.sent_at::date >= $2
		  AND ccp.sent_at::date <= $3
		GROUP BY sent_at::date
		ORDER BY sent_at::date ASC
	`

	params := []any{orgID, from, to}

	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	stats := make([]models.DashboardDailyStats, 0)
	for rows.Next() {
		var s models.DashboardDailyStats
		if err := rows.Scan(&s.Date, &s.Sent, &s.Opens, &s.Clicks, &s.Replies); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		stats = append(stats, s)
	}

	return stats, nil
}

func (r *analyticsRepository) GetAccountHealthSummary(ctx context.Context, orgID uuid.UUID) (*models.AccountHealthSummary, *errx.Error) {
	query := `
		WITH account_health AS (
			SELECT CASE
				WHEN ea.status != 'active'
					OR wh.health_state IN ('throttled', 'quarantined', 'blocked')
					OR EXISTS (
						SELECT 1 FROM email_account_errors eae
						WHERE eae.email_account_id = ea.id
						  AND eae.resolved_at IS NULL AND eae.severity = 'CRITICAL'
					) THEN 'error'
				WHEN wh.health_state = 'watch'
					OR EXISTS (
						SELECT 1 FROM email_account_errors eae
						WHERE eae.email_account_id = ea.id
						  AND eae.resolved_at IS NULL AND eae.severity = 'WARNING'
					) THEN 'warning'
				ELSE 'healthy'
			END AS status
			FROM email_accounts ea
			LEFT JOIN LATERAL (
				SELECT health_state
				FROM warmup_pool_participants
				WHERE email_account_id = ea.id
				ORDER BY CASE health_state
					WHEN 'blocked' THEN 0
					WHEN 'quarantined' THEN 1
					WHEN 'throttled' THEN 2
					WHEN 'watch' THEN 3
					WHEN 'healthy' THEN 4
					ELSE 5
				END
				LIMIT 1
			) wh ON true
			WHERE ea.organization_id = $1
		)
		SELECT
			COUNT(*) as total,
			COUNT(*) FILTER (WHERE status = 'healthy') as healthy,
			COUNT(*) FILTER (WHERE status = 'warning') as warning,
			COUNT(*) FILTER (WHERE status = 'error') as error
		FROM account_health
	`

	var summary models.AccountHealthSummary
	err := r.DB.QueryRow(ctx, query, orgID).Scan(&summary.TotalAccounts, &summary.HealthyAccounts, &summary.WarningAccounts, &summary.ErrorAccounts)
	if err != nil {
		db.CaptureError(err, query, []any{orgID}, "queryrow")
		return nil, errx.InternalError()
	}

	return &summary, nil
}

func (r *analyticsRepository) GetCampaignHourlyStats(ctx context.Context, campaignID uuid.UUID, date time.Time) ([]models.CampaignHourlyStats, *errx.Error) {
	query := `
		SELECT
			EXTRACT(HOUR FROM ccp.sent_at)::int as hour,
			COUNT(*) as sent,
			COUNT(CASE WHEN ccp.opened_at IS NOT NULL AND NOT ccp.opened_machine THEN 1 END) as opens,
			COUNT(CASE WHEN ccp.clicked_at IS NOT NULL THEN 1 END) as clicks,
			COUNT(CASE WHEN ccp.replied_at IS NOT NULL THEN 1 END) as replies
		FROM campaign_contact_progress ccp
		JOIN sequences s ON s.id = ccp.sequence_id AND s.kind = 'email'
		WHERE ccp.campaign_id = $1
		  AND ccp.sent_at IS NOT NULL
		  AND ccp.sent_at::date = $2::date
		GROUP BY EXTRACT(HOUR FROM ccp.sent_at)
		ORDER BY hour
	`

	params := []any{campaignID, date}

	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	stats := make([]models.CampaignHourlyStats, 0)
	for rows.Next() {
		var s models.CampaignHourlyStats
		if err := rows.Scan(&s.Hour, &s.Sent, &s.Opens, &s.Clicks, &s.Replies); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		stats = append(stats, s)
	}

	return stats, nil
}

func (r *analyticsRepository) CompareCampaigns(ctx context.Context, orgID uuid.UUID, campaignIDs []uuid.UUID, from, to time.Time) (*models.CampaignComparison, *errx.Error) {
	query := `
		SELECT
			c.id as campaign_id,
			c.name,
			c.status,
			COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) as emails_sent,
			CASE WHEN COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) > 0
				THEN COUNT(CASE WHEN ccp.opened_at IS NOT NULL AND NOT ccp.opened_machine THEN 1 END)::float / COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) * 100
				ELSE 0 END as open_rate,
			CASE WHEN COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) > 0
				THEN COUNT(CASE WHEN ccp.clicked_at IS NOT NULL THEN 1 END)::float / COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) * 100
				ELSE 0 END as click_rate,
			CASE WHEN COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) > 0
				THEN COUNT(CASE WHEN ccp.replied_at IS NOT NULL THEN 1 END)::float / COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) * 100
				ELSE 0 END as reply_rate,
			CASE WHEN COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) > 0
				THEN COUNT(CASE WHEN ccp.bounced_at IS NOT NULL THEN 1 END)::float / COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) * 100
				ELSE 0 END as bounce_rate
		FROM campaigns c
		LEFT JOIN campaign_contact_progress ccp ON ccp.campaign_id = c.id
			AND ccp.sent_at >= $2 AND ccp.sent_at <= $3
			AND EXISTS (SELECT 1 FROM sequences s WHERE s.id = ccp.sequence_id AND s.kind = 'email')
		WHERE c.organization_id = $1 AND c.id = ANY($4)
		GROUP BY c.id, c.name, c.status
		ORDER BY c.name
	`

	params := []any{orgID, from, to, campaignIDs}

	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	campaigns := make([]models.CampaignComparisonItem, 0)
	for rows.Next() {
		var c models.CampaignComparisonItem
		if err := rows.Scan(&c.CampaignID, &c.Name, &c.Status, &c.EmailsSent, &c.OpenRate, &c.ClickRate, &c.ReplyRate, &c.BounceRate); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		campaigns = append(campaigns, c)
	}

	return &models.CampaignComparison{
		Campaigns: campaigns,
		Period: models.DateRange{
			From: from,
			To:   to,
		},
	}, nil
}

// bareAddr extracts the final parenthesized address stored by mailbox sync.
func bareAddr(col string) string {
	return `LOWER(COALESCE(NULLIF((regexp_match(` + col + `, '\(([^()]+)\)\s*$'))[1], ''), TRIM(` + col + `)))`
}

// GetDirectMailAnalytics reports on hand-written mail for one workspace.
//
// Scoping note: unibox_emails carries no organization_id of its own, so every
// query here reaches the workspace through email_accounts, which is also what
// bounds it to mailboxes this workspace actually owns.
func (r *analyticsRepository) GetDirectMailAnalytics(ctx context.Context, orgID uuid.UUID, from, to time.Time) (*models.DirectMailAnalytics, *errx.Error) {
	out := &models.DirectMailAnalytics{
		DailyTrend:  make([]models.DirectMailDailyStats, 0),
		Mailboxes:   make([]models.DirectMailMailboxStats, 0),
		TopContacts: make([]models.DirectMailContact, 0),
	}

	// A synced Sent folder also contains campaign and warmup messages. Exclude
	// those task-backed messages anywhere an outgoing message is counted.
	directOutgoing := `NOT EXISTS (
		SELECT 1
		FROM tasks automated
		WHERE automated.email_account_id = ue.email_id
		  AND automated.task_type IN ('campaign', 'warmup')
		  AND automated.message_id <> ''
		  AND BTRIM(automated.message_id, '<> ') = BTRIM(ue.message_id, '<> ')
	)`

	volumeQuery := `
		SELECT
			COUNT(*) FILTER (WHERE ue.folder = 'sent' AND ` + directOutgoing + `) AS sent,
			COUNT(*) FILTER (WHERE ue.folder = 'inbox') AS received
		FROM unibox_emails ue
		JOIN email_accounts ea ON ea.id = ue.email_id
		WHERE ea.organization_id = $1
		  AND ue.internal_date >= $2 AND ue.internal_date <= $3
	`
	if err := r.DB.QueryRow(ctx, volumeQuery, orgID, from, to).Scan(&out.Volume.Sent, &out.Volume.Received); err != nil {
		db.CaptureError(err, volumeQuery, []any{orgID, from, to}, "queryrow")
		return nil, errx.InternalError()
	}

	// A reply is the first real inbound message after a direct thread's first
	// send. Build thread history without clipping it to the reporting window.
	realInbound := `
		ue.folder = 'inbox'
		AND ue.subject !~* '^(delivery status notification|undeliverable|mail delivery|returned mail|automatic reply|auto(matic)?[ -]?response|out of office)'
		AND COALESCE(ue.from_addr[1], '') !~* '(mailer-daemon|postmaster|no-?reply)'
		AND ` + bareAddr("COALESCE(ue.from_addr[1], '')") + ` NOT IN (
			SELECT LOWER(email) FROM email_accounts WHERE organization_id = $1
		)
	`
	replyQuery := `
		WITH threads AS (
			SELECT ue.email_id, ue.thread_id,
			       MIN(ue.internal_date) FILTER (WHERE ue.folder = 'sent' AND ` + directOutgoing + `) AS first_sent,
			       MIN(ue.internal_date) FILTER (WHERE ` + realInbound + `) AS first_in
			FROM unibox_emails ue
			JOIN email_accounts ea ON ea.id = ue.email_id
			WHERE ea.organization_id = $1
			  AND ue.thread_id <> ''
			GROUP BY ue.email_id, ue.thread_id
		), ours AS (
			SELECT first_sent, CASE WHEN first_in <= $3 THEN first_in END AS first_in
			FROM threads
			WHERE first_sent >= $2 AND first_sent <= $3
			  AND (first_in IS NULL OR first_in > first_sent)
		)
		SELECT
			COUNT(*),
			COUNT(*) FILTER (WHERE first_in IS NOT NULL),
			COALESCE(
				PERCENTILE_CONT(0.5) WITHIN GROUP (
					ORDER BY EXTRACT(EPOCH FROM (first_in - first_sent)) / 60
				) FILTER (WHERE first_in IS NOT NULL),
			0)
		FROM ours
	`
	var medianMinutes float64
	if err := r.DB.QueryRow(ctx, replyQuery, orgID, from, to).Scan(
		&out.Volume.ThreadsStarted, &out.Volume.Replied, &medianMinutes,
	); err != nil {
		db.CaptureError(err, replyQuery, []any{orgID, from, to}, "queryrow")
		return nil, errx.InternalError()
	}
	out.Volume.MedianReplyMinutes = int(medianMinutes)
	if out.Volume.ThreadsStarted > 0 {
		out.Volume.ReplyRate = float64(out.Volume.Replied) / float64(out.Volume.ThreadsStarted) * 100
	}

	const bounceQuery = `
		SELECT COUNT(*)
		FROM unibox_emails ue
		JOIN email_accounts ea ON ea.id = ue.email_id
		WHERE ea.organization_id = $1
		  AND ue.folder = 'inbox'
		  AND ue.internal_date >= $2 AND ue.internal_date <= $3
		  AND (ue.subject ~* '^(delivery status notification|undeliverable|mail delivery|returned mail)'
		       OR COALESCE(ue.from_addr[1], '') ~* '(mailer-daemon|postmaster)')
	`
	if err := r.DB.QueryRow(ctx, bounceQuery, orgID, from, to).Scan(&out.Volume.Bounced); err != nil {
		db.CaptureError(err, bounceQuery, []any{orgID, from, to}, "queryrow")
		return nil, errx.InternalError()
	}

	// ── Opt-in tracking, from the send records ─────────────────────────
	const trackingQuery = `
		SELECT
			(SELECT COUNT(*) FROM email_accounts WHERE organization_id = $1) AS mailboxes_total,
			(SELECT COUNT(*) FROM email_accounts WHERE organization_id = $1 AND track_direct_mail) AS mailboxes_opted_in,
			COUNT(*)                                                  AS tracked_sent,
			COUNT(*) FILTER (WHERE et.opened_at IS NOT NULL AND NOT et.opened_machine) AS opened,
			COUNT(*) FILTER (WHERE et.opened_at IS NOT NULL AND et.opened_machine)     AS machine_opened,
			COUNT(*) FILTER (WHERE et.clicked_at IS NOT NULL)         AS clicked
		FROM email_tasks et
		JOIN tasks t ON t.id = et.task_id
		JOIN email_accounts ea ON ea.id = t.email_account_id
		WHERE ea.organization_id = $1
		  AND et.tracked
		  AND t.status = 'completed'
		  AND t.completed_at >= $2 AND t.completed_at <= $3
	`
	tr := &out.Tracking
	if err := r.DB.QueryRow(ctx, trackingQuery, orgID, from, to).Scan(
		&tr.MailboxesTotal, &tr.MailboxesOptedIn, &tr.TrackedSent, &tr.Opened, &tr.MachineOpened, &tr.Clicked,
	); err != nil {
		db.CaptureError(err, trackingQuery, []any{orgID, from, to}, "queryrow")
		return nil, errx.InternalError()
	}
	if tr.TrackedSent > 0 {
		tr.OpenRate = float64(tr.Opened) / float64(tr.TrackedSent) * 100
		tr.ClickRate = float64(tr.Clicked) / float64(tr.TrackedSent) * 100
	}

	trendQuery := `
		SELECT DATE_TRUNC('day', ue.internal_date) AS day,
		       COUNT(*) FILTER (WHERE ue.folder = 'sent' AND ` + directOutgoing + `) AS sent,
		       COUNT(*) FILTER (WHERE ue.folder = 'inbox') AS received
		FROM unibox_emails ue
		JOIN email_accounts ea ON ea.id = ue.email_id
		WHERE ea.organization_id = $1
		  AND ue.internal_date >= $2 AND ue.internal_date <= $3
		GROUP BY day
		ORDER BY day
	`
	rows, err := r.DB.Query(ctx, trendQuery, orgID, from, to)
	if err != nil {
		db.CaptureError(err, trendQuery, []any{orgID, from, to}, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()
	for rows.Next() {
		var d models.DirectMailDailyStats
		if err := rows.Scan(&d.Date, &d.Sent, &d.Received); err != nil {
			return nil, errx.InternalError()
		}
		out.DailyTrend = append(out.DailyTrend, d)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, trendQuery, []any{orgID, from, to}, "rows")
		return nil, errx.InternalError()
	}
	rows.Close()

	mailboxQuery := `
		SELECT ea.id, ea.email, ea.track_direct_mail,
		       COUNT(ue.id) FILTER (WHERE ue.folder = 'sent' AND ` + directOutgoing + `) AS sent,
		       COUNT(ue.id) FILTER (WHERE ue.folder = 'inbox') AS received
		FROM email_accounts ea
		LEFT JOIN unibox_emails ue
		       ON ue.email_id = ea.id
		      AND ue.internal_date >= $2 AND ue.internal_date <= $3
		WHERE ea.organization_id = $1
		GROUP BY ea.id, ea.email, ea.track_direct_mail
		ORDER BY sent DESC, ea.email
	`
	mrows, err := r.DB.Query(ctx, mailboxQuery, orgID, from, to)
	if err != nil {
		db.CaptureError(err, mailboxQuery, []any{orgID, from, to}, "query")
		return nil, errx.InternalError()
	}
	defer mrows.Close()
	for mrows.Next() {
		var m models.DirectMailMailboxStats
		if err := mrows.Scan(&m.EmailAccountID, &m.Email, &m.TrackDirectMail, &m.Sent, &m.Received); err != nil {
			return nil, errx.InternalError()
		}
		out.Mailboxes = append(out.Mailboxes, m)
	}
	if err := mrows.Err(); err != nil {
		db.CaptureError(err, mailboxQuery, []any{orgID, from, to}, "rows")
		return nil, errx.InternalError()
	}
	mrows.Close()

	// ── Top correspondents ─────────────────────────────────────────────
	// The other party is the first recipient on what we sent and the sender on
	// what we received, lowercased so one person is one row. Our own mailboxes
	// are excluded: a copy to yourself is not a correspondent.
	contactQuery := `
		WITH msg AS (
			SELECT
				` + bareAddr("CASE WHEN ue.folder = 'sent' THEN ue.to_addr[1] ELSE ue.from_addr[1] END") + ` AS addr,
				ue.folder,
				ue.internal_date
			FROM unibox_emails ue
			JOIN email_accounts ea ON ea.id = ue.email_id
			WHERE ea.organization_id = $1
			  AND ue.folder IN ('sent', 'inbox')
			  AND (ue.folder <> 'sent' OR ` + directOutgoing + `)
			  AND ue.internal_date >= $2 AND ue.internal_date <= $3
		)
		SELECT addr,
		       COUNT(*) FILTER (WHERE folder = 'sent')  AS sent,
		       COUNT(*) FILTER (WHERE folder = 'inbox') AS received,
		       MAX(internal_date) AS last_at
		FROM msg
		WHERE addr IS NOT NULL AND addr <> ''
		  AND addr NOT IN (SELECT LOWER(email) FROM email_accounts WHERE organization_id = $1)
		GROUP BY addr
		ORDER BY sent DESC, received DESC
		LIMIT 10
	`
	crows, err := r.DB.Query(ctx, contactQuery, orgID, from, to)
	if err != nil {
		db.CaptureError(err, contactQuery, []any{orgID, from, to}, "query")
		return nil, errx.InternalError()
	}
	defer crows.Close()
	for crows.Next() {
		var c models.DirectMailContact
		if err := crows.Scan(&c.Email, &c.Sent, &c.Received, &c.LastAt); err != nil {
			return nil, errx.InternalError()
		}
		out.TopContacts = append(out.TopContacts, c)
	}
	if err := crows.Err(); err != nil {
		db.CaptureError(err, contactQuery, []any{orgID, from, to}, "rows")
		return nil, errx.InternalError()
	}

	return out, nil
}
