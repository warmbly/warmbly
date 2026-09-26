package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// Issue #143: partner selection needs THIS sender's record per recipient mail
// host, not the pool-wide rollup the admin overview reads. These prove the
// queries that feed both against the real schema.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run 'LiveProviderRouting|LiveHostPlacement|LivePoolPlacements' -v

// premiumPoolID is the premium pool migration 000156 seeds on every instance.
var premiumPoolID = models.WarmupPoolPremiumID

type providerRoutingFixture struct {
	pool      *pgxpool.Pool
	user      uuid.UUID
	org       uuid.UUID
	sender    uuid.UUID
	atGoogle  uuid.UUID
	atMSGraph uuid.UUID
	atCustom  uuid.UUID
}

func newProviderRoutingFixture(t *testing.T) *providerRoutingFixture {
	t.Helper()
	_, pool := liveContactDB(t)
	ctx := context.Background()

	f := &providerRoutingFixture{
		pool: pool, user: uuid.New(), org: uuid.New(),
		sender: uuid.New(), atGoogle: uuid.New(), atMSGraph: uuid.New(), atCustom: uuid.New(),
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
		{f.atCustom, "smtp_imap", "acme.test"},
	} {
		exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain,
		          signature_html, provider, status, campaign_limit, min_wait_time, timezone)
		      VALUES ($1, $2, $3, $4, 'Route', '', '', $5, 'active', 50, 600, 'UTC')`,
			m.id, f.user, f.org, "route-"+m.id.String()[:8]+"@"+m.domain, m.provider)
	}
	// Only the recipients join the pool; the sender does not, so it must never
	// appear in the participants map.
	for _, id := range []uuid.UUID{f.atGoogle, f.atMSGraph, f.atCustom} {
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

// placementFrom writes the row the way the consumer does: recipient_provider is
// the connect method the mailbox uses, recipient_domain is who its mail is at.
func (f *providerRoutingFixture) placementFrom(t *testing.T, connectMethod, domain string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, err := f.pool.Exec(context.Background(),
			`INSERT INTO warmup_spam_reports (id, reporter_account_id, reported_account_id, message_id,
			     report_type, recipient_provider, recipient_domain, created_at)
			 VALUES (gen_random_uuid(), $1, $1, $2, 'spam_placement', $3, $4, NOW())`,
			f.sender, "m-"+uuid.New().String(), connectMethod, domain); err != nil {
			t.Fatalf("insert placement: %v", err)
		}
	}
}

// rollup writes one day of the sender's placement the way the consumer counts
// it: keyed by the recipient's resolved mail host.
func (f *providerRoutingFixture) rollup(t *testing.T, group, host string, daysAgo, inbox, spam int) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(),
		`INSERT INTO warmup_placement_daily (sender_account_id, date, recipient_group, recipient_host, inbox, spam)
		 VALUES ($1, (NOW() AT TIME ZONE 'UTC')::date - $4::int, $2, $3, $5, $6)`,
		f.sender, group, host, daysAgo, inbox, spam); err != nil {
		t.Fatalf("insert rollup: %v", err)
	}
}

// Two custom domains, one on Google Workspace and one on a small host, must
// not share a record: that is the split partner selection exists to act on.
func TestLiveHostPlacementSegmentsOneSendersRecord(t *testing.T) {
	handle, _ := liveContactDB(t)
	f := newProviderRoutingFixture(t)
	repo := NewWarmupRepository(handle.Pool)

	f.rollup(t, "google", "google_workspace", 0, 20, 0)
	f.rollup(t, "other", "hostinger", 0, 10, 10)
	f.rollup(t, "other", "hostinger", 1, 0, 0)

	got, err := repo.SenderPlacementByHost(context.Background(), f.sender, time.Now().Add(-7*24*time.Hour))
	if err != nil {
		t.Fatalf("SenderPlacementByHost: %v", err)
	}
	if g := got["google_workspace"]; g.Delivered != 20 || g.Spam != 0 || g.Rate() != 0 {
		t.Errorf("google_workspace = %+v (rate %v), want 20 delivered and nothing in junk", g, g.Rate())
	}
	if h := got["hostinger"]; h.Delivered != 20 || h.Spam != 10 || h.Rate() != 0.5 {
		t.Errorf("hostinger = %+v (rate %v), want 20 delivered, 10 spam, 0.5", h, h.Rate())
	}
}

// A sender must not be penalised forever at a host it has recovered at.
func TestLiveHostPlacementWindowExcludesOldSignal(t *testing.T) {
	handle, _ := liveContactDB(t)
	f := newProviderRoutingFixture(t)
	repo := NewWarmupRepository(handle.Pool)

	f.rollup(t, "other", "zoho", 10, 0, 20)

	got, err := repo.SenderPlacementByHost(context.Background(), f.sender, time.Now().Add(-7*24*time.Hour))
	if err != nil {
		t.Fatalf("SenderPlacementByHost: %v", err)
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

// A receipt counted with no host belongs to no host. Keying it anywhere would
// demote partners for a failure that was never theirs.
func TestLiveHostPlacementIgnoresUnattributedRows(t *testing.T) {
	handle, _ := liveContactDB(t)
	f := newProviderRoutingFixture(t)
	repo := NewWarmupRepository(handle.Pool)

	f.rollup(t, "other", "", 0, 0, 5)
	f.rollup(t, "other", "other", 0, 10, 0)

	got, err := repo.SenderPlacementByHost(context.Background(), f.sender, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("SenderPlacementByHost: %v", err)
	}
	if _, ok := got[""]; ok {
		t.Error("a hostless row was attributed")
	}
	if o := got["other"]; o.Delivered != 10 || o.Spam != 0 {
		t.Errorf("other = %+v, want the hostless spam kept out of it", o)
	}
}

// The admin overview has to name the same providers routing does. The stored
// recipient_provider column is the connect method, which files a custom-domain
// Microsoft 365 mailbox under smtp_imap: the bucket an operator most needs
// split, and one routing never uses.
func TestLivePoolPlacementsUseTheRoutingVocabulary(t *testing.T) {
	handle, _ := liveContactDB(t)
	f := newProviderRoutingFixture(t)
	repo := NewWarmupRepository(handle.Pool)
	ctx := context.Background()
	since := time.Now().Add(-24 * time.Hour)

	// The rollup is pool-wide, so measure what these rows added to it.
	before, err := repo.PoolSpamPlacementsByProvider(ctx, since)
	if err != nil {
		t.Fatalf("PoolSpamPlacementsByProvider: %v", err)
	}

	f.placementFrom(t, "smtp_imap", "outlook.com", 3)
	f.placementFrom(t, "gmail", "", 2)

	after, err := repo.PoolSpamPlacementsByProvider(ctx, since)
	if err != nil {
		t.Fatalf("PoolSpamPlacementsByProvider: %v", err)
	}
	delta := func(k string) int { return after[k] - before[k] }

	if got := delta("microsoft"); got != 3 {
		t.Errorf("microsoft delta = %d, want 3: mail run by Microsoft over plain IMAP", got)
	}
	if got := delta("smtp_imap"); got != 0 {
		t.Errorf("smtp_imap delta = %d, want 0: the connect method is not a provider", got)
	}
	// Domainless rows stay their own bucket rather than inflating custom.
	if got := delta("unknown"); got != 2 {
		t.Errorf("unknown delta = %d, want 2", got)
	}
	if got := delta("custom"); got != 0 {
		t.Errorf("custom delta = %d, want 0", got)
	}
}
