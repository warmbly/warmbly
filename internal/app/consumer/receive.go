package jobs

import (
	"context"

	cfk "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *JobsService) Receive(msg *cfk.Message) error {
	var event models.JobEvent

	if err := s.Consumer.Avrov2.Deser.DeserializeInto(*msg.TopicPartition.Topic, msg.Value, &event); err != nil {
		return err
	}

	return s.HandleEvent(context.TODO(), &event)
}
