package jobs

import (
	"context"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/models"
)

const (
	// A reply lands between these, so an answer reads as someone getting to
	// their inbox rather than a bot acknowledging receipt.
	replyBackMinDelay = 25 * time.Minute
	replyBackMaxDelay = 5 * time.Hour
)

// scheduleWarmupReplyBack occasionally points the RECIPIENT's next warmup send
// back at the sender, so a thread continues as a conversation instead of two
// mailboxes monologuing on their own ramps.
//
// It only ever pulls an already-pending task earlier and re-points it; it never
// creates work, so a mailbox's daily budget and health gating are untouched.
func (s *JobsService) scheduleWarmupReplyBack(ctx context.Context, token *models.WarmupToken, recipientAccountID uuid.UUID) {
	if s.TaskRepo == nil || s.EmailRepository == nil || token == nil {
		return
	}

	// Depth bound. The thread cap already stops a single conversation running
	// away, but without this a reply-back could answer a reply-back forever by
	// starting a fresh thread each time.
	settings := s.getGenerationSettings(ctx)
	maxTurns := settings.MaxMessagesPerThread
	if maxTurns <= 0 {
		maxTurns = 5
	}
	if token.ConversationTurn+1 >= maxTurns {
		return
	}

	recipient, xerr := s.EmailRepository.GetByID(ctx, recipientAccountID)
	if xerr != nil || recipient == nil {
		return
	}
	// A mailbox that is not actively warming is in the monitor lane at a cap of
	// five; pulling its work forward would spend that budget on a reply.
	if !recipient.IsWarmingActive() {
		return
	}

	if rand.Float64()*100 >= float64(recipient.WarmupReplyRate) {
		return
	}

	at := time.Now().Add(replyBackMinDelay +
		time.Duration(rand.Int63n(int64(replyBackMaxDelay-replyBackMinDelay))))
	// Keep it inside the recipient's own warmup hours: a reply arriving at
	// 04:00 from a mailbox that never sends then is worse than no reply.
	at = withinWarmupHours(at, recipient)

	moved, err := s.TaskRepo.DirectPendingWarmupTask(ctx, recipientAccountID, token.SenderAccountID, at)
	if err != nil {
		log.Warn().Err(err).Str("email_id", recipientAccountID.String()).Msg("could not schedule the warmup reply-back")
		return
	}
	if moved {
		log.Debug().
			Str("email_id", recipientAccountID.String()).
			Str("replying_to", token.SenderAccountID.String()).
			Time("at", at).
			Msg("warmup reply-back scheduled")
	}
}

// withinWarmupHours moves t into the mailbox's configured warmup window,
// rolling to the next day's opening when it falls past the close.
func withinWarmupHours(t time.Time, account *models.Email) time.Time {
	loc := time.UTC
	if account.Timezone != "" {
		if l, err := time.LoadLocation(account.Timezone); err == nil {
			loc = l
		}
	}
	start := models.ClockMinutes(account.WarmupStartTime, 8*60)
	end := models.ClockMinutes(account.WarmupEndTime, 20*60)
	if end <= start {
		return t
	}

	local := t.In(loc)
	mins := local.Hour()*60 + local.Minute()
	switch {
	case mins < start:
		return time.Date(local.Year(), local.Month(), local.Day(), start/60, start%60, 0, 0, loc).
			Add(time.Duration(rand.Intn(30)) * time.Minute)
	case mins > end:
		next := local.AddDate(0, 0, 1)
		return time.Date(next.Year(), next.Month(), next.Day(), start/60, start%60, 0, 0, loc).
			Add(time.Duration(rand.Intn(60)) * time.Minute)
	default:
		return t
	}
}
