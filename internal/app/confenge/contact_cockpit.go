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

type ContactCockpit struct {
	Funnel  ContactFunnel     `json:"funnel"`
	Manual  []ManualQueueItem `json:"manual"`
	Ready   []ManualQueueItem `json:"needs_review"`
	Today   TodayView         `json:"today"`
	Metrics ActionMetrics     `json:"metrics"`
}

func humanQueueStates() []string {
	return []string{
		models.OutreachQueueNeedsReview,
		models.OutreachQueueReadyToGenerate,
		models.OutreachQueueNeedsContact,
		models.OutreachQueueApproved,
	}
}

func outcomeQueueStates() []string {
	return []string{
		models.OutreachQueueSent,
		models.OutreachQueueReplied,
		models.OutreachQueueMeeting,
	}
}

func (s *service) CollectContactCockpit(ctx context.Context, orgID uuid.UUID) (*ContactCockpit, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	now := time.Now().UTC()
	pb := MustPlaybook()
	var classes []ContactClass
	var manual []ManualQueueItem
	var ready []ManualQueueItem
	var planned []models.OutreachCommercialAction
	extras := ContactFunnel{}
	for _, qs := range outcomeQueueStates() {
		accs, err := s.repo.ListAccounts(ctx, orgID, repository.OutreachAccountFilter{QueueState: qs, Limit: 200})
		if err != nil {
			return nil, errx.New(errx.Internal, "list outcome accounts: "+err.Error())
		}
		switch qs {
		case models.OutreachQueueSent:
			extras.Contacted += len(accs)
		case models.OutreachQueueReplied:
			extras.Replied += len(accs)
		case models.OutreachQueueMeeting:
			extras.Meeting += len(accs)
		}
	}
	for _, qs := range humanQueueStates() {
		accs, err := s.repo.ListAccounts(ctx, orgID, repository.OutreachAccountFilter{QueueState: qs, Limit: 200})
		if err != nil {
			return nil, errx.New(errx.Internal, "list accounts: "+err.Error())
		}
		for i := range accs {
			acc := accs[i]
			if acc.QueueState == models.OutreachQueueApproved {
				extras.Approved++
			}
			cands, _ := s.repo.ListCandidates(ctx, orgID, acc.ID)
			ev, _ := s.repo.ListEvidence(ctx, orgID, acc.ID)
			rec := ResolveRecipient(&acc, cands, now)
			var cand *models.OutreachContactCandidate
			if len(cands) > 0 {
				cand = pickResolvedCandidate(cands, rec)
			}
			cls := ClassifyContactTier(&acc, cand, now)
			_, plan := BuildOutboundPlan(pb, &acc, cand, ev, 1)
			body := ""
			if cls.Tier == ContactTierA && rec.State == RecipientValidated && plan.Messageability == MessageabilityReady {
				out := ComposeFromPlan(plan, &acc, cand, ChannelEmailInitial)
				body = out.BodyText
			}
			cls.Lane = ClassifyActionLane(cls, rec, plan, body)
			cls.Messageability = plan.Messageability
			classes = append(classes, cls)
			item := BuildManualItem(&acc, cand, rec, cls, plan)
			item.CanonicalTargetID = acc.ID.String()
			switch cls.Lane {
			case LaneNeedsReviewEmail:
				ready = append(ready, item)
			case LaneManualOutreach, LaneRoleMailboxException, LaneLowConfidenceManual:
				manual = append(manual, item)
			}
			p := PlanCommercialAction(PlanInput{
				Account: &acc, Candidate: cand, Candidates: cands, Evidence: ev, Now: now,
				Snapshot: acc.LastPayloadHash,
			})
			if !p.NoAction {
				planned = append(planned, p.Action)
				s.persistPlannedAction(ctx, p)
			}
		}
	}
	merged := mergeTodayActions(planned, s.loadPersistedActions(ctx, orgID))
	today := AssembleToday(merged)
	metrics := SummarizeActionMetrics(merged)
	funnel := SummarizeContactFunnel(classes, extras)
	funnel.Actionable = today.Summary.Total
	funnel.ActionPlanned = metrics.Planned
	funnel.Touched = metrics.Executed
	funnel.TargetReached = metrics.TargetReachedCount
	funnel.Conversation = metrics.ConversationCount
	funnel.Interested = metrics.InterestCount
	return &ContactCockpit{Funnel: funnel, Manual: manual, Ready: ready, Today: today, Metrics: metrics}, nil
}

func (s *service) ApplyManualAction(ctx context.Context, orgID, userID, accountID uuid.UUID, action, reason string) (*HumanCorrection, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	act := strings.ToUpper(strings.TrimSpace(action))
	switch act {
	case ManualMarkContacted, ManualSkip, ManualNeedsEnrichment, ManualCorrectContact, ManualPromoteAfterEvidence:
	default:
		return nil, errx.New(errx.BadRequest, "unsupported manual action")
	}
	if act == ManualPromoteAfterEvidence {
		return nil, errx.New(errx.BadRequest, "PROMOTE_AFTER_NEW_EVIDENCE requires a later VALIDATED recipient; it does not approve send")
	}
	acc, err := s.repo.GetAccount(ctx, orgID, accountID)
	if err != nil || acc == nil {
		return nil, errx.New(errx.NotFound, "account not found")
	}
	draft, _ := s.repo.GetActiveDraftForAccount(ctx, orgID, accountID)
	if draft == nil {
		draft = &models.OutreachDraft{
			OrganizationID: orgID, AccountID: accountID,
			Channel: models.OutreachChannelEmail, Status: models.OutreachDraftSkipped,
			PromptVersion: PromptVersion, RiskFlags: []string{"manual_action_ledger"},
		}
		if err := s.repo.UpsertDraft(ctx, draft); err != nil {
			return nil, errx.New(errx.Internal, "persist ledger draft: "+err.Error())
		}
	}
	decision := DecisionSkip
	switch act {
	case ManualCorrectContact:
		decision = DecisionRecipientChange
	case ManualNeedsEnrichment:
		decision = DecisionReject
	}
	reasons := []string{}
	if reason != "" {
		reasons = append(reasons, reason)
	}
	hc, herr := RecordHumanDecision(decision, draft.ID.String(), userID.String(), draft.BodyText, draft.BodyText, draft.Subject, draft.Subject, reasons)
	if herr != nil {
		return nil, errx.New(errx.BadRequest, herr.Error())
	}
	attachHumanCorrection(draft, hc)
	if err := s.repo.UpsertDraft(ctx, draft); err != nil {
		return nil, errx.New(errx.Internal, "persist correction: "+err.Error())
	}
	if s.audit != nil {
		s.audit.LogAction(ctx, orgID, userID, models.AuditActionUpdate, models.AuditEntityOutreachAccount, &accountID, "", "",
			map[string]string{"action": "manual_queue_" + strings.ToLower(act), "draft_id": draft.ID.String()},
			map[string]string{"reason": reason},
		)
	}
	return &hc, nil
}
