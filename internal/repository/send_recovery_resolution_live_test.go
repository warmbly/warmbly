package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveUnknownResolutionRejectsMissingEvidenceAndLiveExecution(t *testing.T) {
	f, r := lineageFixture(t)
	ctx := t.Context()
	repo := NewEmailRepostory(&db.DB{Pool: f.pool}, nil)
	if _, err := f.pool.Exec(ctx, `UPDATE tasks SET status='completed',completed_at=NOW()-INTERVAL '5 minutes',send_reserved_at=NOW()-INTERVAL '5 minutes',send_result_state='unknown' WHERE id=$1`, f.task); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='unknown',send_recovery_task_id=$2 WHERE id=$1`, f.sender, f.task); err != nil {
		t.Fatal(err)
	}
	valid := models.SendRecoveryResolution{HeldTaskID: f.task, HeldReason: "unknown", EvidenceType: "operator_confirmed_not_sent", ConfirmationReference: "Provider ticket fixture-907"}
	for _, mutate := range []func(*models.SendRecoveryResolution){
		func(v *models.SendRecoveryResolution) { v.ConfirmationReference = " " },
		func(v *models.SendRecoveryResolution) { v.ConfirmationReference = "unsafe\nreference" },
		func(v *models.SendRecoveryResolution) { v.HeldTaskID = uuid.New() },
		func(v *models.SendRecoveryResolution) { v.HeldReason = "permanent" },
		func(v *models.SendRecoveryResolution) { v.EvidenceType = "no_sent_copy" },
		func(v *models.SendRecoveryResolution) { v.EvidenceType = "operator_confirmed_sent" },
		func(v *models.SendRecoveryResolution) { v.MessageID = "unsent-cannot-have-message@example.test" },
		func(v *models.SendRecoveryResolution) { v.EvidenceTaskID = &f.task },
	} {
		v := valid
		mutate(&v)
		if _, xerr := repo.Update(ctx, f.org.String(), f.sender.String(), &models.UpdateEmail{SendRecoveryResolution: &v}); xerr == nil {
			t.Fatalf("unsafe evidence accepted: %+v", v)
		}
	}
	if _, err := f.pool.Exec(ctx, `UPDATE tasks SET send_executor_started_at=NOW() WHERE id=$1`, f.task); err != nil {
		t.Fatal(err)
	}
	if _, xerr := repo.Update(ctx, f.org.String(), f.sender.String(), &models.UpdateEmail{SendRecoveryResolution: &valid}); xerr == nil {
		t.Fatal("live provider execution resolved")
	}
	if _, err := f.pool.Exec(ctx, `UPDATE tasks SET send_executor_started_at=NOW()-INTERVAL '5 minutes' WHERE id=$1`, f.task); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(models.SendEmailResult{TaskID: f.task, Success: true, MessageID: "already-sent@example.test"})
	if _, err := f.pool.Exec(ctx, `UPDATE tasks SET send_executor_result=$2 WHERE id=$1`, f.task, raw); err != nil {
		t.Fatal(err)
	}
	if _, xerr := repo.Update(ctx, f.org.String(), f.sender.String(), &models.UpdateEmail{SendRecoveryResolution: &valid}); xerr == nil {
		t.Fatal("operator overrode durable provider outcome")
	}
	if _, err := f.pool.Exec(ctx, `UPDATE tasks SET send_executor_result=NULL WHERE id=$1`, f.task); err != nil {
		t.Fatal(err)
	}
	other := uuid.New()
	if _, err := f.pool.Exec(ctx, `INSERT INTO tasks(id,email_account_id,task_type,status,message_id,send_result_state)VALUES($1,$2,'email','completed','','unknown')`, other, f.sender); err != nil {
		t.Fatal(err)
	}
	if _, xerr := repo.Update(ctx, f.org.String(), f.sender.String(), &models.UpdateEmail{SendRecoveryResolution: &valid}); xerr != nil {
		t.Fatal(xerr)
	}
	results, err := r.ListUnappliedSendResults(ctx, 200)
	if err != nil || len(results) != 1 {
		t.Fatal("confirmed outcome unavailable", err, len(results))
	}
	if err := r.ApplySendResult(ctx, results[0], func(ctx context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	var held bool
	var heldTask uuid.UUID
	if err := f.pool.QueryRow(ctx, `SELECT send_recovery_hold,send_recovery_task_id FROM email_accounts WHERE id=$1`, f.sender).Scan(&held, &heldTask); err != nil {
		t.Fatal(err)
	}
	if !held || heldTask != other {
		t.Fatal("resolving one unknown freed another", held, heldTask)
	}
	native := models.SendEmailResult{TaskID: f.task, Success: true, MessageID: "late@example.test"}
	raw, err = json.Marshal(native)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE tasks SET send_executor_result=$2 WHERE id=$1`, f.task, raw); err != nil {
		t.Fatal(err)
	}
	results, err = r.ListUnappliedSendResults(ctx, 200)
	if err != nil || len(results) != 1 || results[0].TaskID != f.task {
		t.Fatal("late definitive outcome unavailable", err, len(results))
	}
	if err := r.ApplySendResult(ctx, results[0], func(context.Context) error { t.Fatal("late result changed committed accounting"); return nil }); err != nil {
		t.Fatal(err)
	}
	var reason string
	if err := f.pool.QueryRow(ctx, `SELECT send_recovery_reason,send_recovery_task_id FROM email_accounts WHERE id=$1`, f.sender).Scan(&reason, &heldTask); err != nil {
		t.Fatal(err)
	}
	if reason != "unknown" || heldTask != other {
		t.Fatal("late conflict displaced a different unknown hold", reason, heldTask)
	}
	results, err = r.ListUnappliedSendResults(ctx, 200)
	if err != nil || len(results) != 0 {
		t.Fatal("resolved conflict replayed on every sweep", err, len(results))
	}
	if _, err := f.pool.Exec(ctx, `UPDATE tasks SET status='completed',send_reserved_at=NOW()-INTERVAL '5 minutes',completed_at=NOW()-INTERVAL '4 minutes' WHERE id=$1`, other); err != nil {
		t.Fatal(err)
	}
	second := models.SendRecoveryResolution{HeldTaskID: other, HeldReason: "unknown", EvidenceType: "operator_confirmed_sent", MessageID: "second@example.test", ConfirmationReference: "Provider ticket fixture-908"}
	if _, xerr := repo.Update(ctx, f.org.String(), f.sender.String(), &models.UpdateEmail{SendRecoveryResolution: &second}); xerr != nil {
		t.Fatal(xerr)
	}
	results, err = r.ListUnappliedSendResults(ctx, 200)
	if err != nil || len(results) != 1 || results[0].TaskID != other {
		t.Fatal("second confirmation unavailable", err, len(results))
	}
	if err := r.ApplySendResult(ctx, results[0], func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT send_recovery_reason,send_recovery_task_id FROM email_accounts WHERE id=$1`, f.sender).Scan(&reason, &heldTask); err != nil {
		t.Fatal(err)
	}
	if reason != "conflict" || heldTask != f.task {
		t.Fatal("late conflict disappeared after other unknown was resolved", reason, heldTask)
	}
}

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
