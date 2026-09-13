package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// Issue #476: everything warmup knows about a mailbox cascades off its row,
// and the pool row alone dies on paths that never touch the mailbox, so a
// penalised address rejoined clean. These prove the mirror that now holds the
// standing by address (migration 000152), and the floor that keeps a block for
// its term, against the real schema.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_ledger?sslmode=disable \
//	  go test ./internal/repository/ -run 'LiveReputation|LiveHealthFloor' -v

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
	f := &ledgerFixture{
		pool: pool, emails: NewEmailRepostory(handle, nil), warmups: NewWarmupRepository(pool),
		user: uuid.New(), org: uuid.New(), poolID: uuid.New(),
	}
	// Mixed case on purpose: the mirror keys on the normalized form.
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
			{`DELETE FROM warmup_pool_participants WHERE email_account_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)`, f.org},
			{`DELETE FROM warmup_reputation_ledger WHERE organization_id = $1`, f.org},
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
	return f
}

func (f *ledgerFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("fixture %q: %v", sql[:min(70, len(sql))], err)
	}
}

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

// penalise writes a standing the way the sweep or a tampering block would; the
// trigger mirrors it. A nil until is the review-required block.
func (f *ledgerFixture) penalise(t *testing.T, id uuid.UUID, score int, state string, until *time.Time) {
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
	blockedAt   *time.Time
	signalsFrom time.Time
}

func (f *ledgerFixture) standing(t *testing.T, id uuid.UUID) standing {
	t.Helper()
	var s standing
	err := f.pool.QueryRow(context.Background(),
		`SELECT spam_score, health_state, blocked_until, blocked_at, health_signals_from FROM warmup_pool_participants WHERE email_account_id = $1`, id).
		Scan(&s.score, &s.state, &s.until, &s.blockedAt, &s.signalsFrom)
	if err != nil {
		t.Fatalf("read standing: %v", err)
	}
	return s
}

type mirror struct {
	score      int
	state      string
	until      *time.Time
	recordedAt time.Time
	standing   *time.Time
}

// mirrorRow returns the address's mirror row, or nil when there is none.
func (f *ledgerFixture) mirrorRow(t *testing.T) *mirror {
	t.Helper()
	var m mirror
	err := f.pool.QueryRow(context.Background(),
		`SELECT spam_score, health_state, blocked_until, recorded_at, standing_until FROM warmup_reputation_ledger WHERE organization_id = $1`, f.org).
		Scan(&m.score, &m.state, &m.until, &m.recordedAt, &m.standing)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil
		}
		t.Fatalf("read mirror: %v", err)
	}
	return &m
}

func sameInstant(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	d := a.Sub(*b)
	return d < time.Second && d > -time.Second
}

func ptr(t time.Time) *time.Time { return &t }

func TestLiveReputationMirrorFollowsTheAddressAcrossRemoval(t *testing.T) {
	f := newLedgerFixture(t)
	start := time.Now().UTC()
	until := start.Add(20 * 24 * time.Hour)

	first := f.addMailbox(t, f.user)
	f.join(t, first)
	if m := f.mirrorRow(t); m != nil {
		t.Fatalf("a fresh healthy member left a mirror row: %+v", m)
	}
	f.penalise(t, first, 40, "blocked", &until)
	m := f.mirrorRow(t)
	if m == nil || m.score != 40 || m.state != "blocked" || !sameInstant(m.until, &until) || !sameInstant(m.standing, &until) {
		t.Fatalf("the trigger did not mirror the penalty: %+v", m)
	}

	written := m.recordedAt
	time.Sleep(20 * time.Millisecond)
	f.remove(t, f.user, first)
	m = f.mirrorRow(t)
	if m == nil {
		t.Fatal("removing the mailbox lost its standing")
	}
	if !m.recordedAt.After(written) {
		t.Fatalf("removal did not restart the retention window: recorded_at %v, written %v", m.recordedAt, written)
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
	if f.mirrorRow(t) == nil {
		t.Fatal("the seed consumed the mirror; it must stay, re-mirrored from the new row")
	}
}

func TestLiveReputationMirrorLeavesNothingForGoodStanding(t *testing.T) {
	f := newLedgerFixture(t)
	first := f.addMailbox(t, f.user)
	f.join(t, first)
	f.remove(t, f.user, first)
	if m := f.mirrorRow(t); m != nil {
		t.Fatalf("a mailbox in good standing left a mirror row: %+v", m)
	}
	second := f.addMailbox(t, f.user)
	f.join(t, second)
	if got := f.standing(t, second); got.score != 0 || got.state != "healthy" {
		t.Fatalf("re-added healthy mailbox rejoined as %+v", got)
	}
}

// An admin unblock is a write to the pool row like any other; the mirror must
// follow it, or the next re-add would resurrect a block that was lifted.
func TestLiveReputationMirrorClearsOnRecovery(t *testing.T) {
	f := newLedgerFixture(t)
	id := f.addMailbox(t, f.user)
	f.join(t, id)
	f.penalise(t, id, 40, "blocked", ptr(time.Now().Add(20*24*time.Hour)))
	if f.mirrorRow(t) == nil {
		t.Fatal("penalty was not mirrored")
	}
	f.exec(t, `UPDATE warmup_pool_participants SET spam_score = 0, health_state = 'healthy', blocked_at = NULL, blocked_until = NULL, blocked_reason = NULL WHERE email_account_id = $1`, id)
	if m := f.mirrorRow(t); m != nil {
		t.Fatalf("recovery left a mirror row: %+v", m)
	}
}

// Leaving the pool without removing the mailbox (auth error, lapsed plan,
// warmup toggled off) was the hole that survived the first design: the pool
// row died with its standing and nothing was written anywhere.
func TestLiveReputationMirrorSurvivesLeavingThePool(t *testing.T) {
	f := newLedgerFixture(t)
	until := time.Now().UTC().Add(20 * 24 * time.Hour)
	id := f.addMailbox(t, f.user)
	f.join(t, id)
	f.penalise(t, id, 40, "blocked", &until)
	mirrored := f.mirrorRow(t)
	if mirrored == nil {
		t.Fatal("penalty was not mirrored")
	}
	written := mirrored.recordedAt
	time.Sleep(20 * time.Millisecond)

	if err := f.warmups.LeaveAllPools(context.Background(), id); err != nil {
		t.Fatalf("LeaveAllPools: %v", err)
	}
	m := f.mirrorRow(t)
	if m == nil || m.state != "blocked" {
		t.Fatalf("leaving the pool lost the standing: %+v", m)
	}
	if !m.recordedAt.After(written) {
		t.Fatalf("leaving did not restart the retention window: recorded_at %v, written %v", m.recordedAt, written)
	}
	f.join(t, id)
	if got := f.standing(t, id); got.state != "blocked" || got.score != 40 || !sameInstant(got.until, &until) {
		t.Fatalf("rejoined the pool as %+v, want the block it left under", got)
	}
}

// A block that requires review has no end, and must never lapse into a clean
// re-add by the calendar.
func TestLiveReputationMirrorNeverLapsesAReviewBlock(t *testing.T) {
	f := newLedgerFixture(t)
	id := f.addMailbox(t, f.user)
	f.join(t, id)
	f.penalise(t, id, 60, "blocked", nil)
	m := f.mirrorRow(t)
	if m == nil || m.standing != nil {
		t.Fatalf("a review block must mirror with no standing_until: %+v", m)
	}
	f.remove(t, f.user, id)
	f.exec(t, `UPDATE warmup_reputation_ledger SET recorded_at = now() - interval '400 days' WHERE organization_id = $1`, f.org)
	if _, err := f.warmups.PurgeExpiredReputationLedger(context.Background()); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if f.mirrorRow(t) == nil {
		t.Fatal("the purge forgot a block that requires review")
	}
	again := f.addMailbox(t, f.user)
	f.join(t, again)
	if got := f.standing(t, again); got.state != "blocked" || got.until != nil {
		t.Fatalf("re-added under a review block as %+v", got)
	}
}

func TestLiveReputationMirrorLapsesOnlyWithoutALiveRow(t *testing.T) {
	f := newLedgerFixture(t)
	id := f.addMailbox(t, f.user)
	f.join(t, id)
	f.penalise(t, id, 40, "quarantined", ptr(time.Now().Add(7*24*time.Hour)))
	age := func() {
		f.exec(t, `UPDATE warmup_reputation_ledger SET standing_until = now() - interval '200 days', recorded_at = now() - interval '200 days' WHERE organization_id = $1`, f.org)
	}

	// A live pool row backs the standing: the purge must not forget it, however
	// old the row looks.
	age()
	if _, err := f.warmups.PurgeExpiredReputationLedger(context.Background()); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if f.mirrorRow(t) == nil {
		t.Fatal("the purge forgot a standing that a live pool row still backs")
	}

	// Removed and lapsed: forgotten, and reported.
	f.remove(t, f.user, id)
	age()
	purged, err := f.warmups.PurgeExpiredReputationLedger(context.Background())
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if purged < 1 || f.mirrorRow(t) != nil {
		t.Fatalf("purge reported %d and left %+v; want the lapsed row gone", purged, f.mirrorRow(t))
	}
}

// A lapsed standing is not inherited, and the healthy rejoin clears it: the
// address's standing is what its live rows say.
func TestLiveReputationMirrorDoesNotInheritALapsedStanding(t *testing.T) {
	f := newLedgerFixture(t)
	id := f.addMailbox(t, f.user)
	f.join(t, id)
	f.penalise(t, id, 40, "quarantined", ptr(time.Now().Add(7*24*time.Hour)))
	f.remove(t, f.user, id)
	f.exec(t, `UPDATE warmup_reputation_ledger SET standing_until = now() - interval '200 days', recorded_at = now() - interval '200 days' WHERE organization_id = $1`, f.org)

	again := f.addMailbox(t, f.user)
	f.join(t, again)
	if got := f.standing(t, again); got.state != "healthy" || got.score != 0 {
		t.Fatalf("a lapsed standing was inherited: %+v", got)
	}
	if m := f.mirrorRow(t); m != nil {
		t.Fatalf("a healthy rejoin left the lapsed mirror in place: %+v", m)
	}
}

// Two members of one workspace can each connect the same address. The mirror
// is the worse of the two, and a recovery on the milder row cannot clear it.
func TestLiveReputationMirrorKeepsTheWorseOfTwoLiveRows(t *testing.T) {
	f := newLedgerFixture(t)
	other := uuid.New()
	f.exec(t, `INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Ledger', 'Other')`,
		other, "ledger-other-"+other.String()[:8]+"@test.local")
	t.Cleanup(func() { f.exec(t, `DELETE FROM users WHERE id = $1`, other) })

	severe := f.addMailbox(t, f.user)
	f.join(t, severe)
	f.penalise(t, severe, 60, "blocked", nil)
	mild := f.addMailbox(t, other)
	f.join(t, mild)
	f.exec(t, `UPDATE warmup_pool_participants SET spam_score = 5, health_state = 'watch' WHERE email_account_id = $1`, mild)

	if m := f.mirrorRow(t); m == nil || m.state != "blocked" || m.score != 60 {
		t.Fatalf("mirror after the milder write = %+v, want the block kept (60, blocked)", m)
	}
	f.exec(t, `UPDATE warmup_pool_participants SET spam_score = 0, health_state = 'healthy' WHERE email_account_id = $1`, mild)
	if m := f.mirrorRow(t); m == nil || m.state != "blocked" {
		t.Fatalf("the milder row's recovery cleared a block the other row still holds: %+v", m)
	}
}

// The floor. The bands read seven-day windows against 30-day blocks, so a
// decision from fresh metrics used to release every block within a week, and
// a re-added mailbox with no history on the next sweep.
func TestLiveHealthFloorHoldsAQuarantineOrBlockForItsTerm(t *testing.T) {
	f := newLedgerFixture(t)
	ctx := context.Background()
	id := f.addMailbox(t, f.user)
	f.join(t, id)
	long := time.Now().UTC().Add(90 * 24 * time.Hour)
	short := time.Now().UTC().Add(30 * 24 * time.Hour)

	f.penalise(t, id, 40, "blocked", &long)
	before := f.standing(t, id)

	// A clean reading does not release the block, and does not restart it.
	if err := f.warmups.UpdateParticipantHealth(ctx, id, models.WarmupHealthHealthy, nil, "", 0.5); err != nil {
		t.Fatalf("UpdateParticipantHealth(healthy): %v", err)
	}
	got := f.standing(t, id)
	if got.state != "blocked" || !sameInstant(got.until, &long) || !sameInstant(got.blockedAt, before.blockedAt) {
		t.Fatalf("a clean reading changed a block in force: %+v (was %+v)", got, before)
	}

	// An equally severe reading with an earlier end keeps the later end.
	if err := f.warmups.UpdateParticipantHealth(ctx, id, models.WarmupHealthBlocked, &short, "placement", 50); err != nil {
		t.Fatalf("UpdateParticipantHealth(blocked, shorter): %v", err)
	}
	got = f.standing(t, id)
	if !sameInstant(got.until, &long) || !sameInstant(got.blockedAt, before.blockedAt) {
		t.Fatalf("a shorter block cut the term: %+v", got)
	}

	// A quarantine is a floor too.
	f.penalise(t, id, 20, "quarantined", ptr(time.Now().Add(7*24*time.Hour)))
	if err := f.warmups.UpdateParticipantHealth(ctx, id, models.WarmupHealthWatch, nil, "", 1); err != nil {
		t.Fatalf("UpdateParticipantHealth(watch): %v", err)
	}
	if got := f.standing(t, id); got.state != "quarantined" {
		t.Fatalf("a milder reading lowered a quarantine in force: %+v", got)
	}
	// A more severe reading applies over it.
	if err := f.warmups.UpdateParticipantHealth(ctx, id, models.WarmupHealthBlocked, &short, "complaints", 9); err != nil {
		t.Fatalf("UpdateParticipantHealth(blocked): %v", err)
	}
	if got := f.standing(t, id); got.state != "blocked" || !sameInstant(got.until, &short) {
		t.Fatalf("a more severe reading did not apply: %+v", got)
	}
}

func TestLiveHealthFloorReleasesWhatItShould(t *testing.T) {
	f := newLedgerFixture(t)
	ctx := context.Background()
	id := f.addMailbox(t, f.user)
	f.join(t, id)

	// Throttled lifts on recovery: it is not floored.
	f.penalise(t, id, 5, "throttled", ptr(time.Now().Add(3*24*time.Hour)))
	if err := f.warmups.UpdateParticipantHealth(ctx, id, models.WarmupHealthHealthy, nil, "", 0); err != nil {
		t.Fatalf("UpdateParticipantHealth: %v", err)
	}
	if got := f.standing(t, id); got.state != "healthy" {
		t.Fatalf("a throttle was floored: %+v", got)
	}

	// A served block is released by the reading.
	f.penalise(t, id, 40, "blocked", ptr(time.Now().Add(-time.Hour)))
	if err := f.warmups.UpdateParticipantHealth(ctx, id, models.WarmupHealthHealthy, nil, "", 0); err != nil {
		t.Fatalf("UpdateParticipantHealth: %v", err)
	}
	if got := f.standing(t, id); got.state != "healthy" || got.until != nil {
		t.Fatalf("a served block was not released: %+v", got)
	}

	// A review block is still untouchable by the sweep.
	f.penalise(t, id, 60, "blocked", nil)
	if err := f.warmups.UpdateParticipantHealth(ctx, id, models.WarmupHealthHealthy, nil, "", 0); err != nil {
		t.Fatalf("UpdateParticipantHealth: %v", err)
	}
	if got := f.standing(t, id); got.state != "blocked" || got.until != nil {
		t.Fatalf("the sweep overturned a review block: %+v", got)
	}
}
