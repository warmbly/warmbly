package scheduler

import (
	"testing"
	"time"

	"github.com/warmbly/warmbly/internal/app/warmupramp"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func flat(n int) func(int) int { return func(int) int { return n } }

func TestSimulateLeadsSingleStep(t *testing.T) {
	sim := simulateLeads(flat(40), 100, nil, 30)
	if sim.finishDay != 2 || sim.firstTouchDay != 2 || sim.sendingDays != 3 {
		t.Fatalf("100 at 40/day: finish %d, first touch %d, days %d", sim.finishDay, sim.firstTouchDay, sim.sendingDays)
	}
	if got := sim.days[2].first; got != 20 {
		t.Fatalf("last day sends %d, want 20", got)
	}
}

func TestFullCapacityDateDoesNotProjectMissingFeedbackIntoReadiness(t *testing.T) {
	warmed := time.Now().Add(-90 * 24 * time.Hour)
	ramp := time.Now().Add(-30 * 24 * time.Hour)
	p := &projectedSender{cap: 50, hasCold: true, cold: repository.ColdRampState{WarmupStartedAt: &warmed, ColdRampStartedAt: &ramp}}
	if fullCapacityEvidenceKnown([]*projectedSender{p}) {
		t.Fatal("synthetic age projected full-cap readiness")
	}
	p.cold.ConfirmedReplies = 100
	if !fullCapacityEvidenceKnown([]*projectedSender{p}) {
		t.Fatal("current already-full evidence lost")
	}
}

func TestSimulateLeadsFollowUpsFirst(t *testing.T) {
	// Two steps three days apart: day 3's capacity goes to day 0's follow-ups
	// before any new contact.
	sim := simulateLeads(flat(10), 50, []int{3}, 60)
	if d := sim.days[3]; d.followUps != 10 || d.first != 0 {
		t.Fatalf("day 3: %+v, want 10 follow-ups and no first emails", d)
	}
	total := 0
	for _, d := range sim.days {
		total += d.first + d.followUps
	}
	if total != 100 {
		t.Fatalf("sent %d, want 100", total)
	}
	if sim.finishDay < 0 || sim.firstTouchDay >= sim.finishDay {
		t.Fatalf("first touch %d must land before finish %d", sim.firstTouchDay, sim.finishDay)
	}
}

func TestSimulateLeadsZeroWaitSameDay(t *testing.T) {
	sim := simulateLeads(flat(10), 5, []int{0}, 10)
	if sim.finishDay != 0 || sim.days[0].followUps != 5 {
		t.Fatalf("a zero wait must go out the same day: %+v finish %d", sim.days[0], sim.finishDay)
	}
}

func TestSimulateLeadsNoCapacityNeverFinishes(t *testing.T) {
	sim := simulateLeads(flat(0), 5, nil, 20)
	if sim.finishDay != -1 || sim.firstTouchDay != -1 {
		t.Fatalf("no capacity must not finish: %+v", sim)
	}
}

func TestSimulateLeadsDoesNotMutateWaits(t *testing.T) {
	waits := []int{-2, 3}
	simulateLeads(flat(5), 5, waits, 10)
	if waits[0] != -2 {
		t.Fatal("the caller's waits were rewritten")
	}
}

func workday(loc *time.Location, day time.Time) (time.Time, []span) {
	mid := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	return mid, []span{{mid.Add(8 * time.Hour), mid.Add(18 * time.Hour)}}
}

func TestProjectSenderDayGraduationRequiresConfirmedReplies(t *testing.T) {
	loc := time.UTC
	start := time.Date(2026, 3, 2, 0, 0, 0, 0, loc)
	warmed := start.AddDate(0, 0, -20)
	for _, tc := range []struct {
		name    string
		replies int
		caps    [3]int
	}{
		{"synthetic age without replies", 0, [3]int{5, 5, 5}},
		{"growth bounded by confirmed replies", 8, [3]int{5, 13, 13}},
		{"confirmed replies permit time-bounded growth", 100, [3]int{5, 15, 50}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &projectedSender{
				acct: models.Email{CampaignLimit: 50, MinWaitTime: 60},
				cap:  50, loc: loc, hasCold: true,
				cold: repository.ColdRampState{WarmupStartedAt: &warmed, ConfirmedReplies: tc.replies},
			}
			for i, day := range []int{0, 2, 10} {
				mid, spans := workday(loc, start.AddDate(0, 0, day))
				if d := projectSenderDay(p, spans, mid, start, false); d.sends != tc.caps[i] || d.graduation != 50-tc.caps[i] {
					t.Fatalf("day %d: %+v, want %d sends and %d withheld", day, d, tc.caps[i], 50-tc.caps[i])
				}
			}
		})
	}
}

func TestProjectSenderDayWarmupTakesSpacingSlots(t *testing.T) {
	loc := time.UTC
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, loc)
	began := day.AddDate(0, 0, -30)
	p := &projectedSender{
		acct: models.Email{
			CampaignLimit: 100, MinWaitTime: 1200, Warmup: &began,
			WarmupBase: 10, WarmupIncrease: 1, WarmupMax: 40,
			WarmupStartTime: "08:00", WarmupEndTime: "18:00",
		},
		cap: 100, loc: loc,
	}
	mid, spans := workday(loc, day)
	d := projectSenderDay(p, spans, mid, day, false)
	// Ten hours at a 20 minute gap is 31 slots; the 5 warmup emails a
	// mailbox in a live campaign keeps sending take 5 of them.
	if d.warmup != warmupramp.ActiveCampaignCap || d.sends != 31-warmupramp.ActiveCampaignCap {
		t.Fatalf("got %+v, want %d warmup and %d sends", d, warmupramp.ActiveCampaignCap, 31-warmupramp.ActiveCampaignCap)
	}
}

func TestProjectSenderDayOtherCampaignsShareTheCap(t *testing.T) {
	loc := time.UTC
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, loc)
	p := &projectedSender{acct: models.Email{CampaignLimit: 50, MinWaitTime: 60}, cap: 50, loc: loc, other: 20}
	mid, spans := workday(loc, day)
	if d := projectSenderDay(p, spans, mid, day, false); d.sends != 30 || d.other != 20 {
		t.Fatalf("got %+v, want 30 sends and 20 taken by other campaigns", d)
	}
}

func TestProjectSenderDayHealthHoldLapses(t *testing.T) {
	loc := time.UTC
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, loc)
	until := day.Add(13 * time.Hour)
	p := &projectedSender{acct: models.Email{CampaignLimit: 50, MinWaitTime: 600}, cap: 50, loc: loc, held: true, heldTill: &until}
	mid, spans := workday(loc, day)
	// Five hours left after the hold at a 10 minute gap: 31 slots.
	if d := projectSenderDay(p, spans, mid, day, false); d.sends != 31 {
		t.Fatalf("got %d sends after the hold lapsed, want 31", d.sends)
	}
	p.heldTill = nil
	if d := projectSenderDay(p, spans, mid, day, false); d.sends != 0 || d.held != 50 {
		t.Fatalf("an open-ended hold sends nothing: %+v", d)
	}
}
