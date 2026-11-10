package repository

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

// GetReportCampaignTable counts each campaign's sends in the window exactly as GetDashboardOverallStats does, so the rows add up to it.
func (r *analyticsRepository) GetReportCampaignTable(ctx context.Context, orgID uuid.UUID, from, to time.Time, scope *models.CampaignScope, limit int) ([]models.ClientReportCampaign, *errx.Error) {
	query := `
		SELECT c.id, c.name, c.status,
			COUNT(*) AS sent,
			COUNT(CASE WHEN ccp.opened_at IS NOT NULL AND NOT ccp.opened_machine THEN 1 END) AS opens,
			COUNT(CASE WHEN ccp.clicked_at IS NOT NULL THEN 1 END) AS clicks,
			COUNT(CASE WHEN ccp.replied_at IS NOT NULL THEN 1 END) AS replies,
			` + positiveReplyCount + `,
			COUNT(CASE WHEN ccp.bounced_at IS NOT NULL THEN 1 END) AS bounces
		FROM campaign_contact_progress ccp
		JOIN campaigns c ON c.id = ccp.campaign_id
		JOIN sequences s ON s.id = ccp.sequence_id AND s.kind = 'email'
		WHERE c.organization_id = $1` + inCampaignScope("c.id", scope, 5) + `
		  AND ccp.sent_at >= $2 AND ccp.sent_at <= $3
		GROUP BY c.id, c.name, c.status
		ORDER BY sent DESC, c.name
		LIMIT $4`
	params := append([]any{orgID, from, to, limit}, scopeArgs(scope)...)
	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()
	out := []models.ClientReportCampaign{}
	for rows.Next() {
		var c models.ClientReportCampaign
		var positive int
		if err := rows.Scan(&c.ID, &c.Name, &c.Status, &c.Sent, &c.Opens, &c.Clicks, &c.Replies, &positive, &c.Bounces); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		c.Positive = &positive
		c.OpenRate, c.ReplyRate, c.BounceRate = models.Rate(c.Opens, c.Sent), models.Rate(c.Replies, c.Sent), models.Rate(c.Bounces, c.Sent)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, query, params, "rows")
		return nil, errx.InternalError()
	}
	return out, nil
}

// GetReportReplies lists replies by when they arrived; the subject and snippet come from the sender's synced copy, when there is one.
func (r *analyticsRepository) GetReportReplies(ctx context.Context, orgID uuid.UUID, from, to time.Time, scope *models.CampaignScope, positiveOnly, withSnippet bool, limit int) ([]models.ClientReportReply, *errx.Error) {
	filter := ""
	if positiveOnly {
		filter = " AND " + positiveReply
	}
	query := `
		SELECT ccp.replied_at, COALESCE(NULLIF(ccp.reply_class, ''), 'unclassified'), c.name,
			co.email, co.first_name, co.last_name,
			COALESCE(rm.subject, ''), CASE WHEN $5::boolean THEN COALESCE(rm.snippet, '') ELSE '' END
		FROM campaign_contact_progress ccp
		JOIN campaigns c ON c.id = ccp.campaign_id
		JOIN contacts co ON co.id = ccp.contact_id AND co.organization_id = $1
		LEFT JOIN tasks t ON t.id = ccp.dispatch_task_id
		LEFT JOIN LATERAL (
			SELECT u.subject, u.snippet FROM unibox_emails u
			JOIN email_accounts ea ON ea.id = u.email_id AND ea.organization_id = $1
			WHERE u.email_id = t.email_account_id
			  AND u.internal_date >= COALESCE(ccp.sent_at, ccp.replied_at - interval '30 days')
			  AND u.internal_date <= ccp.replied_at + interval '1 hour'
			  AND u.folder NOT IN ('sent', 'drafts') AND u.provider_folder NOT IN ('sent', 'drafts')
			  AND ` + bareFrom + ` = lower(co.email)
			ORDER BY u.internal_date DESC LIMIT 1
		) rm ON true
		WHERE c.organization_id = $1` + inCampaignScope("c.id", scope, 6) + `
		  AND ccp.replied_at >= $2 AND ccp.replied_at <= $3` + filter + `
		ORDER BY ccp.replied_at DESC
		LIMIT $4`
	params := append([]any{orgID, from, to, limit, withSnippet}, scopeArgs(scope)...)
	rows, err := r.DB.Query(ctx, query, params...)
	if err != nil {
		db.CaptureError(err, query, params, "query")
		return nil, errx.InternalError()
	}
	defer rows.Close()
	out := []models.ClientReportReply{}
	for rows.Next() {
		var rep models.ClientReportReply
		var first, last string
		if err := rows.Scan(&rep.RepliedAt, &rep.Classification, &rep.Campaign, &rep.Email, &first, &last, &rep.Subject, &rep.Snippet); err != nil {
			db.CaptureError(err, "", nil, "scan")
			return nil, errx.InternalError()
		}
		rep.Name = reportContactName(first, last)
		out = append(out, rep)
	}
	if err := rows.Err(); err != nil {
		db.CaptureError(err, query, params, "rows")
		return nil, errx.InternalError()
	}
	return out, nil
}

func reportContactName(first, last string) string {
	switch {
	case first == "":
		return last
	case last == "":
		return first
	}
	return first + " " + last
}
