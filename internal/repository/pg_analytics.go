package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

type AnalyticsRepository interface {
	// Warmup analytics
	GetWarmupStats(ctx context.Context, userID uuid.UUID, emailAccountID *uuid.UUID, from, to time.Time) ([]models.WarmupDailyStats, *errx.Error)

	// Campaign analytics
	GetCampaignSummary(ctx context.Context, userID, campaignID uuid.UUID) (*models.CampaignSummary, *errx.Error)
	GetCampaignDailyStats(ctx context.Context, campaignID uuid.UUID, from, to time.Time) ([]models.CampaignDailyStats, *errx.Error)
	GetSequenceStats(ctx context.Context, campaignID uuid.UUID) ([]models.SequenceStats, *errx.Error)

	// Email account status
	GetAccountsWithErrors(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, *errx.Error)
	GetAccountDailyUsage(ctx context.Context, accountID uuid.UUID, date time.Time) (*models.AccountDailyUsage, *errx.Error)

	// Usage overview
	GetEmailAccountCounts(ctx context.Context, userID uuid.UUID) (*models.AccountsUsage, *errx.Error)
	GetCampaignCounts(ctx context.Context, userID uuid.UUID) (*models.CampaignsUsage, *errx.Error)
	GetContactCounts(ctx context.Context, userID uuid.UUID) (*models.ContactsUsage, *errx.Error)

	// Dashboard analytics
	GetDashboardOverallStats(ctx context.Context, userID uuid.UUID, from, to time.Time) (*models.DashboardOverallStats, *errx.Error)
	GetRecentActivity(ctx context.Context, userID uuid.UUID, limit int) ([]models.RecentActivityItem, *errx.Error)
	GetTopCampaigns(ctx context.Context, userID uuid.UUID, from, to time.Time, limit int, sortBy string) ([]models.TopCampaignStats, *errx.Error)
	GetDashboardDailyTrend(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]models.DashboardDailyStats, *errx.Error)
	GetAccountHealthSummary(ctx context.Context, userID uuid.UUID) (*models.AccountHealthSummary, *errx.Error)

	// Campaign hourly stats
	GetCampaignHourlyStats(ctx context.Context, campaignID uuid.UUID, date time.Time) ([]models.CampaignHourlyStats, *errx.Error)

	// Campaign comparison
	CompareCampaigns(ctx context.Context, userID uuid.UUID, campaignIDs []uuid.UUID, from, to time.Time) (*models.CampaignComparison, *errx.Error)
}

type analyticsRepository struct {
	DB *db.DB
}

func NewAnalyticsRepository(db *db.DB) AnalyticsRepository {
	return &analyticsRepository{DB: db}
}

func (r *analyticsRepository) GetWarmupStats(ctx context.Context, userID uuid.UUID, emailAccountID *uuid.UUID, from, to time.Time) ([]models.WarmupDailyStats, *errx.Error) {
	query := `
		SELECT
			ws.date::text,
			ws.emails_sent,
			ws.emails_replied,
			ws.target_volume
		FROM warmup_statistics ws
		JOIN email_accounts ea ON ea.id = ws.email_account_id
		WHERE ea.user_id = $1
		  AND ws.date >= $2
		  AND ws.date <= $3
		  AND ($4::uuid IS NULL OR ws.email_account_id = $4)
		ORDER BY ws.date ASC
	`

	params := []any{userID, from, to, emailAccountID}

	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	stats := make([]models.WarmupDailyStats, 0)
	for rows.Next() {
		var s models.WarmupDailyStats
		if err := rows.Scan(&s.Date, &s.EmailsSent, &s.EmailsReplied, &s.TargetVolume); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		stats = append(stats, s)
	}

	return stats, nil
}

func (r *analyticsRepository) GetCampaignSummary(ctx context.Context, userID, campaignID uuid.UUID) (*models.CampaignSummary, *errx.Error) {
	query := `
		SELECT
			COUNT(DISTINCT ccp.contact_id) as total_contacts,
			COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) as emails_sent,
			COUNT(CASE WHEN ccp.sent_at IS NULL THEN 1 END) as emails_pending,
			COUNT(CASE WHEN ccp.opened_at IS NOT NULL THEN 1 END) as unique_opens,
			COUNT(CASE WHEN ccp.clicked_at IS NOT NULL THEN 1 END) as unique_clicks,
			COUNT(CASE WHEN ccp.replied_at IS NOT NULL THEN 1 END) as replies,
			COUNT(CASE WHEN ccp.bounced_at IS NOT NULL THEN 1 END) as bounces
		FROM campaign_contact_progress ccp
		JOIN campaigns c ON c.id = ccp.campaign_id
		WHERE ccp.campaign_id = $1 AND c.user_id = $2
	`

	params := []any{campaignID, userID}

	var summary models.CampaignSummary
	err := r.DB.QueryRow(ctx, query, params...).Scan(
		&summary.TotalContacts,
		&summary.EmailsSent,
		&summary.EmailsPending,
		&summary.UniqueOpens,
		&summary.UniqueClicks,
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
			sent_at::date::text as date,
			COUNT(*) as sent,
			COUNT(CASE WHEN opened_at IS NOT NULL THEN 1 END) as opens,
			COUNT(CASE WHEN clicked_at IS NOT NULL THEN 1 END) as clicks,
			COUNT(CASE WHEN replied_at IS NOT NULL THEN 1 END) as replies
		FROM campaign_contact_progress
		WHERE campaign_id = $1
		  AND sent_at IS NOT NULL
		  AND sent_at::date >= $2
		  AND sent_at::date <= $3
		GROUP BY sent_at::date
		ORDER BY sent_at::date ASC
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

func (r *analyticsRepository) GetSequenceStats(ctx context.Context, campaignID uuid.UUID) ([]models.SequenceStats, *errx.Error) {
	query := `
		SELECT
			s.id,
			s.name,
			ROW_NUMBER() OVER (ORDER BY s.created_at) as position,
			COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) as emails_sent,
			COUNT(CASE WHEN ccp.opened_at IS NOT NULL THEN 1 END) as opens,
			COUNT(CASE WHEN ccp.clicked_at IS NOT NULL THEN 1 END) as clicks,
			COUNT(CASE WHEN ccp.replied_at IS NOT NULL THEN 1 END) as replies,
			COUNT(CASE WHEN ccp.bounced_at IS NOT NULL THEN 1 END) as bounces
		FROM sequences s
		LEFT JOIN campaign_contact_progress ccp ON ccp.sequence_id = s.id AND ccp.campaign_id = $1
		WHERE s.campaign_id = $1
		GROUP BY s.id, s.name, s.created_at
		ORDER BY s.created_at
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
		if err := rows.Scan(&s.SequenceID, &s.Name, &s.Position, &s.EmailsSent, &s.Opens, &s.Clicks, &s.Replies, &s.Bounces); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		stats = append(stats, s)
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

func (r *analyticsRepository) GetAccountDailyUsage(ctx context.Context, accountID uuid.UUID, date time.Time) (*models.AccountDailyUsage, *errx.Error) {
	query := `
		SELECT
			$2::date::text as date,
			COALESCE(dec.count, 0) as campaign_sent,
			COALESCE(ea.campaign_limit, 50) as campaign_limit,
			COALESCE(ws.emails_sent, 0) as warmup_sent,
			COALESCE(ea.warmup_max, 0) as warmup_limit
		FROM email_accounts ea
		LEFT JOIN daily_email_counts dec ON dec.email_account_id = ea.id AND dec.date = $2::date
		LEFT JOIN warmup_statistics ws ON ws.email_account_id = ea.id AND ws.date = $2::date
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
		db.CaptureError(err, query, params, "queryrow")
		return nil, errx.InternalError()
	}

	return &usage, nil
}

func (r *analyticsRepository) GetEmailAccountCounts(ctx context.Context, userID uuid.UUID) (*models.AccountsUsage, *errx.Error) {
	query := `
		SELECT
			COUNT(*) as total,
			COUNT(CASE WHEN status = 'active' THEN 1 END) as active,
			COUNT(CASE WHEN warmup IS NOT NULL THEN 1 END) as in_warmup,
			COUNT(DISTINCT eae.email_account_id) as with_errors
		FROM email_accounts ea
		LEFT JOIN email_account_errors eae ON eae.email_account_id = ea.id AND eae.resolved_at IS NULL
		WHERE ea.user_id = $1
	`

	var usage models.AccountsUsage
	err := r.DB.QueryRow(ctx, query, userID).Scan(&usage.Total, &usage.Active, &usage.InWarmup, &usage.WithErrors)
	if err != nil {
		db.CaptureError(err, query, []any{userID}, "queryrow")
		return nil, errx.InternalError()
	}

	return &usage, nil
}

func (r *analyticsRepository) GetCampaignCounts(ctx context.Context, userID uuid.UUID) (*models.CampaignsUsage, *errx.Error) {
	query := `
		SELECT
			COUNT(*) as total,
			COUNT(CASE WHEN status = 'active' THEN 1 END) as active,
			COUNT(CASE WHEN status = 'paused' THEN 1 END) as paused,
			COUNT(CASE WHEN status = 'draft' THEN 1 END) as draft,
			(SELECT COUNT(*) FROM campaign_contact_progress ccp
			 JOIN campaigns c ON c.id = ccp.campaign_id
			 WHERE c.user_id = $1 AND ccp.sent_at IS NOT NULL) as emails_sent
		FROM campaigns
		WHERE user_id = $1
	`

	var usage models.CampaignsUsage
	err := r.DB.QueryRow(ctx, query, userID).Scan(&usage.Total, &usage.Active, &usage.Paused, &usage.Draft, &usage.EmailsSent)
	if err != nil {
		db.CaptureError(err, query, []any{userID}, "queryrow")
		return nil, errx.InternalError()
	}

	return &usage, nil
}

func (r *analyticsRepository) GetContactCounts(ctx context.Context, userID uuid.UUID) (*models.ContactsUsage, *errx.Error) {
	query := `
		SELECT
			COUNT(*) as total,
			COUNT(CASE WHEN subscribed = true THEN 1 END) as subscribed,
			COUNT(CASE WHEN created_at::date = CURRENT_DATE THEN 1 END) as added_today
		FROM contacts
		WHERE user_id = $1
	`

	var usage models.ContactsUsage
	err := r.DB.QueryRow(ctx, query, userID).Scan(&usage.Total, &usage.Subscribed, &usage.AddedToday)
	if err != nil {
		db.CaptureError(err, query, []any{userID}, "queryrow")
		return nil, errx.InternalError()
	}

	return &usage, nil
}

// Dashboard Analytics Methods

func (r *analyticsRepository) GetDashboardOverallStats(ctx context.Context, userID uuid.UUID, from, to time.Time) (*models.DashboardOverallStats, *errx.Error) {
	query := `
		SELECT
			COUNT(CASE WHEN ccp.sent_at IS NOT NULL AND ccp.sent_at >= $2 AND ccp.sent_at <= $3 THEN 1 END) as total_sent,
			COUNT(CASE WHEN ccp.opened_at IS NOT NULL AND ccp.sent_at >= $2 AND ccp.sent_at <= $3 THEN 1 END) as total_opens,
			COUNT(CASE WHEN ccp.clicked_at IS NOT NULL AND ccp.sent_at >= $2 AND ccp.sent_at <= $3 THEN 1 END) as total_clicks,
			COUNT(CASE WHEN ccp.replied_at IS NOT NULL AND ccp.sent_at >= $2 AND ccp.sent_at <= $3 THEN 1 END) as total_replies,
			COUNT(CASE WHEN ccp.bounced_at IS NOT NULL AND ccp.sent_at >= $2 AND ccp.sent_at <= $3 THEN 1 END) as total_bounces,
			(SELECT COUNT(*) FROM campaigns WHERE user_id = $1 AND status = 'active') as active_campaigns,
			(SELECT COUNT(*) FROM email_accounts WHERE user_id = $1 AND status = 'active') as active_accounts
		FROM campaign_contact_progress ccp
		JOIN campaigns c ON c.id = ccp.campaign_id
		WHERE c.user_id = $1
	`

	params := []any{userID, from, to}

	var stats models.DashboardOverallStats
	err := r.DB.QueryRow(ctx, query, params...).Scan(
		&stats.TotalEmailsSent,
		&stats.TotalOpens,
		&stats.TotalClicks,
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

func (r *analyticsRepository) GetRecentActivity(ctx context.Context, userID uuid.UUID, limit int) ([]models.RecentActivityItem, *errx.Error) {
	// Union query to get recent opens, clicks, replies, and bounces
	query := `
		WITH recent_events AS (
			-- Opens
			SELECT 'opened' as type, ccp.campaign_id, c.name as campaign_name,
				   co.email as contact_email, ccp.contact_id, ccp.opened_at as timestamp, NULL as link
			FROM campaign_contact_progress ccp
			JOIN campaigns c ON c.id = ccp.campaign_id
			JOIN contacts co ON co.id = ccp.contact_id
			WHERE c.user_id = $1 AND ccp.opened_at IS NOT NULL

			UNION ALL

			-- Clicks
			SELECT 'clicked' as type, ccp.campaign_id, c.name as campaign_name,
				   co.email as contact_email, ccp.contact_id, ccp.clicked_at as timestamp, NULL as link
			FROM campaign_contact_progress ccp
			JOIN campaigns c ON c.id = ccp.campaign_id
			JOIN contacts co ON co.id = ccp.contact_id
			WHERE c.user_id = $1 AND ccp.clicked_at IS NOT NULL

			UNION ALL

			-- Replies
			SELECT 'replied' as type, ccp.campaign_id, c.name as campaign_name,
				   co.email as contact_email, ccp.contact_id, ccp.replied_at as timestamp, NULL as link
			FROM campaign_contact_progress ccp
			JOIN campaigns c ON c.id = ccp.campaign_id
			JOIN contacts co ON co.id = ccp.contact_id
			WHERE c.user_id = $1 AND ccp.replied_at IS NOT NULL

			UNION ALL

			-- Bounces
			SELECT 'bounced' as type, ccp.campaign_id, c.name as campaign_name,
				   co.email as contact_email, ccp.contact_id, ccp.bounced_at as timestamp, NULL as link
			FROM campaign_contact_progress ccp
			JOIN campaigns c ON c.id = ccp.campaign_id
			JOIN contacts co ON co.id = ccp.contact_id
			WHERE c.user_id = $1 AND ccp.bounced_at IS NOT NULL
		)
		SELECT type, campaign_id, campaign_name, contact_email, contact_id, timestamp, COALESCE(link, '') as link
		FROM recent_events
		ORDER BY timestamp DESC
		LIMIT $2
	`

	params := []any{userID, limit}

	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()

	activities := make([]models.RecentActivityItem, 0)
	for rows.Next() {
		var a models.RecentActivityItem
		if err := rows.Scan(&a.Type, &a.CampaignID, &a.CampaignName, &a.ContactEmail, &a.ContactID, &a.Timestamp, &a.Link); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		activities = append(activities, a)
	}

	return activities, nil
}

func (r *analyticsRepository) GetTopCampaigns(ctx context.Context, userID uuid.UUID, from, to time.Time, limit int, sortBy string) ([]models.TopCampaignStats, *errx.Error) {
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
				THEN COUNT(CASE WHEN ccp.opened_at IS NOT NULL THEN 1 END)::float / COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) * 100
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
		WHERE c.user_id = $1
		GROUP BY c.id, c.name, c.status
		HAVING COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) > 0
		ORDER BY ` + orderClause + `
		LIMIT $4
	`

	params := []any{userID, from, to, limit}

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

func (r *analyticsRepository) GetDashboardDailyTrend(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]models.DashboardDailyStats, *errx.Error) {
	query := `
		SELECT
			sent_at::date::text as date,
			COUNT(*) as sent,
			COUNT(CASE WHEN opened_at IS NOT NULL THEN 1 END) as opens,
			COUNT(CASE WHEN clicked_at IS NOT NULL THEN 1 END) as clicks,
			COUNT(CASE WHEN replied_at IS NOT NULL THEN 1 END) as replies
		FROM campaign_contact_progress ccp
		JOIN campaigns c ON c.id = ccp.campaign_id
		WHERE c.user_id = $1
		  AND ccp.sent_at IS NOT NULL
		  AND ccp.sent_at::date >= $2
		  AND ccp.sent_at::date <= $3
		GROUP BY sent_at::date
		ORDER BY sent_at::date ASC
	`

	params := []any{userID, from, to}

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

func (r *analyticsRepository) GetAccountHealthSummary(ctx context.Context, userID uuid.UUID) (*models.AccountHealthSummary, *errx.Error) {
	query := `
		SELECT
			COUNT(*) as total,
			COUNT(CASE WHEN ea.status = 'active' AND NOT EXISTS (
				SELECT 1 FROM email_account_errors eae
				WHERE eae.email_account_id = ea.id AND eae.resolved_at IS NULL
			) THEN 1 END) as healthy,
			COUNT(CASE WHEN EXISTS (
				SELECT 1 FROM email_account_errors eae
				WHERE eae.email_account_id = ea.id AND eae.resolved_at IS NULL AND eae.severity = 'WARNING'
			) THEN 1 END) as warning,
			COUNT(CASE WHEN ea.status != 'active' OR EXISTS (
				SELECT 1 FROM email_account_errors eae
				WHERE eae.email_account_id = ea.id AND eae.resolved_at IS NULL AND eae.severity = 'CRITICAL'
			) THEN 1 END) as error
		FROM email_accounts ea
		WHERE ea.user_id = $1
	`

	var summary models.AccountHealthSummary
	err := r.DB.QueryRow(ctx, query, userID).Scan(&summary.TotalAccounts, &summary.HealthyAccounts, &summary.WarningAccounts, &summary.ErrorAccounts)
	if err != nil {
		db.CaptureError(err, query, []any{userID}, "queryrow")
		return nil, errx.InternalError()
	}

	return &summary, nil
}

func (r *analyticsRepository) GetCampaignHourlyStats(ctx context.Context, campaignID uuid.UUID, date time.Time) ([]models.CampaignHourlyStats, *errx.Error) {
	query := `
		SELECT
			EXTRACT(HOUR FROM sent_at)::int as hour,
			COUNT(*) as sent,
			COUNT(CASE WHEN opened_at IS NOT NULL THEN 1 END) as opens,
			COUNT(CASE WHEN clicked_at IS NOT NULL THEN 1 END) as clicks,
			COUNT(CASE WHEN replied_at IS NOT NULL THEN 1 END) as replies
		FROM campaign_contact_progress
		WHERE campaign_id = $1
		  AND sent_at IS NOT NULL
		  AND sent_at::date = $2::date
		GROUP BY EXTRACT(HOUR FROM sent_at)
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

func (r *analyticsRepository) CompareCampaigns(ctx context.Context, userID uuid.UUID, campaignIDs []uuid.UUID, from, to time.Time) (*models.CampaignComparison, *errx.Error) {
	query := `
		SELECT
			c.id as campaign_id,
			c.name,
			c.status,
			COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) as emails_sent,
			CASE WHEN COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) > 0
				THEN COUNT(CASE WHEN ccp.opened_at IS NOT NULL THEN 1 END)::float / COUNT(CASE WHEN ccp.sent_at IS NOT NULL THEN 1 END) * 100
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
		WHERE c.user_id = $1 AND c.id = ANY($4)
		GROUP BY c.id, c.name, c.status
		ORDER BY c.name
	`

	params := []any{userID, from, to, campaignIDs}

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
