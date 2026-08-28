package jobs

import (
	"context"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

// HandleInboundComplaint records an abuse feedback report (parsed worker-side)
// against the original campaign send: suppress the recipient, mark the send
// complained, and feed the deliverability breaker. Best-effort — a report we
// cannot attribute is logged, not retried.
func (s *JobsService) HandleInboundComplaint(ctx context.Context, e *models.JobEventInboundComplaint) error {
	if s.AdvancedService == nil {
		return nil
	}
	if xerr := s.AdvancedService.RecordInboundComplaint(ctx, e.EmailID, e.OriginalMessageID, e.ComplainedRecipient, e.Provider); xerr != nil {
		log.Warn().
			Str("email_id", e.EmailID.String()).
			Str("original_message_id", e.OriginalMessageID).
			Str("error", xerr.Message).
			Msg("Failed to record inbound complaint")
	}
	return nil
}
