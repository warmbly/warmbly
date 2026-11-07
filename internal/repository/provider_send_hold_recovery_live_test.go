package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveProviderRestoredHoldReleasesOnceTheMailboxSyncsAgain(t *testing.T) {
	f := newCampaignAdmissionFixture(t, 100)
	release := func() int {
		t.Helper()
		n, err := f.r.ReleaseProviderRestoredHolds(t.Context(), time.Now().Add(-6*time.Hour), 100)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	refuse := func() uuid.UUID {
		t.Helper()
		task := uuid.New()
		f.exec(t, `INSERT INTO tasks(id,task_type,email_account_id,status,message_id,send_result_state,completed_at,send_result_applied_at)
			VALUES($1,'warmup',$2,'failed','','failed',NOW()-INTERVAL '2 hours',NOW()-INTERVAL '2 hours')`, task, f.sender)
		f.exec(t, `UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='permanent',send_recovery_task_id=$2,send_cooldown_provider=provider::text WHERE id=$1`, f.sender, task)
		return task
	}
	synced := func(ago time.Duration) {
		t.Helper()
		f.exec(t, `INSERT INTO email_sync_state(email_id,user_id,last_synced_at) VALUES($1,$2,$3)
			ON CONFLICT(email_id) DO UPDATE SET last_synced_at=EXCLUDED.last_synced_at`, f.sender, f.user, time.Now().Add(-ago))
	}
	f.exec(t, `UPDATE email_accounts SET provider='gmail' WHERE id=$1`, f.sender)
	first := refuse()

	synced(3 * time.Hour)
	if n := release(); n != 0 {
		t.Fatalf("a sync from before the refusal released the hold: %d", n)
	}

	f.exec(t, `UPDATE email_accounts SET provider='smtp_imap',send_cooldown_provider='smtp_imap' WHERE id=$1`, f.sender)
	synced(time.Minute)
	if n := release(); n != 0 {
		t.Fatalf("an IMAP sync released an SMTP refusal: %d", n)
	}
	f.exec(t, `UPDATE email_accounts SET provider='gmail',send_cooldown_provider='gmail' WHERE id=$1`, f.sender)

	if n := release(); n != 1 {
		t.Fatalf("released=%d", n)
	}
	admission, err := f.r.GetSendAdmission(t.Context(), f.org, f.sender, models.InboxProviderGoogle, time.Now())
	if err != nil || admission.RecoveryHold || !admission.Allowed {
		t.Fatalf("mailbox still held: %+v %v", admission, err)
	}
	var recorded int
	if err := f.pool.QueryRow(t.Context(), `SELECT count(*) FROM send_recovery_resolutions WHERE email_account_id=$1 AND recovery_task_id=$2
		AND previous_reason='permanent' AND evidence_type='provider_access_restored'`, f.sender, first).Scan(&recorded); err != nil || recorded != 1 {
		t.Fatalf("release not recorded: %d %v", recorded, err)
	}

	// The provider still refuses: the next refusal holds again and waits out the recheck interval.
	refuse()
	if n := release(); n != 0 {
		t.Fatalf("a repeated refusal was released inside the recheck interval: %d", n)
	}
	f.exec(t, `UPDATE send_recovery_resolutions SET created_at=NOW()-INTERVAL '7 hours' WHERE email_account_id=$1`, f.sender)

	unknown := uuid.New()
	f.exec(t, `INSERT INTO tasks(id,task_type,email_account_id,status,message_id,send_result_state) VALUES($1,'warmup',$2,'failed','','unknown')`, unknown, f.sender)
	if n := release(); n != 1 {
		t.Fatalf("released=%d", n)
	}
	var held bool
	var reason *string
	if err := f.pool.QueryRow(t.Context(), `SELECT send_recovery_hold,send_recovery_reason FROM email_accounts WHERE id=$1`, f.sender).Scan(&held, &reason); err != nil {
		t.Fatal(err)
	}
	if !held || reason == nil || *reason != "unknown" {
		t.Fatalf("an unresolved send must keep the mailbox held as unknown, got %v %v", held, reason)
	}

	// With the refused task gone there is no refusal time, so only a recent sync counts.
	f.exec(t, `UPDATE tasks SET send_result_state='failed' WHERE id=$1`, unknown)
	f.exec(t, `UPDATE send_recovery_resolutions SET created_at=NOW()-INTERVAL '7 hours' WHERE email_account_id=$1`, f.sender)
	f.exec(t, `UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='permanent',send_recovery_task_id=$2 WHERE id=$1`, f.sender, uuid.New())
	synced(2 * time.Hour)
	if n := release(); n != 0 {
		t.Fatalf("a stale sync released a hold with no refused task: %d", n)
	}
	synced(time.Minute)
	if n := release(); n != 1 {
		t.Fatalf("released=%d", n)
	}
}
