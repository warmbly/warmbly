package tasks

import (
	"testing"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/scheduler"
)

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
