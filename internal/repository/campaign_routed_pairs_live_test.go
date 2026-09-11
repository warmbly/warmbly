package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FindRoutedPairs is the query the campaign chain asks "who do I send next".
// It used to return one pair, and issue #437 widened it to a batch so the
// scheduler can move past a lead placement refuses. That contract — DUE leads
// only, routing order, at most `limit`, and a next-due time ONLY when the batch
// is empty — is hand-written SQL plus a hand-written scan loop, so it is worth
// asserting directly rather than only through the scheduler above it.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveFindRoutedPairs -v

type routedPairsFixture struct {
	pool                          *pgxpool.Pool
	owner, org, mailbox, campaign uuid.UUID
	step                          uuid.UUID
	leads                         []uuid.UUID
}

// newRoutedPairsFixture builds an always-open campaign with one entry step and
// `leads` brand-new leads, ordered by created_at one second apart so routing
// order is unambiguous.
func newRoutedPairsFixture(t *testing.T, pool *pgxpool.Pool, leads int) *routedPairsFixture {
	t.Helper()
	ctx := context.Background()
	f := &routedPairsFixture{
		pool: pool, owner: uuid.New(), org: uuid.New(), mailbox: uuid.New(),
		campaign: uuid.New(), step: uuid.New(),
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("fixture %q: %v", sql[:min(70, len(sql))], err)
		}
	}
	exec(`INSERT INTO users (id, first_name, last_name, email, password_hash)
	      VALUES ($1, 'Routed', 'Pairs', $2, 'x')`, f.owner, "routed-"+f.owner.String()[:8]+"@test.local")
	exec(`INSERT INTO organizations (id, name, slug, owner_user_id) VALUES ($1, 'Routed Pairs', $2, $3)`,
		f.org, "routed-"+f.org.String()[:8], f.owner)
	exec(`INSERT INTO email_accounts (id, user_id, organization_id, email, name,
	          signature_plain, signature_html, provider, status, campaign_limit, min_wait_time, timezone)
	      VALUES ($1, $2, $3, $4, 'Routed', '', '', 'smtp_imap', 'active', 50, 0, 'UTC')`,
		f.mailbox, f.owner, f.org, "routed-mb-"+f.mailbox.String()[:8]+"@test.local")
	exec(`INSERT INTO campaigns (id, user_id, organization_id, name, description, status,
	          daily_limit, timezone, days, start_time, end_time, rotation_mode, updated_at, created_at)
	      VALUES ($1, $2, $3, 'Routed Pairs', '', 'active', 50, 'UTC', 127, '00:00', '23:59',
	              'least_recently_used', NOW(), NOW())`, f.campaign, f.owner, f.org)
	exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
	          body_plain, body_html, wait_after, position, kind)
	      VALUES ($1, $2, $3, 'Step 1', 'Hi', 'Hello', '<p>Hello</p>', 0, 0, 'email')`,
		f.step, f.campaign, f.org)
	for i := 0; i < leads; i++ {
		id := uuid.New()
		f.leads = append(f.leads, id)
		exec(`INSERT INTO contacts (id, user_id, organization_id, email, first_name, last_name,
		          company, phone, custom_fields, verification_status, created_at)
		      VALUES ($1, $2, $3, $4, 'Routed', 'Lead', '', '', '{}', 'valid', NOW() + make_interval(secs => $5))`,
			id, f.owner, f.org, "routed-lead-"+id.String()[:8]+"@test.local", float64(i))
		exec(`INSERT INTO campaign_leads (campaign_id, contact_id, position) VALUES ($1, $2, $3)`,
			f.campaign, id, i)
	}

	t.Cleanup(func() {
		c := context.Background()
		for _, step := range []struct {
			sql string
			arg any
		}{
			{`DELETE FROM campaign_contact_progress WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM campaign_leads WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM sequences WHERE campaign_id = $1`, f.campaign},
			{`DELETE FROM campaigns WHERE id = $1`, f.campaign},
			{`DELETE FROM email_accounts WHERE id = $1`, f.mailbox},
			{`DELETE FROM contacts WHERE organization_id = $1`, f.org},
			{`DELETE FROM organizations WHERE id = $1`, f.org},
			{`DELETE FROM users WHERE id = $1`, f.owner},
		} {
			if _, err := pool.Exec(c, step.sql, step.arg); err != nil {
				t.Errorf("cleanup %q: %v", step.sql, err)
			}
		}
	})
	return f
}

func (f *routedPairsFixture) find(t *testing.T, paced PacedSenders, limit int) ([]ContactSequencePair, *time.Time, bool) {
	t.Helper()
	pairs, nextDue, onSender, err := NewCampaignProgressRepository(f.pool).FindRoutedPairs(
		context.Background(), f.campaign, "created_at", "asc", "", false, false, paced, limit)
	if err != nil {
		t.Fatalf("find routed pairs: %v", err)
	}
	return pairs, nextDue, onSender
}

// The batch is the first `limit` DUE leads in routing order, and a batch that
// found somebody reports no next-due: the caller is sending, not waiting.
func TestLiveFindRoutedPairsReturnsDueLeadsInOrderUpToTheLimit(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 5)

	pairs, nextDue, onSender := f.find(t, nil, 3)
	if len(pairs) != 3 {
		t.Fatalf("got %d pairs, want the limit of 3", len(pairs))
	}
	for i := range pairs {
		if pairs[i].ContactID != f.leads[i] {
			t.Fatalf("pair %d is contact %s, want %s: the batch must keep routing order",
				i, pairs[i].ContactID, f.leads[i])
		}
		if pairs[i].SequenceID != f.step || !pairs[i].IsNewLead {
			t.Fatalf("pair %d = %+v, want the entry step for a new lead", i, pairs[i])
		}
	}
	if nextDue != nil || onSender {
		t.Fatalf("a batch that found leads reported next_due=%v waiting_on_sender=%v", nextDue, onSender)
	}
}

// A limit below one is still a request for work, not for nothing.
func TestLiveFindRoutedPairsLimitBelowOneReturnsOne(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 3)

	for _, limit := range []int{0, -1} {
		pairs, _, _ := f.find(t, nil, limit)
		if len(pairs) != 1 {
			t.Fatalf("limit %d returned %d pairs, want 1", limit, len(pairs))
		}
	}
}

// A limit above the number of due leads returns all of them, and stopping the
// scan early must not lose the ones behind it.
func TestLiveFindRoutedPairsReturnsEveryDueLeadBelowTheLimit(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 4)

	pairs, _, _ := f.find(t, nil, 25)
	if len(pairs) != 4 {
		t.Fatalf("got %d pairs, want all 4 due leads", len(pairs))
	}
}

// Nothing due: no pairs, and the soonest moment something becomes due, so the
// scheduler defers until then instead of calling the campaign complete.
func TestLiveFindRoutedPairsReportsNextDueWhenNothingIsDue(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 3)
	// An entry delay holds every first email back for two hours.
	if _, err := pool.Exec(context.Background(),
		`UPDATE campaigns SET entry_delay_minutes = 120 WHERE id = $1`, f.campaign); err != nil {
		t.Fatalf("set entry delay: %v", err)
	}

	pairs, nextDue, onSender := f.find(t, nil, 25)
	if len(pairs) != 0 {
		t.Fatalf("got %d pairs while every lead was inside the entry delay", len(pairs))
	}
	if nextDue == nil {
		t.Fatal("nothing due and no next-due time: the scheduler would complete the campaign")
	}
	if until := time.Until(*nextDue); until < 110*time.Minute || until > 121*time.Minute {
		t.Fatalf("next due in %s, want about the two-hour entry delay", until.Round(time.Minute))
	}
	if onSender {
		t.Fatal("an entry delay is not a lead waiting for its mailbox")
	}
}

// A lead bound to a mailbox that is merely paced waits for it and is reported
// through the next-due time; the leads behind it are still offered, so one busy
// mailbox never parks the campaign.
func TestLiveFindRoutedPairsSkipsALeadWaitingOnItsPacedMailbox(t *testing.T) {
	_, pool := liveContactDB(t)
	f := newRoutedPairsFixture(t, pool, 2)
	ctx := context.Background()

	// The first lead has heard from the mailbox already, so it is bound to it.
	if _, err := pool.Exec(ctx,
		`UPDATE campaign_leads SET email_account_id = $3, sender_assigned_at = NOW()
		 WHERE campaign_id = $1 AND contact_id = $2`, f.campaign, f.leads[0], f.mailbox); err != nil {
		t.Fatalf("bind lead: %v", err)
	}
	back := time.Now().Add(3 * time.Hour).Truncate(time.Second)

	pairs, nextDue, onSender := f.find(t, PacedSenders{f.mailbox: back}, 25)
	if len(pairs) != 1 {
		t.Fatalf("got %d pairs, want only the unbound lead behind the waiting one", len(pairs))
	}
	if pairs[0].ContactID != f.leads[1] {
		t.Fatalf("offered contact %s, want the unbound lead %s", pairs[0].ContactID, f.leads[1])
	}
	// A due pair is what the caller acts on, so the wait is not reported here.
	if nextDue != nil || onSender {
		t.Fatalf("reported next_due=%v waiting_on_sender=%v alongside a sendable pair", nextDue, onSender)
	}

	// With the free lead gone, the wait is the whole answer.
	if _, err := pool.Exec(ctx, `DELETE FROM campaign_leads WHERE campaign_id = $1 AND contact_id = $2`,
		f.campaign, f.leads[1]); err != nil {
		t.Fatalf("drop the free lead: %v", err)
	}
	pairs, nextDue, onSender = f.find(t, PacedSenders{f.mailbox: back}, 25)
	if len(pairs) != 0 {
		t.Fatalf("got %d pairs while the only lead was waiting for its own mailbox", len(pairs))
	}
	if nextDue == nil || !nextDue.Equal(back) {
		t.Fatalf("next due = %v, want the mailbox's reopening %s", nextDue, back)
	}
	if !onSender {
		t.Fatal("the wait must be reported as a lead waiting for its mailbox, not as a step's delay")
	}
}
