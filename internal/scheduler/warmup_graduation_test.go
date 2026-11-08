package scheduler

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestCampaignWithoutGraduationSendsAtTheMailboxCap(t *testing.T) {
	now := time.Now()
	warmed := now.Add(-60 * 24 * time.Hour)
	acct := models.Email{ID: uuid.New(), CampaignLimit: 50, MinWaitTime: 60, Warmup: &warmed}
	states := map[uuid.UUID]repository.ColdRampState{acct.ID: {WarmupStartedAt: &warmed}}

	on := &campaignPass{campaign: &models.Campaign{DailyLimit: 50, WarmupGraduation: true}, coldRamp: states, risk: models.OrgRiskTrusted}
	if c := on.explainCap(acct); c.Cap != 5 || c.LimitedBy != capByGraduation {
		t.Fatalf("graduating campaign: got %+v, want 5 by %s", c, capByGraduation)
	}
	off := &campaignPass{campaign: &models.Campaign{DailyLimit: 50}, coldRamp: states, risk: models.OrgRiskTrusted,
		health: map[uuid.UUID]healthRead{acct.ID: {}}}
	if c := off.explainCap(acct); c.Cap != 50 || c.LimitedBy != capByMailbox {
		t.Fatalf("campaign without graduation: got %+v, want the mailbox cap", c)
	}
	s := &schedulerService{}
	if plan := s.planMailbox(t.Context(), off, acct, 0, 0, nil, now, 10*3600, now.Add(10*time.Hour)); plan.byGraduation != 0 || plan.graduation != nil {
		t.Fatalf("plan still explains a graduation ceiling: %+v", plan)
	}
	// The campaign's own ramp still applies when graduation is off.
	ramped := &campaignPass{campaign: &models.Campaign{DailyLimit: 50, RampEnabled: true, RampStart: 10, RampIncrement: 5, RampCeiling: 40, RampLevel: 15},
		coldRamp: states, risk: models.OrgRiskTrusted}
	if c := ramped.explainCap(acct); c.Cap != 15 || c.LimitedBy != capByRamp {
		t.Fatalf("ramped campaign: got %+v, want 15 by %s", c, capByRamp)
	}
}

func TestProjectSenderDayAppliesTheCampaignRamp(t *testing.T) {
	loc := time.UTC
	day := time.Date(2026, 3, 2, 0, 0, 0, 0, loc)
	p := &projectedSender{acct: models.Email{CampaignLimit: 50, MinWaitTime: 60}, cap: 50, loc: loc}
	mid, spans := workday(loc, day)
	if d := projectSenderDay(p, spans, mid, day, false, 12); d.sends != 12 || d.ramp != 38 {
		t.Fatalf("got %+v, want 12 sends and 38 held by the ramp", d)
	}
	if d := projectSenderDay(p, spans, mid, day, false, -1); d.sends != 50 || d.ramp != 0 {
		t.Fatalf("no ramp: got %+v, want the full cap", d)
	}
	// Workspace risk cuts what the ramp left, as stagedCap does.
	p.risk = models.OrgRiskRestricted
	want := 3
	if d := projectSenderDay(p, spans, mid, day, false, 10); d.sends != want || d.ramp != 40 || d.risk != 10-want {
		t.Fatalf("restricted: got %+v, want %d sends after the ramp", d, want)
	}
	// A held day still reports what risk would have taken.
	p.cap, p.held = 40, true
	if d := projectSenderDay(p, spans, mid, day, false, -1); d.sends != 0 || d.risk != 30 || d.held != 10 {
		t.Fatalf("held and restricted: got %+v, want 30 to risk and 10 held", d)
	}
}

func TestNextRampLevelStartsAtRampStart(t *testing.T) {
	level, got := 0, []int{}
	for range 6 {
		level = nextRampLevel(level, 10, 5, 30)
		got = append(got, level)
	}
	want := []int{10, 15, 20, 25, 30, 30}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("levels %v, want %v", got, want)
		}
	}
}
