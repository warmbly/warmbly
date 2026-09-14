package warmup

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Live checks that a mailbox is in exactly one warmup pool, and that changing
// which one carries its reputation across. Skipped unless WARMBLY_TEST_DB is
// set:
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/app/warmup/ -run Live -v
//
// Issue #211: joining and leaving were both scoped to the pool the mailbox is
// entitled to RIGHT NOW, so an entitlement change added a row in the new pool
// and left the old one behind. A downgraded mailbox kept its premium row and
// went on being offered to paying customers as a warmup partner, and its spam
// score was counted once per row by the health evaluator.
//
// The whole thing lives in SQL — an upsert conflict target, a unique index, an
// aggregate — so it only reproduces against a real database.

// poolMailbox is a mailbox with a warmup pool membership and a reputation worth
// losing track of.
type poolMailbox struct {
	user, org, account uuid.UUID
	handle             *db.DB
}

func newPoolMailbox(t *testing.T, handle *db.DB, poolType string) *poolMailbox {
	t.Helper()
	pool := handle.Pool

	requireSeededPools(t, pool)

	f := &poolMailbox{user: uuid.New(), org: uuid.New(), account: uuid.New(), handle: handle}

	exec := func(sql string, args ...any) { t.Helper(); execSQL(t, pool, sql, args...) }

	exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Pool', 'Live')`,
		f.user, "wp-"+f.user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Warmup Pool Live', $2, $3)`,
		f.org, "wp-"+f.org.String()[:8], f.user)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name,
	          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone, warmup_pool_type)
	      VALUES ($1, $2, $3, $4, 'Pool', '', '', 'smtp_imap', 'active', 50, 600, 'UTC', $5)`,
		f.account, f.user, f.org, "wp-"+f.account.String()[:8]+"@test.local", poolType)

	if poolType != "" {
		exec(`INSERT INTO warmup_pool_participants (pool_id, email_account_id) VALUES ($1, $2)`, poolIDFor(t, poolType), f.account)
	}

	t.Cleanup(func() {
		c := context.Background()
		for _, s := range []string{
			`DELETE FROM warmup_pool_participants WHERE email_account_id = $1`,
			`DELETE FROM email_accounts WHERE id = $1`,
		} {
			if _, err := pool.Exec(c, s, f.account); err != nil {
				t.Errorf("cleanup %q: %v", s, err)
			}
		}
		if _, err := pool.Exec(c, `DELETE FROM organizations WHERE id = $1`, f.org); err != nil {
			t.Errorf("cleanup organizations: %v", err)
		}
		if _, err := pool.Exec(c, `DELETE FROM users WHERE id = $1`, f.user); err != nil {
			t.Errorf("cleanup users: %v", err)
		}
	})

	return f
}

// memberships returns the pool types this mailbox is a participant of.
func (f *poolMailbox) memberships(t *testing.T) []string {
	t.Helper()
	rows, err := f.handle.Pool.Query(context.Background(), `
		SELECT wp.pool_type::text
		FROM warmup_pool_participants wpp
		JOIN warmup_pools wp ON wp.id = wpp.pool_id
		WHERE wpp.email_account_id = $1
		ORDER BY 1`, f.account)
	if err != nil {
		t.Fatalf("read memberships: %v", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan membership: %v", err)
		}
		out = append(out, s)
	}
	return out
}

// The bug itself: a mailbox that changes tier moves pools, it does not collect
// them.
func TestLiveJoiningTheOtherPoolMovesTheMailboxRatherThanDuplicatingIt(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	f := newPoolMailbox(t, handle, "premium")
	ctx := context.Background()

	if err := repo.MoveToPool(ctx, models.WarmupPoolFreeID, f.account, "sender_receiver"); err != nil {
		t.Fatalf("move to free: %v", err)
	}

	got := f.memberships(t)
	if len(got) != 1 || got[0] != "free" {
		t.Fatalf("memberships %v, want exactly [free]; the mailbox is in two pools at once", got)
	}
}

// Moving pools must not launder a penalty. A fresh row would start healthy with
// a zero score, which is how a blocked mailbox could have re-entered the pool it
// was blocked out of.
func TestLiveMovingPoolsCarriesTheMailboxReputation(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	f := newPoolMailbox(t, handle, "premium")
	ctx := context.Background()

	if _, err := handle.Pool.Exec(ctx, `
		UPDATE warmup_pool_participants
		SET spam_score = 47, health_state = 'blocked', blocked_at = NOW(),
		    blocked_until = NOW() + INTERVAL '30 days', blocked_reason = 'tampering'
		WHERE email_account_id = $1`, f.account); err != nil {
		t.Fatalf("stain the mailbox: %v", err)
	}

	if err := repo.MoveToPool(ctx, models.WarmupPoolFreeID, f.account, "recipient_only"); err != nil {
		t.Fatalf("move to free: %v", err)
	}

	health, err := repo.GetParticipantHealthForAccount(ctx, f.account)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if health == nil {
		t.Fatal("the mailbox lost its membership entirely")
	}
	if health.PoolType != "free" {
		t.Fatalf("pool %q, want free", health.PoolType)
	}
	if health.HealthState != models.WarmupHealthBlocked {
		t.Fatalf("health %q after the move, want it still blocked", health.HealthState)
	}
	if health.SpamScore != 47 {
		t.Fatalf("spam score %d after the move, want 47", health.SpamScore)
	}
	if health.BlockedUntil == nil {
		t.Fatal("the block expiry was dropped by the move")
	}
}

// An indefinite block (tampering, appeal only) has no blocked_until at all.
// It has to survive a pool move as an indefinite block, not become an expiry.
func TestLiveMovingPoolsKeepsAnIndefiniteBlockIndefinite(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	f := newPoolMailbox(t, handle, "premium")
	ctx := context.Background()

	if _, err := handle.Pool.Exec(ctx, `
		UPDATE warmup_pool_participants
		SET health_state = 'blocked', blocked_at = NOW(), blocked_until = NULL,
		    blocked_reason = 'tampering'
		WHERE email_account_id = $1`, f.account); err != nil {
		t.Fatalf("block the mailbox: %v", err)
	}

	if err := repo.MoveToPool(ctx, models.WarmupPoolFreeID, f.account, "sender_receiver"); err != nil {
		t.Fatalf("move to free: %v", err)
	}

	health, err := repo.GetParticipantHealthForAccount(ctx, f.account)
	if err != nil || health == nil {
		t.Fatalf("read back: %v", err)
	}
	if health.HealthState != models.WarmupHealthBlocked {
		t.Fatalf("health %q after the move, want it still blocked", health.HealthState)
	}
	if health.BlockedUntil != nil {
		t.Fatalf("blocked_until %v after the move, want an indefinite block", health.BlockedUntil)
	}
}

// The invariant is the database's, not the caller's: a second membership cannot
// be written even by SQL that asks for one.
func TestLiveASecondPoolMembershipIsRejected(t *testing.T) {
	_, handle := liveWarmupRepo(t)
	f := newPoolMailbox(t, handle, "premium")

	_, err := handle.Pool.Exec(context.Background(),
		`INSERT INTO warmup_pool_participants (pool_id, email_account_id) VALUES ($1, $2)`,
		models.WarmupPoolFreeID, f.account)
	if err == nil {
		t.Fatal("the database accepted a mailbox into two warmup pools")
	}
}

// The score is the mailbox's, not the pool row's. Summing it across memberships
// counted every increment twice for a dual-member mailbox.
func TestLiveSpamScoreIsCountedOnce(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	f := newPoolMailbox(t, handle, "premium")
	ctx := context.Background()

	after, err := repo.IncrementSpamScore(ctx, f.account, 5)
	if err != nil {
		t.Fatalf("increment: %v", err)
	}
	if after != 5 {
		t.Fatalf("increment returned %d, want 5", after)
	}

	score, err := repo.GetSpamScore(ctx, f.account)
	if err != nil {
		t.Fatalf("read score: %v", err)
	}
	if score != 5 {
		t.Fatalf("score %d, want 5", score)
	}
}

// The column caps the score at 100. Adding past the cap used to violate the
// CHECK, and every caller ignores the error, so a noisy mailbox silently kept
// whatever score it had.
func TestLiveSpamScoreClampsInsteadOfFailing(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	f := newPoolMailbox(t, handle, "premium")
	ctx := context.Background()

	if _, err := handle.Pool.Exec(ctx,
		`UPDATE warmup_pool_participants SET spam_score = 97 WHERE email_account_id = $1`, f.account); err != nil {
		t.Fatalf("preload score: %v", err)
	}

	after, err := repo.IncrementSpamScore(ctx, f.account, 10)
	if err != nil {
		t.Fatalf("increment past the ceiling: %v", err)
	}
	if after != 100 {
		t.Fatalf("score %d, want it clamped to 100", after)
	}
}

// A late signal about a mailbox that just left warmup is normal, not an error.
func TestLiveSpamScoreForAMailboxInNoPoolIsNotAFailure(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	f := newPoolMailbox(t, handle, "")
	ctx := context.Background()

	after, err := repo.IncrementSpamScore(ctx, f.account, 5)
	if err != nil {
		t.Fatalf("increment for a non-participant: %v", err)
	}
	if after != 0 {
		t.Fatalf("score %d, want 0", after)
	}
}

// Removal cannot be pool-scoped, because the caller that knows the mailbox is
// no longer entitled does not know which pool it ended up in.
func TestLiveLeaveAllPoolsRemovesTheMailboxWhereverItIs(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	f := newPoolMailbox(t, handle, "premium")

	if err := repo.LeaveAllPools(context.Background(), f.account); err != nil {
		t.Fatalf("leave: %v", err)
	}
	if got := f.memberships(t); len(got) != 0 {
		t.Fatalf("memberships %v, want none", got)
	}
}

// MoveExistingToPool is the reconciler's tool: it corrects a member's pool and
// leaves its role alone, and never joins a mailbox that is in no pool.
func TestLiveMoveExistingOnlyMovesActualMembers(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	ctx := context.Background()

	member := newPoolMailbox(t, handle, "premium")
	if _, err := handle.Pool.Exec(ctx,
		`UPDATE warmup_pool_participants SET participant_role = 'recipient_only' WHERE email_account_id = $1`,
		member.account); err != nil {
		t.Fatalf("demote: %v", err)
	}

	moved, err := repo.MoveExistingToPool(ctx, models.WarmupPoolFreeID, member.account)
	if err != nil {
		t.Fatalf("move member: %v", err)
	}
	if !moved {
		t.Fatal("reported no move for a mailbox that was in the other pool")
	}
	if got := member.memberships(t); len(got) != 1 || got[0] != "free" {
		t.Fatalf("memberships %v, want [free]", got)
	}

	var role string
	if err := handle.Pool.QueryRow(ctx,
		`SELECT participant_role FROM warmup_pool_participants WHERE email_account_id = $1`,
		member.account).Scan(&role); err != nil {
		t.Fatalf("read role: %v", err)
	}
	if role != "recipient_only" {
		t.Fatalf("role %q after a move, want the demotion preserved", role)
	}

	again, err := repo.MoveExistingToPool(ctx, models.WarmupPoolFreeID, member.account)
	if err != nil {
		t.Fatalf("move again: %v", err)
	}
	if again {
		t.Fatal("reported a move for a mailbox already in that pool")
	}

	outsider := newPoolMailbox(t, handle, "")
	moved, err = repo.MoveExistingToPool(ctx, models.WarmupPoolFreeID, outsider.account)
	if err != nil {
		t.Fatalf("move non-member: %v", err)
	}
	if moved {
		t.Fatal("moved a mailbox that is in no pool")
	}
	if got := outsider.memberships(t); len(got) != 0 {
		t.Fatalf("memberships %v, want the non-member left out of warmup", got)
	}
}

// A tier move writes the mailbox's pool type and its pool membership together.
// Updating only the column is what stranded downgraded mailboxes in the premium
// pool in the first place.
func TestLiveChangingTheTierMovesTheMembershipWithIt(t *testing.T) {
	_, handle := liveWarmupRepo(t)
	f := newPoolMailbox(t, handle, "premium")
	workerRepo := repository.NewWorkerRepository(handle.Pool)

	if err := workerRepo.UpdateEmailAccountWarmupPoolType(context.Background(), f.account, "free"); err != nil {
		t.Fatalf("downgrade: %v", err)
	}

	if got := f.memberships(t); len(got) != 1 || got[0] != "free" {
		t.Fatalf("memberships %v, want [free]; the downgraded mailbox is still a premium warmup partner", got)
	}
}

// A mailbox in no pool must not be dragged into one by a tier change.
func TestLiveChangingTheTierDoesNotJoinANonParticipant(t *testing.T) {
	_, handle := liveWarmupRepo(t)
	f := newPoolMailbox(t, handle, "")
	workerRepo := repository.NewWorkerRepository(handle.Pool)

	if err := workerRepo.UpdateEmailAccountWarmupPoolType(context.Background(), f.account, "premium"); err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	if got := f.memberships(t); len(got) != 0 {
		t.Fatalf("memberships %v, want none", got)
	}
}

// The account-scoped read finds the row whichever pool it is in, so no caller
// has to probe the pools in an order and bias toward the first one.
func TestLiveParticipantHealthIsFoundWithoutNamingThePool(t *testing.T) {
	repo, handle := liveWarmupRepo(t)
	ctx := context.Background()

	for _, poolType := range []string{"free", "premium"} {
		f := newPoolMailbox(t, handle, poolType)
		health, err := repo.GetParticipantHealthForAccount(ctx, f.account)
		if err != nil {
			t.Fatalf("%s: read: %v", poolType, err)
		}
		if health == nil || health.PoolType != poolType {
			t.Fatalf("%s: got %+v, want that pool's row", poolType, health)
		}
	}

	outsider := newPoolMailbox(t, handle, "")
	health, err := repo.GetParticipantHealthForAccount(ctx, outsider.account)
	if err != nil {
		t.Fatalf("non-member: %v", err)
	}
	if health != nil {
		t.Fatal("a mailbox in no pool reported a membership")
	}
}
