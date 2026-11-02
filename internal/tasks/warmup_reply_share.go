package tasks

import (
	"context"
	"math/rand"
	"sort"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/app/warmupramp"
	"github.com/warmbly/warmbly/internal/repository"
)

// owedReplyCandidates bounds how much received mail one send considers answering.
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
	owed, err := share.OwedWarmupReplies(ctx, account.ID, owedReplyCandidates)
	if err != nil || len(owed) == 0 {
		return nil
	}
	signals := warmupramp.RecoverySignals{}
	if s.warmupRepo != nil {
		signals.Health = s.warmupRepo
	}
	if s.emailRepo != nil {
		signals.Mailboxes = s.emailRepo
	}
	if s.campaignRepo != nil {
		signals.Campaigns = s.campaignRepo
	}
	recovering := make(map[uuid.UUID]bool, len(owed))
	for _, o := range owed {
		if _, seen := recovering[o.SenderID]; !seen {
			recovering[o.SenderID] = signals.Recovering(ctx, o.SenderID)
		}
	}
	sort.SliceStable(owed, func(i, j int) bool { return recovering[owed[i].SenderID] && !recovering[owed[j].SenderID] })

	for _, o := range owed {
		if _, _, _, err := s.exactReplyContent(ctx, &o.Parent, *account); err != nil {
			continue
		}
		partner := s.replyPartner(ctx, taskID, o.SenderID, account, poolType)
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
	return nil
}
