package tasks

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

// admittingSender reserves the send through the real outbound admission, the
// gate every worker SEND_EMAIL passes, and counts the sends it admitted.
type admittingSender struct {
	repo   repository.OutboundAdmissionRepository
	worker uuid.UUID
	before func(taskID uuid.UUID)

	mu       sync.Mutex
	admitted int
}

func (a *admittingSender) Send(ctx context.Context, taskID uuid.UUID, msg EmailMessage, account models.Email) error {
	if a.before != nil {
		a.before(taskID)
	}
	recipients := append(append(append([]string{}, msg.To...), msg.CC...), msg.BCC...)
	if _, err := a.repo.ReserveOutbound(ctx, repository.OutboundReservation{
		TaskID: taskID, MailboxID: account.ID, OrganizationID: *account.OrganizationID, WorkerID: a.worker,
		Provider: models.InboxProvider(account.Provider), Recipients: recipients,
	}); err != nil {
		return err
	}
	a.mu.Lock()
	a.admitted++
	a.mu.Unlock()
	return nil
}

func (a *admittingSender) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.admitted
}

// hookGate runs a callback inside the unibox entitlement check, which the
// executor asks after it has read the task as pending and before it claims it.
type hookGate struct {
	planGate
	onCheck func()
}

func (g hookGate) CanUseUnibox(ctx context.Context, org uuid.UUID) (bool, *errx.Error) {
	if g.onCheck != nil {
		g.onCheck()
	}
	return g.planGate.CanUseUnibox(ctx, org)
}

type scheduledCancelFixture struct {
	*campaignSendFixture
	worker uuid.UUID
	repo   repository.TaskRepository
}

func newScheduledCancelFixture(t *testing.T) (*scheduledCancelFixture, func(EmailSender) *tasksService) {
	t.Helper()
	handle := liveCampaignDB(t)
	f := &scheduledCancelFixture{campaignSendFixture: newCampaignSendFixture(t, handle.Pool), worker: uuid.New(), repo: repository.NewTaskRepository(handle.Pool)}
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol) VALUES($1,'worker',true,NOW(),2)`,
		`INSERT INTO workers(id) VALUES($1)`,
	} {
		if _, err := handle.Pool.Exec(ctx, q, f.worker); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := handle.Pool.Exec(ctx, `UPDATE email_accounts SET worker_id=$1 WHERE id=$2`, f.worker, f.mailbox); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = handle.Pool.Exec(c, `UPDATE email_accounts SET worker_id=NULL WHERE id=$1`, f.mailbox)
		_, _ = handle.Pool.Exec(c, `DELETE FROM fleet_nodes WHERE id=$1`, f.worker)
	})
	return f, func(sender EmailSender) *tasksService {
		svc := liveCampaignService(t, handle, sender)
		svc.featureGate = planGate{daily: -1}
		return svc
	}
}

func (f *scheduledCancelFixture) status(t *testing.T, taskID uuid.UUID) string {
	t.Helper()
	var status string
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM tasks WHERE id=$1`, taskID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func (f *scheduledCancelFixture) reservation(taskID uuid.UUID) repository.OutboundReservation {
	return repository.OutboundReservation{
		TaskID: taskID, MailboxID: f.mailbox, OrganizationID: f.org, WorkerID: f.worker,
		Provider: models.InboxProviderSMTPIMAP, Recipients: []string{"them@test.local"},
	}
}

// A cancel that commits after the executor read the task as pending, but
// before it claimed it, wins: nothing is admitted, then or on a redelivery.
func TestLiveCancelledScheduledEmailIsNeverAdmitted(t *testing.T) {
	f, service := newScheduledCancelFixture(t)
	sender := &admittingSender{repo: f.repo.(repository.OutboundAdmissionRepository), worker: f.worker}
	svc := service(sender)
	taskID := queueUniboxReply(t, f.pool, f.mailbox, "", "")

	var cancelled bool
	svc.featureGate = hookGate{planGate: planGate{daily: -1}, onCheck: func() {
		_, outcome, err := f.repo.CancelScheduledInOrg(context.Background(), taskID, f.org, nil)
		if err != nil {
			t.Error(err)
		}
		cancelled = outcome == repository.ScheduledCancelled
	}}
	if xerr := svc.HandleUserEmailTask(&proto.ProcessTask{TaskId: taskID.String()}); xerr != nil {
		t.Fatalf("handle: %v", xerr)
	}
	if !cancelled {
		t.Fatal("the cancel of a pending task did not succeed")
	}
	if n := sender.count(); n != 0 {
		t.Fatalf("a cancelled scheduled email was admitted %d time(s)", n)
	}
	if got := f.status(t, taskID); got != "cancelled" {
		t.Fatalf("status = %q, want cancelled", got)
	}

	// A redelivery of the same queue message admits nothing either.
	svc.featureGate = planGate{daily: -1}
	if xerr := svc.HandleUserEmailTask(&proto.ProcessTask{TaskId: taskID.String()}); xerr != nil {
		t.Fatalf("redelivery: %v", xerr)
	}
	if xerr := svc.HandleTask(&proto.ProcessTask{TaskId: taskID.String()}); xerr != nil {
		t.Fatalf("dispatch redelivery: %v", xerr)
	}
	if n := sender.count(); n != 0 {
		t.Fatalf("a redelivered cancelled scheduled email was admitted %d time(s)", n)
	}
	if got := f.status(t, taskID); got != "cancelled" {
		t.Fatalf("status after redelivery = %q, want cancelled", got)
	}
	if _, err := sender.repo.ReserveOutbound(context.Background(), f.reservation(taskID)); !errors.Is(err, repository.ErrSendAdmissionDenied) {
		t.Fatalf("outbound admission of a cancelled task = %v, want ErrSendAdmissionDenied", err)
	}
}

// Once the executor has claimed the task, a cancel reports it is no longer
// pending and the send goes ahead exactly once.
func TestLiveClaimedScheduledEmailRefusesCancel(t *testing.T) {
	f, service := newScheduledCancelFixture(t)
	taskID := queueUniboxReply(t, f.pool, f.mailbox, "", "")
	var outcome repository.ScheduledCancelOutcome
	sender := &admittingSender{repo: f.repo.(repository.OutboundAdmissionRepository), worker: f.worker, before: func(id uuid.UUID) {
		var err error
		if _, outcome, err = f.repo.CancelScheduledInOrg(context.Background(), id, f.org, nil); err != nil {
			t.Error(err)
		}
	}}
	svc := service(sender)
	if xerr := svc.HandleUserEmailTask(&proto.ProcessTask{TaskId: taskID.String()}); xerr != nil {
		t.Fatalf("handle: %v", xerr)
	}
	if outcome != repository.ScheduledCancelNotPending {
		t.Fatalf("cancel after the claim = %v, want not pending", outcome)
	}
	if n := sender.count(); n != 1 {
		t.Fatalf("admitted %d sends, want 1", n)
	}
	if got := f.status(t, taskID); got != "completed" {
		t.Fatalf("status = %q, want completed", got)
	}
	if _, outcome, err := f.repo.CancelScheduledInOrg(context.Background(), taskID, f.org, nil); err != nil || outcome != repository.ScheduledCancelNotPending {
		t.Fatalf("cancel of a sent task = %v, %v; want not pending", outcome, err)
	}
	if _, outcome, err := f.repo.CancelScheduledInOrg(context.Background(), uuid.New(), f.org, nil); err != nil || outcome != repository.ScheduledCancelNotFound {
		t.Fatalf("cancel of an unknown task = %v, %v; want not found", outcome, err)
	}
	if _, outcome, err := f.repo.CancelScheduledInOrg(context.Background(), taskID, uuid.New(), nil); err != nil || outcome != repository.ScheduledCancelNotFound {
		t.Fatalf("cancel from another workspace = %v, %v; want not found", outcome, err)
	}
}

// Racing a cancel against execution, exactly one of them wins every time.
func TestLiveScheduledEmailCancelAndExecutionNeverBothWin(t *testing.T) {
	f, service := newScheduledCancelFixture(t)
	sender := &admittingSender{repo: f.repo.(repository.OutboundAdmissionRepository), worker: f.worker}
	svc := service(sender)
	for i := range 25 {
		taskID := queueUniboxReply(t, f.pool, f.mailbox, "", "")
		before := sender.count()
		var wg sync.WaitGroup
		var outcome repository.ScheduledCancelOutcome
		wg.Add(2)
		go func() {
			defer wg.Done()
			if xerr := svc.HandleUserEmailTask(&proto.ProcessTask{TaskId: taskID.String()}); xerr != nil {
				t.Error(xerr)
			}
		}()
		go func() {
			defer wg.Done()
			var err error
			if _, outcome, err = f.repo.CancelScheduledInOrg(context.Background(), taskID, f.org, nil); err != nil {
				t.Error(err)
			}
		}()
		wg.Wait()
		sent := sender.count() - before
		switch {
		case outcome == repository.ScheduledCancelled && sent != 0:
			t.Fatalf("round %d: the cancel succeeded and the send was admitted", i)
		case outcome == repository.ScheduledCancelNotPending && sent != 1:
			t.Fatalf("round %d: the cancel lost but %d sends were admitted", i, sent)
		case outcome == repository.ScheduledCancelNotFound:
			t.Fatalf("round %d: the task was not found", i)
		}
	}
}
