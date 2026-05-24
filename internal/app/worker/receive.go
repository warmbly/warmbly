package worker

import (
	"context"
	"time"

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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return w.HandleEvent(ctx, &event)
}
