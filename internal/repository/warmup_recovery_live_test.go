package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveWarmupRecoverySurvivesMailboxReplacement(t *testing.T) {
	database, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 265)
	ctx := context.Background()
	f := newSharedOrgFixture(t, pool)
	other := newSharedOrgFixture(t, pool)
	repo := NewWarmupRecoveryRepository(pool)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	mailbox := func(org, id uuid.UUID, address string) {
		exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, status)
			VALUES ($1, $2, $3, $4, 'Recovery', '', '', 'gmail', 'active')`, id, f.owner, org, address)
		t.Cleanup(func() { exec(`DELETE FROM email_accounts WHERE id = $1`, id) })
	}
	old, replacement, partner := uuid.New(), uuid.New(), uuid.New()
	address := "recovery-" + uuid.NewString() + "@example.test"
	mailbox(f.org, old, address)
	mailbox(f.org, partner, "partner-"+uuid.NewString()+"@example.test")
	token, task := uuid.New(), uuid.New()
	exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id) VALUES ($1, 'warmup', $2, 'completed', '<submitted@example.test>')`, task, partner)
	exec(`INSERT INTO warmup_tokens (token, task_id, sender_account_id, recipient_account_id, sent_message_id)
		VALUES ($1, $2, $3, $4, '')`, token, task, partner, old)
	exec(`UPDATE warmup_tokens SET sent_message_id = '<sent@example.test>' WHERE token = $1`, token)
	exec(`INSERT INTO warmup_received (email_account_id, internal_id, message_id, sender_account_id)
		VALUES ($1, $2, '<receipt@example.test>', $3)`, old, uuid.New(), partner)
	exec(`INSERT INTO warmup_thread_messages (email_account_id, message_id) VALUES ($1, '<reply@example.test>')`, old)
	ids, err := (&emailRepository{DB: database}).WarmupDisconnectMessageIDs(ctx, old, 200)
	if err != nil || len(ids) != 3 {
		t.Fatalf("pre-disconnect snapshot omitted warmup identifiers: %+v %v", ids, err)
	}
	func() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		for _, suffix := range []string{"down", "up"} {
			migration, err := os.ReadFile("../infrastructure/db/migrations/000265_warmup_recovery." + suffix + ".sql")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, string(migration)); err != nil {
				t.Fatalf("migration %s: %v", suffix, err)
			}
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM warmup_recovery_identifiers WHERE organization_id = $1`, f.org).Scan(&count); err != nil || count < 4 {
			t.Fatalf("migration did not backfill existing warmup records: count=%d err=%v", count, err)
		}
	}()
	t.Cleanup(func() { exec(`DELETE FROM tasks WHERE id = $1`, task) })
	exec(`DELETE FROM email_accounts WHERE id = $1`, old)
	mailbox(f.org, replacement, address)
	foreign, neighbor := uuid.New(), uuid.New()
	mailbox(other.org, foreign, address)
	mailbox(f.org, neighbor, "neighbor-"+uuid.NewString()+"@example.test")
	for _, tc := range []struct {
		name    string
		account uuid.UUID
		token   string
		message string
		want    bool
	}{
		{"confirmed send", replacement, "", "<sent@example.test>", true},
		{"receipt", replacement, "", " receipt@example.test ", true},
		{"thread turn", replacement, "", "<reply@example.test>", true},
		{"token-only", replacement, token.String(), "", true},
		{"ordinary message", replacement, "", "<normal@example.test>", false},
		{"another workspace", foreign, token.String(), "<receipt@example.test>", false},
		{"another address", neighbor, token.String(), "<receipt@example.test>", false},
		{"case-sensitive message ID", replacement, "", "<Receipt@example.test>", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			known, err := repo.IsKnown(ctx, tc.account, tc.token, []string{tc.message})
			if err != nil || known != tc.want {
				t.Fatalf("known=%v want=%v err=%v", known, tc.want, err)
			}
		})
	}
	exec(`UPDATE email_accounts SET email = upper(email) WHERE id = $1`, replacement)
	if known, err := repo.IsKnown(ctx, replacement, "", []string{"<receipt@example.test>"}); err != nil || !known {
		t.Fatalf("address case change lost recovery: known=%v err=%v", known, err)
	}
	exec(`UPDATE warmup_recovery_identifiers SET expires_at = NOW() - INTERVAL '1 second' WHERE organization_id = $1`, f.org)
	if known, err := repo.IsKnown(ctx, replacement, token.String(), []string{"<receipt@example.test>"}); err != nil || known {
		t.Fatalf("expired fingerprint matched: known=%v err=%v", known, err)
	}
	if err := repo.PurgeExpiredIdentifiers(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM warmup_recovery_identifiers WHERE organization_id = $1`, f.org).Scan(&count); err != nil || count != 0 {
		t.Fatalf("expired records not purged: count=%d err=%v", count, err)
	}
}

func TestLiveWarmupFilingRequiresAcknowledgement(t *testing.T) {
	_, pool := liveContactDB(t)
	requireSchemaVersion(t, pool, 265)
	ctx := context.Background()
	f := newSharedOrgFixture(t, pool)
	account, waitingAccount, worker := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO fleet_nodes (id, role, name) VALUES ($1, 'worker', 'filing-test')`, worker); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM fleet_nodes WHERE id = $1`, worker); err != nil {
			t.Error(err)
		}
	})
	if _, err := pool.Exec(ctx, `INSERT INTO workers (id) VALUES ($1)`, worker); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, status)
		VALUES ($1, $2, $3, $4, 'Filing', '', '', 'gmail', 'active')`, account, f.owner, f.org, uuid.NewString()+"@example.test"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM email_accounts WHERE id = ANY($1)`, []uuid.UUID{account, waitingAccount}); err != nil {
			t.Error(err)
		}
	})
	repo := NewWarmupRecoveryRepository(pool)
	action := models.WarmupEmailAction{EmailID: account, UserID: f.owner, RFCMessageID: "<retry@example.test>", Actions: []string{models.WarmupActionFile}}
	id, err := repo.EnqueueFiling(ctx, action)
	if err != nil {
		t.Fatal(err)
	}
	action.RFCMessageID = "retry@example.test"
	duplicate, err := repo.EnqueueFiling(ctx, action)
	if err != nil || id != duplicate {
		t.Fatalf("duplicate filing: %v, %v, %v", id, duplicate, err)
	}
	if claimed, err := repo.ClaimFilings(ctx, 1); err != nil || len(claimed) != 0 {
		t.Fatalf("unassigned filing consumed a claim: %+v %v", claimed, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO email_accounts (id, user_id, organization_id, email, name, signature_plain, signature_html, provider, status)
		VALUES ($1, $2, $3, $4, 'Waiting', '', '', 'gmail', 'active')`, waitingAccount, f.owner, f.org, uuid.NewString()+"@example.test"); err != nil {
		t.Fatal(err)
	}
	waitingAction := action
	waitingAction.EmailID = waitingAccount
	waitingID, err := repo.EnqueueFiling(ctx, waitingAction)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE warmup_pending_filings SET next_attempt_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, waitingID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE email_accounts SET worker_id = $2 WHERE id = $1`, account, worker); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimFilings(ctx, 1)
	if err != nil || len(claimed) != 1 || claimed[0].FilingID != id.String() || len(claimed[0].Actions) != 1 {
		t.Fatalf("claim: %+v %v", claimed, err)
	}
	if claimed, err := repo.ClaimFilings(ctx, 100); err != nil || len(claimed) != 0 {
		t.Fatalf("lease did not prevent duplicate dispatch: %+v %v", claimed, err)
	}
	for _, backoff := range []struct {
		age   time.Duration
		delay time.Duration
	}{{10 * time.Minute, 5 * time.Minute}, {25 * time.Minute, 15 * time.Minute}, {2 * time.Hour, 30 * time.Minute}} {
		if _, err := pool.Exec(ctx, `UPDATE warmup_pending_filings SET created_at=$2,next_attempt_at=NOW()-INTERVAL '1 second' WHERE id=$1`, id, time.Now().Add(-backoff.age)); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.EnqueueFiling(ctx, action); err != nil {
			t.Fatal(err)
		}
		if claimed, err := repo.ClaimFilings(ctx, 100); err != nil || len(claimed) != 1 || claimed[0].FilingID != id.String() {
			t.Fatalf("durable retry was lost: %+v %v", claimed, err)
		}
		var next time.Time
		if err := pool.QueryRow(ctx, `SELECT next_attempt_at FROM warmup_pending_filings WHERE id=$1`, id).Scan(&next); err != nil {
			t.Fatal(err)
		}
		if delay := time.Until(next); delay < backoff.delay-time.Minute || delay > backoff.delay+time.Minute {
			t.Fatalf("age=%v delay=%v want=%v", backoff.age, delay, backoff.delay)
		}
		if claimed, err := repo.ClaimFilings(ctx, 100); err != nil || len(claimed) != 0 {
			t.Fatalf("old filing flooded the retry loop: %+v %v", claimed, err)
		}
	}
	var waiting bool
	if err := pool.QueryRow(ctx, `SELECT next_attempt_at < NOW() - INTERVAL '30 minutes' FROM warmup_pending_filings WHERE id = $1`, waitingID).Scan(&waiting); err != nil || !waiting {
		t.Fatalf("unassigned backlog was leased instead of remaining pending: %v %v", waiting, err)
	}
	if err := repo.CompleteFiling(ctx, uuid.New(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE warmup_pending_filings SET next_attempt_at = NOW() - INTERVAL '1 second' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	claimed, err = repo.ClaimFilings(ctx, 100)
	if err != nil || len(claimed) != 1 || claimed[0].FilingID != id.String() {
		t.Fatalf("unacknowledged filing did not retry: %+v %v", claimed, err)
	}
	if err := repo.RememberMessage(ctx, account, action.RFCMessageID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteFiling(ctx, account, id); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM warmup_pending_filings WHERE email_account_id = $1`, account).Scan(&count); err != nil || count != 0 {
		t.Fatalf("acknowledged filing retained: count=%d err=%v", count, err)
	}
}
