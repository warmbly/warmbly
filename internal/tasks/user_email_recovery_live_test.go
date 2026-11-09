package tasks

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/events"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

// countingPublisher hands a send to nobody; fail makes the publish call itself error.
type countingPublisher struct {
	events.Publisher
	mu     sync.Mutex
	fail   error
	nonces map[uuid.UUID]string
}

func (p *countingPublisher) PublishSendEmail(_ context.Context, _ uuid.UUID, params *events.SendEmailParams) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.nonces == nil {
		p.nonces = map[uuid.UUID]string{}
	}
	if _, again := p.nonces[params.TaskID]; again {
		p.nonces[params.TaskID] += "+again"
	} else {
		p.nonces[params.TaskID] = params.DispatchNonce
	}
	return p.fail
}

func (p *countingPublisher) published(id uuid.UUID) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	nonce, ok := p.nonces[id]
	return nonce, ok
}

type manualRecoveryFixture struct {
	*campaignSendFixture
	svc    *tasksService
	pub    *countingPublisher
	worker uuid.UUID
}

type manualRecoveryRepo interface {
	interruptedManualSends
	RecoverUnknownSendHolds(context.Context, time.Time, time.Time, int) (int, []uuid.UUID, error)
	BeginOutbound(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) (*repository.WarmupDispatchState, error)
}

// newManualRecoveryFixture runs unibox sends through the real sender and admission, on a live worker.
func newManualRecoveryFixture(t *testing.T) *manualRecoveryFixture {
	t.Helper()
	handle := liveCampaignDB(t)
	f := &manualRecoveryFixture{campaignSendFixture: newCampaignSendFixture(t, handle.Pool), worker: uuid.New(), pub: &countingPublisher{}}
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM task_dead_letters WHERE task_id IN (SELECT id FROM tasks WHERE email_account_id=$1)`, f.mailbox)
		_, _ = f.pool.Exec(context.Background(), `DELETE FROM fleet_nodes WHERE id=$1`, f.worker)
	})
	f.exec(t, `INSERT INTO fleet_nodes(id,role,active,last_seen_at,warmup_send_protocol) VALUES($1,'worker',true,NOW(),2)`, f.worker)
	f.exec(t, `INSERT INTO workers(id) VALUES($1)`, f.worker)
	f.exec(t, `UPDATE email_accounts SET worker_id=$1 WHERE id=$2`, f.worker, f.mailbox)
	f.svc = liveCampaignService(t, handle, nil)
	sender := NewEmailSender(f.svc.emailRepo, f.pub).(*emailSender)
	sender.WireSendAdmission(f.svc.taskRepo.(repository.OutboundAdmissionRepository))
	f.svc.emailSender = sender
	return f
}

func (f *manualRecoveryFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(t.Context(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func (f *manualRecoveryFixture) repo() manualRecoveryRepo { return f.svc.taskRepo.(manualRecoveryRepo) }

func (f *manualRecoveryFixture) handle(t *testing.T, id uuid.UUID) {
	t.Helper()
	if xerr := f.svc.HandleUserEmailTask(&proto.ProcessTask{TaskId: id.String()}); xerr != nil {
		t.Fatalf("handle send: %v", xerr)
	}
}

// recoverHolds runs the hold sweep as if every reservation had outlived its reclaim window.
func (f *manualRecoveryFixture) recoverHolds(t *testing.T) []uuid.UUID {
	t.Helper()
	later := time.Now().Add(time.Hour)
	_, unsent, err := f.repo().RecoverUnknownSendHolds(t.Context(), later, later, 100)
	if err != nil {
		t.Fatal(err)
	}
	return unsent
}

type manualSendState struct {
	status, result string
	hold           bool
}

func (f *manualRecoveryFixture) state(t *testing.T, id uuid.UUID) manualSendState {
	t.Helper()
	var s manualSendState
	if err := f.pool.QueryRow(t.Context(), `SELECT t.status,COALESCE(t.send_result_state,''),ea.send_recovery_hold
		FROM tasks t JOIN email_accounts ea ON ea.id=t.email_account_id WHERE t.id=$1`, id).Scan(&s.status, &s.result, &s.hold); err != nil {
		t.Fatal(err)
	}
	return s
}

// A manual send whose hand-off is unknown keeps its mailbox held only until
// recovery proves no worker started it; then it is released as not sent, the
// mailbox sends again, and the original command can never start.
func TestLiveManualSendWithUnknownHandOffIsReleasedUnsent(t *testing.T) {
	f := newManualRecoveryFixture(t)
	f.pub.fail = errors.New("publish timed out")
	id := queueUniboxReply(t, f.pool, f.mailbox, "", "")
	f.handle(t, id)
	if got := f.state(t, id); got != (manualSendState{"dead_lettered", "unknown", true}) {
		t.Fatalf("after an unknown hand-off: %+v", got)
	}
	nonce, _ := f.pub.published(id)

	if unsent := f.recoverHolds(t); !slices.Equal(unsent, []uuid.UUID{id}) {
		t.Fatalf("recovery released %v, want the unknown send", unsent)
	}
	if got := f.state(t, id); got != (manualSendState{"cancelled", "failed", false}) {
		t.Fatalf("after recovery: %+v", got)
	}
	var letter string
	if err := f.pool.QueryRow(t.Context(), `SELECT status FROM task_dead_letters WHERE task_id=$1`, id).Scan(&letter); err != nil || letter != "resolved" {
		t.Fatalf("dead letter %q %v, want resolved", letter, err)
	}
	if again := f.recoverHolds(t); len(again) != 0 {
		t.Fatalf("a second sweep released %v again", again)
	}

	state, err := f.repo().BeginOutbound(t.Context(), id, f.mailbox, f.worker, uuid.MustParse(nonce))
	if err == nil && state.State == "execute" {
		t.Fatal("a released send started when its command arrived late")
	}

	f.pub.fail = nil
	next := queueUniboxReply(t, f.pool, f.mailbox, "", "")
	f.handle(t, next)
	if _, ok := f.pub.published(next); !ok {
		t.Fatalf("the mailbox's next send was not handed over: %+v", f.state(t, next))
	}
}

// A send handed to a worker that never starts it is released as not sent.
func TestLiveUnansweredManualSendIsReleasedUnsent(t *testing.T) {
	f := newManualRecoveryFixture(t)
	id := queueUniboxReply(t, f.pool, f.mailbox, "", "")
	f.handle(t, id)
	if got := f.state(t, id); got != (manualSendState{"completed", "unknown", true}) {
		t.Fatalf("after hand-off: %+v", got)
	}
	if unsent := f.recoverHolds(t); !slices.Equal(unsent, []uuid.UUID{id}) {
		t.Fatalf("recovery released %v, want the unanswered send", unsent)
	}
	if got := f.state(t, id); got != (manualSendState{"cancelled", "failed", false}) {
		t.Fatalf("after recovery: %+v", got)
	}
}

// A send that stopped between its claim and its reservation runs again exactly
// once; a redelivery while it is stranded, or after it went out, sends nothing.
func TestLiveManualSendInterruptedBeforeReservationRunsOnce(t *testing.T) {
	f := newManualRecoveryFixture(t)
	id := queueUniboxReply(t, f.pool, f.mailbox, "", "")
	f.exec(t, `UPDATE tasks SET status='active',updated_at=NOW()-INTERVAL '20 minutes' WHERE id=$1`, id)
	f.exec(t, `INSERT INTO task_execution_keys(task_id,execution_key,status,metadata,first_seen_at,last_seen_at,attempts)
		VALUES($1,$2,'in_progress','{}',NOW()-INTERVAL '20 minutes',NOW()-INTERVAL '20 minutes',1)`, id, "user-email:"+id.String())

	f.handle(t, id)
	if _, ok := f.pub.published(id); ok {
		t.Fatal("a redelivery of a claimed send handed it over")
	}
	f.svc.recoverInterruptedManualSends(t.Context())
	if got := f.state(t, id); got.status != "pending" {
		t.Fatalf("interrupted send is %+v, want pending", got)
	}
	f.svc.recoverInterruptedManualSends(t.Context())
	f.handle(t, id)
	f.handle(t, id)
	if nonce, ok := f.pub.published(id); !ok || nonce == "" || len(nonce) != len(uuid.Nil.String()) {
		t.Fatalf("interrupted send handed over %q %v, want exactly once", nonce, ok)
	}
	if got := f.state(t, id); got != (manualSendState{"completed", "unknown", true}) {
		t.Fatalf("after the retry: %+v", got)
	}
	f.svc.recoverInterruptedManualSends(t.Context())
	if got := f.state(t, id); got.status != "completed" {
		t.Fatalf("recovery touched a reserved send: %+v", got)
	}
}

// An interrupted send too old to go out now, or one with nothing to run again
// (a test send), fails with a recorded reason instead of staying claimed.
func TestLiveStaleOrUnrunnableInterruptedManualSendFails(t *testing.T) {
	f := newManualRecoveryFixture(t)
	stale := queueUniboxReply(t, f.pool, f.mailbox, "", "")
	f.exec(t, `UPDATE tasks SET status='active',updated_at=NOW()-INTERVAL '3 hours' WHERE id=$1`, stale)
	testSend := uuid.New()
	f.exec(t, `INSERT INTO tasks(id,task_type,email_account_id,status,message_id,updated_at) VALUES($1,'email',$2,'active','',NOW()-INTERVAL '20 minutes')`, testSend, f.mailbox)
	fresh := queueUniboxReply(t, f.pool, f.mailbox, "", "")
	f.exec(t, `UPDATE tasks SET status='active',updated_at=NOW() WHERE id=$1`, fresh)

	now := time.Now()
	got, err := f.repo().RecoverInterruptedManualSends(t.Context(), now.Add(-10*time.Minute), now.Add(-time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	settled := map[uuid.UUID]bool{}
	for _, s := range got {
		if s.MailboxID != f.mailbox {
			t.Fatalf("settled send names mailbox %s", s.MailboxID)
		}
		settled[s.TaskID] = s.Retried
	}
	if retried, ok := settled[stale]; !ok || retried {
		t.Fatalf("stale send settled=%v retried=%v, want failed", ok, retried)
	}
	if retried, ok := settled[testSend]; !ok || retried {
		t.Fatalf("test send settled=%v retried=%v, want failed", ok, retried)
	}
	if _, ok := settled[fresh]; ok {
		t.Fatal("a send still inside its handler's window was settled")
	}
	for _, id := range []uuid.UUID{stale, testSend} {
		var status, title string
		if err := f.pool.QueryRow(t.Context(), `SELECT t.status,f.title FROM tasks t JOIN task_failures f ON f.task_id=t.id WHERE t.id=$1`, id).Scan(&status, &title); err != nil || status != "failed" || title != "Send interrupted" {
			t.Fatalf("%s: status=%q failure=%q %v", id, status, title, err)
		}
	}
	if got := f.state(t, fresh); got.status != "active" {
		t.Fatalf("fresh claim is %+v", got)
	}
}
