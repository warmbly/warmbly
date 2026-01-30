package worker

import (
	"github.com/warmbly/warmbly/internal/infrastructure/kafka"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *WorkerService) Produce(jobEventType models.JobEventType, key string, body any) error {
	resp, err := s.KafkaProducer.Avrov2.Ser.Serialize(kafka.TopicWorkerEvents, &models.JobEvent{
		Type: jobEventType,
		Body: body,
	})
	if err != nil {
		return err
	}

	return s.KafkaProducer.Produce(kafka.TopicWorkerEvents, []byte(key), resp)
}
