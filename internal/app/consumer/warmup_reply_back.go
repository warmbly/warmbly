package jobs

import (
	"context"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/models"
)

// A reply lands in this range, so it reads as someone getting to their inbox
// rather than a bot acknowledging receipt.
const (
	replyBackMinDelay = 25 * time.Minute
	replyBackMaxDelay = 5 * time.Hour
)

// scheduleWarmupReplyBack occasionally points the RECIPIENT's next warmup send
// back at the sender. It re-points an already-pending task and only ever pulls
// it earlier, so budgets and health gating are untouched.
func (s *JobsService) scheduleWarmupReplyBack(ctx context.Context, token *models.WarmupToken, recipientAccountID uuid.UUID) {
	if s.TaskRepo == nil || s.EmailRepository == nil || token == nil {
		return
	}

	// Without this a reply-back could answer a reply-back forever by starting a
	// fresh thread each time.
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
	// Not actively warming means the monitor lane at a cap of five; pulling that
	// work forward would spend the budget on a reply.
	if !recipient.IsWarmingActive() {
		return
	}

	if rand.Float64()*100 >= float64(recipient.WarmupReplyRate) {
		return
	}

	at := time.Now().Add(replyBackMinDelay +
		time.Duration(rand.Int63n(int64(replyBackMaxDelay-replyBackMinDelay))))
	// A reply at 04:00 from a mailbox that never sends then is worse than none.
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

// withinWarmupHours moves t into the mailbox's warmup window, rolling to the
// next day's opening when it falls past the close.
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
	// Jitter is capped to the window, or a mailbox with a short window (say
	// 09:00-09:20) would be scheduled past its own close.
	jitter := func(maxMinutes int) time.Duration {
		if width := end - start; maxMinutes > width {
			maxMinutes = width
		}
		if maxMinutes <= 0 {
			return 0
		}
		return time.Duration(rand.Intn(maxMinutes)) * time.Minute
	}

	local := t.In(loc)
	openAt := func(d time.Time) time.Time {
		return time.Date(d.Year(), d.Month(), d.Day(), start/60, start%60, 0, 0, loc)
	}

	switch mins := local.Hour()*60 + local.Minute(); {
	case mins < start:
		return openAt(local).Add(jitter(30))
	case mins > end:
		return openAt(local.AddDate(0, 0, 1)).Add(jitter(60))
	default:
		return t
	}
}
