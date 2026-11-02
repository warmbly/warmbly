package repository

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLiveReplyShareAnswersOwedMailOnAClaimedSend(t *testing.T) {
	f, r := lineageFixture(t)
	ctx := t.Context()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO warmup_tasks(task_id,lineage_version,subject,scenario_version,rendering_version,max_turns)
		VALUES($1,1,'Pinned diagnostic','diagnostic-v1','canonical-v1',3)`, f.task)
	exec(`INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,conversation_id,conversation_turn,sent_message_id)
		VALUES($1,$2,$3,$4,$5,0,'<[EMAIL]>')`, uuid.New(), f.task, f.sender, f.recipient, uuid.New())
	receipt := uuid.New()
	exec(`INSERT INTO warmup_received(email_account_id,internal_id,sender_account_id,message_id,created_at)
		VALUES($1,$2,$3,'[EMAIL]',NOW()-INTERVAL '10 minutes')`, f.recipient, receipt, f.sender)
	if err := r.RecordVerifiedWarmupParent(ctx, f.task, f.recipient, receipt, "thread", WarmupReceiptProof{SenderAddress: f.senderTo}); err != nil {
		t.Fatal(err)
	}

	owed, err := r.OwedWarmupReplies(ctx, f.recipient, 8)
	if err != nil || len(owed) != 0 {
		t.Fatalf("mail younger than the reply delay was offered: %d %v", len(owed), err)
	}
	exec(`UPDATE warmup_received SET created_at=NOW()-INTERVAL '2 hours' WHERE internal_id=$1`, receipt)
	owed, err = r.OwedWarmupReplies(ctx, f.recipient, 8)
	if err != nil || len(owed) != 1 || owed[0].ParentTaskID != f.task || owed[0].SenderID != f.sender || owed[0].Parent.MessageID != "[EMAIL]" {
		t.Fatalf("owed reply not offered: %+v %v", owed, err)
	}

	pending, due := uuid.New(), time.Now()
	created, err := r.CreateWarmupTaskWithLock(ctx, &Task{ID: pending, EmailAccountID: f.recipient, TaskType: "warmup", Status: "pending", ScheduledAt: &due}, &WarmupTask{TaskID: pending})
	if err != nil || !created {
		t.Fatalf("create pending: %v %v", created, err)
	}
	if bound, err := r.BindClaimedWarmupReply(ctx, pending, f.recipient, f.task, receipt); err != nil || bound {
		t.Fatalf("an unclaimed send was bound: %v %v", bound, err)
	}
	exec(`UPDATE tasks SET status='active' WHERE id=$1`, pending)
	claimed := pending
	if bound, err := r.BindClaimedWarmupReply(ctx, claimed, f.sender, f.task, receipt); err != nil || bound {
		t.Fatalf("another mailbox answered mail it never received: %v %v", bound, err)
	}
	if bound, err := r.BindClaimedWarmupReply(ctx, claimed, f.recipient, f.task, receipt); err != nil || !bound {
		t.Fatalf("claimed send not bound to the owed reply: %v %v", bound, err)
	}
	var parent, target *uuid.UUID
	if err := f.pool.QueryRow(ctx, `SELECT parent_task_id,target_account_id FROM warmup_tasks WHERE task_id=$1`, claimed).Scan(&parent, &target); err != nil {
		t.Fatal(err)
	}
	if parent == nil || *parent != f.task || target == nil || *target != f.sender {
		t.Fatalf("reply authority missing: parent=%v target=%v", parent, target)
	}
	if c, err := r.ExactWarmupParent(ctx, claimed); err != nil || c == nil {
		t.Fatalf("bound reply has no exact parent: %v %v", c, err)
	}
	if owed, err := r.OwedWarmupReplies(ctx, f.recipient, 8); err != nil || len(owed) != 0 {
		t.Fatalf("answered mail still offered: %d %v", len(owed), err)
	}

	exec(`INSERT INTO warmup_statistics(email_account_id,date,emails_sent,emails_replied,target_volume) VALUES
		($1,(NOW() AT TIME ZONE 'UTC')::date,10,3,0),($1,(NOW() AT TIME ZONE 'UTC')::date-6,4,1,0),($1,(NOW() AT TIME ZONE 'UTC')::date-7,50,0,0)`, f.recipient)
	sent, replied, err := r.WarmupReplyShare(ctx, f.recipient)
	if err != nil || sent != 14 || replied != 4 {
		t.Fatalf("share window = %d sent %d replied (%v), want 14 and 4", sent, replied, err)
	}
}
