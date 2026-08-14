package confenge

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

const (
	// Risk / stop reason stamped on prior-composer invalidation.
	FlagComposerStale        = "composer_version_stale"
	FlagRequiresRegen        = "requires_regeneration"
	StopComposerStale        = "composer_version_stale"
	AuditReasonComposerStale = "prior_composer_invalidated"
)

// Unsent draft statuses that a prior composer may have left in the human queue.
var priorComposerDraftStatuses = []string{
	models.OutreachDraftNeedsReview,
	models.OutreachDraftApproved,
	models.OutreachDraftGenerating,
}

// Unsent touchpoint states that must not remain implicitly valid.
var priorComposerTouchStates = []string{
	models.TouchpointNeedsReview,
	models.TouchpointApproved,
	models.TouchpointDrafted,
	models.TouchpointQueued,
	models.TouchpointDue,
	models.TouchpointPlanned,
}

// DraftInvalidationReport is the auditable result of one invalidation pass.
type DraftInvalidationReport struct {
	DraftsSeen             int `json:"drafts_seen"`
	DraftsInvalidated      int `json:"drafts_invalidated"`
	ApprovalsRevoked       int `json:"approvals_revoked"`
	TouchpointsSeen        int `json:"touchpoints_seen"`
	TouchpointsInvalidated int `json:"touchpoints_invalidated"`
	SentLeftUntouched      int `json:"sent_left_untouched"`
	AlreadyCurrent         int `json:"already_current"`
}

// CurrentComposerPrompt reports whether a stored prompt version is the
// current composer (v4 and optional +touch / .reply suffixes).
func CurrentComposerPrompt(promptVersion string) bool {
	base := strings.TrimSpace(promptVersion)
	if i := strings.Index(base, "+"); i >= 0 {
		base = base[:i]
	}
	return base == PromptVersion
}

// IsPriorComposerDraft is true for unsent drafts stamped before this composer.
func IsPriorComposerDraft(d *models.OutreachDraft) bool {
	if d == nil {
		return false
	}
	switch d.Status {
	case models.OutreachDraftSent, models.OutreachDraftReplied, models.OutreachDraftEnrolled:
		return false
	}
	if CurrentComposerPrompt(d.PromptVersion) {
		return false
	}
	// Empty version is treated as prior (legacy rows).
	return true
}

// IsSentDraft is true when the row is a completed send. Never mutate these.
func IsSentDraft(d *models.OutreachDraft) bool {
	if d == nil {
		return false
	}
	switch d.Status {
	case models.OutreachDraftSent, models.OutreachDraftReplied:
		return true
	}
	return false
}

// InvalidatePriorComposerDrafts marks unsent prior-version drafts/touchpoints
// for regeneration. SENT is never touched. APPROVED-unsent is explicitly
// revoked with an audit record. Idempotent: current-version rows are skipped.
func (s *service) InvalidatePriorComposerDrafts(ctx context.Context, orgID, actorID uuid.UUID) (*DraftInvalidationReport, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	rep := &DraftInvalidationReport{}
	now := time.Now().UTC()

	for _, st := range priorComposerDraftStatuses {
		list, err := s.repo.ListDrafts(ctx, orgID, st, 500, 0)
		if err != nil {
			return nil, errx.New(errx.Internal, "list drafts: "+err.Error())
		}
		for i := range list {
			d := list[i]
			rep.DraftsSeen++
			if IsSentDraft(&d) {
				rep.SentLeftUntouched++
				continue
			}
			if !IsPriorComposerDraft(&d) {
				rep.AlreadyCurrent++
				continue
			}
			revoked := d.Status == models.OutreachDraftApproved && (d.ApprovedBy != nil || d.ApprovedAt != nil)
			d.ApprovedBy = nil
			d.ApprovedAt = nil
			d.CampaignID = nil
			d.EnrollmentContactID = nil
			d.EnrolledAt = nil
			d.Status = models.OutreachDraftSkipped
			d.RiskFlags = appendUnique(d.RiskFlags, FlagComposerStale, FlagRequiresRegen)
			d.UpdatedAt = now
			if err := s.repo.UpdateDraftStatus(ctx, &d); err != nil {
				return nil, errx.New(errx.Internal, "update draft: "+err.Error())
			}
			rep.DraftsInvalidated++
			if revoked {
				rep.ApprovalsRevoked++
			}
			if s.audit != nil {
				accID := d.AccountID
				s.audit.LogAction(ctx, orgID, actorID, models.AuditActionUpdate, models.AuditEntityOutreachAccount, &accID, "", "",
					map[string]string{
						"draft_id": d.ID.String(),
						"action":   AuditReasonComposerStale,
						"from":     st,
						"to":       models.OutreachDraftSkipped,
					},
					map[string]string{
						"prompt_version":   d.PromptVersion,
						"current_prompt":   PromptVersion,
						"approval_revoked": boolString(revoked),
						"composer_version": ComposerVersion,
					},
				)
			}
			if acc, _ := s.repo.GetAccount(ctx, orgID, d.AccountID); acc != nil {
				switch acc.QueueState {
				case models.OutreachQueueNeedsReview, models.OutreachQueueApproved:
					_ = s.repo.SetAccountHumanFlags(ctx, orgID, acc.ID, acc.Blocked, acc.DoNotContact, acc.BlockReason, models.OutreachQueueReadyToGenerate)
				}
			}
		}
	}

	accounts := map[uuid.UUID]bool{}
	for _, qs := range []string{
		models.OutreachQueueNeedsReview,
		models.OutreachQueueApproved,
		models.OutreachQueueReadyToGenerate,
		models.OutreachQueueNeedsContact,
		models.OutreachQueueEnrolled,
	} {
		accList, err := s.repo.ListAccounts(ctx, orgID, repository.OutreachAccountFilter{QueueState: qs, Limit: 500})
		if err != nil {
			return nil, errx.New(errx.Internal, "list accounts: "+err.Error())
		}
		for _, a := range accList {
			accounts[a.ID] = true
		}
	}
	for accID := range accounts {
		for _, stt := range priorComposerTouchStates {
			tps, err := s.repo.ListTouchpoints(ctx, orgID, accID, stt, 50, 0)
			if err != nil {
				continue
			}
			for i := range tps {
				tp := tps[i]
				rep.TouchpointsSeen++
				if tp.State == models.TouchpointSent {
					rep.SentLeftUntouched++
					continue
				}
				stale := true
				if tp.DraftID != nil {
					if d, _ := s.repo.GetDraft(ctx, orgID, *tp.DraftID); d != nil {
						if IsSentDraft(d) {
							rep.SentLeftUntouched++
							continue
						}
						stale = IsPriorComposerDraft(d)
					}
				}
				if !stale {
					rep.AlreadyCurrent++
					continue
				}
				ClearApproval(&tp)
				tp.State = models.TouchpointCancelled
				tp.StopReason = StopComposerStale
				tp.UpdatedAt = now
				if err := s.repo.UpdateTouchpoint(ctx, &tp); err != nil {
					return nil, errx.New(errx.Internal, "update touchpoint: "+err.Error())
				}
				rep.TouchpointsInvalidated++
				if s.audit != nil {
					id := tp.ID
					s.audit.LogAction(ctx, orgID, actorID, models.AuditActionUpdate, models.AuditEntityOutreachAccount, &tp.AccountID, "", "",
						map[string]string{
							"touchpoint_id": id.String(),
							"action":        AuditReasonComposerStale,
							"from":          stt,
							"to":            models.TouchpointCancelled,
						},
						map[string]string{"stop_reason": StopComposerStale, "composer_version": ComposerVersion},
					)
				}
			}
		}
	}
	return rep, nil
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}
