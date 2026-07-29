package sandbox

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
)

// This file rounds the sandbox out to "everything the dashboard can show":
// the two campaign statuses the base seed skips (completed + paused), the
// deliverability/analytics tables, per-contact activity timelines, reply-intent
// classification, the suppression list, mailbox errors, an audit trail, a
// second teammate, and an API key. All idempotent so re-seeding is safe.
var (
	campaignCompleted = uuid.MustParse("44444444-aaaa-0000-0000-000000000004")
	campaignPaused    = uuid.MustParse("44444444-aaaa-0000-0000-000000000005")

	seqCompleted1 = uuid.MustParse("55555555-aaaa-0000-0000-000000000041")
	seqCompleted2 = uuid.MustParse("55555555-aaaa-0000-0000-000000000042")
	seqPaused1    = uuid.MustParse("55555555-aaaa-0000-0000-000000000051")

	// Second teammate + a pending invite, so Team and presence look real.
	sandboxUser2   = uuid.MustParse("11111111-aaaa-0000-0000-000000000002")
	sandboxInvite  = uuid.MustParse("ffffffff-aaaa-0000-0000-000000000001")
	sandboxAPIKey  = uuid.MustParse("f0f0f0f0-aaaa-0000-0000-000000000001")
	sandboxUser2Em = "marco.diaz@sunrise.test"
)

// seedAnalytics layers the remaining history + analytics on top of the base
// sandbox: extra campaign statuses, deliverability events, contact activity,
// reply intents, suppression, mailbox errors, audit log, team, and an API key.
func seedAnalytics(ctx context.Context, pool *pgxpool.Pool) error {
	for _, step := range []struct {
		name string
		fn   func(context.Context, *pgxpool.Pool) error
	}{
		{"team", seedTeam},
		{"extra campaigns", seedExtraCampaigns},
		{"deliverability", seedDeliverabilityEvents},
		{"contact activity", seedContactActivities},
		{"reply intents", seedReplyIntents},
		{"suppression", seedSuppression},
		{"mailbox errors", seedMailboxErrors},
		{"audit log", seedAuditLog},
		{"api key", seedAPIKey},
	} {
		if err := step.fn(ctx, pool); err != nil {
			return fmt.Errorf("%s: %w", step.name, err)
		}
	}
	return nil
}

// seedTeam adds a second accepted member (Marco Diaz, admin) and one pending
// invitation, so the Team page, member avatars, and presence have company.
func seedTeam(ctx context.Context, pool *pgxpool.Pool) error {
	hash, err := argon2.Hash(SandboxLoginPassword)
	if err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, first_name, last_name, email, password_hash)
		VALUES ($1, 'Marco', 'Diaz', $2, $3)
		ON CONFLICT (id) DO NOTHING`,
		sandboxUser2, sandboxUser2Em, hash); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO organization_members (organization_id, user_id, role, permissions, invited_by, invited_at, accepted_at)
		VALUES ($1, $2, 'admin', $3, $4, NOW() - INTERVAL '20 days', NOW() - INTERVAL '19 days')
		ON CONFLICT (organization_id, user_id) DO UPDATE SET
			role = EXCLUDED.role, permissions = EXCLUDED.permissions`,
		sandboxOrg, sandboxUser2, models.RolePermissions[models.RoleAdmin], sandboxUser); err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO organization_invitations (id, organization_id, email, role, permissions, invited_by, token, expires_at, created_at)
		VALUES ($1, $2, 'newhire@sunrise.test', 'manager', $3, $4, 'sbx-invite-token-newhire-0001', NOW() + INTERVAL '6 days', NOW() - INTERVAL '1 days')
		ON CONFLICT (id) DO NOTHING`,
		sandboxInvite, sandboxOrg, models.RolePermissions[models.RoleManager], sandboxUser)
	return err
}

// seedExtraCampaigns adds a completed campaign and a paused campaign so all four
// list buckets (draft/active/paused/completed) are populated. Both reuse the
// already-seeded launch/agency contacts as leads (a contact can be in several
// campaigns) and get their own funnel so their stats read as real.
func seedExtraCampaigns(ctx context.Context, pool *pgxpool.Pool) error {
	// Completed: fully wrapped, wide-open historical window.
	if _, err := pool.Exec(ctx, `
		INSERT INTO campaigns (
			id, user_id, organization_id, name, description,
			status, days, start_time, end_time, timezone,
			open_tracking, link_tracking,
			start_date, end_date, last_status_change_at, updated_at, created_at
		) VALUES (
			$1, $2, $3, 'Spring product launch (wrapped)', 'Completed sandbox campaign',
			'completed', 127, '00:00', '23:59', 'UTC',
			TRUE, TRUE,
			NOW() - INTERVAL '30 days', NOW() - INTERVAL '4 days', NOW() - INTERVAL '4 days',
			NOW() - INTERVAL '4 days', NOW() - INTERVAL '30 days'
		)
		ON CONFLICT (id) DO UPDATE SET status = 'completed', updated_at = NOW()`,
		campaignCompleted, sandboxUser, sandboxOrg); err != nil {
		return fmt.Errorf("completed campaign: %w", err)
	}
	// Paused: mid-flight, operator stopped it.
	if _, err := pool.Exec(ctx, `
		INSERT INTO campaigns (
			id, user_id, organization_id, name, description,
			status, days, start_time, end_time, timezone,
			open_tracking, link_tracking,
			start_date, last_status_change_at, updated_at, created_at
		) VALUES (
			$1, $2, $3, 'Feature announcement nudge', 'Paused sandbox campaign',
			'paused', 127, '00:00', '23:59', 'UTC',
			TRUE, TRUE,
			NOW() - INTERVAL '6 days', NOW() - INTERVAL '2 days',
			NOW() - INTERVAL '2 days', NOW() - INTERVAL '6 days'
		)
		ON CONFLICT (id) DO UPDATE SET status = 'paused', updated_at = NOW()`,
		campaignPaused, sandboxUser, sandboxOrg); err != nil {
		return fmt.Errorf("paused campaign: %w", err)
	}

	seqs := []struct {
		id       uuid.UUID
		campaign uuid.UUID
		name     string
		subject  string
		body     string
		wait     int
		pos      int
	}{
		{seqCompleted1, campaignCompleted, "Announcement", "{{.Company}} + our spring release",
			"Hi {{.FirstName}},\n\nOur spring release is live and a few things map directly to what {{.Company}} asked for. Two minute tour: https://warmbly.com/spring\n\nBest,\nSunrise team", 0, 0},
		{seqCompleted2, campaignCompleted, "Recap", "Re: {{.Company}} + our spring release",
			"Hi {{.FirstName}},\n\nClosing the loop before the quarter ends. Full changelog: https://warmbly.com/changelog\n\nBest,\nSunrise team", 3, 1},
		{seqPaused1, campaignPaused, "Heads up", "A quick heads up for {{.Company}}",
			"Hi {{.FirstName}},\n\nWe shipped the workflow you flagged. Want a walkthrough? https://warmbly.com/workflows\n\nBest,\nSunrise team", 0, 0},
	}
	for _, s := range seqs {
		if _, err := pool.Exec(ctx, `
			INSERT INTO sequences (id, campaign_id, organization_id, name, subject, body_plain, body_html, wait_after, position)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (id) DO NOTHING`,
			s.id, s.campaign, sandboxOrg, s.name, s.subject, s.body, plainToHTML(s.body), s.wait, s.pos); err != nil {
			return fmt.Errorf("sequence %s: %w", s.name, err)
		}
	}

	// Completed: reuse launch contacts 0..11, both steps sent, high open/click,
	// a few replies, one bounce (index 11, no second step).
	for i := 0; i < 12; i++ {
		cid := launchContactID(i)
		if _, err := pool.Exec(ctx, `INSERT INTO campaign_leads (campaign_id, contact_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, campaignCompleted, cid); err != nil {
			return err
		}
		st1 := progressStep{seq: seqCompleted1, sentDaysAgo: 26, openedDaysAgo: 25.8, clickedDaysAgo: -1, repliedDaysAgo: -1, bouncedDaysAgo: -1}
		switch {
		case i == 11:
			st1 = progressStep{seq: seqCompleted1, sentDaysAgo: 26, openedDaysAgo: -1, clickedDaysAgo: -1, repliedDaysAgo: -1, bouncedDaysAgo: 25.9}
		case i%5 == 0 && i != 0:
			st1.openedDaysAgo = -1 // a couple never opened
		case i%3 == 0:
			st1.clickedDaysAgo = 25.7
		}
		if err := insertProgress(ctx, pool, campaignCompleted, cid, st1); err != nil {
			return err
		}
		if i == 11 {
			continue
		}
		st2 := progressStep{seq: seqCompleted2, sentDaysAgo: 22, openedDaysAgo: 21.8, clickedDaysAgo: -1, repliedDaysAgo: -1, bouncedDaysAgo: -1}
		if i%6 == 0 {
			st2.repliedDaysAgo = 21.4
		}
		if err := insertProgress(ctx, pool, campaignCompleted, cid, st2); err != nil {
			return err
		}
	}

	// Paused: reuse agency contacts 0..7, only step 1 sent, ~half opened.
	for i := 0; i < 8; i++ {
		cid := agencyContactID(i)
		if _, err := pool.Exec(ctx, `INSERT INTO campaign_leads (campaign_id, contact_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, campaignPaused, cid); err != nil {
			return err
		}
		st := progressStep{seq: seqPaused1, sentDaysAgo: 3, openedDaysAgo: 2.8, clickedDaysAgo: -1, repliedDaysAgo: -1, bouncedDaysAgo: -1}
		if i%2 == 1 {
			st.openedDaysAgo = -1
		}
		if err := insertProgress(ctx, pool, campaignPaused, cid, st); err != nil {
			return err
		}
	}

	// Daily send rollups for both, so their charts aren't empty.
	if _, err := pool.Exec(ctx, `
		INSERT INTO campaign_daily_sends (campaign_id, send_date, emails_sent, new_leads_started)
		SELECT $1, d::date, 6 + ((EXTRACT(DAY FROM d)::int * 5) % 14), 1 + ((EXTRACT(DAY FROM d)::int) % 3)
		FROM generate_series(NOW() - INTERVAL '28 days', NOW() - INTERVAL '5 days', INTERVAL '1 day') AS d
		ON CONFLICT (campaign_id, send_date) DO UPDATE SET emails_sent = EXCLUDED.emails_sent, new_leads_started = EXCLUDED.new_leads_started`,
		campaignCompleted); err != nil {
		return fmt.Errorf("completed daily sends: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO campaign_daily_sends (campaign_id, send_date, emails_sent, new_leads_started)
		SELECT $1, d::date, 2 + ((EXTRACT(DAY FROM d)::int * 3) % 6), 1
		FROM generate_series(NOW() - INTERVAL '5 days', NOW() - INTERVAL '2 days', INTERVAL '1 day') AS d
		ON CONFLICT (campaign_id, send_date) DO UPDATE SET emails_sent = EXCLUDED.emails_sent, new_leads_started = EXCLUDED.new_leads_started`,
		campaignPaused); err != nil {
		return fmt.Errorf("paused daily sends: %w", err)
	}
	return nil
}

// delivEvent is one row for the deliverability dashboard.
type delivEvent struct {
	key       string // idempotency_key (also the ON CONFLICT target)
	campaign  uuid.UUID
	contact   uuid.UUID
	eventType string // bounce | complaint | unsubscribe | reply | open | click
	email     string
	reason    string
	daysAgo   float64
}

// seedDeliverabilityEvents populates the org deliverability feed: engagement
// (open/click/reply) plus the negative signals (bounce/complaint/unsubscribe)
// the dashboard buckets and rates. event_type values match models.Deliverability*.
func seedDeliverabilityEvents(ctx context.Context, pool *pgxpool.Pool) error {
	events := []delivEvent{
		{"sbx-deliv-open-1", campaignLaunch, launchContactID(0), "open", "aiden.park@northwind.test", "", 0.4},
		{"sbx-deliv-open-2", campaignLaunch, launchContactID(4), "open", "eli.grant@hooli.test", "", 0.9},
		{"sbx-deliv-open-3", campaignLaunch, launchContactID(7), "open", "hana.jules@wayne.test", "", 1.6},
		{"sbx-deliv-open-4", campaignCompleted, launchContactID(2), "open", "carlos.diaz@piedpiper.test", "", 24.0},
		{"sbx-deliv-click-1", campaignLaunch, launchContactID(0), "click", "aiden.park@northwind.test", "", 0.3},
		{"sbx-deliv-click-2", campaignLaunch, launchContactID(4), "click", "eli.grant@hooli.test", "", 0.8},
		{"sbx-deliv-click-3", campaignCompleted, launchContactID(3), "click", "diana.fox@globex.test", "", 23.5},
		{"sbx-deliv-reply-1", campaignLaunch, launchContactID(0), "reply", "aiden.park@northwind.test", "positive reply", 0.4},
		{"sbx-deliv-reply-2", campaignLaunch, launchContactID(4), "reply", "eli.grant@hooli.test", "positive reply", 0.9},
		{"sbx-deliv-reply-3", campaignAgency, agencyContactID(0), "reply", "amara.bell@brightloop.test", "positive reply", 1.2},
		{"sbx-deliv-bounce-1", campaignLaunch, launchContactID(8), "bounce", "ivan.kova@tyrell.test", "550 5.1.1 recipient rejected", 3.0},
		{"sbx-deliv-bounce-2", campaignCompleted, launchContactID(11), "bounce", "lena.novak@cyberdyne.test", "550 mailbox unavailable", 25.9},
		{"sbx-deliv-complaint-1", campaignLaunch, launchContactID(10), "complaint", "kofi.mensah@acme-corp.test", "marked as spam", 2.6},
		{"sbx-deliv-unsub-1", campaignLaunch, launchContactID(5), "unsubscribe", "fiona.hale@umbrella.test", "one-click unsubscribe", 5.1},
		{"sbx-deliv-unsub-2", campaignLaunch, launchContactID(13), "unsubscribe", "nils.pett@aperture.test", "one-click unsubscribe", 6.3},
	}
	for _, e := range events {
		sql := fmt.Sprintf(`
			INSERT INTO deliverability_events
				(organization_id, campaign_id, contact_id, event_type, provider, recipient_email, reason, idempotency_key, created_at)
			VALUES ($1, $2, $3, $4, 'sandbox', $5, $6, $7, NOW() - INTERVAL '%f days')
			ON CONFLICT (idempotency_key) DO NOTHING`, e.daysAgo)
		if _, err := pool.Exec(ctx, sql, sandboxOrg, e.campaign, e.contact, e.eventType, e.email, e.reason, e.key); err != nil {
			return fmt.Errorf("deliverability %s: %w", e.key, err)
		}
	}
	return nil
}

// seedContactActivities builds a 360 timeline for the deal contacts so the
// contact detail pane reads as a real relationship, not a bare row.
func seedContactActivities(ctx context.Context, pool *pgxpool.Pool) error {
	type act struct {
		n       int
		contact uuid.UUID
		kind    string // activity_type enum
		daysAgo float64
	}
	acts := []act{
		{1, launchContactID(0), "contact_created", 12},
		{2, launchContactID(0), "email_sent", 6},
		{3, launchContactID(0), "email_opened", 5.9},
		{4, launchContactID(0), "email_replied", 0.4},
		{5, launchContactID(0), "note_added", 0.3},
		{6, launchContactID(0), "deal_created", 0.2},
		{7, launchContactID(0), "task_created", 0.1},
		{8, launchContactID(4), "contact_created", 10},
		{9, launchContactID(4), "email_sent", 5},
		{10, launchContactID(4), "email_opened", 4.9},
		{11, launchContactID(4), "email_clicked", 4.8},
		{12, launchContactID(4), "email_replied", 0.9},
		{13, launchContactID(4), "deal_created", 0.8},
		{14, launchContactID(7), "contact_created", 9},
		{15, launchContactID(7), "email_sent", 4},
		{16, launchContactID(7), "email_opened", 3.9},
		{17, launchContactID(7), "email_replied", 1.6},
		{18, agencyContactID(0), "contact_created", 8},
		{19, agencyContactID(0), "email_sent", 3},
		{20, agencyContactID(0), "email_replied", 1.2},
		{21, agencyContactID(0), "deal_created", 1.1},
		{22, launchContactID(1), "email_sent", 5},
		{23, launchContactID(1), "email_opened", 4.9},
		{24, launchContactID(1), "deal_won", 5},
	}
	for _, a := range acts {
		id := uuid.MustParse(fmt.Sprintf("a0a0a0a0-aaaa-0000-0000-%012d", a.n))
		sql := fmt.Sprintf(`
			INSERT INTO contact_activities (id, contact_id, organization_id, user_id, activity_type, created_at)
			VALUES ($1, $2, $3, $4, $5::public.activity_type, NOW() - INTERVAL '%f days')
			ON CONFLICT (id) DO NOTHING`, a.daysAgo)
		if _, err := pool.Exec(ctx, sql, id, a.contact, sandboxOrg, sandboxUser, a.kind); err != nil {
			return fmt.Errorf("activity %d: %w", a.n, err)
		}
	}
	return nil
}

// seedReplyIntents classifies the seeded inbound replies so the reply-intent
// analytics (positive/negative/out_of_office/question/neutral) show a spread.
func seedReplyIntents(ctx context.Context, pool *pgxpool.Pool) error {
	type ri struct {
		n          int
		email      string
		campaign   uuid.UUID
		intent     string
		confidence float64
		action     string
		daysAgo    float64
	}
	rows := []ri{
		{1, "aiden.park@northwind.test", campaignLaunch, "positive", 0.94, "created deal", 0.4},
		{2, "eli.grant@hooli.test", campaignLaunch, "positive", 0.91, "sent benchmarks", 0.9},
		{3, "amara.bell@brightloop.test", campaignAgency, "positive", 0.88, "booked call", 1.2},
		{4, "hana.jules@wayne.test", campaignLaunch, "question", 0.72, "proposed demo slot", 1.6},
		{5, "diana.fox@globex.test", campaignLaunch, "out_of_office", 0.99, "snoozed 3 days", 2.1},
		{6, "kofi.mensah@acme-corp.test", campaignLaunch, "negative", 0.87, "suppressed contact", 2.6},
		{7, "boris.chan@funnelworks.test", campaignAgency, "neutral", 0.55, "no action", 4.2},
	}
	for _, r := range rows {
		id := uuid.MustParse(fmt.Sprintf("b0b0b0b0-aaaa-0000-0000-%012d", r.n))
		sql := fmt.Sprintf(`
			INSERT INTO reply_intents (id, organization_id, contact_email, campaign_id, intent, confidence, action_taken, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, NOW() - INTERVAL '%f days')
			ON CONFLICT (id) DO NOTHING`, r.daysAgo)
		if _, err := pool.Exec(ctx, sql, id, sandboxOrg, r.email, r.campaign, r.intent, r.confidence, r.action); err != nil {
			return fmt.Errorf("reply intent %d: %w", r.n, err)
		}
	}
	return nil
}

// seedSuppression fills the suppression list: the two unsubscribed prospects, a
// spam complaint, and a hard bounce, so the Suppression view and the
// campaign-skip logic have real entries.
func seedSuppression(ctx context.Context, pool *pgxpool.Pool) error {
	type sup struct {
		email    string
		source   string // bounce | complaint | unsubscribe
		reason   string
		campaign uuid.UUID
		daysAgo  float64
	}
	rows := []sup{
		{"fiona.hale@umbrella.test", "unsubscribe", "one-click unsubscribe", campaignLaunch, 5.1},
		{"nils.pett@aperture.test", "unsubscribe", "one-click unsubscribe", campaignLaunch, 6.3},
		{"kofi.mensah@acme-corp.test", "complaint", "marked message as spam", campaignLaunch, 2.6},
		{"ivan.kova@tyrell.test", "bounce", "hard bounce: recipient rejected", campaignLaunch, 3.0},
	}
	for _, r := range rows {
		sql := fmt.Sprintf(`
			INSERT INTO suppressed_recipients (organization_id, email, reason, source, campaign_id, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, NOW() - INTERVAL '%f days', NOW() - INTERVAL '%f days')
			ON CONFLICT (organization_id, email) DO UPDATE SET
				reason = EXCLUDED.reason, source = EXCLUDED.source, updated_at = EXCLUDED.updated_at`, r.daysAgo, r.daysAgo)
		if _, err := pool.Exec(ctx, sql, sandboxOrg, r.email, r.reason, r.source, r.campaign); err != nil {
			return fmt.Errorf("suppression %s: %w", r.email, err)
		}
	}
	return nil
}

// seedMailboxErrors attaches a resolved warning and a live informational notice
// to two senders so mailbox health and the deliverability issues list aren't
// blank. severity/error_code/resolve_method match the enums in the schema.
func seedMailboxErrors(ctx context.Context, pool *pgxpool.Pool) error {
	type merr struct {
		id       uuid.UUID
		mailbox  uuid.UUID
		code     string
		severity string
		resolve  string
		title    string
		message  string
		resolved bool
		daysAgo  float64
	}
	rows := []merr{
		{
			uuid.MustParse("e1e1e1e1-aaaa-0000-0000-000000000001"),
			sandboxMailboxes[3].id, // Tom Abel
			"SENDING_TOO_FAST", "WARNING", "RETRY",
			"Provider asked us to slow down",
			"The provider throttled a burst of sends. Warmbly backed off and spaced the queue out; no mail was lost.",
			true, 4.0,
		},
		{
			uuid.MustParse("e1e1e1e1-aaaa-0000-0000-000000000002"),
			sandboxMailboxes[4].id, // Elena Voss
			"RATE_LIMIT_EXCEEDED", "INFORMATIONAL", "NONE",
			"Approaching the daily send limit",
			"This mailbox is close to its configured daily cap. Sends will resume tomorrow or when you raise the limit.",
			false, 0.3,
		},
		{
			uuid.MustParse("e1e1e1e1-aaaa-0000-0000-000000000003"),
			sandboxMailboxes[18].id, // Yuki Sato - the watch-state pool member
			"SPAM_PLACEMENT_RISING", "WARNING", "NONE",
			"Warmup mail landing in spam more often",
			"Recent warmup deliveries from this mailbox were placed in spam folders more often than the pool average. Warmup volume was reduced while it recovers.",
			false, 1.2,
		},
	}
	for _, r := range rows {
		resolvedExpr := "NULL"
		if r.resolved {
			resolvedExpr = fmt.Sprintf("NOW() - INTERVAL '%f days'", r.daysAgo-0.5)
		}
		sql := fmt.Sprintf(`
			INSERT INTO email_account_errors
				(id, email_account_id, user_id, error_code, severity, resolve_method, title, message, resolved_at, created_at)
			VALUES ($1, $2, $3, $4, $5::public.email_error_severity, $6::public.email_error_resolve_method, $7, $8, %s, NOW() - INTERVAL '%f days')
			ON CONFLICT (id) DO NOTHING`, resolvedExpr, r.daysAgo)
		if _, err := pool.Exec(ctx, sql, r.id, r.mailbox, sandboxUser, r.code, r.severity, r.resolve, r.title, r.message); err != nil {
			return fmt.Errorf("mailbox error %s: %w", r.code, err)
		}
	}
	return nil
}

// seedAuditLog writes a believable org activity trail so the Audit Log page has
// depth. action/entity_type pairs match what the real handlers write.
func seedAuditLog(ctx context.Context, pool *pgxpool.Pool) error {
	type entry struct {
		n          int
		actor      uuid.UUID
		action     string
		entityType string
		entity     uuid.UUID
		daysAgo    float64
	}
	entries := []entry{
		{1, sandboxUser, "create", "campaign", campaignCompleted, 30},
		{2, sandboxUser, "start", "campaign", campaignCompleted, 29},
		{3, sandboxUser, "create", "email_account", sandboxMailboxes[0].id, 25},
		{4, sandboxUser2, "create", "email_account", sandboxMailboxes[5].id, 24},
		{5, sandboxUser, "create", "campaign", campaignLaunch, 13},
		{6, sandboxUser, "start", "campaign", campaignLaunch, 12},
		{7, sandboxUser2, "create", "campaign", campaignAgency, 10},
		{8, sandboxUser, "invite", "organization_member", sandboxUser2, 20},
		{9, sandboxUser, "create", "crm_deal", dealNorthwind, 1},
		{10, sandboxUser2, "create", "crm_task", crmTaskFollowup, 1},
		{11, sandboxUser, "stop", "campaign", campaignPaused, 2},
		{12, sandboxUser, "create", "api_key", sandboxAPIKey, 7},
	}
	for _, e := range entries {
		id := uuid.MustParse(fmt.Sprintf("adadadad-aaaa-0000-0000-%012d", e.n))
		sql := fmt.Sprintf(`
			INSERT INTO audit_logs (id, organization_id, actor_id, action, entity_type, entity_id, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, NOW() - INTERVAL '%f days')
			ON CONFLICT (id) DO NOTHING`, e.daysAgo)
		if _, err := pool.Exec(ctx, sql, id, sandboxOrg, e.actor, e.action, e.entityType, e.entity); err != nil {
			return fmt.Errorf("audit %d: %w", e.n, err)
		}
	}
	return nil
}

// seedAPIKey seeds one active developer key so the API keys surface isn't empty.
// The hash is a placeholder (the plaintext key is never recoverable after
// creation anyway), so it lists and shows metadata but won't authenticate.
func seedAPIKey(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO api_keys
			(id, user_id, organization_id, name, key_prefix, key_suffix, key_hash, permissions, status, description, last_used_at, created_at, updated_at)
		VALUES ($1, $2, $3, 'Production integration', 'wk_live_', 'a1b9',
			'sandbox-placeholder-not-a-real-key-hash', 2047, 'active',
			'Server-to-server key used by the demo integration.',
			NOW() - INTERVAL '2 hours', NOW() - INTERVAL '7 days', NOW())
		ON CONFLICT (id) DO NOTHING`,
		sandboxAPIKey, sandboxUser, sandboxOrg)
	return err
}
