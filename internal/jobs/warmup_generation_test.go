package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/warmbly/warmbly/internal/app/warmupcontent"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type generationRetryService struct {
	warmupcontent.Service
	calls int
}

func (*generationRetryService) Enabled() bool { return true }
func (s *generationRetryService) RunScheduled(context.Context) error {
	s.calls++
	if s.calls == 1 {
		return errors.New("submission unavailable")
	}
	return nil
}

type generationRetryRepo struct {
	repository.WarmupContentRepository
}

func (generationRetryRepo) GetGenerationSettings(context.Context) (*models.WarmupGenerationSettings, error) {
	settings := models.DefaultWarmupGenerationSettings()
	return &settings, nil
}

func TestWarmupGenerationRetriesFailureBeforeNormalCadence(t *testing.T) {
	svc := &generationRetryService{}
	job := NewWarmupGenerationJob(svc, generationRetryRepo{})
	if err := job.Run(t.Context()); err == nil {
		t.Fatal("submission failure was hidden")
	}
	if err := job.Run(t.Context()); err != nil || svc.calls != 2 {
		t.Fatalf("failed attempt blocked retry: calls=%d err=%v", svc.calls, err)
	}
	if err := job.Run(t.Context()); err != nil || svc.calls != 2 {
		t.Fatal("successful attempt did not retain normal cadence")
	}
}
