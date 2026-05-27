// Dev seeder.
//
// Idempotent fixture loader for the local dev/sim stack. Always loads:
//
//   - baseline user dev@warmbly.com / password123 with one org, one
//     shared worker, two connected email accounts (warmup-pooled), and
//     a sample webhook endpoint pointing at the docker-compose
//     webhook-sink service for end-to-end testing
//
// When SEED_RICH=true (default in docker-compose), also loads:
//
//   - 3 orgs across tiers: Acme (free shared), Beta (premium shared),
//     Gamma (dedicated)
//   - 3 workers matching the worker hostnames in docker-compose.yml
//   - 6 email accounts spread across workers, joined to the right warmup pool
//   - one Beta campaign with a 2-step sequence and 10 contacts
//     (2 unsubscribed, so suppression behavior is observable in the UI)
//
// When SEED_FULL=true (also default in docker-compose), additionally runs
// the comprehensive seed in internal/seed: paid plans + Stripe-style
// subscriptions, admin/manager/viewer team users, reply templates, a full
// CRM (pipelines/deals/tasks/notes/activity), an API key, admin audit log
// entries, and an enterprise inquiry. Disjoint from SEED_RICH's IDs, so
// the two can coexist.
//
// Re-running the binary is safe: every insert is guarded by an EXISTS check
// or an ON CONFLICT clause.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/pkg/argon2"
	"github.com/warmbly/warmbly/internal/seed"
)

// Stable UUIDs so fixture data is referenceable across runs.
var (
	userDev   = uuid.MustParse("11111111-0000-0000-0000-000000000001")
	userAcme  = uuid.MustParse("11111111-0000-0000-0000-000000000002")
	userBeta  = uuid.MustParse("11111111-0000-0000-0000-000000000003")
	userGamma = uuid.MustParse("11111111-0000-0000-0000-000000000004")

	orgDev   = uuid.MustParse("22222222-0000-0000-0000-000000000001")
	orgAcme  = uuid.MustParse("22222222-0000-0000-0000-000000000002")
	orgBeta  = uuid.MustParse("22222222-0000-0000-0000-000000000003")
	orgGamma = uuid.MustParse("22222222-0000-0000-0000-000000000004")

	// Match docker-compose.yml worker hostnames so seeded workers correspond
	// to running containers.
	workerShared    = uuid.MustParse("10c8f5e4-1c39-5b2a-9c8b-3d2f0a8b1a01")
	workerPremium   = uuid.MustParse("10c8f5e4-1c39-5b2a-9c8b-3d2f0a8b1a02")
	workerDedicated = uuid.MustParse("10c8f5e4-1c39-5b2a-9c8b-3d2f0a8b1a03")
)

func main() {
	dsn := os.Getenv("PRIMARY_DB")
	if dsn == "" {
		dsn = "postgres://warmbly:warmbly@localhost:5432/warmbly_dev?sslmode=disable"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	if err := seedBaseline(ctx, pool); err != nil {
		log.Fatalf("baseline: %v", err)
	}

	if os.Getenv("SEED_RICH") == "true" {
		if err := seedRich(ctx, pool); err != nil {
			log.Fatalf("rich: %v", err)
		}
	}

	if os.Getenv("SEED_FULL") == "true" {
		result, err := seed.Run(ctx, pool)
		if err != nil {
			log.Fatalf("full: %v", err)
		}
		result.Print(os.Stdout)
	}

	fmt.Println("seed complete")
}

// baseline

func seedBaseline(ctx context.Context, pool *pgxpool.Pool) error {
	if err := upsertUser(ctx, pool, userDev, "Dev", "User", "dev@warmbly.com", "password123"); err != nil {
		return err
	}
	if err := upsertOrg(ctx, pool, orgDev, "Dev's Organization", "dev", userDev); err != nil {
		return err
	}

	// Worker for dev accounts. Matches the docker-compose shared worker
	// hostname so the running worker container can pick the assignments up.
	if err := upsertWorker(ctx, pool, workerShared, "shared-1", "10.0.0.11", "shared", true, true); err != nil {
		return fmt.Errorf("dev worker: %w", err)
	}

	// Connect two email accounts on the dev org. Without these a fresh
	// stack has no mailboxes to test campaigns or warmup against, which
	// is what `make seed` users hit immediately after `make up`.
	devAccounts := []struct {
		id        uuid.UUID
		email     string
		name      string
		warmupTag string
		poolType  string
	}{
		{uuid.MustParse("33333333-0000-0000-0000-0000dddd0001"), "dev.send@warmbly.test", "Dev Sender", "dev-warmup-a", "premium"},
		{uuid.MustParse("33333333-0000-0000-0000-0000dddd0002"), "dev.outbound@warmbly.test", "Dev Outbound", "dev-warmup-b", "premium"},
	}
	for _, a := range devAccounts {
		if err := upsertEmailAccount(ctx, pool, a.id, userDev, orgDev, workerShared, a.email, a.name, a.warmupTag, a.poolType); err != nil {
			return fmt.Errorf("dev email_account %s: %w", a.email, err)
		}
		if err := joinWarmupPool(ctx, pool, a.id, a.poolType); err != nil {
			return fmt.Errorf("dev pool join %s: %w", a.email, err)
		}
	}

	// Sample webhook endpoint. The URL targets a local sink (e.g. an
	// ngrok-style capture or webhook-sink container) so devs can observe
	// outbound deliveries without external setup. Disabled by default so a
	// fresh stack never accidentally retries a nonexistent endpoint forever.
	if err := upsertWebhookEndpoint(ctx, pool,
		uuid.MustParse("77777777-0000-0000-0000-000000000001"),
		orgDev,
		"http://webhook-sink:8080/in",
		"Local dev sink (disabled — enable in /webhooks UI)",
		false,
	); err != nil {
		return fmt.Errorf("dev webhook endpoint: %w", err)
	}

	fmt.Println("baseline ok: dev@warmbly.com / password123")
	fmt.Println("  dev accounts: dev.send@warmbly.test, dev.outbound@warmbly.test (premium pool)")
	fmt.Println("  dev webhook : sample endpoint at http://webhook-sink:8080/in (disabled by default)")
	return nil
}

// rich fixture

func seedRich(ctx context.Context, pool *pgxpool.Pool) error {
	// users + orgs
	users := []struct {
		id                       uuid.UUID
		first, last, email, pass string
	}{
		{userAcme, "Alex", "Free", "alex@acme.test", "password123"},
		{userBeta, "Beth", "Pro", "beth@beta.test", "password123"},
		{userGamma, "Gus", "Dedicated", "gus@gamma.test", "password123"},
	}
	for _, u := range users {
		if err := upsertUser(ctx, pool, u.id, u.first, u.last, u.email, u.pass); err != nil {
			return fmt.Errorf("user %s: %w", u.email, err)
		}
	}

	orgs := []struct {
		id        uuid.UUID
		name      string
		slug      string
		ownerUser uuid.UUID
	}{
		{orgAcme, "Acme (free)", "acme", userAcme},
		{orgBeta, "Beta (pro)", "beta", userBeta},
		{orgGamma, "Gamma (enterprise)", "gamma", userGamma},
	}
	for _, o := range orgs {
		if err := upsertOrg(ctx, pool, o.id, o.name, o.slug, o.ownerUser); err != nil {
			return fmt.Errorf("org %s: %w", o.slug, err)
		}
	}

	// workers
	workers := []struct {
		id       uuid.UUID
		name     string
		ip       string
		wtype    string
		freeTier bool
		active   bool
	}{
		{workerShared, "shared-1", "10.0.0.11", "shared", true, true},
		{workerPremium, "premium-1", "10.0.0.12", "shared", false, true},
		{workerDedicated, "dedicated-1", "10.0.0.13", "dedicated", false, true},
	}
	for _, w := range workers {
		if err := upsertWorker(ctx, pool, w.id, w.name, w.ip, w.wtype, w.freeTier, w.active); err != nil {
			return fmt.Errorf("worker %s: %w", w.name, err)
		}
	}

	// email accounts: 2 per org, assigned to the matching worker
	type acct struct {
		id, user, org, worker uuid.UUID
		email, name, tag      string
		poolType              string // free / premium / nil for dedicated
	}
	accounts := []acct{
		{uuid.MustParse("33333333-0000-0000-0000-000000000001"), userAcme, orgAcme, workerShared,
			"alex.send@acme.test", "Alex Free", "acme-warmup-a", "free"},
		{uuid.MustParse("33333333-0000-0000-0000-000000000002"), userAcme, orgAcme, workerShared,
			"alex.outbound@acme.test", "Alex Free", "acme-warmup-b", "free"},
		{uuid.MustParse("33333333-0000-0000-0000-000000000003"), userBeta, orgBeta, workerPremium,
			"beth.send@beta.test", "Beth Pro", "beta-warmup-a", "premium"},
		{uuid.MustParse("33333333-0000-0000-0000-000000000004"), userBeta, orgBeta, workerPremium,
			"beth.outbound@beta.test", "Beth Pro", "beta-warmup-b", "premium"},
		{uuid.MustParse("33333333-0000-0000-0000-000000000005"), userGamma, orgGamma, workerDedicated,
			"gus.send@gamma.test", "Gus Dedicated", "gamma-warmup-a", "premium"},
		{uuid.MustParse("33333333-0000-0000-0000-000000000006"), userGamma, orgGamma, workerDedicated,
			"gus.outbound@gamma.test", "Gus Dedicated", "gamma-warmup-b", "premium"},
	}
	for _, a := range accounts {
		if err := upsertEmailAccount(ctx, pool, a.id, a.user, a.org, a.worker, a.email, a.name, a.tag, a.poolType); err != nil {
			return fmt.Errorf("email_account %s: %w", a.email, err)
		}
		if err := joinWarmupPool(ctx, pool, a.id, a.poolType); err != nil {
			return fmt.Errorf("join pool for %s: %w", a.email, err)
		}
	}

	// campaign + sequence + contacts for Beta
	campaignID := uuid.MustParse("44444444-0000-0000-0000-000000000001")
	if err := upsertCampaign(ctx, pool, campaignID, userBeta, orgBeta, "Beta Cold Outreach Q1"); err != nil {
		return err
	}
	seq1 := uuid.MustParse("55555555-0000-0000-0000-000000000001")
	seq2 := uuid.MustParse("55555555-0000-0000-0000-000000000002")
	if err := upsertSequence(ctx, pool, seq1, campaignID, orgBeta, "Intro", "Quick question, {{first_name}}",
		"Hi {{first_name}}, ...intro body...", 0); err != nil {
		return err
	}
	if err := upsertSequence(ctx, pool, seq2, campaignID, orgBeta, "Follow-up", "Re: Quick question",
		"Hi {{first_name}}, following up...", 3); err != nil {
		return err
	}

	// 10 contacts; index 2 and 7 are unsubscribed to exercise suppression
	for i := 1; i <= 10; i++ {
		cid := uuid.MustParse(fmt.Sprintf("66666666-0000-0000-0000-%012d", i))
		subscribed := i != 2 && i != 7
		if err := upsertContact(ctx, pool, cid, userBeta, orgBeta,
			fmt.Sprintf("Lead%d", i), "Beta",
			fmt.Sprintf("lead%d@prospect.test", i),
			"Prospect Inc", subscribed); err != nil {
			return fmt.Errorf("contact %d: %w", i, err)
		}
		if err := upsertCampaignLead(ctx, pool, campaignID, cid); err != nil {
			return fmt.Errorf("campaign_lead %d: %w", i, err)
		}
	}

	fmt.Println("rich fixture loaded:")
	fmt.Println("  users     dev@warmbly.com, alex@acme.test, beth@beta.test, gus@gamma.test (pw: password123)")
	fmt.Println("  workers   shared / premium / dedicated (match docker-compose hostnames)")
	fmt.Println("  accounts  2 per org, joined to free or premium warmup pool")
	fmt.Println("  campaign  Beta Cold Outreach Q1 (2-step sequence, 10 contacts, 2 unsubscribed)")
	return nil
}

// helpers

func upsertUser(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, first, last, email, password string) error {
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)", email).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	hash, err := argon2.Hash(password)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO users (id, first_name, last_name, email, password_hash)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO NOTHING`,
		id, first, last, email, hash)
	return err
}

func upsertOrg(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, name, slug string, owner uuid.UUID) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO organizations (id, name, slug, owner_user_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (id) DO NOTHING`,
		id, name, slug, owner); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO organization_members (organization_id, user_id, role, accepted_at)
		VALUES ($1, $2, 'owner', NOW())
		ON CONFLICT DO NOTHING`,
		id, owner); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func upsertWorker(ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, name, ip, wtype string, freeTier, active bool) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO workers (id, name, ip_addr, worker_type, free_tier, active)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			ip_addr = EXCLUDED.ip_addr,
			worker_type = EXCLUDED.worker_type,
			free_tier = EXCLUDED.free_tier,
			active = EXCLUDED.active,
			updated_at = NOW()`,
		id, name, ip, wtype, freeTier, active)
	return err
}

func upsertEmailAccount(ctx context.Context, pool *pgxpool.Pool, id, userID, orgID, workerID uuid.UUID,
	email, name, warmupTag, poolType string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO email_accounts (
			id, user_id, organization_id, worker_id,
			email, name,
			signature_plain, signature_html,
			provider, status,
			warmup_tag, warmup_pool_type
		) VALUES (
			$1, $2, $3, $4,
			$5, $6,
			'', '',
			'smtp_imap', 'active',
			$7, $8
		)
		ON CONFLICT (id) DO NOTHING`,
		id, userID, orgID, workerID, email, name, warmupTag, poolType)
	return err
}

func joinWarmupPool(ctx context.Context, pool *pgxpool.Pool, accountID uuid.UUID, poolType string) error {
	if poolType == "" {
		return nil
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO warmup_pool_participants (pool_id, email_account_id)
		SELECT id, $1 FROM warmup_pools WHERE pool_type = $2::warmup_pool_type
		ON CONFLICT DO NOTHING`,
		accountID, poolType)
	return err
}

func upsertCampaign(ctx context.Context, pool *pgxpool.Pool, id, userID, orgID uuid.UUID, name string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO campaigns (
			id, user_id, organization_id, name, description,
			status, days, start_time, end_time, timezone,
			updated_at, created_at
		) VALUES (
			$1, $2, $3, $4, 'Seeded sample campaign',
			'active', 31, '09:00', '17:00', 'UTC',
			NOW(), NOW()
		)
		ON CONFLICT (id) DO NOTHING`,
		id, userID, orgID, name)
	return err
}

func upsertSequence(ctx context.Context, pool *pgxpool.Pool, id, campaignID, orgID uuid.UUID,
	name, subject, body string, waitAfter int) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO sequences (
			id, campaign_id, organization_id, name, subject,
			body_plain, body_html, wait_after
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $6, $7
		)
		ON CONFLICT (id) DO NOTHING`,
		id, campaignID, orgID, name, subject, body, waitAfter)
	return err
}

func upsertContact(ctx context.Context, pool *pgxpool.Pool, id, userID, orgID uuid.UUID,
	first, last, email, company string, subscribed bool) error {
	custom, _ := json.Marshal(map[string]any{})
	_, err := pool.Exec(ctx, `
		INSERT INTO contacts (
			id, user_id, organization_id,
			first_name, last_name, email, company, phone,
			custom_fields, subscribed
		) VALUES (
			$1, $2, $3,
			$4, $5, $6, $7, '',
			$8, $9
		)
		ON CONFLICT (id) DO NOTHING`,
		id, userID, orgID, first, last, email, company, custom, subscribed)
	return err
}

// upsertWebhookEndpoint inserts a sample webhook subscription. Uses a
// deterministic UUID so re-running the seed is idempotent. The secret is
// fixed in dev — never use this value in production.
func upsertWebhookEndpoint(ctx context.Context, pool *pgxpool.Pool, id, orgID uuid.UUID, url, description string, enabled bool) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO webhook_endpoints (
			id, organization_id, url, description, secret, event_types, enabled
		) VALUES (
			$1, $2, $3, $4, 'whsec_dev_seed_do_not_use_in_prod', '{}', $5
		)
		ON CONFLICT (id) DO NOTHING`,
		id, orgID, url, description, enabled)
	return err
}

func upsertCampaignLead(ctx context.Context, pool *pgxpool.Pool, campaignID, contactID uuid.UUID) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO campaign_leads (campaign_id, contact_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING`,
		campaignID, contactID)
	return err
}
