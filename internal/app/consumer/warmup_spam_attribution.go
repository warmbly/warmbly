package jobs

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/config"
	"github.com/warmbly/warmbly/internal/jobrun"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	spamMoveAttributionInterval = 5 * time.Minute
	spamMoveAttributionBatch    = 200
	// spamMoveClaimLease outlives one move's work; a consumer that dies holding it frees it on expiry.
	spamMoveClaimLease = 5 * time.Minute
)

// Evidence names stored with each verdict, so an operator can read why.
const (
	spamMoveCorrelated   = "correlated"
	spamMoveOnArrival    = "on_arrival"
	spamMoveOwnerActive  = "owner_active"
	spamMoveRepeated     = "repeated"
	spamMoveDormant      = "dormant"
	spamMoveNoOwnerTrace = "no_owner_activity"
)

// Attribution is inferred, not provider proof; recipient withdrawal is not tampering.
func attributeSpamMove(m repository.WarmupSpamMove, ev repository.WarmupSpamMoveEvidence) (string, []string) {
	quick := m.ObservedAt.Sub(m.ReceivedAt) < time.Duration(config.WarmupSpamMoveQuickMinutes)*time.Minute
	switch {
	case ev.CorrelatedElsewhere > 0:
		return repository.SpamMoveProvider, []string{spamMoveCorrelated}
	case quick && !ev.OwnerActiveNear:
		return repository.SpamMoveProvider, []string{spamMoveOnArrival}
	case ev.OwnerActiveNear:
		return repository.SpamMoveOwner, []string{spamMoveOwnerActive}
	case !ev.OwnerActiveRecently:
		return repository.SpamMoveUnattributed, []string{spamMoveDormant}
	case ev.PatternSenders >= config.WarmupSpamMovePatternSenders:
		return repository.SpamMoveOwner, []string{spamMoveRepeated}
	}
	return repository.SpamMoveUnattributed, []string{spamMoveNoOwnerTrace}
}

// StartWarmupSpamMoveAttribution decides each held spam move once it has settled.
func (s *JobsService) StartWarmupSpamMoveAttribution(ctx context.Context) {
	if s.WarmupRepo == nil || s.WarmupService == nil {
		return
	}
	jobrun.Loop(ctx, "warmup_spam_move_attribution", spamMoveAttributionInterval, true, func(ctx context.Context) error {
		batchCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		return s.attributeSpamMoves(batchCtx, time.Now())
	})
}

func (s *JobsService) attributeSpamMoves(ctx context.Context, now time.Time) error {
	settled := now.Add(-time.Duration(config.WarmupSpamMoveSettleMinutes) * time.Minute)
	moves, err := s.WarmupRepo.ListSettledWarmupSpamMoves(ctx, settled, spamMoveAttributionBatch)
	if err != nil {
		return fmt.Errorf("list warmup spam moves: %w", err)
	}
	for _, m := range moves {
		if err := s.attributeOneSpamMove(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

// attributeOneSpamMove claims the move, fixes its verdict once and applies it.
// Effects are idempotent and the move is completed only after all of them, so
// a failure part way re-applies the same verdict on a later pass.
func (s *JobsService) attributeOneSpamMove(ctx context.Context, m repository.WarmupSpamMove) error {
	claimed, err := s.WarmupRepo.ClaimWarmupSpamMove(ctx, m.EmailAccountID, m.MessageID, spamMoveClaimLease)
	if err != nil {
		return fmt.Errorf("claim warmup spam move: %w", err)
	}
	if !claimed {
		return nil
	}

	verdict, signals := m.Verdict, m.Signals
	if verdict == repository.SpamMovePending {
		ev, err := s.WarmupRepo.WarmupSpamMoveEvidence(ctx, m)
		if err != nil {
			return fmt.Errorf("warmup spam move evidence: %w", err)
		}
		verdict, signals = attributeSpamMove(m, ev)
		fixed, err := s.WarmupRepo.FixWarmupSpamMoveVerdict(ctx, m.EmailAccountID, m.MessageID, verdict, signals)
		if err != nil {
			return fmt.Errorf("fix warmup spam move verdict: %w", err)
		}
		if !fixed {
			return nil
		}
	}

	if verdict == repository.SpamMoveOwner {
		hSender, xerr := s.WarmupService.ApplySpamReport(ctx, m.EmailAccountID, m.SenderAccountID, m.MessageID, "user_complaint")
		if xerr != nil {
			return fmt.Errorf("record warmup spam complaint: %w", xerr)
		}
		s.markRiskBandFromWarmupHealth(ctx, m.SenderAccountID, hSender)
	} else {
		// The provider junked the sender's mail, or may have: a placement
		// reading against the sender, which only ever slows it down.
		provider, domain := recipientProviderDomain(s.recipientAccount(ctx, m.EmailAccountID))
		hSender, xerr := s.WarmupService.RecordSpamPlacement(ctx, m.EmailAccountID, m.SenderAccountID, m.MessageID, "", provider, domain)
		if xerr != nil {
			return fmt.Errorf("record warmup spam placement: %w", xerr)
		}
		s.markRiskBandFromWarmupHealth(ctx, m.SenderAccountID, hSender)
	}

	if slices.Contains(signals, spamMoveCorrelated) {
		if err := s.withdrawCorrelatedOwnerMoves(ctx, m); err != nil {
			return err
		}
	}

	if err := s.WarmupRepo.CompleteWarmupSpamMove(ctx, m.EmailAccountID, m.MessageID); err != nil {
		return fmt.Errorf("complete warmup spam move: %w", err)
	}
	log.Info().Str("email_id", m.EmailAccountID.String()).Str("verdict", verdict).Strs("signals", signals).
		Msg("warmup spam move attributed")
	return nil
}

// withdrawCorrelatedOwnerMoves takes back the strikes charged for the same
// sender's mail in other workspaces before this move showed the provider at work.
func (s *JobsService) withdrawCorrelatedOwnerMoves(ctx context.Context, m repository.WarmupSpamMove) error {
	owners, err := s.WarmupRepo.CorrelatedOwnerSpamMoves(ctx, m.SenderAccountID, m.EmailAccountID, m.ObservedAt)
	if err != nil {
		return fmt.Errorf("correlated spam moves: %w", err)
	}
	for _, o := range owners {
		health, xerr := s.WarmupService.WithdrawTampering(ctx, o.EmailAccountID, o.MessageID, "spam_flag")
		if xerr != nil {
			return fmt.Errorf("withdraw correlated spam strike: %w", xerr)
		}
		s.markRiskBandFromWarmupHealth(ctx, o.EmailAccountID, health)
	}
	if len(owners) == 0 {
		return nil
	}
	if err := s.WarmupRepo.ReattributeOwnerSpamMoves(ctx, m.SenderAccountID, m.EmailAccountID, m.ObservedAt); err != nil {
		return fmt.Errorf("reattribute correlated spam moves: %w", err)
	}
	return nil
}
