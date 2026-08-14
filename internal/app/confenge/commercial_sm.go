package confenge

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

const CorrectionSourceHuman = "HUMAN_INTERACTION"

const (
	CorrectionPersonConfirmed     = "PERSON_CONFIRMED"
	CorrectionPersonRejected      = "PERSON_REJECTED"
	CorrectionRoleCorrected       = "ROLE_CORRECTED"
	CorrectionNewPersonDiscovered = "NEW_PERSON_DISCOVERED"
	CorrectionRouteConfirmed      = "ROUTE_CONFIRMED"
	CorrectionRouteInvalid        = "ROUTE_INVALID"
	CorrectionNewRouteDiscovered  = "NEW_ROUTE_DISCOVERED"
	CorrectionPreferredChannel    = "PREFERRED_CHANNEL"
	CorrectionDNC                 = "DNC"
)

// OutcomeRequest is one human-recorded commercial result. State and outcome
// stay separate: COMPLETED can still be NO_ANSWER.
type OutcomeRequest struct {
	OutcomeCode             string
	Notes                   string
	Actor                   string
	ReferralName            string
	ReferralRole            string
	ReferralChannel         string
	NextActionType          string
	NextActionAt            *time.Time
	RouteQualityFeedback    string
	PersonRelevanceFeedback string
	MessageFeedback         string
	Now                     time.Time
}

// OutcomeApply is the result of applying one outcome to an action.
type OutcomeApply struct {
	Action     models.OutreachCommercialAction
	Followup   *models.OutreachCommercialAction
	Correction *HumanCorrection
	History    []string
}

var allowedOutcomeCodes = map[string]bool{
	models.OutcomeNoAnswer: true, models.OutcomeBusy: true, models.OutcomeInvalidChannel: true,
	models.OutcomeGatekeeperReached: true, models.OutcomeReferredToOtherPerson: true,
	models.OutcomeWrongPerson: true, models.OutcomeTargetReached: true,
	models.OutcomeCallbackRequested: true, models.OutcomeNotInterested: true,
	models.OutcomeInterested: true, models.OutcomeMeetingScheduled: true,
	models.OutcomeRepliedCode: true, models.OutcomeBouncedCode: true,
	models.OutcomeComplaint: true, models.OutcomeDNCCode: true,
	models.OutcomeFormSubmitted: true, models.OutcomeSocialMessageSent: true,
	models.OutcomeSkippedCode: true, models.OutcomeBlockedCode: true,
	models.OutcomeWrongChannel: true, models.OutcomeInvalidRoute: true,
	models.OutcomeAttempted: true, models.OutcomeContactedCode: true, models.OutcomeFollowUp: true,
}

var terminalOutcomes = map[string]bool{
	models.OutcomeNotInterested: true, models.OutcomeDNCCode: true,
	models.OutcomeBouncedCode: true, models.OutcomeComplaint: true,
	models.OutcomeSkippedCode: true, models.OutcomeBlockedCode: true,
	models.OutcomeInvalidChannel: true, models.OutcomeWrongChannel: true,
	models.OutcomeInvalidRoute: true, models.OutcomeWrongPerson: true,
	models.OutcomeMeetingScheduled: true, models.OutcomeFormSubmitted: true,
	models.OutcomeSocialMessageSent: true, models.OutcomeRepliedCode: true,
}

// StartCommercialAction moves READY/PLANNED to IN_PROGRESS.
func StartCommercialAction(a models.OutreachCommercialAction, actor string, now time.Time) (models.OutreachCommercialAction, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	switch a.State {
	case models.ActionStatePlanned, models.ActionStateReady, models.ActionStateNeedsFollowup:
	default:
		return a, errHuman("action_not_startable")
	}
	if a.State == models.ActionStateBlocked || !a.Actionable {
		return a, errHuman("action_not_actionable")
	}
	if a.RequiresFresh && strings.TrimSpace(a.StaleWarning) != "" {
		return a, errHuman("stale_requires_review")
	}
	a.State = models.ActionStateInProgress
	a.HumanActor = actor
	a.StartedAt = &now
	a.UpdatedAt = now
	return a, nil
}

// ApplyCommercialOutcome records a lean outcome and optionally mints a
// follow-up. WON is never inferred.
func ApplyCommercialOutcome(a models.OutreachCommercialAction, req OutcomeRequest) (OutcomeApply, error) {
	code := strings.ToUpper(strings.TrimSpace(req.OutcomeCode))
	if !allowedOutcomeCodes[code] {
		return OutcomeApply{}, errHuman("unknown_outcome")
	}
	if code == "WON" {
		return OutcomeApply{}, errHuman("won_never_inferred")
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if a.State == models.ActionStatePlanned || a.State == models.ActionStateReady {
		started, err := StartCommercialAction(a, req.Actor, now)
		if err != nil {
			return OutcomeApply{}, err
		}
		a = started
	}
	a.OutcomeCode = code
	a.OutcomeNotes = strings.TrimSpace(req.Notes)
	a.HumanActor = firstNonEmpty(req.Actor, a.HumanActor)
	a.RouteQualityFeedback = firstNonEmpty(req.RouteQualityFeedback, a.RouteQualityFeedback)
	a.PersonRelevanceFeedback = firstNonEmpty(req.PersonRelevanceFeedback, a.PersonRelevanceFeedback)
	a.MessageFeedback = firstNonEmpty(req.MessageFeedback, a.MessageFeedback)
	a.NextActionType = strings.ToUpper(strings.TrimSpace(req.NextActionType))
	a.NextActionAt = req.NextActionAt
	a.UpdatedAt = now

	res := OutcomeApply{Action: a, History: appendOutcomeHistory(a, code)}

	switch code {
	case models.OutcomeAttempted:
		a.State = models.ActionStateInProgress
		if a.StartedAt == nil {
			a.StartedAt = &now
		}
	case models.OutcomeContactedCode:
		a.State = models.ActionStateNeedsFollowup
		t := true
		a.TargetReached = &t
		a.ConversationStarted = true
	case models.OutcomeFollowUp:
		a.State = models.ActionStateNeedsFollowup
		if a.NextActionType == "" {
			a.NextActionType = a.ActionType
		}
	case models.OutcomeGatekeeperReached:
		a.State = models.ActionStateNeedsFollowup
		a.ConversationStarted = false
		f := false
		a.TargetReached = &f
		res.Correction = interactionCorrection(CorrectionRouteConfirmed, req.Actor, a.ChannelValue, a.ChannelValue, "gatekeeper_reached")
	case models.OutcomeReferredToOtherPerson:
		a.State = models.ActionStateNeedsFollowup
		a.ConversationStarted = true
		f := false
		a.TargetReached = &f
		child := mintReferralFollowup(a, req, now)
		a.FollowupActionID = &child.ID
		res.Followup = &child
		res.Correction = interactionCorrection(CorrectionNewPersonDiscovered, req.Actor, a.PersonName, req.ReferralName, "referral")
	case models.OutcomeTargetReached:
		a.State = models.ActionStateNeedsFollowup
		t := true
		a.TargetReached = &t
		a.ConversationStarted = true
	case models.OutcomeCallbackRequested:
		a.State = models.ActionStateNeedsFollowup
		t := true
		a.TargetReached = &t
		a.ConversationStarted = true
		if a.NextActionType == "" {
			a.NextActionType = a.ActionType
		}
	case models.OutcomeInterested:
		a.InterestState = models.OutcomeInterested
		a.ConversationStarted = true
		t := true
		a.TargetReached = &t
		a.State = models.ActionStateNeedsFollowup
	case models.OutcomeMeetingScheduled:
		a.InterestState = models.OutcomeMeetingScheduled
		a.ConversationStarted = true
		t := true
		a.TargetReached = &t
		a.State = models.ActionStateCompleted
		a.CompletedAt = &now
		// Meeting is not WON.
	case models.OutcomeWrongPerson:
		a.State = models.ActionStateCompleted
		a.CompletedAt = &now
		a.BlockedPerson = true
		a.PersonRelevanceFeedback = firstNonEmpty(a.PersonRelevanceFeedback, "WRONG_PERSON")
		res.Correction = interactionCorrection(CorrectionPersonRejected, req.Actor, a.PersonName, "", "wrong_person")
	case models.OutcomeInvalidRoute:
		a.State = models.ActionStateCompleted
		a.CompletedAt = &now
		a.BlockedRoute = true
		a.RouteQualityFeedback = firstNonEmpty(a.RouteQualityFeedback, "INVALID_ROUTE")
		res.Correction = interactionCorrection(CorrectionRouteInvalid, req.Actor, a.ChannelValue, "", "invalid_route")
	case models.OutcomeDNCCode:
		a.State = models.ActionStateBlocked
		a.Lane = models.LaneBlockedAction
		a.Actionable = false
		a.EmailSendable = false
		a.Dispatchable = false
		a.BlockedPerson = true
		a.BlockedRoute = true
		a.CompletedAt = &now
		res.Correction = interactionCorrection(CorrectionDNC, req.Actor, "", "DNC", req.Notes)
	case models.OutcomeSkippedCode:
		a.State = models.ActionStateSkipped
		a.CompletedAt = &now
	case models.OutcomeBlockedCode:
		a.State = models.ActionStateBlocked
		a.Actionable = false
		a.CompletedAt = &now
	default:
		if terminalOutcomes[code] {
			a.State = models.ActionStateCompleted
			a.CompletedAt = &now
		} else {
			a.State = models.ActionStateNeedsFollowup
		}
		if code == models.OutcomeFormSubmitted || code == models.OutcomeSocialMessageSent || code == models.OutcomeRepliedCode {
			a.ConversationStarted = true
		}
	}
	if a.Lane != models.LaneBlockedAction && (a.State == models.ActionStateCompleted || a.State == models.ActionStateSkipped) {
		a.Lane = models.LaneDone
	}
	res.Action = a
	return res, nil
}

func mintReferralFollowup(parent models.OutreachCommercialAction, req OutcomeRequest, now time.Time) models.OutreachCommercialAction {
	name := strings.TrimSpace(req.ReferralName)
	role := strings.TrimSpace(req.ReferralRole)
	child := parent
	child.ID = uuid.Nil
	child.ParentActionID = &parent.ID
	child.FollowupActionID = nil
	child.PersonName = name
	child.ObservedRole = role
	child.TargetRole = firstNonEmpty(role, parent.TargetRole)
	child.OutcomeCode = ""
	child.OutcomeNotes = ""
	child.InterestState = ""
	child.BlockedPerson = false
	child.BlockedRoute = false
	child.ConversationStarted = false
	child.TargetReached = nil
	child.HumanNotes = "follow-up from referral"
	child.State = models.ActionStateReady
	child.CreatedAt = now
	child.UpdatedAt = now
	child.StartedAt = nil
	child.CompletedAt = nil
	// A switchboard number stays routed even if the UI asks for DIRECT_CALL.
	// The new person is not proven to own the company number.
	if parent.ActionType == models.ActionRoutedCall || parent.RouteRelation == models.RouteRelRoutesToNamedPerson {
		child.ActionType = models.ActionRoutedCall
		child.RouteRelation = models.RouteRelRoutesToNamedPerson
		child.RouteType = firstNonEmpty(parent.RouteType, "phone")
		child.ChannelValue = firstNonEmpty(req.ReferralChannel, parent.ChannelValue)
		child.ChannelDisplay = firstNonEmpty(parent.ChannelDisplay, "telefone oficial da empresa")
	} else if req.NextActionType != "" {
		child.ActionType = strings.ToUpper(strings.TrimSpace(req.NextActionType))
	} else if parent.ActionType == models.ActionDirectCall {
		child.ActionType = models.ActionDirectCall
	}
	switch child.ActionType {
	case models.ActionDirectCall:
		child.Lane = models.LaneCallQueue
		if child.RouteRelation == "" {
			child.RouteRelation = models.RouteRelBelongsToNamedPerson
		}
	case models.ActionRoutedCall:
		child.Lane = models.LaneRoutedCallQueue
		child.RouteRelation = models.RouteRelRoutesToNamedPerson
	case models.ActionWhatsApp:
		child.Lane = models.LaneWhatsAppQueue
	default:
		child.Lane = LaneManualOutreach
	}
	if child.ActionType == models.ActionRoutedCall {
		child.RecommendedAction = "Ligar para o telefone oficial da empresa e pedir para falar com " + firstNonEmpty(name, "a pessoa indicada") + "."
	} else {
		child.RecommendedAction = "Ligar para " + firstNonEmpty(name, "a pessoa indicada")
		if role != "" {
			child.RecommendedAction += " (" + role + ")"
		}
	}
	child.Warnings = []string{"Indicacao humana. Validar identidade antes de tratar como decision-maker confirmado."}
	if child.ActionType == models.ActionRoutedCall {
		child.Warnings = append(child.Warnings, "Este numero e da empresa. Nao e o telefone direto de "+firstNonEmpty(name, "a pessoa indicada")+".")
	}
	child.PersonFingerprint = personFingerprint(name, role)
	child.RouteFingerprint = routeFingerprint(child.ActionType, child.RouteType, child.RouteRelation, firstNonEmpty(req.ReferralChannel, child.ChannelValue), name)
	child.IdempotencyKey = "followup:" + parent.ID.String() + ":" + child.PersonFingerprint
	child.ID = DeterministicActionID(parent.OrganizationID, parent.AccountID, child.ActionType, child.IdempotencyKey)
	content := ComposeActionContent(child)
	child.ContentJSON = mustJSON(content)
	child.ContentHash = contentHashOf(content)
	return child
}

// CanReplanPerson is false after WRONG_PERSON until a human correction.
func CanReplanPerson(existing models.OutreachCommercialAction, personFP string) bool {
	if !existing.BlockedPerson {
		return true
	}
	return personFingerprint(existing.PersonName, existing.ObservedRole) != personFP
}

// CanReplanRoute is false after INVALID_ROUTE for the same route.
func CanReplanRoute(existing models.OutreachCommercialAction, routeFP string) bool {
	if !existing.BlockedRoute {
		return true
	}
	return existing.RouteFingerprint != routeFP
}

// InvalidateOnPersonChange clears dependent content/approval when the
// published person no longer matches.
func InvalidateOnPersonChange(a *models.OutreachCommercialAction, newPersonFP string) {
	if a == nil || a.PersonFingerprint == "" || a.PersonFingerprint == newPersonFP {
		return
	}
	a.State = models.ActionStatePlanned
	a.ContentHash = ""
	a.ContentJSON = nil
	a.Warnings = appendUnique(a.Warnings, "Pessoa alterada. Conteudo e aprovacao anteriores invalidos.")
	a.UpdatedAt = time.Now().UTC()
}

// InvalidateOnRouteChange clears dependent content when the route changes.
func InvalidateOnRouteChange(a *models.OutreachCommercialAction, newRouteFP string) {
	if a == nil || a.RouteFingerprint == "" || a.RouteFingerprint == newRouteFP {
		return
	}
	a.State = models.ActionStatePlanned
	a.ContentHash = ""
	a.ContentJSON = nil
	a.Dispatchable = false
	a.EmailSendable = false
	a.Warnings = appendUnique(a.Warnings, "Rota alterada. Conteudo e aprovacao anteriores invalidos.")
	a.UpdatedAt = time.Now().UTC()
}

// MarkStaleFreshness gates execution until a human reviews freshness.
func MarkStaleFreshness(a *models.OutreachCommercialAction, reason string) {
	if a == nil {
		return
	}
	a.RequiresFresh = true
	a.StaleWarning = firstNonEmpty(reason, "Snapshot upstream desatualizado. Nao executar sem revisar.")
	a.Warnings = appendUnique(a.Warnings, a.StaleWarning)
}

func interactionCorrection(kind, actor, before, after, reason string) *HumanCorrection {
	hc := HumanCorrection{
		Decision:    mapCorrectionDecision(kind),
		Kind:        kind,
		Source:      CorrectionSourceHuman,
		ActorID:     actor,
		At:          time.Now().UTC(),
		BeforeBody:  before,
		AfterBody:   after,
		ReasonCodes: normalizeHumanReasons([]string{reason}),
		Silent:      false,
	}
	return &hc
}

func mapCorrectionDecision(kind string) string {
	switch kind {
	case CorrectionPersonRejected, CorrectionRouteInvalid, CorrectionDNC:
		return DecisionReject
	case CorrectionRoleCorrected, CorrectionNewPersonDiscovered, CorrectionNewRouteDiscovered, CorrectionPreferredChannel:
		return DecisionRecipientChange
	case CorrectionPersonConfirmed, CorrectionRouteConfirmed:
		return DecisionApprove
	default:
		return DecisionSkip
	}
}

func appendOutcomeHistory(a models.OutreachCommercialAction, code string) []string {
	var hist []string
	if len(a.CorrectionJSON) > 0 {
		_ = json.Unmarshal(a.CorrectionJSON, &hist)
	}
	return append(hist, code)
}

// CompareActionPriority orders by decision relevance, then why-now, then
// upstream priority, then route strength. Email is not preferred over a
// routed call to the real decision-maker.
func CompareActionPriority(a, b models.OutreachCommercialAction) int {
	da, db := decisionRelevance(a), decisionRelevance(b)
	if da != db {
		return db - da
	}
	wa, wb := whyNowStrength(a.Confidence), whyNowStrength(b.Confidence)
	if wa != wb {
		return wb - wa
	}
	if a.PriorityScore != b.PriorityScore {
		if a.PriorityScore > b.PriorityScore {
			return -1
		}
		return 1
	}
	if a.PriorityRank != b.PriorityRank && a.PriorityRank > 0 && b.PriorityRank > 0 {
		return a.PriorityRank - b.PriorityRank
	}
	return routeStrength(b.ReachabilityClass) - routeStrength(a.ReachabilityClass)
}

func decisionRelevance(a models.OutreachCommercialAction) int {
	n := 0
	if strings.TrimSpace(a.PersonName) != "" {
		n += 4
	}
	if strings.TrimSpace(a.ObservedRole) != "" || strings.TrimSpace(a.TargetRole) != "" {
		n += 2
	}
	switch a.ActionType {
	case models.ActionRoutedCall, models.ActionDirectCall:
		n += 1
	}
	return n
}
