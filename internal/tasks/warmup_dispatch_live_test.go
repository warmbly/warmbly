package tasks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

func TestLiveWarmupDispatchDefersInvalidHoursBeforeClaimOrSend(t *testing.T) {
	f := newPartnerRoutingFixture(t)
	ctx := t.Context()
	task := uuid.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE email_accounts SET warmup=NOW(),warmup_start_time='08:00',warmup_end_time='07:00',warmup_pool_type='free' WHERE id=$1`, f.sender.ID)
	exec(`INSERT INTO tasks(id,email_account_id,task_type,status,message_id,scheduled_at)VALUES($1,$2,'warmup','pending','',NOW()-INTERVAL '1 hour')`, task, f.sender.ID)
	exec(`INSERT INTO warmup_tasks(task_id)VALUES($1)`, task)
	base := repository.NewTaskRepository(f.pool)
	observed := &windowCheckedTasks{TaskRepository: base, WarmupLineageRepository: base.(repository.WarmupLineageRepository), SendResultRecovery: base.(repository.SendResultRecovery)}
	f.svc.taskRepo = observed
	f.svc.scheduler = scheduler.NewSchedulerService(f.svc.taskRepo, f.svc.warmupRepo, nil, f.svc.emailRepo, nil, nil, nil)
	sender := &preparationDeferringSender{}
	f.svc.emailSender = sender
	if xerr := f.svc.HandleEmailTask(&proto.ProcessTask{TaskId: task.String()}); xerr != nil {
		t.Fatalf("invalid configured hours remained a dispatcher error: %v", xerr)
	}
	var status string
	var scheduled time.Time
	var unstarted bool
	if err := f.pool.QueryRow(ctx, `SELECT status,scheduled_at,send_reserved_at IS NULL AND send_executor_started_at IS NULL AND send_result_state IS NULL FROM tasks WHERE id=$1`, task).Scan(&status, &scheduled, &unstarted); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || !scheduled.After(time.Now().Add(4*time.Minute)) || !unstarted || sender.calls != 0 {
		t.Fatalf("status=%s scheduled=%v unstarted=%v calls=%d", status, scheduled, unstarted, sender.calls)
	}
	if observed.claimed {
		t.Fatal("invalid window reached the task claim")
	}
	var start, end string
	if err := f.pool.QueryRow(ctx, `SELECT warmup_start_time::text,warmup_end_time::text FROM email_accounts WHERE id=$1`, f.sender.ID).Scan(&start, &end); err != nil || start != "08:00:00" || end != "07:00:00" {
		t.Fatalf("configured hours silently changed: %s/%s err=%v", start, end, err)
	}
	// Real database failures must still fail dispatch rather than count as deferral.
	exec(`UPDATE tasks SET scheduled_at=NOW()-INTERVAL '1 hour' WHERE id=$1`, task)
	f.svc.scheduler = failedWindowScheduler{}
	if xerr := f.svc.HandleEmailTask(&proto.ProcessTask{TaskId: task.String()}); xerr == nil {
		t.Fatal("authority failure became a successful configuration deferral")
	}
	f.svc.scheduler = scheduler.NewSchedulerService(f.svc.taskRepo, f.svc.warmupRepo, nil, f.svc.emailRepo, nil, nil, nil)
	observed.deferralErr = context.DeadlineExceeded
	if xerr := f.svc.HandleEmailTask(&proto.ProcessTask{TaskId: task.String()}); xerr == nil {
		t.Fatal("failed reschedule was acknowledged as successful configuration deferral")
	}
	if observed.claimed || sender.calls != 0 {
		t.Fatal("failed authority or reschedule reached claim or provider execution")
	}
}

type windowCheckedTasks struct {
	repository.TaskRepository
	repository.WarmupLineageRepository
	repository.SendResultRecovery
	claimed     bool
	deferralErr error
}

func (r *windowCheckedTasks) ClaimWarmupTask(ctx context.Context, id uuid.UUID, at time.Time) (bool, error) {
	r.claimed = true
	return r.WarmupLineageRepository.ClaimWarmupTask(ctx, id, at)
}

func (r *windowCheckedTasks) RescheduleWarmupTask(ctx context.Context, id uuid.UUID, at time.Time) error {
	if r.deferralErr != nil {
		return r.deferralErr
	}
	return r.WarmupLineageRepository.RescheduleWarmupTask(ctx, id, at)
}

type failedWindowScheduler struct{ scheduler.SchedulerService }

func (failedWindowScheduler) WarmupDispatchNotBefore(context.Context, uuid.UUID, time.Time) (time.Time, error) {
	return time.Time{}, context.DeadlineExceeded
}

func TestLiveWarmupDispatchRechecksOpeningSourceAndExactRecipient(t *testing.T) {
	f := newPartnerRoutingFixture(t)
	ctx := t.Context()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE organizations SET risk_state='trusted' WHERE id=$1`, f.org)
	exec(`UPDATE email_accounts SET test_mode='diagnostic',test_send_enabled=true,test_receive_enabled=true,warmup=NOW()-INTERVAL '30 days',warmup_pool_type='free',warmup_days=127,warmup_start_time='00:00',warmup_end_time='23:59',warmup_base=20,warmup_max=20,name='Ádám Support <EMEA>' WHERE organization_id=$1`, f.org)
	task := uuid.New()
	source := VettedDiagnosticConversations()[0]
	exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id,scheduled_at)VALUES($1,'warmup',$2,'active','',NOW())`, task, f.sender.ID)
	exec(`INSERT INTO warmup_tasks(task_id,lineage_version,subject,scenario_version,rendering_version,max_turns)VALUES($1,1,$2,$3,$4,$5)`, task, source.Subject, source.Version, generation.CanonicalRenderingVersion, len(source.Messages)+1)
	exec(`INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,subject,content_source,conversation_id,conversation_turn)VALUES($1,$2,$3,$4,$5,'static',$6,0)`, uuid.New(), task, f.sender.ID, f.atWorkspace, source.Subject, source.ID)
	f.svc.taskRepo = repository.NewTaskRepository(f.pool)
	f.svc.scheduler = scheduler.NewSchedulerService(f.svc.taskRepo, f.svc.warmupRepo, nil, f.svc.emailRepo, nil, nil, nil)
	if err := f.svc.ValidateWarmupExecution(ctx, task); err != nil {
		t.Fatal("valid configured identity/opening rejected:", err)
	}
	exec(`UPDATE warmup_pool_participants SET health_state='blocked' WHERE email_account_id=$1`, f.atWorkspace)
	if err := f.svc.ValidateWarmupExecution(ctx, task); err == nil {
		t.Fatal("blocked recipient became diagnostic recipient")
	}
	exec(`UPDATE warmup_pool_participants SET participant_role='recipient_only',health_state='healthy' WHERE email_account_id=$1`, f.atWorkspace)
	if err := f.svc.ValidateWarmupExecution(ctx, task); err != nil {
		t.Fatal("recipient-only consent lost:", err)
	}
	exec(`INSERT INTO suppressed_recipients(organization_id,email,reason,source)SELECT $1,email,'unsubscribe','manual' FROM email_accounts WHERE id=$2`, f.org, f.atWorkspace)
	t.Cleanup(func() { _, _ = f.pool.Exec(ctx, `DELETE FROM suppressed_recipients WHERE organization_id=$1`, f.org) })
	if err := f.svc.ValidateWarmupExecution(ctx, task); err == nil {
		t.Fatal("suppressed exact recipient accepted")
	}
	exec(`DELETE FROM suppressed_recipients WHERE organization_id=$1`, f.org)
	exec(`UPDATE warmup_tokens SET conversation_id=$2 WHERE task_id=$1`, task, uuid.New())
	if err := f.svc.ValidateWarmupExecution(ctx, task); err == nil {
		t.Fatal("missing source continued")
	}
	exec(`UPDATE warmup_tokens SET conversation_id=$2 WHERE task_id=$1`, task, source.ID)
	exec(`UPDATE warmup_pool_participants SET participant_role='recipient_only' WHERE email_account_id=$1`, f.sender.ID)
	if err := f.svc.ValidateWarmupExecution(ctx, task); err == nil {
		t.Fatal("recipient-only mailbox promoted to sending")
	}
}
