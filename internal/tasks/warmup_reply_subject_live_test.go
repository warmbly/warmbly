package tasks

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	warmupapp "github.com/warmbly/warmbly/internal/app/warmup"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/generation"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

type capturingWarmupSender struct {
	preparationDeferringSender
	pool           *pgxpool.Pool
	sent           []EmailMessage
	tokenSubject   string
	lineageSubject string
}

// Send reads the token before deferring, because a deferral retires it.
func (s *capturingWarmupSender) Send(ctx context.Context, task uuid.UUID, msg EmailMessage, account models.Email) error {
	s.sent = append(s.sent, msg)
	if err := s.pool.QueryRow(ctx, `SELECT wt.subject,w.subject FROM warmup_tokens wt JOIN warmup_tasks w ON w.task_id=wt.task_id WHERE wt.task_id=$1`, task).Scan(&s.tokenSubject, &s.lineageSubject); err != nil {
		return err
	}
	return s.preparationDeferringSender.Send(ctx, task, msg, account)
}

func TestLiveWarmupReplySubjectReadsAsAReply(t *testing.T) {
	f := newPartnerRoutingFixture(t)
	ctx := t.Context()
	worker, task := uuid.New(), uuid.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, query := range []string{
			`DELETE FROM warmup_received WHERE email_account_id IN(SELECT id FROM email_accounts WHERE organization_id=$1)`,
			`DELETE FROM warmup_tokens WHERE sender_account_id IN(SELECT id FROM email_accounts WHERE organization_id=$1)`,
			`DELETE FROM tasks WHERE email_account_id IN(SELECT id FROM email_accounts WHERE organization_id=$1)`,
		} {
			if _, err := f.pool.Exec(context.Background(), query, f.org); err != nil {
				t.Error(err)
			}
		}
	})
	exec(`INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol) VALUES($1,'worker',true,NOW(),2)`, worker)
	exec(`INSERT INTO workers(id) VALUES($1)`, worker)
	t.Cleanup(func() { _, _ = f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, worker) })
	exec(`UPDATE email_accounts SET worker_id=$1,warmup=NOW()-INTERVAL '30 days',warmup_days=127,warmup_pool_type='free',warmup_reply_rate=100 WHERE organization_id=$2`, worker, f.org)
	f.svc.taskRepo = repository.NewTaskRepository(f.pool)
	f.svc.warmupHealth = warmupapp.NewService(f.svc.warmupRepo)
	f.svc.scheduler = preparationScheduler{}

	// The partner opened a thread to this mailbox, which verified its arrival.
	conv := warmupConversations()[0]
	parent, receipt := uuid.New(), uuid.New()
	parentMessageID := parent.String() + "@shop.test"
	exec(`INSERT INTO tasks(id,task_type,email_account_id,status,message_id) VALUES($1,'warmup',$2,'completed','')`, parent, f.atSmallHost)
	exec(`INSERT INTO warmup_tasks(task_id,lineage_version,subject,scenario_version,rendering_version,max_turns) VALUES($1,1,$2,$3,$4,$5)`,
		parent, conv.Subject, conv.Version, generation.CanonicalRenderingVersion, len(conv.Messages)+1)
	exec(`INSERT INTO warmup_tokens(token,task_id,sender_account_id,recipient_account_id,subject,conversation_id,conversation_turn,sent_message_id,content_source)
		VALUES($1,$2,$3,$4,$5,$6,0,$7,'static')`, uuid.New(), parent, f.atSmallHost, f.sender.ID, conv.Subject, conv.ID, "<"+parentMessageID+">")
	exec(`INSERT INTO warmup_received(email_account_id,internal_id,sender_account_id,message_id,created_at) VALUES($1,$2,$3,$4,NOW()-INTERVAL '1 hour')`,
		f.sender.ID, receipt, f.atSmallHost, parentMessageID)
	partner, xerr := f.svc.emailRepo.GetByID(ctx, f.atSmallHost)
	if xerr != nil {
		t.Fatal(xerr)
	}
	lineage := f.svc.taskRepo.(repository.WarmupLineageRepository)
	if err := lineage.RecordVerifiedWarmupParent(ctx, parent, f.sender.ID, receipt, "thread", repository.WarmupReceiptProof{SenderAddress: partner.Email}); err != nil {
		t.Fatal(err)
	}

	exec(`INSERT INTO tasks(id,email_account_id,task_type,status,message_id,scheduled_at) VALUES($1,$2,'warmup','pending','',NOW()-INTERVAL '1 minute')`, task, f.sender.ID)
	exec(`INSERT INTO warmup_tasks(task_id) VALUES($1)`, task)
	sender := &capturingWarmupSender{preparationDeferringSender: preparationDeferringSender{repo: f.svc.taskRepo.(repository.WarmupDispatchRepository)}, pool: f.pool}
	f.svc.emailSender = sender
	if xerr := f.svc.HandleEmailTask(&proto.ProcessTask{TaskId: task.String()}); xerr != nil {
		t.Fatal(xerr)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("expected one send, got %d", len(sender.sent))
	}
	msg := sender.sent[0]
	want := "Re: " + conv.Subject
	if msg.InReplyTo != parentMessageID || msg.Subject != want {
		t.Fatalf("reply went out as subject=%q in_reply_to=%q, want %q answering %q", msg.Subject, msg.InReplyTo, want, parentMessageID)
	}
	// The recipient matches the token on the subject it receives; the thread keeps its own.
	if sender.tokenSubject != want || sender.lineageSubject != conv.Subject {
		t.Fatalf("token subject=%q lineage subject=%q", sender.tokenSubject, sender.lineageSubject)
	}
}
