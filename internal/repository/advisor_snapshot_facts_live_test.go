package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The snapshot reads the facts detectors claim on the send path's terms: an
// enabled sender, a stopped warmup and the tracking host actually used.
func TestLiveAdvisorSnapshotReadsTheSendPathsFacts(t *testing.T) {
	handle, pool := liveContactDB(t)
	ctx := context.Background()
	requireSchemaVersion(t, pool, 357)

	user, org, campaign := uuid.New(), uuid.New(), uuid.New()
	sending, disabled := uuid.New(), uuid.New()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(50, len(sql))], err)
		}
	}
	t.Cleanup(func() {
		for _, sql := range []string{
			`DELETE FROM campaigns WHERE organization_id = $1`,
			`DELETE FROM email_accounts WHERE organization_id = $1`,
			`DELETE FROM organizations WHERE id = $1`,
		} {
			if _, err := pool.Exec(ctx, sql, org); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
		if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user); err != nil {
			t.Errorf("cleanup user: %v", err)
		}
	})

	exec(`INSERT INTO users (id, email, first_name, last_name) VALUES ($1, $2, 'Advisor', 'Live')`,
		user, "adv-"+user.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Advisor Live', $2, $3)`,
		org, "adv-"+org.String()[:8], user)
	for _, a := range []uuid.UUID{sending, disabled} {
		exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html,
		          provider, status, campaign_limit, min_wait_time, timezone, warmup, warmup_paused_at, warmup_stopped_at)
		      VALUES ($1, $2, $3, $4, 'Advisor', '', '', 'smtp_imap', 'active', 30, 600, 'UTC', NOW(), NOW(), NOW())`,
			a, user, org, "adv-"+a.String()[:8]+"@test.local")
	}
	exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, days, status, sender_strategy, open_tracking, updated_at, created_at)
	      VALUES ($1, $2, $3, 'Advisor', '', 62, 'active', 'explicit', true, NOW(), NOW())`, campaign, user, org)
	exec(`INSERT INTO campaign_senders (campaign_id, email_account_id, enabled) VALUES ($1, $2, true), ($1, $3, false)`,
		campaign, sending, disabled)
	snap, err := NewAdvisorRepository(handle).LoadSnapshot(ctx, org, time.Now())
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	for _, m := range snap.Mailboxes {
		switch m.ID {
		case sending:
			if !m.InActiveCampaign || !m.TrackedOnMailboxHost {
				t.Errorf("enabled sender: in campaign=%v tracked=%v, want both", m.InActiveCampaign, m.TrackedOnMailboxHost)
			}
			if m.WarmupPaused {
				t.Error("a stopped warmup read as paused")
			}
		case disabled:
			if m.InActiveCampaign {
				t.Error("a disabled sender read as in a running campaign")
			}
		}
	}
}
