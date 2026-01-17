package worker

import (
	"context"
	"encoding/json"

	cfk "github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

func (w *WorkerService) Receive(msg *cfk.Message) error {
	var event models.WorkerEvent
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		log.Debug().Err(err).Msg("Receive Unmarshal")
	}

	return w.HandleEvent(context.TODO(), &event)
}
