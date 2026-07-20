package jobs

import (
	"context"
	"time"

	"github.com/warmbly/warmbly/internal/infrastructure/eventbus"
	"github.com/warmbly/warmbly/internal/models"
)

// receive decodes a worker-events bus message and dispatches it. The codec must
// match the producing services' CODEC_PROVIDER (JSON on NATS, Avro on Kafka).
func (s *JobsService) receive(_ context.Context, msg eventbus.Message) error {
	var event models.JobEvent

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.Codec.Deserialize(ctx, msg.Topic, msg.Payload, &event); err != nil {
		return err
	}

	return s.HandleEvent(ctx, &event)
}
