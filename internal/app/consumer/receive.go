package jobs

import (
	"context"
	"time"

	cfk "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *JobsService) Receive(msg *cfk.Message) error {
	var event models.JobEvent

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if s.Codec != nil {
		if err := s.Codec.Deserialize(ctx, *msg.TopicPartition.Topic, msg.Value, &event); err != nil {
			return err
		}
	} else if err := s.Consumer.Avrov2.Deser.DeserializeInto(*msg.TopicPartition.Topic, msg.Value, &event); err != nil {
		return err
	}

	return s.HandleEvent(ctx, &event)
}
