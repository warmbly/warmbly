package jobs

import (
	"context"
	"encoding/binary"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/bitmask"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// Reply pacing is stable across receipt redeliveries.
const (
	replyBackMinDelay = 45 * time.Minute
	replyBackMaxDelay = 5 * time.Hour
)

func (s *JobsService) scheduleWarmupReplyBack(ctx context.Context, token *models.WarmupToken, recipientAccountID uuid.UUID, receivedIDs ...uuid.UUID) error {
	lineage, ok := s.TaskRepo.(repository.WarmupLineageRepository)
	if !ok || len(receivedIDs) != 1 {
		return nil
	}
	if s.TaskRepo == nil || s.EmailRepository == nil || token == nil {
		return nil
	}

	recipient, xerr := s.EmailRepository.GetByID(ctx, recipientAccountID)
	if xerr != nil {
		return xerr
	}
	if recipient == nil {
		return nil
	}
	// Not actively warming means the monitor lane at a cap of five; pulling that
	// work forward would spend the budget on a reply.
	if !recipient.IsWarmingActive() {
		return nil
	}

	if int(binary.BigEndian.Uint32(token.Token[:4])%100) >= recipient.WarmupReplyRate {
		return nil
	}

	at := time.Now().Add(replyBackMinDelay +
		time.Duration(uint64(binary.BigEndian.Uint32(token.Token[4:8]))%uint64(replyBackMaxDelay-replyBackMinDelay)))
	// A reply at 04:00 from a mailbox that never sends then is worse than none.
	at = withinWarmupHours(at, recipient)

	moved, err := lineage.BindWarmupSuccessor(ctx, token.TaskID, recipientAccountID, receivedIDs[0], at)
	if err != nil {
		log.Warn().Err(err).Str("email_id", recipientAccountID.String()).Msg("could not schedule the warmup reply-back")
		return err
	}
	if moved {
		log.Debug().
			Str("email_id", recipientAccountID.String()).
			Str("replying_to", token.SenderAccountID.String()).
			Time("at", at).
			Msg("warmup reply-back scheduled")
	}
	return nil
}

// withinWarmupHours moves t into the mailbox's warmup window, rolling to the
// next day's opening when it falls past the close.
func withinWarmupHours(t time.Time, account *models.Email) time.Time {
	loc := time.UTC
	if zone := account.ClockTimezone(); zone != "" {
		if l, err := time.LoadLocation(zone); err == nil {
			loc = l
		}
	}
	start := models.ClockMinutes(account.WarmupStartTime, 8*60)
	end := models.ClockMinutes(account.WarmupEndTime, 20*60)
	if end <= start {
		return t
	}

	openAt := func(d time.Time) time.Time {
		return time.Date(d.Year(), d.Month(), d.Day(), start/60, start%60, 0, 0, loc)
	}
	local := t.In(loc)
	for account.WarmupDays > 0 && !bitmask.HasWeekday(uint8(account.WarmupDays), local.Weekday()) {
		local = openAt(local.AddDate(0, 0, 1))
		t = local
	}

	switch mins := local.Hour()*60 + local.Minute(); {
	case mins < start:
		return openAt(local)
	case mins >= end:
		return withinWarmupHours(openAt(local.AddDate(0, 0, 1)), account)
	default:
		return t
	}
}
