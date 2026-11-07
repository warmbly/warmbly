package scheduler

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

func TestColdCeilingFollowsWarmupLifecycle(t *testing.T) {
	now := time.Now()
	day := 24 * time.Hour
	warmed := now.Add(-90 * day)
	ramp := now.Add(-30 * day)
	stoppedAfter := now.Add(-2 * day)
	stoppedBefore := now.Add(-120 * day)
	for _, tc := range []struct {
		name  string
		state repository.ColdRampState
		want  int
	}{
		{"never warmed", repository.ColdRampState{}, 50},
		{"warming without replies", repository.ColdRampState{WarmupStartedAt: &warmed}, 5},
		// Pause leaves warmup_stopped_at unset, so it stays under the ramp.
		{"paused", repository.ColdRampState{WarmupStartedAt: &warmed, ColdRampStartedAt: &ramp, ConfirmedReplies: 3}, 8},
		{"stopped", repository.ColdRampState{WarmupStartedAt: &warmed, WarmupStoppedAt: &stoppedAfter}, 50},
		{"stopped the moment it started", repository.ColdRampState{WarmupStartedAt: &warmed, WarmupStoppedAt: &warmed}, 50},
		// A restart clears the stamp; one older than the start no longer counts.
		{"restarted", repository.ColdRampState{WarmupStartedAt: &warmed, ColdRampStartedAt: &ramp, ConfirmedReplies: 12}, 17},
		{"restarted after an older stop", repository.ColdRampState{WarmupStartedAt: &warmed, WarmupStoppedAt: &stoppedBefore, ColdRampStartedAt: &ramp, ConfirmedReplies: 12}, 17},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := coldCeilingFor(tc.state, 50); got != tc.want {
				t.Fatalf("ceiling = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestStoppedWarmupLiftsColdCeilingForEveryReader(t *testing.T) {
	now := time.Now()
	warmed := now.Add(-90 * 24 * time.Hour)
	stopped := now.Add(-time.Hour)
	account := models.Email{ID: uuid.New(), CampaignLimit: 50, MinWaitTime: 600, Warmup: &warmed}
	s := &schedulerService{warmupRepo: coldRampRepositoryStub{states: map[uuid.UUID]repository.ColdRampState{
		account.ID: {WarmupStartedAt: &warmed, WarmupStoppedAt: &stopped},
	}}}
	states := s.coldRampStates(t.Context(), []models.Email{account})
	pass := &campaignPass{campaign: &models.Campaign{DailyLimit: 50}, coldRamp: states, risk: models.OrgRiskTrusted,
		health: map[uuid.UUID]healthRead{account.ID: {}}}
	if cap := pass.explainCap(account); cap.Cap != 50 || cap.LimitedBy == capByGraduation {
		t.Fatal("execution kept the ceiling on a stopped mailbox", cap)
	}
	plan := s.planMailbox(t.Context(), pass, account, 0, 0, nil, now, 10*3600, now.Add(10*time.Hour))
	if plan.byGraduation != 0 || plan.graduation != nil {
		t.Fatal("send plan explained a ceiling that no longer applies", plan)
	}
	p := &projectedSender{acct: account, cap: 50, loc: time.UTC, cold: states[account.ID], hasCold: true}
	mid, spans := workday(time.UTC, now.AddDate(0, 0, 10))
	if day := projectSenderDay(p, spans, mid, now, false); day.graduation != 0 {
		t.Fatal("projection kept the ceiling on a stopped mailbox", day)
	}
	if !fullCapacityEvidenceKnown([]*projectedSender{p}) {
		t.Fatal("projection treated a stopped mailbox as still ramping")
	}
}
