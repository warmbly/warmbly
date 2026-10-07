package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type coldRampRepositoryStub struct {
	repository.WarmupRepository
	states map[uuid.UUID]repository.ColdRampState
	err    error
}

func (r coldRampRepositoryStub) ColdRampStateForAccounts(context.Context, []uuid.UUID, time.Time) (map[uuid.UUID]repository.ColdRampState, error) {
	return r.states, r.err
}

func TestColdRampUnknownFeedbackPreservesKnownWarmupInExecutionAndProjection(t *testing.T) {
	now := time.Now()
	warmed := now.Add(-90 * 24 * time.Hour)
	ramp := now.Add(-30 * 24 * time.Hour)
	account := models.Email{ID: uuid.New(), CampaignLimit: 50, MinWaitTime: 60, Warmup: &warmed}
	unwarmed := models.Email{ID: uuid.New(), CampaignLimit: 50}
	for _, tc := range []struct {
		name string
		repo repository.WarmupRepository
	}{
		{"repository unavailable", nil},
		{"query failure", coldRampRepositoryStub{err: errors.New("feedback unavailable")}},
		{"partial failed query cannot grant capacity", coldRampRepositoryStub{
			states: map[uuid.UUID]repository.ColdRampState{account.ID: {ConfirmedReplies: 100, ColdRampStartedAt: &ramp}},
			err:    errors.New("feedback incomplete"),
		}},
		{"missing mailbox", coldRampRepositoryStub{states: map[uuid.UUID]repository.ColdRampState{}}},
		{"missing warmup history", coldRampRepositoryStub{states: map[uuid.UUID]repository.ColdRampState{account.ID: {}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &schedulerService{warmupRepo: tc.repo}
			states := s.coldRampStates(t.Context(), []models.Email{account, unwarmed})
			if state := states[account.ID]; state.WarmupStartedAt == nil || !state.WarmupStartedAt.Equal(warmed) || state.ConfirmedReplies != 0 {
				t.Fatal("unknown feedback erased history or manufactured replies", state)
			}
			pass := &campaignPass{campaign: &models.Campaign{DailyLimit: 50}, coldRamp: states, risk: models.OrgRiskTrusted,
				health: map[uuid.UUID]healthRead{account.ID: {}}}
			if cap := pass.explainCap(account); cap.Cap != 5 || cap.LimitedBy != capByGraduation {
				t.Fatal("execution lifted unknown evidence to full cap", cap)
			}
			if cap := pass.explainCap(unwarmed); cap.Cap != 50 {
				t.Fatal("unknown feedback capped a never-warmed mailbox", cap)
			}
			plan := s.planMailbox(t.Context(), pass, account, 0, 0, nil, now, 10*3600, now.Add(10*time.Hour))
			if plan.byGraduation != 45 || plan.graduation == nil || plan.graduation.Ceiling != 5 || plan.graduation.DaysToFullCap != -1 {
				t.Fatal("plan omitted the conservative feedback explanation", plan)
			}
			p := &projectedSender{acct: account, cap: 50, loc: time.UTC, cold: states[account.ID], hasCold: true}
			mid, spans := workday(time.UTC, now.AddDate(0, 0, 10))
			if day := projectSenderDay(p, spans, mid, now, false); day.sends != 5 || day.graduation != 45 {
				t.Fatal("projection turned unknown feedback into readiness", day)
			}
			if fullCapacityEvidenceKnown([]*projectedSender{p}) {
				t.Fatal("unknown feedback projected full-cap readiness")
			}
		})
	}
	pass := &campaignPass{campaign: &models.Campaign{DailyLimit: 50}, risk: models.OrgRiskTrusted}
	if cap := pass.explainCap(account); cap.Cap != 5 {
		t.Fatal("missing pass entry bypassed known warmup", cap)
	}
	observed := coldRampRepositoryStub{states: map[uuid.UUID]repository.ColdRampState{
		account.ID: {ConfirmedReplies: 12, ColdRampStartedAt: &ramp},
	}}
	s := &schedulerService{warmupRepo: observed}
	pass.coldRamp = s.coldRampStates(t.Context(), []models.Email{account})
	if cap := pass.explainCap(account); cap.Cap != 17 {
		t.Fatal("fallback discarded genuine reply evidence", cap)
	}
}
