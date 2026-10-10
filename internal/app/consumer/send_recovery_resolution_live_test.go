package jobs

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestLiveUnknownSendOperatorResolutionReconcilesAndAudits(t *testing.T) {
	for _, sent := range []bool{false, true} {
		t.Run(map[bool]string{false: "not sent", true: "sent"}[sent], func(t *testing.T) {
			h := liveDB(t)
			f := newSendResultFixture(t, h)
			s := liveJobsService(h)
			id := f.stampSend(t, s)
			ctx := t.Context()
			if _, err := h.Pool.Exec(ctx, `UPDATE tasks SET send_result_state='unknown',send_reserved_at=NOW()-INTERVAL '5 minutes' WHERE id=$1`, id); err != nil {
				t.Fatal(err)
			}
			if err := s.HandleEmailFailed(ctx, models.SendEmailResult{TaskID: id}); err != nil {
				t.Fatal(err)
			}
			resolution := &models.SendRecoveryResolution{HeldTaskID: id, HeldReason: "unknown", EvidenceType: "operator_confirmed_not_sent", ConfirmationReference: "Provider support confirmation fixture-907"}
			if sent {
				resolution.EvidenceType = "operator_confirmed_sent"
				resolution.MessageID = "confirmed@example.test"
				resolution.ThreadID = "confirmed-thread"
			}
			repo := repository.NewEmailRepostory(h, nil)
			if _, xerr := repo.Update(ctx, uuid.NewString(), f.mailbox.String(), &models.UpdateEmail{SendRecoveryResolution: resolution}); xerr == nil {
				t.Fatal("foreign organization resolved send")
			}
			if _, xerr := repo.Update(ctx, f.org.String(), f.mailbox.String(), &models.UpdateEmail{SendRecoveryResolution: resolution}); xerr != nil {
				t.Fatal(xerr)
			}
			if _, xerr := repo.Update(ctx, f.org.String(), f.mailbox.String(), &models.UpdateEmail{SendRecoveryResolution: resolution}); xerr == nil {
				t.Fatal("duplicate confirmation overwrote audit")
			}
			var hold bool
			if err := h.Pool.QueryRow(ctx, `SELECT send_recovery_hold FROM email_accounts WHERE id=$1`, f.mailbox).Scan(&hold); err != nil || !hold {
				t.Fatal("confirmation freed admission before reconciliation", err)
			}
			// A replacement consumer replays the durable confirmation without a broker event.
			replacement := liveJobsService(h)
			replacement.reconcileRecordedSendResults(ctx)
			replacement.reconcileRecordedSendResults(ctx)
			var state string
			var applied bool
			var audits, attempts, counted int
			if err := h.Pool.QueryRow(ctx, `SELECT send_result_state,send_result_applied_at IS NOT NULL FROM tasks WHERE id=$1`, id).Scan(&state, &applied); err != nil {
				t.Fatal(err)
			}
			if err := h.Pool.QueryRow(ctx, `SELECT send_recovery_hold FROM email_accounts WHERE id=$1`, f.mailbox).Scan(&hold); err != nil {
				t.Fatal(err)
			}
			if err := h.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM send_recovery_resolutions WHERE recovery_task_id=$1 AND confirmed_result IS NOT NULL`, id).Scan(&audits); err != nil {
				t.Fatal(err)
			}
			if err := h.Pool.QueryRow(ctx, `SELECT send_attempts FROM campaign_contact_progress WHERE campaign_id=$1 AND contact_id=$2 AND sequence_id=$3`, f.campaign, f.contact, f.step).Scan(&attempts); err != nil {
				t.Fatal(err)
			}
			if err := h.Pool.QueryRow(ctx, `SELECT emails_sent FROM campaign_daily_sends WHERE campaign_id=$1 AND send_date=CURRENT_DATE`, f.campaign).Scan(&counted); err != nil {
				t.Fatal(err)
			}
			wantState, wantAttempts, wantCount := "failed", 1, 0
			if sent {
				wantState, wantAttempts, wantCount = "sent", 0, 1
			}
			if state != wantState || !applied || hold || audits != 1 || attempts != wantAttempts || counted != wantCount {
				t.Fatalf("resolution state=%s applied=%v hold=%v audits=%d attempts=%d sends=%d", state, applied, hold, audits, attempts, counted)
			}
			if sent {
				if pair := f.nextPair(t, replacement); pair != nil {
					t.Fatal("confirmed sent email became retryable")
				}
			} else if pair := f.nextPair(t, replacement); pair == nil {
				t.Fatal("confirmed unsent step remains parked")
			}
		})
	}
}

func TestLiveRecordedWorkerOutcomeSurvivesLostEvent(t *testing.T) {
	h := liveDB(t)
	f := newSendResultFixture(t, h)
	s := liveJobsService(h)
	id := f.stampSend(t, s)
	ctx := t.Context()
	result := models.SendEmailResult{TaskID: id, Success: true, MessageID: "durable@example.test"}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.Pool.Exec(ctx, `UPDATE tasks SET send_result_state='unknown',send_executor_result=$2 WHERE id=$1`, id, raw); err != nil {
		t.Fatal(err)
	}
	if err := s.HandleEmailFailed(ctx, models.SendEmailResult{TaskID: id}); err != nil {
		t.Fatal(err)
	}
	replacement := liveJobsService(h)
	replacement.reconcileRecordedSendResults(ctx)
	replacement.reconcileRecordedSendResults(ctx)
	var applied, hold bool
	var message string
	if err := h.Pool.QueryRow(ctx, `SELECT t.send_result_applied_at IS NOT NULL,ea.send_recovery_hold,t.message_id FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id WHERE t.id=$1`, id).Scan(&applied, &hold, &message); err != nil {
		t.Fatal(err)
	}
	if !applied || hold || message != result.MessageID {
		t.Fatal("durable outcome not recovered", applied, hold, message)
	}
}

func TestLiveUnreservedLegacyFailedTaskRetainsUnknownHold(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new ambiguous result", true: "existing unknown hold"}[existing], func(t *testing.T) {
			h := liveDB(t)
			f := newSendResultFixture(t, h)
			s := liveJobsService(h)
			ctx := t.Context()
			id := uuid.New()
			if _, err := h.Pool.Exec(ctx, `INSERT INTO tasks(id,email_account_id,task_type,status,message_id)VALUES($1,$2,'email','failed','')`, id, f.mailbox); err != nil {
				t.Fatal(err)
			}
			if existing {
				if _, err := h.Pool.Exec(ctx, `UPDATE tasks SET send_result_state='unknown' WHERE id=$1`, id); err != nil {
					t.Fatal(err)
				}
				if _, err := h.Pool.Exec(ctx, `UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='unknown',send_recovery_task_id=$2 WHERE id=$1`, f.mailbox, id); err != nil {
					t.Fatal(err)
				}
				s.reconcileRecordedSendResults(ctx)
			} else if err := s.HandleEmailFailed(ctx, models.SendEmailResult{TaskID: id, LegacyErrorMsg: "send authority unavailable"}); err != nil {
				t.Fatal(err)
			}
			var hold, applied bool
			var state string
			if err := h.Pool.QueryRow(ctx, `SELECT ea.send_recovery_hold,t.send_result_applied_at IS NOT NULL,t.send_result_state FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id WHERE t.id=$1`, id).Scan(&hold, &applied, &state); err != nil {
				t.Fatal(err)
			}
			if !hold || applied || state != "unknown" {
				t.Fatal("missing authority markers resolved a legacy unknown", hold, applied, state)
			}
		})
	}
}
