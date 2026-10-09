package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/warmbly/warmbly/internal/models"
)

// Issue #209: six admin queries named schema that does not exist, so each one
// failed on every call. The prepare sweep (TestLiveEveryQueryPrepares) proves a
// statement CAN run; these tests prove the fixed statements do the right thing.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveAdmin -v

// adminFixture is one user with one organization, one campaign and one mailbox
// that has never synced.
type adminFixture struct {
	org      uuid.UUID
	user     uuid.UUID
	campaign uuid.UUID
	mailbox  uuid.UUID
	tag      string
}

func TestLiveAdminPlatformOverviewConfirmedSends(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := t.Context()
	f := newAdminFixture(t, pool)
	config := pool.Config()
	config.ConnConfig.RuntimeParams["timezone"] = "Pacific/Honolulu"
	localPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer localPool.Close()
	repo := &adminRepository{db: localPool}
	before, err := repo.GetPlatformOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ids []uuid.UUID
	t.Cleanup(func() {
		for _, query := range []string{
			`DELETE FROM warmup_tokens WHERE task_id = ANY($1::uuid[])`,
			`DELETE FROM tasks WHERE id = ANY($1::uuid[])`,
		} {
			if _, err := pool.Exec(context.Background(), query, ids); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})
	today := time.Now().UTC().Truncate(24 * time.Hour)
	yesterday := today.Add(-24 * time.Hour)
	tomorrow := today.Add(24 * time.Hour)
	for _, tt := range []struct {
		name, kind, status, messageID, state string
		completedAt                          time.Time
		appliedAt                            *time.Time
		tokens                               int
		confirmedToken                       bool
	}{
		{"legacy warmup", "warmup", "completed", "", "", yesterday, nil, 1, true},
		{"delayed warmup result", "warmup", "completed", "planned@test.local", "sent", yesterday, &today, 2, true},
		{"campaign result", "campaign", "completed", "", "sent", yesterday, &today, 0, false},
		{"legacy direct dispatch", "email", "completed", "planned@test.local", "", today, nil, 0, false},
		{"direct result", "email", "completed", "", "sent", today, &today, 0, false},
		{"placement result", "placement", "completed", "", "sent", today, &today, 0, false},
		{"future receipt", "email", "completed", "", "sent", today, &tomorrow, 0, false},
		{"warmup dispatch", "warmup", "completed", "planned@test.local", "unknown", today, nil, 1, false},
		{"warmup without receipt", "warmup", "completed", "planned@test.local", "sent", today, &today, 1, false},
		{"campaign dispatch", "campaign", "completed", "", "", today, nil, 0, false},
		{"unknown with message ID", "email", "completed", "planned@test.local", "unknown", today, nil, 0, false},
		{"unapplied success", "campaign", "completed", "planned@test.local", "sent", today, nil, 0, false},
		{"legacy failure", "email", "failed", "planned@test.local", "", today, nil, 0, false},
		{"failed result", "campaign", "failed", "planned@test.local", "failed", today, &today, 0, false},
		{"cancelled dispatch", "warmup", "cancelled", "planned@test.local", "failed", today, &today, 1, false},
	} {
		id := uuid.New()
		ids = append(ids, id)
		_, err := pool.Exec(ctx, `INSERT INTO tasks
			(id, task_type, email_account_id, status, message_id, completed_at, send_result_state, send_result_applied_at)
			VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8)`,
			id, tt.kind, f.mailbox, tt.status, tt.messageID, tt.completedAt, tt.state, tt.appliedAt)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		for range tt.tokens {
			messageID := ""
			if tt.confirmedToken {
				messageID = "<confirmed@test.local>"
			}
			_, err := pool.Exec(ctx, `INSERT INTO warmup_tokens
				(token, task_id, sender_account_id, recipient_account_id, conversation_turn, sent_message_id)
				VALUES ($1, $2, $3, $3, 0, $4)`, uuid.New(), id, f.mailbox, messageID)
			if err != nil {
				t.Fatalf("%s token: %v", tt.name, err)
			}
		}
	}
	after, err := repo.GetPlatformOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := after.TotalEmailsSent - before.TotalEmailsSent; got != 6 {
		t.Fatalf("confirmed total delta = %d, want 6", got)
	}
	if got := after.EmailsSentToday - before.EmailsSentToday; got != 4 {
		t.Fatalf("confirmed UTC today delta = %d, want 4", got)
	}
}

func TestAdminPlatformOverviewReturnsSendCountError(t *testing.T) {
	pool, err := pgxpool.New(t.Context(), "postgres://test:test@127.0.0.1:1/test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	got, err := (&adminRepository{db: pool}).GetPlatformOverview(t.Context())
	if err == nil || got != nil {
		t.Fatalf("closed database: got %+v, %v; want an error, not zero counters", got, err)
	}
}

func newAdminFixture(t *testing.T, pool *pgxpool.Pool) *adminFixture {
	t.Helper()
	ctx := context.Background()
	f := &adminFixture{
		org:      uuid.New(),
		user:     uuid.New(),
		campaign: uuid.New(),
		mailbox:  uuid.New(),
	}
	f.tag = "i209-" + f.org.String()[:8]

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(60, len(sql))], err)
		}
	}

	exec(`INSERT INTO users (id, first_name, last_name, email, password_hash)
	      VALUES ($1, 'Nadia', 'Live', $2, 'x')`, f.user, f.tag+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id)
	      VALUES ($1, 'Issue 209', $2, $3)`, f.org, f.tag, f.user)
	exec(`INSERT INTO organization_members (organization_id, user_id, role, accepted_at)
	      VALUES ($1, $2, 'owner', NOW())`, f.org, f.user)
	exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, days, status, updated_at, created_at)
	      VALUES ($1, $2, $3, 'Issue 209 outreach', '', 62, 'active', NOW(), NOW())`,
		f.campaign, f.user, f.org)
	// last_synced_at stays NULL: a mailbox that has been connected but never
	// synced is the common case right after setup, and it used to break the
	// scan for the whole list.
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider)
	      VALUES ($1, $2, $3, $4, 'Nadia', '', '', 'smtp_imap')`,
		f.mailbox, f.user, f.org, f.tag+"-mb@test.local")

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM campaign_logs WHERE campaign_id IN (SELECT id FROM campaigns WHERE organization_id = $1)`, f.org},
			{`DELETE FROM campaigns WHERE organization_id = $1`, f.org},
			{`DELETE FROM email_accounts WHERE organization_id = $1`, f.org},
			{`DELETE FROM organization_members WHERE organization_id = $1`, f.org},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM user_rate_limits WHERE user_id = $1`, f.user},
			{`DELETE FROM admin_audit_logs WHERE admin_user_id = $1`, f.user},
			{`DELETE FROM users WHERE id = $1`, f.user},
		} {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})
	return f
}

// The email-account section of the user preview compared a uuid column against
// a text parameter, so the query errored and the caller swallowed it: every
// user looked like they had no mailboxes.
func TestLiveAdminUserPreviewListsMailboxes(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newAdminFixture(t, pool)
	repo := NewAdminRepository(pool)

	preview, err := repo.GetUserPreview(context.Background(), f.user)
	if err != nil {
		t.Fatalf("GetUserPreview: %v", err)
	}
	if preview == nil {
		t.Fatal("GetUserPreview returned no preview for an existing user")
	}
	if len(preview.EmailAccounts) != 1 || preview.EmailAccounts[0].ID != f.mailbox {
		t.Fatalf("preview lists %d mailbox(es), want the one that was connected", len(preview.EmailAccounts))
	}
	if preview.EmailAccounts[0].LastSyncedAt != nil {
		t.Fatalf("a mailbox that never synced reads last_synced_at = %v, want nil", preview.EmailAccounts[0].LastSyncedAt)
	}
	if len(preview.Organizations) != 1 || preview.Organizations[0].ID != f.org {
		t.Fatalf("preview lists %d organization(s), want 1", len(preview.Organizations))
	}

	// The paginated mailbox list behind the user detail page compared the same
	// uuid column against a text parameter, and that one surfaced as a 500.
	emails, pagination, err := repo.GetUserEmails(context.Background(), f.user, 0, 50)
	if err != nil {
		t.Fatalf("GetUserEmails: %v", err)
	}
	if len(emails) != 1 || emails[0].ID != f.mailbox {
		t.Fatalf("GetUserEmails returned %d mailbox(es), want the one that was connected", len(emails))
	}
	if pagination == nil || pagination.HasMore {
		t.Fatalf("pagination = %+v, want a single complete page", pagination)
	}
}

// A force-stop wrote status = 'stopped', which campaign_status has never had,
// and stopped_at, which campaigns has never had.
func TestLiveAdminForceStopPausesCampaign(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newAdminFixture(t, pool)
	repo := NewAdminRepository(pool)
	ctx := context.Background()

	stopped, err := repo.StopCampaign(ctx, f.campaign)
	if err != nil {
		t.Fatalf("StopCampaign: %v", err)
	}
	if !stopped {
		t.Fatal("StopCampaign refused an active campaign")
	}

	var status string
	var changedAt *time.Time
	if err = pool.QueryRow(ctx,
		`SELECT status::text, last_status_change_at FROM campaigns WHERE id = $1`, f.campaign,
	).Scan(&status, &changedAt); err != nil {
		t.Fatalf("read campaign back: %v", err)
	}
	if status != "paused" {
		t.Fatalf("campaign status = %q after a force-stop, want %q", status, "paused")
	}
	if changedAt == nil {
		t.Fatal("force-stop left last_status_change_at unset, so the owner's stop cooldown never starts")
	}

	// The scheduler only runs campaigns that are still 'active', which is what
	// makes the status flip enough to halt the send loop.
	detail, err := repo.GetCampaignDetail(ctx, f.campaign)
	if err != nil {
		t.Fatalf("GetCampaignDetail: %v", err)
	}
	if detail == nil || detail.Status != "paused" {
		t.Fatalf("campaign detail reports %v, want a paused campaign", detail)
	}
}

// A campaign that reaches a terminal state while the operator is deciding must
// not be dragged back out of it, so the eligibility test lives in the UPDATE.
func TestLiveAdminForceStopLeavesTerminalCampaignsAlone(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newAdminFixture(t, pool)
	repo := NewAdminRepository(pool)
	ctx := context.Background()

	for _, status := range []string{"completed", "draft"} {
		if _, err := pool.Exec(ctx,
			`UPDATE campaigns SET status = $2::campaign_status WHERE id = $1`, f.campaign, status,
		); err != nil {
			t.Fatalf("park campaign at %s: %v", status, err)
		}

		stopped, err := repo.StopCampaign(ctx, f.campaign)
		if err != nil {
			t.Fatalf("StopCampaign on a %s campaign: %v", status, err)
		}
		if stopped {
			t.Fatalf("StopCampaign reported that it stopped a %s campaign", status)
		}

		var got string
		if err := pool.QueryRow(ctx, `SELECT status::text FROM campaigns WHERE id = $1`, f.campaign).Scan(&got); err != nil {
			t.Fatalf("read campaign back: %v", err)
		}
		if got != status {
			t.Fatalf("a %s campaign is now %q; the stop overwrote a state it had no business touching", status, got)
		}
	}
}

// user_rate_limits has limit_api_calls_daily / limit_bulk_ops_daily and no
// daily_email_limit, so both the read and the write failed every time.
func TestLiveAdminUserRateLimitsRoundTrip(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newAdminFixture(t, pool)
	repo := NewAdminRepository(pool)
	ctx := context.Background()

	existing, err := repo.GetUserRateLimits(ctx, f.user)
	if err != nil {
		t.Fatalf("GetUserRateLimits before any override: %v", err)
	}
	if existing != nil {
		t.Fatalf("a fresh user already has an override row: %+v", existing)
	}

	// A partial patch is the normal case, and it has to create the row without
	// tripping the NOT NULL constraint on every column it does not mention.
	writes := 42
	if err := repo.UpdateUserRateLimits(ctx, f.user, f.user, &models.UpdateUserRateLimitsRequest{
		LimitWritePM: &writes,
	}); err != nil {
		t.Fatalf("UpdateUserRateLimits (first, partial): %v", err)
	}

	limits, err := repo.GetUserRateLimits(ctx, f.user)
	if err != nil {
		t.Fatalf("GetUserRateLimits: %v", err)
	}
	if limits == nil {
		t.Fatal("no override row after a successful update")
	}
	if limits.LimitWritePM != writes {
		t.Fatalf("limit_write_pm = %d, want %d", limits.LimitWritePM, writes)
	}
	if limits.LimitReadPM == 0 || limits.LimitAPICallsDaily == 0 || limits.MaxConnections == 0 {
		t.Fatalf("a partial patch zeroed the untouched columns: %+v", limits)
	}
	if limits.UpdatedBy == nil || *limits.UpdatedBy != f.user {
		t.Fatalf("updated_by = %v, want the acting admin", limits.UpdatedBy)
	}

	// A second patch must leave the first one alone.
	daily := 7
	if err := repo.UpdateUserRateLimits(ctx, f.user, f.user, &models.UpdateUserRateLimitsRequest{
		LimitBulkOpsDaily: &daily,
	}); err != nil {
		t.Fatalf("UpdateUserRateLimits (second, partial): %v", err)
	}
	after, err := repo.GetUserRateLimits(ctx, f.user)
	if err != nil {
		t.Fatalf("GetUserRateLimits after second patch: %v", err)
	}
	if after.LimitBulkOpsDaily != daily {
		t.Fatalf("limit_bulk_ops_daily = %d, want %d", after.LimitBulkOpsDaily, daily)
	}
	if after.LimitWritePM != writes {
		t.Fatalf("the second patch reset limit_write_pm to %d, want %d", after.LimitWritePM, writes)
	}
}
