package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

func TestLiveParticipationPreservesNullUntilExplicitActionAndStopsBothDirections(t *testing.T) {
	f, _ := lineageFixture(t)
	ctx := t.Context()
	r := NewEmailRepostory(&db.DB{Pool: f.pool}, nil)
	if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET test_mode=NULL, test_send_enabled=false, test_receive_enabled=false WHERE id=$1`, f.sender); err != nil {
		t.Fatal(err)
	}
	name := "Unrelated edit"
	row, xerr := r.Update(ctx, f.org.String(), f.sender.String(), &models.UpdateEmail{Name: &name})
	if xerr != nil || row.TestMode != nil || !row.TestSendingAllowed() || !row.TestReceivingAllowed() {
		t.Fatal("legacy NULL changed", row, xerr)
	}
	row, xerr = r.SetWarmupLifecycle(ctx, f.org.String(), f.sender.String(), "start")
	if xerr != nil || row.TestMode == nil || *row.TestMode != models.TestParticipationDiagnostic || !row.TestSendingAllowed() || !row.TestReceivingAllowed() || row.SyntheticActionsAllowed() {
		t.Fatal("explicit start lacked diagnostic consent", row, xerr)
	}
	started := *row.Warmup
	row, xerr = r.SetWarmupLifecycle(ctx, f.org.String(), f.sender.String(), "pause")
	if xerr != nil || row.TestSendingAllowed() || !row.TestReceivingAllowed() || !row.Warmup.Equal(started) {
		t.Fatal("pause lost receiving/history", row, xerr)
	}
	row, xerr = r.SetWarmupLifecycle(ctx, f.org.String(), f.sender.String(), "stop")
	if xerr != nil || row.TestSendingAllowed() || row.TestReceivingAllowed() || !row.Warmup.Equal(started) {
		t.Fatal("stop did not fence both directions", row, xerr)
	}
	enable := true
	row, xerr = r.Update(ctx, f.org.String(), f.sender.String(), &models.UpdateEmail{Warmup: &enable})
	if xerr != nil || row.TestMode == nil || *row.TestMode != models.TestParticipationDiagnostic || !row.TestSendingAllowed() || !row.TestReceivingAllowed() {
		t.Fatal("explicit legacy API/bulk start did not activate off mailbox", row, xerr)
	}
	diagnostic, receiveOnly := models.TestParticipationDiagnostic, false
	row, xerr = r.Update(ctx, f.org.String(), f.sender.String(), &models.UpdateEmail{Warmup: &enable, TestMode: &diagnostic, TestSendEnabled: &enable, TestReceiveEnabled: &receiveOnly})
	if xerr != nil || !row.TestSendingAllowed() || row.TestReceivingAllowed() {
		t.Fatal("Cloud-shaped sender-only update lost direction or duplicated SQL assignments", row, xerr)
	}
	if _, xerr = r.SetWarmupLifecycle(ctx, uuid.NewString(), f.sender.String(), "start"); xerr == nil {
		t.Fatal("cross-tenant consent accepted")
	}
	legacy := models.TestParticipationLegacy
	if _, xerr = r.Update(ctx, f.org.String(), f.sender.String(), &models.UpdateEmail{TestMode: &legacy}); xerr == nil {
		t.Fatal("explicit legacy reactivation accepted")
	}
	receive, send, daily, rolling := true, false, 8, 11
	row, xerr = r.Update(ctx, f.org.String(), f.sender.String(), &models.UpdateEmail{TestSendEnabled: &send, TestReceiveEnabled: &receive, SharedDailyLimit: &daily, RollingRecipientLimit: &rolling})
	if xerr != nil || row.TestMode == nil || *row.TestMode != models.TestParticipationDiagnostic || row.TestSendingAllowed() || !row.TestReceivingAllowed() || row.SharedDailyLimit == nil || *row.SharedDailyLimit != daily || row.RollingRecipientLimit == nil || *row.RollingRecipientLimit != rolling {
		t.Fatal("receive-only/limits not persisted", row, xerr)
	}
	zero := 0
	row, xerr = r.Update(ctx, f.org.String(), f.sender.String(), &models.UpdateEmail{SharedDailyLimit: &zero, RollingRecipientLimit: &zero})
	if xerr != nil || row.SharedDailyLimit != nil || row.RollingRecipientLimit != nil || row.TestSendingAllowed() || !row.TestReceivingAllowed() {
		t.Fatal("reset ceiling changed consent or failed to restore default", row, xerr)
	}
}

func TestLiveFailedProviderAttemptSurvivesRefundAndCapsOtherLanes(t *testing.T) {
	f, r := lineageFixture(t)
	ctx := t.Context()
	worker := uuid.New()
	exec := func(q string, a ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, q, a...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol)VALUES($1,'worker',true,NOW(),2)`, worker)
	exec(`INSERT INTO workers(id)VALUES($1)`, worker)
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, worker) })
	exec(`UPDATE email_accounts SET worker_id=$1, shared_daily_limit=1, rolling_recipient_limit=1 WHERE id=$2`, worker, f.sender)
	reservation := func(lane string) OutboundReservation {
		id := uuid.New()
		exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id)VALUES($1,$2,$3,'active','')`, id, lane, f.sender)
		return OutboundReservation{TaskID: id, MailboxID: f.sender, OrganizationID: f.org, WorkerID: worker, Provider: models.InboxProviderSMTPIMAP, Recipients: []string{"recipient@example.test"}}
	}
	in := reservation("email")
	nonce, err := r.ReserveOutbound(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		state, err := r.BeginOutbound(ctx, in.TaskID, in.MailboxID, in.WorkerID, nonce)
		if err != nil || state == nil {
			t.Fatal(state, err)
		}
	}
	result := models.SendEmailResult{TaskID: in.TaskID, Error: &models.EmailSendError{Code: "RECIPIENT_REJECTED", Failure: &errx.SendFailure{Provider: string(models.InboxProviderSMTPIMAP), Protocol: "smtp", Status: 550, Disposition: errx.SendPermanent, Scope: "recipient", ObservedAt: time.Now()}}}
	if err = r.FinishOutbound(ctx, in.TaskID, in.MailboxID, in.WorkerID, result); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = r.ApplySendResult(ctx, result, func(context.Context) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	var released bool
	var attempts int
	if err = f.pool.QueryRow(ctx, `SELECT send_released_at IS NOT NULL FROM tasks WHERE id=$1`, in.TaskID).Scan(&released); err != nil || !released {
		t.Fatal("failed capacity not refunded", released, err)
	}
	if err = f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbound_attempts WHERE task_id=$1`, in.TaskID).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatal("duplicate execution/result changed attempts", attempts, err)
	}
	for _, lane := range []string{"email", "placement"} {
		if _, err = r.ReserveOutbound(ctx, reservation(lane)); !errors.Is(err, ErrSendAdmissionDenied) {
			t.Fatal("refunded attempt escaped shared rate limit", lane, err)
		}
	}
}

func TestLiveDiagnosticUnknownRetryIsBoundedAndDefinitiveProofStopsFetches(t *testing.T) {
	f, r := lineageFixture(t)
	ctx := t.Context()
	worker, token := uuid.New(), uuid.New()
	exec := func(q string, a ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, q, a...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol)VALUES($1,'worker',true,NOW(),2)`, worker)
	exec(`INSERT INTO workers(id)VALUES($1)`, worker)
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, worker) })
	exec(`UPDATE email_accounts SET worker_id=$1 WHERE id=$2`, worker, f.recipient)
	exec(`INSERT INTO warmup_pool_participants(pool_id,email_account_id)SELECT id,$1 FROM warmup_pools WHERE pool_type='free'`, f.recipient)
	exec(`INSERT INTO warmup_tasks(task_id,lineage_version,subject,scenario_version,rendering_version,max_turns)VALUES($1,1,'Diagnostic','diagnostic-v1','canonical-v1',1)`, f.task)
	exec(`INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,sent_message_id)VALUES($1,$2,$3,$4,'<retry@example.test>')`, token, f.task, f.sender, f.recipient)
	req := models.DiagnosticAuthRequest{Token: token, MailboxID: f.recipient, WorkerID: worker, MessageID: "retry@example.test"}
	for attempt := 0; attempt < 3; attempt++ {
		grant, err := r.DiagnosticAuth(ctx, req)
		if err != nil || grant == nil {
			t.Fatal("unknown permanently lost retry", attempt, grant, err)
		}
		completion := req
		completion.Nonce = grant.Nonce
		completion.Result = &models.DiagnosticDKIMResult{DKIM: "unknown", Alignment: "unknown", Verifier: models.DiagnosticDKIMVerifier, ObservedAt: time.Now()}
		if _, err = r.DiagnosticAuth(ctx, completion); err != nil {
			t.Fatal(err)
		}
		if next, err := r.DiagnosticAuth(ctx, req); err != nil || next != nil {
			t.Fatal("retry bypassed spacing", next, err)
		}
		exec(`UPDATE diagnostic_auth_verifications SET authorized_at=NOW()-INTERVAL '1 minute' WHERE task_id=$1`, f.task)
	}
	if next, err := r.DiagnosticAuth(ctx, req); err != nil || next != nil {
		t.Fatal("retry budget exceeded", next, err)
	}
}
