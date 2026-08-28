package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Issue #143: partner selection needs THIS sender's record per recipient
// provider, not the pool-wide rollup the admin overview reads. These prove the
// two queries that feed it against the real schema.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveProviderRouting -v

// premiumPoolID is the seeded premium pool. Pools are seed data, not migration
// data, so a database without them skips rather than fails.
const premiumPoolID = "77777777-aaaa-0000-0000-000000000002"

type providerRoutingFixture struct {
	pool      *pgxpool.Pool
	user      uuid.UUID
	org       uuid.UUID
	sender    uuid.UUID
	atGoogle  uuid.UUID
	atMSGraph uuid.UUID
}

func newProviderRoutingFixture(t *testing.T) *providerRoutingFixture {
	t.Helper()
	_, pool := liveContactDB(t)
	ctx := context.Background()

	var pools int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM warmup_pools WHERE id = $1`, premiumPoolID).Scan(&pools); err != nil || pools == 0 {
		t.Skip("premium warmup pool not seeded in this database")
	}

	f := &providerRoutingFixture{
		pool: pool, user: uuid.New(), org: uuid.New(),
		sender: uuid.New(), atGoogle: uuid.New(), atMSGraph: uuid.New(),
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Route', 'Test')`,
		f.user, "route-"+f.user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Route Test', $2, $3)`,
		f.org, "route-"+f.org.String()[:8], f.user)

	// The recipient DOMAIN is what the placement signal keys on, so these carry
	// real provider domains; the connect method is deliberately different from
	// it on the sender to prove the two are not confused.
	for _, m := range []struct {
		id       uuid.UUID
		provider string
		domain   string
	}{
		{f.sender, "smtp_imap", "test.local"},
		{f.atGoogle, "gmail", "gmail.com"},
		{f.atMSGraph, "smtp_imap", "outlook.com"},
	} {
		exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain,
		          signature_html, provider, status, campaign_limit, min_wait_time, timezone)
		      VALUES ($1, $2, $3, $4, 'Route', '', '', $5, 'active', 50, 600, 'UTC')`,
			m.id, f.user, f.org, "route-"+m.id.String()[:8]+"@"+m.domain, m.provider)
	}
	// Only the two recipients join the pool; the sender does not, so the
	// participants map must contain exactly them.
	for _, id := range []uuid.UUID{f.atGoogle, f.atMSGraph} {
		exec(`INSERT INTO warmup_pool_participants (pool_id, email_account_id, participant_role, health_state)
		      VALUES ($1, $2, 'sender_receiver', 'healthy')`, premiumPoolID, id)
	}

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM warmup_spam_reports WHERE reported_account_id = $1`, f.sender},
			{`DELETE FROM warmup_tokens WHERE sender_account_id = $1`, f.sender},
			{`DELETE FROM tasks WHERE email_account_id = $1`, f.sender},
			{`DELETE FROM warmup_pool_participants WHERE email_account_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)`, f.org},
			{`DELETE FROM email_accounts WHERE organization_id = $1`, f.org},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = $1`, f.user},
		} {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})
	return f
}

func (f *providerRoutingFixture) send(t *testing.T, recipient uuid.UUID, n int, status string) {
	t.Helper()
	for i := 0; i < n; i++ {
		// warmup_tokens.task_id is a real foreign key, so each send needs the
		// task it belongs to.
		taskID := uuid.New()
		if _, err := f.pool.Exec(context.Background(),
			`INSERT INTO tasks (id, task_type, email_account_id, status, message_id)
			 VALUES ($1, 'warmup', $2, $3, '')`, taskID, f.sender, status); err != nil {
			t.Fatalf("insert task: %v", err)
		}
		if _, err := f.pool.Exec(context.Background(),
			`INSERT INTO warmup_tokens (token, task_id, sender_account_id, recipient_account_id, created_at)
			 VALUES (gen_random_uuid(), $1, $2, $3, NOW())`,
			taskID, f.sender, recipient); err != nil {
			t.Fatalf("insert token: %v", err)
		}
	}
}

func (f *providerRoutingFixture) placement(t *testing.T, domain string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := f.pool.Exec(context.Background(),
			`INSERT INTO warmup_spam_reports (id, reporter_account_id, reported_account_id, message_id,
			     report_type, recipient_domain, created_at)
			 VALUES (gen_random_uuid(), $1, $1, $2, 'spam_placement', $3, NOW())`,
			f.sender, "m-"+uuid.New().String(), domain); err != nil {
			t.Fatalf("insert placement: %v", err)
		}
	}
}

func TestLiveProviderRoutingSegmentsOneSendersRecord(t *testing.T) {
	handle, _ := liveContactDB(t)
	f := newProviderRoutingFixture(t)
	repo := NewWarmupRepository(handle.Pool)
	ctx := context.Background()

	f.send(t, f.atGoogle, 20, "completed")
	f.send(t, f.atMSGraph, 20, "completed")
	f.placement(t, "outlook.com", 10) // failing only at Microsoft

	got, err := repo.SenderPlacementByProvider(ctx, f.sender, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("SenderPlacementByProvider: %v", err)
	}
	if g := got["google"]; g.Sends != 20 || g.Placements != 0 || g.Rate() != 0 {
		t.Errorf("google = %+v (rate %v), want 20 sends and nothing in junk", g, g.Rate())
	}
	if m := got["microsoft"]; m.Sends != 20 || m.Placements != 10 || m.Rate() != 0.5 {
		t.Errorf("microsoft = %+v (rate %v), want 20 sends, 10 placements, 0.5", m, m.Rate())
	}
}

func TestLiveProviderRoutingWindowExcludesOldSignal(t *testing.T) {
	handle, _ := liveContactDB(t)
	f := newProviderRoutingFixture(t)
	repo := NewWarmupRepository(handle.Pool)
	ctx := context.Background()

	f.send(t, f.atMSGraph, 5, "completed")
	f.placement(t, "outlook.com", 5)

	// A window that starts after everything was written must see nothing, or
	// a sender would be penalized forever for a provider it has recovered at.
	got, err := repo.SenderPlacementByProvider(ctx, f.sender, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("SenderPlacementByProvider: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("out-of-window rows returned: %+v", got)
	}
}

func TestLiveProviderRoutingParticipantProviders(t *testing.T) {
	handle, _ := liveContactDB(t)
	f := newProviderRoutingFixture(t)
	repo := NewWarmupRepository(handle.Pool)

	got, err := repo.GetPoolParticipantProviders(context.Background(), "premium", true)
	if err != nil {
		t.Fatalf("GetPoolParticipantProviders: %v", err)
	}
	if got[f.atGoogle] != "google" {
		t.Errorf("gmail.com participant = %q, want google", got[f.atGoogle])
	}
	// Connects over plain IMAP, but its mail is run by Microsoft. Keying on
	// the connect method would have filed this under smtp_imap.
	if got[f.atMSGraph] != "microsoft" {
		t.Errorf("outlook.com participant = %q, want microsoft", got[f.atMSGraph])
	}
	// The sender never joined the pool, so it must not appear as a partner.
	if _, ok := got[f.sender]; ok {
		t.Error("a non-participant mailbox is being offered as a warmup partner")
	}
}

// A token is written before the send goes out, so a failed send leaves one
// behind. Counting it would put the failure in the denominator and understate
// the provider's junk rate exactly when the sender is doing worst.
func TestLiveProviderRoutingIgnoresFailedSends(t *testing.T) {
	handle, _ := liveContactDB(t)
	f := newProviderRoutingFixture(t)
	repo := NewWarmupRepository(handle.Pool)

	f.send(t, f.atMSGraph, 10, "completed")
	f.send(t, f.atMSGraph, 90, "failed")
	f.placement(t, "outlook.com", 5)

	got, err := repo.SenderPlacementByProvider(context.Background(), f.sender, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("SenderPlacementByProvider: %v", err)
	}
	m := got["microsoft"]
	if m.Sends != 10 {
		t.Errorf("sends = %d, want only the 10 that completed", m.Sends)
	}
	if m.Rate() != 0.5 {
		t.Errorf("rate = %v, want 0.5; counting the failures would have read 0.05", m.Rate())
	}
}
