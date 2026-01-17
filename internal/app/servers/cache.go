package servers

import (
	"context"
	"encoding/json"

	"github.com/getsentry/sentry-go"
	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

func GetWorkersKey() string {
	return "workers"
}

func (s *serversService) getWorkers(ctx context.Context) ([]models.Worker, *errx.Error) {
	key := GetWorkersKey()

	bytes, err := s.cache.Get(ctx, key).Bytes()
	if err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	var workers []models.Worker
	if err := json.Unmarshal(bytes, &workers); err != nil {
		sentry.CaptureException(err)
		return nil, errx.InternalError()
	}

	return workers, nil
}

func (s *serversService) saveWorkers(ctx context.Context, workers []models.Worker) *errx.Error {
	data, err := json.Marshal(workers)
	if err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	if err := s.cache.SetNX(ctx, GetWorkersKey(), data, WorkersTTL).Err(); err != nil {
		sentry.CaptureException(err)
		return errx.InternalError()
	}

	return nil
}
