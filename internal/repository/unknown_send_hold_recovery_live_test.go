package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

func recoverHolds(t *testing.T, f *campaignAdmissionFixture) int {
	t.Helper()
	now := time.Now()
	n, err := f.r.RecoverUnknownSendHolds(t.Context(), now.Add(-10*time.Minute), now.Add(-30*time.Minute), 100)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func assertMailboxFree(t *testing.T, f *campaignAdmissionFixture) {
	t.Helper()
	admission, err := f.r.GetSendAdmission(t.Context(), f.org, f.sender, models.InboxProviderSMTPIMAP, time.Now())
	if err != nil || admission.RecoveryHold || !admission.Allowed {
		t.Fatalf("mailbox still held: %+v %v", admission, err)
	}
}

func TestLiveUnstartedCampaignSendReleasesMailboxAndCannotStartLater(t *testing.T) {
	f := newCampaignAdmissionFixture(t, 100)
	request, contact := f.request(t, f.sender)
	nonce, err := f.r.ReserveOutbound(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if n := recoverHolds(t, f); n != 0 {
		t.Fatalf("a fresh reservation was released: %d", n)
	}
	f.exec(t, `UPDATE tasks SET send_reserved_at=NOW()-INTERVAL '1 hour' WHERE id=$1`, request.TaskID)
	if n := recoverHolds(t, f); n != 1 {
		t.Fatalf("recovered=%d", n)
	}
	assertMailboxFree(t, f)
	state, err := f.r.BeginOutbound(t.Context(), request.TaskID, f.sender, f.worker, nonce)
	if err == nil && state.State == "execute" {
		t.Fatal("a released send started afterwards")
	}
	var dispatched bool
	if err := f.pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM campaign_contact_progress WHERE campaign_id=$1 AND contact_id=$2 AND dispatched_at IS NOT NULL)`, f.campaign, contact).Scan(&dispatched); err != nil || dispatched {
		t.Fatalf("the step was not handed back for a fresh send: %v %v", dispatched, err)
	}
}

func TestLiveStartedManualSendLiftsHoldWithoutRunningAgain(t *testing.T) {
	f := newCampaignAdmissionFixture(t, 100)
	task, nonce := uuid.New(), uuid.New()
	f.exec(t, `INSERT INTO tasks(id,task_type,email_account_id,status,message_id,send_result_state,send_executor_nonce,send_executor_worker,
		send_reserved_at,send_executor_started_at,completed_at) VALUES($1,'email',$2,'completed','','unknown',$3,$4,NOW()-INTERVAL '1 hour',NOW()-INTERVAL '1 hour',NOW()-INTERVAL '1 hour')`,
		task, f.sender, nonce, f.worker)
	f.exec(t, `UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='unknown',send_recovery_task_id=$2 WHERE id=$1`, f.sender, task)
	if n := recoverHolds(t, f); n != 1 {
		t.Fatalf("recovered=%d", n)
	}
	assertMailboxFree(t, f)
	// A repeated unknown report from the worker must not put the hold back.
	if err := f.r.ApplySendResult(t.Context(), models.SendEmailResult{TaskID: task}, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	assertMailboxFree(t, f)
	var lifted time.Time
	if err := f.pool.QueryRow(t.Context(), `SELECT send_hold_lifted_at FROM tasks WHERE id=$1`, task).Scan(&lifted); err != nil {
		t.Fatal(err)
	}
	// An older replica that does not know the lift can put the hold back; the next sweep lifts it again.
	f.exec(t, `UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='unknown',send_recovery_task_id=$2 WHERE id=$1`, f.sender, task)
	if n := recoverHolds(t, f); n != 1 {
		t.Fatalf("re-held lifted send recovered=%d", n)
	}
	assertMailboxFree(t, f)
	var relifted time.Time
	if err := f.pool.QueryRow(t.Context(), `SELECT send_hold_lifted_at FROM tasks WHERE id=$1`, task).Scan(&relifted); err != nil || !relifted.Equal(lifted) {
		t.Fatalf("the first lift was not kept: %v -> %v %v", lifted, relifted, err)
	}
	state, err := f.r.InspectOutbound(t.Context(), task, f.sender, f.worker)
	if err != nil || state.State != "started" {
		t.Fatalf("lifted send became runnable: %+v %v", state, err)
	}
}

func TestLiveHoldOnMissingTaskIsCleared(t *testing.T) {
	f := newCampaignAdmissionFixture(t, 100)
	f.exec(t, `UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='unknown',send_recovery_task_id=$2 WHERE id=$1`, f.sender, uuid.New())
	if n := recoverHolds(t, f); n != 1 {
		t.Fatalf("recovered=%d", n)
	}
	assertMailboxFree(t, f)
}
