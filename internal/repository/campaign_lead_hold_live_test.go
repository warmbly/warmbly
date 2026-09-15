package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// The per-lead hold (issue #470): an out-of-office auto-reply, or a member's
// own pause, parks ONE contact's flow inside ONE campaign without
// unsubscribing them and without removing them from the campaign. Every rule
// below is hand-written SQL plus the router's own arithmetic, so it is
// asserted against the real query rather than only through the scheduler.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveLeadHold -v

func holdRepo(t *testing.T, f *routedPairsFixture) CampaignProgressRepository {
	t.Helper()
	return NewCampaignProgressRepository(f.pool)
}

// A dated hold takes the lead out of the batch and reports its end as the
// next-due moment, so the campaign defers until then instead of completing.
func TestLiveLeadHoldTakesTheLeadOutOfTheBatchUntilItLifts(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 2)
	repo := holdRepo(t, f)
	ctx := context.Background()

	until := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	hold, err := repo.HoldLead(ctx, f.campaign, f.leads[0], &until, "back 14 Sep 2026", "out_of_office")
	if err != nil || hold == nil {
		t.Fatalf("HoldLead = %v, %v; want a hold written", hold, err)
	}

	pairs, nextDue, _ := f.find(t, nil, 25)
	if len(pairs) != 1 || pairs[0].ContactID != f.leads[1] {
		t.Fatalf("got %d pairs (%+v), want only the lead that is not held", len(pairs), pairs)
	}
	if nextDue != nil {
		t.Fatalf("a sendable pair reported next_due=%v", nextDue)
	}

	// With the free lead gone, the hold is the whole answer and has to be
	// reported, or the campaign completes and the held lead is never sent.
	if _, err := pool.Exec(ctx, `DELETE FROM campaign_leads WHERE campaign_id = $1 AND contact_id = $2`,
		f.campaign, f.leads[1]); err != nil {
		t.Fatalf("drop the free lead: %v", err)
	}
	pairs, nextDue, _ = f.find(t, nil, 25)
	if len(pairs) != 0 {
		t.Fatalf("got %d pairs while the only lead was held", len(pairs))
	}
	if nextDue == nil {
		t.Fatal("a held lead reported no next-due time: the campaign would complete and never resume it")
	}
	if d := nextDue.Sub(until); d < -2*time.Second || d > 2*time.Second {
		t.Fatalf("next due = %v, want the hold's end %s", nextDue, until)
	}
}

// A hold with no end has no moment to wake up for, so it must not be reported
// as a next-due time either — that would park the campaign's chain on an
// instant that never arrives.
func TestLiveLeadHoldWithNoEndReportsNoNextDue(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 1)
	repo := holdRepo(t, f)
	ctx := context.Background()

	if hold, err := repo.HoldLead(ctx, f.campaign, f.leads[0], nil, "paused by hand", "manual"); err != nil || hold == nil {
		t.Fatalf("HoldLead = %v, %v; want a hold written", hold, err)
	}
	pairs, nextDue, onSender := f.find(t, nil, 25)
	if len(pairs) != 0 {
		t.Fatalf("got %d pairs while the only lead was held with no end", len(pairs))
	}
	if nextDue != nil || onSender {
		t.Fatalf("an open-ended hold reported next_due=%v waiting_on_sender=%v", nextDue, onSender)
	}
}

// Resuming lifts the hold now: the lead is offered on the next pass, and the
// time it spent held is dropped rather than pushing the step further out.
// "Resume now" has to mean now.
func TestLiveLeadHoldResumeOffersTheLeadAgainImmediately(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 1)
	repo := holdRepo(t, f)
	ctx := context.Background()

	until := time.Now().Add(72 * time.Hour)
	if hold, _ := repo.HoldLead(ctx, f.campaign, f.leads[0], &until, "", "out_of_office"); hold == nil {
		t.Fatal("HoldLead wrote nothing")
	}
	if lifted, err := repo.ResumeLead(ctx, f.campaign, f.leads[0]); err != nil || !lifted {
		t.Fatalf("ResumeLead = %v, %v; want the hold lifted", lifted, err)
	}
	// A second resume changed nothing, and has to say so: the caller only
	// wakes the campaign when a hold was actually lifted.
	if lifted, err := repo.ResumeLead(ctx, f.campaign, f.leads[0]); err != nil || lifted {
		t.Fatalf("ResumeLead on an unheld lead = %v, %v; want (false, nil)", lifted, err)
	}
	pairs, _, _ := f.find(t, nil, 25)
	if len(pairs) != 1 {
		t.Fatalf("got %d pairs after resuming, want the lead offered again", len(pairs))
	}
	hold, err := repo.GetLeadHold(ctx, f.campaign, f.leads[0])
	if err != nil || hold != nil {
		t.Fatalf("GetLeadHold after a resume = %v, %v; want no hold left", hold, err)
	}
}

// The held time does not count against the step's wait. A follow-up three days
// behind its step, held for two days and then left to expire, is due two days
// later than it was — not on the hold's end, and not on its original slot.
func TestLiveLeadHoldDoesNotSpendTheStepsWait(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 1)
	repo := holdRepo(t, f)
	ctx := context.Background()

	// A second step three days after the first, connected from it, with the
	// first already sent an hour ago.
	second := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
	        body_plain, body_html, wait_after, position, kind)
	    VALUES ($1, $2, $3, 'Step 2', 'Following up', 'Hello again', '<p>Hello again</p>', 3, 1, 'email')`,
		second, f.campaign, f.org); err != nil {
		t.Fatalf("second step: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE sequences SET conditions = $2 WHERE id = $1`,
		f.step, `{"branches":[{"conditions":[],"target_step_id":"`+second.String()+`"}]}`); err != nil {
		t.Fatalf("connect the steps: %v", err)
	}
	sentAt := time.Now().Add(-49 * time.Hour).Truncate(time.Second)
	if _, err := pool.Exec(ctx,
		`INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at, dispatched_at)
		 VALUES ($1, $2, $3, $4, $4)`, f.campaign, f.leads[0], f.step, sentAt); err != nil {
		t.Fatalf("stamp the first step: %v", err)
	}

	// The auto-reply landed an hour after the step and held the contact for two
	// days; the hold has since run its course.
	start := time.Now().Add(-48 * time.Hour)
	end := time.Now().Add(-time.Hour)
	if _, err := pool.Exec(ctx,
		`UPDATE campaign_leads SET paused_at = $3, paused_until = $4, pause_source = 'out_of_office'
		 WHERE campaign_id = $1 AND contact_id = $2`, f.campaign, f.leads[0], start, end); err != nil {
		t.Fatalf("write the expired hold: %v", err)
	}

	route, err := repo.RouteContact(ctx, f.campaign, f.leads[0])
	if err != nil {
		t.Fatalf("RouteContact: %v", err)
	}
	if route.Target == nil || *route.Target != second {
		t.Fatalf("routed to %v, want the second step", route.Target)
	}
	if route.Hold != nil {
		t.Fatalf("an expired hold is still reported as live: %+v", route.Hold)
	}
	if route.DueAt == nil {
		t.Fatal("no due time for a follow-up with a wait")
	}
	// Without the hold the step would be due at sentAt + 3 days; the 47 hours
	// the contact was away do not count against that wait.
	want := sentAt.Add(72 * time.Hour).Add(47 * time.Hour)
	if d := route.DueAt.Sub(want); d < -2*time.Minute || d > 2*time.Minute {
		t.Fatalf("due at %s, want about %s (the step's three days plus the time it spent held)",
			route.DueAt.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

// An out-of-office auto-reply must never overrule a person: a live manual hold
// is left alone, and an automatic hold is only ever extended, never cut short.
func TestLiveLeadHoldAutomaticNeverOverrulesAPerson(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 1)
	repo := holdRepo(t, f)
	ctx := context.Background()

	manual := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Second)
	if hold, _ := repo.HoldLead(ctx, f.campaign, f.leads[0], &manual, "long trip", "manual"); hold == nil {
		t.Fatal("the manual hold wrote nothing")
	}
	shorter := time.Now().Add(48 * time.Hour)
	if hold, err := repo.HoldLead(ctx, f.campaign, f.leads[0], &shorter, "auto", "out_of_office"); err != nil || hold != nil {
		t.Fatalf("HoldLead(out_of_office) = %v, %v; an auto-reply must not touch a member's own pause", hold, err)
	}
	hold, err := repo.GetLeadHold(ctx, f.campaign, f.leads[0])
	if err != nil || hold == nil {
		t.Fatalf("GetLeadHold = %v, %v; want the manual hold intact", hold, err)
	}
	if hold.Source != "manual" || hold.Until == nil || !hold.Until.Equal(manual) {
		t.Fatalf("hold = %+v, want the manual hold until %s", hold, manual)
	}

	// An automatic hold, on the other hand, is extended by a later one.
	if _, err := repo.ResumeLead(ctx, f.campaign, f.leads[0]); err != nil {
		t.Fatalf("resume: %v", err)
	}
	near := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	far := time.Now().Add(96 * time.Hour).Truncate(time.Second)
	if hold, _ := repo.HoldLead(ctx, f.campaign, f.leads[0], &near, "back tomorrow", "out_of_office"); hold == nil {
		t.Fatal("the first automatic hold wrote nothing")
	}
	if hold, _ := repo.HoldLead(ctx, f.campaign, f.leads[0], &far, "back in four days", "out_of_office"); hold == nil {
		t.Fatal("a later automatic hold must extend the one already there")
	}
	if hold, _ := repo.HoldLead(ctx, f.campaign, f.leads[0], &near, "back tomorrow", "out_of_office"); hold != nil {
		t.Fatal("an earlier automatic hold must not cut a longer one short")
	}
	hold, _ = repo.GetLeadHold(ctx, f.campaign, f.leads[0])
	if hold == nil || hold.Until == nil || !hold.Until.Equal(far) {
		t.Fatalf("hold = %+v, want the longer automatic hold until %s", hold, far)
	}
}

// A hold on a contact that is not a lead of the campaign writes nothing, and
// reading one says so rather than answering "not held".
func TestLiveLeadHoldRefusesAContactThatIsNotALead(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 1)
	repo := holdRepo(t, f)
	ctx := context.Background()

	stranger := uuid.New()
	until := time.Now().Add(time.Hour)
	if hold, err := repo.HoldLead(ctx, f.campaign, stranger, &until, "", "manual"); err != nil || hold != nil {
		t.Fatalf("HoldLead on a non-lead = %v, %v; want nothing written", hold, err)
	}
	if _, err := repo.ResumeLead(ctx, f.campaign, stranger); err != ErrLeadNotInCampaign {
		t.Fatalf("ResumeLead on a non-lead = %v, want ErrLeadNotInCampaign", err)
	}
	if _, err := repo.GetLeadHold(ctx, f.campaign, stranger); err != ErrLeadNotInCampaign {
		t.Fatalf("GetLeadHold on a non-lead = %v, want ErrLeadNotInCampaign", err)
	}
}

// A hold that has been served stops counting on its own. The shift is derived
// from the overlap between the hold and the wait the next step is serving, so
// once a step has gone out after the hold ended the two stop overlapping — no
// path has to remember to clear anything, and the action and wait nodes that
// advance a lead without ever reserving a send cannot charge it twice.
func TestLiveLeadHoldStopsCountingOnceAStepGoesOutAfterIt(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 1)
	repo := holdRepo(t, f)
	ctx := context.Background()

	second, third := uuid.New(), uuid.New()
	for _, st := range []struct {
		id   uuid.UUID
		name string
		wait int
		pos  int
	}{{second, "Step 2", 0, 1}, {third, "Step 3", 3, 2}} {
		if _, err := pool.Exec(ctx, `INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
		        body_plain, body_html, wait_after, position, kind)
		    VALUES ($1, $2, $3, $4, 'Hi', 'Hello', '<p>Hello</p>', $5, $6, 'email')`,
			st.id, f.campaign, f.org, st.name, st.wait, st.pos); err != nil {
			t.Fatalf("step: %v", err)
		}
	}
	connect := func(from, to uuid.UUID) {
		if _, err := pool.Exec(ctx, `UPDATE sequences SET conditions = $2 WHERE id = $1`,
			from, `{"branches":[{"conditions":[],"target_step_id":"`+to.String()+`"}]}`); err != nil {
			t.Fatalf("connect: %v", err)
		}
	}
	connect(f.step, second)
	connect(second, third)

	// Step 1 sent, then a hold that has since run its course, then step 2 sent
	// on the far side of it — which is what a wait or action node does without
	// ever reserving a send.
	stamp := func(step uuid.UUID, at time.Time) {
		if _, err := pool.Exec(ctx,
			`INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at, dispatched_at)
			 VALUES ($1, $2, $3, $4, $4)`, f.campaign, f.leads[0], step, at); err != nil {
			t.Fatalf("stamp: %v", err)
		}
	}
	stamp(f.step, time.Now().Add(-96*time.Hour))
	if _, err := pool.Exec(ctx,
		`UPDATE campaign_leads SET paused_at = $3, paused_until = $4, pause_source = 'out_of_office'
		 WHERE campaign_id = $1 AND contact_id = $2`,
		f.campaign, f.leads[0], time.Now().Add(-95*time.Hour), time.Now().Add(-48*time.Hour)); err != nil {
		t.Fatalf("write the served hold: %v", err)
	}
	sentAt := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
	stamp(second, sentAt)

	route, err := repo.RouteContact(ctx, f.campaign, f.leads[0])
	if err != nil {
		t.Fatalf("RouteContact: %v", err)
	}
	if route.Target == nil || *route.Target != third {
		t.Fatalf("routed to %v, want the third step", route.Target)
	}
	// Step 3's three days run from step 2, with nothing added: the hold was
	// already served before step 2 went out.
	want := sentAt.Add(72 * time.Hour)
	if d := route.DueAt.Sub(want); d < -2*time.Minute || d > 2*time.Minute {
		t.Fatalf("due at %s, want %s (a served hold must not be charged again)",
			route.DueAt.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

// A pause states an absolute hold, so replaying it lands on exactly the same
// row: the start of a hold that is still live is kept, because the lead has
// been held continuously since then and that is what the remaining wait is
// measured against.
func TestLiveLeadHoldPauseIsIdempotent(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 1)
	repo := holdRepo(t, f)
	ctx := context.Background()

	until := time.Now().Add(72 * time.Hour).Truncate(time.Second)
	first, err := repo.HoldLead(ctx, f.campaign, f.leads[0], &until, "On holiday", "manual")
	if err != nil || first == nil {
		t.Fatalf("HoldLead = %v, %v", first, err)
	}
	time.Sleep(20 * time.Millisecond)
	again, err := repo.HoldLead(ctx, f.campaign, f.leads[0], &until, "On holiday", "manual")
	if err != nil || again == nil {
		t.Fatalf("replayed HoldLead = %v, %v", again, err)
	}
	if !again.Since.Equal(first.Since) {
		t.Fatalf("replay moved the hold's start from %s to %s; the write is not idempotent",
			first.Since.Format(time.RFC3339Nano), again.Since.Format(time.RFC3339Nano))
	}
	if again.Until == nil || !again.Until.Equal(*first.Until) {
		t.Fatalf("replay moved the hold's end to %v, want %s", again.Until, until)
	}
}

// A campaign whose remaining leads are all held has not finished. An
// open-ended hold reports no next-due moment, so without a count of held leads
// the scheduler reads that as "everything sent" and closes the campaign.
func TestLiveLeadHoldCountsHeldLeadsSoTheCampaignIsNotClosed(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 2)
	repo := holdRepo(t, f)
	ctx := context.Background()

	if n, err := repo.CountHeldLeads(ctx, f.campaign); err != nil || n != 0 {
		t.Fatalf("CountHeldLeads = %d, %v; want 0", n, err)
	}
	if _, err := repo.HoldLead(ctx, f.campaign, f.leads[0], nil, "", "manual"); err != nil {
		t.Fatalf("hold: %v", err)
	}
	expired := time.Now().Add(-time.Minute)
	if _, err := pool.Exec(ctx,
		`UPDATE campaign_leads SET paused_at = NOW() - interval '2 days', paused_until = $3, pause_source = 'manual'
		 WHERE campaign_id = $1 AND contact_id = $2`, f.campaign, f.leads[1], expired); err != nil {
		t.Fatalf("write the expired hold: %v", err)
	}

	pairs, nextDue, _ := f.find(t, nil, 25)
	if len(pairs) != 1 || pairs[0].ContactID != f.leads[1] {
		t.Fatalf("got %d pairs, want only the lead whose hold expired", len(pairs))
	}
	_ = nextDue
	// Only the live hold counts, so a campaign is never parked on a hold that
	// has already lifted.
	if n, err := repo.CountHeldLeads(ctx, f.campaign); err != nil || n != 1 {
		t.Fatalf("CountHeldLeads = %d, %v; want 1 (the open-ended hold only)", n, err)
	}
}

// A held lead sitting in an undecided condition window reports the hold, not
// the window: the window must not keep elapsing while the contact is away, and
// the drawer has to name the reason the member can act on.
func TestLiveLeadHoldOutranksAnOpenConditionWindow(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 1)
	repo := holdRepo(t, f)
	ctx := context.Background()

	second := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
	        body_plain, body_html, wait_after, position, kind)
	    VALUES ($1, $2, $3, 'Step 2', 'Hi', 'Hello', '<p>Hello</p>', 0, 1, 'email')`,
		second, f.campaign, f.org); err != nil {
		t.Fatalf("second step: %v", err)
	}
	// "if they opened within 3 days" is undecided until the window closes.
	if _, err := pool.Exec(ctx, `UPDATE sequences SET conditions = $2 WHERE id = $1`, f.step,
		`{"branches":[{"conditions":[{"field":"opened","operator":"within_days","value":3}],"target_step_id":"`+second.String()+`"}]}`); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at, dispatched_at)
		 VALUES ($1, $2, $3, NOW() - interval '1 hour', NOW() - interval '1 hour')`,
		f.campaign, f.leads[0], f.step); err != nil {
		t.Fatalf("stamp step 1: %v", err)
	}
	if _, err := repo.HoldLead(ctx, f.campaign, f.leads[0], nil, "away", "manual"); err != nil {
		t.Fatalf("hold: %v", err)
	}

	route, err := repo.RouteContact(ctx, f.campaign, f.leads[0])
	if err != nil {
		t.Fatalf("RouteContact: %v", err)
	}
	if route.Hold == nil {
		t.Fatal("a held lead inside a condition window reported no hold")
	}
	pairs, nextDue, _ := f.find(t, nil, 25)
	if len(pairs) != 0 {
		t.Fatalf("got %d pairs for a lead held with no end", len(pairs))
	}
	if nextDue != nil {
		t.Fatalf("an open-ended hold reported next_due=%v; nothing will happen at that moment", nextDue)
	}
}

// The Leads list has to SAY a lead is held, or a held lead reads as queued and
// nobody knows why nothing is going out. The row status, the scope-chip counts
// and the status filter are three separate derivations of the same fact, so
// all three are asserted together.
func TestLiveLeadHoldReadsAsPausedInTheLeadsList(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	repo := NewContactRepostory(handle)
	progress := NewCampaignProgressRepository(pool)
	ctx := context.Background()

	free := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name, company, phone, custom_fields, updated_at, created_at)
	      VALUES ($1, $2, $3, $4, 'Ada', 'Ng', '', '', '{}'::jsonb, NOW(), NOW())`,
		free, f.owner, f.org, "hold-"+free.String()[:8]+"@test.local"); err != nil {
		t.Fatalf("insert the second contact: %v", err)
	}
	for _, c := range []uuid.UUID{f.contact, free} {
		if _, err := pool.Exec(ctx, `INSERT INTO campaign_leads (campaign_id, contact_id) VALUES ($1, $2)`,
			f.campaign, c); err != nil {
			t.Fatalf("add lead: %v", err)
		}
	}
	until := time.Now().Add(72 * time.Hour).Truncate(time.Second)
	if hold, herr := progress.HoldLead(ctx, f.campaign, f.contact, &until, "back 15 Sep 2026", "out_of_office"); herr != nil || hold == nil {
		t.Fatalf("HoldLead = %v, %v", hold, herr)
	}

	res, xerr := repo.Search(ctx, f.org.String(), nil, nil, models.SearchContacts{
		CampaignIDs: []string{f.campaign.String()},
	}, 25)
	if xerr != nil {
		t.Fatalf("search: %v", xerr)
	}
	byID := map[uuid.UUID]*models.ContactCampaignProgress{}
	for i := range res.Data {
		byID[res.Data[i].ID] = res.Data[i].CampaignLead
	}
	lead := byID[f.contact]
	if lead == nil || lead.Status != models.LeadStatusPaused {
		t.Fatalf("the held lead reads %+v, want status %q", lead, models.LeadStatusPaused)
	}
	if lead.Hold == nil || lead.Hold.Source != "out_of_office" || lead.Hold.Reason != "back 15 Sep 2026" {
		t.Fatalf("the held lead carries hold %+v, want the out-of-office hold and its reason", lead.Hold)
	}
	if lead.Hold.Until == nil || lead.Hold.Until.Sub(until).Abs() > 2*time.Second {
		t.Fatalf("hold until = %v, want %s", lead.Hold.Until, until)
	}
	if other := byID[free]; other == nil || other.Status != models.LeadStatusPending || other.Hold != nil {
		t.Fatalf("the lead nobody held reads %+v, want a plain queued lead", other)
	}

	// The hold and the sending mailbox come from one campaign_leads row and are
	// read in one subquery, so the hold's arrival must not cost the sender.
	mailbox := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO email_accounts (id, user_id, organization_id, email, name,
	        signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone)
	    VALUES ($1, $2, $3, $4, 'Hold', '', '', 'smtp_imap', 'active', 50, 0, 'UTC')`,
		mailbox, f.owner, f.org, "hold-mb-"+mailbox.String()[:8]+"@test.local"); err != nil {
		t.Fatalf("create a mailbox: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM email_accounts WHERE id = $1`, mailbox)
	})
	if _, err := pool.Exec(ctx,
		`UPDATE campaign_leads SET email_account_id = $3, sender_assigned_at = NOW()
		 WHERE campaign_id = $1 AND contact_id = $2`, f.campaign, f.contact, mailbox); err != nil {
		t.Fatalf("bind the sender: %v", err)
	}
	bound, xerr := repo.Search(ctx, f.org.String(), nil, nil, models.SearchContacts{
		CampaignIDs: []string{f.campaign.String()},
	}, 25)
	if xerr != nil {
		t.Fatalf("search: %v", xerr)
	}
	for i := range bound.Data {
		if bound.Data[i].ID != f.contact {
			continue
		}
		lead := bound.Data[i].CampaignLead
		if lead == nil || lead.Sender == "" {
			t.Fatalf("the held lead lost its sender: %+v", lead)
		}
		if lead.Hold == nil {
			t.Fatalf("the bound lead lost its hold: %+v", lead)
		}
	}

	counts, xerr := repo.CampaignLeadCounts(ctx, f.org.String(), f.campaign.String())
	if xerr != nil {
		t.Fatalf("lead counts: %v", xerr)
	}
	if counts.Paused != 1 || counts.Queued != 1 || counts.Total != 2 {
		t.Fatalf("counts paused/queued/total = %d/%d/%d, want 1/1/2",
			counts.Paused, counts.Queued, counts.Total)
	}

	filtered, xerr := repo.Search(ctx, f.org.String(), nil, nil, models.SearchContacts{
		CampaignIDs: []string{f.campaign.String()},
		LeadStatus:  models.LeadStatusPaused,
	}, 25)
	if xerr != nil {
		t.Fatalf("filtered search: %v", xerr)
	}
	if len(filtered.Data) != 1 || filtered.Data[0].ID != f.contact {
		t.Fatalf("the paused filter returned %d rows, want just the held lead", len(filtered.Data))
	}

	// The hold stops counting the moment it expires, with nothing having
	// written: the list, the counts and the filter all have to agree on that.
	if _, err := pool.Exec(ctx,
		`UPDATE campaign_leads SET paused_until = NOW() - interval '1 minute'
		 WHERE campaign_id = $1 AND contact_id = $2`, f.campaign, f.contact); err != nil {
		t.Fatalf("expire the hold: %v", err)
	}
	counts, _ = repo.CampaignLeadCounts(ctx, f.org.String(), f.campaign.String())
	if counts.Paused != 0 || counts.Queued != 2 {
		t.Fatalf("after the hold expired: paused=%d queued=%d, want 0/2", counts.Paused, counts.Queued)
	}
	res, _ = repo.Search(ctx, f.org.String(), nil, nil, models.SearchContacts{
		CampaignIDs: []string{f.campaign.String()},
	}, 25)
	for i := range res.Data {
		if res.Data[i].ID == f.contact && res.Data[i].CampaignLead.Status != models.LeadStatusPending {
			t.Fatalf("an expired hold still reads %q", res.Data[i].CampaignLead.Status)
		}
	}
}

// The reservation is the only atomic gate on a send, so it has to refuse a
// held lead itself: routing and the pre-send check are both reads taken before
// the transaction, and a pause that lands in between would otherwise be raced.
func TestLiveLeadHoldIsRefusedByTheReservation(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 1)
	repo := holdRepo(t, f)
	ctx := context.Background()

	until := time.Now().Add(48 * time.Hour)
	if hold, err := repo.HoldLead(ctx, f.campaign, f.leads[0], &until, "away", "manual"); err != nil || hold == nil {
		t.Fatalf("HoldLead = %v, %v", hold, err)
	}
	ok, err := repo.ReserveSend(ctx, f.campaign, f.leads[0], f.step, uuid.New(), f.mailbox, true)
	if err != nil {
		t.Fatalf("ReserveSend: %v", err)
	}
	if ok {
		t.Fatal("the send was reserved for a lead whose flow is held")
	}
	// Nothing was claimed and nothing was counted, so the step is untouched.
	var rows int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM campaign_contact_progress WHERE campaign_id = $1 AND contact_id = $2`,
		f.campaign, f.leads[0]).Scan(&rows); err != nil {
		t.Fatalf("read progress: %v", err)
	}
	if rows != 0 {
		t.Fatalf("%d progress rows written for a refused reservation", rows)
	}

	// Lifting the hold makes the very same claim succeed.
	if _, err := repo.ResumeLead(ctx, f.campaign, f.leads[0]); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if ok, err := repo.ReserveSend(ctx, f.campaign, f.leads[0], f.step, uuid.New(), f.mailbox, true); err != nil || !ok {
		t.Fatalf("ReserveSend after the resume = %v, %v; want the claim to succeed", ok, err)
	}
}

// A contact that is no longer a lead of the campaign has nothing to reserve.
func TestLiveReserveSendRefusesAContactThatIsNotALead(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 1)
	repo := holdRepo(t, f)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `DELETE FROM campaign_leads WHERE campaign_id = $1 AND contact_id = $2`,
		f.campaign, f.leads[0]); err != nil {
		t.Fatalf("drop the lead: %v", err)
	}
	if ok, err := repo.ReserveSend(ctx, f.campaign, f.leads[0], f.step, uuid.New(), f.mailbox, true); err != nil || ok {
		t.Fatalf("ReserveSend for a non-lead = %v, %v; want it refused", ok, err)
	}
}

// secondCampaign adds another active campaign in the same organization, with
// its own entry step, carrying the given contacts as leads. status is the
// campaign's own, so a completed one can be asserted on too.
func secondCampaign(t *testing.T, f *routedPairsFixture, status string, leads ...uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	campaign, step := uuid.New(), uuid.New()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("second campaign %q: %v", sql[:min(70, len(sql))], err)
		}
	}
	exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, status,
	          daily_limit, timezone, days, start_time, end_time, rotation_mode, updated_at, created_at)
	      VALUES ($1, $2, $3, 'Second', '', $4::campaign_status, 50, 'UTC', 127, '00:00', '23:59',
	              'least_recently_used', NOW(), NOW())`, campaign, f.owner, f.org, status)
	exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
	          body_plain, body_html, wait_after, position, kind)
	      VALUES ($1, $2, $3, 'Step 1', 'Hi', 'Hello', '<p>Hello</p>', 0, 0, 'email')`,
		step, campaign, f.org)
	for i, lead := range leads {
		exec(`INSERT INTO campaign_leads (campaign_id, contact_id, position) VALUES ($1, $2, $3)`,
			campaign, lead, i)
	}
	t.Cleanup(func() {
		c := context.Background()
		for _, sql := range []string{
			`DELETE FROM campaign_contact_progress WHERE campaign_id = $1`,
			`DELETE FROM campaign_leads WHERE campaign_id = $1`,
			`DELETE FROM sequences WHERE campaign_id = $1`,
			`DELETE FROM campaigns WHERE id = $1`,
		} {
			if _, err := f.pool.Exec(c, sql, campaign); err != nil {
				t.Errorf("cleanup %q: %v", sql, err)
			}
		}
	})
	return campaign
}

// Being away is a property of the person, not of one sequence. A contact in
// two live campaigns used to have the campaign the reply was attributed to
// held while the other kept writing to the same empty desk (issue #518).
func TestLiveLeadHoldEverywhereCoversEveryCampaignTheContactIsIn(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 2)
	repo := holdRepo(t, f)
	ctx := context.Background()
	other := secondCampaign(t, f, "active", f.leads[0])

	until := time.Now().Add(72 * time.Hour).Truncate(time.Second)
	held, err := repo.HoldLeadEverywhere(ctx, f.leads[0], &until, "back 17 Sep 2026", "out_of_office")
	if err != nil {
		t.Fatalf("HoldLeadEverywhere: %v", err)
	}
	if len(held) != 2 {
		t.Fatalf("held %d campaigns (%v), want both the contact is a lead of", len(held), held)
	}
	for _, campaign := range []uuid.UUID{f.campaign, other} {
		hold, err := repo.GetLeadHold(ctx, campaign, f.leads[0])
		if err != nil || hold == nil {
			t.Fatalf("campaign %s: GetLeadHold = %v, %v; want the hold", campaign, hold, err)
		}
		if hold.Source != "out_of_office" || hold.Until == nil {
			t.Fatalf("campaign %s: hold = %+v, want a dated automatic hold", campaign, hold)
		}
	}

	// The contact's own lead is gone from the batch; the lead who is not away
	// is untouched, so the hold is per person and not per organization.
	pairs, _, _ := f.find(t, nil, 25)
	if len(pairs) != 1 || pairs[0].ContactID != f.leads[1] {
		t.Fatalf("got %+v, want only the lead that is not held", pairs)
	}
}

// An automatic hold never overrules a person, and that has to hold campaign by
// campaign: a member's own pause in one of them must survive an away message
// that legitimately holds the others.
func TestLiveLeadHoldEverywhereLeavesAPersonsOwnPauseAlone(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 1)
	repo := holdRepo(t, f)
	ctx := context.Background()
	other := secondCampaign(t, f, "active", f.leads[0])

	if _, err := repo.HoldLead(ctx, other, f.leads[0], nil, "paused by hand", "manual"); err != nil {
		t.Fatalf("manual hold: %v", err)
	}

	until := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	held, err := repo.HoldLeadEverywhere(ctx, f.leads[0], &until, "back 16 Sep 2026", "out_of_office")
	if err != nil {
		t.Fatalf("HoldLeadEverywhere: %v", err)
	}
	if len(held) != 1 || held[0] != f.campaign {
		t.Fatalf("held %v, want only the campaign with no manual pause", held)
	}
	hold, err := repo.GetLeadHold(ctx, other, f.leads[0])
	if err != nil || hold == nil {
		t.Fatalf("GetLeadHold = %v, %v; want the manual hold still there", hold, err)
	}
	if hold.Source != "manual" || hold.Until != nil {
		t.Fatalf("manual hold became %+v: an auto-reply overruled a person", hold)
	}
}

// A completed campaign will never send again, so holding its leads writes rows
// that decide nothing. Keeping it out is also what stops a contact's whole
// campaign history being rewritten by one away message.
func TestLiveLeadHoldEverywhereSkipsACompletedCampaign(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 1)
	repo := holdRepo(t, f)
	ctx := context.Background()
	done := secondCampaign(t, f, "completed", f.leads[0])

	until := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	held, err := repo.HoldLeadEverywhere(ctx, f.leads[0], &until, "back tomorrow", "out_of_office")
	if err != nil {
		t.Fatalf("HoldLeadEverywhere: %v", err)
	}
	if len(held) != 1 || held[0] != f.campaign {
		t.Fatalf("held %v, want only the campaign that can still send", held)
	}
	if hold, err := repo.GetLeadHold(ctx, done, f.leads[0]); err != nil || hold != nil {
		t.Fatalf("GetLeadHold on the completed campaign = %v, %v; want no hold", hold, err)
	}
}

// A second away message, naming a nearer return date, must not cut short the
// hold already running. The guard decides that per row, so it has to survive
// the move to the multi-campaign write.
func TestLiveLeadHoldEverywhereNeverShortensALiveAutomaticHold(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 1)
	repo := holdRepo(t, f)
	ctx := context.Background()

	far := time.Now().Add(14 * 24 * time.Hour).Truncate(time.Second)
	if _, err := repo.HoldLeadEverywhere(ctx, f.leads[0], &far, "back in a fortnight", "out_of_office"); err != nil {
		t.Fatalf("first hold: %v", err)
	}
	near := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	held, err := repo.HoldLeadEverywhere(ctx, f.leads[0], &near, "back tomorrow", "out_of_office")
	if err != nil {
		t.Fatalf("second hold: %v", err)
	}
	if len(held) != 0 {
		t.Fatalf("held %v; a nearer return date cut the running hold short", held)
	}
	hold, err := repo.GetLeadHold(ctx, f.campaign, f.leads[0])
	if err != nil || hold == nil || hold.Until == nil {
		t.Fatalf("GetLeadHold = %v, %v; want the first hold intact", hold, err)
	}
	if d := hold.Until.Sub(far); d < -2*time.Second || d > 2*time.Second {
		t.Fatalf("hold lifts at %v, want the further date %s", hold.Until, far)
	}
}
