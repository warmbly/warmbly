package tasks

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/repository"
)

// Issue #143 end to end: the per-host placement signal has to reach the
// partner the selector actually returns, not just the query that computes it.
// The repository tests prove the numbers; this proves selectWarmupPartner
// wires them into the weight. Skipped unless WARMBLY_TEST_DB is set:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/tasks/ -run LiveWarmupPartner -v

// freePoolID is the free pool migration 000156 seeds on every instance. The
// test needs it empty, which a scratch database gives and a `make seed` one
// does not (the dev fixtures join two mailboxes to it), so it skips there.
var freePoolID = models.WarmupPoolFreeID

type partnerRoutingFixture struct {
	pool        *pgxpool.Pool
	svc         *tasksService
	sender      models.Email
	user        uuid.UUID
	org         uuid.UUID
	atWorkspace uuid.UUID
	atSmallHost uuid.UUID
}

// requireEmptyPool skips when the pool has members: a pick is weighted across
// the whole pool, so a stray participant would dilute the measurement.
func requireEmptyPool(t *testing.T, pool *pgxpool.Pool, poolID uuid.UUID) {
	t.Helper()
	var occupied int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM warmup_pool_participants WHERE pool_id = $1`, poolID).Scan(&occupied); err != nil {
		t.Fatalf("count pool %s: %v", poolID, err)
	}
	if occupied != 0 {
		t.Skipf("pool %s already has participants; cannot isolate the measurement", poolID)
	}
}

func newPartnerRoutingFixture(t *testing.T) *partnerRoutingFixture {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	ctx := context.Background()
	handle, err := db.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { handle.Pool.Close() })

	requireEmptyPool(t, handle.Pool, freePoolID)

	f := &partnerRoutingFixture{
		pool: handle.Pool, user: uuid.New(), org: uuid.New(),
		atWorkspace: uuid.New(), atSmallHost: uuid.New(),
	}
	senderID := uuid.New()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := handle.Pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Pick', 'Test')`,
		f.user, "pick-"+f.user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Pick Test', $2, $3)`,
		f.org, "pick-"+f.org.String()[:8], f.user)
	// Both partners are on custom domains, so only the detected host can tell
	// them apart.
	for _, m := range []struct {
		id     uuid.UUID
		domain string
		host   string
	}{
		{senderID, "test.local", ""},
		{f.atWorkspace, "acme.test", "google_workspace"},
		{f.atSmallHost, "shop.test", "hostinger"},
	} {
		exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain,
		          signature_html, provider, status, campaign_limit, min_wait_time, timezone, mail_host)
		      VALUES ($1, $2, $3, $4, 'Pick', '', '', 'smtp_imap', 'active', 50, 600, 'UTC', $5)`,
			m.id, f.user, f.org, "pick-"+m.id.String()[:8]+"@"+m.domain, m.host)
	}
	for _, id := range []uuid.UUID{f.atWorkspace, f.atSmallHost} {
		exec(`INSERT INTO warmup_pool_participants (pool_id, email_account_id, participant_role, health_state)
		      VALUES ($1, $2, 'sender_receiver', 'healthy')`, freePoolID, id)
	}

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM warmup_spam_reports WHERE reported_account_id = $1`, senderID},
			{`DELETE FROM warmup_tokens WHERE sender_account_id = $1`, senderID},
			{`DELETE FROM tasks WHERE email_account_id = $1`, senderID},
			{`DELETE FROM warmup_pool_participants WHERE email_account_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)`, f.org},
			{`DELETE FROM email_accounts WHERE organization_id = $1`, f.org},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = $1`, f.user},
		} {
			if _, err := handle.Pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})

	enc, err := encrypt.NewEncrypter([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("encrypter: %v", err)
	}
	f.svc = &tasksService{
		warmupRepo: repository.NewWarmupRepository(handle.Pool),
		emailRepo:  repository.NewEmailRepostory(handle, enc),
	}
	// The stored tier picks the pool outright, and the risk read ahead of it
	// fails open, so the selector runs with no feature gate or org-risk repo.
	f.sender = models.Email{ID: senderID, OrganizationID: &f.org, WarmupPoolType: "free"}
	return f
}

// history writes n completed warmup sends to one partner, backdated two days:
// inside the seven-day placement window, but outside the same-day exclusion
// that would drop both candidates before weighting ever runs.
func (f *partnerRoutingFixture) history(t *testing.T, recipient uuid.UUID, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		taskID := uuid.New()
		if _, err := f.pool.Exec(context.Background(),
			`INSERT INTO tasks (id, task_type, email_account_id, status, message_id)
			 VALUES ($1, 'warmup', $2, 'completed', '')`, taskID, f.sender.ID); err != nil {
			t.Fatalf("insert task: %v", err)
		}
		if _, err := f.pool.Exec(context.Background(),
			`INSERT INTO warmup_tokens (token, task_id, sender_account_id, recipient_account_id, created_at)
			 VALUES (gen_random_uuid(), $1, $2, $3, NOW() - INTERVAL '2 days')`,
			taskID, f.sender.ID, recipient); err != nil {
			t.Fatalf("insert token: %v", err)
		}
	}
}

// placed writes a day of verified placement at one host, as the consumer
// counts it.
func (f *partnerRoutingFixture) placed(t *testing.T, group, host string, inbox, spam int) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO warmup_placement_daily (sender_account_id, date, recipient_group, recipient_host, inbox, spam)
		 VALUES ($1, (NOW() AT TIME ZONE 'UTC')::date - 1, $2, $3, $4, $5)`,
		f.sender.ID, group, host, inbox, spam); err != nil {
		t.Fatalf("insert placement: %v", err)
	}
}

// picks runs the real selector n times and reports how often each partner won.
func (f *partnerRoutingFixture) picks(t *testing.T, n int) (workspace, smallHost int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		partner, err := f.svc.selectWarmupPartner(ctx, f.sender)
		if err != nil {
			t.Fatalf("selectWarmupPartner: %v", err)
		}
		switch partner.ID {
		case f.atWorkspace:
			workspace++
		case f.atSmallHost:
			smallHost++
		default:
			t.Fatalf("selector returned a mailbox outside the fixture: %s", partner.ID)
		}
	}
	return workspace, smallHost
}

// The whole point of #143: a sender landing in junk only at one host stops
// being handed partners there, without an aggregate health band tripping, and
// without costing a partner on another host that shares no domain with it.
func TestLiveWarmupPartnerRoutesAwayFromTheHostItLandsInJunkAt(t *testing.T) {
	f := newPartnerRoutingFixture(t)
	const rounds = 200

	// Equal history on both sides, so the domain-diversity weight cannot be
	// what moves the split.
	f.history(t, f.atWorkspace, 10)
	f.history(t, f.atSmallHost, 10)
	f.placed(t, "google", "google_workspace", 10, 0)

	baseWorkspace, baseSmall := f.picks(t, rounds)
	if baseWorkspace < rounds*35/100 || baseWorkspace > rounds*65/100 {
		t.Fatalf("baseline split is not even: workspace %d, small host %d of %d", baseWorkspace, baseSmall, rounds)
	}

	// 6 of the 10 verified arrivals at the small host were filed into junk.
	// Nothing about the Workspace side changed.
	f.placed(t, "other", "hostinger", 4, 6)

	workspace, smallHost := f.picks(t, rounds)
	// weight ratio is 1 : 1/(1+4*0.6), so the Workspace partner should take ~77%.
	if workspace <= rounds*60/100 {
		t.Errorf("placement signal did not reach the selector: workspace %d, small host %d of %d (baseline was %d/%d)",
			workspace, smallHost, rounds, baseWorkspace, baseSmall)
	}
	// Downweighted, never excluded: a sender that stops mailing a host
	// entirely can never discover it recovered there.
	if smallHost == 0 {
		t.Errorf("the small host was excluded outright over %d picks; the penalty must only downweight", rounds)
	}
}
