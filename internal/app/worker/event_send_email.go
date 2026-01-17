package worker

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

func (w *WorkerService) HandleSendEmail(ctx context.Context, body any) error {
	_, ok := body.(models.SendEmail)
	if !ok {
		log.Debug().Msg("Invalid HandleSendEmail body type")
		return fmt.Errorf("invalid body type")
	}
	return nil
}
