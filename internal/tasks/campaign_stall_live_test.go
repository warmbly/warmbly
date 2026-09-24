package tasks

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/repository"
	"github.com/warmbly/warmbly/internal/tasks/proto"
)

// processTask is the payload the dispatcher hands a handler.
func processTask(id uuid.UUID) *proto.ProcessTask {
	return &proto.ProcessTask{TaskId: id.String()}
}

// Live checks for issue #189: a campaign that shows ACTIVE with every lead at
// "Queued / Not started" and nothing sending.
//
// The chain is a single self-perpetuating task. A tick that finds nothing due
// (every lead mid-wait) used to park its successor at the literal next-due
// moment, which for a "wait 3 days" step is three days out — and since that
// parked task is also the next time anything re-reads the campaign, leads
// imported in between were invisible until it fired.

// addLead attaches one more brand-new lead, the "Import" / "Add lead" action
// from the campaign's Leads tab.
func (f *campaignSendFixture) addLead(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `INSERT INTO contacts (id, user_id, organization_id, email,
	        first_name, last_name, company, phone, custom_fields, created_at)
	    VALUES ($1, $2, $3, $4, 'Late', 'Lead', '', '', '{}', NOW() + interval '1 second')`,
		id, f.user, f.org, "late-"+id.String()[:8]+"@test.local"); err != nil {
		t.Fatalf("add lead: %v", err)
	}
	if _, err := f.pool.Exec(ctx,
		`INSERT INTO campaign_leads (campaign_id, contact_id, position) VALUES ($1, $2, 1)`,
		f.campaign, id); err != nil {
		t.Fatalf("link lead: %v", err)
	}
	return id
}

// addWaitingFollowUp gives the campaign a second step gated behind a long wait
// and routes step 1 into it, then stamps the original lead's step 1 as sent —
// the state in which nothing is due for days.
func (f *campaignSendFixture) addWaitingFollowUp(t *testing.T, waitDays int) {
	t.Helper()
	ctx := context.Background()
	step2 := uuid.New()
	if _, err := f.pool.Exec(ctx, `INSERT INTO sequences (id, campaign_id, organization_id, name,
	        subject, body_plain, body_html, wait_after, position, kind)
	    VALUES ($1, $2, $3, 'Step 2', 'Bump', 'Bump', '<p>Bump</p>', $4, 1, 'email')`,
		step2, f.campaign, f.org, waitDays); err != nil {
		t.Fatalf("add step 2: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE sequences
	    SET conditions = jsonb_build_object('branches', jsonb_build_array(
	        jsonb_build_object('branch_id', 'else', 'target_step_id', $1::text)))
	    WHERE id = $2`, step2.String(), f.step); err != nil {
		t.Fatalf("connect steps: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO campaign_contact_progress
	    (campaign_id, contact_id, sequence_id, sent_at) VALUES ($1, $2, $3, NOW())`,
		f.campaign, f.contact, f.step); err != nil {
		t.Fatalf("stamp step 1: %v", err)
	}
}

// parkedWakeup is the campaign's earliest pending task: when the chain next
// wakes, and the task the next tick will run.
func (f *campaignSendFixture) parkedWakeup(t *testing.T) (uuid.UUID, time.Time) {
	t.Helper()
	var id uuid.UUID
	var at time.Time
	if err := f.pool.QueryRow(context.Background(), `
		SELECT t.id, t.scheduled_at FROM tasks t
		JOIN campaign_tasks ct ON ct.task_id = t.id
		WHERE ct.campaign_id = $1 AND t.status = 'pending'
		ORDER BY t.scheduled_at ASC LIMIT 1`, f.campaign).Scan(&id, &at); err != nil {
		t.Fatalf("no pending wakeup for the campaign: %v", err)
	}
	return id, at
}

// TestLiveDeferredTickDoesNotParkTheCampaignForDays runs a real tick on a
// campaign whose only lead is three days into a follow-up wait, and asserts the
// successor wakes inside the defer horizon rather than in three days.
func TestLiveDeferredTickDoesNotParkTheCampaignForDays(t *testing.T) {
	handle := liveCampaignDB(t)
	sender := &recordingSender{}
	svc := liveCampaignService(t, handle, sender)
	f := newCampaignSendFixture(t, handle.Pool)
	f.addWaitingFollowUp(t, 3)

	taskID := f.queueTick(t, svc.taskRepo)
	if xerr := svc.HandleCampaignTask(processTask(taskID)); xerr != nil {
		t.Fatalf("tick: %v", xerr)
	}
	if sender.count() != 0 {
		t.Fatalf("a deferred tick must not send, sent %d", sender.count())
	}

	_, at := f.parkedWakeup(t)
	horizon := time.Now().Add(config.CampaignMaxDeferMinutes*time.Minute + time.Minute)
	if at.After(horizon) {
		t.Fatalf("campaign parked until %s (%s away); nothing would re-read it before then",
			at.UTC().Format(time.RFC3339), time.Until(at).Round(time.Minute))
	}
	t.Logf("deferred tick re-checks in %s", time.Until(at).Round(time.Second))
}

// TestLiveLeadAddedToAParkedCampaignSendsOnTheNextTick is the reported symptom
// end to end: leads land on a campaign that had nothing to do, and the next
// tick has to pick them up and send.
func TestLiveLeadAddedToAParkedCampaignSendsOnTheNextTick(t *testing.T) {
	handle := liveCampaignDB(t)
	sender := &recordingSender{}
	svc := liveCampaignService(t, handle, sender)
	f := newCampaignSendFixture(t, handle.Pool)
	f.addWaitingFollowUp(t, 3)

	// Tick one: nothing due, so the chain parks.
	if xerr := svc.HandleCampaignTask(processTask(f.queueTick(t, svc.taskRepo))); xerr != nil {
		t.Fatalf("first tick: %v", xerr)
	}
	next, parked := f.parkedWakeup(t)

	// A lead is imported into the running campaign.
	f.addLead(t)

	// The parked wakeup fires (capped, so this is minutes away, not days) and
	// the new lead's first email goes out.
	if xerr := svc.HandleCampaignTask(processTask(next)); xerr != nil {
		t.Fatalf("second tick: %v", xerr)
	}
	if sender.count() != 1 {
		t.Fatalf("the imported lead was not sent: sends=%d, chain was parked until %s",
			sender.count(), parked.UTC().Format(time.RFC3339))
	}
}

// TestLiveReconcilerPullsAStaleParkForward covers chains parked before the
// deferral cap existed: the wakeup sits days out while work is due now, and
// nothing re-reads the campaign until it fires. The reconciler has to move it.
func TestLiveReconcilerPullsAStaleParkForward(t *testing.T) {
	handle := liveCampaignDB(t)
	svc := liveCampaignService(t, handle, &recordingSender{})
	f := newCampaignSendFixture(t, handle.Pool)

	// The stale row: a pending wakeup three days out, with a lead due now.
	stale := uuid.New()
	at := time.Now().Add(72 * time.Hour)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `INSERT INTO tasks (id, task_type, email_account_id, status, message_id, scheduled_at)
	    VALUES ($1, 'campaign', $2, 'pending', '', $3)`, stale, f.mailbox, at); err != nil {
		t.Fatalf("park task: %v", err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO campaign_tasks (task_id, campaign_id) VALUES ($1, $2)`,
		stale, f.campaign); err != nil {
		t.Fatalf("park campaign task: %v", err)
	}

	if _, err := svc.ReconcileCampaignSchedules(ctx, 500); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	_, moved := f.parkedWakeup(t)
	if !moved.Before(at.Add(-config.CampaignReparkMarginMinutes * time.Minute)) {
		t.Fatalf("stale park left at %s; the campaign has a lead due now",
			moved.UTC().Format(time.RFC3339))
	}
	t.Logf("pulled the wakeup from %s forward to %s",
		at.UTC().Format(time.RFC3339), moved.UTC().Format(time.RFC3339))
}

// TestLiveReseededChainWakesWhenALeadIsDue: a chain seeded with a lead due now
// wakes now. Its old wakeup was the paced slot of the send after that one, so a
// campaign that had just started sat at "Sending" for minutes with nothing sent.
func TestLiveReseededChainWakesWhenALeadIsDue(t *testing.T) {
	handle := liveCampaignDB(t)
	svc := liveCampaignService(t, handle, &recordingSender{})
	f := newCampaignSendFixture(t, handle.Pool)

	if _, err := svc.ReconcileCampaignSchedules(context.Background(), 500); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	_, at := f.parkedWakeup(t)
	if wait := time.Until(at); wait > time.Minute {
		t.Fatalf("a campaign with a lead due now first wakes in %s", wait.Round(time.Second))
	}
}

// TestLiveIsPacedSuccessorTellsSendSpacingFromARecheck: leads arriving may
// pull a deferral's recheck or a reconciler re-seed forward, but never the
// successor a sending tick parked, which is the campaign's send spacing.
func TestLiveIsPacedSuccessorTellsSendSpacingFromARecheck(t *testing.T) {
	handle := liveCampaignDB(t)
	sender := &recordingSender{}
	svc := liveCampaignService(t, handle, sender)
	ctx := context.Background()
	paced := func(f *campaignSendFixture) bool {
		t.Helper()
		id, _ := f.parkedWakeup(t)
		var task repository.Task
		if err := f.pool.QueryRow(ctx, `SELECT id, created_at FROM tasks WHERE id = $1`, id).
			Scan(&task.ID, &task.CreatedAt); err != nil {
			t.Fatalf("read parked task: %v", err)
		}
		got, err := svc.campaignRepo.IsPacedSuccessor(ctx, f.campaign, task)
		if err != nil {
			t.Fatalf("IsPacedSuccessor: %v", err)
		}
		return got
	}

	sent := newCampaignSendFixture(t, handle.Pool)
	sent.addLead(t)
	if xerr := svc.HandleCampaignTask(processTask(sent.queueTick(t, svc.taskRepo))); xerr != nil {
		t.Fatalf("sending tick: %v", xerr)
	}
	if sender.count() != 1 {
		t.Fatalf("the due lead was not sent: sends=%d", sender.count())
	}
	if !paced(sent) {
		t.Fatal("the successor a sending tick parked read as a recheck")
	}

	// The successor was lost and the reconciler re-seeds the chain minutes later.
	if _, err := sent.pool.Exec(ctx, `DELETE FROM campaign_tasks WHERE task_id IN
	    (SELECT id FROM tasks WHERE status = 'pending' AND email_account_id = $1)`, sent.mailbox); err != nil {
		t.Fatalf("drop successor link: %v", err)
	}
	if _, err := sent.pool.Exec(ctx, `DELETE FROM tasks WHERE status = 'pending' AND email_account_id = $1`, sent.mailbox); err != nil {
		t.Fatalf("drop successor: %v", err)
	}
	if _, err := sent.pool.Exec(ctx, `UPDATE tasks SET created_at = created_at - interval '5 minutes',
	    completed_at = completed_at - interval '5 minutes' WHERE email_account_id = $1`, sent.mailbox); err != nil {
		t.Fatalf("age the sending tick: %v", err)
	}
	if _, err := svc.ReconcileCampaignSchedules(ctx, 500); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if paced(sent) {
		t.Fatal("a reconciler re-seed read as the sending tick's paced successor")
	}

	waiting := newCampaignSendFixture(t, handle.Pool)
	waiting.addWaitingFollowUp(t, 3)
	if xerr := svc.HandleCampaignTask(processTask(waiting.queueTick(t, svc.taskRepo))); xerr != nil {
		t.Fatalf("deferred tick: %v", xerr)
	}
	if paced(waiting) {
		t.Fatal("a deferral's recheck read as send spacing")
	}
}
