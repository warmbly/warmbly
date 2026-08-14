package confenge

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/models"
)

// commercialActionStore is implemented by the Postgres outreach repo and memRepo.
// It is intentionally not on OutreachRepository so existing stubs stay valid.
type commercialActionStore interface {
	UpsertCommercialAction(ctx context.Context, a *models.OutreachCommercialAction) error
	GetCommercialAction(ctx context.Context, orgID, id uuid.UUID) (*models.OutreachCommercialAction, error)
	GetCommercialActionByIdempotency(ctx context.Context, orgID uuid.UUID, key string) (*models.OutreachCommercialAction, error)
	ListCommercialActions(ctx context.Context, orgID uuid.UUID, accountID uuid.UUID, openOnly bool, limit int) ([]models.OutreachCommercialAction, error)
}

func (s *service) actionStore() commercialActionStore {
	if st, ok := s.repo.(commercialActionStore); ok {
		return st
	}
	return nil
}

func (s *service) persistPlannedAction(ctx context.Context, planned PlannedAction) {
	if planned.NoAction || s.actionStore() == nil {
		return
	}
	a := planned.Action
	st := s.actionStore()
	if existing, _ := st.GetCommercialActionByIdempotency(ctx, a.OrganizationID, a.IdempotencyKey); existing != nil {
		if !CanReplanPerson(*existing, a.PersonFingerprint) || !CanReplanRoute(*existing, a.RouteFingerprint) {
			return
		}
		if keepPersistedCommercialAction(*existing) {
			// Keep execution / DNC history; refresh display fields only.
			existing.WhyNow = a.WhyNow
			existing.FactualHook = a.FactualHook
			existing.CompanyName = a.CompanyName
			existing.UpdatedAt = time.Now().UTC()
			_ = st.UpsertCommercialAction(ctx, existing)
			return
		}
		InvalidateOnPersonChange(existing, a.PersonFingerprint)
		InvalidateOnRouteChange(existing, a.RouteFingerprint)
		a.ID = existing.ID
		a.CreatedAt = existing.CreatedAt
		a.OutcomeCode = existing.OutcomeCode
		a.BlockedPerson = existing.BlockedPerson
		a.BlockedRoute = existing.BlockedRoute
	}
	_ = st.UpsertCommercialAction(ctx, &a)
}

func keepPersistedCommercialAction(existing models.OutreachCommercialAction) bool {
	switch existing.State {
	case models.ActionStateInProgress, models.ActionStateNeedsFollowup, models.ActionStateCompleted, models.ActionStateBlocked:
		return true
	}
	return existing.OutcomeCode == models.OutcomeDNCCode
}

func (s *service) CollectToday(ctx context.Context, orgID uuid.UUID) (*TodayView, *errx.Error) {
	cockpit, xerr := s.CollectContactCockpit(ctx, orgID)
	if xerr != nil {
		return nil, xerr
	}
	return &cockpit.Today, nil
}

func (s *service) RecordCommercialOutcome(ctx context.Context, orgID, userID, actionID uuid.UUID, req OutcomeRequest) (*OutcomeApply, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	st := s.actionStore()
	if st == nil {
		return nil, errx.New(errx.Internal, "commercial action store unavailable")
	}
	a, err := st.GetCommercialAction(ctx, orgID, actionID)
	if err != nil || a == nil {
		return nil, errx.New(errx.NotFound, "commercial action not found")
	}
	if req.Actor == "" {
		req.Actor = userID.String()
	}
	res, aerr := ApplyCommercialOutcome(*a, req)
	if aerr != nil {
		return nil, errx.New(errx.BadRequest, aerr.Error())
	}
	if err := st.UpsertCommercialAction(ctx, &res.Action); err != nil {
		return nil, errx.New(errx.Internal, "persist action: "+err.Error())
	}
	if res.Followup != nil {
		if err := st.UpsertCommercialAction(ctx, res.Followup); err != nil {
			return nil, errx.New(errx.Internal, "persist follow-up: "+err.Error())
		}
	}
	if s.audit != nil {
		s.audit.LogAction(ctx, orgID, userID, models.AuditActionUpdate, models.AuditEntityOutreachCommercialAction, &actionID, "", "",
			map[string]string{"action": "commercial_outcome", "outcome": res.Action.OutcomeCode, "action_type": res.Action.ActionType},
			map[string]string{"notes": req.Notes},
		)
	}
	s.enqueueActionOutcome(ctx, orgID, res)
	return &res, nil
}

func (s *service) StartCommercialWork(ctx context.Context, orgID, userID, actionID uuid.UUID) (*models.OutreachCommercialAction, *errx.Error) {
	if xerr := s.requireEnabled(); xerr != nil {
		return nil, xerr
	}
	st := s.actionStore()
	if st == nil {
		return nil, errx.New(errx.Internal, "commercial action store unavailable")
	}
	a, err := st.GetCommercialAction(ctx, orgID, actionID)
	if err != nil || a == nil {
		return nil, errx.New(errx.NotFound, "commercial action not found")
	}
	started, serr := StartCommercialAction(*a, userID.String(), time.Now().UTC())
	if serr != nil {
		return nil, errx.New(errx.BadRequest, serr.Error())
	}
	if err := st.UpsertCommercialAction(ctx, &started); err != nil {
		return nil, errx.New(errx.Internal, "persist action: "+err.Error())
	}
	return &started, nil
}

func (s *service) enqueueActionOutcome(ctx context.Context, orgID uuid.UUID, res OutcomeApply) {
	a := res.Action
	meta := map[string]any{
		"action_id":                 a.ID.String(),
		"action_type":               a.ActionType,
		"reachability_class":        a.ReachabilityClass,
		"route_type":                a.RouteType,
		"route_relation":            a.RouteRelation,
		"outcome_code":              a.OutcomeCode,
		"target_reached":            a.TargetReached,
		"conversation_started":      a.ConversationStarted,
		"interest_state":            a.InterestState,
		"route_quality_feedback":    a.RouteQualityFeedback,
		"person_relevance_feedback": a.PersonRelevanceFeedback,
		"message_feedback":          a.MessageFeedback,
		"channel":                   firstNonEmpty(a.RouteType, a.ActionType),
	}
	if res.Followup != nil {
		meta["referral"] = map[string]any{
			"name": res.Followup.PersonName, "role": res.Followup.ObservedRole,
			"followup_action_id": res.Followup.ID.String(),
		}
		meta["new_person"] = res.Followup.PersonName
		meta["new_role"] = res.Followup.ObservedRole
	}
	if res.Correction != nil {
		meta["correction_kind"] = res.Correction.Kind
	}
	payload, _ := json.Marshal(meta)
	eventType := mapOutcomeEventType(a.OutcomeCode)
	_ = s.EnqueueOutcome(ctx, orgID, models.OutreachOutcome{
		IdempotencyKey: "action_outcome:" + a.ID.String() + ":" + a.OutcomeCode + ":" + a.UpdatedAt.UTC().Format(time.RFC3339Nano),
		SourceLeadID:   a.SourceLeadID,
		EventType:      eventType,
		OccurredAt:     time.Now().UTC(),
		Payload:        payload,
	})
}

func mapOutcomeEventType(code string) string {
	switch code {
	case models.OutcomeRepliedCode:
		return OutcomeReplied
	case models.OutcomeMeetingScheduled:
		return OutcomeMeeting
	case models.OutcomeDNCCode:
		return OutcomeDoNotContact
	case models.OutcomeBouncedCode:
		return OutcomeBounced
	case models.OutcomeNotInterested:
		return OutcomeLost
	default:
		return OutcomeContacted
	}
}

func (s *service) loadPersistedActions(ctx context.Context, orgID uuid.UUID) []models.OutreachCommercialAction {
	st := s.actionStore()
	if st == nil {
		return nil
	}
	list, err := st.ListCommercialActions(ctx, orgID, uuid.Nil, false, 500)
	if err != nil {
		return nil
	}
	return list
}

func mergeTodayActions(planned []models.OutreachCommercialAction, persisted []models.OutreachCommercialAction) []models.OutreachCommercialAction {
	byKey := map[string]models.OutreachCommercialAction{}
	for _, a := range planned {
		if a.IdempotencyKey != "" {
			byKey[a.IdempotencyKey] = a
		}
	}
	for _, p := range persisted {
		if p.IdempotencyKey == "" {
			planned = append(planned, p)
			continue
		}
		if cur, ok := byKey[p.IdempotencyKey]; ok {
			if p.State == models.ActionStateBlocked || p.OutcomeCode == models.OutcomeDNCCode {
				p.Actionable = false
				p.EmailSendable = false
				p.Dispatchable = false
				p.Lane = models.LaneBlockedAction
				p.CompanyName = firstNonEmpty(p.CompanyName, cur.CompanyName)
				p.WhyNow = firstNonEmpty(p.WhyNow, cur.WhyNow)
				p.FactualHook = firstNonEmpty(p.FactualHook, cur.FactualHook)
				byKey[p.IdempotencyKey] = p
				continue
			}
			InvalidateOnPersonChange(&p, cur.PersonFingerprint)
			InvalidateOnRouteChange(&p, cur.RouteFingerprint)
			if !CanReplanPerson(p, cur.PersonFingerprint) || !CanReplanRoute(p, cur.RouteFingerprint) {
				p.Actionable = false
				p.Warnings = appendUnique(p.Warnings, "Recomendacao anterior bloqueada ate revisao humana.")
			}
			p.CompanyName = firstNonEmpty(p.CompanyName, cur.CompanyName)
			p.WhyNow = firstNonEmpty(p.WhyNow, cur.WhyNow)
			p.FactualHook = firstNonEmpty(p.FactualHook, cur.FactualHook)
			byKey[p.IdempotencyKey] = p
		} else {
			byKey[p.IdempotencyKey] = p
		}
	}
	out := make([]models.OutreachCommercialAction, 0, len(byKey))
	seen := map[string]bool{}
	for _, a := range planned {
		if a.IdempotencyKey == "" {
			out = append(out, a)
			continue
		}
		if seen[a.IdempotencyKey] {
			continue
		}
		seen[a.IdempotencyKey] = true
		out = append(out, byKey[a.IdempotencyKey])
	}
	for k, a := range byKey {
		if !seen[k] {
			out = append(out, a)
		}
	}
	return out
}

func planAccountAction(acc *models.OutreachAccount, cands []models.OutreachContactCandidate, ev []models.OutreachEvidence, now time.Time) PlannedAction {
	rec := ResolveRecipient(acc, cands, now)
	cand := pickResolvedCandidate(cands, rec)
	return PlanCommercialAction(PlanInput{
		Account: acc, Candidate: cand, Candidates: cands, Evidence: ev, Now: now,
		Snapshot: acc.LastPayloadHash,
	})
}

// Used by import after each lead apply.
func (s *service) planAndPersistAccount(ctx context.Context, orgID uuid.UUID, acc *models.OutreachAccount) {
	if acc == nil {
		return
	}
	now := time.Now().UTC()
	cands, _ := s.repo.ListCandidates(ctx, orgID, acc.ID)
	ev, _ := s.repo.ListEvidence(ctx, orgID, acc.ID)
	planned := planAccountAction(acc, cands, ev, now)
	s.persistPlannedAction(ctx, planned)
}
