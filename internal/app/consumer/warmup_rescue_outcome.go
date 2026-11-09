package jobs

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/models"
)

// HandleWarmupRescued records what a spam rescue came to on the sender's placement history.
func (s *JobsService) HandleWarmupRescued(ctx context.Context, event *models.JobEventWarmupRescued) error {
	if event == nil || s.WarmupPlacementRepo == nil {
		return nil
	}
	if !models.ValidWarmupRescueOutcome(event.Outcome) {
		log.Warn().Str("email_id", event.EmailID.String()).Str("outcome", event.Outcome).Msg("Ignoring unknown warmup rescue outcome")
		return nil
	}
	group, host := placementRecipient(s.recipientAccount(ctx, event.EmailID))
	sender, err := s.WarmupPlacementRepo.RecordRescueOutcome(ctx, event.EmailID, event.InternalID, group, host, event.Outcome)
	if err != nil {
		return fmt.Errorf("record warmup rescue outcome: %w", err)
	}
	if sender == nil || sender.OrgID == nil || s.StreamingPublisher == nil {
		return nil
	}
	s.StreamingPublisher.PublishWarmupPlacement(ctx, sender.OrgID.String(), sender.UserID, sender.ID.String(), sender.Email, "rescue_"+event.Outcome)
	return nil
}
