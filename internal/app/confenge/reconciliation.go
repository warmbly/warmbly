package confenge

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type TargetFitReconciliationReport struct {
	DryRun               bool           `json:"dry_run"`
	AccountsScanned      int            `json:"accounts_scanned"`
	BeforeOperational    int            `json:"before_operational"`
	AfterOperational     int            `json:"after_operational"`
	AccountsChanged      int            `json:"accounts_changed"`
	SuppressedByReason   map[string]int `json:"suppressed_by_reason"`
	CancelledTouchpoints int            `json:"cancelled_touchpoints"`
	BlockedDrafts        int            `json:"blocked_drafts"`
	DetachedEnrollments  int            `json:"detached_enrollments"`
	CancelledDispatch    int            `json:"cancelled_dispatch_items"`
}

func (s *service) ReconcileTargetFit(ctx context.Context, orgID uuid.UUID, dryRun bool) (*TargetFitReconciliationReport, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	report := &TargetFitReconciliationReport{DryRun: dryRun, SuppressedByReason: map[string]int{}}
	var accounts []models.OutreachAccount
	for offset := 0; ; offset += 1000 {
		batch, err := s.repo.ListAccounts(ctx, orgID, repository.OutreachAccountFilter{Limit: 1000, Offset: offset, StableOrder: true})
		if err != nil {
			return nil, errx.New(errx.Internal, "target-fit reconciliation scan: "+err.Error())
		}
		accounts = append(accounts, batch...)
		if len(batch) < 1000 {
			break
		}
	}
	for i := range accounts {
		acc := &accounts[i]
		report.AccountsScanned++
		candidates, err := s.repo.ListCandidates(ctx, orgID, acc.ID)
		if err != nil {
			return nil, errx.New(errx.Internal, "target-fit reconciliation contacts: "+err.Error())
		}
		contactReady := hasSendReadyEmailIgnoringTargetFit(acc, candidates)
		if acc.TargetFitEligible && contactReady {
			report.BeforeOperational++
		}
		decision := EvaluateTargetFit(acc)
		if decision.Eligible {
			if contactReady {
				report.AfterOperational++
			}
		} else {
			report.SuppressedByReason[decision.Reason]++
		}
		changed := acc.TargetFitEligible != decision.Eligible || acc.TargetFitSuppressionReason != decision.Reason
		if !decision.Eligible && !isHistoricalTerminalQueue(acc.QueueState) && acc.QueueState != models.OutreachQueueTargetFitSuppressed {
			changed = true
		}
		if decision.Eligible && acc.QueueState == models.OutreachQueueTargetFitSuppressed {
			changed = true
		}
		if changed {
			report.AccountsChanged++
		}
		if dryRun {
			continue
		}
		acc.TargetFitEligible = decision.Eligible
		acc.TargetFitSuppressionReason = decision.Reason
		now := time.Now().UTC()
		acc.TargetFitReconciledAt = &now
		if !decision.Eligible && !isHistoricalTerminalQueue(acc.QueueState) {
			acc.QueueState = models.OutreachQueueTargetFitSuppressed
		} else if decision.Eligible && acc.QueueState == models.OutreachQueueTargetFitSuppressed {
			acc.QueueState = models.OutreachQueueNeedsContact
			for j := range candidates {
				if RequireEmailOutbound(acc, &candidates[j]) == nil {
					acc.QueueState = models.OutreachQueueReadyToGenerate
					break
				}
			}
		}
		if _, err := s.repo.UpsertAccount(ctx, acc); err != nil {
			return nil, errx.New(errx.Internal, "target-fit reconciliation account: "+err.Error())
		}
		if !decision.Eligible {
			counts, err := s.repo.InvalidateAccountOutboundForTargetFit(ctx, orgID, acc.ID, decision.Reason)
			if err != nil {
				return nil, errx.New(errx.Internal, "target-fit outbound invalidation: "+err.Error())
			}
			report.CancelledTouchpoints += counts.Touchpoints
			report.BlockedDrafts += counts.Drafts
			report.DetachedEnrollments += counts.Enrollments
			report.CancelledDispatch += counts.DispatchItems
		}
	}
	return report, nil
}
