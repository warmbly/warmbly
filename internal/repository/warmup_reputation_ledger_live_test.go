package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Issue #476: everything warmup knows about a mailbox cascades off its row, so
// removing a blocked mailbox and adding it back gave the same address a clean
// standing. These prove the ledger that now holds the standing between rows,
// against the real schema.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_ledger?sslmode=disable \
//	  go test ./internal/repository/ -run LiveReputationLedger -v

type ledgerFixture struct {
	pool    *pgxpool.Pool
	emails  EmailRepository
	warmups WarmupRepository
	user    uuid.UUID
	org     uuid.UUID
	poolID  uuid.UUID
	address string
}

func newLedgerFixture(t *testing.T) *ledgerFixture {
	t.Helper()
	handle, pool := liveContactDB(t)
	ctx := context.Background()
	f := &ledgerFixture{
		pool: pool, emails: NewEmailRepostory(handle, nil), warmups: NewWarmupRepository(pool),
		user: uuid.New(), org: uuid.New(), poolID: uuid.New(),
	}
	f.address = "Ledger-" + f.org.String()[:8] + "@Test.Local"
	f.exec(t, `INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Ledger', 'Test')`,
		f.user, "ledger-"+f.user.String()[:8]+"@test.local")
	f.exec(t, `INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Ledger Test', $2, $3)`,
		f.org, "ledger-"+f.org.String()[:8], f.user)
	f.exec(t, `INSERT INTO warmup_pools (id, pool_type, name) VALUES ($1, 'premium', 'Ledger test pool')`, f.poolID)

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM warmup_reputation_ledger WHERE organization_id = $1`, f.org},
			{`DELETE FROM warmup_pool_participants WHERE email_account_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)`, f.org},
			{`DELETE FROM email_accounts WHERE organization_id = $1`, f.org},
			{`DELETE FROM warmup_pools WHERE id = $1`, f.poolID},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = $1`, f.user},
		} {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})
	_ = ctx
	return f
}

func (f *ledgerFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("fixture %q: %v", sql[:min(70, len(sql))], err)
	}
}

// addMailbox connects the fixture's address as the given user. The address is
// deliberately mixed case: the ledger keys on the normalized form.
func (f *ledgerFixture) addMailbox(t *testing.T, user uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	f.exec(t, `INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain,
	              signature_html, provider, status, campaign_limit, min_wait_time, timezone)
	          VALUES ($1, $2, $3, $4, 'Ledger', '', '', 'smtp_imap', 'active', 50, 600, 'UTC')`,
		id, user, f.org, f.address)
	return id
}

func (f *ledgerFixture) join(t *testing.T, id uuid.UUID) {
	t.Helper()
	if err := f.warmups.MoveToPool(context.Background(), f.poolID, id, "sender_receiver"); err != nil {
		t.Fatalf("MoveToPool: %v", err)
	}
}

func (f *ledgerFixture) penalise(t *testing.T, id uuid.UUID, score int, state string, until time.Time) {
	t.Helper()
	f.exec(t, `UPDATE warmup_pool_participants
	           SET spam_score = $2, health_state = $3, blocked_at = now(), blocked_until = $4, blocked_reason = 'ledger test'
	           WHERE email_account_id = $1`, id, score, state, until)
}

func (f *ledgerFixture) remove(t *testing.T, user, id uuid.UUID) {
	t.Helper()
	if xerr := f.emails.Delete(context.Background(), user.String(), id.String(), 0); xerr != nil {
		t.Fatalf("Delete: %v", xerr)
	}
}

type standing struct {
	score       int
	state       string
	until       *time.Time
	signalsFrom time.Time
}

func (f *ledgerFixture) standing(t *testing.T, id uuid.UUID) standing {
	t.Helper()
	var s standing
	err := f.pool.QueryRow(context.Background(),
		`SELECT spam_score, health_state, blocked_until, health_signals_from FROM warmup_pool_participants WHERE email_account_id = $1`, id).
		Scan(&s.score, &s.state, &s.until, &s.signalsFrom)
	if err != nil {
		t.Fatalf("read standing: %v", err)
	}
	return s
}

func (f *ledgerFixture) ledgerRows(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM warmup_reputation_ledger WHERE organization_id = $1`, f.org).Scan(&n); err != nil {
		t.Fatalf("count ledger: %v", err)
	}
	return n
}

func sameInstant(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	d := a.Sub(*b)
	return d < time.Second && d > -time.Second
}

func TestLiveReputationLedgerCarriesAPenaltyAcrossRemoval(t *testing.T) {
	f := newLedgerFixture(t)
	start := time.Now().UTC()
	until := start.Add(20 * 24 * time.Hour)

	first := f.addMailbox(t, f.user)
	f.join(t, first)
	f.penalise(t, first, 40, "blocked", until)

	f.remove(t, f.user, first)
	if got := f.ledgerRows(t); got != 1 {
		t.Fatalf("ledger rows after removing a blocked mailbox = %d, want 1", got)
	}
	var expires time.Time
	if err := f.pool.QueryRow(context.Background(), `SELECT expires_at FROM warmup_reputation_ledger WHERE organization_id = $1`, f.org).Scan(&expires); err != nil {
		t.Fatalf("read expires_at: %v", err)
	}
	if expires.Before(until.Add(89 * 24 * time.Hour)) {
		t.Fatalf("expires_at %v is not the window past the end of the block (%v)", expires, until)
	}

	second := f.addMailbox(t, f.user)
	f.join(t, second)
	got := f.standing(t, second)
	if got.score != 40 || got.state != "blocked" || !sameInstant(got.until, &until) {
		t.Fatalf("re-added mailbox rejoined as %+v, want the standing it left with (40, blocked, until %v)", got, until)
	}
	if got.signalsFrom.Before(start.Add(-time.Second)) {
		t.Fatalf("health_signals_from %v was carried over; new signals must count from the re-add", got.signalsFrom)
	}
	if rows := f.ledgerRows(t); rows != 0 {
		t.Fatalf("ledger rows after the standing was inherited = %d, want 0 (consumed)", rows)
	}
}

func TestLiveReputationLedgerLeavesNothingForGoodStanding(t *testing.T) {
	f := newLedgerFixture(t)

	first := f.addMailbox(t, f.user)
	f.join(t, first)
	f.remove(t, f.user, first)
	if got := f.ledgerRows(t); got != 0 {
		t.Fatalf("a mailbox in good standing left %d ledger rows, want 0", got)
	}

	second := f.addMailbox(t, f.user)
	f.join(t, second)
	if got := f.standing(t, second); got.score != 0 || got.state != "healthy" {
		t.Fatalf("re-added healthy mailbox rejoined as %+v", got)
	}
}

func TestLiveReputationLedgerLapses(t *testing.T) {
	f := newLedgerFixture(t)
	f.exec(t, `INSERT INTO warmup_reputation_ledger (organization_id, email, spam_score, health_state, expires_at)
	           VALUES ($1, $2, 40, 'blocked', now() - interval '1 hour')`, f.org, "ledger-"+f.org.String()[:8]+"@test.local")

	// Lapsed standing is not inherited, and the row waits for the purge.
	id := f.addMailbox(t, f.user)
	f.join(t, id)
	if got := f.standing(t, id); got.score != 0 || got.state != "healthy" {
		t.Fatalf("a lapsed standing was inherited: %+v", got)
	}
	if got := f.ledgerRows(t); got != 1 {
		t.Fatalf("ledger rows before the purge = %d, want 1", got)
	}

	purged, err := f.warmups.PurgeExpiredReputationLedger(context.Background())
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if purged < 1 {
		t.Fatalf("purge reported %d rows, want at least the lapsed one", purged)
	}
	if got := f.ledgerRows(t); got != 0 {
		t.Fatalf("ledger rows after the purge = %d, want 0", got)
	}
}

// Two members of one workspace can each connect the same address. When both
// are removed, the address must carry the worse standing whichever went last.
func TestLiveReputationLedgerKeepsTheWorseOfTwoRemovals(t *testing.T) {
	f := newLedgerFixture(t)
	other := uuid.New()
	f.exec(t, `INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Ledger', 'Other')`,
		other, "ledger-other-"+other.String()[:8]+"@test.local")
	t.Cleanup(func() { f.exec(t, `DELETE FROM users WHERE id = $1`, other) })
	until := time.Now().UTC().Add(20 * 24 * time.Hour)

	severe := f.addMailbox(t, f.user)
	f.join(t, severe)
	f.penalise(t, severe, 60, "blocked", until)
	mild := f.addMailbox(t, other)
	f.join(t, mild)
	f.exec(t, `UPDATE warmup_pool_participants SET spam_score = 5, health_state = 'watch' WHERE email_account_id = $1`, mild)

	f.remove(t, f.user, severe)
	f.remove(t, other, mild)

	var score int
	var state string
	if err := f.pool.QueryRow(context.Background(), `SELECT spam_score, health_state FROM warmup_reputation_ledger WHERE organization_id = $1`, f.org).Scan(&score, &state); err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if score != 60 || state != "blocked" {
		t.Fatalf("ledger holds (%d, %s) after the milder removal, want the block kept (60, blocked)", score, state)
	}
}
