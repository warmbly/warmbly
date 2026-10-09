package tasks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

type deferredWarmupAdmission struct {
	repository.OutboundAdmissionRepository
	repository.WarmupDispatchRepository
	err      error
	deferred bool
}

func (a *deferredWarmupAdmission) ReserveOutbound(context.Context, repository.OutboundReservation) (uuid.UUID, error) {
	return uuid.Nil, repository.ErrSendAdmissionDenied
}

func (a *deferredWarmupAdmission) DeferWarmupDispatch(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) error {
	a.deferred = true
	return a.err
}

func TestWarmupSenderDistinguishesRequeueFromUnknownDeferral(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"confirmed deferral", nil, ErrWarmupDispatchDeferred},
		{"uncertain transaction", errors.New("commit response lost"), ErrSendDispatchUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			admission := &deferredWarmupAdmission{err: tc.err}
			worker, org := uuid.New(), uuid.New()
			sender := &emailSender{admission: admission}
			err := sender.Send(t.Context(), uuid.New(), EmailMessage{
				IsWarmup: true, DispatchNonce: uuid.NewString(), To: []string{"partner@example.test"},
			}, models.Email{ID: uuid.New(), Email: "sender@example.test", WorkerID: &worker, OrganizationID: &org})
			if !admission.deferred || !errors.Is(err, tc.want) {
				t.Fatalf("deferred=%v error=%v want=%v", admission.deferred, err, tc.want)
			}
		})
	}
}

type preparationDeferringSender struct {
	EmailSender
	repo  repository.WarmupDispatchRepository
	calls int
}

func (*preparationDeferringSender) WarmupWorkerReady(context.Context, models.Email) (bool, error) {
	return true, nil
}

func (s *preparationDeferringSender) Send(ctx context.Context, task uuid.UUID, msg EmailMessage, account models.Email) error {
	nonce, err := uuid.Parse(msg.DispatchNonce)
	if err != nil {
		return err
	}
	if err := s.repo.DeferWarmupDispatch(ctx, task, account.ID, *account.WorkerID, nonce, time.Now().Add(5*time.Minute)); err != nil {
		return err
	}
	s.calls++
	return ErrWarmupDispatchDeferred
}

type preparationScheduler struct{ scheduler.SchedulerService }

func (preparationScheduler) WarmupDispatchNotBefore(_ context.Context, _ uuid.UUID, now time.Time) (time.Time, error) {
	return now, nil
}

func TestLiveWarmupTaskDoesNotApplyResultsAfterConfirmedDeferral(t *testing.T) {
	f := newPartnerRoutingFixture(t)
	ctx := t.Context()
	worker, task := uuid.New(), uuid.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol) VALUES($1,'worker',true,NOW(),2)`, worker)
	exec(`INSERT INTO workers(id) VALUES($1)`, worker)
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, worker) })
	exec(`UPDATE email_accounts SET worker_id=$1,warmup=NOW()-INTERVAL '30 days',warmup_days=127,warmup_pool_type='free' WHERE organization_id=$2`, worker, f.org)
	exec(`INSERT INTO tasks(id,email_account_id,task_type,status,message_id,scheduled_at) VALUES($1,$2,'warmup','pending','',NOW()-INTERVAL '1 minute')`, task, f.sender.ID)
	exec(`INSERT INTO warmup_tasks(task_id) VALUES($1)`, task)
	f.svc.taskRepo = repository.NewTaskRepository(f.pool)
	f.svc.scheduler = preparationScheduler{}
	sender := &preparationDeferringSender{repo: f.svc.taskRepo.(repository.WarmupDispatchRepository)}
	f.svc.emailSender = sender
	for i := 1; i <= 2; i++ {
		if xerr := f.svc.HandleEmailTask(&proto.ProcessTask{TaskId: task.String()}); xerr != nil {
			t.Fatal(xerr)
		}
		var status string
		var clean, held bool
		var reserved int
		if err := f.pool.QueryRow(ctx, `SELECT t.status,
			t.send_result_state IS NULL AND t.send_result_applied_at IS NULL AND t.send_released_at IS NULL AND t.send_result_evidence IS NULL,
			ea.send_recovery_hold,ws.emails_sent FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id
			JOIN warmup_statistics ws ON ws.email_account_id=ea.id AND ws.date=CURRENT_DATE WHERE t.id=$1`, task).Scan(&status, &clean, &held, &reserved); err != nil {
			t.Fatal(err)
		}
		if sender.calls != i || status != "pending" || !clean || held || reserved != 0 {
			t.Fatalf("attempt=%d calls=%d status=%s clean=%v held=%v reserved=%d", i, sender.calls, status, clean, held, reserved)
		}
		exec(`UPDATE tasks SET scheduled_at=NOW()-INTERVAL '1 minute' WHERE id=$1`, task)
	}
}
