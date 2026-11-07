package analytics

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

// The status page's other reads find nothing, so only the cold ramp speaks.
func (coldRampRepositoryStub) GetCloudStandings(context.Context, []uuid.UUID) (map[uuid.UUID]*models.WarmupHealthInfo, error) {
	return nil, nil
}

func (coldRampRepositoryStub) SpamPlacementsSinceForAccounts(context.Context, []uuid.UUID, time.Time) (map[uuid.UUID][]time.Time, error) {
	return nil, nil
}

func (coldRampRepositoryStub) LastWarmupSendFailures(context.Context, []uuid.UUID, time.Time) (map[uuid.UUID]*models.WarmupSendFailure, error) {
	return nil, nil
}

func TestColdRampUnknownFeedbackKeepsConservativeMailboxExplanation(t *testing.T) {
	warmed := time.Now().Add(-90 * 24 * time.Hour)
	ramp := time.Now().Add(-30 * 24 * time.Hour)
	email := &models.Email{ID: uuid.New(), Warmup: &warmed, CampaignLimit: 50}
	for _, tc := range []struct {
		name string
		repo repository.WarmupRepository
	}{
		{"repository unavailable", nil},
		{"query failure", coldRampRepositoryStub{err: errors.New("feedback unavailable")}},
		{"partial failed query cannot grant capacity", coldRampRepositoryStub{
			states: map[uuid.UUID]repository.ColdRampState{email.ID: {ConfirmedReplies: 100, ColdRampStartedAt: &ramp}},
			err:    errors.New("feedback incomplete"),
		}},
		{"missing mailbox", coldRampRepositoryStub{states: map[uuid.UUID]repository.ColdRampState{}}},
		{"missing warmup history", coldRampRepositoryStub{states: map[uuid.UUID]repository.ColdRampState{email.ID: {}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &analyticsService{warmupRepo: tc.repo}
			if info := coldRampFor(t, s, email); info == nil || info.Ceiling != 5 || info.DaysToFullCap != -1 {
				t.Fatal("feedback gap hid the conservative explanation", info)
			}
			unwarmed := &models.Email{ID: uuid.New(), CampaignLimit: 50}
			if info := coldRampFor(t, s, unwarmed); info != nil {
				t.Fatal("never-warmed mailbox acquired a graduation claim", info)
			}
		})
	}
	s := &analyticsService{warmupRepo: coldRampRepositoryStub{states: map[uuid.UUID]repository.ColdRampState{
		email.ID: {ConfirmedReplies: 12, ColdRampStartedAt: &ramp},
	}}}
	if info := coldRampFor(t, s, email); info == nil || info.Ceiling != 17 {
		t.Fatal("explanation lost genuine reply evidence", info)
	}
}

func TestColdRampNoticeQuietOnceWarmupStopped(t *testing.T) {
	warmed := time.Now().Add(-90 * 24 * time.Hour)
	stopped := time.Now().Add(-time.Hour)
	email := &models.Email{ID: uuid.New(), Warmup: &warmed, CampaignLimit: 50}
	s := &analyticsService{warmupRepo: coldRampRepositoryStub{states: map[uuid.UUID]repository.ColdRampState{
		email.ID: {WarmupStartedAt: &warmed, WarmupStoppedAt: &stopped},
	}}}
	if info := coldRampFor(t, s, email); info != nil {
		t.Fatal("stopped mailbox still shows the cold ceiling", info)
	}
}

// coldRampFor reads one mailbox's cold-ramp notice the way a status page does.
func coldRampFor(t *testing.T, s *analyticsService, email *models.Email) *models.ColdRampInfo {
	t.Helper()
	reads := s.loadStatusReads(t.Context(), []models.Email{*email}, nil)
	return coldRampNotice(email, reads.coldRamp, reads.now)
}
