package confenge

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/warmbly/warmbly/internal/models"
)

const (
	SchemaOperatorProjection  = "confenge.operator_projection.v1"
	SchemaDecisionUnitAccount = "confenge.decision_unit_account.v1"
	SchemaDUIRun              = "confenge.dui.run.v1"

	ActionModeManualRoutedCall     = "MANUAL_ROUTED_CALL"
	ActionModeManualCall           = "MANUAL_CALL"
	ActionModeHumanReviewEmail     = "HUMAN_REVIEW_EMAIL"
	ActionModeDirectEmailValidated = "DIRECT_EMAIL_VALIDATED"
	ActionModeNamedHumanManual     = "NAMED_HUMAN_MANUAL_CHANNEL"
	ActionModeRoleEmail            = "ROLE_EMAIL"
	ActionModeRoleMailbox          = "ROLE_MAILBOX"
	ActionModeGenericEmail         = "GENERIC_EMAIL_LAST_RESORT"
	ActionModeGeneric              = "GENERIC"
	ActionModeContactForm          = "CONTACT_FORM"
	ActionModeNeedsEnrichment      = "NEEDS_ENRICHMENT"
	ActionModeNoActionable         = "NO_ACTIONABLE_ROUTE"
	ActionModeBlocked              = "BLOCKED"
	ActionModeManualWhatsApp       = "MANUAL_WHATSAPP"
	ActionModeManualSocial         = "MANUAL_PROFESSIONAL_SOCIAL"
)

// MapActionMode translates a published extra-cli ActionMode into a
// reachability class. Empty input is left empty. Unknown tokens fail closed
// to UNMAPPED so they cannot become email-sendable.
func MapActionMode(raw string) string {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "":
		return ""
	case ActionModeManualRoutedCall:
		return models.ReachabilityR3Routed
	case ActionModeHumanReviewEmail, ActionModeDirectEmailValidated, "DIRECT_EMAIL":
		return models.ReachabilityR1Direct
	case ActionModeRoleEmail, ActionModeRoleMailbox:
		return models.ReachabilityR4Role
	case ActionModeGenericEmail, ActionModeGeneric, ActionModeContactForm:
		return models.ReachabilityR5Corporate
	case ActionModeNeedsEnrichment, ActionModeNoActionable:
		return models.ReachabilityR0None
	case ActionModeBlocked:
		return models.ReachabilityBlocked
	case ActionModeNamedHumanManual, ActionModeManualCall, ActionModeManualWhatsApp, ActionModeManualSocial:
		// Named-human manual lanes stay on the current-contract path unless
		// extra-cli also published a reachability class.
		return ""
	default:
		return models.ReachabilityUnmapped
	}
}

// ResolveImportedRoute prefers a published reachability class. ActionMode
// only fills a class when extra-cli omitted one.
func ResolveImportedRoute(actionMode, reachability, relation string) (class, rel string) {
	class = MapReachability(reachability)
	if class == "" {
		class = MapActionMode(actionMode)
	}
	rel = MapRouteRelation(relation)
	if rel == "" && class == models.ReachabilityR3Routed {
		rel = models.RouteRelRoutesToNamedPerson
	}
	if rel == "" && class == models.ReachabilityR4Role {
		rel = models.RouteRelRoleMailbox
	}
	if rel == "" && class == models.ReachabilityR5Corporate {
		rel = models.RouteRelCorporateGeneric
	}
	return class, rel
}

func isOperatorProjection(schemaID, schemaVersion string, wrap map[string]any) bool {
	switch strings.TrimSpace(schemaID) {
	case SchemaOperatorProjection, SchemaDecisionUnitAccount, SchemaDUIRun:
		return true
	}
	if wrap == nil {
		return false
	}
	if _, ok := wrap["cards"]; ok && wrap["leads"] == nil {
		return true
	}
	if _, ok := wrap["accounts"]; ok && wrap["leads"] == nil && schemaVersion != models.OutreachSchemaV1 {
		return true
	}
	return false
}

func normalizeOperatorProjection(raw []byte) (*Feed, error) {
	var wrap map[string]any
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, fmt.Errorf("operator projection: %w", err)
	}
	src := FeedSource{
		System:         firstNonEmpty(strField(wrap, "system"), "extra-cli"),
		RunID:          strField(wrap, "run_id", "source_run_id"),
		SnapshotHash:   strField(wrap, "snapshot_hash"),
		RepoSHA:        strField(wrap, "repo_sha", "git_sha"),
		ProfileID:      firstNonEmpty(strField(wrap, "profile_id"), "confenge"),
		ProfileVersion: firstNonEmpty(strField(wrap, "profile_version", "policy_version"), "dui.policy.v1"),
	}
	if nested, ok := wrap["source"].(map[string]any); ok {
		src.System = firstNonEmpty(strField(nested, "system"), src.System)
		src.RunID = firstNonEmpty(strField(nested, "run_id", "source_run_id"), src.RunID)
		src.SnapshotHash = firstNonEmpty(strField(nested, "snapshot_hash"), src.SnapshotHash)
		src.RepoSHA = firstNonEmpty(strField(nested, "repo_sha"), src.RepoSHA)
		src.ProfileID = firstNonEmpty(strField(nested, "profile_id"), src.ProfileID)
		src.ProfileVersion = firstNonEmpty(strField(nested, "profile_version"), src.ProfileVersion)
	}

	accounts := indexDUIAccounts(wrap)
	cards, _ := asMapSlice(wrap["cards"])
	if len(cards) == 0 && wrap["schema_id"] == SchemaDecisionUnitAccount {
		cards = []map[string]any{wrap}
	}
	if len(cards) == 0 && len(accounts) > 0 {
		for _, acc := range accounts {
			cards = append(cards, acc)
		}
	}
	if len(cards) == 0 {
		return nil, fmt.Errorf("operator projection has no cards or accounts")
	}

	leads := make([]FeedLead, 0, len(cards))
	for i, card := range cards {
		cnpj := digitsOnly(strField(card, "cnpj", "cnpj14"))
		dui := accounts[cnpj]
		leads = append(leads, operatorCardToLead(card, dui, i+1))
	}
	generated := strField(wrap, "generated_at", "built_at")
	if generated == "" {
		generated = time.Now().UTC().Format(time.RFC3339)
	}
	return &Feed{
		SchemaVersion: models.OutreachSchemaV1,
		GeneratedAt:   generated,
		Source:        src,
		Leads:         leads,
		Legacy:        true,
	}, nil
}

func indexDUIAccounts(wrap map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	arr, err := asMapSlice(wrap["accounts"])
	if err != nil || len(arr) == 0 {
		if wrap["schema_id"] == SchemaDecisionUnitAccount {
			cnpj := digitsOnly(strField(wrap, "cnpj", "cnpj14"))
			if cnpj != "" {
				out[cnpj] = wrap
			}
		}
		return out
	}
	for _, acc := range arr {
		cnpj := digitsOnly(strField(acc, "cnpj", "cnpj14"))
		if cnpj != "" {
			out[cnpj] = acc
		}
	}
	return out
}

func operatorCardToLead(card, dui map[string]any, rank int) FeedLead {
	cnpj := digitsOnly(strField(card, "cnpj", "cnpj14"))
	if cnpj == "" && dui != nil {
		cnpj = digitsOnly(strField(dui, "cnpj", "cnpj14"))
	}
	accountID := firstNonEmpty(
		strField(card, "company_entity_id", "account_id", "canonical_account_id"),
		strField(dui, "company_entity_id", "account_id"),
		"cnpj:"+cnpj,
	)
	name := firstNonEmpty(strField(card, "empresa", "legal_name", "razao_social"), strField(dui, "legal_name"))
	whyNow := firstNonEmpty(strField(card, "why_now"), strField(dui, "why_now"))
	offer := firstNonEmpty(strField(card, "oferta_recomendada", "service_context", "service"), strField(dui, "service_context"))
	actionMode := firstNonEmpty(strField(card, "action_mode"), recString(dui, "action_mode"))
	routeClass := firstNonEmpty(strField(card, "route_class", "reachability_class"), recString(dui, "reachability_class"))
	relation := firstNonEmpty(strField(card, "route_relation"), recString(dui, "route_relation"))
	class, rel := ResolveImportedRoute(actionMode, routeClass, relation)
	personName := firstNonEmpty(strField(card, "primary_decision_unit_target", "person_name", "person"), duiPersonName(dui))
	role := firstNonEmpty(roleFromCard(card), duiPersonRole(dui))
	personID := firstNonEmpty(strField(card, "person_id"), duiPersonID(dui))
	candidateID := firstNonEmpty(strField(card, "candidate_id", "source_contact_id"), duiCandidateID(dui), personID)
	channel := firstNonEmpty(strField(card, "channel", "channel_value"), duiChannel(dui))
	channelType := firstNonEmpty(strField(card, "primary_route", "route_type", "channel_type"), duiChannelType(dui))
	next := firstNonEmpty(strField(card, "exact_next_action", "recommended_action", "next_action"), recString(dui, "next_action"))
	confidence := firstNonEmpty(strField(card, "confidence"), cardConfidence(card), duiConfidence(dui))
	doNot := stringSlice(card["do_not_claim"])
	if len(doNot) == 0 && dui != nil {
		doNot = stringSlice(dui["warnings"])
	}
	evidenceIDs := collectOperatorEvidenceIDs(card, dui)
	email, phone, site := splitChannel(channel, channelType)
	// Operator projection is WHO/WHY NOW. Generic/role mailboxes stay unsendable.
	sendReady := false
	if class == models.ReachabilityR1Direct && email != "" && personName != "" {
		// Still not VALIDATED here. EmailSendReady stays false unless extra-cli said so.
		if b, ok := card["email_send_ready"].(bool); ok {
			sendReady = b
		}
	}
	sendPtr := boolPtr(sendReady)

	contact := FeedContact{
		SourceContactID:    firstNonEmpty(candidateID, personID),
		PersonID:           personID,
		Name:               personName,
		Role:               role,
		Email:              email,
		Phone:              phone,
		SourceURL:          firstNonEmpty(strField(card, "channel_source_url"), site),
		VerificationStatus: "OFFICIAL_SOURCE",
		Confidence:         firstNonEmpty(confidence, "MEDIUM"),
		Recommended:        true,
		EmailSendReady:     sendPtr,
		MailboxPurpose:     mailboxPurposeFor(class, actionMode, email),
		OwnershipStatus:    firstNonEmpty(strField(card, "channel_ownership"), "COMPANY_OWNED"),
		ContactTier:        contactTierFor(class, actionMode),
		Channel:            routeTypeFor(channelType, class, phone, email),
		ReachabilityClass:  class,
		RouteType:          routeTypeFor(channelType, class, phone, email),
		RouteRelation:      rel,
		ChannelValue:       firstNonEmpty(channel, phone, email),
		ChannelDisplay:     firstNonEmpty(strField(card, "channel_display"), channel),
		RecommendedAction:  next,
		ActionMode:         actionMode,
	}

	lead := FeedLead{
		SourceLeadID: accountID,
		Company: FeedCompany{
			CNPJ14:      cnpj,
			RazaoSocial: name,
			Website:     site,
		},
		Priority: FeedPriority{
			Rank:       rank,
			Confidence: firstNonEmpty(confidence, "MEDIUM"),
		},
		Moment: FeedMoment{
			Code:        "OPERATOR_PROJECTION",
			Summary:     whyNow,
			Confidence:  firstNonEmpty(confidence, "MEDIUM"),
			EvidenceIDs: evidenceIDs,
		},
		Offer: FeedOffer{
			ServiceCode: offer,
			ServiceName: offer,
			EntryOffer:  next,
		},
		MessagingContext: FeedMessaging{
			FactToMention: whyNow,
			ClaimsToAvoid: doNot,
		},
		Contacts:          []FeedContact{contact},
		Evidence:          evidenceFromIDs(evidenceIDs, card),
		CommercialState:   "NEW",
		RecommendedAction: next,
	}
	if class == models.ReachabilityR0None || actionMode == ActionModeNeedsEnrichment {
		lead.CommercialState = "NEEDS_CONTACT"
	}
	return lead
}

func recString(dui map[string]any, key string) string {
	if dui == nil {
		return ""
	}
	if rec, ok := dui["recommendation"].(map[string]any); ok {
		return strField(rec, key)
	}
	return ""
}

func duiPerson(dui map[string]any) map[string]any {
	if dui == nil {
		return nil
	}
	rec, _ := dui["recommendation"].(map[string]any)
	targetID := ""
	if rec != nil {
		targetID = strField(rec, "primary_target_id")
	}
	cands, _ := asMapSlice(dui["candidates"])
	for _, c := range cands {
		if targetID != "" && strField(c, "candidate_id") == targetID {
			return c
		}
	}
	if len(cands) > 0 {
		return cands[0]
	}
	return nil
}

func duiPersonName(dui map[string]any) string {
	p := duiPerson(dui)
	if p == nil {
		return ""
	}
	return strField(p, "person_name", "name")
}

func duiPersonRole(dui map[string]any) string {
	p := duiPerson(dui)
	if p == nil {
		return ""
	}
	if roles, ok := p["observed_roles"].([]any); ok && len(roles) > 0 {
		if s, ok := roles[0].(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return strField(p, "decision_role_class", "role")
}

func duiPersonID(dui map[string]any) string {
	p := duiPerson(dui)
	if p == nil {
		return ""
	}
	return strField(p, "person_id")
}

func duiCandidateID(dui map[string]any) string {
	p := duiPerson(dui)
	if p == nil {
		return ""
	}
	return strField(p, "candidate_id")
}

func duiPrimaryRoute(dui map[string]any) map[string]any {
	if dui == nil {
		return nil
	}
	rec, _ := dui["recommendation"].(map[string]any)
	routeID := ""
	if rec != nil {
		routeID = strField(rec, "primary_route_id")
	}
	routes, _ := asMapSlice(dui["routes"])
	for _, r := range routes {
		if routeID != "" && strField(r, "route_id") == routeID {
			return r
		}
	}
	if len(routes) > 0 {
		return routes[0]
	}
	return nil
}

func duiChannel(dui map[string]any) string {
	r := duiPrimaryRoute(dui)
	if r == nil {
		return ""
	}
	return strField(r, "channel_value", "channel")
}

func duiChannelType(dui map[string]any) string {
	r := duiPrimaryRoute(dui)
	if r == nil {
		return ""
	}
	return strField(r, "channel_type", "route_type")
}

func duiConfidence(dui map[string]any) string {
	p := duiPerson(dui)
	if p == nil {
		return ""
	}
	return strField(p, "identity_confidence", "role_confidence", "confidence")
}

func roleFromCard(card map[string]any) string {
	if ev, ok := card["role_evidence"].(map[string]any); ok {
		if roles, ok := ev["observed_roles"].([]any); ok && len(roles) > 0 {
			if s, ok := roles[0].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
		return strField(ev, "decision_role_class")
	}
	return strField(card, "role")
}

func cardConfidence(card map[string]any) string {
	if dims, ok := card["confidence_dimensions"].(map[string]any); ok {
		return firstNonEmpty(strField(dims, "route_confidence"), strField(dims, "identity_confidence"), strField(dims, "role_confidence"))
	}
	return ""
}

func collectOperatorEvidenceIDs(card, dui map[string]any) []string {
	var ids []string
	if ev, ok := card["role_evidence"].(map[string]any); ok {
		ids = append(ids, stringSlice(ev["evidence_ids"])...)
	}
	ids = append(ids, stringSlice(card["evidence_ids"])...)
	if dui != nil {
		if rec, ok := dui["recommendation"].(map[string]any); ok {
			ids = append(ids, stringSlice(rec["evidence_ids"])...)
		}
		p := duiPerson(dui)
		if p != nil {
			ids = append(ids, stringSlice(p["evidence_ids"])...)
		}
	}
	return uniqueNonEmpty(ids)
}

func evidenceFromIDs(ids []string, card map[string]any) []FeedEvidence {
	links := stringSlice(card["evidence_links"])
	out := make([]FeedEvidence, 0, len(ids))
	for i, id := range ids {
		url := ""
		if i < len(links) {
			url = links[i]
		}
		out = append(out, FeedEvidence{ID: id, Type: "operator_projection", URL: url})
	}
	return out
}

func splitChannel(channel, channelType string) (email, phone, site string) {
	v := strings.TrimSpace(channel)
	ct := strings.ToUpper(strings.TrimSpace(channelType))
	switch {
	case strings.Contains(v, "@"):
		email = strings.ToLower(v)
	case strings.HasPrefix(strings.ToLower(v), "http"):
		site = v
	case v != "":
		phone = v
	}
	if strings.Contains(ct, "EMAIL") && email == "" && strings.Contains(v, "@") {
		email = strings.ToLower(v)
	}
	if strings.Contains(ct, "PHONE") || strings.Contains(ct, "CALL") || strings.Contains(ct, "SWITCHBOARD") {
		if phone == "" && !strings.Contains(v, "@") && !strings.HasPrefix(strings.ToLower(v), "http") {
			phone = v
		}
	}
	return email, phone, site
}

func mailboxPurposeFor(class, actionMode, email string) string {
	switch class {
	case models.ReachabilityR4Role:
		return "ROLE_MAILBOX"
	case models.ReachabilityR5Corporate:
		return "GENERIC_CONTACT"
	case models.ReachabilityR3Routed:
		return "MANUAL_CHANNEL"
	}
	switch strings.ToUpper(strings.TrimSpace(actionMode)) {
	case ActionModeNamedHumanManual, ActionModeManualRoutedCall, ActionModeManualCall:
		return "MANUAL_CHANNEL"
	case ActionModeRoleEmail, ActionModeRoleMailbox:
		return "ROLE_MAILBOX"
	case ActionModeGeneric, ActionModeGenericEmail:
		return "GENERIC_CONTACT"
	}
	if email != "" && (isRoleMailboxLocal(email) || isGenericCorporateLocal(email)) {
		if isRoleMailboxLocal(email) {
			return "ROLE_MAILBOX"
		}
		return "GENERIC_CONTACT"
	}
	return ""
}

func contactTierFor(class, actionMode string) string {
	switch class {
	case models.ReachabilityR1Direct:
		return ContactTierA
	case models.ReachabilityR3Routed:
		return ContactTierB
	case models.ReachabilityR4Role:
		return ContactTierC
	case models.ReachabilityR5Corporate:
		return ContactTierD
	case models.ReachabilityR0None, models.ReachabilityBlocked, models.ReachabilityUnmapped:
		return ContactTierE
	}
	switch strings.ToUpper(strings.TrimSpace(actionMode)) {
	case ActionModeDirectEmailValidated, ActionModeHumanReviewEmail:
		return ContactTierA
	case ActionModeNamedHumanManual, ActionModeManualRoutedCall, ActionModeManualCall:
		return ContactTierB
	case ActionModeRoleEmail, ActionModeRoleMailbox:
		return ContactTierC
	case ActionModeGeneric, ActionModeGenericEmail, ActionModeContactForm:
		return ContactTierD
	}
	return ""
}

func routeTypeFor(channelType, class, phone, email string) string {
	ct := strings.ToUpper(strings.TrimSpace(channelType))
	switch {
	case strings.Contains(ct, "WHATSAPP"):
		return "whatsapp"
	case strings.Contains(ct, "FORM"):
		return "form"
	case strings.Contains(ct, "PROFILE") || strings.Contains(ct, "LINKEDIN") || strings.Contains(ct, "SOCIAL"):
		return "linkedin"
	case strings.Contains(ct, "EMAIL"):
		return "email"
	case strings.Contains(ct, "PHONE") || strings.Contains(ct, "CALL") || strings.Contains(ct, "SWITCHBOARD"):
		return "phone"
	case class == models.ReachabilityR3Routed || phone != "":
		return "phone"
	case email != "":
		return "email"
	default:
		return "manual"
	}
}

func stringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	default:
		return nil
	}
}

func uniqueNonEmpty(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func boolPtr(v bool) *bool { return &v }
