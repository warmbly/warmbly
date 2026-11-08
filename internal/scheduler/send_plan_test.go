package scheduler

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// The plan explains the cap with the working shown; explainCap decides the
// send. They read the same clamps, and this keeps them from drifting apart.
func TestStagedCapAgreesWithExplainCap(t *testing.T) {
	id := uuid.New()
	warmed := time.Now().Add(-2 * 24 * time.Hour)
	for _, tc := range []struct {
		name string
		pass *campaignPass
		acct models.Email
	}{
		{"plain", &campaignPass{campaign: &models.Campaign{DailyLimit: 50}, risk: models.OrgRiskTrusted}, models.Email{ID: id, CampaignLimit: 50}},
		{"campaign limit", &campaignPass{campaign: &models.Campaign{DailyLimit: 20}, risk: models.OrgRiskTrusted}, models.Email{ID: id, CampaignLimit: 50}},
		{"ramp", &campaignPass{campaign: &models.Campaign{DailyLimit: 50, RampEnabled: true, RampStart: 10, RampCeiling: 50, RampLevel: 15}, risk: models.OrgRiskTrusted}, models.Email{ID: id, CampaignLimit: 50}},
		{"graduation", &campaignPass{campaign: &models.Campaign{DailyLimit: 50, WarmupGraduation: true}, coldRamp: map[uuid.UUID]repository.ColdRampState{id: {WarmupStartedAt: &warmed}}, risk: models.OrgRiskTrusted}, models.Email{ID: id, CampaignLimit: 50}},
		{"restricted", &campaignPass{campaign: &models.Campaign{DailyLimit: 50}, risk: models.OrgRiskRestricted}, models.Email{ID: id, CampaignLimit: 50}},
		{"everything", &campaignPass{campaign: &models.Campaign{DailyLimit: 30, WarmupGraduation: true, RampEnabled: true, RampStart: 10, RampCeiling: 50, RampLevel: 25}, coldRamp: map[uuid.UUID]repository.ColdRampState{id: {WarmupStartedAt: &warmed}}, risk: models.OrgRiskRestricted}, models.Email{ID: id, CampaignLimit: 80}},
	} {
		stages, by := stagedCap(tc.pass, tc.acct)
		want := tc.pass.explainCap(tc.acct)
		if stages[4] != want.Cap || by != want.LimitedBy {
			t.Errorf("%s: staged %d by %q, explainCap %d by %q", tc.name, stages[4], by, want.Cap, want.LimitedBy)
		}
		for i := 1; i < len(stages); i++ {
			if stages[i] > stages[i-1] {
				t.Errorf("%s: stage %d (%d) above stage %d (%d): a clamp can only lower", tc.name, i, stages[i], i-1, stages[i-1])
			}
		}
	}
}

// A campaign with no daily limit (the workspace meter, the wizard before a
// limit is chosen) must not be clamped to zero by it.
func TestStagedCapIgnoresAnUnsetCampaignLimit(t *testing.T) {
	pass := &campaignPass{campaign: &models.Campaign{}, risk: models.OrgRiskTrusted}
	stages, by := stagedCap(pass, models.Email{ID: uuid.New(), CampaignLimit: 50})
	if stages[4] != 50 || by != capByMailbox {
		t.Fatalf("got %d by %q, want 50 by %q", stages[4], by, capByMailbox)
	}
}

func TestDayWindow(t *testing.T) {
	tz := "Europe/Paris"
	loc, _ := time.LoadLocation(tz)
	// Wednesday 14:30 Paris.
	now := time.Date(2026, 9, 16, 14, 30, 0, 0, loc)
	nineToFive := models.ScheduleWindows{}
	for wd := 1; wd <= 5; wd++ {
		nineToFive[wd] = []models.TimeInterval{{Start: 9 * 60, End: 17 * 60}}
	}

	t.Run("open, with the rest of the window ahead", func(t *testing.T) {
		w, secs, closes := dayWindow(&models.Campaign{Timezone: tz, ScheduleWindows: nineToFive}, now)
		if !w.SendingDay || !w.OpenNow || w.MinutesLeft != 150 || secs != 150*60 {
			t.Fatalf("got %+v secs %d", w, secs)
		}
		if closes.In(loc).Hour() != 17 || closes.In(loc).Minute() != 0 {
			t.Fatalf("closes at %v", closes.In(loc))
		}
	})
	t.Run("closed for the day, opens tomorrow", func(t *testing.T) {
		late := time.Date(2026, 9, 16, 18, 0, 0, 0, loc)
		w, secs, _ := dayWindow(&models.Campaign{Timezone: tz, ScheduleWindows: nineToFive}, late)
		if !w.SendingDay || w.OpenNow || secs != 0 || w.OpensAt == nil || w.OpensAt.In(loc).Day() != 17 || w.OpensAt.In(loc).Hour() != 9 {
			t.Fatalf("got %+v secs %d", w, secs)
		}
	})
	t.Run("weekend is not a sending day", func(t *testing.T) {
		sat := time.Date(2026, 9, 19, 10, 0, 0, 0, loc)
		w, secs, _ := dayWindow(&models.Campaign{Timezone: tz, ScheduleWindows: nineToFive}, sat)
		if w.SendingDay || w.OpenNow || secs != 0 || w.OpensAt == nil || w.OpensAt.In(loc).Weekday() != time.Monday {
			t.Fatalf("got %+v secs %d", w, secs)
		}
	})
	t.Run("before the window opens today", func(t *testing.T) {
		early := time.Date(2026, 9, 16, 8, 0, 0, 0, loc)
		w, secs, _ := dayWindow(&models.Campaign{Timezone: tz, ScheduleWindows: nineToFive}, early)
		if !w.SendingDay || w.OpenNow || w.MinutesLeft != 8*60 || secs != 8*3600 || w.OpensAt == nil || w.OpensAt.In(loc).Hour() != 9 {
			t.Fatalf("got %+v secs %d", w, secs)
		}
	})
	t.Run("unconstrained schedule runs to midnight", func(t *testing.T) {
		w, secs, _ := dayWindow(&models.Campaign{Timezone: tz}, now)
		if !w.SendingDay || !w.OpenNow || w.MinutesLeft != 9*60+30 || secs != (9*60+30)*60 {
			t.Fatalf("got %+v secs %d", w, secs)
		}
	})
	t.Run("a start date ahead holds the day", func(t *testing.T) {
		start := time.Date(2026, 9, 18, 9, 0, 0, 0, loc)
		w, secs, _ := dayWindow(&models.Campaign{Timezone: tz, ScheduleWindows: nineToFive, StartDate: &start}, now)
		if w.OpenNow || secs != 0 || w.StartsAt == nil || w.OpensAt == nil || !w.OpensAt.Equal(start) {
			t.Fatalf("got %+v secs %d", w, secs)
		}
	})
	t.Run("an end date later today ends the day there", func(t *testing.T) {
		end := time.Date(2026, 9, 16, 15, 0, 0, 0, loc)
		w, secs, closes := dayWindow(&models.Campaign{Timezone: tz, ScheduleWindows: nineToFive, EndDate: &end}, now)
		if !w.OpenNow || w.MinutesLeft != 30 || secs != 30*60 || !closes.Equal(end) {
			t.Fatalf("got %+v secs %d closes %v", w, secs, closes.In(loc))
		}
	})
	t.Run("past the end date nothing is left", func(t *testing.T) {
		end := time.Date(2026, 9, 15, 9, 0, 0, 0, loc)
		w, secs, _ := dayWindow(&models.Campaign{Timezone: tz, ScheduleWindows: nineToFive, EndDate: &end}, now)
		if w.OpenNow || w.SendingDay || secs != 0 {
			t.Fatalf("got %+v secs %d", w, secs)
		}
	})
}

// The waterfall's arithmetic on one mailbox, with no repositories behind it:
// the cap clamps and the sends already made must add up to what is left.
func TestRoomAddsUp(t *testing.T) {
	if room(50, 20) != 30 || room(10, 20) != 0 || room(0, 0) != 0 {
		t.Fatal("room is cap minus sent, never negative")
	}
}
