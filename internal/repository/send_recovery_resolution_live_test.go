package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveSendRecoveryResolutionRequiresEvidenceAndRetainsUnknown(t *testing.T) {
	f, _ := lineageFixture(t)
	ctx := t.Context()
	repo := NewEmailRepostory(&db.DB{Pool: f.pool}, nil)
	held := f.task
	if _, err := f.pool.Exec(ctx, `UPDATE tasks SET status='failed',send_result_state='failed',send_result_applied_at=NOW(),completed_at=NOW()-INTERVAL '2 minutes' WHERE id=$1`, held); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='authentication',send_recovery_task_id=$2,last_synced_at=NOW()-INTERVAL '3 minutes' WHERE id=$1`, f.sender, held); err != nil {
		t.Fatal(err)
	}
	resolve := &models.UpdateEmail{SendRecoveryResolution: &models.SendRecoveryResolution{HeldTaskID: held, HeldReason: "authentication", EvidenceType: "authentication_repaired"}}
	if _, xerr := repo.Update(ctx, f.org.String(), f.sender.String(), resolve); xerr == nil {
		t.Fatal("stale sync cleared authentication hold")
	}
	unknown := uuid.New()
	if _, err := f.pool.Exec(ctx, `INSERT INTO tasks(id,task_type,email_account_id,status,message_id,send_result_state)VALUES($1,'email',$2,'active','','unknown')`, unknown, f.sender); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET last_synced_at=NOW() WHERE id=$1`, f.sender); err != nil {
		t.Fatal(err)
	}
	if _, xerr := repo.Update(ctx, f.org.String(), f.sender.String(), resolve); xerr == nil {
		t.Fatal("unknown send was discarded by authentication repair")
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM tasks WHERE id=$1`, unknown); err != nil {
		t.Fatal(err)
	}
	if _, xerr := repo.Update(ctx, f.org.String(), f.sender.String(), resolve); xerr == nil {
		t.Fatal("fresh inbox sync alone cleared outbound authentication hold")
	}
	evidence := uuid.New()
	if _, err := f.pool.Exec(ctx, `INSERT INTO tasks(id,task_type,email_account_id,status,message_id,send_result_state,send_result_applied_at,send_executor_started_at)VALUES($1,'email',$2,'completed','evidence@example.test','sent',NOW(),NOW())`, evidence, f.sender); err != nil {
		t.Fatal(err)
	}
	resolve.SendRecoveryResolution.EvidenceTaskID = &evidence
	if _, xerr := repo.Update(ctx, f.org.String(), f.sender.String(), resolve); xerr != nil {
		t.Fatal(xerr)
	}
	var heldNow bool
	var history int
	if err := f.pool.QueryRow(ctx, `SELECT send_recovery_hold FROM email_accounts WHERE id=$1`, f.sender).Scan(&heldNow); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM send_recovery_resolutions WHERE email_account_id=$1 AND recovery_task_id=$2 AND evidence_type='authentication_repaired'`, f.sender, held).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if heldNow || history != 1 {
		t.Fatal("repair was not atomically audited", heldNow, history)
	}

	if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='conflict',send_recovery_task_id=$2 WHERE id=$1`, f.sender, held); err != nil {
		t.Fatal(err)
	}
	reference := "provider trace reviewed at " + time.Now().UTC().Format(time.RFC3339)
	confirmation := &models.UpdateEmail{SendRecoveryResolution: &models.SendRecoveryResolution{HeldTaskID: held, HeldReason: "conflict", EvidenceType: "operator_provider_confirmation", EvidenceTaskID: &held, ConfirmationReference: reference}}
	if _, xerr := repo.Update(ctx, uuid.New().String(), f.sender.String(), confirmation); xerr == nil {
		t.Fatal("cross-organization resolution accepted")
	}
	if _, xerr := repo.Update(ctx, f.org.String(), f.sender.String(), confirmation); xerr != nil {
		t.Fatal(xerr)
	}
	if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM send_recovery_resolutions WHERE email_account_id=$1 AND previous_reason='conflict' AND confirmation_reference=$2`, f.sender, reference).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if history != 1 {
		t.Fatal("operator evidence reference was not retained", history)
	}
}
