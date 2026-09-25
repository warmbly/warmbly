package tasks

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/app/cipher"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/scheduler"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

// A campaign is one self-perpetuating task, and "leads stay Queued until the
// campaign is restarted" is that task stopping: a pass that could not hand its
// send to a worker, or failed on anything else, used to end the chain, and a
// mailbox whose worker was gone kept being picked, so pass after pass died the
// same way until the reconciler, or a restart, seeded another.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/tasks/ -run LiveCampaignChain -v

// liveWorkers answers liveness from a fixed set, as the heartbeat would.
type liveWorkers map[uuid.UUID]bool

func (l liveWorkers) IsWorkerLive(_ context.Context, id uuid.UUID) (bool, error) {
	return l[id], nil
}

// workerSender refuses a mailbox no live worker holds, the way the real sender
// does before it publishes, and records every send it accepts.
type workerSender struct {
	recordingSender
	live    liveWorkers
	refused int
	from    map[uuid.UUID]int
}

func (w *workerSender) Send(ctx context.Context, taskID uuid.UUID, msg EmailMessage, account models.Email) error {
	if account.WorkerID == nil || !w.live[*account.WorkerID] {
		w.mu.Lock()
		w.refused++
		w.mu.Unlock()
		return fmt.Errorf("%w (mailbox %s)", ErrWorkerOffline, account.Email)
	}
	if err := w.recordingSender.Send(ctx, taskID, msg, account); err != nil {
		return err
	}
	w.mu.Lock()
	w.from[account.ID]++
	w.mu.Unlock()
	return nil
}

// addWorker registers a heartbeating worker and places the mailbox on it.
func (f *campaignSendFixture) addWorker(t *testing.T, mailbox uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	for _, q := range []string{
		`INSERT INTO fleet_nodes (id, role, active, last_seen_at) VALUES ($1, 'worker', true, now())`,
		`INSERT INTO workers (id) VALUES ($1)`,
	} {
		if _, err := f.pool.Exec(ctx, q, id); err != nil {
			t.Fatalf("worker: %v", err)
		}
	}
	if _, err := f.pool.Exec(ctx, `UPDATE email_accounts SET worker_id = $2 WHERE id = $1`, mailbox, id); err != nil {
		t.Fatalf("place mailbox: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		_, _ = f.pool.Exec(c, `UPDATE email_accounts SET worker_id = NULL WHERE worker_id = $1`, id)
		_, _ = f.pool.Exec(c, `DELETE FROM workers WHERE id = $1`, id)
		_, _ = f.pool.Exec(c, `DELETE FROM fleet_nodes WHERE id = $1`, id)
	})
	return id
}

// addMailbox adds a second sending mailbox to the workspace, held by no worker.
func (f *campaignSendFixture) addMailbox(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	if _, err := f.pool.Exec(ctx, `INSERT INTO email_accounts (id, user_id, organization_id, email, name,
	          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone)
	      VALUES ($1, $2, $3, $4, 'Unplaced', '', '', 'smtp_imap', 'active', 50, 0, 'UTC')`,
		id, f.user, f.org, "unplaced-"+id.String()[:8]+"@test.local"); err != nil {
		t.Fatalf("mailbox: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		for _, q := range []string{
			`DELETE FROM task_execution_keys WHERE task_id IN (SELECT id FROM tasks WHERE email_account_id = $1)`,
			`DELETE FROM task_failures WHERE task_id IN (SELECT id FROM tasks WHERE email_account_id = $1)`,
			`DELETE FROM campaign_tasks WHERE task_id IN (SELECT id FROM tasks WHERE email_account_id = $1)`,
			`DELETE FROM tasks WHERE email_account_id = $1`,
			`UPDATE campaign_leads SET email_account_id = NULL WHERE email_account_id = $1`,
			`DELETE FROM email_accounts WHERE id = $1`,
		} {
			_, _ = f.pool.Exec(c, q, id)
		}
	})
	return id
}

// runChain runs the campaign's pending pass up to n times, the way the
// dispatcher would once each slot arrives, and fails if the chain ever stops.
func runChain(t *testing.T, svc *tasksService, f *campaignSendFixture, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		id, _ := f.parkedWakeup(t)
		_ = svc.HandleCampaignTask(processTask(id))
	}
	f.parkedWakeup(t)
}

// A mailbox whose worker is gone is passed over, so the campaign keeps sending
// from the ones that can, pass after pass, and never lands on it.
func TestLiveCampaignChainSkipsAMailboxWithNoWorker(t *testing.T) {
	handle := liveCampaignDB(t)
	f := newCampaignSendFixture(t, handle.Pool)
	placed := f.mailbox
	worker := f.addWorker(t, placed)
	unplaced := f.addMailbox(t)
	for i := 0; i < 5; i++ {
		f.addLead(t)
	}

	live := liveWorkers{worker: true}
	sender := &workerSender{live: live, from: map[uuid.UUID]int{}}
	svc := liveCampaignService(t, handle, sender)
	svc.scheduler.(scheduler.WorkerLivenessAware).WireWorkerLiveness(live)

	f.queueTick(t, svc.taskRepo)
	runChain(t, svc, f, 6)

	if sender.from[placed] != 6 {
		rows, _ := f.pool.Query(context.Background(), `SELECT t.status, t.scheduled_at, COALESCE(tf.message,'') FROM tasks t JOIN campaign_tasks ct ON ct.task_id=t.id LEFT JOIN task_failures tf ON tf.task_id=t.id WHERE ct.campaign_id=$1 ORDER BY t.created_at`, f.campaign)
		for rows.Next() {
			var st, msg string
			var at time.Time
			_ = rows.Scan(&st, &at, &msg)
			t.Logf("task %s at %s %s", st, time.Until(at).Round(time.Second), msg)
		}
		rows.Close()
		t.Logf("logs: %v", f.logEvents(t))
	}
	if sender.refused != 0 {
		t.Errorf("%d sends were handed to the mailbox no worker holds", sender.refused)
	}
	if got := sender.from[placed]; got != 6 {
		t.Errorf("sent %d from the placed mailbox over 6 passes, want 6", got)
	}
	if sender.from[unplaced] != 0 {
		t.Errorf("sent from the unplaced mailbox")
	}
}

// A pass whose hand-off fails is followed by another a minute later, instead
// of ending the campaign until the reconciler notices, and is not
// dead-lettered for a replay that would run beside the chain.
func TestLiveCampaignChainContinuesAfterAFailedHandOff(t *testing.T) {
	handle := liveCampaignDB(t)
	f := newCampaignSendFixture(t, handle.Pool)
	sender := &recordingSender{}
	sender.setFail(fmt.Errorf("%w (worker gone)", ErrWorkerOffline))
	svc := liveCampaignService(t, handle, sender)

	taskID := f.queueTick(t, svc.taskRepo)
	if xerr := svc.HandleCampaignTask(processTask(taskID)); xerr != nil {
		t.Fatalf("tick: %v", xerr)
	}
	next, at := f.parkedWakeup(t)
	if next == taskID {
		t.Fatal("the failed pass is still the pending one")
	}
	if wait := time.Until(at); wait > 2*time.Minute {
		t.Errorf("next pass in %s, want about a minute", wait.Round(time.Second))
	}
	var status string
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM tasks WHERE id = $1`, taskID).Scan(&status); err != nil || status != "failed" {
		t.Errorf("failed pass status = %q (%v), want failed", status, err)
	}
	var letters int
	_ = f.pool.QueryRow(context.Background(), `SELECT count(*) FROM task_dead_letters WHERE task_id = $1`, taskID).Scan(&letters)
	if letters != 0 {
		t.Errorf("the pass was dead-lettered %d times", letters)
	}

	// Once the worker is back, the next pass sends.
	sender.setFail(nil)
	if xerr := svc.HandleCampaignTask(processTask(next)); xerr != nil {
		t.Fatalf("next tick: %v", xerr)
	}
	if sender.count() != 1 {
		t.Errorf("sent %d after the worker came back, want 1", sender.count())
	}
}

// failingCipher is a KMS that cannot be reached.
type failingCipher struct{}

func (failingCipher) Cipher(context.Context, uuid.UUID) (*cipher.Cipher, error) {
	return nil, errors.New("kms unreachable")
}

// A pass that fails on anything else still leaves the campaign a next pass,
// a minute out, and closes itself rather than staying active forever.
func TestLiveCampaignChainSurvivesAFailedPass(t *testing.T) {
	handle := liveCampaignDB(t)
	f := newCampaignSendFixture(t, handle.Pool)
	svc := liveCampaignService(t, handle, &recordingSender{})
	svc.cipherService = failingCipher{}

	taskID := f.queueTick(t, svc.taskRepo)
	if xerr := svc.HandleCampaignTask(processTask(taskID)); xerr == nil {
		t.Fatal("the pass was expected to fail on the cipher")
	}
	next, at := f.parkedWakeup(t)
	if next == taskID {
		t.Fatal("the failed pass is still the pending one")
	}
	if wait := time.Until(at); wait > 2*time.Minute || wait < 30*time.Second {
		t.Errorf("next pass in %s, want about a minute", wait.Round(time.Second))
	}
	var status string
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM tasks WHERE id = $1`, taskID).Scan(&status); err != nil || status != "failed" {
		t.Errorf("failed pass status = %q (%v), want failed", status, err)
	}
}

// The first 25 due leads are the same on every pass. When each is waiting for
// a reason of its own (here, the mailbox their conversation belongs to is
// inside its send gap), the pass looks further down the queue and sends the
// lead that is ready, instead of deferring the whole campaign.
func TestLiveCampaignPassLooksPastAWaitingHeadOfQueue(t *testing.T) {
	handle := liveCampaignDB(t)
	f := newCampaignSendFixture(t, handle.Pool)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, q, args...); err != nil {
			t.Fatalf("fixture %q: %v", q[:min(60, len(q))], err)
		}
	}
	busy := f.addMailbox(t)
	// The busy mailbox sent a minute ago and waits an hour between sends.
	exec(`UPDATE email_accounts SET min_wait_time = 3600 WHERE id = $1`, busy)
	exec(`INSERT INTO tasks (id, task_type, email_account_id, status, message_id, scheduled_at, completed_at, created_at, updated_at)
	      VALUES ($1, 'warmup', $2, 'completed', '<w@test>', NOW() - interval '1 minute', NOW() - interval '1 minute', NOW(), NOW())`,
		uuid.New(), busy)

	// The fixture's lead and 24 more, oldest first, all bound to it.
	exec(`UPDATE contacts SET created_at = NOW() - interval '2 hours' WHERE id = $1`, f.contact)
	exec(`UPDATE campaign_leads SET email_account_id = $2 WHERE campaign_id = $1 AND contact_id = $3`, f.campaign, busy, f.contact)
	for i := 0; i < 24; i++ {
		id := uuid.New()
		exec(`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, created_at)
		      VALUES ($1, $2, $3, $4, 'Bound', 'Lead', '', '', '{}', NOW() - make_interval(mins => $5))`,
			id, f.user, f.org, "bound-"+id.String()[:8]+"@test.local", 90-i)
		exec(`INSERT INTO campaign_leads (campaign_id, contact_id, position, email_account_id) VALUES ($1, $2, 0, $3)`, f.campaign, id, busy)
	}
	// One free lead at the back of the queue.
	free := uuid.New()
	exec(`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, created_at)
	      VALUES ($1, $2, $3, $4, 'Free', 'Lead', '', '', '{}', NOW())`, free, f.user, f.org, "free-"+free.String()[:8]+"@test.local")
	exec(`INSERT INTO campaign_leads (campaign_id, contact_id, position) VALUES ($1, $2, 0)`, f.campaign, free)

	sender := &recordingSender{}
	svc := liveCampaignService(t, handle, sender)
	taskID := f.queueTick(t, svc.taskRepo)
	if xerr := svc.HandleCampaignTask(processTask(taskID)); xerr != nil {
		t.Fatalf("tick: %v", xerr)
	}
	if sender.count() != 1 {
		t.Fatalf("sent %d, want the free lead behind the waiting ones sent", sender.count())
	}
	if to := sender.message(t, 0).To; len(to) != 1 || to[0] != "free-"+free.String()[:8]+"@test.local" {
		t.Fatalf("sent to %v, want the free lead", to)
	}
}

// A hand-off that fails for a reason of the lead's own counts as an attempt,
// so a lead that can never be handed off is dropped after the usual number of
// tries rather than retried every minute for ever. One refused because no
// worker holds the mailbox is not the lead's doing and counts nothing.
func TestLiveCampaignHandOffFailuresCountOnlyWhenTheyAreTheLeads(t *testing.T) {
	handle := liveCampaignDB(t)
	attempts := func(f *campaignSendFixture) int {
		t.Helper()
		var n int
		_ = f.pool.QueryRow(context.Background(), `SELECT COALESCE(MAX(send_attempts), 0) FROM campaign_contact_progress
		    WHERE campaign_id = $1 AND contact_id = $2`, f.campaign, f.contact).Scan(&n)
		return n
	}
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"worker offline", fmt.Errorf("%w (worker gone)", ErrWorkerOffline), 0},
		{"worker liveness unreadable", fmt.Errorf("%w: redis down", ErrWorkerUnconfirmed), 0},
		{"anything else", errors.New("email account has no organization"), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCampaignSendFixture(t, handle.Pool)
			sender := &recordingSender{}
			sender.setFail(tc.err)
			svc := liveCampaignService(t, handle, sender)
			taskID := f.queueTick(t, svc.taskRepo)
			if xerr := svc.HandleCampaignTask(processTask(taskID)); xerr != nil {
				t.Fatalf("tick: %v", xerr)
			}
			if got := attempts(f); got != tc.want {
				t.Errorf("send attempts = %d, want %d", got, tc.want)
			}
			f.parkedWakeup(t)
		})
	}
}

// flakyQueue refuses its first n enqueues.
type flakyQueue struct{ refuse int }

func (q *flakyQueue) CreateTask(context.Context, *proto.ProcessTask, time.Time) (string, error) {
	if q.refuse > 0 {
		q.refuse--
		return "", errors.New("queue unavailable")
	}
	return "queued", nil
}
func (*flakyQueue) DeleteTask(context.Context, string) error { return nil }

// A next pass the task queue refused is taken back, not left pending with
// nothing to fire it, so the retry seeds one that runs.
func TestLiveCampaignChainRecoversWhenTheQueueRefusesTheNextPass(t *testing.T) {
	handle := liveCampaignDB(t)
	f := newCampaignSendFixture(t, handle.Pool)
	sender := &recordingSender{}
	sender.setFail(fmt.Errorf("%w (worker gone)", ErrWorkerOffline))
	svc := liveCampaignService(t, handle, sender)
	svc.tasksClient = &flakyQueue{refuse: 1}

	taskID := f.queueTick(t, svc.taskRepo)
	if xerr := svc.HandleCampaignTask(processTask(taskID)); xerr != nil {
		t.Fatalf("tick: %v", xerr)
	}
	var pending int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM tasks t JOIN campaign_tasks ct ON ct.task_id = t.id
	    WHERE ct.campaign_id = $1 AND t.status = 'pending'`, f.campaign).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Fatalf("%d pending passes, want exactly the one the retry queued", pending)
	}
	var name string
	_ = f.pool.QueryRow(context.Background(), `SELECT COALESCE(t.cloud_task_name, '') FROM tasks t JOIN campaign_tasks ct ON ct.task_id = t.id
	    WHERE ct.campaign_id = $1 AND t.status = 'pending'`, f.campaign).Scan(&name)
	if name != "queued" {
		t.Fatalf("the pending pass was never queued (cloud_task_name %q)", name)
	}
}
