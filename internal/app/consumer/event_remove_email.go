package jobs

import (
	"context"

	"github.com/warmbly/warmbly/internal/models"
)

// HandleRemoveEmail processes a message removal observed during mailbox sync.
//
// Tampering protection: if the removed message was a warmup email (tracked in
// warmup_received), the recipient deleted pool warmup mail — that harms the
// pool, so we record a tampering strike against the mailbox and ban it from
// warmup once the threshold is crossed. The owner can appeal.
//
// It also drops the local unibox entry for the removed message (best-effort).
func (s *JobsService) HandleRemoveEmail(ctx context.Context, e *models.JobEventRemoveEmail) error {
	if s.WarmupRepo != nil {
		if rec, _ := s.WarmupRepo.GetWarmupReceived(ctx, e.EmailID, e.ID); rec != nil {
			if s.WarmupService != nil {
				health, _ := s.WarmupService.RecordTampering(ctx, e.EmailID, rec.MessageID, "deletion")
				s.markRiskBandFromWarmupHealth(ctx, e.EmailID, health)
			}
		}
	}

	if s.UniboxRepository != nil {
		_ = s.UniboxRepository.Delete(ctx, e.UserID, e.ID)
	}
	return nil
}
