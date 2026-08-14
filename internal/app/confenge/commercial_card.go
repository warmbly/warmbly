package confenge

import (
	"encoding/json"
	"sort"

	"github.com/warmbly/warmbly/internal/models"
)

// ActionCard is the operator-facing payload. The five-minute question
// ("para quem eu ligo agora e por que?") must be answerable without JSON.
type ActionCard struct {
	ActionID          string                  `json:"action_id"`
	AccountID         string                  `json:"account_id,omitempty"`
	Company           string                  `json:"company"`
	Person            string                  `json:"person,omitempty"`
	PersonID          string                  `json:"person_id,omitempty"`
	Role              string                  `json:"role,omitempty"`
	TargetRole        string                  `json:"target_role,omitempty"`
	WhyNow            string                  `json:"why_now,omitempty"`
	Offer             string                  `json:"offer,omitempty"`
	RecommendedAction string                  `json:"recommended_action,omitempty"`
	Channel           string                  `json:"channel,omitempty"`
	ChannelValue      string                  `json:"channel_value,omitempty"`
	NextActionAt      string                  `json:"next_action_at,omitempty"`
	RouteType         string                  `json:"route_type,omitempty"`
	RouteRelation     string                  `json:"route_relation,omitempty"`
	ReachabilityClass string                  `json:"reachability_class,omitempty"`
	Confidence        string                  `json:"confidence,omitempty"`
	FactualHook       string                  `json:"factual_hook,omitempty"`
	Evidence          []string                `json:"evidence,omitempty"`
	Warnings          []string                `json:"warnings,omitempty"`
	Copy              CommercialActionContent `json:"copy"`
	LastOutcome       string                  `json:"last_outcome,omitempty"`
	NextAction        string                  `json:"next_action,omitempty"`
	RouteEpistemology string                  `json:"route_epistemology,omitempty"`
	ActionType        string                  `json:"action_type"`
	Lane              string                  `json:"lane"`
	State             string                  `json:"state"`
	Actionable        bool                    `json:"actionable"`
	EmailSendable     bool                    `json:"email_sendable"`
	Dispatchable      bool                    `json:"dispatchable"`
	ParentActionID    string                  `json:"parent_action_id,omitempty"`
	FollowupActionID  string                  `json:"followup_action_id,omitempty"`
	StaleWarning      string                  `json:"stale_warning,omitempty"`
}

// TodayView is "o que fazer agora": only executable human work.
type TodayView struct {
	Summary TodaySummary `json:"summary"`
	Actions []ActionCard `json:"actions"`
	Lanes   TodayLanes   `json:"lanes"`
}

type TodaySummary struct {
	Calls              int `json:"calls"`
	RoutedCalls        int `json:"routed_calls"`
	EmailsToReview     int `json:"emails_to_review"`
	InferredEmails     int `json:"inferred_emails"`
	RoleEmails         int `json:"role_emails"`
	WhatsApp           int `json:"whatsapp"`
	ProfessionalSocial int `json:"professional_social"`
	ContactForms       int `json:"contact_forms"`
	LowConfidence      int `json:"low_confidence"`
	Total              int `json:"total"`
}

type TodayLanes struct {
	EmailNeedsReview []ActionCard `json:"email_needs_review,omitempty"`
	HumanReviewEmail []ActionCard `json:"human_review_email,omitempty"`
	CallQueue        []ActionCard `json:"call_queue,omitempty"`
	RoutedCallQueue  []ActionCard `json:"routed_call_queue,omitempty"`
	WhatsAppQueue    []ActionCard `json:"whatsapp_queue,omitempty"`
	SocialQueue      []ActionCard `json:"professional_social_queue,omitempty"`
	RoleEmailQueue   []ActionCard `json:"role_email_queue,omitempty"`
	ContactFormQueue []ActionCard `json:"contact_form_queue,omitempty"`
	LowConfidence    []ActionCard `json:"low_confidence_manual,omitempty"`
	Blocked          []ActionCard `json:"blocked,omitempty"`
	Done             []ActionCard `json:"done,omitempty"`
}

// ActionMetrics are honest rates: a missing denominator is omitted.
type ActionMetrics struct {
	Planned            int            `json:"actions_planned"`
	Executed           int            `json:"actions_executed"`
	ByActionType       map[string]int `json:"by_action_type,omitempty"`
	ByReachability     map[string]int `json:"by_reachability_class,omitempty"`
	TargetReachedRate  *float64       `json:"target_reached_rate,omitempty"`
	ConversationRate   *float64       `json:"conversation_rate,omitempty"`
	InterestRate       *float64       `json:"interest_rate,omitempty"`
	MeetingRate        *float64       `json:"meeting_rate,omitempty"`
	WrongPerson        int            `json:"wrong_person"`
	WrongChannel       int            `json:"wrong_channel"`
	ReferralCount      int            `json:"referrals"`
	DNCCount           int            `json:"dnc"`
	BounceCount        int            `json:"bounce"`
	TargetReachedCount int            `json:"target_reached"`
	ConversationCount  int            `json:"conversations"`
	InterestCount      int            `json:"interested"`
	MeetingCount       int            `json:"meetings"`
	ExecutedDenom      int            `json:"-"`
}

// AssembleActionCard projects a persisted/planned action into the card.
func AssembleActionCard(a models.OutreachCommercialAction) ActionCard {
	copy := CommercialActionContent{}
	if len(a.ContentJSON) > 0 {
		_ = json.Unmarshal(a.ContentJSON, &copy)
	}
	if copy.Kind == "" {
		copy = ComposeActionContent(a)
	}
	card := ActionCard{
		ActionID:          a.ID.String(),
		AccountID:         a.AccountID.String(),
		Company:           a.CompanyName,
		Person:            a.PersonName,
		PersonID:          firstNonEmpty(a.PersonID, copy.PersonID),
		Role:              firstNonEmpty(a.ObservedRole, a.TargetRole),
		TargetRole:        a.TargetRole,
		WhyNow:            a.WhyNow,
		Offer:             firstNonEmpty(a.ServiceContext, a.ServiceCode),
		RecommendedAction: a.RecommendedAction,
		Channel:           firstNonEmpty(a.ChannelDisplay, a.ChannelValue, a.RouteType),
		ChannelValue:      a.ChannelValue,
		RouteType:         a.RouteType,
		RouteRelation:     a.RouteRelation,
		ReachabilityClass: a.ReachabilityClass,
		Confidence:        a.Confidence,
		FactualHook:       a.FactualHook,
		Evidence:          append([]string{}, a.EvidenceIDs...),
		Warnings:          append([]string{}, a.Warnings...),
		Copy:              copy,
		LastOutcome:       a.OutcomeCode,
		NextAction:        a.NextActionType,
		ActionType:        a.ActionType,
		Lane:              a.Lane,
		State:             a.State,
		Actionable:        a.Actionable,
		EmailSendable:     a.EmailSendable,
		Dispatchable:      a.Dispatchable,
		StaleWarning:      a.StaleWarning,
	}
	if a.ParentActionID != nil {
		card.ParentActionID = a.ParentActionID.String()
	}
	if a.FollowupActionID != nil {
		card.FollowupActionID = a.FollowupActionID.String()
	}
	if a.NextActionAt != nil && !a.NextActionAt.IsZero() {
		card.NextActionAt = a.NextActionAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	card.RouteEpistemology = routeEpistemology(a)
	if card.RouteEpistemology != "" {
		card.Warnings = appendUnique(card.Warnings, card.RouteEpistemology)
	}
	return card
}

func routeEpistemology(a models.OutreachCommercialAction) string {
	person := firstNonEmpty(a.PersonName, "a pessoa alvo")
	switch {
	case a.ActionType == models.ActionRoutedCall || a.RouteRelation == models.RouteRelRoutesToNamedPerson:
		return "Este numero e da empresa. Nao e o telefone direto de " + person + "."
	case a.ActionType == models.ActionRoleEmail || a.RouteRelation == models.RouteRelRoleMailbox:
		return "Esta caixa e da funcao/equipe. Nao e o e-mail pessoal de uma pessoa."
	case a.ActionType == models.ActionInferredEmailReview:
		return "E-mail inferido. Nao e um destinatario VALIDATED e nao autoriza envio."
	case a.ActionType == models.ActionGenericEmail || a.ActionType == models.ActionContactForm:
		return "Rota corporativa generica. Nao tratar como destinatario nominal."
	default:
		return ""
	}
}

func isExecutableHumanWork(a models.OutreachCommercialAction) bool {
	if !a.Actionable {
		return false
	}
	switch a.State {
	case models.ActionStatePlanned, models.ActionStateReady, models.ActionStateInProgress, models.ActionStateNeedsFollowup:
	default:
		return false
	}
	switch a.Lane {
	case models.LaneBlockedAction, models.LaneDone:
		return false
	}
	return a.ActionType != ""
}

// AssembleToday keeps only executable human work and sorts by commercial value.
func AssembleToday(actions []models.OutreachCommercialAction) TodayView {
	var exec []models.OutreachCommercialAction
	for _, a := range actions {
		if isExecutableHumanWork(a) {
			exec = append(exec, a)
		}
	}
	sort.SliceStable(exec, func(i, j int) bool {
		return CompareActionPriority(exec[i], exec[j]) < 0
	})
	view := TodayView{Actions: make([]ActionCard, 0, len(exec))}
	for _, a := range exec {
		card := AssembleActionCard(a)
		view.Actions = append(view.Actions, card)
		switch a.ActionType {
		case models.ActionDirectCall:
			view.Summary.Calls++
			view.Lanes.CallQueue = append(view.Lanes.CallQueue, card)
		case models.ActionRoutedCall:
			view.Summary.RoutedCalls++
			view.Lanes.RoutedCallQueue = append(view.Lanes.RoutedCallQueue, card)
		case models.ActionDirectEmail:
			view.Summary.EmailsToReview++
			view.Lanes.EmailNeedsReview = append(view.Lanes.EmailNeedsReview, card)
		case models.ActionInferredEmailReview:
			view.Summary.InferredEmails++
			view.Lanes.HumanReviewEmail = append(view.Lanes.HumanReviewEmail, card)
		case models.ActionRoleEmail:
			view.Summary.RoleEmails++
			view.Lanes.RoleEmailQueue = append(view.Lanes.RoleEmailQueue, card)
		case models.ActionWhatsApp:
			view.Summary.WhatsApp++
			view.Lanes.WhatsAppQueue = append(view.Lanes.WhatsAppQueue, card)
		case models.ActionProfessionalSocial:
			view.Summary.ProfessionalSocial++
			view.Lanes.SocialQueue = append(view.Lanes.SocialQueue, card)
		case models.ActionContactForm:
			view.Summary.ContactForms++
			view.Lanes.ContactFormQueue = append(view.Lanes.ContactFormQueue, card)
		default:
			view.Summary.LowConfidence++
			view.Lanes.LowConfidence = append(view.Lanes.LowConfidence, card)
		}
	}
	view.Summary.Total = len(view.Actions)
	return view
}

func SummarizeActionMetrics(actions []models.OutreachCommercialAction) ActionMetrics {
	m := ActionMetrics{
		ByActionType:   map[string]int{},
		ByReachability: map[string]int{},
	}
	var executed, reachedKnown, reached, conv, interest, meeting int
	for _, a := range actions {
		if a.ActionType != "" {
			m.Planned++
			m.ByActionType[a.ActionType]++
		}
		if a.ReachabilityClass != "" {
			m.ByReachability[a.ReachabilityClass]++
		}
		switch a.State {
		case models.ActionStateCompleted, models.ActionStateNeedsFollowup, models.ActionStateSkipped, models.ActionStateBlocked:
			if a.OutcomeCode != "" {
				executed++
			}
		}
		if a.OutcomeCode == "" {
			continue
		}
		switch a.OutcomeCode {
		case models.OutcomeWrongPerson:
			m.WrongPerson++
		case models.OutcomeWrongChannel:
			m.WrongChannel++
		case models.OutcomeReferredToOtherPerson:
			m.ReferralCount++
		case models.OutcomeDNCCode:
			m.DNCCount++
		case models.OutcomeBouncedCode:
			m.BounceCount++
		case models.OutcomeMeetingScheduled:
			meeting++
		case models.OutcomeInterested:
			interest++
		}
		if a.TargetReached != nil {
			reachedKnown++
			if *a.TargetReached {
				reached++
			}
		}
		if a.ConversationStarted {
			conv++
		}
	}
	m.Executed = executed
	m.ExecutedDenom = executed
	m.TargetReachedCount = reached
	m.ConversationCount = conv
	m.InterestCount = interest
	m.MeetingCount = meeting
	if reachedKnown > 0 {
		v := float64(reached) / float64(reachedKnown)
		m.TargetReachedRate = &v
	}
	if executed > 0 {
		cv := float64(conv) / float64(executed)
		m.ConversationRate = &cv
		iv := float64(interest) / float64(executed)
		m.InterestRate = &iv
		mv := float64(meeting) / float64(executed)
		m.MeetingRate = &mv
	}
	return m
}
