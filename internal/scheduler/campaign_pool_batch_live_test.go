package scheduler

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type campaignPoolTaskReads struct {
	repository.TaskRepository
	single, batch int
	batchErr      error
}

func (r *campaignPoolTaskReads) CountCampaignEmailsSentToday(ctx context.Context, id uuid.UUID) (int, error) {
	r.single++
	return r.TaskRepository.CountCampaignEmailsSentToday(ctx, id)
}

func (r *campaignPoolTaskReads) CountCampaignEmailsSentTodayByAccounts(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]int, error) {
	r.batch++
	if r.batchErr != nil {
		return nil, r.batchErr
	}
	return r.TaskRepository.CountCampaignEmailsSentTodayByAccounts(ctx, ids)
}

func (r *campaignPoolTaskReads) GetSendAdmission(ctx context.Context, org, id uuid.UUID, provider models.InboxProvider, at time.Time) (*repository.SendAdmission, error) {
	return r.TaskRepository.(interface {
		GetSendAdmission(context.Context, uuid.UUID, uuid.UUID, models.InboxProvider, time.Time) (*repository.SendAdmission, error)
	}).GetSendAdmission(ctx, org, id, provider, at)
}

type campaignPoolHealthReads struct {
	repository.WarmupRepository
	single, batch int
	batchErr      error
}

func (r *campaignPoolHealthReads) GetHealthState(ctx context.Context, id uuid.UUID) (models.WarmupHealthState, *time.Time, error) {
	r.single++
	return r.WarmupRepository.GetHealthState(ctx, id)
}

func (r *campaignPoolHealthReads) GetHealthStates(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]repository.WarmupHealthRead, error) {
	r.batch++
	if r.batchErr != nil {
		err := r.batchErr
		r.batchErr = nil
		return nil, err
	}
	return r.WarmupRepository.GetHealthStates(ctx, ids)
}

func TestLiveCampaignPoolReadsStayBatched(t *testing.T) {
	for _, size := range []int{1, 250} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			handle, pool := liveDB(t)
			f := newLiveFixture(t, pool, "UTC")
			if _, err := pool.Exec(t.Context(), `INSERT INTO email_accounts
				(id, user_id, organization_id, email, name, signature_plain, signature_html,
				 provider, status, campaign_limit, min_wait_time, timezone)
				SELECT gen_random_uuid(), $1, $2, 'batch-' || gen_random_uuid() || '@test.local', 'Batch', '', '',
				       'smtp_imap', 'active', 50, 0, 'UTC' FROM generate_series(1, $3::int)`,
				f.user, f.org, size-1); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := pool.Exec(context.Background(), `DELETE FROM email_accounts WHERE organization_id=$1 AND id<>$2`, f.org, f.mailbox); err != nil {
					t.Error(err)
				}
			})
			s := liveScheduler(t, handle, pool).(*schedulerService)
			tasks := &campaignPoolTaskReads{TaskRepository: s.taskRepo}
			health := &campaignPoolHealthReads{WarmupRepository: s.warmupRepo}
			s.taskRepo, s.warmupRepo = tasks, health
			_, pair, _, err := s.CalculateNextCampaignTime(t.Context(), f.campaign)
			if err != nil && !errors.Is(err, ErrCampaignDeferred) || pair == nil {
				t.Fatalf("schedule: pair=%v err=%v", pair, err)
			}
			if tasks.batch != 1 || tasks.single != 0 || health.batch != 1 || health.single != 0 {
				t.Fatalf("%d mailboxes: task reads batch=%d single=%d; health reads batch=%d single=%d",
					size, tasks.batch, tasks.single, health.batch, health.single)
			}
		})
	}
}

func TestLiveCampaignPoolBatchFailuresKeepSafety(t *testing.T) {
	for _, budgetFails := range []bool{true, false} {
		t.Run(fmt.Sprint("budget=", budgetFails), func(t *testing.T) {
			handle, pool := liveDB(t)
			f := newLiveFixture(t, pool, "UTC")
			s := liveScheduler(t, handle, pool).(*schedulerService)
			failure := errors.New("batch unavailable")
			tasks := &campaignPoolTaskReads{TaskRepository: s.taskRepo}
			health := &campaignPoolHealthReads{WarmupRepository: s.warmupRepo}
			if budgetFails {
				tasks.batchErr = failure
			} else {
				health.batchErr = failure
			}
			s.taskRepo, s.warmupRepo = tasks, health
			_, pair, _, err := s.CalculateNextCampaignTime(t.Context(), f.campaign)
			if budgetFails {
				if !errors.Is(err, failure) || pair != nil || tasks.single != 0 || health.batch != 0 {
					t.Fatalf("unknown budget must not send: pair=%v err=%v task singles=%d health batches=%d", pair, err, tasks.single, health.batch)
				}
			} else if err != nil && !errors.Is(err, ErrCampaignDeferred) || pair == nil || health.single != 0 || health.batch != 2 {
				t.Fatalf("health must fall back to a one-mailbox rich read: pair=%v err=%v health singles=%d batches=%d", pair, err, health.single, health.batch)
			}
		})
	}
}

func TestLiveCampaignPoolStandalonePlacementIsBatched(t *testing.T) {
	handle, pool := liveDB(t)
	f := newLiveFixture(t, pool, "UTC")
	s := liveScheduler(t, handle, pool).(*schedulerService)
	campaign, err := s.campaignRepo.GetByID(t.Context(), f.campaign)
	if err != nil {
		t.Fatal(err)
	}
	accounts, meta, err := s.campaignSenders(t.Context(), campaign)
	if err != nil {
		t.Fatal(err)
	}
	pairs, _, _, err := s.campaignProgressRepo.FindRoutedPairs(t.Context(), f.campaign, "created_at", "asc", "", false, false, nil, nil, 1)
	if err != nil || len(pairs) != 1 {
		t.Fatalf("pairs=%v err=%v", pairs, err)
	}
	tasks := &campaignPoolTaskReads{TaskRepository: s.taskRepo}
	health := &campaignPoolHealthReads{WarmupRepository: s.warmupRepo}
	s.taskRepo, s.warmupRepo = tasks, health
	_, pair, _, err := s.placeCampaignSend(t.Context(), campaign, accounts, meta, &pairs[0], nil, true)
	if err != nil && !errors.Is(err, ErrCampaignDeferred) || pair == nil {
		t.Fatalf("placement: pair=%v err=%v", pair, err)
	}
	if tasks.batch != 1 || tasks.single != 0 || health.batch != 1 || health.single != 0 {
		t.Fatalf("standalone reads: tasks=%+v health=%+v", tasks, health)
	}
}
