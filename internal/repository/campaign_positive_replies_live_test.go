package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// Issue #859: campaign analytics count positive replies and interested leads
// off the stored reply classification, on the same send cohort as replies.
//
//	WARMBLY_TEST_DB=postgres://warmbly:warmbly@localhost:15432/<db>?sslmode=disable \
//	  go test ./internal/repository/ -run LiveCampaignPositiveReplies -v
func TestLiveCampaignPositiveReplies(t *testing.T) {
	handle, pool := liveContactDB(t)
	f := newSharedOrgFixture(t, pool)
	ctx := context.Background()

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("seed %q: %v", sql[:min(60, len(sql))], err)
		}
	}
	intro, follow := uuid.New(), uuid.New()
	for i, step := range []uuid.UUID{intro, follow} {
		exec(`INSERT INTO sequences (id, campaign_id, organization_id, name, subject, body_plain, body_html, wait_after, position, kind)
		      VALUES ($1, $2, $3, $4, 'Hi', 'Hello', '<p>Hello</p>', 0, $5, 'email')`,
			step, f.campaign, f.org, []string{"Intro", "Follow-up"}[i], i+1)
	}

	day := func(d, h int) time.Time { return time.Date(2026, time.September, d, h, 0, 0, 0, time.UTC) }
	period := &models.DateRange{From: day(1, 0), To: day(7, 0)}
	lead := func(tag string) uuid.UUID {
		return addLead(t, f, "i859-"+tag+"-"+uuid.New().String()[:6]+"@test.local", "valid", true)
	}
	progress := func(step, contact uuid.UUID, sent time.Time, replied *time.Time, class string) {
		t.Helper()
		exec(`INSERT INTO campaign_contact_progress (campaign_id, contact_id, sequence_id, sent_at, replied_at, reply_class)
		      VALUES ($1, $2, $3, $4, $5, $6)`, f.campaign, contact, step, sent, replied, class)
	}
	at := func(t time.Time) *time.Time { return &t }

	// Positive on both steps: two positive replies, one interested lead. The
	// intro's reply lands after the period and still counts.
	keen := lead("keen")
	progress(intro, keen, day(2, 9), at(day(10, 12)), "positive")
	progress(follow, keen, day(5, 9), at(day(5, 15)), "positive")
	// A second interested lead on the intro only.
	other := lead("other")
	progress(intro, other, day(3, 9), at(day(3, 11)), "positive")
	// Human replies that are not positive.
	progress(intro, lead("neg"), day(3, 9), at(day(3, 10)), "negative")
	progress(intro, lead("neutral"), day(3, 9), at(day(3, 10)), "neutral")
	progress(intro, lead("unsub"), day(3, 9), at(day(3, 10)), "unsubscribe")
	progress(intro, lead("unknown"), day(3, 9), at(day(3, 10)), "unknown")
	progress(intro, lead("legacy"), day(3, 9), at(day(3, 10)), "")
	// Automated answers never stamp replied_at and are never replies.
	progress(intro, lead("ooo"), day(4, 9), nil, "out_of_office")
	progress(intro, lead("auto"), day(4, 9), nil, "auto_reply")
	// Classified positive but never accepted as a human reply.
	progress(follow, lead("unaccepted"), day(4, 9), nil, "positive")
	// A positive reply inside the period to a send before it.
	progress(intro, lead("old"), day(1, 0).Add(-time.Second), at(day(3, 10)), "positive")

	repo := &analyticsRepository{DB: handle}

	sum, xerr := repo.GetCampaignSummary(ctx, f.org, f.campaign, period)
	if xerr != nil {
		t.Fatalf("period summary: %v", xerr)
	}
	if sum.EmailsSent != 11 || sum.Replies != 8 {
		t.Fatalf("period sent/replies = %d/%d, want 11/8", sum.EmailsSent, sum.Replies)
	}
	if sum.PositiveReplies != 3 || sum.InterestedLeads != 2 {
		t.Errorf("period positive/interested = %d/%d, want 3/2 (the old send's reply is not the period's)",
			sum.PositiveReplies, sum.InterestedLeads)
	}
	if want := models.Rate(3, 11); sum.PositiveReplyRate < want-0.01 || sum.PositiveReplyRate > want+0.01 {
		t.Errorf("positive reply rate = %.2f, want %.2f of the period's sends", sum.PositiveReplyRate, want)
	}
	b := sum.ReplyBreakdown
	if b.Positive != 3 || b.Negative != 1 || b.Neutral != 1 || b.Unsubscribe != 1 || b.Unclassified != 2 ||
		b.OutOfOffice != 1 || b.AutoReply != 1 {
		t.Errorf("breakdown = %+v, want 3 positive, 1 each of negative/neutral/unsubscribe/ooo/auto, 2 unclassified", b)
	}
	if human := b.Positive + b.Neutral + b.Negative + b.Unsubscribe + b.Unclassified; human != sum.Replies {
		t.Errorf("human breakdown adds up to %d, replies are %d", human, sum.Replies)
	}

	all, xerr := repo.GetCampaignSummary(ctx, f.org, f.campaign, nil)
	if xerr != nil {
		t.Fatalf("all-time summary: %v", xerr)
	}
	if all.PositiveReplies != 4 || all.InterestedLeads != 3 {
		t.Errorf("all-time positive/interested = %d/%d, want 4/3", all.PositiveReplies, all.InterestedLeads)
	}

	steps, xerr := repo.GetSequenceStats(ctx, f.campaign, period)
	if xerr != nil {
		t.Fatalf("period steps: %v", xerr)
	}
	if len(steps) != 2 {
		t.Fatalf("got %d steps, want 2", len(steps))
	}
	if s := steps[0]; s.PositiveReplies != 2 || s.EmailsSent != 9 {
		t.Errorf("intro = %d positive of %d sent, want 2 of 9", s.PositiveReplies, s.EmailsSent)
	}
	if s := steps[1]; s.PositiveReplies != 1 || s.PositiveReplyRate != 50 {
		t.Errorf("follow-up = %d positive at %.1f%%, want 1 at 50%%", s.PositiveReplies, s.PositiveReplyRate)
	}
	var stepPositive int
	for _, s := range steps {
		stepPositive += s.PositiveReplies
	}
	if stepPositive != sum.PositiveReplies {
		t.Errorf("steps add up to %d positive, the summary says %d", stepPositive, sum.PositiveReplies)
	}

	daily, xerr := repo.GetCampaignDailyStats(ctx, f.campaign, period.From, period.To)
	if xerr != nil {
		t.Fatalf("daily: %v", xerr)
	}
	var dailyPositive int
	for _, d := range daily {
		dailyPositive += d.PositiveReplies
	}
	if dailyPositive != sum.PositiveReplies {
		t.Errorf("daily adds up to %d positive, the summary says %d", dailyPositive, sum.PositiveReplies)
	}
}
