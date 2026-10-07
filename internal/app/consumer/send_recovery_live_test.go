package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestLiveSendCooldownSurvivesRepositoryReplacement(t *testing.T) {
	h := liveDB(t)
	ctx := t.Context()
	f := newSendResultFixture(t, h)
	s := liveJobsService(h)
	recovery := s.TaskRepo.(repository.SendResultRecovery)
	now := time.Now().UTC()
	retry := now.Add(time.Hour)
	id := f.stampSend(t, s)
	result := models.SendEmailResult{TaskID: id, Error: &models.EmailSendError{Code: "SENDING_TOO_FAST", Failure: &errx.SendFailure{Provider: "smtp", Protocol: "smtp", Status: 421, Disposition: errx.SendThrottle, Scope: "mailbox", ObservedAt: now, RetryAt: &retry}}}
	if err := s.HandleEmailFailed(ctx, result); err != nil {
		t.Fatal(err)
	}
	if err := s.HandleEmailFailed(ctx, result); err != nil {
		t.Fatal(err)
	}
	replacement := liveJobsService(liveDB(t))
	if err := replacement.HandleEmailFailed(ctx, result); err != nil {
		t.Fatal(err)
	}
	a, err := replacement.TaskRepo.(repository.SendResultRecovery).GetSendAdmission(ctx, f.org, f.mailbox, models.InboxProviderSMTPIMAP, now)
	if err != nil || a.Allowed || a.RecoveryHold || a.Scope != "mailbox" || a.RetryAt == nil || a.RetryAt.Sub(retry).Abs() > time.Microsecond {
		t.Fatalf("admission=%+v err=%v", a, err)
	}
	a, err = recovery.GetSendAdmission(ctx, f.org, f.mailbox, models.InboxProviderSMTPIMAP, retry.Add(time.Second))
	if err != nil || !a.Allowed {
		t.Fatalf("expired cooldown admission=%+v err=%v", a, err)
	}
	a, err = recovery.GetSendAdmission(ctx, f.org, f.mailbox, models.InboxProviderGoogle, retry.Add(time.Second))
	if err != nil || a.Allowed {
		t.Fatalf("wrong provider admission=%+v err=%v", a, err)
	}
	if _, err := recovery.GetSendAdmission(ctx, uuid.New(), f.mailbox, models.InboxProviderSMTPIMAP, now); err == nil {
		t.Fatal("cross-tenant admission exposed")
	}
	var attempts int
	if err := h.Pool.QueryRow(ctx, `SELECT send_attempts FROM campaign_contact_progress WHERE campaign_id=$1 AND contact_id=$2 AND sequence_id=$3`, f.campaign, f.contact, f.step).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("duplicate results charged %d attempts", attempts)
	}
	other := newSendResultFixture(t, h)
	a, err = recovery.GetSendAdmission(ctx, other.org, other.mailbox, models.InboxProviderSMTPIMAP, now)
	if err != nil || !a.Allowed || a.RetryAt != nil {
		t.Fatalf("mailbox-local failure blocked unrelated mailbox: %+v %v", a, err)
	}
	if _, err := h.Pool.Exec(ctx, `UPDATE email_accounts SET status='inactive' WHERE id=$1`, other.mailbox); err != nil {
		t.Fatal(err)
	}
	a, err = recovery.GetSendAdmission(ctx, other.org, other.mailbox, models.InboxProviderSMTPIMAP, retry.Add(time.Second))
	if err != nil || a.Allowed {
		t.Fatalf("inactive mailbox admitted: %+v %v", a, err)
	}
}

func TestLiveSendConcurrentResultsChargeOneAttempt(t *testing.T) {
	h := liveDB(t)
	f := newSendResultFixture(t, h)
	s := liveJobsService(h)
	id := f.stampSend(t, s)
	result := models.SendEmailResult{TaskID: id, Error: &models.EmailSendError{Code: "UNSUPPORTED"}}
	var group sync.WaitGroup
	errors := make(chan error, 12)
	for i := 0; i < 12; i++ {
		group.Add(1)
		go func() { defer group.Done(); errors <- s.HandleEmailFailed(t.Context(), result) }()
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	var attempts, sent int
	if err := h.Pool.QueryRow(t.Context(), `SELECT send_attempts FROM campaign_contact_progress WHERE campaign_id=$1 AND contact_id=$2 AND sequence_id=$3`, f.campaign, f.contact, f.step).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if err := h.Pool.QueryRow(t.Context(), `SELECT emails_sent FROM campaign_daily_sends WHERE campaign_id=$1 AND send_date=CURRENT_DATE`, f.campaign).Scan(&sent); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || sent != 0 {
		t.Fatalf("attempts=%d daily reservation=%d", attempts, sent)
	}
}

func TestLiveRecoveryUnknownAndConflictingResults(t *testing.T) {
	for _, first := range []string{"unknown", "sent", "failed"} {
		t.Run(first, func(t *testing.T) {
			h := liveDB(t)
			f := newSendResultFixture(t, h)
			s := liveJobsService(h)
			id := f.stampSend(t, s)
			unknown := models.SendEmailResult{TaskID: id, Error: &models.EmailSendError{Code: "SERVER_UNREACHABLE"}}
			sent := models.SendEmailResult{TaskID: id, Success: true, MessageID: "<reconciled@example.test>"}
			failed := models.SendEmailResult{TaskID: id, Error: &models.EmailSendError{Code: "RECIPIENT_REJECTED"}}
			if first == "unknown" {
				if err := s.HandleEmailFailed(t.Context(), unknown); err != nil {
					t.Fatal(err)
				}
				var applied *time.Time
				var state string
				if err := h.Pool.QueryRow(t.Context(), `SELECT send_result_state, send_result_applied_at FROM tasks WHERE id=$1`, id).Scan(&state, &applied); err != nil {
					t.Fatal(err)
				}
				if state != "unknown" || applied != nil {
					t.Fatal("legacy unknown was treated as terminal")
				}
				if err := liveJobsService(h).HandleEmailSent(t.Context(), sent); err != nil {
					t.Fatal(err)
				}
			} else if first == "sent" {
				if err := s.HandleEmailSent(t.Context(), sent); err != nil {
					t.Fatal(err)
				}
				if err := s.HandleEmailFailed(t.Context(), failed); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := s.HandleEmailFailed(t.Context(), failed); err != nil {
					t.Fatal(err)
				}
				if err := s.HandleEmailSent(t.Context(), sent); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.HandleEmailFailed(t.Context(), unknown); err != nil {
				t.Fatal(err)
			}
			var hold bool
			var reason *string
			var attempts int
			if err := h.Pool.QueryRow(t.Context(), `SELECT send_recovery_hold, send_recovery_reason FROM email_accounts WHERE id=$1`, f.mailbox).Scan(&hold, &reason); err != nil {
				t.Fatal(err)
			}
			if first == "unknown" && hold || first != "unknown" && (!hold || reason == nil || *reason != "conflict") {
				t.Fatalf("hold=%v reason=%v", hold, reason)
			}
			if err := h.Pool.QueryRow(t.Context(), `SELECT send_attempts FROM campaign_contact_progress WHERE campaign_id=$1 AND contact_id=$2 AND sequence_id=$3`, f.campaign, f.contact, f.step).Scan(&attempts); err != nil {
				t.Fatal(err)
			}
			want := 0
			if first == "failed" {
				want = 1
			}
			if attempts != want {
				t.Fatalf("attempts=%d want=%d", attempts, want)
			}
		})
	}
}

func TestLiveRecoveryMultipleUnknownsAndRestrictiveHolds(t *testing.T) {
	for _, knownHold := range []string{"", errx.SendPermanent, errx.SendAuth} {
		t.Run(knownHold, func(t *testing.T) {
			h := liveDB(t)
			ctx := t.Context()
			f := newSendResultFixture(t, h)
			r := repository.NewTaskRepository(h.Pool).(repository.SendResultRecovery)
			ids := []uuid.UUID{uuid.New(), uuid.New()}
			apply := func(context.Context) error { return nil }
			for _, id := range ids {
				if _, err := h.Pool.Exec(ctx, `INSERT INTO tasks (id, task_type, email_account_id, status, message_id) VALUES ($1, 'email', $2, 'completed', '')`, id, f.mailbox); err != nil {
					t.Fatal(err)
				}
				if err := r.ApplySendResult(ctx, models.SendEmailResult{TaskID: id}, apply); err != nil {
					t.Fatal(err)
				}
			}
			first := models.SendEmailResult{TaskID: ids[0], Success: true}
			if knownHold != "" {
				first.Success = false
				first.Error = &models.EmailSendError{Failure: &errx.SendFailure{Disposition: knownHold, Scope: "mailbox"}}
			}
			if err := r.ApplySendResult(ctx, first, apply); err != nil {
				t.Fatal(err)
			}
			a, err := r.GetSendAdmission(ctx, f.org, f.mailbox, models.InboxProviderSMTPIMAP, time.Now())
			if err != nil || !a.RecoveryHold {
				t.Fatalf("second unresolved task lost hold: %+v %v", a, err)
			}
			if err := r.ApplySendResult(ctx, models.SendEmailResult{TaskID: ids[1], Success: true}, apply); err != nil {
				t.Fatal(err)
			}
			a, err = repository.NewTaskRepository(h.Pool).(repository.SendResultRecovery).GetSendAdmission(ctx, f.org, f.mailbox, models.InboxProviderSMTPIMAP, time.Now())
			if err != nil || a.RecoveryHold != (knownHold != "") || a.HoldReason != knownHold {
				t.Fatalf("final hold=%+v err=%v", a, err)
			}
		})
	}
}

func TestLiveRecoveryRollsBackCallbackAndTerminalMarkerTogether(t *testing.T) {
	h := liveDB(t)
	ctx := t.Context()
	f := newSendResultFixture(t, h)
	r := repository.NewTaskRepository(h.Pool)
	recovery := r.(repository.SendResultRecovery)
	id := uuid.New()
	if _, err := h.Pool.Exec(ctx, `INSERT INTO tasks (id, task_type, email_account_id, status, message_id) VALUES ($1, 'email', $2, 'completed', '')`, id, f.mailbox); err != nil {
		t.Fatal(err)
	}
	result := models.SendEmailResult{TaskID: id, Success: true}
	err := recovery.ApplySendResult(ctx, result, func(inner context.Context) error {
		if err := r.UpdateTaskMessageID(inner, id, "<rollback@example.test>"); err != nil {
			return err
		}
		return errors.New("fixture callback unavailable")
	})
	if err == nil {
		t.Fatal("failed callback committed")
	}
	var state *string
	var applied *time.Time
	var message string
	if err := h.Pool.QueryRow(ctx, `SELECT send_result_state, send_result_applied_at, message_id FROM tasks WHERE id=$1`, id).Scan(&state, &applied, &message); err != nil {
		t.Fatal(err)
	}
	if state != nil || applied != nil || message != "" {
		t.Fatalf("partial commit: %v %v %q", state, applied, message)
	}
	calls := 0
	apply := func(inner context.Context) error {
		calls++
		return r.UpdateTaskMessageID(inner, id, "<committed@example.test>")
	}
	if err := recovery.ApplySendResult(ctx, result, apply); err != nil {
		t.Fatal(err)
	}
	if err := repository.NewTaskRepository(h.Pool).(repository.SendResultRecovery).ApplySendResult(ctx, result, apply); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("callback applied %d times", calls)
	}
}
