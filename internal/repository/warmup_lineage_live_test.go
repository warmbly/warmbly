package repository

import (
	"context"
	"errors"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/warmbly/warmbly/internal/infrastructure/db"
	"github.com/warmbly/warmbly/internal/models"
)

func lineageFixture(t *testing.T) (*poolLinkFixture, *taskRepository) {
	t.Helper()
	dsn := os.Getenv("WARMBLY_TEST_DB")
	if dsn == "" {
		t.Skip("WARMBLY_TEST_DB not set")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		t.Fatal("lineage fixture requires local PostgreSQL")
	}
	if err := db.RunMigrations(dsn); err != nil {
		t.Fatal(err)
	}
	f := newPoolLinkFixture(t)
	_, err = f.pool.Exec(t.Context(), `UPDATE organizations SET risk_state='trusted' WHERE id=$1`, f.org)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.pool.Exec(t.Context(), `UPDATE email_accounts SET warmup=NOW(),warmup_days=127 WHERE organization_id=$1`, f.org)
	if err != nil {
		t.Fatal(err)
	}
	return f, NewTaskRepository(f.pool).(*taskRepository)
}

func TestLiveLineageBindsOnlyExactVerifiedParentAndOneConcurrentSuccessor(t *testing.T) {
	f, r := lineageFixture(t)
	ctx := t.Context()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO warmup_tasks(task_id,lineage_version,subject,scenario_version,rendering_version,max_turns,reference_ids)VALUES($1,1,'Pinned diagnostic','diagnostic-v1','canonical-v1',3,'{root@example.test}')`, f.task)
	exec(`INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,subject,content_source,conversation_id,conversation_turn)VALUES($1,$2,$3,$4,'Pinned diagnostic','static',$5,0)`, uuid.New(), f.task, f.sender, f.recipient, uuid.New())
	receipt := uuid.New()
	exec(`INSERT INTO warmup_received(email_account_id,internal_id,sender_account_id,message_id)VALUES($1,$2,$3,'parent@example.test')`, f.recipient, receipt, f.sender)
	if err := r.RecordVerifiedWarmupParent(ctx, f.task, f.recipient, receipt, "receiving-mailbox-thread", WarmupReceiptProof{SenderAddress: f.senderTo}); err == nil {
		t.Fatal("unstamped header claim became send authority")
	}
	exec(`UPDATE warmup_tokens SET sent_message_id='<parent@example.test>' WHERE task_id=$1`, f.task)
	if err := r.RecordVerifiedWarmupParent(ctx, f.task, f.recipient, receipt, "receiving-mailbox-thread", WarmupReceiptProof{SenderAddress: f.senderTo}); err != nil {
		t.Fatal(err)
	}
	if err := r.RecordVerifiedWarmupParent(ctx, f.task, f.recipient, receipt, "receiving-mailbox-thread", WarmupReceiptProof{SenderAddress: f.senderTo}); err != nil {
		t.Fatal(err)
	}
	var won atomic.Int32
	var wg sync.WaitGroup
	failures := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := r.BindWarmupSuccessor(ctx, f.task, f.recipient, receipt, time.Now())
			if ok {
				won.Add(1)
			}
			if err != nil {
				failures <- err
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	if won.Load() != 1 {
		t.Fatalf("successors=%d", won.Load())
	}
	var successor uuid.UUID
	var at, received time.Time
	if err := f.pool.QueryRow(ctx, `SELECT w.task_id,t.scheduled_at,wr.created_at FROM warmup_tasks w JOIN tasks t ON t.id=w.task_id JOIN warmup_received wr ON wr.internal_id=w.parent_received_id AND wr.email_account_id=t.email_account_id WHERE w.parent_task_id=$1`, f.task).Scan(&successor, &at, &received); err != nil {
		t.Fatal(err)
	}
	if at.Before(received.Add(45 * time.Minute)) {
		t.Fatal("successor escaped minimum reply delay")
	}
	parent, err := r.ExactWarmupParent(ctx, successor)
	if err != nil || parent == nil || parent.MessageID != "parent@example.test" || parent.Subject != "Pinned diagnostic" || parent.ThreadID == nil || *parent.ThreadID != "receiving-mailbox-thread" || len(parent.References) != 1 {
		t.Fatalf("exact parent=%+v %v", parent, err)
	}
	exec(`INSERT INTO warmup_received(email_account_id,internal_id,sender_account_id,message_id,task_id,provider_thread_id)VALUES($1,$2,$3,'unrelated@example.test',$4,'foreign-newer-thread')`, f.recipient, uuid.New(), f.sender, f.task)
	parent, err = r.ExactWarmupParent(ctx, successor)
	if err != nil || parent == nil || parent.MessageID != "parent@example.test" {
		t.Fatal("newest pair replaced exact parent")
	}
	for _, query := range []string{
		`UPDATE warmup_tokens SET expires_at=NOW()-INTERVAL '1 second' WHERE task_id=$1`,
		`UPDATE warmup_tokens SET expires_at=NOW()+INTERVAL '1 day',sent_retired_at=NOW() WHERE task_id=$1`,
		`UPDATE warmup_tasks SET max_turns=1 WHERE task_id=$1`,
		`UPDATE warmup_tasks SET lineage_version=NULL WHERE task_id=$1`,
	} {
		exec(query, f.task)
		if got, err := r.ExactWarmupParent(ctx, successor); err != nil || got != nil {
			t.Fatalf("unsafe parent continued: %+v %v", got, err)
		}
	}
	if err := r.RecordVerifiedWarmupParent(ctx, uuid.New(), f.recipient, receipt, "foreign", WarmupReceiptProof{SenderAddress: f.senderTo}); err == nil {
		var bound uuid.UUID
		if err := f.pool.QueryRow(ctx, `SELECT task_id FROM warmup_received WHERE internal_id=$1 AND email_account_id=$2`, receipt, f.recipient).Scan(&bound); err != nil || bound != f.task {
			t.Fatal("legacy or foreign task claimed receipt")
		}
	}
}

func TestLiveLineageQueueRevisionsRejectStaleAcknowledgements(t *testing.T) {
	f, r := lineageFixture(t)
	ctx := t.Context()
	if _, err := f.pool.Exec(ctx, `INSERT INTO warmup_tasks(task_id)VALUES($1)`, f.task); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE tasks SET status='pending',scheduled_at=NOW()-INTERVAL '1 minute' WHERE id=$1`, f.task); err != nil {
		t.Fatal(err)
	}
	rows, err := r.UnqueuedWarmupTasks(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	var first WarmupQueueRevision
	for _, row := range rows {
		if row.TaskID == f.task {
			first = row
		}
	}
	if first.TaskID == uuid.Nil {
		t.Fatal("pending SQL task absent from reconciliation")
	}
	if ok, err := r.AckWarmupQueue(ctx, first, "first-handle"); err != nil || !ok {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := r.RescheduleWarmupTask(ctx, f.task, later); err != nil {
		t.Fatal(err)
	}
	if ok, err := r.AckWarmupQueue(ctx, first, "stale-handle"); err != nil || ok {
		t.Fatal("stale revision replaced schedule")
	}
	rows, err = r.UnqueuedWarmupTasks(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	var second WarmupQueueRevision
	for _, row := range rows {
		if row.TaskID == f.task {
			second = row
		}
	}
	if second.Revision <= first.Revision || second.PreviousHandle == nil || *second.PreviousHandle != "first-handle" {
		t.Fatal("new revision lost old queue handle")
	}
	if ok, err := r.AckWarmupQueue(ctx, second, "new-handle"); err != nil || !ok {
		t.Fatal(err)
	}
	if ok, err := r.ClaimWarmupTask(ctx, f.task, time.Now()); err != nil || ok {
		t.Fatal("early callback bypassed authoritative schedule")
	}
	if ok, err := r.ClaimWarmupTask(ctx, f.task, later.Add(time.Second)); err != nil || !ok {
		t.Fatal(err)
	}
	if ok, err := r.ClaimWarmupTask(ctx, f.task, later.Add(time.Second)); err != nil || ok {
		t.Fatal("duplicate callback claimed active task")
	}
	if err := r.ReleaseUnpublishedWarmupTask(ctx, f.task, later.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := f.pool.QueryRow(ctx, `SELECT status FROM tasks WHERE id=$1`, f.task).Scan(&status); err != nil || status != "pending" {
		t.Fatal("unpublished task was lost")
	}
}

func TestLiveLineageRelayRewriteRequiresExplicitReceiptAuthority(t *testing.T) {
	for _, name := range []string{"marked rewrite", "exact headerless", "headerless rewrite", "wrong token", "wrong sender", "foreign recipient", "unstamped", "unfinished", "expired", "consumed", "retired"} {
		t.Run(name, func(t *testing.T) {
			f, r := lineageFixture(t)
			ctx := t.Context()
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := f.pool.Exec(ctx, query, args...); err != nil {
					t.Fatal(err)
				}
			}
			token, receipt := uuid.New(), uuid.New()
			exec(`INSERT INTO warmup_tasks(task_id,lineage_version,subject,max_turns,scenario_version,rendering_version)VALUES($1,1,'Pinned',3,'static-v1','canonical-v1')`, f.task)
			exec(`INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,sent_message_id,conversation_id)VALUES($1,$2,$3,$4,'<sent@example.test>',$5)`, token, f.task, f.sender, f.recipient, uuid.New())
			proof := WarmupReceiptProof{Token: token, SenderAddress: f.senderTo}
			recipient, messageID := f.recipient, "received@mailjet.com"
			switch name {
			case "exact headerless":
				proof.Token, messageID = uuid.Nil, "sent@example.test"
			case "headerless rewrite":
				proof.Token = uuid.Nil
			case "wrong token":
				proof.Token = uuid.New()
			case "wrong sender":
				proof.SenderAddress = "forwarder@example.test"
			case "foreign recipient":
				recipient = f.sender
			case "unstamped":
				exec(`UPDATE warmup_tokens SET sent_message_id='' WHERE token=$1`, token)
			case "unfinished":
				exec(`UPDATE tasks SET status='active' WHERE id=$1`, f.task)
			case "expired":
				exec(`UPDATE warmup_tokens SET expires_at=NOW()-INTERVAL '1 second' WHERE token=$1`, token)
			case "consumed":
				exec(`UPDATE warmup_tokens SET consumed_at=NOW() WHERE token=$1`, token)
			case "retired":
				exec(`UPDATE warmup_tokens SET sent_retired_at=NOW() WHERE token=$1`, token)
			}
			exec(`INSERT INTO warmup_received(email_account_id,internal_id,sender_account_id,message_id)VALUES($1,$2,$3,$4)`, recipient, receipt, f.sender, messageID)
			err := r.RecordVerifiedWarmupParent(ctx, f.task, recipient, receipt, "recipient-thread", proof)
			allowed := name == "marked rewrite" || name == "exact headerless"
			if (err == nil) != allowed {
				t.Fatalf("allowed=%v err=%v", allowed, err)
			}
			if !allowed {
				return
			}
			if err := r.RecordVerifiedWarmupParent(ctx, f.task, recipient, receipt, "recipient-thread", proof); err != nil {
				t.Fatal("retry of bound receipt failed", err)
			}
			duplicate := uuid.New()
			exec(`INSERT INTO warmup_received(email_account_id,internal_id,sender_account_id,message_id)VALUES($1,$2,$3,'replayed@smtp-pulse.com')`, recipient, duplicate, f.sender)
			if err := r.RecordVerifiedWarmupParent(ctx, f.task, recipient, duplicate, "", proof); err == nil {
				t.Fatal("repeated marker authorized another receipt")
			}
			ok, err := r.BindWarmupSuccessor(ctx, f.task, recipient, receipt, time.Now().Add(time.Hour))
			if err != nil || !ok {
				t.Fatalf("successor bound=%v err=%v", ok, err)
			}
			var successor uuid.UUID
			if err := f.pool.QueryRow(ctx, `SELECT task_id FROM warmup_tasks WHERE parent_task_id=$1`, f.task).Scan(&successor); err != nil {
				t.Fatal(err)
			}
			parent, err := r.ExactWarmupParent(ctx, successor)
			if err != nil || parent == nil || parent.MessageID != messageID || parent.ThreadID == nil || *parent.ThreadID != "recipient-thread" {
				t.Fatalf("reply did not retain actual received ID and thread: %+v %v", parent, err)
			}
		})
	}
}

func TestLiveWarmupReceiptSerializationLeavesSmallPoolCapacity(t *testing.T) {
	f, _ := lineageFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(os.Getenv("WARMBLY_TEST_DB"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxConns, cfg.MinConns = 2, 0
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	r := NewWarmupRepository(pool).(*warmupRepository)
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for range 16 {
		token := uuid.New()
		if _, err := f.pool.Exec(ctx, `INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id)VALUES($1,$2,$3,$4)`, token, f.task, f.sender, f.recipient); err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.ProcessWarmupReceipt(ctx, token, f.recipient, WarmupReceiptProof{SenderAddress: f.senderTo}, func(current *models.WarmupToken) error {
				var value int
				if err := pool.QueryRow(ctx, `SELECT 1`).Scan(&value); err != nil {
					return err
				}
				accepted.Add(1)
				return r.ConsumeWarmupToken(ctx, current.Token)
			})
			if err != nil && !errors.Is(err, ErrWarmupReceiptBusy) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() == 0 {
		t.Fatal("receipt lock exhausted capacity for the acceptance callback")
	}
}

func TestLiveLineageDispatchClaimsNativeExecutionOnceAndReplaysDurableResult(t *testing.T) {
	f, r := lineageFixture(t)
	ctx := t.Context()
	worker := uuid.New()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol)VALUES($1,'worker',true,NOW(),1)`, worker)
	exec(`INSERT INTO workers(id)VALUES($1)`, worker)
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, worker) })
	exec(`UPDATE email_accounts SET worker_id=$1 WHERE id=$2`, worker, f.sender)
	exec(`INSERT INTO warmup_pool_participants(pool_id,email_account_id) SELECT wp.id,ea.id FROM warmup_pools wp CROSS JOIN email_accounts ea WHERE pool_type='free' AND ea.id IN($1,$2) ON CONFLICT DO NOTHING`, f.sender, f.recipient)
	exec(`INSERT INTO warmup_tasks(task_id,target_account_id,lineage_version,subject,scenario_version,rendering_version,max_turns)VALUES($1,$2,1,'Pinned diagnostic','diagnostic-v1','canonical-v1',3)`, f.task, f.recipient)
	exec(`UPDATE tasks SET status='active' WHERE id=$1`, f.task)
	f.token(t, "", "Pinned diagnostic", false)
	nonce, err := r.AuthorizeWarmupDispatch(ctx, f.task, f.sender, worker)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := r.GetSendAdmission(ctx, f.org, f.sender, models.InboxProviderSMTPIMAP, time.Now())
	if err != nil || admission.Allowed || !admission.RecoveryHold {
		t.Fatal("reservation failed to hold unknown send")
	}
	if _, err := r.BeginWarmupDispatch(ctx, f.task, f.sender, worker, uuid.New()); err == nil {
		t.Fatal("foreign nonce authorized or revoked dispatch")
	}
	var native atomic.Int32
	var wg sync.WaitGroup
	failures := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			state, err := r.BeginWarmupDispatch(ctx, f.task, f.sender, worker, nonce)
			if err != nil {
				failures <- err
			} else if state.State == "execute" {
				native.Add(1)
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	if native.Load() != 1 {
		t.Fatalf("native executions=%d", native.Load())
	}
	if err := r.DeferWarmupDispatch(ctx, f.task, f.sender, worker, nonce, time.Now()); err == nil {
		t.Fatal("started execution was returned to pending")
	}
	result := models.SendEmailResult{TaskID: f.task, Success: true, MessageID: "sent@example.test", SentAt: time.Now().UTC()}
	for range 2 {
		if err := r.FinishWarmupDispatch(ctx, f.task, f.sender, worker, result); err != nil {
			t.Fatal(err)
		}
	}
	state, err := NewTaskRepository(f.pool).(*taskRepository).InspectWarmupDispatch(ctx, f.task, f.sender, worker)
	if err != nil || state.State != "finished" || state.Result == nil || state.Result.MessageID != result.MessageID {
		t.Fatalf("restart result=%+v %v", state, err)
	}
	conflict := result
	conflict.Success = false
	if err := r.FinishWarmupDispatch(ctx, f.task, f.sender, worker, conflict); err == nil {
		t.Fatal("terminal evidence overwritten")
	}
	if err := r.ApplySendResult(ctx, result, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	admission, err = r.GetSendAdmission(ctx, f.org, f.sender, models.InboxProviderSMTPIMAP, time.Now())
	if err != nil || !admission.Allowed {
		t.Fatalf("reconciled send still held: %+v %v", admission, err)
	}
	// A policy change after publication defers safely only while native execution has not begun.
	next := uuid.New()
	exec(`INSERT INTO tasks(id,email_account_id,task_type,status,message_id)VALUES($1,$2,'warmup','active','')`, next, f.sender)
	exec(`INSERT INTO warmup_tasks(task_id,target_account_id,lineage_version)VALUES($1,$2,1)`, next, f.recipient)
	exec(`INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id)VALUES($1,$2,$3,$4)`, uuid.New(), next, f.sender, f.recipient)
	nonce, err = r.AuthorizeWarmupDispatch(ctx, next, f.sender, worker)
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE email_accounts SET status='inactive' WHERE id=$1`, f.recipient)
	state, err = r.BeginWarmupDispatch(ctx, next, f.sender, worker, nonce)
	if err != nil || state.State != "deferred" {
		t.Fatalf("revoked partner executed: %+v %v", state, err)
	}
	var status string
	var revoked *uuid.UUID
	if err := f.pool.QueryRow(ctx, `SELECT t.status,w.dispatch_nonce FROM tasks t JOIN warmup_tasks w ON w.task_id=t.id WHERE t.id=$1`, next).Scan(&status, &revoked); err != nil || status != "pending" || revoked != nil {
		t.Fatal("unsent reservation not safely requeued")
	}
}
