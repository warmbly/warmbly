package sandbox

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/seed"
)

// Stable UUIDs (middle group "aaaa" marks sandbox rows; disjoint from the
// cmd/seed and internal/seed namespaces).
var (
	sandboxUser = uuid.MustParse("11111111-aaaa-0000-0000-000000000001")
	sandboxOrg  = uuid.MustParse("22222222-aaaa-0000-0000-000000000001")
	sandboxSub  = uuid.MustParse("88888888-aaaa-0000-0000-000000000001")

	// The premium shared worker (`make worker-premium` natively, or the
	// docker worker-premium-1). Paid orgs place strictly onto premium
	// workers, so the sandbox mailboxes must live here.
	sandboxWorker = uuid.MustParse("10c8f5e4-1c39-5b2a-9c8b-3d2f0a8b1a02")

	campaignLaunch  = uuid.MustParse("44444444-aaaa-0000-0000-000000000001")
	campaignAgency  = uuid.MustParse("44444444-aaaa-0000-0000-000000000002")
	campaignDormant = uuid.MustParse("44444444-aaaa-0000-0000-000000000003")
)

// SandboxLoginEmail / SandboxLoginPassword are the dashboard credentials the
// seeder prints; keep in sync with docs/content/docs/development/sandbox.mdx.
const (
	SandboxLoginEmail    = "sandbox@warmbly.test"
	SandboxLoginPassword = "password123"
)

type mailboxSeed struct {
	id    uuid.UUID
	email string
	name  string
}

// sandboxMailboxes are the org's senders, all hosted on the local dovecot.
var sandboxMailboxes = []mailboxSeed{
	{uuid.MustParse("33333333-aaaa-0000-0000-000000000001"), "sarah.lin@sunrise.test", "Sarah Lin"},
	{uuid.MustParse("33333333-aaaa-0000-0000-000000000002"), "marcus.reid@sunrise.test", "Marcus Reid"},
	{uuid.MustParse("33333333-aaaa-0000-0000-000000000003"), "priya.nair@sunrise.test", "Priya Nair"},
	{uuid.MustParse("33333333-aaaa-0000-0000-000000000004"), "tom.abel@sunrise.test", "Tom Abel"},
	{uuid.MustParse("33333333-aaaa-0000-0000-000000000005"), "elena.voss@sunrise.test", "Elena Voss"},
	{uuid.MustParse("33333333-aaaa-0000-0000-000000000006"), "dan.okafor@sunrise.test", "Dan Okafor"},
}

type contactSeed struct {
	first, last, email, company string
	subscribed                  bool
}

// Launch-campaign prospects. Two are unsubscribed so suppression shows up.
var launchContacts = []contactSeed{
	{"Aiden", "Park", "aiden.park@northwind.test", "Northwind", true},
	{"Beth", "Chen", "beth.chen@initech.test", "Initech", true},
	{"Carlos", "Diaz", "carlos.diaz@piedpiper.test", "Pied Piper", true},
	{"Diana", "Fox", "diana.fox@globex.test", "Globex", true},
	{"Eli", "Grant", "eli.grant@hooli.test", "Hooli", true},
	{"Fiona", "Hale", "fiona.hale@umbrella.test", "Umbrella", false},
	{"Greg", "Iver", "greg.iver@stark.test", "Stark Industries", true},
	{"Hana", "Jules", "hana.jules@wayne.test", "Wayne Enterprises", true},
	{"Ivan", "Kova", "ivan.kova@tyrell.test", "Tyrell", true},
	{"Jade", "Lund", "jade.lund@wonka.test", "Wonka", true},
	{"Kofi", "Mensah", "kofi.mensah@acme-corp.test", "Acme Corp", true},
	{"Lena", "Novak", "lena.novak@cyberdyne.test", "Cyberdyne", true},
	{"Mia", "Ono", "mia.ono@soylent.test", "Soylent", true},
	{"Nils", "Pett", "nils.pett@aperture.test", "Aperture", false},
	{"Olga", "Quist", "olga.quist@blackmesa.test", "Black Mesa", true},
	{"Pablo", "Rey", "pablo.rey@monsters.test", "Monsters Inc", true},
	{"Quinn", "Soto", "quinn.soto@dunder.test", "Dunder Mifflin", true},
	{"Rita", "Tam", "rita.tam@vandelay.test", "Vandelay", true},
	{"Sam", "Ueda", "sam.ueda@prestige.test", "Prestige Worldwide", true},
	{"Tara", "Vale", "tara.vale@oceanic.test", "Oceanic", true},
	{"Umar", "Wolf", "umar.wolf@massive.test", "Massive Dynamic", true},
	{"Vera", "Xu", "vera.xu@virtucon.test", "Virtucon", true},
	{"Wes", "York", "wes.york@octan.test", "Octan", true},
	{"Xena", "Zair", "xena.zair@gringotts.test", "Gringotts", true},
}

// Agency-campaign prospects.
var agencyContacts = []contactSeed{
	{"Amara", "Bell", "amara.bell@brightloop.test", "Brightloop Agency", true},
	{"Boris", "Chan", "boris.chan@funnelworks.test", "Funnelworks", true},
	{"Cleo", "Danes", "cleo.danes@leadcraft.test", "Leadcraft", true},
	{"Derek", "Enns", "derek.enns@growthlab.test", "Growthlab", true},
	{"Esme", "Ford", "esme.ford@pipelinehq.test", "Pipeline HQ", true},
	{"Felix", "Gaunt", "felix.gaunt@outbounders.test", "Outbounders", true},
	{"Gita", "Hart", "gita.hart@replyrate.test", "Replyrate", true},
	{"Hugo", "Ines", "hugo.ines@coldsmiths.test", "Coldsmiths", true},
	{"Iris", "Joon", "iris.joon@meetingmakers.test", "Meeting Makers", true},
	{"Jonas", "Kemp", "jonas.kemp@quotaquest.test", "Quotaquest", true},
	{"Kira", "Lowe", "kira.lowe@demodesk.test", "Demodesk Partners", true},
	{"Liam", "Moss", "liam.moss@sequoialeads.test", "Sequoia Leads", true},
}

// Seed provisions the sandbox: the full internal/seed fixture (plans, demo
// orgs, workers), then the sandbox org with live mailboxes, active campaigns,
// contacts, warmup membership, and a paid subscription. Finally it repairs
// EVERY smtp_imap account's credentials to point at mailpit/dovecot so the
// whole warmup pool can actually send and sync. Idempotent throughout.
func Seed(ctx context.Context, pool *pgxpool.Pool, cfg Config) error {
	if cfg.CredentialsKey == "" {
		return fmt.Errorf("CREDENTIALS_ENCRYPTION_KEY is required to seed working mailboxes")
	}
	enc, err := encrypt.NewEncrypterFromHex(cfg.CredentialsKey)
	if err != nil {
		return fmt.Errorf("CREDENTIALS_ENCRYPTION_KEY: %w", err)
	}

	// Base fixture: plans + durations + workers + the two demo orgs. Running
	// it here makes `make sandbox` self-contained on a fresh database.
	if _, err := seed.Run(ctx, pool); err != nil {
		return fmt.Errorf("base seed: %w", err)
	}

	if err := seedIdentity(ctx, pool); err != nil {
		return err
	}
	if err := seedMailboxes(ctx, pool); err != nil {
		return err
	}
	if err := seedSubscription(ctx, pool); err != nil {
		return err
	}
	if err := seedCampaigns(ctx, pool); err != nil {
		return err
	}
	if err := repairSMTPIMAPCredentials(ctx, pool, cfg, enc); err != nil {
		return err
	}
	if err := repairContactVerification(ctx, pool); err != nil {
		return err
	}
	if err := deactivateIdleFixtureWorkers(ctx, pool); err != nil {
		return err
	}

	fmt.Println("sandbox seeded:")
	fmt.Printf("  dashboard  %s / %s (org: Sunrise Labs)\n", SandboxLoginEmail, SandboxLoginPassword)
	fmt.Printf("  mailboxes  %d senders on @sunrise.test (SMTP -> mailpit, IMAP -> dovecot)\n", len(sandboxMailboxes))
	fmt.Println("  campaigns  Sunrise Q3 launch + Agency partnerships (active), Dormant reactivation (draft)")
	fmt.Println("  warmup     enabled on all senders, premium pool")
	return nil
}

func seedIdentity(ctx context.Context, pool *pgxpool.Pool) error {
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)", sandboxUser).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		hash, err := argon2.Hash(SandboxLoginPassword)
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO users (id, first_name, last_name, email, password_hash)
			VALUES ($1, 'Sunny', 'Sandbox', $2, $3)
			ON CONFLICT (id) DO NOTHING`,
			sandboxUser, SandboxLoginEmail, hash); err != nil {
			return err
		}
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO organizations (id, name, slug, owner_user_id)
		VALUES ($1, 'Sunrise Labs', 'sunrise-sandbox', $2)
		ON CONFLICT (id) DO NOTHING`,
		sandboxOrg, sandboxUser); err != nil {
		return err
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO organization_members (organization_id, user_id, role, accepted_at)
		VALUES ($1, $2, 'owner', NOW())
		ON CONFLICT DO NOTHING`,
		sandboxOrg, sandboxUser)
	return err
}

func seedMailboxes(ctx context.Context, pool *pgxpool.Pool) error {
	for _, m := range sandboxMailboxes {
		// Warmup started 10 days ago so ramp progression is mid-flight; the
		// send window is wide open and pacing is demo-friendly (90s min gap).
		if _, err := pool.Exec(ctx, `
			INSERT INTO email_accounts (
				id, user_id, organization_id, worker_id,
				email, name, signature_plain, signature_html,
				provider, status,
				campaign_limit, min_wait_time, timezone,
				warmup, warmup_tag, warmup_pool_type,
				warmup_start_time, warmup_end_time
			) VALUES (
				$1, $2, $3, $4,
				$5, $6, '', '',
				'smtp_imap', 'active',
				100, 90, 'UTC',
				NOW() - INTERVAL '10 days', 'sandbox', 'premium',
				'00:00', '23:59'
			)
			ON CONFLICT (id) DO UPDATE SET
				worker_id = EXCLUDED.worker_id,
				status = 'active',
				campaign_limit = EXCLUDED.campaign_limit,
				min_wait_time = EXCLUDED.min_wait_time,
				warmup = COALESCE(email_accounts.warmup, EXCLUDED.warmup),
				warmup_paused_at = NULL,
				updated_at = NOW()`,
			m.id, sandboxUser, sandboxOrg, sandboxWorker, m.email, m.name); err != nil {
			return fmt.Errorf("mailbox %s: %w", m.email, err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO warmup_pool_participants (pool_id, email_account_id)
			SELECT id, $1 FROM warmup_pools WHERE pool_type = 'premium'::warmup_pool_type
			ON CONFLICT DO NOTHING`,
			m.id); err != nil {
			return fmt.Errorf("pool join %s: %w", m.email, err)
		}
	}
	return nil
}

func seedSubscription(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO subscriptions (
			id, user_id, organization_id, plan_id,
			stripe_customer_id, stripe_subscription_id, stripe_price_id,
			status, current_period_start, current_period_end,
			is_enterprise, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4,
			'cus_sandbox', 'sub_sandbox_starter', 'price_sandbox_starter',
			'active', NOW(), NOW() + INTERVAL '30 days',
			FALSE, NOW(), NOW()
		)
		ON CONFLICT (organization_id) DO UPDATE SET
			plan_id = EXCLUDED.plan_id,
			status = 'active',
			stripe_subscription_id = EXCLUDED.stripe_subscription_id,
			current_period_start = EXCLUDED.current_period_start,
			current_period_end = EXCLUDED.current_period_end,
			updated_at = NOW()`,
		sandboxSub, sandboxUser, sandboxOrg, seed.PlanStarterID)
	return err
}

type campaignSeed struct {
	id       uuid.UUID
	name     string
	status   string
	steps    []stepSeed
	contacts []contactSeed
	// contactBase is the deterministic UUID prefix for this campaign's contacts.
	contactBase string
}

type stepSeed struct {
	id        uuid.UUID
	name      string
	subject   string
	body      string
	waitAfter int
}

func seedCampaigns(ctx context.Context, pool *pgxpool.Pool) error {
	campaigns := []campaignSeed{
		{
			id: campaignLaunch, name: "Sunrise Q3 launch outreach", status: "active",
			contactBase: "66666666-aaaa-0000-0001",
			contacts:    launchContacts,
			steps: []stepSeed{
				{uuid.MustParse("55555555-aaaa-0000-0000-000000000011"), "Intro",
					"Quick question about {{.Company}}",
					"Hi {{.FirstName}},\n\nWe just launched a tool that cuts outbound setup from weeks to minutes, and {{.Company}} came up twice in customer calls last month.\n\nWorth a quick look? Here is a two minute overview: https://warmbly.com/overview\n\nBest,\nSunrise team", 0},
				{uuid.MustParse("55555555-aaaa-0000-0000-000000000012"), "Follow-up",
					"Re: Quick question about {{.Company}}",
					"Hi {{.FirstName}},\n\nFloating this back up. Happy to share the deliverability numbers from the beta if useful: https://warmbly.com/benchmarks\n\nBest,\nSunrise team", 2},
				{uuid.MustParse("55555555-aaaa-0000-0000-000000000013"), "Breakup",
					"Closing the loop",
					"Hi {{.FirstName}},\n\nSounds like the timing is off. I will stop here; if outbound comes back on the roadmap, you know where to find us.\n\nBest,\nSunrise team", 4},
			},
		},
		{
			id: campaignAgency, name: "Agency partnerships", status: "active",
			contactBase: "66666666-aaaa-0000-0002",
			contacts:    agencyContacts,
			steps: []stepSeed{
				{uuid.MustParse("55555555-aaaa-0000-0000-000000000021"), "Partner intro",
					"Partnering with {{.Company}}",
					"Hi {{.FirstName}},\n\nWe work with agencies like {{.Company}} on white-label sending infrastructure. Margins are meaningfully better than reselling seats.\n\nOpen to a short call? Details: https://warmbly.com/partners\n\nBest,\nSunrise partnerships", 0},
				{uuid.MustParse("55555555-aaaa-0000-0000-000000000022"), "Partner follow-up",
					"Re: Partnering with {{.Company}}",
					"Hi {{.FirstName}},\n\nOne more nudge; the partner program closes new slots at the end of the quarter.\n\nBest,\nSunrise partnerships", 3},
			},
		},
		{
			id: campaignDormant, name: "Dormant accounts reactivation", status: "draft",
			contactBase: "66666666-aaaa-0000-0003",
			steps: []stepSeed{
				{uuid.MustParse("55555555-aaaa-0000-0000-000000000031"), "Win-back",
					"We miss you at {{.Company}}",
					"Hi {{.FirstName}},\n\nA lot has shipped since you last looked. Draft for review before this goes anywhere.\n\nBest,\nSunrise team", 0},
			},
		},
	}

	for _, c := range campaigns {
		// Wide-open schedule (all days, 00:00-23:59) so the demo sends now,
		// not at the next business-hours boundary. Tracking on for both.
		if _, err := pool.Exec(ctx, `
			INSERT INTO campaigns (
				id, user_id, organization_id, name, description,
				status, days, start_time, end_time, timezone,
				open_tracking, link_tracking,
				updated_at, created_at
			) VALUES (
				$1, $2, $3, $4, 'Sandbox showcase campaign',
				$5, 127, '00:00', '23:59', 'UTC',
				TRUE, TRUE,
				NOW(), NOW()
			)
			ON CONFLICT (id) DO UPDATE SET
				status = EXCLUDED.status,
				days = EXCLUDED.days,
				start_time = EXCLUDED.start_time,
				end_time = EXCLUDED.end_time,
				updated_at = NOW()`,
			c.id, sandboxUser, sandboxOrg, c.name, c.status); err != nil {
			return fmt.Errorf("campaign %s: %w", c.name, err)
		}

		for i, s := range c.steps {
			if _, err := pool.Exec(ctx, `
				INSERT INTO sequences (
					id, campaign_id, organization_id, name, subject,
					body_plain, body_html, wait_after, position
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
				ON CONFLICT (id) DO NOTHING`,
				s.id, c.id, sandboxOrg, s.name, s.subject, s.body, plainToHTML(s.body), s.waitAfter, i); err != nil {
				return fmt.Errorf("sequence %s: %w", s.name, err)
			}
		}

		for i, ct := range c.contacts {
			cid := uuid.MustParse(fmt.Sprintf("%s-%012d", c.contactBase, i+1))
			// Pre-verified: .test domains have no MX, so the live verifier
			// would mark them invalid and the pre-send gate would skip every
			// send. A stamped verdict is final (the sweep only processes
			// unchecked contacts).
			if _, err := pool.Exec(ctx, `
				INSERT INTO contacts (
					id, user_id, organization_id,
					first_name, last_name, email, company, phone,
					custom_fields, subscribed,
					verification_status, verification_reason, verification_checked_at
				) VALUES ($1, $2, $3, $4, $5, $6, $7, '', '{}', $8,
					'valid', 'sandbox fixture address', NOW())
				ON CONFLICT (id) DO UPDATE SET
					verification_status = 'valid',
					verification_reason = 'sandbox fixture address',
					verification_checked_at = NOW()`,
				cid, sandboxUser, sandboxOrg, ct.first, ct.last, ct.email, ct.company, ct.subscribed); err != nil {
				return fmt.Errorf("contact %s: %w", ct.email, err)
			}
			if _, err := pool.Exec(ctx, `
				INSERT INTO campaign_leads (campaign_id, contact_id)
				VALUES ($1, $2)
				ON CONFLICT DO NOTHING`,
				c.id, cid); err != nil {
				return fmt.Errorf("campaign lead %s: %w", ct.email, err)
			}
		}
	}
	return nil
}

// repairSMTPIMAPCredentials points every smtp_imap account (sandbox AND the
// cmd/seed / internal/seed fixtures, whose stored credentials are plaintext
// placeholders) at the local mail stack, sealed with the credentials key so
// the worker loader can decrypt and actually send/sync them.
func repairSMTPIMAPCredentials(ctx context.Context, pool *pgxpool.Pool, cfg Config, enc *encrypt.Encrypter) error {
	rows, err := pool.Query(ctx, `SELECT id, email FROM email_accounts WHERE provider = 'smtp_imap'`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type acct struct {
		id    uuid.UUID
		email string
	}
	var accts []acct
	for rows.Next() {
		var a acct
		if err := rows.Scan(&a.id, &a.email); err != nil {
			return err
		}
		accts = append(accts, a)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	sealed := func(s string) (string, error) { return enc.Encrypt(s) }
	for _, a := range accts {
		smtpHost, err1 := sealed(cfg.SMTPHost)
		smtpUser, err2 := sealed(a.email)
		smtpPass, err3 := sealed(cfg.IMAPPassword)
		imapHost, err4 := sealed(cfg.IMAPHost)
		imapUser, err5 := sealed(a.email)
		imapPass, err6 := sealed(cfg.IMAPPassword)
		for _, e := range []error{err1, err2, err3, err4, err5, err6} {
			if e != nil {
				return e
			}
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO email_accounts_smtp_imap (
				email_account_id,
				smtp_host, smtp_port, smtp_user, smtp_password,
				imap_host, imap_port, imap_user, imap_password
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (email_account_id) DO UPDATE SET
				smtp_host = EXCLUDED.smtp_host,
				smtp_port = EXCLUDED.smtp_port,
				smtp_user = EXCLUDED.smtp_user,
				smtp_password = EXCLUDED.smtp_password,
				imap_host = EXCLUDED.imap_host,
				imap_port = EXCLUDED.imap_port,
				imap_user = EXCLUDED.imap_user,
				imap_password = EXCLUDED.imap_password`,
			a.id, smtpHost, cfg.SMTPPort, smtpUser, smtpPass,
			imapHost, cfg.IMAPPort, imapUser, imapPass); err != nil {
			return fmt.Errorf("credentials for %s: %w", a.email, err)
		}
	}
	fmt.Printf("  credentials repaired for %d smtp_imap accounts\n", len(accts))
	return nil
}

// repairContactVerification marks every fixture contact (.test addresses,
// including the cmd/seed and internal/seed ones) as verified. The live
// verifier finds no MX for .test domains and flags them invalid, after which
// the pre-send gate would skip every campaign send in the sandbox.
func repairContactVerification(ctx context.Context, pool *pgxpool.Pool) error {
	tag, err := pool.Exec(ctx, `
		UPDATE contacts
		SET verification_status = 'valid',
		    verification_reason = 'sandbox fixture address',
		    verification_checked_at = NOW()
		WHERE email LIKE '%.test' AND verification_status <> 'valid'`)
	if err != nil {
		return err
	}
	if n := tag.RowsAffected(); n > 0 {
		fmt.Printf("  verification repaired for %d fixture contacts\n", n)
	}
	return nil
}

// deactivateIdleFixtureWorkers deactivates every seeded worker except the two
// the native stack actually runs (`make worker` / `make worker-premium`).
// Fixture workers are seeded active but never heartbeat, so placement keeps
// choosing them and the dead-worker sweep keeps draining them - an assignment
// ping-pong that strands mailboxes mid-send. `make seed` re-activates them
// for the docker `make sim` flow.
func deactivateIdleFixtureWorkers(ctx context.Context, pool *pgxpool.Pool) error {
	tag, err := pool.Exec(ctx, `
		UPDATE workers SET active = FALSE, updated_at = NOW()
		WHERE active AND id NOT IN ($1, $2)`,
		uuid.MustParse("10c8f5e4-1c39-5b2a-9c8b-3d2f0a8b1a01"), sandboxWorker)
	if err != nil {
		return err
	}
	if n := tag.RowsAffected(); n > 0 {
		fmt.Printf("  deactivated %d fixture workers not running in the native stack\n", n)
	}
	return nil
}

// plainToHTML renders the plaintext step body as minimal paragraph HTML,
// linkifying bare URLs into anchors so tracking has both a pixel target and
// hrefs to wrap into click tickets.
func plainToHTML(body string) string {
	html := ""
	for _, para := range splitParagraphs(body) {
		html += "<p>" + linkifyURLs(para) + "</p>"
	}
	return html
}

var urlPattern = regexp.MustCompile(`https://[^\s<]+`)

func linkifyURLs(s string) string {
	return urlPattern.ReplaceAllString(s, `<a href="$0">$0</a>`)
}

func splitParagraphs(s string) []string {
	var out []string
	cur := ""
	for _, line := range strings.Split(s, "\n") {
		if line == "" {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		if cur != "" {
			cur += "<br>"
		}
		cur += line
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
