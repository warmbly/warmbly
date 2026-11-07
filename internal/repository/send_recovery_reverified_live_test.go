package repository

import (
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
)

func TestLiveReconnectReleasesAuthenticationAndPermanentHolds(t *testing.T) {
	f, _ := lineageFixture(t)
	ctx := t.Context()
	r := &emailRepository{DB: &db.DB{Pool: f.pool}}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	hold := func() (bool, string) {
		t.Helper()
		var held bool
		var reason *string
		if err := f.pool.QueryRow(ctx, `SELECT send_recovery_hold,send_recovery_reason FROM email_accounts WHERE id=$1`, f.sender).Scan(&held, &reason); err != nil {
			t.Fatal(err)
		}
		if reason == nil {
			return held, ""
		}
		return held, *reason
	}
	failed := uuid.New()
	exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id,send_result_state,completed_at) VALUES($1,'warmup',$2,'failed','','failed',NOW())`, failed, f.sender)

	exec(`UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='conflict',send_recovery_task_id=$2 WHERE id=$1`, f.sender, failed)
	if released, err := r.ReleaseReconnectedHold(ctx, f.org, f.sender); err != nil || released {
		t.Fatalf("a conflict hold was released by a reconnect: released=%v err=%v", released, err)
	}

	exec(`UPDATE email_accounts SET send_recovery_reason='permanent' WHERE id=$1`, f.sender)
	if released, err := r.ReleaseReconnectedHold(ctx, f.org, f.sender); err != nil || !released {
		t.Fatalf("permanent hold not released by a reconnect: released=%v err=%v", released, err)
	}
	if held, reason := hold(); held || reason != "" {
		t.Fatalf("still held: %v %q", held, reason)
	}
	exec(`UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='permanent',send_recovery_task_id=$2 WHERE id=$1`, f.sender, uuid.New())
	if released, err := r.ReleaseReconnectedHold(ctx, f.org, f.sender); err != nil || !released {
		t.Fatalf("a hold whose refused task is gone was not released by a reconnect: released=%v err=%v", released, err)
	}
	exec(`UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='permanent',send_recovery_task_id=$2 WHERE id=$1`, f.sender, failed)
	if released, err := r.ReleaseReconnectedHold(ctx, uuid.New(), f.sender); err != nil || released {
		t.Fatalf("another workspace released the hold: released=%v err=%v", released, err)
	}
	if released, err := r.ReleaseReconnectedHold(ctx, f.org, f.sender); err != nil || !released {
		t.Fatalf("permanent hold not released by a reconnect: released=%v err=%v", released, err)
	}

	exec(`UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='authentication',send_recovery_task_id=$2 WHERE id=$1`, f.sender, failed)
	if released, err := r.ReleaseReconnectedHold(ctx, uuid.New(), f.sender); err != nil || released {
		t.Fatalf("another workspace released the hold: released=%v err=%v", released, err)
	}
	if released, err := r.ReleaseReconnectedHold(ctx, f.org, f.sender); err != nil || !released {
		t.Fatalf("authentication hold not released: released=%v err=%v", released, err)
	}
	if held, reason := hold(); held || reason != "" {
		t.Fatalf("still held: %v %q", held, reason)
	}
	var recorded int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM send_recovery_resolutions WHERE email_account_id=$1 AND previous_reason='authentication' AND evidence_type='credentials_reverified' AND recovery_task_id=$2`, f.sender, failed).Scan(&recorded); err != nil || recorded != 1 {
		t.Fatalf("release not recorded: %d %v", recorded, err)
	}

	unknown := uuid.New()
	exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id,send_result_state) VALUES($1,'warmup',$2,'failed','','unknown')`, unknown, f.sender)
	exec(`UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='authentication',send_recovery_task_id=$2 WHERE id=$1`, f.sender, failed)
	if released, err := r.ReleaseReconnectedHold(ctx, f.org, f.sender); err != nil || !released {
		t.Fatalf("authentication hold not released: released=%v err=%v", released, err)
	}
	if held, reason := hold(); !held || reason != "unknown" {
		t.Fatalf("an unresolved send must keep the mailbox held as unknown, got %v %q", held, reason)
	}
}
