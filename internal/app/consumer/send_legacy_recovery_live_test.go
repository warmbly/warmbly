package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/iskorotkov/avro/v2"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func legacySendResultSchema(t *testing.T) (avro.Schema, avro.Schema) {
	t.Helper()
	reader := models.JobEvent{}.Schema()
	doc, err := models.SchemaDocument(reader)
	if err != nil {
		t.Fatal(err)
	}
	var tree any
	if err = json.Unmarshal(doc, &tree); err != nil {
		t.Fatal(err)
	}
	var remove func(any)
	remove = func(node any) {
		switch v := node.(type) {
		case []any:
			for _, item := range v {
				remove(item)
			}
		case map[string]any:
			if fields, ok := v["fields"].([]any); ok {
				var kept []any
				for _, raw := range fields {
					f := raw.(map[string]any)
					if f["name"] != "failure" {
						kept = append(kept, f)
						remove(f["type"])
					}
				}
				v["fields"] = kept
			}
			for key, value := range v {
				if key != "fields" {
					remove(value)
				}
			}
		}
	}
	remove(tree)
	doc, err = json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := avro.Parse(string(doc))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := avro.NewSchemaCompatibility().Resolve(reader, writer)
	if err != nil {
		t.Fatal(err)
	}
	return writer, resolved
}

func TestLiveLegacyFailedStatusCannotProveNonExecution(t *testing.T) {
	for _, status := range []string{"failed", "dead_lettered"} {
		t.Run(status, func(t *testing.T) {
			h := liveDB(t)
			f := newSendResultFixture(t, h)
			s := liveJobsService(h)
			id := f.stampSend(t, s)
			if _, err := h.Exec(t.Context(), `UPDATE tasks SET status=$2::task_status,send_result_state='unknown' WHERE id=$1`, id, status); err != nil {
				t.Fatal(err)
			}
			if _, err := h.Exec(t.Context(), `UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='unknown',send_recovery_task_id=$2 WHERE id=$1`, f.mailbox, id); err != nil {
				t.Fatal(err)
			}
			reader := s.TaskRepo.(interface {
				ListUnappliedSendResults(context.Context, int) ([]models.SendEmailResult, error)
			})
			results, err := reader.ListUnappliedSendResults(t.Context(), 200)
			if err != nil {
				t.Fatal(err)
			}
			for _, result := range results {
				if result.TaskID == id {
					t.Fatal("task status and missing modern markers manufactured provider failure evidence")
				}
			}
			ambiguous := models.SendEmailResult{TaskID: id, Error: &models.EmailSendError{Failure: &errx.SendFailure{Disposition: errx.SendAmbiguous}}}
			if err := s.HandleEmailFailed(t.Context(), ambiguous); err != nil {
				t.Fatal(err)
			}
			var state string
			var applied, released, hold bool
			if err := h.QueryRow(t.Context(), `SELECT t.send_result_state,t.send_result_applied_at IS NOT NULL,t.send_released_at IS NOT NULL,e.send_recovery_hold FROM tasks t JOIN email_accounts e ON e.id=t.email_account_id WHERE t.id=$1`, id).Scan(&state, &applied, &released, &hold); err != nil {
				t.Fatal(err)
			}
			if state != "unknown" || applied || released || !hold {
				t.Fatalf("legacy outcome guessed: state=%s applied=%t released=%t hold=%t", state, applied, released, hold)
			}
		})
	}
}

func TestLiveAuthoritativeDeadLetterResultsReconcileWithoutReplay(t *testing.T) {
	for _, outcome := range []string{"sent", "failed", "ambiguous", "previously applied sent", "previously applied failed", "unstamped failure", "legacy failure"} {
		t.Run(outcome, func(t *testing.T) {
			s, h := liveWarmupService(t)
			f := newWarmupFixture(t, h)
			token := f.mintToken(t, s.WarmupRepo)
			var day time.Time
			if err := h.QueryRow(t.Context(), `SELECT completed_at FROM tasks WHERE id=$1`, f.task).Scan(&day); err != nil {
				t.Fatal(err)
			}
			if err := s.WarmupRepo.IncrementDailyCount(t.Context(), f.sender, day); err != nil {
				t.Fatal(err)
			}
			if outcome == "failed" {
				if _, err := h.Exec(t.Context(), `INSERT INTO warmup_tasks(task_id,warmup_charged_date,warmup_reply_charged) VALUES($1,($2::timestamptz AT TIME ZONE 'UTC')::date,false)`, f.task, day); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := h.Exec(t.Context(), `UPDATE tasks SET status='dead_lettered',send_result_state='unknown' WHERE id=$1`, f.task); err != nil {
				t.Fatal(err)
			}
			if outcome != "legacy failure" {
				if _, err := h.Exec(t.Context(), `UPDATE tasks SET send_reserved_at=completed_at,send_executor_nonce=$2 WHERE id=$1`, f.task, uuid.New()); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := h.Exec(t.Context(), `INSERT INTO task_dead_letters(task_id,task_type,payload,last_error,attempts,next_retry_at) VALUES($1,'warmup','{"task_id":"preserved"}','legacy dispatch error',1,NOW())`, f.task); err != nil {
				t.Fatal(err)
			}
			if _, err := h.Exec(t.Context(), `UPDATE email_accounts SET send_recovery_hold=true,send_recovery_reason='unknown',send_recovery_task_id=$2 WHERE id=$1`, f.sender, f.task); err != nil {
				t.Fatal(err)
			}
			if outcome == "unstamped failure" {
				if _, err := h.Exec(t.Context(), `UPDATE tasks SET completed_at=NULL WHERE id=$1`, f.task); err != nil {
					t.Fatal(err)
				}
			}
			result := models.SendEmailResult{TaskID: f.task, Success: outcome == "sent" || outcome == "previously applied sent", MessageID: f.sentMsgID, SentAt: day}
			if !result.Success {
				disposition := errx.SendRetry
				if outcome == "ambiguous" {
					disposition = errx.SendAmbiguous
				}
				result.MessageID = ""
				result.Error = &models.EmailSendError{Code: "SERVER_UNREACHABLE", Failure: &errx.SendFailure{Disposition: disposition, Stage: "dial", Protocol: "smtp", Scope: "mailbox"}}
			}
			if outcome == "previously applied sent" || outcome == "previously applied failed" {
				raw, err := json.Marshal(result)
				if err != nil {
					t.Fatal(err)
				}
				state := "failed"
				if result.Success {
					state = "sent"
				}
				if _, err := h.Exec(t.Context(), `UPDATE tasks SET send_result_state=$2,send_result_applied_at=NOW(),send_executor_result=$3 WHERE id=$1`, f.task, state, raw); err != nil {
					t.Fatal(err)
				}
				s.reconcileRecordedSendResults(t.Context())
			}
			for range 2 {
				var err error
				if result.Success {
					err = s.HandleEmailSent(t.Context(), result)
				} else {
					err = s.HandleEmailFailed(t.Context(), result)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			var status, state, dlq, message, lastError string
			var applied, released, hold, replayed, evidenceKept bool
			var count, attempts int
			if err := h.QueryRow(t.Context(), `SELECT t.status,t.send_result_state,t.send_result_applied_at IS NOT NULL,t.send_released_at IS NOT NULL,e.send_recovery_hold,d.status,d.replayed_at IS NOT NULL,d.payload='{"task_id":"preserved"}'::jsonb,d.last_error,d.attempts,wt.sent_message_id,ws.emails_sent FROM tasks t JOIN email_accounts e ON e.id=t.email_account_id JOIN task_dead_letters d ON d.task_id=t.id JOIN warmup_tokens wt ON wt.task_id=t.id JOIN warmup_statistics ws ON ws.email_account_id=e.id AND ws.date=DATE($3::timestamptz) WHERE t.id=$1 AND wt.token=$2`, f.task, token, day).Scan(&status, &state, &applied, &released, &hold, &dlq, &replayed, &evidenceKept, &lastError, &attempts, &message, &count); err != nil {
				t.Fatal(err)
			}
			if replayed || !evidenceKept || lastError != "legacy dispatch error" || attempts != 1 {
				t.Fatal("recovery replayed or erased original dead-letter evidence")
			}
			switch outcome {
			case "sent", "previously applied sent":
				if status != "completed" || state != "sent" || !applied || released || hold || dlq != "resolved" || message != f.sentMsgID || count != 1 {
					t.Fatalf("sent recovery incomplete: status=%s state=%s applied=%t released=%t hold=%t dlq=%s count=%d", status, state, applied, released, hold, dlq, count)
				}
			case "failed", "unstamped failure", "legacy failure":
				wantCount := 0
				if outcome == "unstamped failure" || outcome == "legacy failure" {
					wantCount = 1
				}
				if status != "failed" || state != "failed" || !applied || !released || hold || dlq != "resolved" || message != "" || count != wantCount {
					t.Fatalf("failure recovery incomplete: status=%s state=%s applied=%t released=%t hold=%t dlq=%s count=%d", status, state, applied, released, hold, dlq, count)
				}
			case "previously applied failed":
				if status != "dead_lettered" || state != "failed" || !applied || released || !hold || dlq != "pending" || message != "" || count != 1 {
					t.Fatal("already-applied failure refunded without accounting provenance")
				}
			default:
				if status != "dead_lettered" || state != "unknown" || applied || released || !hold || dlq != "pending" || message != "" || count != 1 {
					t.Fatal("ambiguous dead letter was guessed resolved")
				}
			}
		})
	}
}

func TestLiveDeadLetterResultEffectsAndSettlementRollBackTogether(t *testing.T) {
	s, h := liveWarmupService(t)
	f := newWarmupFixture(t, h)
	f.mintToken(t, s.WarmupRepo)
	if _, err := h.Exec(t.Context(), `UPDATE tasks SET status='dead_lettered',send_result_state='unknown' WHERE id=$1`, f.task); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Exec(t.Context(), `INSERT INTO task_dead_letters(task_id,task_type) VALUES($1,'warmup')`, f.task); err != nil {
		t.Fatal(err)
	}
	want := errors.New("effect failed after task transition")
	result := models.SendEmailResult{TaskID: f.task, Success: true, MessageID: f.sentMsgID}
	recovery := s.TaskRepo.(repository.SendResultRecovery)
	err := recovery.ApplySendResult(t.Context(), result, func(ctx context.Context) error {
		if err := s.applyEmailSent(ctx, result); err != nil {
			return err
		}
		return want
	})
	if !errors.Is(err, want) {
		t.Fatal(err)
	}
	var status, state, dlq, message string
	var applied bool
	if err := h.QueryRow(t.Context(), `SELECT t.status,t.send_result_state,t.send_result_applied_at IS NOT NULL,d.status,wt.sent_message_id FROM tasks t JOIN task_dead_letters d ON d.task_id=t.id JOIN warmup_tokens wt ON wt.task_id=t.id WHERE t.id=$1`, f.task).Scan(&status, &state, &applied, &dlq, &message); err != nil {
		t.Fatal(err)
	}
	if status != "dead_lettered" || state != "unknown" || applied || dlq != "pending" || message != "" {
		t.Fatal("partial result effects escaped rollback")
	}
}

func TestLiveCampaignDeadLetterWorkerResultReleasesReservationOnce(t *testing.T) {
	h := liveDB(t)
	f := newSendResultFixture(t, h)
	s := liveJobsService(h)
	id := f.stampSend(t, s)
	if _, err := h.Exec(t.Context(), `UPDATE tasks SET status='dead_lettered',send_result_state='unknown' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Exec(t.Context(), `INSERT INTO task_dead_letters(task_id,task_type) VALUES($1,'campaign')`, id); err != nil {
		t.Fatal(err)
	}
	result := models.SendEmailResult{TaskID: id, SentAt: time.Now().UTC(), Error: &models.EmailSendError{Code: "SERVER_UNREACHABLE", Failure: &errx.SendFailure{Protocol: "smtp", Stage: "dial", Disposition: errx.SendRetry, Scope: "mailbox"}}}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Exec(t.Context(), `UPDATE tasks SET send_executor_result=$2 WHERE id=$1`, id, raw); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		s.reconcileRecordedSendResults(t.Context())
		if err := s.HandleEmailFailed(t.Context(), result); err != nil {
			t.Fatal(err)
		}
	}
	var status, dlq string
	var sent, reserved, applied bool
	var attempts int
	if err := h.QueryRow(t.Context(), `SELECT t.status,d.status,p.sent_at IS NOT NULL,p.dispatched_at IS NOT NULL,t.send_result_applied_at IS NOT NULL,p.send_attempts FROM tasks t JOIN task_dead_letters d ON d.task_id=t.id JOIN campaign_contact_progress p ON p.campaign_id=$2 AND p.contact_id=$3 AND p.sequence_id=$4 WHERE t.id=$1`, id, f.campaign, f.contact, f.step).Scan(&status, &dlq, &sent, &reserved, &applied, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || dlq != "resolved" || sent || reserved || !applied || attempts != 1 {
		t.Fatalf("definitive campaign refusal not reconciled once: status=%s dlq=%s sent=%t reserved=%t applied=%t attempts=%d", status, dlq, sent, reserved, applied, attempts)
	}
}

func TestLiveUserEmailDeadLetterUsesUnderlyingSendTaskType(t *testing.T) {
	h := liveDB(t)
	f := newSendResultFixture(t, h)
	s := liveJobsService(h)
	id := uuid.New()
	if _, err := h.Exec(t.Context(), `INSERT INTO tasks(id,email_account_id,task_type,status,message_id) VALUES($1,$2,'email','dead_lettered','')`, id, f.mailbox); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Exec(t.Context(), `INSERT INTO task_dead_letters(task_id,task_type) VALUES($1,'user_email')`, id); err != nil {
		t.Fatal(err)
	}
	if err := s.HandleEmailSent(t.Context(), models.SendEmailResult{TaskID: id, MessageID: "confirmed@example.test"}); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := h.QueryRow(t.Context(), `SELECT status FROM task_dead_letters WHERE task_id=$1`, id).Scan(&status); err != nil || status != "resolved" {
		t.Fatal("user_email dead letter was not settled", status, err)
	}
}

func TestLiveCampaignForeignKeyCleanupDoesNotProveWakeupOnly(t *testing.T) {
	h := liveDB(t)
	f := newSendResultFixture(t, h)
	s := liveJobsService(h)
	id := f.stampSend(t, s)
	before, err := s.TaskRepo.GetCampaignTask(t.Context(), id)
	if err != nil || before == nil || before.ContactID == nil || before.SequenceID == nil {
		t.Fatal("send-capable campaign fixture missing", err)
	}
	if _, err := h.Exec(t.Context(), `DELETE FROM contacts WHERE id=$1`, f.contact); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Exec(t.Context(), `DELETE FROM sequences WHERE id=$1`, *before.SequenceID); err != nil {
		t.Fatal(err)
	}
	after, err := s.TaskRepo.GetCampaignTask(t.Context(), id)
	if err != nil || after == nil || after.ContactID != nil || after.SequenceID != nil || after.CampaignID == nil {
		t.Fatal("FK-null former send no longer matches the unsafe wakeup-only predicate", err)
	}
}

func TestLiveLegacyJSONAndAvroFailurePersistConservativeAdmission(t *testing.T) {
	writer, reader := legacySendResultSchema(t)
	for _, format := range []string{"json", "avro"} {
		for _, code := range []string{"SENDING_TOO_FAST", "QUOTA_EXCEEDED", "RECIPIENT_REJECTED", "UNKNOWN"} {
			t.Run(format+"/"+code, func(t *testing.T) {
				h := liveDB(t)
				f := newSendResultFixture(t, h)
				svc := liveJobsService(h)
				id := f.stampSend(t, svc)
				now := time.Now().UTC()
				original := models.SendEmailResult{TaskID: id, SentAt: now, Error: &models.EmailSendError{Code: code}}
				var result models.SendEmailResult
				if format == "json" {
					data, err := json.Marshal(original)
					if err != nil {
						t.Fatal(err)
					}
					if err = json.Unmarshal(data, &result); err != nil {
						t.Fatal(err)
					}
				} else {
					data, err := models.EventAvro.Marshal(writer, models.JobEvent{Type: models.JobEventTypeEmailFailed, Body: original})
					if err != nil {
						t.Fatal(err)
					}
					var event models.JobEvent
					if err = models.EventAvro.Unmarshal(reader, data, &event); err != nil {
						t.Fatal(err)
					}
					var ok bool
					result, ok = event.Body.(models.SendEmailResult)
					if !ok {
						t.Fatalf("decoded body %T", event.Body)
					}
				}
				if result.Error == nil || result.Error.Failure != nil {
					t.Fatal("legacy transport invented native evidence")
				}
				for range 2 {
					if err := svc.HandleEmailFailed(t.Context(), result); err != nil {
						t.Fatal(err)
					}
				}
				replaced := liveJobsService(h)
				admission, err := replaced.TaskRepo.(repository.SendResultRecovery).GetSendAdmission(t.Context(), f.org, f.mailbox, models.InboxProviderSMTPIMAP, now)
				if err != nil {
					t.Fatal(err)
				}
				switch code {
				case "SENDING_TOO_FAST", "QUOTA_EXCEEDED":
					if admission.Allowed || admission.RecoveryHold || admission.Scope != "mailbox" || admission.RetryAt == nil || admission.RetryAt.Sub(now).Abs() < 4*time.Minute {
						t.Fatalf("legacy throttle admission %+v", admission)
					}
					after, err := replaced.TaskRepo.(repository.SendResultRecovery).GetSendAdmission(t.Context(), f.org, f.mailbox, models.InboxProviderSMTPIMAP, now.Add(6*time.Minute))
					if err != nil || !after.Allowed {
						t.Fatal("transient throttle became auth hold")
					}
				case "RECIPIENT_REJECTED":
					if !admission.Allowed || admission.RetryAt != nil {
						t.Fatal("recipient refusal became mailbox hold")
					}
				default:
					if admission.Allowed || !admission.RecoveryHold {
						t.Fatal("unknown old result became success")
					}
				}
				var status string
				if err = h.Pool.QueryRow(t.Context(), `SELECT status FROM email_accounts WHERE id=$1`, f.mailbox).Scan(&status); err != nil || status != "active" {
					t.Fatal("legacy failure disabled mailbox")
				}
			})
		}
	}
}
