package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/models"
)

func CommandConcurrency() int {
	if n, err := strconv.Atoi(os.Getenv("WORKER_COMMAND_CONCURRENCY")); err == nil && n >= 1 && n <= 64 {
		return n
	}
	return 8
}

// ResolveCommandKey uses the payload so old task-keyed sends share their mailbox's lane too.
func (w *WorkerService) ResolveCommandKey(ctx context.Context, msg eventbus.Message) (string, error) {
	var event models.WorkerEvent
	if err := w.Codec.Deserialize(ctx, msg.Topic, msg.Payload, &event); err != nil {
		return "", err
	}
	raw, err := json.Marshal(event.Body)
	if err != nil {
		return "", err
	}
	var body struct {
		EmailID   uuid.UUID `json:"email_id"`
		ID        uuid.UUID `json:"id"`
		ProcessID uuid.UUID `json:"process_id"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return "", err
	}
	id := body.EmailID
	switch event.Type {
	case models.WorkerEventTypeAddEmail:
		id = body.ID
	case models.WorkerEventTypeEmailValidation:
		id = body.ProcessID
	}
	if id == uuid.Nil {
		return "", fmt.Errorf("worker command %s has no mailbox or validation identity", event.Type)
	}
	return id.String(), nil
}
