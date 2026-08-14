package confenge

import (
	"crypto/sha1"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// actionNS is a stable UUID namespace for deterministic commercial-action IDs.
var actionNS = uuid.MustParse("a11c7100-c0ff-4e11-9e55-0c0ffee00001")

// PlanInput is the mapper boundary: a feed/account snapshot plus optional
// additive reachability. Missing reachability is not invented.
type PlanInput struct {
	Account    *models.OutreachAccount
	Candidate  *models.OutreachContactCandidate
	Candidates []models.OutreachContactCandidate
	Evidence   []models.OutreachEvidence
	Now        time.Time
	Snapshot   string

	BlockedPersonFingerprint string
	BlockedRouteFingerprint  string
	PersonRequiresReview     bool
	RouteRequiresReview      bool
	Stale                    bool
}

// PlannedAction is the mapper output. NoAction is true for R0 / exhausted.
type PlannedAction struct {
	Action         models.OutreachCommercialAction
	NoAction       bool
	RecipientState string
}

// PlanCommercialAction is the shipped mapper. Fixtures, import, and Today
// all call this function. It never promotes generic/role/inferred email
// to VALIDATED and never marks a manual route dispatchable.
func PlanCommercialAction(in PlanInput) PlannedAction {
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	acc := in.Account
	c := in.Candidate
	if c == nil && len(in.Candidates) > 0 {
		c = &in.Candidates[0]
	}

	rawClass := ""
	routeType, routeRel, channelVal, channelDisp := "", "", "", ""
	if c != nil {
		rawClass = c.ReachabilityClass
		routeType = c.RouteType
		routeRel = MapRouteRelation(c.RouteRelation)
		channelVal = firstNonEmpty(c.ChannelValue, firstNonEmpty(c.PhoneE164, c.Phone), c.Email, c.LinkedInURL)
		channelDisp = c.ChannelDisplay
	}
	class := MapReachability(rawClass)

	out := models.OutreachCommercialAction{
		MappingVersion:    models.ReachabilityMappingVersionV1,
		ReachabilityClass: class,
		RouteType:         routeType,
		RouteRelation:     routeRel,
		ChannelValue:      channelVal,
		ChannelDisplay:    channelDisp,
		State:             models.ActionStatePlanned,
		CreatedAt:         now,
		UpdatedAt:         now,
		RequiresFresh:     in.Stale,
	}
	if acc != nil {
		out.AccountID = acc.ID
		out.OrganizationID = acc.OrganizationID
		out.SourceLeadID = acc.SourceLeadID
		out.CompanyName = accName(acc)
		out.WhyNow = firstNonEmpty(acc.MomentSummary, acc.FactToMention)
		out.FactualHook = acc.FactToMention
		out.ServiceCode = acc.ServiceCode
		out.ServiceContext = acc.EntryOffer
		out.PriorityRank = acc.PriorityRank
		out.PriorityScore = firstPositive(acc.ActivationScore, acc.PriorityScore)
		out.SnapshotHash = firstNonEmpty(in.Snapshot, acc.LastPayloadHash, acc.ActivationSourceHash)
		out.EvidenceIDs = append([]string{}, acc.MomentEvidenceIDs...)
	}
	if c != nil {
		id := c.ID
		if id != uuid.Nil {
			out.CandidateID = &id
		}
		out.Confidence = c.Confidence
		out.PersonID = firstNonEmpty(c.PersonID, "")
		if provenPersonName(c) {
			out.PersonName = strings.TrimSpace(c.Name)
		}
		if provenRole(c) {
			out.ObservedRole = strings.TrimSpace(c.Role)
			out.TargetRole = out.ObservedRole
		} else if strings.TrimSpace(c.Role) != "" && !provenPersonName(c) {
			out.TargetRole = strings.TrimSpace(c.Role)
		}
		if channelDisp == "" {
			out.ChannelDisplay = defaultChannelDisplay(c, routeRel)
		}
	}
	for _, ev := range in.Evidence {
		if ev.SourceEvidenceID != "" {
			out.EvidenceIDs = appendUnique(out.EvidenceIDs, ev.SourceEvidenceID)
		}
	}

	if acc != nil && (acc.DoNotContact || acc.Blocked) {
		return blockedPlan(out, "Conta bloqueada ou marcada como nao contatar.")
	}
	if c != nil && (c.DoNotContact || c.Blocked || c.Bounced) {
		return blockedPlan(out, firstNonEmpty(c.BlockReason, "Destinatario bloqueado, bounce ou opt-out."))
	}
	if class == models.ReachabilityBlocked {
		return blockedPlan(out, "Rota bloqueada (DNC/supressao/conflito).")
	}

	rec := ResolveRecipient(acc, planCandidates(in, c), now)
	cls := ClassifyContactTier(acc, c, now)
	pb := MustPlaybook()
	_, plan := BuildOutboundPlan(pb, acc, c, in.Evidence, 1)
	body := ""
	if cls.Tier == ContactTierA && rec.State == RecipientValidated && plan.Messageability == MessageabilityReady {
		composed := ComposeFromPlan(plan, acc, c, ChannelEmailInitial)
		body = composed.BodyText
	}

	// Reachability, when present, is the authority for action type.
	// Absent class falls back to the current contact-tier contract.
	switch class {
	case models.ReachabilityR0None:
		out.Lane = models.LaneBlockedAction
		out.RecommendedAction = "Sem rota acionavel."
		out.Warnings = append(out.Warnings, "Nenhuma rota comercial publicada.")
		return PlannedAction{Action: out, NoAction: true, RecipientState: rec.State}
	case models.ReachabilityUnmapped:
		out.ActionType = models.ActionOtherManual
		out.Lane = models.LaneNeedsEnrichment
		out.Actionable = false
		out.EmailSendable = false
		out.Dispatchable = false
		out.RecommendedAction = "Classe de alcance desconhecida. Nao enviar automaticamente."
		out.Warnings = append(out.Warnings, "reachability_unmapped: fail-closed, sem auto-envio.")
		return applySuppression(in, finalizePlan(out, rec.State))
	case models.ReachabilityR1Direct:
		return applySuppression(in, planR1(out, rec, cls, plan, body))
	case models.ReachabilityR2Inferred:
		return applySuppression(in, planR2(out, rec))
	case models.ReachabilityR3Routed:
		return applySuppression(in, planR3(out, rec))
	case models.ReachabilityR4Role:
		return applySuppression(in, planR4(out, rec))
	case models.ReachabilityR5Corporate:
		return applySuppression(in, planR5(out, c, rec))
	}

	// Current-contract path: do not invent an R-class.
	return applySuppression(in, planFromCurrentContract(out, acc, c, rec, cls, plan, body))
}

func planR1(out models.OutreachCommercialAction, rec RecipientResolution, cls ContactClass, plan OutboundMessagePlan, body string) PlannedAction {
	out.ActionType = models.ActionDirectEmail
	out.RouteType = firstNonEmpty(out.RouteType, "email")
	out.RouteRelation = firstNonEmpty(out.RouteRelation, models.RouteRelBelongsToNamedPerson)
	sendable := rec.State == RecipientValidated && cls.Tier == ContactTierA &&
		plan.Messageability == MessageabilityReady && strings.TrimSpace(body) != ""
	out.EmailSendable = sendable
	out.Dispatchable = false
	out.Actionable = sendable || rec.State == RecipientValidated
	if sendable {
		out.Lane = models.LaneEmailNeedsReview
		out.State = models.ActionStateReady
		out.RecommendedAction = "Revisar e autorizar o e-mail direto. Envio so apos aprovacao humana."
	} else {
		out.Lane = models.LaneNeedsEnrichment
		out.RecommendedAction = "E-mail direto observado, mas ainda nao sendable (VALIDATED+READY)."
		out.Warnings = append(out.Warnings, "R1 sem messageability READY nao entra em NEEDS_REVIEW.")
	}
	return finalizePlan(out, rec.State)
}

func planR2(out models.OutreachCommercialAction, rec RecipientResolution) PlannedAction {
	out.ActionType = models.ActionInferredEmailReview
	out.RouteType = firstNonEmpty(out.RouteType, "email")
	out.Lane = models.LaneHumanReviewEmail
	out.State = models.ActionStateReady
	out.Actionable = true
	out.EmailSendable = false
	out.Dispatchable = false
	out.RecommendedAction = "Revisar e-mail inferido. Nao entra no pipeline VALIDATED e nao autoriza dispatch."
	out.Warnings = append(out.Warnings,
		"E-mail inferido: nao e VALIDATED.",
		"Nenhum auto-envio. HUMAN_REVIEW_REQUIRED.",
	)
	// Force recipient state off VALIDATED for this action even if the
	// existing resolver would accept the address.
	return finalizePlan(out, RecipientException)
}

func planR3(out models.OutreachCommercialAction, rec RecipientResolution) PlannedAction {
	out.ActionType = models.ActionRoutedCall
	out.RouteType = firstNonEmpty(out.RouteType, "phone")
	out.RouteRelation = firstNonEmpty(out.RouteRelation, models.RouteRelRoutesToNamedPerson)
	out.Lane = models.LaneRoutedCallQueue
	out.State = models.ActionStateReady
	out.Actionable = true
	out.EmailSendable = false
	out.Dispatchable = false
	person := firstNonEmpty(out.PersonName, "a pessoa alvo")
	out.RecommendedAction = "Ligar para o telefone oficial da empresa e pedir para falar com " + person + "."
	out.Warnings = append(out.Warnings,
		"Este numero e da empresa. Nao e o telefone direto de "+person+".",
		"Nao alegar que o ramal pertence a "+person+".",
	)
	if out.ChannelDisplay == "" {
		out.ChannelDisplay = "telefone oficial da empresa"
	}
	return finalizePlan(out, rec.State)
}

func planR4(out models.OutreachCommercialAction, rec RecipientResolution) PlannedAction {
	out.ActionType = models.ActionRoleEmail
	out.RouteType = firstNonEmpty(out.RouteType, "email")
	out.RouteRelation = firstNonEmpty(out.RouteRelation, models.RouteRelRoleMailbox)
	out.Lane = models.LaneRoleEmailQueue
	out.State = models.ActionStateReady
	out.Actionable = true
	out.EmailSendable = false
	out.Dispatchable = false
	out.PersonName = "" // never invent a person for a role mailbox
	out.RecommendedAction = "Escrever para a caixa funcional. Nao tratar como e-mail pessoal."
	out.Warnings = append(out.Warnings, "Caixa funcional oficial. Nao e uma pessoa nomeada.")
	return finalizePlan(out, rec.State)
}

func planR5(out models.OutreachCommercialAction, c *models.OutreachContactCandidate, rec RecipientResolution) PlannedAction {
	ch := ""
	if c != nil {
		ch = strings.ToLower(strings.TrimSpace(firstNonEmpty(c.RouteType, channelFromCandidate(c))))
	}
	switch {
	case ch == "form" || ch == "contact_form":
		out.ActionType = models.ActionContactForm
	default:
		out.ActionType = models.ActionGenericEmail
	}
	out.Lane = models.LaneLowConfidenceManual
	out.RouteRelation = firstNonEmpty(out.RouteRelation, models.RouteRelCorporateGeneric)
	out.State = models.ActionStateReady
	out.Actionable = true
	out.EmailSendable = false
	out.Dispatchable = false
	out.PersonName = ""
	out.RecommendedAction = "Abordagem manual de baixa confianca. Nunca mascarar como pessoa."
	out.Warnings = append(out.Warnings, "Rota corporativa generica. Nao e destinatario nominal.")
	return finalizePlan(out, rec.State)
}

func planFromCurrentContract(out models.OutreachCommercialAction, acc *models.OutreachAccount, c *models.OutreachContactCandidate, rec RecipientResolution, cls ContactClass, plan OutboundMessagePlan, body string) PlannedAction {
	lane := ClassifyActionLane(cls, rec, plan, body)
	switch {
	case lane == LaneNeedsReviewEmail:
		out.ActionType = models.ActionDirectEmail
		out.Lane = models.LaneEmailNeedsReview
		out.EmailSendable = true
		out.Actionable = true
		out.Dispatchable = false
		out.State = models.ActionStateReady
		out.RecommendedAction = "Revisar e autorizar o e-mail direto."
	case cls.Tier == ContactTierB:
		return planNamedManual(out, c, rec)
	case cls.Tier == ContactTierC:
		return planR4(out, rec)
	case cls.Tier == ContactTierD:
		return planR5(out, c, rec)
	default:
		out.Lane = models.LaneBlockedAction
		out.RecommendedAction = "Sem acao automatica."
		out.Warnings = append(out.Warnings, firstNonEmpty(cls.Warning, "Sem fundamento suficiente para um proximo passo."))
		return PlannedAction{Action: out, NoAction: true, RecipientState: rec.State}
	}
	return finalizePlan(out, rec.State)
}

func planNamedManual(out models.OutreachCommercialAction, c *models.OutreachContactCandidate, rec RecipientResolution) PlannedAction {
	out.Actionable = true
	out.EmailSendable = false
	out.Dispatchable = false
	out.State = models.ActionStateReady
	rel := out.RouteRelation
	phone := ""
	linkedin := ""
	wa := ""
	if c != nil {
		phone = strings.TrimSpace(firstNonEmpty(c.PhoneE164, c.Phone))
		linkedin = strings.TrimSpace(c.LinkedInURL)
		if strings.EqualFold(c.WhatsAppConsentStatus, "CONSENTED") || strings.EqualFold(c.WhatsAppConsentStatus, "OPT_IN") {
			wa = phone
		}
	}
	switch {
	case rel == models.RouteRelRoutesToNamedPerson && phone != "":
		return planR3(out, rec)
	case rel == models.RouteRelBelongsToNamedPerson && phone != "":
		out.ActionType = models.ActionDirectCall
		out.Lane = models.LaneCallQueue
		out.RouteType = "phone"
		out.RecommendedAction = "Ligar para o telefone publicado da pessoa."
	case wa != "":
		out.ActionType = models.ActionWhatsApp
		out.Lane = models.LaneWhatsAppQueue
		out.RouteType = "whatsapp"
		out.RecommendedAction = "Enviar WhatsApp manualmente. O sistema nao envia."
	case linkedin != "":
		out.ActionType = models.ActionProfessionalSocial
		out.Lane = models.LaneProfessionalSocialQueue
		out.RouteType = "linkedin"
		out.RecommendedAction = "Abordar o perfil profissional publicado. Sem envio automatico."
	case phone != "":
		// Fail closed: a company number plus a named person is routed, not direct.
		return planR3(out, rec)
	default:
		out.ActionType = models.ActionOtherManual
		out.Lane = LaneManualOutreach
		out.RecommendedAction = "Abordar no canal publicado. Nao enviar e-mail automatico."
	}
	return finalizePlan(out, rec.State)
}

func blockedPlan(out models.OutreachCommercialAction, reason string) PlannedAction {
	out.ActionType = models.ActionOtherManual
	out.ReachabilityClass = firstNonEmpty(out.ReachabilityClass, models.ReachabilityBlocked)
	out.Lane = models.LaneBlockedAction
	out.State = models.ActionStateBlocked
	out.Actionable = false
	out.EmailSendable = false
	out.Dispatchable = false
	out.RecommendedAction = "Nao abordar."
	out.Warnings = append(out.Warnings, reason)
	return finalizePlan(out, RecipientBlocked)
}

func finalizePlan(out models.OutreachCommercialAction, recState string) PlannedAction {
	out.PersonFingerprint = personFingerprint(out.PersonName, out.ObservedRole)
	out.RouteFingerprint = routeFingerprint(out.ActionType, out.RouteType, out.RouteRelation, out.ChannelValue, out.PersonName)
	out.IdempotencyKey = actionIdempotency(out)
	out.ID = DeterministicActionID(out.OrganizationID, out.AccountID, out.ActionType, out.RouteFingerprint)
	if out.ContentJSON == nil {
		content := ComposeActionContent(out)
		out.ContentJSON = mustJSON(content)
		out.ContentHash = contentHashOf(content)
	}
	if out.RecommendedAction == "" {
		out.RecommendedAction = defaultRecommended(out)
	}
	return PlannedAction{Action: out, RecipientState: recState}
}

func applySuppression(in PlanInput, planned PlannedAction) PlannedAction {
	if planned.NoAction {
		return planned
	}
	a := planned.Action
	if in.PersonRequiresReview && a.PersonFingerprint != "" && a.PersonFingerprint == in.BlockedPersonFingerprint {
		a.Actionable = false
		a.EmailSendable = false
		a.Dispatchable = false
		a.Lane = models.LaneNeedsEnrichment
		a.State = models.ActionStatePlanned
		a.Warnings = appendUnique(a.Warnings, "WRONG_PERSON: mesma pessoa exige revisao humana antes de recomendar de novo.")
		planned.Action = a
		return planned
	}
	if in.RouteRequiresReview && a.RouteFingerprint != "" && a.RouteFingerprint == in.BlockedRouteFingerprint {
		a.Actionable = false
		a.EmailSendable = false
		a.Dispatchable = false
		a.Lane = models.LaneNeedsEnrichment
		a.State = models.ActionStatePlanned
		a.Warnings = appendUnique(a.Warnings, "INVALID_ROUTE: a mesma rota nao pode ser replanejada em silencio.")
		planned.Action = a
		return planned
	}
	if in.Stale {
		MarkStaleFreshness(&a, "Snapshot upstream desatualizado. Nao executar sem revisar.")
		planned.Action = a
	}
	return planned
}

// DeterministicActionID is stable across replay of the same open work.
func DeterministicActionID(orgID, accountID uuid.UUID, actionType, routeFP string) uuid.UUID {
	payload := orgID.String() + "|" + accountID.String() + "|" + actionType + "|" + routeFP
	return uuid.NewSHA1(actionNS, []byte(payload))
}

func actionIdempotency(a models.OutreachCommercialAction) string {
	lead := firstNonEmpty(a.SourceLeadID, a.AccountID.String())
	return "action:" + lead + ":" + a.ActionType + ":" + a.RouteFingerprint
}

func personFingerprint(name, role string) string {
	return strings.ToLower(strings.TrimSpace(name)) + "|" + strings.ToLower(strings.TrimSpace(role))
}

func routeFingerprint(actionType, routeType, rel, value, person string) string {
	return strings.ToLower(strings.Join([]string{actionType, routeType, rel, strings.TrimSpace(value), strings.TrimSpace(person)}, "|"))
}

func contentHashOf(c CommercialActionContent) string {
	sum := sha1.Sum([]byte(c.Kind + "|" + c.Subject + "|" + c.Body + "|" + c.Opening + "|" + c.Ask))
	return fmt.Sprintf("%x", sum[:])
}

func planCandidates(in PlanInput, c *models.OutreachContactCandidate) []models.OutreachContactCandidate {
	if len(in.Candidates) > 0 {
		return in.Candidates
	}
	if c != nil {
		return []models.OutreachContactCandidate{*c}
	}
	return nil
}

func defaultChannelDisplay(c *models.OutreachContactCandidate, rel string) string {
	if c == nil {
		return ""
	}
	if rel == models.RouteRelRoutesToNamedPerson {
		return "telefone oficial da empresa"
	}
	if v := strings.TrimSpace(firstNonEmpty(c.PhoneE164, c.Phone)); v != "" {
		return v
	}
	if c.Email != "" {
		return c.Email
	}
	return strings.TrimSpace(c.LinkedInURL)
}

func defaultRecommended(a models.OutreachCommercialAction) string {
	switch a.ActionType {
	case models.ActionDirectCall:
		return "Ligar para o numero publicado."
	case models.ActionRoutedCall:
		return "Ligar para a empresa e pedir a pessoa alvo."
	case models.ActionWhatsApp:
		return "Enviar WhatsApp manualmente."
	case models.ActionProfessionalSocial:
		return "Enviar abordagem curta no perfil profissional."
	case models.ActionContactForm:
		return "Colar a mensagem no formulario publicado."
	case models.ActionRoleEmail:
		return "Escrever para a caixa da funcao."
	default:
		return "Executar a acao comercial recomendada."
	}
}

func firstPositive(vals ...float64) float64 {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}
