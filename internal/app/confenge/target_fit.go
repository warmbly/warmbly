package confenge

import (
	"fmt"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

const (
	TargetFitConfirmed         = "TARGET_CONFIRMED"
	TargetFitProbableResearch  = "TARGET_PROBABLE_RESEARCH"
	TargetFitOutOfScope        = "TARGET_OUT_OF_SCOPE"
	TargetFitRefreshFailed     = "REFRESH_FAILED"
	TargetFitRecomputeRequired = "RECOMPUTE_REQUIRED"

	TargetFitReasonOut         = "TARGET_FIT_OUT"
	TargetFitReasonMissing     = "TARGET_FIT_MISSING"
	TargetFitReasonStale       = "TARGET_FIT_STALE"
	TargetFitReasonDowngraded  = "TARGET_FIT_DOWNGRADED"
	TargetFitReasonDeactivated = "UPSTREAM_DEACTIVATED"
)

type TargetFitAuthorization struct {
	Eligible bool
	Reason   string
}

func EvaluateTargetFit(acc *models.OutreachAccount) TargetFitAuthorization {
	if acc == nil {
		return TargetFitAuthorization{Reason: TargetFitReasonMissing}
	}
	class := strings.ToUpper(strings.TrimSpace(acc.TargetFitClass))
	if class == "" || strings.TrimSpace(acc.TargetFitVersion) == "" ||
		strings.TrimSpace(acc.TargetFitSourceWatermark) == "" || acc.TargetFitObservedAt == nil {
		return TargetFitAuthorization{Reason: TargetFitReasonMissing}
	}
	if !acc.TargetFitFresh {
		return TargetFitAuthorization{Reason: TargetFitReasonStale}
	}
	if class != TargetFitConfirmed {
		return TargetFitAuthorization{Reason: TargetFitReasonOut}
	}
	tier := strings.ToUpper(strings.TrimSpace(acc.TargetFitSendTier))
	if tier != "" && !ImportedSendTierEligible(tier) {
		return TargetFitAuthorization{Reason: TargetFitReasonDowngraded}
	}
	return TargetFitAuthorization{Eligible: true}
}

func LeadTargetFitDecision(lead FeedLead) TargetFitAuthorization {
	acc := &models.OutreachAccount{
		TargetFitClass: lead.TargetFitClass, TargetFitVersion: lead.TargetFitVersion,
		TargetFitSourceWatermark: lead.TargetFitSourceWatermark,
		TargetFitObservedAt:      firstTargetFitTime(lead.TargetFitSourceWatermark, lead.TargetFitComputedAt),
		TargetFitFresh:           lead.TargetFitFresh != nil && *lead.TargetFitFresh,
		TargetFitSendTier:        lead.TargetFitSendTier,
	}
	return EvaluateTargetFit(acc)
}

func RequireTargetFit(acc *models.OutreachAccount) error {
	d := EvaluateTargetFit(acc)
	if !d.Eligible {
		return fmt.Errorf("commercial outreach blocked: %s", d.Reason)
	}
	return nil
}

func RequireEmailOutbound(acc *models.OutreachAccount, cand *models.OutreachContactCandidate) error {
	if err := RequireTargetFit(acc); err != nil {
		return err
	}
	if acc.DoNotContact || acc.Blocked {
		return fmt.Errorf("account blocked or DNC")
	}
	if !acc.EmailSendReady {
		return fmt.Errorf("company EMAIL_SEND_READY is false")
	}
	if cand == nil {
		return fmt.Errorf("contact candidate is missing")
	}
	if !cand.CanEnroll() {
		return fmt.Errorf("contact is not enrollable")
	}
	if !cand.EmailSendReady {
		return fmt.Errorf("contact EMAIL_SEND_READY is false")
	}
	if cand.MailboxPurposeSendBlocked {
		return fmt.Errorf("contact mailbox purpose blocks send")
	}
	return nil
}

func hasSendReadyEmailIgnoringTargetFit(acc *models.OutreachAccount, candidates []models.OutreachContactCandidate) bool {
	if acc == nil || acc.DoNotContact || acc.Blocked || !acc.EmailSendReady {
		return false
	}
	for i := range candidates {
		cand := &candidates[i]
		if cand.CanEnroll() && cand.EmailSendReady && !cand.MailboxPurposeSendBlocked {
			return true
		}
	}
	return false
}

func firstTargetFitTime(values ...string) *time.Time {
	for _, value := range values {
		if t := parseTimePtr(value); t != nil {
			return t
		}
	}
	return nil
}

func TargetFitMayReplace(current, incoming *models.OutreachAccount) bool {
	if current == nil {
		return true
	}
	if incoming == nil || incoming.TargetFitObservedAt == nil {
		return current.TargetFitObservedAt == nil && current.TargetFitClass == ""
	}
	if current.TargetFitObservedAt == nil {
		return true
	}
	if incoming.TargetFitObservedAt.After(*current.TargetFitObservedAt) {
		return true
	}
	if incoming.TargetFitObservedAt.Before(*current.TargetFitObservedAt) {
		return false
	}
	// Equal watermarks may only move toward the more restrictive decision.
	return !EvaluateTargetFit(incoming).Eligible || EvaluateTargetFit(current).Eligible
}

func copyTargetFit(dst, src *models.OutreachAccount) {
	dst.TargetFitSendTier, dst.TargetFitReasons = src.TargetFitSendTier, append([]string{}, src.TargetFitReasons...)
	dst.TargetFitClass, dst.TargetFitConfidence = src.TargetFitClass, src.TargetFitConfidence
	dst.TargetFitVersion, dst.TargetFitComputedAt = src.TargetFitVersion, src.TargetFitComputedAt
	dst.TargetFitSourceWatermark, dst.TargetFitObservedAt = src.TargetFitSourceWatermark, src.TargetFitObservedAt
	dst.TargetFitFresh = src.TargetFitFresh
	dst.TargetFitEvidenceIDs = append([]string{}, src.TargetFitEvidenceIDs...)
	dst.TargetFitFreshnessReason = src.TargetFitFreshnessReason
	dst.TargetFitEligible, dst.TargetFitSuppressionReason = src.TargetFitEligible, src.TargetFitSuppressionReason
}

func isHistoricalTerminalQueue(state string) bool {
	switch state {
	case models.OutreachQueueSent, models.OutreachQueueReplied, models.OutreachQueueMeeting,
		models.OutreachQueueProposal, models.OutreachQueueWon, models.OutreachQueueLost,
		models.OutreachQueueDoNotContact, models.OutreachQueueBlocked, models.OutreachQueueBounced:
		return true
	}
	return false
}
