package sandbox

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Extra sandbox-history UUIDs (all carry the "aaaa" sandbox marker). Kept
// disjoint from the identity/campaign IDs declared in seed.go.
var (
	// Unibox inbound messages (77777777-aaaa-...).
	uniboxReplyPositive1 = uuid.MustParse("77777777-aaaa-0000-0000-000000000001")
	uniboxReplyPositive2 = uuid.MustParse("77777777-aaaa-0000-0000-000000000002")
	uniboxReplyPositive3 = uuid.MustParse("77777777-aaaa-0000-0000-000000000003")
	uniboxReplyOOO       = uuid.MustParse("77777777-aaaa-0000-0000-000000000004")
	uniboxReplyMeeting   = uuid.MustParse("77777777-aaaa-0000-0000-000000000005")
	uniboxReplyNotInt    = uuid.MustParse("77777777-aaaa-0000-0000-000000000006")
	uniboxReplyBounce    = uuid.MustParse("77777777-aaaa-0000-0000-000000000007")
	uniboxReplySeen      = uuid.MustParse("77777777-aaaa-0000-0000-000000000008")

	// CRM pipeline + stages (99999999-aaaa-...).
	pipelineSales = uuid.MustParse("99999999-aaaa-0000-0000-000000000001")
	stageNew      = uuid.MustParse("99999999-aaaa-0000-0000-000000000011")
	stageQual     = uuid.MustParse("99999999-aaaa-0000-0000-000000000012")
	stageDemo     = uuid.MustParse("99999999-aaaa-0000-0000-000000000013")
	stageWon      = uuid.MustParse("99999999-aaaa-0000-0000-000000000014")
	stageLost     = uuid.MustParse("99999999-aaaa-0000-0000-000000000015")

	// Deals (aaaaaaaa-aaaa-...).
	dealNorthwind  = uuid.MustParse("aaaaaaaa-aaaa-0000-0000-000000000001")
	dealInitech    = uuid.MustParse("aaaaaaaa-aaaa-0000-0000-000000000002")
	dealHooli      = uuid.MustParse("aaaaaaaa-aaaa-0000-0000-000000000003")
	dealBrightloop = uuid.MustParse("aaaaaaaa-aaaa-0000-0000-000000000004")
	dealWayne      = uuid.MustParse("aaaaaaaa-aaaa-0000-0000-000000000005")

	// CRM tasks (bbbbbbbb-aaaa-...).
	crmTaskFollowup = uuid.MustParse("bbbbbbbb-aaaa-0000-0000-000000000001")
	crmTaskProposal = uuid.MustParse("bbbbbbbb-aaaa-0000-0000-000000000002")
	crmTaskDemo     = uuid.MustParse("bbbbbbbb-aaaa-0000-0000-000000000003")

	// Contact notes (cccccccc-aaaa-...).
	noteNorthwind  = uuid.MustParse("cccccccc-aaaa-0000-0000-000000000001")
	noteHooli      = uuid.MustParse("cccccccc-aaaa-0000-0000-000000000002")
	noteBrightloop = uuid.MustParse("cccccccc-aaaa-0000-0000-000000000003")

	// Reply templates (dddddddd-aaaa-...).
	tplQuickYes = uuid.MustParse("dddddddd-aaaa-0000-0000-000000000001")
	tplBookCall = uuid.MustParse("dddddddd-aaaa-0000-0000-000000000002")
	tplPoliteNo = uuid.MustParse("dddddddd-aaaa-0000-0000-000000000003")

	// Notifications (eeeeeeee-aaaa-...).
	notifReply    = uuid.MustParse("eeeeeeee-aaaa-0000-0000-000000000001")
	notifCampaign = uuid.MustParse("eeeeeeee-aaaa-0000-0000-000000000002")
	notifWarmup   = uuid.MustParse("eeeeeeee-aaaa-0000-0000-000000000003")
	notifDeal     = uuid.MustParse("eeeeeeee-aaaa-0000-0000-000000000004")
	notifMeeting  = uuid.MustParse("eeeeeeee-aaaa-0000-0000-000000000005")
)

// launchContactID / agencyContactID resolve a campaign contact's deterministic
// UUID (same scheme seedCampaigns uses: base-<12-digit index+1>).
func launchContactID(i int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("66666666-aaaa-0000-0001-%012d", i+1))
}

func agencyContactID(i int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("66666666-aaaa-0000-0002-%012d", i+1))
}

// seedHistory populates the demo-only history layers on top of the campaigns
// and contacts already seeded: funnel progress, a live unified inbox, CRM
// pipeline/deals/tasks/notes, reply templates, notifications, and stats
// rollups. Everything is idempotent so `make sandbox-seed` is re-runnable.
func seedHistory(ctx context.Context, pool *pgxpool.Pool) error {
	if err := seedContactProgress(ctx, pool); err != nil {
		return err
	}
	if err := seedUniboxHistory(ctx, pool); err != nil {
		return err
	}
	if err := seedCRMHistory(ctx, pool); err != nil {
		return err
	}
	if err := seedReplyTemplates(ctx, pool); err != nil {
		return err
	}
	if err := seedNotifications(ctx, pool); err != nil {
		return err
	}
	if err := seedStatsRollups(ctx, pool); err != nil {
		return err
	}
	return nil
}

// launch sequence IDs, in send order.
var launchSteps = []uuid.UUID{
	uuid.MustParse("55555555-aaaa-0000-0000-000000000011"),
	uuid.MustParse("55555555-aaaa-0000-0000-000000000012"),
	uuid.MustParse("55555555-aaaa-0000-0000-000000000013"),
}

// agency sequence IDs, in send order.
var agencySteps = []uuid.UUID{
	uuid.MustParse("55555555-aaaa-0000-0000-000000000021"),
	uuid.MustParse("55555555-aaaa-0000-0000-000000000022"),
}

// progressStep is one campaign_contact_progress row with offsets (in days ago)
// for each timestamp; a negative offset means "unset".
type progressStep struct {
	seq                                        uuid.UUID
	sentDaysAgo, openedDaysAgo, clickedDaysAgo float64
	repliedDaysAgo, bouncedDaysAgo             float64
}

// funnelState is the deterministic bucket a contact lands in based on its index.
type funnelState int

const (
	stateQueued funnelState = iota
	stateDone
	stateProcessing
	stateReplied
	stateBounced
)

// classify buckets a contact index into a funnel state. Distribution roughly
// matches the demo target (~30% done, ~25% processing, ~15% replied, ~8%
// bounced, rest queued) using index modulo so it is stable across re-seeds.
func classify(i int) funnelState {
	switch i % 8 {
	case 0, 1, 2: // ~37% done
		return stateDone
	case 3, 4: // ~25% processing
		return stateProcessing
	case 5: // ~12% replied
		return stateReplied
	case 6: // ~12% bounced
		return stateBounced
	default: // case 7: queued
		return stateQueued
	}
}

// progressRows builds the concrete rows for a contact in a given campaign.
// Returns nil for queued contacts (no rows means "Queued" in the UI).
func progressRows(steps []uuid.UUID, state funnelState) []progressStep {
	switch state {
	case stateDone:
		// Every step sent, staggered; opened on all, clicked on the first.
		rows := make([]progressStep, 0, len(steps))
		for i, s := range steps {
			sent := 6 - float64(i*2) // -6d, -4d, -2d for 3 steps
			clicked := -1.0
			if i == 0 {
				clicked = sent - 0.1
			}
			rows = append(rows, progressStep{
				seq: s, sentDaysAgo: sent, openedDaysAgo: sent - 0.05,
				clickedDaysAgo: clicked, repliedDaysAgo: -1, bouncedDaysAgo: -1,
			})
		}
		return rows
	case stateProcessing:
		// Step 1 sent+opened, step 2 sent+opened when it exists, rest absent.
		rows := []progressStep{
			{seq: steps[0], sentDaysAgo: 4, openedDaysAgo: 3.9, clickedDaysAgo: -1, repliedDaysAgo: -1, bouncedDaysAgo: -1},
		}
		if len(steps) > 2 {
			rows = append(rows, progressStep{seq: steps[1], sentDaysAgo: 2, openedDaysAgo: 1.9, clickedDaysAgo: -1, repliedDaysAgo: -1, bouncedDaysAgo: -1})
		}
		return rows
	case stateReplied:
		// Step 1 sent+opened, replied on the latest sent step.
		if len(steps) > 2 {
			return []progressStep{
				{seq: steps[0], sentDaysAgo: 5, openedDaysAgo: 4.9, clickedDaysAgo: -1, repliedDaysAgo: -1, bouncedDaysAgo: -1},
				{seq: steps[1], sentDaysAgo: 3, openedDaysAgo: 2.9, clickedDaysAgo: -1, repliedDaysAgo: 2.5, bouncedDaysAgo: -1},
			}
		}
		return []progressStep{
			{seq: steps[0], sentDaysAgo: 3, openedDaysAgo: 2.9, clickedDaysAgo: -1, repliedDaysAgo: 2.5, bouncedDaysAgo: -1},
		}
	case stateBounced:
		// Step 1 sent + bounced, nothing else.
		return []progressStep{
			{seq: steps[0], sentDaysAgo: 4, openedDaysAgo: -1, clickedDaysAgo: -1, repliedDaysAgo: -1, bouncedDaysAgo: 3.9},
		}
	default:
		return nil
	}
}

// seedContactProgress writes a realistic funnel across both active campaigns.
// Unsubscribed contacts (Fiona Hale i=5, Nils Pett i=13) are left with no rows
// so they read as unsubscribed, not processed.
func seedContactProgress(ctx context.Context, pool *pgxpool.Pool) error {
	unsubscribedLaunch := map[int]bool{5: true, 13: true}

	for i := 0; i < len(launchContacts); i++ {
		if unsubscribedLaunch[i] {
			continue
		}
		for _, row := range progressRows(launchSteps, classify(i)) {
			if err := insertProgress(ctx, pool, campaignLaunch, launchContactID(i), row); err != nil {
				return err
			}
		}
	}

	for i := 0; i < len(agencyContacts); i++ {
		for _, row := range progressRows(agencySteps, classify(i)) {
			if err := insertProgress(ctx, pool, campaignAgency, agencyContactID(i), row); err != nil {
				return err
			}
		}
	}
	return nil
}

// dayExpr renders "NOW() - INTERVAL 'N days'" or NULL for a negative offset.
func dayExpr(daysAgo float64) string {
	if daysAgo < 0 {
		return "NULL"
	}
	return fmt.Sprintf("NOW() - INTERVAL '%f days'", daysAgo)
}

func insertProgress(ctx context.Context, pool *pgxpool.Pool, campaign, contact uuid.UUID, r progressStep) error {
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
		dayExpr(r.sentDaysAgo), dayExpr(r.openedDaysAgo), dayExpr(r.clickedDaysAgo),
		dayExpr(r.repliedDaysAgo), dayExpr(r.bouncedDaysAgo))
	if _, err := pool.Exec(ctx, sql, campaign, contact, r.seq); err != nil {
		return fmt.Errorf("progress %s/%s: %w", campaign, contact, err)
	}
	return nil
}

// uniboxRow is one inbound message for the sandbox user's unified inbox.
type uniboxRow struct {
	id          uuid.UUID
	emailID     uuid.UUID
	threadID    string
	messageID   string
	parentID    string
	uid         int
	flags       []string
	from        []string
	to          []string
	subject     string
	snippet     string
	seen        bool
	sentDaysAgo float64
}

// seedUniboxHistory seeds ~8 inbound messages across a few senders so the
// unified inbox and unread badge look alive. Upserts the parent mailbox rows
// first so the inbox tree has folders.
func seedUniboxHistory(ctx context.Context, pool *pgxpool.Pool) error {
	// Sarah Lin ...001, Marcus Reid ...002, Priya Nair ...003.
	mbSarah := sandboxMailboxes[0].id
	mbMarcus := sandboxMailboxes[1].id
	mbPriya := sandboxMailboxes[2].id

	mailboxes := []struct {
		emailID     uuid.UUID
		uidValidity int
	}{
		{mbSarah, 5001},
		{mbMarcus, 5002},
		{mbPriya, 5003},
	}
	for _, mb := range mailboxes {
		if _, err := pool.Exec(ctx, `
			INSERT INTO unibox_mailboxes (email_id, uid_validity, mailbox, attributes, highestmodseq, updated_at)
			VALUES ($1, $2, 'INBOX', '{"\\HasNoChildren"}', 1, NOW())
			ON CONFLICT (email_id, uid_validity) DO UPDATE SET
				mailbox = EXCLUDED.mailbox,
				attributes = EXCLUDED.attributes,
				highestmodseq = EXCLUDED.highestmodseq,
				updated_at = NOW()`,
			mb.emailID, mb.uidValidity); err != nil {
			return fmt.Errorf("unibox mailbox %s: %w", mb.emailID, err)
		}
	}

	rows := []uniboxRow{
		{
			id: uniboxReplyPositive1, emailID: mbSarah,
			threadID: "sbx-thread-northwind", messageID: "<sbx-northwind-reply@northwind.test>",
			uid: 601, flags: []string{}, from: []string{"Aiden Park <aiden.park@northwind.test>"},
			to: []string{"Sarah Lin <sarah.lin@sunrise.test>"}, subject: "Re: Quick question about Northwind",
			snippet:     "This is timely. We are re-evaluating our outbound stack this quarter, happy to take a look at the overview.",
			seen:        false,
			sentDaysAgo: 0.4,
		},
		{
			id: uniboxReplyPositive2, emailID: mbMarcus,
			threadID: "sbx-thread-hooli", messageID: "<sbx-hooli-reply@hooli.test>",
			uid: 602, flags: []string{}, from: []string{"Eli Grant <eli.grant@hooli.test>"},
			to: []string{"Marcus Reid <marcus.reid@sunrise.test>"}, subject: "Re: Quick question about Hooli",
			snippet:     "Interested. Can you send the deliverability benchmarks you mentioned? Our current provider has been rough.",
			seen:        false,
			sentDaysAgo: 0.9,
		},
		{
			id: uniboxReplyPositive3, emailID: mbPriya,
			threadID: "sbx-thread-brightloop", messageID: "<sbx-brightloop-reply@brightloop.test>",
			uid: 603, flags: []string{}, from: []string{"Amara Bell <amara.bell@brightloop.test>"},
			to: []string{"Priya Nair <priya.nair@sunrise.test>"}, subject: "Re: Partnering with Brightloop Agency",
			snippet:     "The white-label angle is compelling. Let's set up a call to talk margins and onboarding.",
			seen:        false,
			sentDaysAgo: 1.2,
		},
		{
			id: uniboxReplyOOO, emailID: mbSarah,
			threadID: "sbx-thread-globex-ooo", messageID: "<sbx-globex-ooo@globex.test>",
			uid: 604, flags: []string{"\\Seen"}, from: []string{"Diana Fox <diana.fox@globex.test>"},
			to: []string{"Sarah Lin <sarah.lin@sunrise.test>"}, subject: "Automatic reply: Quick question about Globex",
			snippet:     "I am out of office until next Monday with limited email access. For anything urgent contact our ops alias.",
			seen:        true,
			sentDaysAgo: 2.1,
		},
		{
			id: uniboxReplyMeeting, emailID: mbMarcus,
			threadID: "sbx-thread-wayne-meeting", messageID: "<sbx-wayne-meeting@wayne.test>",
			uid: 605, flags: []string{}, from: []string{"Hana Jules <hana.jules@wayne.test>"},
			to: []string{"Marcus Reid <marcus.reid@sunrise.test>"}, subject: "Re: Quick question about Wayne Enterprises",
			snippet:     "Thursday at 2pm works for a demo. Send the invite and I'll loop in our ops lead.",
			seen:        false,
			sentDaysAgo: 1.6,
		},
		{
			id: uniboxReplyNotInt, emailID: mbSarah,
			threadID: "sbx-thread-acme-notinterested", messageID: "<sbx-acme-notint@acme-corp.test>",
			uid: 606, flags: []string{"\\Seen"}, from: []string{"Kofi Mensah <kofi.mensah@acme-corp.test>"},
			to: []string{"Sarah Lin <sarah.lin@sunrise.test>"}, subject: "Re: Quick question about Acme Corp",
			snippet:     "Not a fit for us right now, please remove me from this list. Thanks.",
			seen:        true,
			sentDaysAgo: 2.6,
		},
		{
			id: uniboxReplyBounce, emailID: mbMarcus,
			threadID: "sbx-thread-tyrell-bounce", messageID: "<sbx-tyrell-bounce@mailer-daemon.test>",
			uid: 607, flags: []string{}, from: []string{"Mail Delivery Subsystem <mailer-daemon@tyrell.test>"},
			to: []string{"Marcus Reid <marcus.reid@sunrise.test>"}, subject: "Delivery Status Notification (Failure)",
			snippet:     "The message to ivan.kova@tyrell.test could not be delivered. The recipient address was rejected by the server.",
			seen:        false,
			sentDaysAgo: 3.0,
		},
		{
			id: uniboxReplySeen, emailID: mbPriya,
			threadID: "sbx-thread-funnelworks", messageID: "<sbx-funnelworks-reply@funnelworks.test>",
			uid: 608, flags: []string{"\\Seen"}, from: []string{"Boris Chan <boris.chan@funnelworks.test>"},
			to: []string{"Priya Nair <priya.nair@sunrise.test>"}, subject: "Re: Partnering with Funnelworks",
			snippet:     "Thanks for the details, reviewed the partner deck. Circling back internally and will follow up next week.",
			seen:        true,
			sentDaysAgo: 4.2,
		},
	}

	for _, r := range rows {
		if err := insertUnibox(ctx, pool, r); err != nil {
			return err
		}
	}
	return nil
}

func insertUnibox(ctx context.Context, pool *pgxpool.Pool, r uniboxRow) error {
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
		dayExpr(r.sentDaysAgo), dayExpr(r.sentDaysAgo))
	if _, err := pool.Exec(ctx, sql,
		r.id, sandboxUser, r.emailID, r.threadID, r.messageID,
		r.parentID, r.uid, r.flags, r.from, r.to, r.subject, int64(len(r.snippet)),
		r.snippet, r.seen); err != nil {
		return fmt.Errorf("unibox email %s: %w", r.id, err)
	}
	return nil
}

// seedCRMHistory seeds one pipeline with five stages, five deals across the
// stages, three tasks, and three contact notes for the sandbox org.
func seedCRMHistory(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `
		INSERT INTO pipelines (id, organization_id, name, position, created_at, updated_at)
		VALUES ($1, $2, 'Sales pipeline', 0, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, updated_at = NOW()`,
		pipelineSales, sandboxOrg); err != nil {
		return fmt.Errorf("pipeline: %w", err)
	}

	stages := []struct {
		id    uuid.UUID
		name  string
		color string
		pos   int
	}{
		{stageNew, "New", "#3b82f6", 0},
		{stageQual, "Qualified", "#a855f7", 1},
		{stageDemo, "Demo booked", "#f59e0b", 2},
		{stageWon, "Won", "#10b981", 3},
		{stageLost, "Lost", "#ef4444", 4},
	}
	for _, s := range stages {
		if _, err := pool.Exec(ctx, `
			INSERT INTO pipeline_stages (id, pipeline_id, name, color, position, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name, color = EXCLUDED.color, position = EXCLUDED.position, updated_at = NOW()`,
			s.id, pipelineSales, s.name, s.color, s.pos); err != nil {
			return fmt.Errorf("stage %s: %w", s.name, err)
		}
	}

	deals := []struct {
		id      uuid.UUID
		stage   uuid.UUID
		name    string
		value   float64
		status  string
		won     bool
		contact uuid.UUID
	}{
		{dealNorthwind, stageDemo, "Northwind - team-wide rollout", 24_000, "open", false, launchContactID(0)},
		{dealInitech, stageWon, "Initech - pilot extension", 8_000, "won", true, launchContactID(1)},
		{dealHooli, stageQual, "Hooli - deliverability pilot", 15_000, "open", false, launchContactID(4)},
		{dealBrightloop, stageNew, "Brightloop - white-label partnership", 30_000, "open", false, agencyContactID(0)},
		{dealWayne, stageDemo, "Wayne Enterprises - evaluation", 4_000, "open", false, launchContactID(7)},
	}
	for _, d := range deals {
		wonAt := "NULL"
		if d.won {
			wonAt = "NOW() - INTERVAL '5 days'"
		}
		sql := `
			INSERT INTO deals (id, organization_id, pipeline_id, stage_id, contact_id, name, value, currency, status, won_at, assigned_to, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'USD', $8, ` + wonAt + `, $9, NOW(), NOW())
			ON CONFLICT (id) DO UPDATE SET stage_id = EXCLUDED.stage_id, status = EXCLUDED.status, updated_at = NOW()`
		if _, err := pool.Exec(ctx, sql,
			d.id, sandboxOrg, pipelineSales, d.stage, d.contact, d.name, d.value, d.status, sandboxUser); err != nil {
			return fmt.Errorf("deal %s: %w", d.name, err)
		}
	}

	tasks := []struct {
		id           uuid.UUID
		title        string
		description  string
		priority     string
		status       string
		dueDaysHence int
		contact      uuid.UUID
		deal         uuid.UUID
	}{
		{crmTaskFollowup, "Follow up with Aiden Park", "He replied positive, nudge for a call slot.", "high", "in_progress", 1, launchContactID(0), dealNorthwind},
		{crmTaskProposal, "Send proposal to Hooli", "Use the deliverability pilot template.", "medium", "pending", 3, launchContactID(4), dealHooli},
		{crmTaskDemo, "Schedule demo with Wayne Enterprises", "Thursday 2pm confirmed, send the invite.", "high", "pending", 2, launchContactID(7), dealWayne},
	}
	for _, t := range tasks {
		sql := fmt.Sprintf(`
			INSERT INTO crm_tasks (id, organization_id, contact_id, deal_id, assigned_to, created_by, title, description, due_date, priority, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $5, $6, $7, NOW() + INTERVAL '%d days', $8, $9, NOW(), NOW())
			ON CONFLICT (id) DO UPDATE SET title = EXCLUDED.title, status = EXCLUDED.status, updated_at = NOW()`, t.dueDaysHence)
		if _, err := pool.Exec(ctx, sql,
			t.id, sandboxOrg, t.contact, t.deal, sandboxUser, t.title, t.description, t.priority, t.status); err != nil {
			return fmt.Errorf("crm task %s: %w", t.title, err)
		}
	}

	notes := []struct {
		id      uuid.UUID
		contact uuid.UUID
		content string
	}{
		{noteNorthwind, launchContactID(0), "Prefers async email over calls. Re-evaluating outbound stack this quarter."},
		{noteHooli, launchContactID(4), "Frustrated with current provider deliverability. Strong intent, move fast."},
		{noteBrightloop, agencyContactID(0), "Agency lead, cares most about white-label margins and onboarding time."},
	}
	for _, n := range notes {
		if _, err := pool.Exec(ctx, `
			INSERT INTO contact_notes (id, contact_id, organization_id, user_id, content, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
			ON CONFLICT (id) DO NOTHING`,
			n.id, n.contact, sandboxOrg, sandboxUser, n.content); err != nil {
			return fmt.Errorf("contact note %s: %w", n.id, err)
		}
	}
	return nil
}

// seedReplyTemplates gives the sandbox user three canned inbox replies.
func seedReplyTemplates(ctx context.Context, pool *pgxpool.Pool) error {
	tpls := []struct {
		id      uuid.UUID
		name    string
		subject string
		html    string
		plain   string
		pos     int
	}{
		{tplQuickYes, "Quick yes", "Re: {{originalSubject}}",
			"<p>Sounds great, sending a calendar invite now. Talk soon.</p>",
			"Sounds great, sending a calendar invite now. Talk soon.", 0},
		{tplBookCall, "Book a call", "Re: {{originalSubject}}",
			"<p>Happy to walk you through it. Here is my calendar: https://warmbly.com/book</p>",
			"Happy to walk you through it. Here is my calendar: https://warmbly.com/book", 1},
		{tplPoliteNo, "Polite no", "Re: {{originalSubject}}",
			"<p>Thanks for the note. Not a fit right now, I'll remove you from this list.</p>",
			"Thanks for the note. Not a fit right now, I'll remove you from this list.", 2},
	}
	for _, t := range tpls {
		if _, err := pool.Exec(ctx, `
			INSERT INTO reply_templates (id, organization_id, user_id, name, subject, body_html, body_plain, position, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				subject = EXCLUDED.subject,
				body_html = EXCLUDED.body_html,
				body_plain = EXCLUDED.body_plain,
				position = EXCLUDED.position,
				updated_at = NOW()`,
			t.id, sandboxOrg, sandboxUser, t.name, t.subject, t.html, t.plain, t.pos); err != nil {
			return fmt.Errorf("reply template %s: %w", t.name, err)
		}
	}
	return nil
}

// seedNotifications seeds five notifications for the sandbox user, three unread.
func seedNotifications(ctx context.Context, pool *pgxpool.Pool) error {
	notifs := []struct {
		id         uuid.UUID
		category   string
		title      string
		body       string
		link       string
		read       bool
		agoDaysAgo float64
	}{
		{notifReply, "reply_received", "Aiden Park replied", "New reply on the Sunrise Q3 launch campaign.", "/app/inbox", false, 0.4},
		{notifMeeting, "reply_received", "Meeting request from Wayne Enterprises", "Hana Jules proposed Thursday at 2pm.", "/app/inbox", false, 1.6},
		{notifDeal, "deal_created", "New deal: Brightloop", "A $30,000 deal was added to the Sales pipeline.", "/app/crm", false, 1.2},
		{notifCampaign, "campaign_started", "Agency partnerships is live", "Your Agency partnerships campaign started sending.", "/app/campaigns", true, 6.0},
		{notifWarmup, "warmup_milestone", "Warmup ramping nicely", "Sarah Lin reached 30 warmup emails per day.", "/app/mailboxes", true, 2.0},
	}
	for _, n := range notifs {
		readExpr := "NULL"
		if n.read {
			readExpr = "NOW() - INTERVAL '1 days'"
		}
		sql := fmt.Sprintf(`
			INSERT INTO notifications (id, user_id, organization_id, category, title, body, link, metadata, read_at, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, '{}'::jsonb, %s, NOW() - INTERVAL '%f days')
			ON CONFLICT (id) DO NOTHING`, readExpr, n.agoDaysAgo)
		if _, err := pool.Exec(ctx, sql,
			n.id, sandboxUser, sandboxOrg, n.category, n.title, n.body, n.link); err != nil {
			return fmt.Errorf("notification %s: %w", n.title, err)
		}
	}
	return nil
}

// seedStatsRollups backfills the daily aggregates the dashboard charts read
// from: campaign send history (14d), per-mailbox warmup stats (10d), and the
// per-mailbox daily email counts (14d). A generate_series INSERT keeps it to
// one statement per table and matches Postgres-native seeding.
func seedStatsRollups(ctx context.Context, pool *pgxpool.Pool) error {
	// Campaign daily sends: last 14 days for both active campaigns. Deterministic
	// but varied per day via the day offset.
	for _, cid := range []uuid.UUID{campaignLaunch, campaignAgency} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO campaign_daily_sends (campaign_id, send_date, emails_sent, new_leads_started)
			SELECT $1, d::date,
				5 + ((EXTRACT(DAY FROM d)::int * 7) % 20),
				1 + ((EXTRACT(DAY FROM d)::int * 3) % 4)
			FROM generate_series(NOW() - INTERVAL '13 days', NOW(), INTERVAL '1 day') AS d
			ON CONFLICT (campaign_id, send_date) DO UPDATE SET
				emails_sent = EXCLUDED.emails_sent,
				new_leads_started = EXCLUDED.new_leads_started`,
			cid); err != nil {
			return fmt.Errorf("campaign_daily_sends %s: %w", cid, err)
		}
	}

	for _, m := range sandboxMailboxes {
		// Warmup stats: last 10 days, volume ramping 10 -> ~30 (2/day), target 40.
		if _, err := pool.Exec(ctx, `
			INSERT INTO warmup_statistics (email_account_id, date, emails_sent, emails_replied, target_volume)
			SELECT $1, d::date,
				LEAST(30, 10 + (9 - g.n) * 2),
				GREATEST(0, ((9 - g.n) / 2)),
				40
			FROM generate_series(0, 9) AS g(n),
				LATERAL (SELECT NOW() - (g.n || ' days')::interval AS d) s
			ON CONFLICT (email_account_id, date) DO UPDATE SET
				emails_sent = EXCLUDED.emails_sent,
				emails_replied = EXCLUDED.emails_replied,
				target_volume = EXCLUDED.target_volume`,
			m.id); err != nil {
			return fmt.Errorf("warmup_statistics %s: %w", m.email, err)
		}

		// Daily email counts: last 14 days, small counts.
		if _, err := pool.Exec(ctx, `
			INSERT INTO daily_email_counts (email_account_id, date, count)
			SELECT $1, d::date, 3 + ((EXTRACT(DAY FROM d)::int * 5) % 12)
			FROM generate_series(NOW() - INTERVAL '13 days', NOW(), INTERVAL '1 day') AS d
			ON CONFLICT (email_account_id, date) DO UPDATE SET count = EXCLUDED.count`,
			m.id); err != nil {
			return fmt.Errorf("daily_email_counts %s: %w", m.email, err)
		}
	}
	return nil
}
