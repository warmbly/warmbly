package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
)

// AgencyHomeRepository reads the agency home and the cross-client inbox. Every
// metric is one query over a list of organizations the service has already
// authorized, so the cost does not grow with a query per client.
type AgencyHomeRepository interface {
	// ClientMemberships lists the agency's client workspaces with the user's membership in each.
	ClientMemberships(ctx context.Context, agencyID, userID uuid.UUID) ([]models.AgencyClientMembership, error)
	CampaignStats(ctx context.Context, orgIDs []uuid.UUID, since time.Time) (map[uuid.UUID]*models.AgencyClientCampaigns, error)
	MailboxStats(ctx context.Context, orgIDs []uuid.UUID) (map[uuid.UUID]*models.AgencyClientMailboxes, error)
	RepliesWaiting(ctx context.Context, orgIDs []uuid.UUID) (map[uuid.UUID]int64, error)
	AdvisorCounts(ctx context.Context, orgIDs []uuid.UUID) (map[uuid.UUID]*models.AgencyClientAdvisor, error)
	// InboxThreads returns up to q.Limit+1 threads, newest first.
	InboxThreads(ctx context.Context, q models.AgencyInboxQuery) ([]models.AgencyInboxThread, error)
}

type agencyHomeRepository struct {
	db *pgxpool.Pool
}

func NewAgencyHomeRepository(db *pgxpool.Pool) AgencyHomeRepository {
	return &agencyHomeRepository{db: db}
}

const agencyClientMembershipsSQL = `
	SELECT ac.organization_id, o.name, o.avatar_url, ac.monthly_send_limit,
	       om.role, COALESCE(om.permissions, 0), COALESCE(om.access_scope, 'workspace')
	FROM agency_clients ac
	JOIN organizations o ON o.id = ac.organization_id
	LEFT JOIN organization_members om ON om.organization_id = ac.organization_id AND om.user_id = $2
	WHERE ac.agency_id = $1
	ORDER BY lower(o.name), o.id`

func (r *agencyHomeRepository) ClientMemberships(ctx context.Context, agencyID, userID uuid.UUID) ([]models.AgencyClientMembership, error) {
	rows, err := r.db.Query(ctx, agencyClientMembershipsSQL, agencyID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.AgencyClientMembership
	for rows.Next() {
		var m models.AgencyClientMembership
		if err := rows.Scan(&m.OrganizationID, &m.Name, &m.AvatarURL, &m.MonthlySendLimit, &m.Role, &m.Permissions, &m.AccessScope); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// agencyCampaignStatsSQL counts sends and outcomes per workspace; a positive reply belongs to the day it arrived.
const agencyCampaignStatsSQL = `
	WITH sends AS (
		SELECT c.organization_id,
		       COUNT(*) FILTER (WHERE ccp.sent_at >= CURRENT_DATE) AS sent_today,
		       COUNT(*) FILTER (WHERE ccp.sent_at >= $2) AS sent_7d,
		       COUNT(*) FILTER (WHERE ccp.sent_at >= $2 AND ccp.bounced_at IS NOT NULL) AS bounced_7d,
		       COUNT(*) FILTER (WHERE ccp.replied_at >= $2 AND ccp.reply_class = 'positive') AS positive_7d
		FROM campaigns c
		JOIN campaign_contact_progress ccp ON ccp.campaign_id = c.id
		JOIN sequences s ON s.id = ccp.sequence_id AND s.kind = 'email'
		WHERE c.organization_id = ANY($1::uuid[])
		  AND (ccp.sent_at >= $2 OR ccp.replied_at >= $2)
		GROUP BY c.organization_id
	), statuses AS (
		SELECT organization_id,
		       COUNT(*) FILTER (WHERE status::text = 'active') AS active,
		       COUNT(*) FILTER (WHERE status::text = ANY($3::text[])) AS paused_limits,
		       COUNT(*) FILTER (WHERE status::text = ANY($4::text[])) AS paused_billing
		FROM campaigns
		WHERE organization_id = ANY($1::uuid[])
		GROUP BY organization_id
	)
	SELECT o.id,
	       COALESCE(st.active, 0), COALESCE(st.paused_limits, 0), COALESCE(st.paused_billing, 0),
	       COALESCE(se.sent_today, 0), COALESCE(se.sent_7d, 0), COALESCE(se.bounced_7d, 0), COALESCE(se.positive_7d, 0)
	FROM unnest($1::uuid[]) AS o(id)
	LEFT JOIN sends se ON se.organization_id = o.id
	LEFT JOIN statuses st ON st.organization_id = o.id`

func (r *agencyHomeRepository) CampaignStats(ctx context.Context, orgIDs []uuid.UUID, since time.Time) (map[uuid.UUID]*models.AgencyClientCampaigns, error) {
	out := map[uuid.UUID]*models.AgencyClientCampaigns{}
	if len(orgIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, agencyCampaignStatsSQL, orgIDs, since,
		models.CampaignStatusesPausedByLimits, models.CampaignStatusesPausedByBilling)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var s models.AgencyClientCampaigns
		if err := rows.Scan(&id, &s.Active, &s.PausedByLimits, &s.PausedByBilling, &s.SentToday, &s.Sent7d, &s.Bounced7d, &s.PositiveReplies7d); err != nil {
			return nil, err
		}
		if s.Sent7d > 0 {
			s.BounceRate7d = float64(s.Bounced7d) / float64(s.Sent7d)
		}
		out[id] = &s
	}
	return out, rows.Err()
}

// agencyMailboxStatsSQL reads each mailbox's warmup standing the way the mailbox list does; a hold past its term no longer counts.
var agencyMailboxStatsSQL = `
	SELECT o.id,
	       COUNT(ea.id),
	       COUNT(ea.id) FILTER (WHERE ea.status <> 'active'),
	       COUNT(ea.id) FILTER (WHERE ea.status = 'active' AND EXISTS (
	           SELECT 1 FROM email_account_errors eae
	           WHERE eae.email_account_id = ea.id AND eae.resolved_at IS NULL AND eae.severity = 'CRITICAL')),
	       COUNT(ea.id) FILTER (WHERE wh.health_state = 'watch'),
	       COUNT(ea.id) FILTER (WHERE wh.health_state = 'throttled'),
	       COUNT(ea.id) FILTER (WHERE wh.health_state = 'quarantined' AND (wh.blocked_until IS NULL OR wh.blocked_until > NOW())),
	       COUNT(ea.id) FILTER (WHERE wh.health_state = 'blocked' AND (wh.blocked_until IS NULL OR wh.blocked_until > NOW()))
	FROM unnest($1::uuid[]) AS o(id)
	LEFT JOIN email_accounts ea ON ea.organization_id = o.id
	LEFT JOIN LATERAL (` + warmupKnownStandingSQL("ea.id") + `
	) wh ON true
	GROUP BY o.id`

func (r *agencyHomeRepository) MailboxStats(ctx context.Context, orgIDs []uuid.UUID) (map[uuid.UUID]*models.AgencyClientMailboxes, error) {
	out := map[uuid.UUID]*models.AgencyClientMailboxes{}
	if len(orgIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, agencyMailboxStatsSQL, orgIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var s models.AgencyClientMailboxes
		if err := rows.Scan(&id, &s.Total, &s.Disconnected, &s.Erroring, &s.Watch, &s.Throttled, &s.Quarantined, &s.Blocked); err != nil {
			return nil, err
		}
		out[id] = &s
	}
	return out, rows.Err()
}

// agencyRepliesWaitingSQL counts unread conversations exactly as each workspace's Inbox badge does.
var agencyRepliesWaitingSQL = `
	SELECT ea.organization_id, COUNT(DISTINCT COALESCE(NULLIF(ue.thread_id, ''), ue.id::text))
	FROM unibox_emails ue
	JOIN email_accounts ea ON ea.id = ue.email_id
	WHERE ea.organization_id = ANY($1::uuid[])
	  AND ue.seen = FALSE
	  AND ue.folder = '` + models.FolderInbox + `'
	  AND NOT EXISTS (
		SELECT 1 FROM unibox_snoozes s
		WHERE s.organization_id = ea.organization_id AND s.user_id = ue.user_id
		  AND s.thread_id = ue.thread_id
		  AND s.snoozed_until > NOW()
	  )
	  AND NOT ` + automatedThreadSQL("ue.thread_id", "ue.automated", "ea.organization_id") + `
	GROUP BY ea.organization_id`

func (r *agencyHomeRepository) RepliesWaiting(ctx context.Context, orgIDs []uuid.UUID) (map[uuid.UUID]int64, error) {
	out := map[uuid.UUID]int64{}
	if len(orgIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, agencyRepliesWaitingSQL, orgIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var n int64
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

const agencyAdvisorCountsSQL = `
	SELECT organization_id, severity, COUNT(*)
	FROM advisor_findings
	WHERE organization_id = ANY($1::uuid[]) AND status = 'open'
	GROUP BY organization_id, severity`

func (r *agencyHomeRepository) AdvisorCounts(ctx context.Context, orgIDs []uuid.UUID) (map[uuid.UUID]*models.AgencyClientAdvisor, error) {
	out := map[uuid.UUID]*models.AgencyClientAdvisor{}
	if len(orgIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx, agencyAdvisorCountsSQL, orgIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var severity string
		var n int
		if err := rows.Scan(&id, &severity, &n); err != nil {
			return nil, err
		}
		c := out[id]
		if c == nil {
			c = &models.AgencyClientAdvisor{}
			out[id] = c
		}
		switch models.AdvisorSeverity(severity) {
		case models.AdvisorCritical:
			c.Critical += n
		case models.AdvisorHigh:
			c.High += n
		case models.AdvisorMedium:
			c.Medium += n
		case models.AdvisorLow:
			c.Low += n
		}
	}
	return out, rows.Err()
}

// agencyInboxQuery builds the cross-client thread list. $1 are workspaces read
// whole, $2 restricted workspaces and $3 the mailboxes granted in them, so the
// tenant predicate comes only from memberships the service resolved.
func agencyInboxQuery(q models.AgencyInboxQuery) (string, []any) {
	args := []any{nonNilUUIDs(q.OrgIDs), nonNilUUIDs(q.ScopedOrgIDs), nonNilUUIDs(q.ScopedMailboxes)}
	next := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	matched := `
		SELECT ue.id, ue.email_id, sc.organization_id, ue.user_id, ue.thread_id, ue.from_addr, ue.to_addr,
		       ue.subject, ue.snippet, ue.internal_date, ue.seen, ue.automated, ue.folder
		FROM unibox_emails ue
		JOIN scope sc ON sc.id = ue.email_id
		WHERE ue.folder NOT IN ` + foldersOutsideWorkingViews + `
		  AND NOT EXISTS (
			SELECT 1 FROM unibox_snoozes s
			WHERE s.organization_id = sc.organization_id AND s.user_id = ue.user_id
			  AND s.thread_id = ue.thread_id
			  AND s.snoozed_until > NOW()
		  )`
	if q.Unread {
		matched += ` AND ue.seen = FALSE`
	}
	if search := strings.TrimSpace(q.Search); search != "" {
		tsQuery := fmt.Sprintf(`websearch_to_tsquery('english', %s)`, next(search))
		if prefix := prefixTSQuery(search); prefix != "" {
			tsQuery += fmt.Sprintf(` || to_tsquery('english', %s)`, next(prefix))
		}
		match := fmt.Sprintf(`ue.search_tsv @@ (%s)`, tsQuery)
		if participantSearchable(search) {
			match += fmt.Sprintf(` OR public.unibox_participants(ue.from_addr, ue.to_addr, ue.cc) ILIKE '%%' || %s || '%%' ESCAPE '\'`, next(escapeLikePattern(search)))
		}
		matched += ` AND (` + match + `)`
	}

	page := `
		SELECT b.* FROM matched b
		WHERE (b.thread_id = '' OR NOT EXISTS (
			SELECT 1 FROM matched newer
			WHERE newer.organization_id = b.organization_id AND newer.thread_id = b.thread_id
			  AND (newer.internal_date, newer.id) > (b.internal_date, b.id)
		))
		  AND NOT ` + automatedThreadSQL("b.thread_id", "b.automated", "b.organization_id")
	if q.NeedsReply {
		// The newest message came in from someone else, so the conversation waits on the client.
		page += `
		  AND b.folder = '` + models.FolderInbox + `'
		  AND NOT ` + ownAddressSQL("b.from_addr", "b.organization_id")
	}
	if q.Positive {
		page += `
		  AND EXISTS (
			SELECT 1 FROM unibox_emails cm
			CROSS JOIN LATERAL (SELECT ` + messageKeysSQL("cm") + ` AS keys) k
			JOIN tasks t ON t.task_type = 'campaign'
			 AND ((t.message_id <> '' AND t.message_id = ANY(k.keys))
			   OR (cm.thread_id <> '' AND t.thread_id <> '' AND t.email_account_id = cm.email_id AND t.thread_id = cm.thread_id))
			JOIN campaign_tasks ct ON ct.task_id = t.id
			JOIN campaigns cc ON cc.id = ct.campaign_id AND cc.organization_id = b.organization_id
			JOIN campaign_contact_progress p ON p.campaign_id = ct.campaign_id AND p.contact_id = ct.contact_id
			WHERE cm.email_id IN (SELECT id FROM scope WHERE organization_id = b.organization_id)
			  AND ((b.thread_id <> '' AND cm.thread_id = b.thread_id) OR cm.id = b.id)
			  AND p.replied_at IS NOT NULL AND p.reply_class = 'positive'
		  )`
	}
	if q.BeforeID != uuid.Nil {
		page += fmt.Sprintf(`
		  AND (b.internal_date, b.id) < (%s::timestamptz, %s::uuid)`, next(q.BeforeAt), next(q.BeforeID))
	}
	page += fmt.Sprintf(`
		ORDER BY b.internal_date DESC, b.id DESC
		LIMIT %s`, next(q.Limit+1))

	query := `
	WITH scope AS MATERIALIZED (
		SELECT ea.id, ea.organization_id FROM email_accounts ea
		WHERE ea.organization_id = ANY($1::uuid[])
		   OR (ea.organization_id = ANY($2::uuid[]) AND ea.id = ANY($3::uuid[]))
	), matched AS NOT MATERIALIZED (` + matched + `
	), page AS MATERIALIZED (` + page + `
	)
	SELECT b.organization_id, b.id, b.email_id, b.thread_id, b.from_addr, b.to_addr, b.subject, b.snippet,
	       b.internal_date, b.seen, stats.message_count, stats.has_unread
	FROM page b
	CROSS JOIN LATERAL (
		SELECT COUNT(*) AS message_count, COALESCE(bool_or(NOT m.seen), false) AS has_unread
		FROM matched m
		WHERE m.organization_id = b.organization_id
		  AND ((b.thread_id <> '' AND m.thread_id = b.thread_id) OR (b.thread_id = '' AND m.id = b.id))
	) stats
	ORDER BY b.internal_date DESC, b.id DESC`
	return query, args
}

func (r *agencyHomeRepository) InboxThreads(ctx context.Context, q models.AgencyInboxQuery) ([]models.AgencyInboxThread, error) {
	if len(q.OrgIDs) == 0 && (len(q.ScopedOrgIDs) == 0 || len(q.ScopedMailboxes) == 0) {
		return nil, nil
	}
	query, args := agencyInboxQuery(q)
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.AgencyInboxThread, error) {
		var t models.AgencyInboxThread
		err := row.Scan(&t.OrganizationID, &t.ID, &t.EmailID, &t.ThreadID, &t.FromAddr, &t.ToAddr, &t.Subject, &t.Snippet,
			&t.InternalDate, &t.Seen, &t.MessageCount, &t.HasUnread)
		if t.FromAddr == nil {
			t.FromAddr = []string{}
		}
		if t.ToAddr == nil {
			t.ToAddr = []string{}
		}
		return t, err
	})
}
