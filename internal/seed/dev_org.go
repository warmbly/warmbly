package seed

// Dev-org enrichment. seedBaseline (cmd/seed) creates dev@warmbly.com with a
// nearly-empty org; SeedDevOrg turns that org into a mid-flight workspace:
// four warmed mailboxes, labels with real bindings, ~30 contacts, an actively
// sending campaign with funnel history, CRM, templates, credits, and
// notifications. Everything is deterministic-UUID + ON CONFLICT so re-running
// `make seed` never duplicates rows.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Identity anchors shared with cmd/seed's baseline (main.go references these).
var (
	DevUserID   = uuid.MustParse("11111111-0000-0000-0000-000000000001")
	DevOrgID    = uuid.MustParse("22222222-0000-0000-0000-000000000001")
	DevWorkerID = uuid.MustParse("10c8f5e4-1c39-5b2a-9c8b-3d2f0a8b1a01")

	DevMailboxSendID     = uuid.MustParse("33333333-0000-0000-0000-0000dddd0001")
	DevMailboxOutboundID = uuid.MustParse("33333333-0000-0000-0000-0000dddd0002")
	DevMailboxGrowthID   = uuid.MustParse("33333333-0000-0000-0000-0000dddd0003")
	DevMailboxPartnersID = uuid.MustParse("33333333-0000-0000-0000-0000dddd0004")
)

// Dev-org entity IDs ("dddd" prefix, one hex block per domain).
var (
	devFolderOutbound = uuid.MustParse("dddd0a00-0000-0000-0000-000000000001")
	devFolderNurture  = uuid.MustParse("dddd0a00-0000-0000-0000-000000000002")

	devTagVIP    = uuid.MustParse("dddd0a00-0000-0000-0000-000000000011")
	devTagCold   = uuid.MustParse("dddd0a00-0000-0000-0000-000000000012")
	devTagAgency = uuid.MustParse("dddd0a00-0000-0000-0000-000000000013")

	devCategoryLead     = uuid.MustParse("dddd0a00-0000-0000-0000-000000000021")
	devCategoryCustomer = uuid.MustParse("dddd0a00-0000-0000-0000-000000000022")
	devCategoryChurn    = uuid.MustParse("dddd0a00-0000-0000-0000-000000000023")

	DevCampaignActiveID = uuid.MustParse("dddd0d00-0000-0000-0000-000000000001")
	DevCampaignDraftID  = uuid.MustParse("dddd0d00-0000-0000-0000-000000000002")

	devSeqStep1 = uuid.MustParse("dddd0e00-0000-0000-0000-000000000001")
	devSeqStep2 = uuid.MustParse("dddd0e00-0000-0000-0000-000000000002")
	devSeqStep3 = uuid.MustParse("dddd0e00-0000-0000-0000-000000000003")
	devSeqDraft = uuid.MustParse("dddd0e00-0000-0000-0000-000000000004")

	devPipelineID = uuid.MustParse("dddd0b00-0000-0000-0000-000000000001")
	devStageNew   = uuid.MustParse("dddd0b00-0000-0000-0000-000000000011")
	devStageQual  = uuid.MustParse("dddd0b00-0000-0000-0000-000000000012")
	devStageDemo  = uuid.MustParse("dddd0b00-0000-0000-0000-000000000013")
	devStageWon   = uuid.MustParse("dddd0b00-0000-0000-0000-000000000014")
	devStageLost  = uuid.MustParse("dddd0b00-0000-0000-0000-000000000015")

	devDealLumina      = uuid.MustParse("dddd0b00-0000-0000-0000-000000000021")
	devDealCinderworks = uuid.MustParse("dddd0b00-0000-0000-0000-000000000022")
	devDealEmberly     = uuid.MustParse("dddd0b00-0000-0000-0000-000000000023")
	devDealDriftline   = uuid.MustParse("dddd0b00-0000-0000-0000-000000000024")

	devTaskCall    = uuid.MustParse("dddd0b00-0000-0000-0000-000000000031")
	devTaskEmail   = uuid.MustParse("dddd0b00-0000-0000-0000-000000000032")
	devTaskMeeting = uuid.MustParse("dddd0b00-0000-0000-0000-000000000033")

	devNoteMira  = uuid.MustParse("dddd0b00-0000-0000-0000-000000000041")
	devNoteNadia = uuid.MustParse("dddd0b00-0000-0000-0000-000000000042")
	devNoteChloe = uuid.MustParse("dddd0b00-0000-0000-0000-000000000043")

	devTplYes  = uuid.MustParse("dddd0c00-0000-0000-0000-000000000001")
	devTplCall = uuid.MustParse("dddd0c00-0000-0000-0000-000000000002")
	devTplNo   = uuid.MustParse("dddd0c00-0000-0000-0000-000000000003")
)

// devContactID maps a 0-based dev contact index to its stable UUID, continuing
// the 66666666-...-0000dddc00NN block seedBaseline starts at 0001..0003.
func devContactID(i int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("66666666-0000-0000-0000-0000dddc%04d", i+4))
}

// devEntityID derives IDs for repeated rows (activities, logs, txns, unibox).
func devEntityID(block string, n int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("dddd%s00-0000-0000-0000-%012d", block, n))
}

// SeedDevOrg enriches the baseline dev org. Runs on every `make seed` /
// `make dev`, after seedBaseline has created the user, org, and worker.
func SeedDevOrg(ctx context.Context, pool *pgxpool.Pool) error {
	steps := []struct {
		name string
		fn   func(context.Context, *pgxpool.Pool) error
	}{
		{"mailboxes", seedDevMailboxes},
		{"labels", seedDevLabels},
		{"contacts", seedDevContacts},
		{"campaigns", seedDevCampaigns},
		{"suppression", seedDevSuppression},
		{"label-bindings", seedDevLabelBindings},
		{"reply-templates", seedDevReplyTemplates},
		{"crm", seedDevCRM},
		{"credits", seedDevCredits},
		{"notifications", seedDevNotifications},
		{"progress", seedDevProgress},
		{"stats", seedDevStats},
		{"unibox", seedDevUnibox},
		{"campaign-logs", seedDevCampaignLogs},
	}
	for _, s := range steps {
		if err := s.fn(ctx, pool); err != nil {
			return fmt.Errorf("dev %s: %w", s.name, err)
		}
	}
	return nil
}

// seedDevMailboxes upserts all four dev mailboxes with the full sending +
// warmup config (baseline inserts the first two minimally; DO UPDATE heals
// them), attaches SMTP/IMAP credential rows, and joins the premium pool.
func seedDevMailboxes(ctx context.Context, pool *pgxpool.Pool) error {
	boxes := []struct {
		id    uuid.UUID
		email string
		name  string
		tag   string
	}{
		{DevMailboxSendID, "dev.send@warmbly.test", "Dev Sender", "dev-warmup-a"},
		{DevMailboxOutboundID, "dev.outbound@warmbly.test", "Dev Outbound", "dev-warmup-b"},
		{DevMailboxGrowthID, "dev.growth@warmbly.test", "Dev Growth", "dev-warmup-c"},
		{DevMailboxPartnersID, "dev.partners@warmbly.test", "Dev Partners", "dev-warmup-d"},
	}
	for _, b := range boxes {
		if _, err := pool.Exec(ctx, `
			INSERT INTO email_accounts (
				id, user_id, organization_id, worker_id, email, name,
				signature_plain, signature_html, signature_sync, signature_code,
				provider, status, campaign_limit, min_wait_time, reply_to,
				tracking_domain, timezone,
				warmup, warmup_base, warmup_max, warmup_increase, warmup_reply_rate,
				warmup_tag, warmup_start_time, warmup_end_time, warmup_days,
				warmup_pool_type,
				created_at, updated_at
			) VALUES (
				$1,$2,$3,$4,$5,$6,
				'-- Dev --', '<p>-- Dev --</p>', TRUE, FALSE,
				'smtp_imap', 'active', 50, 600, '',
				'localhost:3000', 'UTC',
				NOW() - INTERVAL '21 days', 10, 40, 1, 30,
				$7, '08:00', '20:00', 62,
				'premium',
				NOW() - INTERVAL '21 days', NOW()
			)
			ON CONFLICT (id) DO UPDATE SET
				user_id = EXCLUDED.user_id,
				organization_id = EXCLUDED.organization_id,
				worker_id = EXCLUDED.worker_id,
				name = EXCLUDED.name,
				status = 'active',
				warmup = EXCLUDED.warmup,
				warmup_base = EXCLUDED.warmup_base,
				warmup_max = EXCLUDED.warmup_max,
				warmup_increase = EXCLUDED.warmup_increase,
				warmup_tag = EXCLUDED.warmup_tag,
				warmup_pool_type = EXCLUDED.warmup_pool_type,
				updated_at = NOW()
		`, b.id, DevUserID, DevOrgID, DevWorkerID, b.email, b.name, b.tag); err != nil {
			return fmt.Errorf("mailbox %s: %w", b.email, err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO email_accounts_smtp_imap (
				email_account_id, smtp_host, smtp_port, smtp_user, smtp_password,
				imap_host, imap_port, imap_user, imap_password, updated_at
			) VALUES ($1, 'smtp.test.local', 587, $2, 'seed-fake-smtp-password',
				'imap.test.local', 993, $2, 'seed-fake-imap-password', NOW())
			ON CONFLICT (email_account_id) DO UPDATE SET
				smtp_host = EXCLUDED.smtp_host,
				smtp_user = EXCLUDED.smtp_user,
				imap_host = EXCLUDED.imap_host,
				updated_at = NOW()
		`, b.id, b.email); err != nil {
			return fmt.Errorf("smtp creds %s: %w", b.email, err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO warmup_pool_participants (pool_id, email_account_id)
			SELECT id, $1 FROM warmup_pools WHERE pool_type = 'premium'::warmup_pool_type
			ON CONFLICT DO NOTHING
		`, b.id); err != nil {
			return fmt.Errorf("pool join %s: %w", b.email, err)
		}
	}
	return nil
}

// seedDevLabels creates the dev user's folders, tags, and categories.
func seedDevLabels(ctx context.Context, pool *pgxpool.Pool) error {
	entries := []struct {
		table string
		id    uuid.UUID
		title string
		color string
		pos   int
	}{
		{"folders", devFolderOutbound, "Outbound", "#0ea5e9", 0},
		{"folders", devFolderNurture, "Nurture", "#10b981", 1},
		{"tags", devTagVIP, "VIP", "#a855f7", 0},
		{"tags", devTagCold, "Cold", "#64748b", 1},
		{"tags", devTagAgency, "Agency", "#f59e0b", 2},
		{"categories", devCategoryLead, "Lead", "#f97316", 0},
		{"categories", devCategoryCustomer, "Customer", "#10b981", 1},
		{"categories", devCategoryChurn, "Churn risk", "#ef4444", 2},
	}
	for _, e := range entries {
		if _, err := pool.Exec(ctx, `
			INSERT INTO `+e.table+` (id, user_id, title, color, position, created_at, updated_at)
			VALUES ($1,$2,$3,$4,$5,NOW(),NOW())
			ON CONFLICT (id) DO UPDATE SET
				title = EXCLUDED.title,
				color = EXCLUDED.color,
				position = EXCLUDED.position,
				updated_at = NOW()
		`, e.id, DevUserID, e.title, e.color, e.pos); err != nil {
			return fmt.Errorf("%s %s: %w", e.table, e.title, err)
		}
	}
	return nil
}

type devContact struct {
	first, last, company, title string
	subscribed                  bool
}

// devContacts is the fixed roster. Index order matters: devClassify(i) buckets
// leads 0..23 into the funnel, so personas are placed to line up with their
// unibox threads (Mira replied at 5, Omar bounced at 6, Nadia replied at 13,
// Jonas replied at 21; Talia/Chloe sit in the processing cohort).
var devContacts = []devContact{
	{"Priya", "Shah", "Brightpath", "Head of Growth", true},
	{"Daniel", "Cho", "Vellum Works", "VP Sales", true},
	{"Sofia", "Marino", "Quartzline", "RevOps Lead", true},
	{"Tomas", "Silva", "Harborview", "Sales Manager", true},
	{"Lena", "Fischer", "Copperfield", "Demand Gen Lead", true},
	{"Mira", "Kovacs", "Lumina Labs", "COO", true},
	{"Omar", "Said", "Bluepeak", "Founder", true},
	{"Jack", "Whitman", "Stonebridge", "CEO", true},
	{"Ruben", "Ortiz", "Driftline", "Head of Sales", true},
	{"Emily", "Novak", "Kestrel Data", "GTM Lead", true},
	{"Felix", "Braun", "Tidewater", "VP Marketing", true},
	{"Talia", "Reyes", "Arcadia Metrics", "Ops Director", true},
	{"Chloe", "Bennett", "Emberly", "Head of Partnerships", true},
	{"Nadia", "Osei", "Cinderworks", "Sales Director", true},
	{"Marco", "Ricci", "Foglight", "Founder", true},
	{"Hannah", "Berg", "Saltgrass", "CRO", true},
	{"Leo", "Martins", "Pinemount", "Growth Manager", true},
	{"Ava", "Lindqvist", "Clearharbor", "VP Sales", true},
	{"Noah", "Byrne", "Riverstone Labs", "RevOps Manager", true},
	{"Isla", "Kerr", "Waypoint CRM", "Sales Lead", true},
	{"Ethan", "Cole", "Larkspur", "Head of Outbound", true},
	{"Jonas", "Weber", "Fieldstone", "IT Director", true},
	{"Greta", "Lund", "Mosswood", "Founder", true},
	{"Sam", "Okafor", "Highbeam", "CEO", true},
	{"Wes", "Tucker", "Oakline", "Ops Manager", false},
	{"Ingrid", "Hall", "Ferrow", "Marketing Lead", false},
	{"Victor", "Anand", "Skylark Systems", "CTO", true},
}

// devLeadCount is how many roster contacts join the active campaign.
const devLeadCount = 24

func devContactEmail(c devContact) string {
	return fmt.Sprintf("%s.%s@%s.test", lower(c.first), lower(c.last), lower(c.company))
}

// seedDevContacts inserts the roster with titles in custom_fields.
func seedDevContacts(ctx context.Context, pool *pgxpool.Pool) error {
	for i, c := range devContacts {
		custom, _ := json.Marshal(map[string]any{"title": c.title})
		if _, err := pool.Exec(ctx, `
			INSERT INTO contacts (id, user_id, organization_id, first_name, last_name, email, company, phone, custom_fields, subscribed, updated_at, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'',$8,$9,NOW(),NOW() - INTERVAL '15 days')
			ON CONFLICT (id) DO UPDATE SET
				first_name = EXCLUDED.first_name,
				last_name = EXCLUDED.last_name,
				email = EXCLUDED.email,
				company = EXCLUDED.company,
				custom_fields = EXCLUDED.custom_fields,
				subscribed = EXCLUDED.subscribed,
				updated_at = NOW()
		`, devContactID(i), DevUserID, DevOrgID, c.first, c.last, devContactEmail(c), c.company, custom, c.subscribed); err != nil {
			return fmt.Errorf("contact %s: %w", devContactEmail(c), err)
		}
	}
	return nil
}

// seedDevSuppression runs after campaigns exist (suppressed_recipients has an
// FK on campaign_id) and makes the unsubscribed/bounced states visible.
func seedDevSuppression(ctx context.Context, pool *pgxpool.Pool) error {
	suppressed := []struct {
		i      int
		source string
		reason string
	}{
		{24, "unsubscribe", "Clicked the unsubscribe link"},
		{25, "complaint", "Marked a campaign email as spam"},
		{6, "bounce", "Hard bounce: recipient address rejected"},
	}
	for _, s := range suppressed {
		if _, err := pool.Exec(ctx, `
			INSERT INTO suppressed_recipients (organization_id, email, reason, source, campaign_id, expires_at, metadata, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, NULL, '{}'::jsonb, NOW() - INTERVAL '5 days', NOW())
			ON CONFLICT (organization_id, email) DO UPDATE SET
				reason = EXCLUDED.reason,
				source = EXCLUDED.source,
				updated_at = NOW()
		`, DevOrgID, devContactEmail(devContacts[s.i]), s.reason, s.source, DevCampaignActiveID); err != nil {
			return fmt.Errorf("suppressed %d: %w", s.i, err)
		}
	}
	return nil
}

// seedDevCampaigns creates the mid-flight active campaign (3-step sequence,
// 24 leads) and a draft, both owned by the dev user.
func seedDevCampaigns(ctx context.Context, pool *pgxpool.Pool) error {
	campaigns := []struct {
		id      uuid.UUID
		name    string
		desc    string
		status  string
		ageDays int
	}{
		{DevCampaignActiveID, "RevOps outreach - July", "Cold outreach to RevOps and growth leaders at B2B SaaS companies.", "active", 14},
		{DevCampaignDraftID, "Agency partnerships", "White-label pitch for outbound agencies. Not launched yet.", "draft", 2},
	}
	for _, c := range campaigns {
		if _, err := pool.Exec(ctx, `
			INSERT INTO campaigns (
				id, user_id, organization_id, name, description, status,
				stop_on_reply, open_tracking, link_tracking, text_only,
				daily_limit, unsubscribe_header, risky_emails,
				cc_addr, bcc_addr, timezone, days, start_time, end_time,
				updated_at, created_at
			) VALUES (
				$1,$2,$3,$4,$5,$6,
				TRUE,TRUE,TRUE,FALSE,
				50,TRUE,FALSE,
				'{}','{}','UTC',$7,'08:00','18:00',
				NOW(), NOW() - make_interval(days => $8)
			)
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				description = EXCLUDED.description,
				status = EXCLUDED.status,
				updated_at = NOW()
		`, c.id, DevUserID, DevOrgID, c.name, c.desc, c.status, monFri, c.ageDays); err != nil {
			return fmt.Errorf("campaign %s: %w", c.name, err)
		}
	}

	steps := []struct {
		id        uuid.UUID
		campaign  uuid.UUID
		name      string
		subject   string
		bodyPlain string
		waitAfter int
	}{
		{devSeqStep1, DevCampaignActiveID, "Step 1 - intro",
			"Quick question about outbound at {{company}}",
			"Hi {{firstName}},\n\nSaw {{company}} is scaling its sales team. Most teams we talk to run cold outbound across a handful of mailboxes and only find out something landed in spam when replies dry up.\n\nWe keep every mailbox warm, spread sends across senders, and flag deliverability dips before they hurt. Teams like yours usually see reply rates climb within two weeks.\n\nWorth a quick look? Happy to share a two-minute walkthrough.\n\nBest,\nDev", 0},
		{devSeqStep2, DevCampaignActiveID, "Step 2 - bump",
			"Re: Quick question about outbound at {{company}}",
			"Hi {{firstName}},\n\nBumping this in case it got buried. The short version: we help outbound teams stay out of spam without slowing down sending.\n\nIf deliverability is not a problem for {{company}} right now, no worries at all. If it is, I can show you exactly where your sends are landing.\n\nBest,\nDev", 3},
		{devSeqStep3, DevCampaignActiveID, "Step 3 - break-up",
			"Closing the loop",
			"Hi {{firstName}},\n\nClosing the loop on my side. If inbox placement becomes a priority later this quarter, just reply to this thread and I will pick it right back up.\n\nEither way, good luck with the outbound push at {{company}}.\n\nBest,\nDev", 4},
		{devSeqDraft, DevCampaignDraftID, "Partner intro",
			"White-label outbound for {{company}} clients",
			"Hi {{firstName}},\n\nAgencies use us to run warmed, monitored sending for their clients under their own brand. Would a partner walkthrough be useful?\n\nBest,\nDev", 0},
	}
	for _, s := range steps {
		bodyHTML := "<p>" + s.bodyPlain + "</p>"
		if _, err := pool.Exec(ctx, `
			INSERT INTO sequences (
				id, campaign_id, organization_id, name, subject,
				body_plain, body_html, body_sync, body_code, wait_after,
				updated_at, created_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,TRUE,FALSE,$8,NOW(),NOW())
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				subject = EXCLUDED.subject,
				body_plain = EXCLUDED.body_plain,
				body_html = EXCLUDED.body_html,
				wait_after = EXCLUDED.wait_after,
				updated_at = NOW()
		`, s.id, s.campaign, DevOrgID, s.name, s.subject, s.bodyPlain, bodyHTML, s.waitAfter); err != nil {
			return fmt.Errorf("sequence %s: %w", s.name, err)
		}
	}

	for i := 0; i < devLeadCount; i++ {
		if _, err := pool.Exec(ctx, `
			INSERT INTO campaign_leads (campaign_id, contact_id)
			VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, DevCampaignActiveID, devContactID(i)); err != nil {
			return fmt.Errorf("lead %d: %w", i, err)
		}
	}
	return nil
}

// seedDevLabelBindings attaches the labels to real records: tags on mailboxes,
// folders on campaigns, categories on contacts, and the sender-selection tag
// binding for the active campaign.
func seedDevLabelBindings(ctx context.Context, pool *pgxpool.Pool) error {
	emailTags := []struct{ email, tag uuid.UUID }{
		{DevMailboxSendID, devTagCold},
		{DevMailboxSendID, devTagVIP},
		{DevMailboxOutboundID, devTagCold},
		{DevMailboxGrowthID, devTagAgency},
		{DevMailboxPartnersID, devTagAgency},
	}
	for _, b := range emailTags {
		if _, err := pool.Exec(ctx, `
			INSERT INTO email_tags (email_id, tag_id) VALUES ($1, $2)
			ON CONFLICT (email_id, tag_id) DO NOTHING
		`, b.email, b.tag); err != nil {
			return fmt.Errorf("email tag: %w", err)
		}
	}

	folders := []struct{ campaign, folder uuid.UUID }{
		{DevCampaignActiveID, devFolderOutbound},
		{DevCampaignDraftID, devFolderNurture},
	}
	for _, b := range folders {
		if _, err := pool.Exec(ctx, `
			INSERT INTO campaign_folders (campaign_id, folder_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, b.campaign, b.folder); err != nil {
			return fmt.Errorf("campaign folder: %w", err)
		}
	}

	// Tag-based sender selection: the active campaign sends from Cold-tagged
	// mailboxes (the default sender_strategy = 'tags').
	if _, err := pool.Exec(ctx, `
		INSERT INTO campaign_email_tags (tag_id, campaign_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, devTagCold, DevCampaignActiveID); err != nil {
		return fmt.Errorf("campaign email tag: %w", err)
	}

	categories := []struct {
		contact  int
		category uuid.UUID
	}{
		{5, devCategoryLead},  // Mira
		{12, devCategoryLead}, // Chloe
		{13, devCategoryLead}, // Nadia
		{0, devCategoryLead},  // Priya
		{8, devCategoryCustomer},
		{21, devCategoryChurn}, // Jonas, pushing back
	}
	for _, b := range categories {
		if _, err := pool.Exec(ctx, `
			INSERT INTO contact_categories (contact_id, category_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING
		`, devContactID(b.contact), b.category); err != nil {
			return fmt.Errorf("contact category: %w", err)
		}
	}
	return nil
}

func seedDevReplyTemplates(ctx context.Context, pool *pgxpool.Pool) error {
	tpls := []struct {
		id    uuid.UUID
		name  string
		plain string
		pos   int
	}{
		{devTplYes, "Quick yes", "Sounds great, sending a calendar invite now. Talk soon.", 0},
		{devTplCall, "Book a call", "Happy to walk you through it. Grab a slot here: https://cal.warmbly.test/dev", 1},
		{devTplNo, "Polite no", "Thanks for the note. Not a fit right now, I will remove you from this list.", 2},
	}
	for _, t := range tpls {
		if _, err := pool.Exec(ctx, `
			INSERT INTO reply_templates (id, organization_id, user_id, name, subject, body_html, body_plain, position, created_at, updated_at)
			VALUES ($1, $2, $3, $4, 'Re: {{originalSubject}}', $5, $6, $7, NOW(), NOW())
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				body_html = EXCLUDED.body_html,
				body_plain = EXCLUDED.body_plain,
				position = EXCLUDED.position,
				updated_at = NOW()
		`, t.id, DevOrgID, DevUserID, t.name, "<p>"+t.plain+"</p>", t.plain, t.pos); err != nil {
			return fmt.Errorf("template %s: %w", t.name, err)
		}
	}
	return nil
}

// seedDevCRM builds the dev org's pipeline, deals, tasks, notes, and activity.
func seedDevCRM(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `
		INSERT INTO pipelines (id, organization_id, name, position, created_at, updated_at)
		VALUES ($1, $2, 'Sales pipeline', 0, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, updated_at = NOW()
	`, devPipelineID, DevOrgID); err != nil {
		return fmt.Errorf("pipeline: %w", err)
	}

	stages := []struct {
		id    uuid.UUID
		name  string
		color string
		pos   int
	}{
		{devStageNew, "New", "#3b82f6", 0},
		{devStageQual, "Qualified", "#a855f7", 1},
		{devStageDemo, "Demo booked", "#f59e0b", 2},
		{devStageWon, "Won", "#10b981", 3},
		{devStageLost, "Lost", "#ef4444", 4},
	}
	for _, s := range stages {
		if _, err := pool.Exec(ctx, `
			INSERT INTO pipeline_stages (id, pipeline_id, name, color, position, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
			ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, color = EXCLUDED.color, position = EXCLUDED.position, updated_at = NOW()
		`, s.id, devPipelineID, s.name, s.color, s.pos); err != nil {
			return fmt.Errorf("stage %s: %w", s.name, err)
		}
	}

	for i, tt := range []struct {
		name  string
		color string
	}{{"Call", "#8b5cf6"}, {"Email", "#0ea5e9"}, {"Meeting", "#f59e0b"}} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO crm_task_types (organization_id, name, color, position)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (organization_id, name) DO NOTHING
		`, DevOrgID, tt.name, tt.color, i); err != nil {
			return fmt.Errorf("task type %s: %w", tt.name, err)
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
		{devDealLumina, devStageDemo, "Lumina Labs - team plan", 18_000, "open", false, devContactID(5)},
		{devDealCinderworks, devStageQual, "Cinderworks - pilot", 6_500, "open", false, devContactID(13)},
		{devDealEmberly, devStageNew, "Emberly - annual", 12_000, "open", false, devContactID(12)},
		{devDealDriftline, devStageWon, "Driftline - starter", 4_800, "won", true, devContactID(8)},
	}
	for _, d := range deals {
		wonAt := "NULL"
		if d.won {
			wonAt = "NOW() - INTERVAL '6 days'"
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO deals (id, organization_id, pipeline_id, stage_id, contact_id, name, value, currency, status, won_at, assigned_to, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, 'USD', $8, `+wonAt+`, $9, NOW() - INTERVAL '8 days', NOW())
			ON CONFLICT (id) DO UPDATE SET stage_id = EXCLUDED.stage_id, status = EXCLUDED.status, updated_at = NOW()
		`, d.id, DevOrgID, devPipelineID, d.stage, d.contact, d.name, d.value, d.status, DevUserID); err != nil {
			return fmt.Errorf("deal %s: %w", d.name, err)
		}
	}

	tasks := []struct {
		id       uuid.UUID
		title    string
		desc     string
		ttype    string
		priority string
		status   string
		dueDays  int
		contact  uuid.UUID
		deal     uuid.UUID
	}{
		{devTaskCall, "Call Mira about pricing", "She replied positive, wants team-plan numbers.", "Call", "high", "in_progress", 1, devContactID(5), devDealLumina},
		{devTaskEmail, "Send pilot scope to Nadia", "Use the deliverability pilot outline.", "Email", "medium", "pending", 2, devContactID(13), devDealCinderworks},
		{devTaskMeeting, "Demo with Emberly", "Chloe asked about the partner dashboard, show it live.", "Meeting", "medium", "pending", 4, devContactID(12), devDealEmberly},
	}
	for _, t := range tasks {
		if _, err := pool.Exec(ctx, `
			INSERT INTO crm_tasks (id, organization_id, contact_id, deal_id, assigned_to, created_by, title, description, type, due_date, priority, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $5, $6, $7, $8, NOW() + make_interval(days => $9), $10, $11, NOW(), NOW())
			ON CONFLICT (id) DO UPDATE SET title = EXCLUDED.title, status = EXCLUDED.status, due_date = EXCLUDED.due_date, updated_at = NOW()
		`, t.id, DevOrgID, t.contact, t.deal, DevUserID, t.title, t.desc, t.ttype, t.dueDays, t.priority, t.status); err != nil {
			return fmt.Errorf("task %s: %w", t.title, err)
		}
	}

	notes := []struct {
		id      uuid.UUID
		contact uuid.UUID
		content string
	}{
		{devNoteMira, devContactID(5), "Prefers async email over calls. Evaluating two other tools, decision this month."},
		{devNoteNadia, devContactID(13), "Meeting booked. Cares most about spam-folder visibility per mailbox."},
		{devNoteChloe, devContactID(12), "Partnerships angle: she may bring three client brands if the pilot goes well."},
	}
	for _, n := range notes {
		if _, err := pool.Exec(ctx, `
			INSERT INTO contact_notes (id, contact_id, organization_id, user_id, content, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, NOW() - INTERVAL '2 days', NOW())
			ON CONFLICT (id) DO NOTHING
		`, n.id, n.contact, DevOrgID, DevUserID, n.content); err != nil {
			return fmt.Errorf("note: %w", err)
		}
	}

	activities := []struct {
		contact uuid.UUID
		actType string
		meta    string
		daysAgo float64
	}{
		{devContactID(5), "email_sent", `{"campaign":"RevOps outreach - July","step":1}`, 6.5},
		{devContactID(5), "email_opened", `{"campaign":"RevOps outreach - July","step":1}`, 6.4},
		{devContactID(5), "email_replied", `{"campaign":"RevOps outreach - July","step":2}`, 2.6},
		{devContactID(5), "deal_created", `{"deal":"Lumina Labs - team plan"}`, 2.4},
		{devContactID(13), "email_replied", `{"campaign":"RevOps outreach - July","step":2}`, 3.1},
		{devContactID(6), "email_bounced", `{"campaign":"RevOps outreach - July","step":1}`, 5.7},
		{devContactID(8), "deal_won", `{"deal":"Driftline - starter"}`, 6.0},
		{devContactID(12), "email_replied", `{"campaign":"RevOps outreach - July","step":2}`, 0.1},
	}
	for i, a := range activities {
		sql := fmt.Sprintf(`
			INSERT INTO contact_activities (id, contact_id, organization_id, user_id, activity_type, metadata, created_at)
			VALUES ($1, $2, $3, $4, $5::activity_type, $6::jsonb, %s)
			ON CONFLICT (id) DO NOTHING`, devDayExpr(a.daysAgo))
		if _, err := pool.Exec(ctx, sql, devEntityID("13", i+1), a.contact, DevOrgID, DevUserID, a.actType, a.meta); err != nil {
			return fmt.Errorf("activity %d: %w", i, err)
		}
	}
	return nil
}

// seedDevCredits gives the org a healthy credit balance plus a believable
// transaction trail (grant, top-up, then consumption) for the billing page.
func seedDevCredits(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `
		INSERT INTO credit_ledger (org_id, balance, purchased_balance, total_purchased, month_reset_at, created_at, updated_at)
		VALUES ($1, 275, 200, 200, date_trunc('month', NOW()), NOW() - INTERVAL '14 days', NOW())
		ON CONFLICT (org_id) DO UPDATE SET
			balance = EXCLUDED.balance,
			purchased_balance = EXCLUDED.purchased_balance,
			total_purchased = EXCLUDED.total_purchased,
			updated_at = NOW()
	`, DevOrgID); err != nil {
		return fmt.Errorf("ledger: %w", err)
	}

	// balance_after runs 300 -> 275; purchased pool is untouched after the
	// top-up so purchased_balance_after stays 200.
	txns := []struct {
		amount       int
		reason       string
		model        string
		tokens       int
		balanceAfter int
		purchDelta   int
		purchAfter   int
		daysAgo      float64
	}{
		{300, "trial_grant", "", 0, 300, 0, 0, 13.8},
		{200, "credit_topup", "", 0, 300, 200, 200, 9.0},
		{-4, "writing_assistant", "gpt-4o-mini", 2100, 296, 0, 200, 8.1},
		{-10, "research_run", "gpt-4o", 5400, 286, 0, 200, 6.2},
		{-2, "agent_iteration", "gpt-4o-mini", 900, 284, 0, 200, 4.5},
		{-3, "inbox_agent_draft", "gpt-4o-mini", 1300, 281, 0, 200, 2.2},
		{-4, "writing_assistant", "gpt-4o-mini", 1900, 277, 0, 200, 1.1},
		{-2, "agent_iteration", "gpt-4o-mini", 800, 275, 0, 200, 0.2},
	}
	for i, t := range txns {
		sql := fmt.Sprintf(`
			INSERT INTO credit_ledger_transactions
				(id, org_id, amount, reason, model_used, tokens_used, balance_after, purchased_delta, purchased_balance_after, idempotency_key, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULL, %s)
			ON CONFLICT (id) DO NOTHING`, devDayExpr(t.daysAgo))
		if _, err := pool.Exec(ctx, sql, devEntityID("11", i+1), DevOrgID,
			t.amount, t.reason, t.model, t.tokens, t.balanceAfter, t.purchDelta, t.purchAfter); err != nil {
			return fmt.Errorf("txn %d: %w", i, err)
		}
	}
	return nil
}

// seedDevNotifications fills the bell with recent, mixed-read notifications.
func seedDevNotifications(ctx context.Context, pool *pgxpool.Pool) error {
	notifs := []struct {
		category string
		title    string
		body     string
		link     string
		read     bool
		daysAgo  float64
	}{
		{"inbound_reply", "Mira Kovacs replied", "New reply on RevOps outreach - July.", "/app/inbox", false, 0.2},
		{"campaign_started", "RevOps outreach - July is live", "Your campaign started sending from 2 mailboxes.", "/app/campaigns", true, 13.9},
		{"mailbox_connected", "dev.partners@warmbly.test connected", "SMTP and IMAP checks passed. Warmup has started.", "/app/emails", true, 8.0},
		{"warmup_milestone", "Warmup ramping nicely", "dev.send@warmbly.test reached 30 warmup emails per day.", "/app/emails", true, 2.0},
		{"credits_low", "Monthly credits ran low", "You used 80% of the monthly allowance, so we drew on your purchased credits.", "/app/settings/billing", false, 4.0},
	}
	for i, n := range notifs {
		readExpr := "NULL"
		if n.read {
			readExpr = devDayExpr(n.daysAgo - 0.05)
		}
		sql := fmt.Sprintf(`
			INSERT INTO notifications (id, user_id, organization_id, category, title, body, link, metadata, read_at, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, '{}'::jsonb, %s, %s)
			ON CONFLICT (id) DO NOTHING`, readExpr, devDayExpr(n.daysAgo))
		if _, err := pool.Exec(ctx, sql, devEntityID("0f", i+1), DevUserID, DevOrgID, n.category, n.title, n.body, n.link); err != nil {
			return fmt.Errorf("notification %s: %w", n.title, err)
		}
	}
	return nil
}
