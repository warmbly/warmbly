package repository

import (
	"context"
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

	owed, err := r.OwedWarmupReplies(ctx, f.recipient, 8, WarmupReplyExclusions{})
	if err != nil || len(owed) != 0 {
		t.Fatalf("mail younger than the reply delay was offered: %d %v", len(owed), err)
	}
	exec(`UPDATE warmup_received SET created_at=NOW()-INTERVAL '2 hours' WHERE internal_id=$1`, receipt)
	owed, err = r.OwedWarmupReplies(ctx, f.recipient, 8, WarmupReplyExclusions{})
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
	if owed, err := r.OwedWarmupReplies(ctx, f.recipient, 8, WarmupReplyExclusions{}); err != nil || len(owed) != 0 {
		t.Fatalf("answered mail still offered: %d %v", len(owed), err)
	}

	exec(`INSERT INTO warmup_statistics(email_account_id,date,emails_sent,emails_replied,target_volume) VALUES
		($1,(NOW() AT TIME ZONE 'UTC')::date,10,3,0),($1,(NOW() AT TIME ZONE 'UTC')::date-6,4,1,0),($1,(NOW() AT TIME ZONE 'UTC')::date-7,50,0,0)`, f.recipient)
	sent, replied, err := r.WarmupReplyShare(ctx, f.recipient)
	if err != nil || sent != 14 || replied != 4 {
		t.Fatalf("share window = %d sent %d replied (%v), want 14 and 4", sent, replied, err)
	}
}

func TestLiveReplySharePrioritizesRecoveryBeforeCandidateLimit(t *testing.T) {
	for _, tc := range []struct {
		name, health, host, provider, address string
		campaign                              bool
	}{
		{name: "watch", health: "watch"},
		{name: "throttled", health: "throttled"},
		{name: "consumer Gmail", host: "google_workspace", provider: "gmail", address: "reply-priority@gmail.com"},
		{name: "consumer Outlook", provider: "outlook", address: "reply-priority@hotmail.co.uk"},
		{name: "stored consumer host", host: "outlook"},
		{name: "active campaign", campaign: true},
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
			normal := uuid.New()
			exec(`INSERT INTO email_accounts(id,user_id,organization_id,email,name,signature_plain,signature_html,provider,status)
				VALUES($1,$2,$3,$4,'Partner','','','smtp_imap','active')`, normal, f.user, f.org, normal.String()+"@example.test")
			seed := func(task, sender uuid.UUID, age time.Duration) {
				t.Helper()
				if task != f.task {
					exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id) VALUES($1,'warmup',$2,'completed','')`, task, sender)
				}
				exec(`INSERT INTO warmup_tasks(task_id,lineage_version,subject,scenario_version,rendering_version,max_turns)
					VALUES($1,1,'Project notes','diagnostic-v1','canonical-v1',3)`, task)
				messageID := task.String() + "@example.test"
				exec(`INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,conversation_id,conversation_turn,sent_message_id)
					VALUES($1,$2,$3,$4,$5,0,$6)`, uuid.New(), task, sender, f.recipient, uuid.New(), "<"+messageID+">")
				receipt := uuid.New()
				exec(`INSERT INTO warmup_received(email_account_id,internal_id,sender_account_id,message_id,created_at)
					VALUES($1,$2,$3,$4,$5)`, f.recipient, receipt, sender, messageID, time.Now().Add(-age))
				address := f.senderTo
				if sender == normal {
					address = normal.String() + "@example.test"
				}
				if err := r.RecordVerifiedWarmupParent(ctx, task, f.recipient, receipt, "thread", WarmupReceiptProof{SenderAddress: address}); err != nil {
					t.Fatal(err)
				}
			}
			seed(f.task, f.sender, 3*time.Hour)
			newerRecovery := uuid.New()
			seed(newerRecovery, f.sender, 2*time.Hour)
			for i := range 8 {
				seed(uuid.New(), normal, time.Hour+time.Duration(i)*time.Minute)
			}
			owed, err := r.OwedWarmupReplies(ctx, f.recipient, 8, WarmupReplyExclusions{})
			if err != nil || len(owed) != 8 || owed[0].SenderID != normal || owed[7].SenderID != normal {
				t.Fatalf("ordinary messages not ordered newest first: %+v %v", owed, err)
			}
			if tc.health != "" {
				exec(`INSERT INTO warmup_pool_participants(pool_id,email_account_id,health_state)
					SELECT id,$1,$2 FROM warmup_pools WHERE pool_type='free'`, f.sender, tc.health)
			}
			if tc.host != "" || tc.provider != "" {
				exec(`UPDATE email_accounts SET mail_host=$2,provider=COALESCE(NULLIF($3,''),'smtp_imap')::email_provider,email=COALESCE(NULLIF($4,''),email) WHERE id=$1`, f.sender, tc.host, tc.provider, tc.address)
			}
			if tc.campaign {
				campaign := uuid.New()
				exec(`INSERT INTO campaigns(id,user_id,organization_id,name,description,days,status,created_at,updated_at) VALUES($1,$2,$3,'Priority','',62,'active',NOW(),NOW())`, campaign, f.user, f.org)
				exec(`INSERT INTO campaign_senders(campaign_id,email_account_id) VALUES($1,$2)`, campaign, f.sender)
				t.Cleanup(func() {
					if _, err := f.pool.Exec(context.Background(), `DELETE FROM campaigns WHERE id=$1`, campaign); err != nil {
						t.Error(err)
					}
				})
			}
			owed, err = r.OwedWarmupReplies(ctx, f.recipient, 8, WarmupReplyExclusions{})
			if err != nil || len(owed) != 8 || owed[0].ParentTaskID != newerRecovery || owed[1].ParentTaskID != f.task || owed[2].SenderID != normal {
				t.Fatalf("older recovery mail not prioritized before the limit: %+v %v", owed, err)
			}
			excluded := WarmupReplyExclusions{}
			for _, o := range owed {
				excluded.Parents = append(excluded.Parents, o.ParentTaskID)
			}
			remaining, err := r.OwedWarmupReplies(ctx, f.recipient, 8, excluded)
			if err != nil || len(remaining) != 2 || remaining[0].SenderID != normal || remaining[1].SenderID != normal {
				t.Fatalf("next batch did not advance past attempted parents: %+v %v", remaining, err)
			}
			remaining, err = r.OwedWarmupReplies(ctx, f.recipient, 8, WarmupReplyExclusions{Senders: []uuid.UUID{f.sender}})
			if err != nil || len(remaining) != 8 || remaining[0].SenderID != normal || remaining[7].SenderID != normal {
				t.Fatalf("unavailable recovery sender hid ordinary mail: %+v %v", remaining, err)
			}
		})
	}
}

// A reply in a running conversation is answered before newer mail that opens
// one, so replies build back-and-forth threads instead of one reply on each.
func TestLiveOwedRepliesContinueTheDeepestConversationFirst(t *testing.T) {
	f, r := lineageFixture(t)
	ctx := t.Context()
	seed := func(turn int, age time.Duration) uuid.UUID {
		t.Helper()
		task := uuid.New()
		messageID := task.String() + "@example.test"
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO tasks(id,task_type,email_account_id,status,message_id) VALUES($1,'warmup',$2,'completed','')`, []any{task, f.sender}},
			{`INSERT INTO warmup_tasks(task_id,lineage_version,subject,scenario_version,rendering_version,max_turns) VALUES($1,1,'Pinned diagnostic','diagnostic-v1','canonical-v1',6)`, []any{task}},
			{`INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,conversation_id,conversation_turn,sent_message_id,expires_at)
				VALUES($1,$2,$3,$4,$5,$6,$7,NOW()+INTERVAL '1 day')`, []any{uuid.New(), task, f.sender, f.recipient, uuid.New(), turn, "<" + messageID + ">"}},
			{`INSERT INTO warmup_received(email_account_id,internal_id,sender_account_id,message_id,task_id,created_at) VALUES($1,$2,$3,$4,$5,$6)`, []any{f.recipient, uuid.New(), f.sender, messageID, task, time.Now().Add(-age)}},
		} {
			if _, err := f.pool.Exec(ctx, q.sql, q.args...); err != nil {
				t.Fatal(err)
			}
		}
		return task
	}
	opening := seed(0, time.Hour)
	running := seed(2, 3*time.Hour)
	owed, err := r.OwedWarmupReplies(ctx, f.recipient, 8, WarmupReplyExclusions{})
	if err != nil || len(owed) != 2 || owed[0].ParentTaskID != running || owed[1].ParentTaskID != opening {
		t.Fatalf("owed replies %+v (%v), want the running conversation %v before the newer opening %v", owed, err, running, opening)
	}
}
