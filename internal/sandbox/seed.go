package sandbox

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/models"
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

	// The worker `make worker` / `make dev` / `make sandbox` actually runs
	// (WORKER_ID ...1a01). seedWorker upserts this row so the mailbox
	// worker_id FK holds even before the worker first heartbeats; when the
	// worker boots it adopts this exact row and its assigned mailboxes.
	sandboxWorker = uuid.MustParse("10c8f5e4-1c39-5b2a-9c8b-3d2f0a8b1a01")

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
	title string
}

// sandboxMailboxes are the org's senders, all hosted on the local dovecot.
// 20 of them: the dashboard's warmup-coverage notice considers a pool healthy
// at 20+ warming mailboxes, and the Pro plan allows exactly 20 accounts.
var sandboxMailboxes = []mailboxSeed{
	{uuid.MustParse("33333333-aaaa-0000-0000-000000000001"), "sarah.lin@sunrise.test", "Sarah Lin", "Head of Growth"},
	{uuid.MustParse("33333333-aaaa-0000-0000-000000000002"), "marcus.reid@sunrise.test", "Marcus Reid", "Founder"},
	{uuid.MustParse("33333333-aaaa-0000-0000-000000000003"), "priya.nair@sunrise.test", "Priya Nair", "Partnerships Lead"},
	{uuid.MustParse("33333333-aaaa-0000-0000-000000000004"), "tom.abel@sunrise.test", "Tom Abel", "Account Executive"},
	{uuid.MustParse("33333333-aaaa-0000-0000-000000000005"), "elena.voss@sunrise.test", "Elena Voss", "SDR"},
	{uuid.MustParse("33333333-aaaa-0000-0000-000000000006"), "dan.okafor@sunrise.test", "Dan Okafor", "SDR"},
	{uuid.MustParse("33333333-aaaa-0000-0000-000000000007"), "mei.tanaka@sunrise.test", "Mei Tanaka", "Account Executive"},
	{uuid.MustParse("33333333-aaaa-0000-0000-000000000008"), "lucas.ferro@sunrise.test", "Lucas Ferro", "SDR"},
	{uuid.MustParse("33333333-aaaa-0000-0000-000000000009"), "ana.duarte@sunrise.test", "Ana Duarte", "Customer Success"},
	{uuid.MustParse("33333333-aaaa-0000-0000-00000000000a"), "victor.hsu@sunrise.test", "Victor Hsu", "Sales Manager"},
	{uuid.MustParse("33333333-aaaa-0000-0000-00000000000b"), "nadia.eriksen@sunrise.test", "Nadia Eriksen", "SDR"},
	{uuid.MustParse("33333333-aaaa-0000-0000-00000000000c"), "omar.haddad@sunrise.test", "Omar Haddad", "Account Executive"},
	{uuid.MustParse("33333333-aaaa-0000-0000-00000000000d"), "claire.dubois@sunrise.test", "Claire Dubois", "Partnerships"},
	{uuid.MustParse("33333333-aaaa-0000-0000-00000000000e"), "jonas.weber@sunrise.test", "Jonas Weber", "SDR"},
	{uuid.MustParse("33333333-aaaa-0000-0000-00000000000f"), "ines.moreau@sunrise.test", "Ines Moreau", "Growth"},
	{uuid.MustParse("33333333-aaaa-0000-0000-000000000010"), "ravi.menon@sunrise.test", "Ravi Menon", "Account Executive"},
	{uuid.MustParse("33333333-aaaa-0000-0000-000000000011"), "sofia.petrov@sunrise.test", "Sofia Petrov", "SDR"},
	{uuid.MustParse("33333333-aaaa-0000-0000-000000000012"), "liam.walsh@sunrise.test", "Liam Walsh", "SDR"},
	{uuid.MustParse("33333333-aaaa-0000-0000-000000000013"), "yuki.sato@sunrise.test", "Yuki Sato", "Customer Success"},
	{uuid.MustParse("33333333-aaaa-0000-0000-000000000014"), "erik.lund@sunrise.test", "Erik Lund", "Sales Manager"},
}

// mailboxProfile places a sender at a specific point in the warmup lifecycle
// so the accounts list reads like a real, aged fleet rather than twenty
// identical rows. Volumes feed warmup_statistics / daily_email_counts so the
// numbers the dashboard computes (current/target, health, usage) line up.
type mailboxProfile struct {
	warmupDaysAgo int // days since warmup was enabled
	warmupBase    int
	warmupInc     int
	warmupMax     int
	paused        bool
	healthState   string
	healthScore   int
	spamScore     int
	campaignToday int // today's cold sends (daily_email_counts)
	accountAge    int // created_at, days ago
}

// profileFor maps a roster index onto a lifecycle cohort:
//
//	0-5   graduated: ramp finished weeks ago, holding max warmup volume and
//	      carrying full (~50/day) cold sending
//	6-13  mid-ramp: 5-16 days in, climbing
//	14-16 fresh: connected days ago, first ramp steps
//	17    late-ramp (Liam) — NOT paused: the coverage notice wants 20 warming
//	18    watch: reduced health, an open provider warning (Yuki)
//	19    graduated veteran (Erik)
func profileFor(i int) mailboxProfile {
	switch {
	case i < 6:
		return mailboxProfile{
			warmupDaysAgo: 45 + i*3, warmupBase: 10, warmupInc: 1, warmupMax: 40 + (i%3)*5,
			healthState: "healthy", healthScore: 93 + i%6, spamScore: i % 3,
			campaignToday: 45 + (i*3)%11, accountAge: 60 + i*4,
		}
	case i < 14:
		return mailboxProfile{
			warmupDaysAgo: 5 + (i - 6) + (i % 3), warmupBase: 10, warmupInc: 2, warmupMax: 40,
			healthState: "healthy", healthScore: 84 + (i*7)%12, spamScore: (i * 3) % 6,
			campaignToday: 14 + (i*5)%17, accountAge: 25 + i,
		}
	case i < 17:
		return mailboxProfile{
			warmupDaysAgo: i - 13, warmupBase: 10, warmupInc: 2, warmupMax: 40,
			healthState: "healthy", healthScore: 80 + (i*5)%9, spamScore: 0,
			campaignToday: 4 + i%5, accountAge: 4 + (i - 13),
		}
	case i == 17:
		return mailboxProfile{
			warmupDaysAgo: 12, warmupBase: 10, warmupInc: 2, warmupMax: 40,
			healthState: "healthy", healthScore: 88, spamScore: 1,
			campaignToday: 18, accountAge: 30,
		}
	case i == 18:
		return mailboxProfile{
			warmupDaysAgo: 9, warmupBase: 10, warmupInc: 2, warmupMax: 40,
			healthState: "watch", healthScore: 62, spamScore: 14,
			campaignToday: 6, accountAge: 28,
		}
	default:
		return mailboxProfile{
			warmupDaysAgo: 50, warmupBase: 10, warmupInc: 1, warmupMax: 50,
			healthState: "healthy", healthScore: 90, spamScore: 2,
			campaignToday: 48, accountAge: 70,
		}
	}
}

// signaturePlain / signatureHTML build each sender's signature; every sandbox
// mailbox ships configured with one so composer and sent mail look real.
func signaturePlain(m mailboxSeed) string {
	return fmt.Sprintf("--\n%s\n%s, Sunrise Labs\nhttps://sunrise.test", m.name, m.title)
}

func signatureHTML(m mailboxSeed) string {
	return fmt.Sprintf(
		`<p>--<br>%s<br>%s, Sunrise Labs<br><a href="https://sunrise.test">sunrise.test</a></p>`,
		m.name, m.title)
}

type contactSeed struct {
	first, last, email, company string
	subscribed                  bool
}

// Launch-campaign prospects. Three are unsubscribed so suppression shows up.
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
	{"Yara", "Aziz", "yara.aziz@ingen.test", "InGen", true},
	{"Zack", "Bram", "zack.bram@weyland.test", "Weyland", true},
	{"Alba", "Cruz", "alba.cruz@nakatomi.test", "Nakatomi", true},
	{"Bruno", "Dias", "bruno.dias@oscorp.test", "Oscorp", true},
	{"Carmen", "Egan", "carmen.egan@rekall.test", "Rekall", true},
	{"Dario", "Finn", "dario.finn@omni.test", "Omni Consumer", true},
	{"Edda", "Grieg", "edda.grieg@lexcorp.test", "LexCorp", false},
	{"Frans", "Holt", "frans.holt@zorg.test", "Zorg Industries", true},
	{"Greta", "Isak", "greta.isak@wallace.test", "Wallace Corp", true},
	{"Henrik", "Juul", "henrik.juul@abstergo.test", "Abstergo", true},
	{"Ida", "Krog", "ida.krog@aviato.test", "Aviato", true},
	{"Jesper", "Lie", "jesper.lie@bluthco.test", "Bluth Company", true},
	{"Katya", "Moro", "katya.moro@sterling.test", "Sterling Cooper", true},
	{"Lars", "Nye", "lars.nye@paperstreet.test", "Paper Street", true},
	{"Mona", "Odum", "mona.odum@ewing.test", "Ewing Oil", true},
	{"Noor", "Patel", "noor.patel@genco.test", "Genco", true},
	{"Otto", "Qvist", "otto.qvist@duff.test", "Duff Beverages", true},
	{"Petra", "Rask", "petra.rask@vehement.test", "Vehement Capital", true},
	{"Rasmus", "Skov", "rasmus.skov@hudsucker.test", "Hudsucker", true},
	{"Selma", "Toft", "selma.toft@wernham.test", "Wernham Hogg", true},
	{"Teo", "Urso", "teo.urso@pearson.test", "Pearson Hardman", true},
	{"Uma", "Vang", "uma.vang@compuglobal.test", "CompuGlobal", true},
	{"Viggo", "Wren", "viggo.wren@initrode.test", "Initrode", true},
	{"Wanda", "Yee", "wanda.yee@chotchkies.test", "Chotchkies", true},
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
	{"Mara", "Nunn", "mara.nunn@scaledup.test", "ScaledUp", true},
	{"Noah", "Orr", "noah.orr@leadloft.test", "Leadloft", true},
	{"Opal", "Pryce", "opal.pryce@bookedcal.test", "BookedCal", true},
	{"Pier", "Quon", "pier.quon@outflow.test", "Outflow Media", true},
	{"Rhea", "Sand", "rhea.sand@prospectiv.test", "Prospectiv", true},
	{"Silas", "Thorn", "silas.thorn@dialedin.test", "DialedIn", true},
	{"Tess", "Ulm", "tess.ulm@warmintro.test", "Warm Intro Co", true},
	{"Umi", "Vex", "umi.vex@replyforge.test", "Replyforge", true},
	{"Vito", "Wynn", "vito.wynn@growthkit.test", "Growthkit", true},
	{"Willa", "Young", "willa.young@pipestack.test", "Pipestack", true},
	{"Xavi", "Zorn", "xavi.zorn@closerscrm.test", "Closers CRM", true},
	{"Yuna", "Ash", "yuna.ash@bookmore.test", "Bookmore", true},
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
	if err := seedWorker(ctx, pool); err != nil {
		return err
	}
	if err := seedMailboxes(ctx, pool); err != nil {
		return err
	}
	if err := seedSubscription(ctx, pool); err != nil {
		return err
	}
	if err := seedCredits(ctx, pool); err != nil {
		return err
	}
	if err := seedCampaigns(ctx, pool); err != nil {
		return err
	}
	if err := seedHistory(ctx, pool); err != nil {
		return err
	}
	if err := seedAnalytics(ctx, pool); err != nil {
		return err
	}
	if err := seedLabels(ctx, pool); err != nil {
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
	if err := deactivateFixtureMailboxes(ctx, pool); err != nil {
		return err
	}

	fmt.Println("sandbox seeded:")
	fmt.Printf("  dashboard  %s / %s (org: Sunrise Labs)\n", SandboxLoginEmail, SandboxLoginPassword)
	fmt.Printf("  mailboxes  %d senders on @sunrise.test (SMTP -> mailpit, IMAP -> dovecot), signatures + tags on all\n", len(sandboxMailboxes))
	fmt.Println("  campaigns  active, paused, completed, and draft - every list bucket filled")
	fmt.Println("  warmup     enabled on all senders, premium pool, 10 days of ramp stats")
	fmt.Println("  history    funnel progress, unified inbox, CRM pipeline, templates, notifications, chart rollups")
	fmt.Println("  analytics  deliverability events, contact timelines, reply intents, suppression, mailbox errors, audit log")
	fmt.Println("  team       a second teammate plus a pending invite, and a developer API key")
	fmt.Println("  labels     folders/tags/categories bound to mailboxes, campaigns, contacts, and inbox threads")
	fmt.Printf("  credits    plan allowance + %d purchased (AI assistant ready)\n", sandboxTopupCredits)
	return nil
}

// sandboxTopupCredits is the purchased-pool demo top-up, generous enough that
// AI features never hit "out of credits" mid-demo (Starter's monthly allowance
// alone is 250 and a single agent run can spend up to 20).
const sandboxTopupCredits = 5000

// seedCredits fills the sandbox org's AI-credit ledger: the monthly pool at the
// plan allowance plus a purchased top-up, so every AI surface works out of the
// box. Re-seeding refills both pools; the grant transactions are idempotent so
// the history shows exactly one grant and one purchase.
func seedCredits(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `
		INSERT INTO credit_ledger (org_id, balance, purchased_balance, total_purchased, month_reset_at)
		SELECT $1, p.monthly_credits, $3, $3, NOW() FROM plans p WHERE p.id = $2
		ON CONFLICT (org_id) DO UPDATE SET
			balance = EXCLUDED.balance,
			purchased_balance = EXCLUDED.purchased_balance,
			total_purchased = EXCLUDED.total_purchased,
			month_reset_at = NOW(),
			updated_at = NOW()`,
		sandboxOrg, seed.PlanProMonthlyID, sandboxTopupCredits); err != nil {
		return fmt.Errorf("credit ledger: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO credit_ledger_transactions
			(org_id, amount, reason, balance_after, purchased_delta, purchased_balance_after, idempotency_key)
		SELECT $1, p.monthly_credits, 'monthly_reset', p.monthly_credits, 0, 0, 'sandbox-monthly-grant'
		FROM plans p WHERE p.id = $2
		ON CONFLICT (idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING`,
		sandboxOrg, seed.PlanProMonthlyID); err != nil {
		return fmt.Errorf("credit grant txn: %w", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO credit_ledger_transactions
			(org_id, amount, reason, balance_after, purchased_delta, purchased_balance_after, idempotency_key)
		SELECT $1, $2, 'credit_topup', p.monthly_credits, $2, $2, 'sandbox-topup'
		FROM plans p WHERE p.id = $3
		ON CONFLICT (idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING`,
		sandboxOrg, sandboxTopupCredits, seed.PlanProMonthlyID); err != nil {
		return fmt.Errorf("credit topup txn: %w", err)
	}
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
	// Seed the owner with the full owner permission mask, exactly like a real
	// signup (organization.Service.Create). Without this the member row defaults
	// to permissions = 0 and every org-scoped route 403s ("you don't have access
	// to this feature") across the whole dashboard. DO UPDATE (not DO NOTHING) so
	// re-seeding a DB that already has a broken 0-mask owner repairs it.
	_, err := pool.Exec(ctx, `
		INSERT INTO organization_members (organization_id, user_id, role, permissions, accepted_at)
		VALUES ($1, $2, 'owner', $3, NOW())
		ON CONFLICT (organization_id, user_id) DO UPDATE SET
			role = 'owner',
			permissions = EXCLUDED.permissions`,
		sandboxOrg, sandboxUser, models.RolePermissions[models.RoleOwner])
	return err
}

// seedWorker upserts the one worker the native sandbox stack runs (WORKER_ID
// ...1a01, shared tier). email_accounts.worker_id has an FK to workers, so this
// must exist before seedMailboxes assigns the senders to it. Left active so
// placement and the reconciler treat it as live; the real worker process adopts
// the row on its first heartbeat.
func seedWorker(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO workers (id, name, notes, ip_addr, active, worker_type, account_count, free_tier)
		VALUES ($1, 'worker-sandbox-1', 'Sandbox worker (make sandbox / make worker)', '127.0.0.1', TRUE, 'shared', 0, FALSE)
		ON CONFLICT (id) DO UPDATE SET active = TRUE, updated_at = NOW()`,
		sandboxWorker)
	return err
}

func seedMailboxes(ctx context.Context, pool *pgxpool.Pool) error {
	// The free/premium warmup pool rows: nothing else creates them (no
	// migration seeds warmup_pools and the app only joins existing pools), so
	// without this the participant insert below is a silent no-op and warmup
	// partner selection has an empty pool.
	for _, wp := range []struct {
		id       string
		poolType string
		name     string
	}{
		{"77777777-aaaa-0000-0000-000000000001", "free", "Free warmup pool"},
		{"77777777-aaaa-0000-0000-000000000002", "premium", "Premium warmup pool"},
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO warmup_pools (id, pool_type, name, description, max_participants)
			VALUES ($1, $2::warmup_pool_type, $3, 'Seeded by the sandbox', 1000)
			ON CONFLICT (id) DO NOTHING`,
			uuid.MustParse(wp.id), wp.poolType, wp.name); err != nil {
			return fmt.Errorf("warmup pool %s: %w", wp.poolType, err)
		}
	}

	for i, m := range sandboxMailboxes {
		// Each mailbox sits at its cohort's lifecycle point (see profileFor).
		// The send window starts at 00:01, NOT 00:00: the scheduler treats a
		// 00:00 start as unset and defaults the next day's first slot to 8am,
		// which would idle the demo overnight. created_at is backdated so
		// accounts read as established senders.
		p := profileFor(i)
		replyRate := 30 + (i%3)*8
		if _, err := pool.Exec(ctx, `
			INSERT INTO email_accounts (
				id, user_id, organization_id, worker_id,
				email, name, signature_plain, signature_html,
				provider, status,
				campaign_limit, min_wait_time, timezone,
				warmup, warmup_tag, warmup_pool_type, warmup_reply_rate,
				warmup_base, warmup_increase, warmup_max,
				warmup_start_time, warmup_end_time, warmup_paused_at,
				created_at
			) VALUES (
				$1, $2, $3, $4,
				$5, $6, $7, $8,
				'smtp_imap', 'active',
				100, 45, 'UTC',
				NOW() - make_interval(days => $9), 'sandbox', 'premium', $10,
				$11, $12, $13,
				'00:01', '23:59', CASE WHEN $14 THEN NOW() - INTERVAL '2 days' END,
				NOW() - make_interval(days => $15)
			)
			ON CONFLICT (id) DO UPDATE SET
				worker_id = EXCLUDED.worker_id,
				status = 'active',
				signature_plain = EXCLUDED.signature_plain,
				signature_html = EXCLUDED.signature_html,
				campaign_limit = EXCLUDED.campaign_limit,
				min_wait_time = EXCLUDED.min_wait_time,
				warmup = EXCLUDED.warmup,
				warmup_reply_rate = EXCLUDED.warmup_reply_rate,
				warmup_base = EXCLUDED.warmup_base,
				warmup_increase = EXCLUDED.warmup_increase,
				warmup_max = EXCLUDED.warmup_max,
				warmup_start_time = EXCLUDED.warmup_start_time,
				warmup_end_time = EXCLUDED.warmup_end_time,
				warmup_paused_at = EXCLUDED.warmup_paused_at,
				created_at = EXCLUDED.created_at,
				updated_at = NOW()`,
			m.id, sandboxUser, sandboxOrg, sandboxWorker, m.email, m.name,
			signaturePlain(m), signatureHTML(m), p.warmupDaysAgo, replyRate,
			p.warmupBase, p.warmupInc, p.warmupMax, p.paused, p.accountAge); err != nil {
			return fmt.Errorf("mailbox %s: %w", m.email, err)
		}
		// Pool membership with the cohort's evaluated health record, so the
		// health column shows a believable spread instead of zeros.
		if _, err := pool.Exec(ctx, `
			INSERT INTO warmup_pool_participants
				(pool_id, email_account_id, health_state, last_health_score, spam_score, last_health_evaluated_at)
			SELECT id, $1, $2, $3, $4, NOW()
			FROM warmup_pools WHERE pool_type = 'premium'::warmup_pool_type
			ON CONFLICT (pool_id, email_account_id) DO UPDATE SET
				health_state = EXCLUDED.health_state,
				last_health_score = EXCLUDED.last_health_score,
				spam_score = EXCLUDED.spam_score,
				last_health_evaluated_at = NOW(),
				blocked_at = NULL,
				blocked_until = NULL`,
			m.id, p.healthState, p.healthScore, p.spamScore); err != nil {
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
			'cus_sandbox', 'sub_sandbox_pro', 'price_sandbox_pro',
			'active', NOW(), NOW() + INTERVAL '30 days',
			FALSE, NOW(), NOW()
		)
		ON CONFLICT (organization_id) DO UPDATE SET
			plan_id = EXCLUDED.plan_id,
			status = 'active',
			stripe_subscription_id = EXCLUDED.stripe_subscription_id,
			stripe_price_id = EXCLUDED.stripe_price_id,
			current_period_start = EXCLUDED.current_period_start,
			current_period_end = EXCLUDED.current_period_end,
			updated_at = NOW()`,
		sandboxSub, sandboxUser, sandboxOrg, seed.PlanProMonthlyID)
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
	// ageDays backdates created_at so the campaign reads as an established run.
	ageDays int
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
			contactBase: "66666666-aaaa-0000-0001", ageDays: 21,
			contacts: launchContacts,
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
			contactBase: "66666666-aaaa-0000-0002", ageDays: 18,
			contacts: agencyContacts,
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
			contactBase: "66666666-aaaa-0000-0003", ageDays: 5,
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
		// created_at is backdated so the campaign reads as an established run.
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
				NOW(), NOW() - make_interval(days => $6)
			)
			ON CONFLICT (id) DO UPDATE SET
				status = EXCLUDED.status,
				days = EXCLUDED.days,
				start_time = EXCLUDED.start_time,
				end_time = EXCLUDED.end_time,
				created_at = EXCLUDED.created_at,
				updated_at = NOW()`,
			c.id, sandboxUser, sandboxOrg, c.name, c.status, c.ageDays); err != nil {
			return fmt.Errorf("campaign %s: %w", c.name, err)
		}

		for i, s := range c.steps {
			// Steps are only reachable through explicit branch connections
			// (there is NO implicit advance-by-position in the scheduler), so
			// every non-final step carries an unconditional branch to the next
			// one. Without this the campaign "completes" after step 1.
			conditions := `{"branches":[]}` // final step: no route out = STOP
			if i+1 < len(c.steps) {
				conditions = fmt.Sprintf(`{"branches":[{"branch_id":"sbx-%s-%d","target_step_id":"%s"}]}`,
					c.id.String()[:8], i, c.steps[i+1].id)
			}
			if _, err := pool.Exec(ctx, `
				INSERT INTO sequences (
					id, campaign_id, organization_id, name, subject,
					body_plain, body_html, wait_after, position, conditions
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
				ON CONFLICT (id) DO UPDATE SET
					subject = EXCLUDED.subject,
					body_plain = EXCLUDED.body_plain,
					body_html = EXCLUDED.body_html,
					wait_after = EXCLUDED.wait_after,
					position = EXCLUDED.position,
					conditions = EXCLUDED.conditions`,
				s.id, c.id, sandboxOrg, s.name, s.subject, s.body, plainToHTML(s.body), s.waitAfter, i, conditions); err != nil {
				return fmt.Errorf("sequence %s: %w", s.name, err)
			}
		}

		// Explicit sender pool: every sandbox mailbox rotates through the
		// active campaigns (drives the rotation UI and in_campaign detection).
		if c.status == "active" {
			for pos, m := range sandboxMailboxes {
				if _, err := pool.Exec(ctx, `
					INSERT INTO campaign_senders (campaign_id, email_account_id, weight, rotation_position, enabled)
					VALUES ($1, $2, 1, $3, TRUE)
					ON CONFLICT (campaign_id, email_account_id) DO UPDATE SET enabled = TRUE`,
					c.id, m.id, pos); err != nil {
					return fmt.Errorf("campaign sender %s: %w", m.email, err)
				}
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

// deactivateIdleFixtureWorkers deactivates every seeded worker except the one
// the native stack actually runs (sandboxWorker, `make worker` / `make dev` /
// `make sandbox`). Fixture workers are seeded active but never heartbeat, so
// placement keeps choosing them and the dead-worker sweep keeps draining them -
// an assignment ping-pong that strands mailboxes mid-send.
func deactivateIdleFixtureWorkers(ctx context.Context, pool *pgxpool.Pool) error {
	tag, err := pool.Exec(ctx, `
		UPDATE workers SET active = FALSE, updated_at = NOW()
		WHERE active AND id <> $1`,
		sandboxWorker)
	if err != nil {
		return err
	}
	if n := tag.RowsAffected(); n > 0 {
		fmt.Printf("  deactivated %d fixture workers not running in the native stack\n", n)
	}
	return nil
}

// deactivateFixtureMailboxes turns off the base-fixture demo mailboxes (the Acme
// and Globex orgs), which ship with fake, unsealed placeholder credentials and
// point at hosts that don't exist. Left active + worker-assigned, the worker
// reconciler tries to decrypt their placeholder credentials every cycle and logs
// a load failure (invalid-hex on the "seed-fake-..." value). The sandbox runs
// entirely on the real Sunrise mailboxes, so quiet everything else: unassign it
// from a worker and mark it inactive so the reconciler, warmup scheduler, and
// placement all skip it. Idempotent; `make seed` re-activates them for the
// docker sim flow.
func deactivateFixtureMailboxes(ctx context.Context, pool *pgxpool.Pool) error {
	tag, err := pool.Exec(ctx, `
		UPDATE email_accounts
		SET status = 'inactive', worker_id = NULL, updated_at = NOW()
		WHERE organization_id <> $1 AND (status = 'active' OR worker_id IS NOT NULL)`,
		sandboxOrg)
	if err != nil {
		return err
	}
	if n := tag.RowsAffected(); n > 0 {
		fmt.Printf("  deactivated %d fixture mailboxes with placeholder credentials\n", n)
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
