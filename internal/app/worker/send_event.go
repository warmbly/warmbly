package worker

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *WorkerService) Produce(jobEventType models.JobEventType, key string, body any) error {
	if state, ok := body.(*models.JobEventSyncState); ok {
		workerID, err := uuid.Parse(s.ID)
		if err != nil || workerID == uuid.Nil {
			return fmt.Errorf("sync state requires a valid reporting worker")
		}
		copy := *state
		copy.WorkerID = &workerID
		body = &copy
	}
	ctx := context.Background()
	payload, err := s.Codec.Serialize(ctx, kafka.TopicWorkerEvents, &models.JobEvent{
		Type: jobEventType,
		Body: body,
	})
	if err != nil {
		return err
	}
	return s.Bus.Publish(ctx, kafka.TopicWorkerEvents, key, payload)
}
