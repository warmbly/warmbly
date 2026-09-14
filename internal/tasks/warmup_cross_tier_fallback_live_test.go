package tasks

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/encrypt"
	"github.com/warmbly/warmbly/internal/repository"
)

// The borrowing rule (#495) pinned end to end against the real selector and
// the real SQL; run with WARMBLY_TEST_DB on a scratch database at 156 or later.

type crossTierFixture struct {
	pool *pgxpool.Pool
	svc  *tasksService
	user uuid.UUID
	org  uuid.UUID
	orgs []uuid.UUID
}

func newCrossTierFixture(t *testing.T) *crossTierFixture {
	t.Helper()
	handle := liveCampaignDB(t)
	pool := handle.Pool
	requireEmptyPool(t, pool, models.WarmupPoolFreeID)
	requireEmptyPool(t, pool, models.WarmupPoolPremiumID)

	f := &crossTierFixture{pool: pool, user: uuid.New()}
	// Registered before the first row so a failed insert leaves nothing behind.
	t.Cleanup(func() {
		c := context.Background()
		for _, org := range f.orgs {
			for _, sql := range []string{
				`DELETE FROM warmup_tokens WHERE sender_account_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)`,
				`DELETE FROM warmup_pool_participants WHERE email_account_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)`,
				`DELETE FROM email_accounts WHERE organization_id = $1`,
				`DELETE FROM organizations WHERE id = $1`,
			} {
				if _, err := pool.Exec(c, sql, org); err != nil {
					t.Errorf("cleanup %q: %v", sql, err)
				}
			}
		}
		if _, err := pool.Exec(c, `DELETE FROM users WHERE id = $1`, f.user); err != nil {
			t.Errorf("cleanup user: %v", err)
		}
	})
	f.exec(t, `INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Borrow', 'Test')`, f.user, "borrow-"+f.user.String()[:8]+"@test.local")
	f.org = f.workspace(t, models.OrgRiskTrusted)

	enc, err := encrypt.NewEncrypter([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("encrypter: %v", err)
	}
	warmupRepo := repository.NewWarmupRepository(pool)
	f.svc = &tasksService{
		warmupRepo:   warmupRepo,
		emailRepo:    repository.NewEmailRepostory(handle, enc),
		warmupHealth: warmupapp.NewService(warmupRepo), // the gate only runs when this is wired
	}
	return f
}

// workspace adds an organization in the given risk state.
func (f *crossTierFixture) workspace(t *testing.T, risk models.OrgRiskState) uuid.UUID {
	t.Helper()
	org := uuid.New()
	f.orgs = append(f.orgs, org)
	f.exec(t, `INSERT INTO organizations (id, name, slug, owner_user_id, risk_state) VALUES ($1, 'Borrow Test', $2, $3, $4)`, org, "borrow-"+org.String()[:8], f.user, string(risk))
	return org
}

func (f *crossTierFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
	}
}

// member adds a healthy mailbox to a pool, its join backdated by provenDays.
func (f *crossTierFixture) member(t *testing.T, poolID uuid.UUID, provenDays int) uuid.UUID {
	return f.memberOf(t, f.org, poolID, provenDays)
}

func (f *crossTierFixture) memberOf(t *testing.T, org, poolID uuid.UUID, provenDays int) uuid.UUID {
	t.Helper()
	id := uuid.New()
	f.exec(t, `INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html,
	          provider, status, campaign_limit, min_wait_time, timezone)
	      VALUES ($1, $2, $3, $4, 'Borrow', '', '', 'smtp_imap', 'active', 50, 600, 'UTC')`,
		id, f.user, org, "borrow-"+id.String()[:8]+"@test.local")
	f.exec(t, `INSERT INTO warmup_pool_participants (pool_id, email_account_id, participant_role, health_state, joined_at)
	      VALUES ($1, $2, 'sender_receiver', 'healthy', NOW() - make_interval(days => $3))`, poolID, id, provenDays)
	return id
}

func (f *crossTierFixture) sender(id uuid.UUID, tier string) models.Email {
	return models.Email{ID: id, OrganizationID: &f.org, WarmupPoolType: tier}
}

func TestLiveWarmupBorrowThinPremiumTierBorrowsAProvenFreeMailbox(t *testing.T) {
	f := newCrossTierFixture(t)
	sender := f.member(t, models.WarmupPoolPremiumID, 0)
	borrowed := f.member(t, models.WarmupPoolFreeID, 4)

	partner, err := f.svc.selectWarmupPartner(context.Background(), f.sender(sender, "premium"))
	if err != nil {
		t.Fatalf("a thin premium tier with one proven free mailbox produced no partner: %v", err)
	}
	if partner.ID != borrowed {
		t.Fatalf("selected %s, want the borrowed free mailbox %s", partner.ID, borrowed)
	}
}

func TestLiveWarmupBorrowFreeTierNeverBorrowsPremium(t *testing.T) {
	f := newCrossTierFixture(t)
	sender := f.member(t, models.WarmupPoolFreeID, 0)
	f.member(t, models.WarmupPoolPremiumID, 4)

	if partner, err := f.svc.selectWarmupPartner(context.Background(), f.sender(sender, "free")); !errors.Is(err, errNoWarmupPartners) {
		t.Fatalf("free sender got (%v, %v); want no candidates at all, since free traffic must not reach paying inboxes", partner, err)
	}
}

// A workspace under restriction is forced into the free pool with its
// standing intact, which is exactly what must not count as proven.
func TestLiveWarmupBorrowSkipsARestrictedWorkspace(t *testing.T) {
	f := newCrossTierFixture(t)
	sender := f.member(t, models.WarmupPoolPremiumID, 0)
	restricted := f.workspace(t, models.OrgRiskRestricted)
	f.memberOf(t, restricted, models.WarmupPoolFreeID, 4)

	if partner, err := f.svc.selectWarmupPartner(context.Background(), f.sender(sender, "premium")); !errors.Is(err, errNoWarmupPartners) {
		t.Fatalf("got (%v, %v); a restricted workspace's mailbox must not be borrowed", partner, err)
	}
}

func TestLiveWarmupBorrowPrefersAFreshOwnTierPartner(t *testing.T) {
	f := newCrossTierFixture(t)
	sender := f.member(t, models.WarmupPoolPremiumID, 0)
	own := f.member(t, models.WarmupPoolPremiumID, 0)
	for i := 0; i < 5; i++ {
		f.member(t, models.WarmupPoolFreeID, 4)
	}

	partner, err := f.svc.selectWarmupPartner(context.Background(), f.sender(sender, "premium"))
	if err != nil {
		t.Fatal(err)
	}
	if partner.ID != own {
		t.Fatalf("chose %s over the fresh own-tier partner %s", partner.ID, own)
	}
}

// The floor counts the other mailboxes: a premium tier of exactly the floor
// including the sender is one short and still borrows.
func TestLiveWarmupBorrowFloorCountsTheOtherMailboxes(t *testing.T) {
	f := newCrossTierFixture(t)
	sender := f.member(t, models.WarmupPoolPremiumID, 0)
	// Every own-tier partner was used today, so only a borrowed one can be picked.
	for i := 1; i < config.WarmupPoolTierFallbackFloor; i++ {
		own := f.member(t, models.WarmupPoolPremiumID, 0)
		task := uuid.New()
		f.exec(t, `INSERT INTO tasks (id, task_type, email_account_id, status, message_id) VALUES ($1, 'warmup', $2, 'completed', '')`, task, sender)
		f.exec(t, `INSERT INTO warmup_tokens (task_id, sender_account_id, recipient_account_id) VALUES ($1, $2, $3)`, task, sender, own)
	}
	borrowed := f.member(t, models.WarmupPoolFreeID, 4)

	partner, err := f.svc.selectWarmupPartner(context.Background(), f.sender(sender, "premium"))
	if err != nil {
		t.Fatalf("a premium tier at the floor including its sender did not borrow: %v", err)
	}
	if partner.ID != borrowed {
		t.Fatalf("selected %s, want the borrowed free mailbox %s", partner.ID, borrowed)
	}
}
