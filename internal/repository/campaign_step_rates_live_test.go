package repository

import (
	"context"
	"math"
	"testing"

	"github.com/google/uuid"
)

// Issue #434: Step performance showed counts only, so a first touch that
// reached 337 contacts and a fourth that reached 11 could not be compared.
// GetSequenceStats now returns each step's own rates, against that step's
// sends, plus the automated share of its opens and clicks.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/warmbly_dev?sslmode=disable \
//	  go test ./internal/repository/ -run LiveCampaignStepRates -v

func TestLiveCampaignStepRatesAreOfTheStepsOwnSends(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()

	// Two steps, created in order so position 1 is the intro.
	intro, follow := uuid.New(), uuid.New()
	for i, step := range []uuid.UUID{intro, follow} {
		if _, err := pool.Exec(ctx, `INSERT INTO sequences (id, campaign_id, organization_id, name, subject,
		          body_plain, body_html, wait_after, position, kind, created_at)
		      VALUES ($1, $2, $3, $4, 'Hi', 'Hello', '<p>Hello</p>', 0, $5, 'email', NOW() + make_interval(secs => $6))`,
			step, f.campaign, f.org, []string{"Intro", "Follow-up"}[i], i+1, float64(i)); err != nil {
			t.Fatalf("sequence %d: %v", i, err)
		}
	}

	lead := func(tag string) uuid.UUID {
		return addLead(t, f, "i434-"+tag+"-"+uuid.New().String()[:6]+"@test.local", "valid", true)
	}
	progress := func(step uuid.UUID, contact uuid.UUID, cols, vals string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at`+cols+`)
		      VALUES ($1, $2, $3, NOW() - INTERVAL '1 hour'`+vals+`)`, f.campaign, contact, step); err != nil {
			t.Fatalf("progress %q: %v", cols, err)
		}
	}

	// Intro: four sent. One human open, one machine open, one human click
	// (which also opened), one nothing. Of those, one replied and one bounced.
	humanOpen, machineOpen := lead("open"), lead("mpp")
	clicker, quiet := lead("click"), lead("quiet")
	progress(intro, humanOpen, `, opened_at, replied_at`, `, NOW(), NOW()`)
	progress(intro, machineOpen, `, opened_at, opened_machine`, `, NOW(), true`)
	progress(intro, clicker, `, opened_at, clicked_at`, `, NOW(), NOW()`)
	progress(intro, quiet, `, bounced_at`, `, NOW()`)

	// A gateway walked this contact's links and nobody followed: the step
	// counts no click, and the automated one is reported separately.
	scanned := lead("scan")
	progress(intro, scanned, ``, ``)
	if _, err := pool.Exec(ctx, `INSERT INTO email_link_clicks (task_id, campaign_id, contact_id, sequence_id, destination, machine, machine_reason)
	      VALUES (gen_random_uuid(), $1, $2, $3, 'https://example.com/pricing', true, 'instant')`,
		f.campaign, scanned, intro); err != nil {
		t.Fatalf("machine click: %v", err)
	}

	// Follow-up: queued for one contact, sent to nobody yet.
	progress2 := lead("queued")
	if _, err := pool.Exec(ctx, `INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id)
	      VALUES ($1, $2, $3)`, f.campaign, progress2, follow); err != nil {
		t.Fatalf("queued progress: %v", err)
	}

	repo := &analyticsRepository{DB: handle}
	stats, xerr := repo.GetSequenceStats(ctx, f.campaign)
	if xerr != nil {
		t.Fatalf("GetSequenceStats: %v", xerr)
	}
	if len(stats) != 2 {
		t.Fatalf("got %d steps, want 2", len(stats))
	}

	near := func(got, want float64) bool { return math.Abs(got-want) < 0.01 }

	// Five sent: three opens (the machine one included, as the summary
	// counts it), one click, one reply, one bounce.
	s := stats[0]
	if s.SequenceID != intro || s.Position != 1 {
		t.Fatalf("first row = %s position %d, want the intro step at position 1", s.SequenceID, s.Position)
	}
	if s.EmailsSent != 5 || s.Opens != 3 || s.MachineOpens != 1 || s.Clicks != 1 || s.MachineClicks != 1 || s.Replies != 1 || s.Bounces != 1 {
		t.Errorf("intro counts = sent %d opens %d machine_opens %d clicks %d machine_clicks %d replies %d bounces %d, want 5/3/1/1/1/1/1",
			s.EmailsSent, s.Opens, s.MachineOpens, s.Clicks, s.MachineClicks, s.Replies, s.Bounces)
	}
	if !near(s.OpenRate, 60) || !near(s.ClickRate, 20) || !near(s.ReplyRate, 20) || !near(s.BounceRate, 20) {
		t.Errorf("intro rates = open %.2f click %.2f reply %.2f bounce %.2f, want 60/20/20/20",
			s.OpenRate, s.ClickRate, s.ReplyRate, s.BounceRate)
	}

	// A step that has sent nothing divides by zero; every rate stays 0.
	q := stats[1]
	if q.SequenceID != follow || q.Position != 2 {
		t.Fatalf("second row = %s position %d, want the follow-up at position 2", q.SequenceID, q.Position)
	}
	if q.EmailsSent != 0 || q.Opens != 0 || q.MachineClicks != 0 {
		t.Errorf("follow-up counts = sent %d opens %d machine_clicks %d, want all 0", q.EmailsSent, q.Opens, q.MachineClicks)
	}
	if q.OpenRate != 0 || q.ClickRate != 0 || q.ReplyRate != 0 || q.BounceRate != 0 {
		t.Errorf("follow-up rates = open %.2f click %.2f reply %.2f bounce %.2f, want all 0",
			q.OpenRate, q.ClickRate, q.ReplyRate, q.BounceRate)
	}

	// The steps add up to the summary, which shares the automated-click
	// definition with them: one campaign, read two ways, one answer.
	sum, xerr := repo.GetCampaignSummary(ctx, f.owner, f.campaign)
	if xerr != nil {
		t.Fatalf("GetCampaignSummary: %v", xerr)
	}
	var sent, opens, machineOpens, clicks, machineClicks int
	for _, st := range stats {
		sent += st.EmailsSent
		opens += st.Opens
		machineOpens += st.MachineOpens
		clicks += st.Clicks
		machineClicks += st.MachineClicks
	}
	if sum.EmailsSent != sent || sum.UniqueOpens != opens || sum.MachineOpens != machineOpens ||
		sum.UniqueClicks != clicks || sum.MachineClicks != machineClicks {
		t.Errorf("summary = sent %d opens %d machine_opens %d clicks %d machine_clicks %d, steps add up to %d/%d/%d/%d/%d",
			sum.EmailsSent, sum.UniqueOpens, sum.MachineOpens, sum.UniqueClicks, sum.MachineClicks,
			sent, opens, machineOpens, clicks, machineClicks)
	}
	if !near(sum.OpenRate, s.OpenRate) {
		t.Errorf("summary open rate %.2f, want the single step's %.2f", sum.OpenRate, s.OpenRate)
	}
}
