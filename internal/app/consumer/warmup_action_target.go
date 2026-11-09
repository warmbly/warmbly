package jobs

import (
	"context"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"
)

// warmupActionWorker is the worker a mailbox's warmup actions are published to; false when it has none.
func (s *JobsService) warmupActionWorker(_ context.Context, account *models.Email) (uuid.UUID, bool) {
	if account == nil || account.WorkerID == nil {
		return uuid.Nil, false
	}
	return *account.WorkerID, true
}
