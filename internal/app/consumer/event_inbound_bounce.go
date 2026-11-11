package jobs

import (
	"context"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/dsn"
	"github.com/warmbly/warmbly/internal/pkg/mailhdr"
	"github.com/warmbly/warmbly/internal/repository"
)

// HandleInboundBounce records a permanent NDR (parsed worker-side) against the
// original send. A campaign send's bounce suppresses the recipient, marks the
// send bounced and feeds the deliverability breaker; a warmup send's
// unknown-address bounce takes that address out of the sender's partner draw.
// A bounce that genuinely cannot be attributed reads back nil and is acked; a
// technical failure is returned so the bus redelivers it (bounded by the
// consumer's MaxDeliver) rather than dropping an otherwise attributable notice.
func (s *JobsService) HandleInboundBounce(ctx context.Context, e *models.JobEventInboundBounce) error {
	if err := s.recordWarmupBounce(ctx, e); err != nil {
		log.Warn().Err(err).
			Str("email_id", e.EmailID.String()).
			Msg("Failed to record warmup bounce; leaving it for redelivery")
		return err
	}
	if s.AdvancedService == nil {
		return nil
	}
	if xerr := s.AdvancedService.RecordInboundBounce(ctx, e.EmailID, e.OriginalMessageID, e.FailedRecipient, e.Reason); xerr != nil {
		log.Warn().
			Str("email_id", e.EmailID.String()).
			Str("original_message_id", e.OriginalMessageID).
			Str("error", xerr.Message).
			Msg("Failed to record inbound bounce; leaving it for redelivery")
		return xerr
	}
	return nil
}

// recordWarmupBounce stamps the warmup send a DSN is about when it says the
// address does not exist. Anything else (a full mailbox, a block, a reason it
// cannot place) says nothing about the address and is left alone.
func (s *JobsService) recordWarmupBounce(ctx context.Context, e *models.JobEventInboundBounce) error {
	recorder, ok := s.WarmupRepo.(repository.WarmupBounceRecorder)
	if !ok || !dsn.Blame(e.Reason).NamesAddress() {
		return nil
	}
	id := strings.Trim(strings.TrimSpace(e.OriginalMessageID), "<>")
	if id == "" {
		return nil
	}
	_, err := recorder.RecordWarmupBounce(ctx, e.EmailID, id, mailhdr.Bare(e.FailedRecipient))
	return err
}
