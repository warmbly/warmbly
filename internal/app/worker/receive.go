package worker

import (
	"context"

	cfk "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

func (w *WorkerService) Receive(msg *cfk.Message) error {
	var event models.WorkerEvent

	if w.KafkaConsumer.Avrov2 != nil {
		if err := w.KafkaConsumer.Avrov2.Deser.DeserializeInto(*msg.TopicPartition.Topic, msg.Value, &event); err != nil {
			log.Debug().Err(err).Msg("Receive Avro DeserializeInto")
			return err
		}
	} else {
		log.Error().Msg("Avro v2 deserializer not configured on worker consumer")
		return nil
	}

	return w.HandleEvent(context.TODO(), &event)
}
