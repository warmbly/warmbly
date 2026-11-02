package tasks

import (
	"context"
	"math/rand"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/warmupramp"
	"github.com/warmbly/warmbly/internal/repository"
)

// owedReplyCandidates bounds each batch, not the total mail considered for a send.
const owedReplyCandidates = 8

// owedWarmupReply makes a claimed plain send the reply to received warmup mail
// when the mailbox's reply share is behind its setting, answering recovering
// senders first. Nil leaves the send a new thread.
func (s *tasksService) owedWarmupReply(ctx context.Context, taskID uuid.UUID, account *Email, poolType string) *Email {
	if account == nil || account.WarmupReplyRate <= 0 || !account.IsWarmingActive() {
		return nil
	}
	share, ok := s.taskRepo.(repository.WarmupReplyShareRepository)
	if !ok {
		return nil
	}
	sent, replied, err := share.WarmupReplyShare(ctx, account.ID)
	if err != nil || !warmupramp.ReplyDue(warmupramp.ReplyDeficit(account.WarmupReplyRate, sent, replied), rand.Float64()) {
		return nil
	}
	excluded := repository.WarmupReplyExclusions{}
	partners := make(map[uuid.UUID]*Email)
	for ctx.Err() == nil {
		owed, err := share.OwedWarmupReplies(ctx, account.ID, owedReplyCandidates, excluded)
		if err != nil || len(owed) == 0 {
			return nil
		}
		for _, o := range owed {
			excluded.Parents = append(excluded.Parents, o.ParentTaskID)
			if _, _, _, err := s.exactReplyContent(ctx, &o.Parent, *account); err != nil {
				continue
			}
			partner, checked := partners[o.SenderID]
			if !checked {
				partner = s.replyPartner(ctx, taskID, o.SenderID, account, poolType)
				partners[o.SenderID] = partner
				if partner == nil {
					excluded.Senders = append(excluded.Senders, o.SenderID)
				}
			}
			if partner == nil {
				continue
			}
			bound, err := share.BindClaimedWarmupReply(ctx, taskID, account.ID, o.ParentTaskID, o.ReceivedID)
			if err != nil {
				log.Warn().Err(err).Str("task_id", taskID.String()).Str("email_account_id", account.ID.String()).Msg("warmup reply share: could not bind an owed reply")
				return nil
			}
			if bound {
				return partner
			}
		}
		if len(owed) < owedReplyCandidates {
			return nil
		}
	}
	return nil
}
