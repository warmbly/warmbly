package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/config"
)

// The retention sweep reaches only warmup mail past its window, on a mailbox
// that can act, and each mailbox's own window wins over the instance's.
func TestLiveWarmupMailRetention(t *testing.T) {
	_, pool := liveContactDB(t)
	ctx := context.Background()
	userID, orgID := uuid.New(), uuid.New()
	workerID := uuid.New()
	// following: no window of its own, so the instance's 30 days apply.
	// strict: a 5-day window of its own. idle: no worker, so nothing can act.
	following, strict, idle, partner := uuid.New(), uuid.New(), uuid.New(), uuid.New()

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email) VALUES ($1, 'Warmup', 'Retention', $2)`,
		userID, "warmup-retention-"+uuid.NewString()+"@example.test")
	exec(`INSERT INTO organizations (id, name, owner_user_id) VALUES ($1, 'Warmup retention', $2)`, orgID, userID)
	exec(`INSERT INTO fleet_nodes (id, role, name) VALUES ($1, 'worker', 'retention-test')
	      ON CONFLICT DO NOTHING`, workerID)
	exec(`INSERT INTO workers (id) VALUES ($1) ON CONFLICT DO NOTHING`, workerID)
	mailbox := func(id uuid.UUID, name, provider string, worker *uuid.UUID, retention *int) {
		exec(`INSERT INTO email_accounts
		        (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, status, worker_id, warmup_retention_days, warmup_folder)
		      VALUES ($1, $2, $3, $4, $5, '', '', $6, 'active', $7, $8, 'Reputation')`,
			id, userID, orgID, "warmup-retention-"+uuid.NewString()+"@example.test", name, provider, worker, retention)
	}
	five := 5
	// following is on Gmail, which files a sent copy of its own; strict is an
	// SMTP mailbox, which never has one.
	mailbox(following, "following", "gmail", &workerID, nil)
	mailbox(strict, "strict", "smtp_imap", &workerID, &five)
	mailbox(idle, "idle", "smtp_imap", nil, nil)
	mailbox(partner, "partner", "smtp_imap", &workerID, nil)

	received := func(account uuid.UUID, age time.Duration, msgID string) uuid.UUID {
		internal := uuid.New()
		exec(`INSERT INTO warmup_received (email_account_id, internal_id, message_id, sender_account_id, created_at)
		      VALUES ($1, $2, $3, $4, NOW() - $5::interval)`, account, internal, msgID, partner, age)
		exec(`INSERT INTO email_message_map (user_id, email_id, message_id, id) VALUES ($1, $2, $3, $4)`,
			userID, account, msgID, internal)
		return internal
	}
	oldFollowing := received(following, 40*24*time.Hour, "<old-following@example.test>")
	freshFollowing := received(following, 10*24*time.Hour, "<fresh-following@example.test>")
	oldStrict := received(strict, 10*24*time.Hour, "<old-strict@example.test>")
	freshStrict := received(strict, 2*24*time.Hour, "<fresh-strict@example.test>")
	oldIdle := received(idle, 40*24*time.Hour, "<old-idle@example.test>")

	sentCopy := func(sender uuid.UUID, age time.Duration, msgID string) uuid.UUID {
		token, task := uuid.New(), uuid.New()
		exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id)
		      VALUES ($1, 'warmup', $2, 'completed', $3)`, task, sender, msgID)
		exec(`INSERT INTO warmup_tokens (token, task_id, sender_account_id, recipient_account_id, sent_message_id, created_at)
		      VALUES ($1, $2, $3, $4, $5, NOW() - $6::interval)`, token, task, sender, partner, msgID, age)
		return token
	}
	oldSent := sentCopy(following, 40*24*time.Hour, "<old-sent@example.test>")
	freshSent := sentCopy(following, 10*24*time.Hour, "<fresh-sent@example.test>")
	smtpSent := sentCopy(strict, 40*24*time.Hour, "<smtp-sent@example.test>")
	exec(`INSERT INTO email_message_map (user_id, email_id, message_id, id) VALUES ($1, $2, '<old-sent@example.test>', $3)`,
		userID, following, uuid.New())

	t.Cleanup(func() {
		for _, step := range []struct {
			query string
			arg   uuid.UUID
		}{
			{`DELETE FROM warmup_received WHERE email_account_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)`, orgID},
			{`DELETE FROM warmup_tokens WHERE sender_account_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)`, orgID},
			{`DELETE FROM tasks WHERE email_account_id IN (SELECT id FROM email_accounts WHERE organization_id = $1)`, orgID},
			{`DELETE FROM email_message_map WHERE user_id = $1`, userID},
			{`DELETE FROM email_accounts WHERE organization_id = $1`, orgID},
			{`DELETE FROM organizations WHERE id = $1`, orgID},
			{`DELETE FROM users WHERE id = $1`, userID},
			{`DELETE FROM workers WHERE id = $1`, workerID},
			{`DELETE FROM fleet_nodes WHERE id = $1`, workerID},
		} {
			if _, err := pool.Exec(context.Background(), step.query, step.arg); err != nil {
				t.Errorf("cleanup: %v", err)
			}
		}
	})

	repo := &warmupRepository{db: pool}

	rows, err := repo.ListWarmupMailToRetire(ctx, 30, 100, nil)
	if err != nil {
		t.Fatalf("ListWarmupMailToRetire: %v", err)
	}
	got := map[uuid.UUID]WarmupMailToRetire{}
	for _, r := range rows {
		if r.EmailAccountID == following || r.EmailAccountID == strict || r.EmailAccountID == idle {
			got[r.InternalID] = r
		}
	}
	if len(got) != 2 {
		t.Fatalf("listed %d of this org's receipts, want the two past their windows: %+v", len(got), got)
	}
	for _, want := range []uuid.UUID{oldFollowing, oldStrict} {
		if _, ok := got[want]; !ok {
			t.Errorf("receipt %s past its window was not listed", want)
		}
	}
	for _, kept := range []uuid.UUID{freshFollowing, freshStrict, oldIdle} {
		if _, ok := got[kept]; ok {
			t.Errorf("receipt %s was listed: inside its window, or on a mailbox with no worker", kept)
		}
	}
	// Oldest first, with everything the worker needs to find the message.
	r := got[oldFollowing]
	if r.WorkerID != workerID || r.UserID != userID || r.MessageID != "<old-following@example.test>" ||
		r.ProviderKey != "<old-following@example.test>" || r.Folder != "Reputation" {
		t.Fatalf("listed row = %+v, want the worker, user, Message-ID, map key and folder", r)
	}

	// Retiring takes the row out of the next listing.
	if err := repo.RetireWarmupReceived(ctx, following, oldFollowing); err != nil {
		t.Fatalf("RetireWarmupReceived: %v", err)
	}
	rec, err := repo.GetWarmupReceived(ctx, following, oldFollowing)
	if err != nil || rec == nil || rec.RetiredAt == nil {
		t.Fatalf("GetWarmupReceived after retiring = %+v, %v; want retired_at set", rec, err)
	}
	rows, err = repo.ListWarmupMailToRetire(ctx, 30, 100, nil)
	if err != nil {
		t.Fatalf("ListWarmupMailToRetire again: %v", err)
	}
	for _, r := range rows {
		if r.InternalID == oldFollowing {
			t.Fatal("a retired receipt was offered again")
		}
	}

	// The sender's own copies follow the same window, and the map resolves
	// the provider key where it is keyed by Message-ID.
	sent, err := repo.ListWarmupSentCopiesToRetire(ctx, 30, 100, nil)
	if err != nil {
		t.Fatalf("ListWarmupSentCopiesToRetire: %v", err)
	}
	var listedOld, listedFresh, listedSMTP bool
	for _, r := range sent {
		switch r.Token {
		case oldSent:
			listedOld = true
			if r.ProviderKey != "<old-sent@example.test>" || r.MessageID != "<old-sent@example.test>" || r.EmailAccountID != following {
				t.Fatalf("sent copy row = %+v", r)
			}
		case freshSent:
			listedFresh = true
		case smtpSent:
			listedSMTP = true
		}
	}
	if !listedOld || listedFresh || listedSMTP {
		t.Fatalf("sent copies listed: old=%v fresh=%v smtp=%v, want only the old Gmail one", listedOld, listedFresh, listedSMTP)
	}
	if err := repo.RetireWarmupSentCopy(ctx, oldSent); err != nil {
		t.Fatalf("RetireWarmupSentCopy: %v", err)
	}

	// The prune drops retired receipts and retired tokens past the window,
	// and leaves a receipt whose mail may still be in the mailbox.
	exec(`UPDATE warmup_received SET created_at = NOW() - interval '400 days' WHERE internal_id IN ($1, $2)`, oldFollowing, oldIdle)
	exec(`UPDATE warmup_tokens SET created_at = NOW() - interval '400 days' WHERE token IN ($1, $2)`, oldSent, smtpSent)
	// Owner activity keeps its own window, whatever the instance's is.
	exec(`INSERT INTO mailbox_owner_activity (email_account_id, bucket) VALUES
	        ($1, NOW() - make_interval(days => $2::int + 1)), ($1, NOW() - interval '1 day')`,
		following, config.WarmupOwnerActivityKeepDays)
	// A thread turn goes with the event window unless its mailbox still keeps that mail.
	exec(`UPDATE email_accounts SET warmup_retention_days = 500 WHERE id = $1`, idle)
	exec(`INSERT INTO warmup_thread_messages (email_account_id, message_id, created_at) VALUES
	        ($1, 'turn-past-every-window@example.test', NOW() - interval '400 days'),
	        ($2, 'turn-in-its-mail-window@example.test', NOW() - interval '400 days')`, following, idle)
	if _, err := repo.PruneWarmupEventsBefore(ctx, time.Now().AddDate(0, 0, -365), 30); err != nil {
		t.Fatalf("PruneWarmupEventsBefore: %v", err)
	}
	if rec, _ := repo.GetWarmupReceived(ctx, following, oldFollowing); rec != nil {
		t.Fatal("a retired receipt past the window survived the prune")
	}
	if rec, _ := repo.GetWarmupReceived(ctx, idle, oldIdle); rec == nil {
		t.Fatal("a receipt whose mail was never retired was pruned; its removal could no longer be told from tampering")
	}
	var tokens int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM warmup_tokens WHERE token IN ($1, $2)`, oldSent, smtpSent).Scan(&tokens); err != nil || tokens != 0 {
		t.Fatalf("retired and SMTP tokens past the window: count=%d err=%v, want both pruned", tokens, err)
	}
	var turns []string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(array_agg(message_id), '{}') FROM warmup_thread_messages WHERE email_account_id IN ($1, $2)`, following, idle).Scan(&turns); err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 || turns[0] != "turn-in-its-mail-window@example.test" {
		t.Fatalf("thread turns after the prune: %v, want only the one its mailbox still keeps", turns)
	}
	var buckets int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM mailbox_owner_activity WHERE email_account_id = $1`, following).Scan(&buckets); err != nil || buckets != 1 {
		t.Fatalf("owner activity after the prune: count=%d err=%v, want only the recent bucket", buckets, err)
	}
}
