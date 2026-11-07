package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLiveLineageReplyPreservesPlannedCadence(t *testing.T) {
	for _, tc := range []struct {
		name        string
		planned     time.Duration
		requested   time.Duration
		wantAt      time.Duration
		wantBound   bool
		unscheduled bool
	}{
		{name: "keep an eligible earlier send", planned: time.Hour, requested: 3 * time.Hour, wantAt: time.Hour, wantBound: true},
		{name: "pull a later send forward", planned: 6 * time.Hour, requested: 2 * time.Hour, wantAt: 2 * time.Hour, wantBound: true},
		{name: "leave an imminent opening alone", planned: 10 * time.Minute, requested: 3 * time.Hour, wantAt: 10 * time.Minute},
		{name: "allow the minimum reply delay", planned: 45 * time.Minute, requested: 3 * time.Hour, wantAt: 45 * time.Minute, wantBound: true},
		{name: "enforce the receipt delay", planned: 6 * time.Hour, requested: 10 * time.Minute, wantAt: 45 * time.Minute, wantBound: true},
		{name: "schedule an undated task", requested: 2 * time.Hour, wantAt: 2 * time.Hour, wantBound: true, unscheduled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r := lineageFixture(t)
			ctx := t.Context()
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := f.pool.Exec(ctx, query, args...); err != nil {
					t.Fatal(err)
				}
			}
			receivedAt := time.Now().UTC().Truncate(time.Microsecond)
			receipt, pending := uuid.New(), uuid.New()
			exec(`INSERT INTO warmup_tasks(task_id,lineage_version,subject,scenario_version,rendering_version,max_turns)
				VALUES($1,1,'Pinned diagnostic','diagnostic-v1','canonical-v1',3)`, f.task)
			exec(`INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,conversation_id,conversation_turn,sent_message_id)
				VALUES($1,$2,$3,$4,$5,0,'<parent@example.test>')`, uuid.New(), f.task, f.sender, f.recipient, uuid.New())
			exec(`INSERT INTO warmup_received(email_account_id,internal_id,sender_account_id,message_id,created_at)
				VALUES($1,$2,$3,'parent@example.test',$4)`, f.recipient, receipt, f.sender, receivedAt)
			if err := r.RecordVerifiedWarmupParent(ctx, f.task, f.recipient, receipt, "thread", WarmupReceiptProof{SenderAddress: f.senderTo}); err != nil {
				t.Fatal(err)
			}
			plannedAt := receivedAt.Add(tc.planned)
			var schedule *time.Time
			if !tc.unscheduled {
				schedule = &plannedAt
			}
			created, err := r.CreateWarmupTaskWithLock(ctx,
				&Task{ID: pending, EmailAccountID: f.recipient, TaskType: "warmup", Status: "pending", ScheduledAt: schedule},
				&WarmupTask{TaskID: pending})
			if err != nil || !created {
				t.Fatalf("create pending: created=%v err=%v", created, err)
			}

			bound, err := r.BindWarmupSuccessor(ctx, f.task, f.recipient, receipt, receivedAt.Add(tc.requested))
			if err != nil {
				t.Fatal(err)
			}
			var at time.Time
			var parent, target *uuid.UUID
			var revision int64
			if err := f.pool.QueryRow(ctx, `SELECT t.scheduled_at,w.parent_task_id,w.target_account_id,w.schedule_revision
				FROM tasks t JOIN warmup_tasks w ON w.task_id=t.id WHERE t.id=$1`, pending).Scan(&at, &parent, &target, &revision); err != nil {
				t.Fatal(err)
			}
			if bound != tc.wantBound || !at.Equal(receivedAt.Add(tc.wantAt)) {
				t.Errorf("bound=%v scheduled after receipt=%s, want bound=%v delay=%s", bound, at.Sub(receivedAt), tc.wantBound, tc.wantAt)
			}
			if tc.wantBound {
				if parent == nil || *parent != f.task || target == nil || *target != f.sender || revision != 2 {
					t.Fatalf("reply authority or queue revision missing: parent=%v target=%v revision=%d", parent, target, revision)
				}
				if again, err := r.BindWarmupSuccessor(ctx, f.task, f.recipient, receipt, receivedAt.Add(4*time.Hour)); err != nil || again {
					t.Fatalf("duplicate receipt rebound the reply: bound=%v err=%v", again, err)
				}
			} else if parent != nil || target != nil || revision != 1 {
				t.Fatalf("earlier opening was changed: parent=%v target=%v revision=%d", parent, target, revision)
			}
			if !tc.unscheduled {
				acked, err := r.AckWarmupQueue(ctx, WarmupQueueRevision{TaskID: pending, At: plannedAt, Revision: 1}, "original-queue-handle")
				if err != nil || acked == tc.wantBound {
					t.Fatalf("original queue acknowledgement accepted=%v, reply bound=%v: %v", acked, tc.wantBound, err)
				}
			}
			if tc.wantBound {
				acked, err := r.AckWarmupQueue(ctx, WarmupQueueRevision{TaskID: pending, At: at, Revision: revision}, "reply-queue-handle")
				if err != nil || !acked {
					t.Fatalf("reply could not be queued: accepted=%v err=%v", acked, err)
				}
			}
			var count int
			if err := f.pool.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE email_account_id=$1 AND task_type='warmup' AND status='pending'`, f.recipient).Scan(&count); err != nil || count != 1 {
				t.Fatalf("pending sends=%d, want 1: %v", count, err)
			}
		})
	}
}
