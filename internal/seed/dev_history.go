package seed

// Dev-org history: funnel progress, stats rollups, a live unified inbox, and
// campaign logs. All timestamps are computed relative to NOW() so "today"
// always has activity, no matter when the seed runs. Patterns ported from
// internal/sandbox/history.go without coupling to that package.

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// devDayExpr renders "NOW() - INTERVAL 'N days'" or NULL for a negative offset.
func devDayExpr(daysAgo float64) string {
	if daysAgo < 0 {
		return "NULL"
	}
	return fmt.Sprintf("NOW() - INTERVAL '%f days'", daysAgo)
}

type devFunnelState int

const (
	devQueued devFunnelState = iota
	devDone
	devProcessing
	devReplied
	devBounced
)

// devClassify buckets a lead index into a funnel state (ported from the
// sandbox classify): ~37% done, ~25% processing, ~12% replied, ~12% bounced,
// rest queued. Index modulo keeps it stable across re-seeds.
func devClassify(i int) devFunnelState {
	switch i % 8 {
	case 0, 1, 2:
		return devDone
	case 3, 4:
		return devProcessing
	case 5:
		return devReplied
	case 6:
		return devBounced
	default:
		return devQueued
	}
}

// devProgressStep is one campaign_contact_progress row; offsets are in days
// ago, negative means "unset".
type devProgressStep struct {
	seq                   uuid.UUID
	sent, opened, clicked float64
	replied, bounced      float64
}

// devProgressRows builds the rows for lead i. The processing cohort's latest
// send lands a few hours ago at index-staggered times, so the campaign always
// shows sends from today. Queued leads get no rows.
func devProgressRows(i int) []devProgressStep {
	j := float64(i%5) * 0.09 // per-contact jitter so timestamps don't stack
	switch devClassify(i) {
	case devDone:
		rows := []devProgressStep{
			{seq: devSeqStep1, sent: 13.2 - j, opened: 13.1 - j, clicked: 13.0 - j, replied: -1, bounced: -1},
			{seq: devSeqStep2, sent: 8.4 - j, opened: 8.3 - j, clicked: -1, replied: -1, bounced: -1},
			{seq: devSeqStep3, sent: 3.1 - j, opened: 2.9 - j, clicked: -1, replied: -1, bounced: -1},
		}
		return rows
	case devProcessing:
		// Step 2 went out today, staggered by index (~30min .. ~5.5h ago).
		return []devProgressStep{
			{seq: devSeqStep1, sent: 4.2 - j, opened: 4.1 - j, clicked: -1, replied: -1, bounced: -1},
			{seq: devSeqStep2, sent: 0.02 + float64(i)*0.01, opened: -1, clicked: -1, replied: -1, bounced: -1},
		}
	case devReplied:
		return []devProgressStep{
			{seq: devSeqStep1, sent: 6.5 - j, opened: 6.4 - j, clicked: 6.35 - j, replied: -1, bounced: -1},
			{seq: devSeqStep2, sent: 3.2 - j, opened: 3.0 - j, clicked: -1, replied: 2.6 - j, bounced: -1},
		}
	case devBounced:
		return []devProgressStep{
			{seq: devSeqStep1, sent: 5.8 - j, opened: -1, clicked: -1, replied: -1, bounced: 5.75 - j},
		}
	default:
		return nil
	}
}

// seedDevProgress writes the mid-flight funnel for the active campaign.
func seedDevProgress(ctx context.Context, pool *pgxpool.Pool) error {
	for i := 0; i < devLeadCount; i++ {
		for _, r := range devProgressRows(i) {
			sql := fmt.Sprintf(`
				INSERT INTO campaign_contact_progress
					(campaign_id, contact_id, sequence_id, sent_at, opened_at, clicked_at, replied_at, bounced_at)
				VALUES ($1, $2, $3, %s, %s, %s, %s, %s)
				ON CONFLICT (campaign_id, contact_id, sequence_id) DO UPDATE SET
					sent_at = EXCLUDED.sent_at,
					opened_at = EXCLUDED.opened_at,
					clicked_at = EXCLUDED.clicked_at,
					replied_at = EXCLUDED.replied_at,
					bounced_at = EXCLUDED.bounced_at`,
				devDayExpr(r.sent), devDayExpr(r.opened), devDayExpr(r.clicked),
				devDayExpr(r.replied), devDayExpr(r.bounced))
			if _, err := pool.Exec(ctx, sql, DevCampaignActiveID, devContactID(i), r.seq); err != nil {
				return fmt.Errorf("progress %d: %w", i, err)
			}
		}
	}
	return nil
}

// seedDevStats backfills the daily aggregates the dashboard reads: campaign
// sends, per-mailbox warmup stats, and per-mailbox send counts, all 14 days
// including today.
func seedDevStats(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `
		INSERT INTO campaign_daily_sends (campaign_id, send_date, emails_sent, new_leads_started)
		SELECT $1, d::date,
			8 + ((EXTRACT(DAY FROM d)::int * 7) % 23),
			1 + ((EXTRACT(DAY FROM d)::int * 3) % 4)
		FROM generate_series(NOW() - INTERVAL '13 days', NOW(), INTERVAL '1 day') AS d
		ON CONFLICT (campaign_id, send_date) DO UPDATE SET
			emails_sent = EXCLUDED.emails_sent,
			new_leads_started = EXCLUDED.new_leads_started
	`, DevCampaignActiveID); err != nil {
		return fmt.Errorf("campaign_daily_sends: %w", err)
	}

	for _, id := range []uuid.UUID{DevMailboxSendID, DevMailboxOutboundID, DevMailboxGrowthID, DevMailboxPartnersID} {
		// Warmup volume ramps 12 -> 38 toward today, with occasional replies.
		if _, err := pool.Exec(ctx, `
			INSERT INTO warmup_statistics (email_account_id, date, emails_sent, emails_replied, target_volume)
			SELECT $1, (NOW() - (g.n || ' days')::interval)::date,
				LEAST(38, 12 + (13 - g.n) * 2),
				GREATEST(0, (13 - g.n) / 3),
				40
			FROM generate_series(0, 13) AS g(n)
			ON CONFLICT (email_account_id, date) DO UPDATE SET
				emails_sent = EXCLUDED.emails_sent,
				emails_replied = EXCLUDED.emails_replied,
				target_volume = EXCLUDED.target_volume
		`, id); err != nil {
			return fmt.Errorf("warmup_statistics %s: %w", id, err)
		}

		if _, err := pool.Exec(ctx, `
			INSERT INTO daily_email_counts (email_account_id, date, count)
			SELECT $1, d::date, 4 + ((EXTRACT(DAY FROM d)::int * 5) % 13)
			FROM generate_series(NOW() - INTERVAL '13 days', NOW(), INTERVAL '1 day') AS d
			ON CONFLICT (email_account_id, date) DO UPDATE SET count = EXCLUDED.count
		`, id); err != nil {
			return fmt.Errorf("daily_email_counts %s: %w", id, err)
		}
	}
	return nil
}

// devUniboxMsg is one message in the dev user's unified inbox. Every emailID
// is a dev-org mailbox so the web ReplyComposer can resolve the sender
// (the API's email_id becomes account_id in the client).
type devUniboxMsg struct {
	emailID   uuid.UUID
	threadID  string
	messageID string
	parentID  string
	uid       int
	from      string
	to        string
	subject   string
	snippet   string
	seen      bool
	daysAgo   float64
}

// seedDevUnibox seeds 13 messages across 8 threads (reply chains, an
// objection, OOO, a bounce, a warmup-style exchange) plus thread labels.
// The two seedBaseline messages remain as older history.
func seedDevUnibox(ctx context.Context, pool *pgxpool.Pool) error {
	// Reuse the baseline uid_validity values (101/201) for the first two
	// mailboxes so no duplicate INBOX folder rows appear.
	mailboxes := []struct {
		emailID     uuid.UUID
		uidValidity int
	}{
		{DevMailboxSendID, 101},
		{DevMailboxOutboundID, 201},
		{DevMailboxGrowthID, 301},
		{DevMailboxPartnersID, 401},
	}
	for _, mb := range mailboxes {
		if _, err := pool.Exec(ctx, `
			INSERT INTO unibox_mailboxes (email_id, uid_validity, mailbox, attributes, highestmodseq, updated_at)
			VALUES ($1, $2, 'INBOX', ARRAY['\HasNoChildren'], 1, NOW())
			ON CONFLICT (email_id, uid_validity) DO UPDATE SET
				mailbox = EXCLUDED.mailbox,
				updated_at = NOW()
		`, mb.emailID, mb.uidValidity); err != nil {
			return fmt.Errorf("unibox mailbox %s: %w", mb.emailID, err)
		}
	}

	sender := "Dev Sender <dev.send@warmbly.test>"
	outbound := "Dev Outbound <dev.outbound@warmbly.test>"
	growth := "Dev Growth <dev.growth@warmbly.test>"
	partners := "Dev Partners <dev.partners@warmbly.test>"

	msgs := []devUniboxMsg{
		// Positive pricing thread with Mira (reply chain, 3 messages).
		{DevMailboxSendID, "dev-thread-mira", "<dev-mira-1@lumina-labs.test>", "", 111,
			"Mira Kovacs <mira.kovacs@lumina-labs.test>", sender,
			"Re: Quick question about outbound at Lumina Labs",
			"This is timely. We are re-evaluating our outbound stack this quarter. What does the team plan cost for 8 seats?",
			true, 2.6},
		{DevMailboxSendID, "dev-thread-mira", "<dev-mira-2@warmbly.test>", "<dev-mira-1@lumina-labs.test>", 112,
			sender, "Mira Kovacs <mira.kovacs@lumina-labs.test>",
			"Re: Quick question about outbound at Lumina Labs",
			"Great to hear. For 8 seats the team plan lands at $340/mo, and I attached the placement report we run for every mailbox.",
			true, 2.4},
		{DevMailboxSendID, "dev-thread-mira", "<dev-mira-3@lumina-labs.test>", "<dev-mira-2@warmbly.test>", 113,
			"Mira Kovacs <mira.kovacs@lumina-labs.test>", sender,
			"Re: Quick question about outbound at Lumina Labs",
			"The report is exactly what our COO wants to see. Can you hold that price until the end of the month?",
			false, 0.2},
		// Objection thread with Jonas (2 messages).
		{DevMailboxOutboundID, "dev-thread-jonas", "<dev-jonas-1@fieldstone.test>", "", 211,
			"Jonas Weber <jonas.weber@fieldstone.test>", outbound,
			"Re: Quick question about outbound at Fieldstone",
			"We already route everything through our CRM's sending add-on. Not sure what you would add on top of that.",
			true, 3.4},
		{DevMailboxOutboundID, "dev-thread-jonas", "<dev-jonas-2@warmbly.test>", "<dev-jonas-1@fieldstone.test>", 212,
			outbound, "Jonas Weber <jonas.weber@fieldstone.test>",
			"Re: Quick question about outbound at Fieldstone",
			"Fair question. CRM add-ons send fine but rarely warm or monitor placement. Happy to run a free placement test on one mailbox so you can compare.",
			true, 3.2},
		// Out of office (1 message).
		{DevMailboxGrowthID, "dev-thread-talia-ooo", "<dev-talia-ooo@arcadia-metrics.test>", "", 311,
			"Talia Reyes <talia.reyes@arcadia-metrics.test>", growth,
			"Automatic reply: Quick question about outbound at Arcadia Metrics",
			"I am out of office until next Monday with limited email access. For urgent matters contact ops@arcadia-metrics.test.",
			true, 4.8},
		// Bounce (1 message).
		{DevMailboxOutboundID, "dev-thread-omar-bounce", "<dev-omar-bounce@mailer-daemon.test>", "", 213,
			"Mail Delivery Subsystem <mailer-daemon@bluepeak.test>", outbound,
			"Delivery Status Notification (Failure)",
			"The message to omar.said@bluepeak.test could not be delivered. The recipient address was rejected by the server.",
			false, 5.7},
		// Warmup-style exchange (2 messages).
		{DevMailboxPartnersID, "dev-thread-warmup", "<dev-warmup-1@warmbly-pool.test>", "", 411,
			"Liam Farrell <liam.farrell@warmbly-pool.test>", partners,
			"Notes from the offsite",
			"Thanks for sending those over. The planning doc matches what we discussed, I will pass it along to the team.",
			true, 7.5},
		{DevMailboxPartnersID, "dev-thread-warmup", "<dev-warmup-2@warmbly.test>", "<dev-warmup-1@warmbly-pool.test>", 412,
			partners, "Liam Farrell <liam.farrell@warmbly-pool.test>",
			"Re: Notes from the offsite",
			"Sounds good. Let me know if anything in the doc needs another pass before Friday.",
			true, 7.3},
		// Meeting thread with Nadia (2 messages).
		{DevMailboxSendID, "dev-thread-nadia", "<dev-nadia-1@cinderworks.test>", "", 114,
			"Nadia Osei <nadia.osei@cinderworks.test>", sender,
			"Re: Quick question about outbound at Cinderworks",
			"Thursday at 2pm works for a walkthrough. Send the invite and I will loop in our ops lead.",
			false, 1.6},
		{DevMailboxSendID, "dev-thread-nadia", "<dev-nadia-2@warmbly.test>", "<dev-nadia-1@cinderworks.test>", 115,
			sender, "Nadia Osei <nadia.osei@cinderworks.test>",
			"Re: Quick question about outbound at Cinderworks",
			"Invite sent for Thursday 2pm. I included the pilot scope so your ops lead can skim it beforehand.",
			true, 1.5},
		// Not interested (1 message).
		{DevMailboxGrowthID, "dev-thread-ruben", "<dev-ruben-1@driftline.test>", "", 312,
			"Ruben Ortiz <ruben.ortiz@driftline.test>", growth,
			"Re: Closing the loop",
			"Not a fit for us right now, please take me off this list. Thanks for keeping it short.",
			true, 6.4},
		// Fresh question, arrived today (1 message).
		{DevMailboxSendID, "dev-thread-chloe", "<dev-chloe-1@emberly.test>", "", 116,
			"Chloe Bennett <chloe.bennett@emberly.test>", sender,
			"Re: Quick question about outbound at Emberly",
			"Quick question before we talk: can partners see placement reports for the client mailboxes they manage?",
			false, 0.1},
	}

	for i, m := range msgs {
		sql := fmt.Sprintf(`
			INSERT INTO unibox_emails (
				id, user_id, email_id, mailbox, thread_id, message_id,
				gmail_id, parent_id, uid, mod_seq,
				flags, bcc, cc, from_addr, in_reply_to, reply_to,
				to_addr, subject, size, internal_date, sent_date,
				snippet, seen, created_at, updated_at
			) VALUES (
				$1, $2, $3, 0, $4, $5,
				'', $6, $7, 1,
				$8, '{}', '{}', $9, '{}', '{}',
				$10, $11, $12, %s, %s,
				$13, $14, NOW(), NOW()
			)
			ON CONFLICT (id) DO UPDATE SET
				email_id = EXCLUDED.email_id,
				thread_id = EXCLUDED.thread_id,
				message_id = EXCLUDED.message_id,
				parent_id = EXCLUDED.parent_id,
				uid = EXCLUDED.uid,
				flags = EXCLUDED.flags,
				from_addr = EXCLUDED.from_addr,
				to_addr = EXCLUDED.to_addr,
				subject = EXCLUDED.subject,
				size = EXCLUDED.size,
				internal_date = EXCLUDED.internal_date,
				sent_date = EXCLUDED.sent_date,
				snippet = EXCLUDED.snippet,
				seen = EXCLUDED.seen,
				updated_at = NOW()`,
			devDayExpr(m.daysAgo), devDayExpr(m.daysAgo))
		flags := []string{}
		if m.seen {
			flags = []string{"\\Seen"}
		}
		if _, err := pool.Exec(ctx, sql,
			devEntityID("10", i+1), DevUserID, m.emailID, m.threadID, m.messageID,
			m.parentID, m.uid, flags, []string{m.from},
			[]string{m.to}, m.subject, int64(len(m.snippet)), m.snippet, m.seen); err != nil {
			return fmt.Errorf("unibox %s: %w", m.messageID, err)
		}
	}

	labels := []struct {
		threadID string
		category uuid.UUID
	}{
		{"dev-thread-mira", devCategoryLead},
		{"dev-thread-nadia", devCategoryLead},
		{"dev-thread-chloe", devCategoryCustomer},
		{"dev-thread-jonas", devCategoryChurn},
	}
	for _, l := range labels {
		if _, err := pool.Exec(ctx, `
			INSERT INTO unibox_thread_labels (user_id, thread_id, category_id)
			VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING
		`, DevUserID, l.threadID, l.category); err != nil {
			return fmt.Errorf("thread label %s: %w", l.threadID, err)
		}
	}
	return nil
}

// seedDevCampaignLogs writes the active campaign's activity feed with fixed
// IDs so re-seeding never duplicates entries.
func seedDevCampaignLogs(ctx context.Context, pool *pgxpool.Pool) error {
	logs := []struct {
		campaign uuid.UUID
		event    string
		message  string
		daysAgo  float64
	}{
		{DevCampaignActiveID, "started", "Campaign moved to active", 13.95},
		{DevCampaignActiveID, "contacts_added", "24 contacts added from the RevOps list", 13.9},
		{DevCampaignActiveID, "sequence_added", "Step 3 - break-up added", 10.0},
		{DevCampaignActiveID, "contact_bounced", "omar.said@bluepeak.test hard bounced and was suppressed", 5.7},
		{DevCampaignActiveID, "contact_replied", "mira.kovacs@lumina-labs.test replied", 2.6},
		{DevCampaignActiveID, "contact_replied", "chloe.bennett@emberly.test replied", 0.1},
		{DevCampaignDraftID, "created", "Campaign created", 2.0},
	}
	for i, l := range logs {
		sql := fmt.Sprintf(`
			INSERT INTO campaign_logs (id, campaign_id, event_type, message, metadata, created_at)
			VALUES ($1, $2, $3, $4, '{}'::jsonb, %s)
			ON CONFLICT (id) DO NOTHING`, devDayExpr(l.daysAgo))
		if _, err := pool.Exec(ctx, sql, devEntityID("12", i+1), l.campaign, l.event, l.message); err != nil {
			return fmt.Errorf("log %d: %w", i, err)
		}
	}
	return nil
}
