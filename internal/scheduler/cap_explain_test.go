package scheduler

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// A self-hosted campaign on three fresh mailboxes sent one email a morning and
// its feed said nothing about why. Every clamp below is one that can shorten a
// pool's day, and the breakdown has to name the one that did.
func TestExplainCapNamesTheClampThatDecided(t *testing.T) {
	id := uuid.New()
	warmed := time.Now().Add(-2 * 24 * time.Hour)
	base := func() *campaignPass {
		return &campaignPass{
			campaign:  &models.Campaign{DailyLimit: 50, WarmupGraduation: true},
			coldRamp:  map[uuid.UUID]repository.ColdRampState{},
			risk:      models.OrgRiskTrusted,
			sentToday: map[uuid.UUID]int{},
			health:    map[uuid.UUID]healthRead{},
		}
	}
	acct := models.Email{ID: id, Email: "a@example.com", CampaignLimit: 50}

	for _, tc := range []struct {
		name string
		pass *campaignPass
		acct models.Email
		cap  int
		why  string
	}{
		{"nothing tighter than the mailbox", base(), acct, 50, capByMailbox},
		{"campaign limit below the mailbox", func() *campaignPass { p := base(); p.campaign.DailyLimit = 20; return p }(), acct, 20, capByCampaign},
		{"campaign ramp on its first level", func() *campaignPass {
			p := base()
			p.campaign.RampEnabled, p.campaign.RampStart, p.campaign.RampCeiling, p.campaign.RampLevel = true, 10, 50, 10
			return p
		}(), acct, 10, capByRamp},
		{"warming mailbox graduates at five", func() *campaignPass {
			p := base()
			p.coldRamp[id] = repository.ColdRampState{WarmupStartedAt: &warmed}
			return p
		}(), acct, 5, capByGraduation},
		{"restricted workspace takes a warming mailbox to one", func() *campaignPass {
			p := base()
			p.coldRamp[id] = repository.ColdRampState{WarmupStartedAt: &warmed}
			p.risk = models.OrgRiskRestricted
			return p
		}(), acct, 1, capByRisk},
	} {
		got := tc.pass.explainCap(tc.acct)
		if got.Cap != tc.cap || got.LimitedBy != tc.why {
			t.Errorf("%s: cap %d by %q, want %d by %q", tc.name, got.Cap, got.LimitedBy, tc.cap, tc.why)
		}
		if eff := tc.pass.effectiveCap(tc.acct); eff != got.Cap {
			t.Errorf("%s: effectiveCap %d disagrees with explainCap %d", tc.name, eff, got.Cap)
		}
	}
}

func TestPoolBudgetReportsEveryMailbox(t *testing.T) {
	open, shut := uuid.New(), uuid.New()
	warmed := time.Now().Add(-24 * time.Hour)
	pass := &campaignPass{
		campaign:  &models.Campaign{DailyLimit: 50, WarmupGraduation: true},
		coldRamp:  map[uuid.UUID]repository.ColdRampState{shut: {WarmupStartedAt: &warmed}},
		risk:      models.OrgRiskTrusted,
		sentToday: map[uuid.UUID]int{open: 0, shut: 5},
		health:    map[uuid.UUID]healthRead{shut: {state: models.WarmupHealthWatch, known: true}},
	}
	accounts := []models.Email{
		{ID: open, Email: "open@example.com", CampaignLimit: 50},
		{ID: shut, Email: "shut@example.com", CampaignLimit: 50},
	}
	gates := map[uuid.UUID]mailboxGate{shut: {reason: gateBudget, paced: true}}

	rows := (&schedulerService{}).poolBudget(pass, accounts, gates, uuid.Nil)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want one per mailbox", len(rows))
	}
	if rows[0]["mailbox"] != "open@example.com" || rows[0]["cap"] != 50 || rows[0]["sent_today"] != 0 {
		t.Errorf("open mailbox misreported: %+v", rows[0])
	}
	if _, gated := rows[0]["gate"]; gated {
		t.Errorf("an open mailbox carries a gate: %+v", rows[0])
	}
	if rows[1]["cap"] != 5 || rows[1]["limited_by"] != capByGraduation || rows[1]["sent_today"] != 5 ||
		rows[1]["gate"] != gateBudget || rows[1]["warmup_health"] != "watch" {
		t.Errorf("spent mailbox misreported: %+v", rows[1])
	}
}

// The "last email today" line is written before the send it describes, so the
// sending mailbox has to be shown after it: a cap-one mailbox read at 0 sends
// with no gate would contradict the line it sits on.
func TestPoolBudgetCountsTheSendInFlight(t *testing.T) {
	id := uuid.New()
	warmed := time.Now().Add(-24 * time.Hour)
	pass := &campaignPass{
		campaign:  &models.Campaign{DailyLimit: 50, WarmupGraduation: true},
		coldRamp:  map[uuid.UUID]repository.ColdRampState{id: {WarmupStartedAt: &warmed}},
		risk:      models.OrgRiskRestricted,
		sentToday: map[uuid.UUID]int{id: 0},
		health:    map[uuid.UUID]healthRead{},
	}
	rows := (&schedulerService{}).poolBudget(pass, []models.Email{{ID: id, Email: "one@example.com", CampaignLimit: 50}}, nil, id)
	if rows[0]["cap"] != 1 || rows[0]["sent_today"] != 1 || rows[0]["gate"] != gateBudget || rows[0]["sending_now"] != true {
		t.Errorf("the send in flight is not projected: %+v", rows[0])
	}
}
