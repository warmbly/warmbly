package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// Each receipt is counted once, bucketed by the recipient's host, and every
// read is scoped to the sender's organization.
func TestLiveWarmupPlacementRollup(t *testing.T) {
	handle, pool := liveContactDB(t)
	ctx := context.Background()
	userID, orgID, otherOrg := uuid.New(), uuid.New(), uuid.New()
	sender, recipient, stranger := uuid.New(), uuid.New(), uuid.New()

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'Placement', 'Rollup', $2)`,
		userID, "placement-"+uuid.NewString()+"@example.test")
	exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'Placement', $3), ($2, 'Partner', $3)`, orgID, otherOrg, userID)
	mailbox := func(id, org uuid.UUID, provider, host string) {
		exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, mail_host)
		      VALUES ($1, $2, $3, $4, 'Placement', '', '', $5, $6)`,
			id, userID, org, "placement-box-"+uuid.NewString()+"@example.test", provider, host)
	}
	mailbox(sender, orgID, "smtp_imap", "")
	mailbox(recipient, otherOrg, "smtp_imap", "google_workspace")
	mailbox(stranger, otherOrg, "outlook", "")

	receipt := func(to uuid.UUID) uuid.UUID {
		id := uuid.New()
		exec(`INSERT INTO warmup_received (email_account_id, internal_id, message_id, sender_account_id)
		      VALUES ($1, $2, $3, $4)`, to, id, "<"+id.String()+"@example.test>", sender)
		return id
	}
	inbox, tab, spam, ms := receipt(recipient), receipt(recipient), receipt(recipient), receipt(stranger)

	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM warmup_tokens WHERE sender_account_id IN (SELECT id FROM email_accounts WHERE organization_id IN ($1, $2))`,
			`DELETE FROM tasks WHERE email_account_id IN (SELECT id FROM email_accounts WHERE organization_id IN ($1, $2))`,
			`DELETE FROM warmup_received WHERE email_account_id IN (SELECT id FROM email_accounts WHERE organization_id IN ($1, $2))`,
			`DELETE FROM warmup_statistics WHERE email_account_id IN (SELECT id FROM email_accounts WHERE organization_id IN ($1, $2))`,
			`DELETE FROM email_accounts WHERE organization_id IN ($1, $2)`,
			`DELETE FROM organizations WHERE id IN ($1, $2)`,
		} {
			if _, err := pool.Exec(context.Background(), q, orgID, otherOrg); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})

	repo := NewWarmupPlacementRepository(handle)
	record := func(to, internal uuid.UUID, group, host, landed string, rescued bool) *WarmupPlacementSender {
		t.Helper()
		got, err := repo.RecordPlacement(ctx, to, internal, group, host, landed, rescued)
		if err != nil {
			t.Fatalf("RecordPlacement: %v", err)
		}
		return got
	}
	if got := record(recipient, inbox, "google", "google_workspace", models.WarmupLandedInbox, false); got == nil || got.ID != sender || got.OrgID == nil || *got.OrgID != orgID {
		t.Fatalf("a counted receipt names its sender and the sender's org, got %+v", got)
	}
	record(recipient, tab, "google", "google_workspace", models.WarmupLandedTabs, false)
	record(recipient, spam, "google", "google_workspace", models.WarmupLandedSpam, true)
	// A redelivered arrival adds nothing and names nobody.
	if got := record(recipient, spam, "google", "google_workspace", models.WarmupLandedSpam, true); got != nil {
		t.Fatalf("a redelivered arrival must not count again, got %+v", got)
	}
	record(stranger, ms, "microsoft", "", models.WarmupLandedInbox, false)
	// A receipt that was never written counts nothing either.
	if got := record(recipient, uuid.New(), "google", "google_workspace", models.WarmupLandedInbox, false); got != nil {
		t.Fatalf("a missing receipt must not count, got %+v", got)
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	rows, err := repo.Daily(ctx, orgID, nil, today.AddDate(0, 0, -1), today)
	if err != nil {
		t.Fatalf("Daily: %v", err)
	}
	var google, microsoft WarmupPlacementDayRow
	for _, r := range rows {
		switch r.Group {
		case "google":
			google = r
		case "microsoft":
			microsoft = r
		}
	}
	if len(rows) != 2 || google.Inbox != 1 || google.Tabs != 1 || google.Spam != 1 || google.Rescued != 1 || microsoft.Inbox != 1 {
		t.Fatalf("daily rows = %+v, want one google inbox, tab and rescued spam, and one microsoft inbox", rows)
	}

	hosts, err := repo.Hosts(ctx, orgID, &sender, today, today)
	if err != nil {
		t.Fatalf("Hosts: %v", err)
	}
	if len(hosts) != 2 {
		t.Fatalf("hosts = %+v, want google_workspace and an unknown microsoft host", hosts)
	}

	rates, err := repo.Rates(ctx, orgID, nil, today.AddDate(0, 0, -6))
	if err != nil {
		t.Fatalf("Rates: %v", err)
	}
	if r := rates[sender]; r.Delivered != 4 || r.Spam != 1 || r.Band != models.WarmupPlacementBandCollecting {
		t.Fatalf("rate = %+v, want 4 delivered, below the floor", r)
	}

	// The sweep leaves receipts the live path counted alone, and a fresh one to it.
	if n, err := repo.SweepUnplaced(ctx, time.Now().Add(time.Hour), 100); err != nil || n != 0 {
		t.Fatalf("sweep over counted receipts = %d, %v; want 0", n, err)
	}

	// The partner's organization sees none of it.
	if rows, err := repo.Daily(ctx, otherOrg, nil, today.AddDate(0, 0, -1), today); err != nil || len(rows) != 0 {
		t.Fatalf("other org daily = %+v, %v; want nothing", rows, err)
	}

	// A completed send nobody saw is unconfirmed once it is old enough; a
	// consumed one and a failed one are not.
	token := func(status string, age time.Duration, consumed bool) {
		task := uuid.New()
		exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id) VALUES ($1, 'warmup', $2, $3, '')`, task, sender, status)
		exec(`INSERT INTO warmup_tokens (token, task_id, sender_account_id, recipient_account_id, created_at, consumed_at)
		      VALUES ($1, $2, $3, $4, NOW() - $5::interval, CASE WHEN $6 THEN NOW() END)`,
			uuid.New(), task, sender, recipient, age, consumed)
	}
	token("completed", 30*time.Hour, false)
	token("completed", 30*time.Hour, true)
	token("failed", 30*time.Hour, false)
	token("completed", time.Hour, false)
	exec(`INSERT INTO warmup_statistics (email_account_id, date, emails_sent, emails_replied, target_volume) VALUES ($1, $2, 4, 0, 10)`, sender, today)

	cutoff := now.Add(-models.WarmupUnconfirmedAfterHours * time.Hour)
	unconfirmed, err := repo.Unconfirmed(ctx, orgID, nil, today.AddDate(0, 0, -3), today, cutoff)
	if err != nil {
		t.Fatalf("Unconfirmed: %v", err)
	}
	total := 0
	for _, u := range unconfirmed {
		total += u.Count
	}
	if total != 1 {
		t.Fatalf("unconfirmed = %+v, want exactly the old completed unconsumed send", unconfirmed)
	}
	sent, err := repo.Sent(ctx, orgID, &sender, today, today)
	if err != nil || len(sent) != 1 || sent[0].Count != 4 {
		t.Fatalf("sent = %+v, %v; want 4 today", sent, err)
	}
}

// Receipts no live arrival counted (history, or a consumer mid-upgrade) are
// counted by the sweep, spam from the spam reports, each exactly once.
func TestLiveWarmupPlacementSweep(t *testing.T) {
	handle, pool := liveContactDB(t)
	ctx := context.Background()
	userID, orgID := uuid.New(), uuid.New()
	sender, recipient := uuid.New(), uuid.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'Placement', 'Sweep', $2)`,
		userID, "placement-sweep-"+uuid.NewString()+"@example.test")
	exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'Sweep', $2)`, orgID, userID)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, mail_host)
	      VALUES ($1, $3, $4, $5, 'S', '', '', 'smtp_imap', ''), ($2, $3, $4, $6, 'R', '', '', 'outlook', '')`,
		sender, recipient, userID, orgID, "sweep-s-"+uuid.NewString()+"@example.test", "sweep-r-"+uuid.NewString()+"@example.test")
	t.Cleanup(func() {
		for _, q := range []string{
			`DELETE FROM warmup_spam_reports WHERE reporter_account_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)`,
			`DELETE FROM warmup_received WHERE email_account_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)`,
			`DELETE FROM email_accounts WHERE organization_id = $1`,
			`DELETE FROM organizations WHERE id = $1`,
		} {
			if _, err := pool.Exec(context.Background(), q, orgID); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	day := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	for i, msg := range []string{"<a@x>", "<b@x>", "<c@x>", ""} {
		exec(`INSERT INTO warmup_received (email_account_id, internal_id, message_id, sender_account_id, created_at)
		      VALUES ($1, gen_random_uuid(), $2, $3, $4)`, recipient, msg, sender, day.Add(time.Duration(i)*time.Minute))
	}
	// One spam report matches a receipt; the one with no message id matches nothing.
	exec(`INSERT INTO warmup_spam_reports (reporter_account_id, reported_account_id, message_id, report_type)
	      VALUES ($1, $2, '<b@x>', 'spam_placement'), ($1, $2, '', 'spam_placement')`, recipient, sender)

	repo := NewWarmupPlacementRepository(handle)
	// Too fresh for the sweep: the live path gets the first chance.
	if n, err := repo.SweepUnplaced(ctx, day, 100); err != nil || n != 0 {
		t.Fatalf("sweep before the grace = %d, %v; want 0", n, err)
	}
	if n, err := repo.SweepUnplaced(ctx, day.Add(time.Hour), 2); err != nil || n != 2 {
		t.Fatalf("first batch = %d, %v; want 2", n, err)
	}
	if n, err := repo.SweepUnplaced(ctx, day.Add(time.Hour), 100); err != nil || n != 2 {
		t.Fatalf("second batch = %d, %v; want the other 2", n, err)
	}
	if n, err := repo.SweepUnplaced(ctx, day.Add(time.Hour), 100); err != nil || n != 0 {
		t.Fatalf("third batch = %d, %v; want nothing left", n, err)
	}
	rows, err := repo.Daily(ctx, orgID, &sender, day, day)
	if err != nil {
		t.Fatalf("Daily: %v", err)
	}
	if len(rows) != 1 || rows[0].Group != "microsoft" || rows[0].Inbox != 3 || rows[0].Spam != 1 || rows[0].Tabs != 0 {
		t.Fatalf("swept rows = %+v, want 3 inbox and 1 spam at microsoft", rows)
	}
}
