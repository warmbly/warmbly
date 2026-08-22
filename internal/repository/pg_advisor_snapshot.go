package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// LoadSnapshot reads one org's advisor view. Every section is best-effort: a
// single failing rollup must not cost the org its whole evaluation, because a
// partial snapshot still produces correct findings for the parts that loaded
// (detectors are written to no-op on absent data rather than guess).
func (r *advisorRepository) LoadSnapshot(ctx context.Context, orgID uuid.UUID, now time.Time) (*AdvisorSnapshot, error) {
	snap := &AdvisorSnapshot{
		OrganizationID: orgID,
		Now:            now,
		Lists:          map[uuid.UUID]AdvisorListStats{},
	}

	mailboxes, err := r.loadMailboxes(ctx, orgID)
	if err != nil {
		// Mailboxes are the one section worth failing on: almost every detector
		// is scoped to a mailbox, and an empty list would look like "org has no
		// mailboxes" and resolve every open finding.
		return nil, err
	}
	snap.Mailboxes = mailboxes

	snap.Campaigns, _ = r.loadCampaigns(ctx, orgID)
	snap.Steps, _ = r.loadSteps(ctx, orgID)
	snap.Lists, _ = r.loadListStats(ctx, orgID)
	snap.Org, _ = r.loadOrgStats(ctx, orgID)

	return snap, nil
}

func (r *advisorRepository) loadMailboxes(ctx context.Context, orgID uuid.UUID) ([]AdvisorMailbox, error) {
	// Bounce/complaint attribution runs through tasks: deliverability_events
	// carries task_id, not email_account_id, so a mailbox's complaints are the
	// events whose task was sent by it.
	query := `
		SELECT
			ea.id, ea.email, ea.name, ea.status::text, ea.provider::text,
			-- email_accounts.created_at is a naive UTC timestamp, so it has to be
			-- anchored before subtracting from NOW() (timestamptz).
			GREATEST(0, (EXTRACT(EPOCH FROM (NOW() - (ea.created_at AT TIME ZONE 'UTC'))) / 86400)::int) AS age_days,
			ea.campaign_limit, ea.min_wait_time,
			ea.tracking_domain, ea.tracking_domain_verified,
			ea.auth_state, ea.auth_spf, ea.auth_dkim, ea.auth_dmarc, ea.auth_dmarc_policy, ea.auth_failing_since,
			(ea.warmup IS NOT NULL AND ea.warmup_paused_at IS NULL) AS warmup_active,
			(ea.warmup IS NOT NULL AND ea.warmup_paused_at IS NOT NULL) AS warmup_paused,
			ea.warmup_base, ea.warmup_max, ea.warmup_increase, ea.warmup_reply_rate,
			COALESCE(ea.warmup_pool_type, 'free'),
			ea.risk_band::text,
			COALESCE(sent.d7, 0), COALESCE(sent.d1, 0), COALESCE(sent.d30, 0),
			COALESCE(dl.bounces, 0), COALESCE(dl.complaints, 0),
			COALESCE(w.sent7, 0), COALESCE(w.recv7, 0), COALESCE(ws.spam7, 0),
			COALESCE(p.health_state, ''), COALESCE(p.spam_score, 0),
			COALESCE(p.blocked_until > NOW(), false) AS pool_blocked,
			COALESCE(err.n, 0),
			COALESCE(camp.active, false)
		FROM email_accounts ea
		LEFT JOIN LATERAL (
			SELECT
				COUNT(*) FILTER (WHERE t.completed_at > NOW() - INTERVAL '7 days')  AS d7,
				COUNT(*) FILTER (WHERE t.completed_at > NOW() - INTERVAL '1 day')   AS d1,
				COUNT(*) FILTER (WHERE t.completed_at > NOW() - INTERVAL '30 days') AS d30
			FROM tasks t
			WHERE t.email_account_id = ea.id
			  AND t.task_type = 'campaign' AND t.status = 'completed'
			  AND t.completed_at > NOW() - INTERVAL '30 days'
		) sent ON true
		LEFT JOIN LATERAL (
			SELECT
				COUNT(*) FILTER (WHERE de.event_type = 'bounce')    AS bounces,
				COUNT(*) FILTER (WHERE de.event_type = 'complaint') AS complaints
			FROM deliverability_events de
			JOIN tasks t2 ON t2.id = de.task_id
			WHERE t2.email_account_id = ea.id
			  AND de.created_at > NOW() - INTERVAL '30 days'
		) dl ON true
		LEFT JOIN LATERAL (
			SELECT
				COALESCE(SUM(emails_sent), 0)    AS sent7,
				COALESCE(SUM(emails_replied), 0) AS recv7
			FROM warmup_statistics wst
			WHERE wst.email_account_id = ea.id AND wst.date > CURRENT_DATE - 7
		) w ON true
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS spam7
			FROM warmup_spam_reports sr
			WHERE sr.reported_account_id = ea.id
			  AND sr.report_type = 'spam_placement'
			  AND sr.created_at > NOW() - INTERVAL '7 days'
		) ws ON true
		LEFT JOIN LATERAL (
			SELECT wpp.health_state, wpp.spam_score, wpp.blocked_until
			FROM warmup_pool_participants wpp
			WHERE wpp.email_account_id = ea.id
			ORDER BY wpp.joined_at DESC
			LIMIT 1
		) p ON true
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS n
			FROM email_account_errors eae
			WHERE eae.email_account_id = ea.id
			  AND eae.resolved_at IS NULL
			  AND eae.created_at > NOW() - INTERVAL '7 days'
		) err ON true
		LEFT JOIN LATERAL (
			-- A mailbox counts as "in an active campaign" when a running
			-- campaign either lists it explicitly or matches one of its tags.
			SELECT true AS active
			FROM campaigns c
			WHERE c.organization_id = ea.organization_id
			  AND c.status = 'active'
			  AND (
			    EXISTS (SELECT 1 FROM campaign_senders cs WHERE cs.campaign_id = c.id AND cs.email_account_id = ea.id)
			    OR EXISTS (
			      SELECT 1 FROM campaign_email_tags cet
			      JOIN email_tags et ON et.tag_id = cet.tag_id
			      WHERE cet.campaign_id = c.id AND et.email_id = ea.id
			    )
			  )
			LIMIT 1
		) camp ON true
		WHERE ea.organization_id = $1
		ORDER BY ea.created_at ASC`

	rows, err := r.db.Query(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AdvisorMailbox{}
	for rows.Next() {
		var m AdvisorMailbox
		if err := rows.Scan(
			&m.ID, &m.Email, &m.Name, &m.Status, &m.Provider, &m.AgeDays,
			&m.CampaignLimit, &m.MinWaitTime,
			&m.TrackingDomain, &m.TrackingDomainVerified,
			&m.AuthState, &m.AuthSPF, &m.AuthDKIM, &m.AuthDMARC, &m.AuthDMARCPolicy, &m.AuthFailingSince,
			&m.WarmupActive, &m.WarmupPaused,
			&m.WarmupBase, &m.WarmupMax, &m.WarmupIncrease, &m.WarmupReplyRate, &m.WarmupPoolType,
			&m.RiskBand,
			&m.ColdSent7d, &m.ColdSent1d, &m.ColdSent30d,
			&m.Bounces30d, &m.Complaints30d,
			&m.WarmupSent7d, &m.WarmupRecv7d, &m.WarmupSpam7d,
			&m.PoolHealth, &m.PoolSpamScore, &m.PoolBlocked,
			&m.UnresolvedErrs, &m.InActiveCampaign,
		); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *advisorRepository) loadCampaigns(ctx context.Context, orgID uuid.UUID) ([]AdvisorCampaign, error) {
	// Campaign funnel comes from campaign_contact_progress, which already
	// records per (campaign, contact, step) sent/open/click/reply/bounce. That
	// keeps this one join instead of re-deriving the funnel from events.
	query := `
		SELECT
			c.id, c.name, c.status::text,
			c.daily_limit, c.open_tracking, c.link_tracking, c.unsubscribe_header,
			c.stop_on_reply, c.text_only,
			c.timezone, c.days, to_char(c.start_time, 'HH24:MI'), to_char(c.end_time, 'HH24:MI'),
			COALESCE(c.schedule_windows, '{}'::jsonb),
			c.sender_strategy, c.rotation_mode, c.esp_match_mode,
			c.ramp_enabled, c.ramp_start, c.ramp_ceiling,
			c.tracking_domain, c.tracking_domain_verified,
			c.created_at, c.last_status_change_at,
			COALESCE(snd.n, 0), COALESCE(snd.capacity, 0), COALESCE(st.total, 0), COALESCE(st.emails, 0), COALESCE(ab.n, 0),
			COALESCE(f.sent, 0), COALESCE(f.opened, 0), COALESCE(f.clicked, 0),
			COALESCE(f.replied, 0), COALESCE(f.bounced, 0),
			COALESCE(cx.complaints, 0),
			COALESCE(l.total, 0), COALESCE(l.remaining, 0)
		FROM campaigns c
		LEFT JOIN LATERAL (
			SELECT COUNT(DISTINCT ea.id) AS n, COALESCE(SUM(ea.campaign_limit), 0) AS capacity
			FROM email_accounts ea
			WHERE ea.organization_id = c.organization_id
			  AND ea.status = 'active'
			  AND (
			    EXISTS (SELECT 1 FROM campaign_senders cs WHERE cs.campaign_id = c.id AND cs.email_account_id = ea.id)
			    OR EXISTS (
			      SELECT 1 FROM campaign_email_tags cet
			      JOIN email_tags et ON et.tag_id = cet.tag_id
			      WHERE cet.campaign_id = c.id AND et.email_id = ea.id
			    )
			  )
		) snd ON true
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS total,
			       COUNT(*) FILTER (WHERE COALESCE(s.kind, 'email') = 'email') AS emails
			FROM sequences s WHERE s.campaign_id = c.id
		) st ON true
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS n FROM campaign_ab_variants v
			WHERE v.campaign_id = c.id AND v.is_active
		) ab ON true
		LEFT JOIN LATERAL (
			SELECT
				COUNT(*) FILTER (WHERE ccp.sent_at IS NOT NULL)    AS sent,
				COUNT(*) FILTER (WHERE ccp.opened_at IS NOT NULL)  AS opened,
				COUNT(*) FILTER (WHERE ccp.clicked_at IS NOT NULL) AS clicked,
				COUNT(*) FILTER (WHERE ccp.replied_at IS NOT NULL) AS replied,
				COUNT(*) FILTER (WHERE ccp.bounced_at IS NOT NULL) AS bounced
			FROM campaign_contact_progress ccp
			WHERE ccp.campaign_id = c.id
			  AND ccp.sent_at > NOW() - INTERVAL '30 days'
		) f ON true
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS complaints FROM deliverability_events de
			WHERE de.campaign_id = c.id AND de.event_type = 'complaint'
			  AND de.created_at > NOW() - INTERVAL '30 days'
		) cx ON true
		LEFT JOIN LATERAL (
			SELECT
				COUNT(*) AS total,
				COUNT(*) FILTER (WHERE NOT EXISTS (
					SELECT 1 FROM campaign_contact_progress p
					WHERE p.campaign_id = c.id AND p.contact_id = cl.contact_id AND p.sent_at IS NOT NULL
				)) AS remaining
			FROM campaign_leads cl WHERE cl.campaign_id = c.id
		) l ON true
		WHERE c.organization_id = $1
		  AND c.status IN ('active', 'paused', 'draft')
		ORDER BY c.created_at DESC
		LIMIT 200`

	rows, err := r.db.Query(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AdvisorCampaign{}
	for rows.Next() {
		var c AdvisorCampaign
		if err := rows.Scan(
			&c.ID, &c.Name, &c.Status,
			&c.DailyLimit, &c.OpenTracking, &c.LinkTracking, &c.UnsubscribeHeader,
			&c.StopOnReply, &c.TextOnly,
			&c.Timezone, &c.Days, &c.StartTime, &c.EndTime, &c.ScheduleWindows,
			&c.SenderStrategy, &c.RotationMode, &c.ESPMatchMode,
			&c.RampEnabled, &c.RampStart, &c.RampCeiling,
			&c.TrackingDomain, &c.TrackingDomainVerified,
			&c.CreatedAt, &c.LastStatusChangeAt,
			&c.SenderCount, &c.SenderCapacity, &c.StepCount, &c.EmailStepCount, &c.VariantCount,
			&c.Sent, &c.Opened, &c.Clicked, &c.Replied, &c.Bounced, &c.Complaints,
			&c.LeadsTotal, &c.LeadsRemaining,
		); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *advisorRepository) loadSteps(ctx context.Context, orgID uuid.UUID) ([]AdvisorStep, error) {
	query := `
		SELECT
			s.id, s.campaign_id, s.name, s."position", COALESCE(s.kind, 'email'), s.wait_after,
			s.subject, s.body_plain, s.body_html,
			COALESCE(f.sent, 0), COALESCE(f.opened, 0), COALESCE(f.replied, 0), COALESCE(f.bounced, 0)
		FROM sequences s
		JOIN campaigns c ON c.id = s.campaign_id
		LEFT JOIN LATERAL (
			SELECT
				COUNT(*) FILTER (WHERE ccp.sent_at IS NOT NULL)    AS sent,
				COUNT(*) FILTER (WHERE ccp.opened_at IS NOT NULL)  AS opened,
				COUNT(*) FILTER (WHERE ccp.replied_at IS NOT NULL) AS replied,
				COUNT(*) FILTER (WHERE ccp.bounced_at IS NOT NULL) AS bounced
			FROM campaign_contact_progress ccp
			WHERE ccp.sequence_id = s.id
			  AND ccp.sent_at > NOW() - INTERVAL '30 days'
		) f ON true
		WHERE c.organization_id = $1
		  AND c.status IN ('active', 'paused', 'draft')
		ORDER BY s.campaign_id, s."position"
		LIMIT 2000`

	rows, err := r.db.Query(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []AdvisorStep{}
	for rows.Next() {
		var s AdvisorStep
		if err := rows.Scan(
			&s.ID, &s.CampaignID, &s.Name, &s.Position, &s.Kind, &s.WaitAfter,
			&s.Subject, &s.BodyPlain, &s.BodyHTML,
			&s.Sent, &s.Opened, &s.Replied, &s.Bounced,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// freeMailDomains are the consumer mailbox providers. A cold B2B list that is
// mostly these is usually scraped rather than sourced, and both reply rate and
// complaint rate reflect that.
const freeMailDomainsSQL = `'gmail.com','yahoo.com','hotmail.com','outlook.com','aol.com','icloud.com','gmx.com','proton.me','protonmail.com','mail.com','yandex.com','live.com','msn.com'`

// rolePrefixes are the shared-inbox local parts.
const rolePrefixesSQL = `'info','sales','support','contact','admin','hello','help','office','team','billing','careers','jobs','marketing','noreply','no-reply','webmaster','enquiries','enquiry'`

func (r *advisorRepository) loadListStats(ctx context.Context, orgID uuid.UUID) (map[uuid.UUID]AdvisorListStats, error) {
	query := `
		SELECT
			cl.campaign_id,
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE split_part(lower(ct.email), '@', 1) IN (` + rolePrefixesSQL + `)) AS role_addresses,
			COUNT(*) FILTER (WHERE split_part(lower(ct.email), '@', 2) IN (` + freeMailDomainsSQL + `)) AS free_mail,
			COUNT(*) FILTER (WHERE EXISTS (
				SELECT 1 FROM suppressed_recipients sr
				WHERE sr.organization_id = $1 AND lower(sr.email) = lower(ct.email)
				  AND (sr.expires_at IS NULL OR sr.expires_at > NOW())
			)) AS suppressed,
			COUNT(*) FILTER (WHERE ct.subscribed IS FALSE) AS unsubscribed,
			COUNT(*) FILTER (WHERE btrim(ct.first_name) = '') AS missing_first_name
		FROM campaign_leads cl
		JOIN contacts ct ON ct.id = cl.contact_id
		JOIN campaigns c ON c.id = cl.campaign_id
		WHERE c.organization_id = $1 AND c.status IN ('active', 'paused', 'draft')
		GROUP BY cl.campaign_id`

	rows, err := r.db.Query(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[uuid.UUID]AdvisorListStats{}
	for rows.Next() {
		var s AdvisorListStats
		if err := rows.Scan(&s.CampaignID, &s.Total, &s.RoleAddresses, &s.FreeMail,
			&s.Suppressed, &s.Unsubscribed, &s.MissingFirstName); err != nil {
			return nil, err
		}
		out[s.CampaignID] = s
	}
	return out, rows.Err()
}

func (r *advisorRepository) loadOrgStats(ctx context.Context, orgID uuid.UUID) (AdvisorOrgStats, error) {
	var s AdvisorOrgStats
	query := `
		SELECT
			(SELECT COUNT(*) FROM email_accounts WHERE organization_id = $1),
			(SELECT COUNT(*) FROM email_accounts WHERE organization_id = $1 AND status = 'active'),
			(SELECT COUNT(*) FROM campaigns WHERE organization_id = $1 AND status = 'active'),
			(SELECT COUNT(*) FROM suppressed_recipients
			 WHERE organization_id = $1 AND (expires_at IS NULL OR expires_at > NOW())),
			(SELECT COUNT(*) FROM (
				SELECT cl.contact_id
				FROM campaign_leads cl
				JOIN campaigns c ON c.id = cl.campaign_id
				WHERE c.organization_id = $1 AND c.status = 'active'
				GROUP BY cl.contact_id
				HAVING COUNT(DISTINCT cl.campaign_id) > 1
			) dupes)`
	err := r.db.QueryRow(ctx, query, orgID).Scan(
		&s.Mailboxes, &s.ActiveMailboxes, &s.RunningCampaigns, &s.SuppressedTotal, &s.DuplicateContacts,
	)
	return s, err
}
