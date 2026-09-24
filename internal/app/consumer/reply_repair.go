package jobs

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/jobrun"
)

// replyRepairWindow bounds how far back the sweep looks. The address checks
// that refused IMAP replies shipped on 16 September 2026; older mail was
// processed by code that did not have them.
const replyRepairWindow = 45 * 24 * time.Hour

// StartIncomingReplyRepair re-offers inbound mail that reply processing never
// claimed to ProcessIncomingReply, once at boot and then daily.
//
// A reply the gates refused before claiming left no mark: the message sat in
// the unibox, the lead stayed "not replied", and nothing ever came back for
// it. Replies into IMAP mailboxes were refused this way for a week because the
// sync stored addresses in a form the checks could not read. Re-running the
// same handler is safe: it claims each message exactly once, stamps replied_at
// only if it still is not, and ordinary mail still ends where it ended before.
func (s *JobsService) StartIncomingReplyRepair(ctx context.Context) {
	if s.UniboxRepository == nil || s.AdvancedService == nil {
		return
	}
	var afterID uuid.UUID
	var nextPass time.Time
	jobrun.Loop(ctx, "incoming_reply_repair", time.Minute, true, func(ctx context.Context) error {
		if time.Now().Before(nextPass) {
			return nil
		}
		batchCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		next, done, err := s.repairIncomingReplyBatch(batchCtx, afterID)
		afterID = next
		if err == nil && done {
			afterID = uuid.Nil
			nextPass = time.Now().Add(24 * time.Hour)
		}
		return err
	})
}

func (s *JobsService) repairIncomingReplyBatch(ctx context.Context, afterID uuid.UUID) (uuid.UUID, bool, error) {
	const batchSize = 100
	events, err := s.UniboxRepository.ListUnprocessedCampaignReplies(ctx, time.Now().Add(-replyRepairWindow), afterID, batchSize)
	if err != nil {
		return afterID, false, err
	}
	for _, e := range events {
		if e.Message == nil {
			continue
		}
		if xerr := s.AdvancedService.ProcessIncomingReply(ctx, e.Message.EmailID, e.Message); xerr != nil {
			// Logged and skipped, like the live path: one message that cannot
			// be processed must not stall the rest of the sweep.
			log.Warn().Err(xerr).
				Str("email_account_id", e.Message.EmailID.String()).
				Str("message_id", e.Message.MessageID).
				Msg("incoming reply repair: reply processing failed")
		}
		afterID = e.Message.ID
	}
	return afterID, len(events) < batchSize, nil
}

// StartReplyOptOutRecheck re-reads, once, every suppression a reply opt-out
// wrote before the opt-out was limited to people answering our outreach, and
// lifts the ones a bounce, a newsletter or a quoted footer produced. Each
// entry records its outcome, so a later pass only reads new ones.
func (s *JobsService) StartReplyOptOutRecheck(ctx context.Context) {
	if s.AdvancedService == nil {
		return
	}
	var afterID uuid.UUID
	var nextPass time.Time
	jobrun.Loop(ctx, "reply_optout_recheck", time.Minute, true, func(ctx context.Context) error {
		if time.Now().Before(nextPass) {
			return nil
		}
		batchCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		// Small pages: each entry reads its sender's mail across the workspace.
		next, done, err := s.AdvancedService.RecheckReplyOptOuts(batchCtx, afterID, 25)
		afterID = next
		if err == nil && done {
			afterID = uuid.Nil
			nextPass = time.Now().Add(24 * time.Hour)
		}
		return err
	})
}
