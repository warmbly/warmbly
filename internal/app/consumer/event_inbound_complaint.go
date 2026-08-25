package jobs

import (
	"context"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

func (s *JobsService) HandleInboundComplaint(ctx context.Context, event *models.JobEventInboundComplaint) error {
	if s.AdvancedService == nil {
		return nil
	}
	if xerr := s.AdvancedService.RecordInboundComplaint(ctx, event.EmailID, event.OriginalMessageID, event.Recipient, event.FeedbackType); xerr != nil {
		log.Warn().Str("email_id", event.EmailID.String()).Str("original_message_id", event.OriginalMessageID).Str("error", xerr.Message).Msg("failed to record inbound complaint")
	}
	return nil
}
