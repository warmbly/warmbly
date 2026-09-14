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

// Issue #143 end to end: the per-provider placement signal has to reach the
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
	pool     *pgxpool.Pool
	svc      *tasksService
	sender   models.Email
	user     uuid.UUID
	org      uuid.UUID
	atGoogle uuid.UUID
	atMS     uuid.UUID
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

	// A pick is weighted across the WHOLE pool, so a stray participant would
	// dilute the measurement into a meaningless pass.
	var occupied int
	if err := handle.Pool.QueryRow(ctx, `SELECT count(*) FROM warmup_pool_participants WHERE pool_id = $1`, freePoolID).Scan(&occupied); err != nil {
		t.Fatalf("count free pool: %v", err)
	}
	if occupied != 0 {
		t.Skip("free pool already has participants; cannot isolate the measurement")
	}

	f := &partnerRoutingFixture{
		pool: handle.Pool, user: uuid.New(), org: uuid.New(),
		atGoogle: uuid.New(), atMS: uuid.New(),
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
	for _, m := range []struct {
		id     uuid.UUID
		domain string
	}{
		{senderID, "test.local"},
		{f.atGoogle, "gmail.com"},
		{f.atMS, "outlook.com"},
	} {
		exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain,
		          signature_html, provider, status, campaign_limit, min_wait_time, timezone)
		      VALUES ($1, $2, $3, $4, 'Pick', '', '', 'smtp_imap', 'active', 50, 600, 'UTC')`,
			m.id, f.user, f.org, "pick-"+m.id.String()[:8]+"@"+m.domain)
	}
	for _, id := range []uuid.UUID{f.atGoogle, f.atMS} {
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

func (f *partnerRoutingFixture) junked(t *testing.T, domain string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := f.pool.Exec(context.Background(),
			`INSERT INTO warmup_spam_reports (id, reporter_account_id, reported_account_id, message_id,
			     report_type, recipient_domain, created_at)
			 VALUES (gen_random_uuid(), $1, $1, $2, 'spam_placement', $3, NOW() - INTERVAL '1 day')`,
			f.sender.ID, "m-"+uuid.New().String(), domain); err != nil {
			t.Fatalf("insert placement: %v", err)
		}
	}
}

// picks runs the real selector n times and reports how often each partner won.
func (f *partnerRoutingFixture) picks(t *testing.T, n int) (google, microsoft int) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < n; i++ {
		partner, err := f.svc.selectWarmupPartner(ctx, f.sender)
		if err != nil {
			t.Fatalf("selectWarmupPartner: %v", err)
		}
		switch partner.ID {
		case f.atGoogle:
			google++
		case f.atMS:
			microsoft++
		default:
			t.Fatalf("selector returned a mailbox outside the fixture: %s", partner.ID)
		}
	}
	return google, microsoft
}

// The whole point of #143: a sender landing in junk only at Microsoft stops
// being handed Microsoft partners, without an aggregate health band tripping.
func TestLiveWarmupPartnerRoutesAwayFromTheProviderItLandsInJunkAt(t *testing.T) {
	f := newPartnerRoutingFixture(t)
	const rounds = 200

	// Equal history on both sides, so the domain-diversity weight cannot be
	// what moves the split.
	f.history(t, f.atGoogle, 10)
	f.history(t, f.atMS, 10)

	baseGoogle, baseMS := f.picks(t, rounds)
	if baseGoogle < rounds*35/100 || baseGoogle > rounds*65/100 {
		t.Fatalf("baseline split is not even: google %d, microsoft %d of %d", baseGoogle, baseMS, rounds)
	}

	// 6 of the 10 Microsoft sends were filtered into junk. Nothing about the
	// Google side changed.
	f.junked(t, "outlook.com", 6)

	google, microsoft := f.picks(t, rounds)
	// weight ratio is 1 : 1/(1+4*0.6), so google should take ~77%.
	if google <= rounds*60/100 {
		t.Errorf("placement signal did not reach the selector: google %d, microsoft %d of %d (baseline was %d/%d)",
			google, microsoft, rounds, baseGoogle, baseMS)
	}
	// Downweighted, never excluded: a sender that stops mailing a provider
	// entirely can never discover it recovered there.
	if microsoft == 0 {
		t.Errorf("microsoft was excluded outright over %d picks; the penalty must only downweight", rounds)
	}
}
