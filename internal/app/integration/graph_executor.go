package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

// executeAutomationGraph walks one automation's flow graph for a fired event:
// starting at the trigger node it follows edges, evaluating condition (IF) nodes
// against the event data to pick the true/false branch, and running every
// reachable action node. Best-effort and bounded (visited set + hop cap guard
// against cycles). Each action reuses runAction -> execAction unchanged.
func (s *service) executeAutomationGraph(ctx context.Context, a models.Automation, eventType string, data map[string]any) {
	// A stable per-event seed so random/chance splits + re-deliveries are
	// deterministic for a given event.
	baseSeed := stringFromMap(data, "delivery_id", "id", "booking_id", "contact_email", "invitee_email", "email")

	byID := make(map[string]models.AutomationNode, len(a.Graph.Nodes))
	for _, n := range a.Graph.Nodes {
		byID[n.ID] = n
	}
	outEdges := map[string][]models.AutomationEdge{}
	for _, e := range a.Graph.Edges {
		outEdges[e.Source] = append(outEdges[e.Source], e)
	}

	start := models.AutomationTriggerNodeID
	if _, ok := byID[start]; !ok {
		for _, n := range a.Graph.Nodes {
			if n.Type == models.AutomationNodeTrigger {
				start = n.ID
				break
			}
		}
	}

	// Best-effort run record — observability, never blocks the walk.
	run := &models.AutomationRun{AutomationID: a.ID, OrganizationID: a.OrganizationID, TriggerEvent: eventType, Status: "running"}
	_ = s.repo.CreateAutomationRun(ctx, run)
	results := []models.AutomationNodeResult{}
	anyError := false

	visited := map[string]bool{}
	queue := []string{start}
	const maxHops = 128
	for hops := 0; len(queue) > 0 && hops < maxHops; hops++ {
		id := queue[0]
		queue = queue[1:]
		if visited[id] {
			continue
		}
		visited[id] = true
		n, ok := byID[id]
		if !ok {
			continue
		}
		switch n.Type {
		case models.AutomationNodeCondition:
			res := false
			if n.Condition != nil {
				res = evaluateAutomationCondition(*n.Condition, data, baseSeed+"|"+id)
			}
			want := "false"
			status := "branch_false"
			if res {
				want = "true"
				status = "branch_true"
			}
			results = append(results, models.AutomationNodeResult{NodeID: id, Type: "condition", Label: conditionSummary(n.Condition), Status: status})
			// A condition only follows the branch matching its outcome. Edges
			// out of a condition are always labeled true/false (enforced on
			// write); an unlabeled edge here is malformed and ignored.
			for _, e := range outEdges[id] {
				if e.When == want {
					queue = append(queue, e.Target)
				}
			}
		case models.AutomationNodeAction:
			label, aerr := s.runGraphAction(ctx, a, n, eventType, data)
			nr := models.AutomationNodeResult{NodeID: id, Type: "action", Action: string(n.Action), Label: label, Status: "success"}
			if aerr != nil {
				nr.Status = "error"
				nr.Error = truncate(aerr.Error(), 300)
				anyError = true
			}
			results = append(results, nr)
			for _, e := range outEdges[id] {
				queue = append(queue, e.Target)
			}
		default: // trigger / unknown: just follow outgoing edges
			for _, e := range outEdges[id] {
				queue = append(queue, e.Target)
			}
		}
	}

	status := "success"
	if anyError {
		status = "error"
	}
	_ = s.repo.FinishAutomationRun(ctx, run.ID, status, "", results)

	// Best-effort realtime: refresh the builder's run history live.
	if s.publisher != nil {
		s.publisher.PublishAutomationEvent(ctx, a.OrganizationID, uuid.Nil, pubsub.EventAutomationRun, a.ID.String(), a.Name)
	}
}

// conditionSummary is a short backend label for a condition node in run history.
func conditionSummary(c *models.AutomationCondition) string {
	if c == nil {
		return "condition"
	}
	if c.Field == models.AutoCondExpression {
		return "expression"
	}
	field := c.Field
	if c.Field == models.AutoCondField && c.Key != "" {
		field = c.Key
	}
	if c.Operator != "" {
		return field + " " + c.Operator
	}
	return field
}

// automationDepthKey carries the re-entrancy depth through an automation's event
// data; maxAutomationChainDepth bounds it. Today the only launcher is a campaign
// "Run automation" step (depth 0 -> 1), so the guard never trips in practice. It
// is pre-emptive: if a future action ever launches a campaign or another
// automation, that launcher MUST forward this key so a misconfigured loop
// (campaign -> automation -> campaign -> ...) is bounded instead of infinite.
const (
	automationDepthKey      = "_automation_depth"
	maxAutomationChainDepth = 5
)

// RunAutomationByID runs one automation's graph on demand (launched from a
// campaign step), ignoring the trigger-matching gate. Condition nodes still
// evaluate against `data`. Returns a descriptive error when the automation is
// missing or disabled so the caller can record it — a disabled automation is a
// logged skip, never a silent no-op (a toggled-off automation must not make
// campaign steps quietly stop doing anything).
func (s *service) RunAutomationByID(ctx context.Context, orgID, automationID uuid.UUID, data map[string]any) error {
	depth := int(toFloat(data[automationDepthKey]))
	if depth >= maxAutomationChainDepth {
		return fmt.Errorf("automation chain depth limit (%d) reached; refusing to launch %s", maxAutomationChainDepth, automationID)
	}
	a, err := s.repo.GetAutomation(ctx, orgID, automationID)
	if err != nil {
		return err
	}
	if a == nil {
		return fmt.Errorf("automation not found")
	}
	if !a.Enabled {
		return fmt.Errorf("automation %q is disabled", a.Name)
	}
	eventType := a.TriggerEvent
	if eventType == "" {
		eventType = string(models.WebhookEventCampaignAction)
	}
	// Run on a COPY of the caller's data: a graph with several "run automation"
	// actions shares one data map, so bumping depth in place would let sibling #2
	// inherit the depth that sibling #1's nested chain reached and false-trip the
	// cap. The copy gives each launch the same starting depth, +1.
	d := make(map[string]any, len(data)+2)
	for k, v := range data {
		d[k] = v
	}
	d[automationDepthKey] = float64(depth + 1)
	d["trigger"] = "campaign" // provenance: launched on demand, not event-matched
	s.executeAutomationGraph(ctx, *a, eventType, d)
	return nil
}

// ListAutomationRuns returns recent run history for an automation.
func (s *service) ListAutomationRuns(ctx context.Context, orgID, id uuid.UUID, limit int) ([]models.AutomationRun, error) {
	return s.repo.ListAutomationRuns(ctx, orgID, id, limit)
}

// DryRunAutomation walks the graph against sample (or provided) data WITHOUT any
// side effects — no action execution, no sync runs, no run row, no realtime — and
// returns the path taken plus a preview of what each action would send.
func (s *service) DryRunAutomation(ctx context.Context, orgID, id uuid.UUID, req models.DryRunRequest) (*models.DryRunResponse, error) {
	a, err := s.repo.GetAutomation(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, fmt.Errorf("automation not found")
	}
	data := req.Data
	if len(data) == 0 {
		data = sampleEventData(a.TriggerEvent)
	}
	return &models.DryRunResponse{Trace: dryRunGraph(*a, data), Data: data}, nil
}

// dryRunGraph mirrors executeAutomationGraph's walk but records a preview trace
// instead of executing anything (conditions are pure, so they evaluate for real).
func dryRunGraph(a models.Automation, data map[string]any) []models.AutomationNodeResult {
	baseSeed := stringFromMap(data, "delivery_id", "id", "booking_id", "contact_email", "invitee_email", "email")
	byID := make(map[string]models.AutomationNode, len(a.Graph.Nodes))
	for _, n := range a.Graph.Nodes {
		byID[n.ID] = n
	}
	outEdges := map[string][]models.AutomationEdge{}
	for _, e := range a.Graph.Edges {
		outEdges[e.Source] = append(outEdges[e.Source], e)
	}
	start := models.AutomationTriggerNodeID
	if _, ok := byID[start]; !ok {
		for _, n := range a.Graph.Nodes {
			if n.Type == models.AutomationNodeTrigger {
				start = n.ID
				break
			}
		}
	}
	trace := []models.AutomationNodeResult{}
	visited := map[string]bool{}
	queue := []string{start}
	const maxHops = 128
	for hops := 0; len(queue) > 0 && hops < maxHops; hops++ {
		id := queue[0]
		queue = queue[1:]
		if visited[id] {
			continue
		}
		visited[id] = true
		n, ok := byID[id]
		if !ok {
			continue
		}
		switch n.Type {
		case models.AutomationNodeCondition:
			res := false
			if n.Condition != nil {
				res = evaluateAutomationCondition(*n.Condition, data, baseSeed+"|"+id)
			}
			want := "false"
			status := "branch_false"
			if res {
				want = "true"
				status = "branch_true"
			}
			trace = append(trace, models.AutomationNodeResult{NodeID: id, Type: "condition", Label: conditionSummary(n.Condition), Status: status})
			for _, e := range outEdges[id] {
				if e.When == want {
					queue = append(queue, e.Target)
				}
			}
		case models.AutomationNodeAction:
			trace = append(trace, models.AutomationNodeResult{
				NodeID:  id,
				Type:    "action",
				Action:  string(n.Action),
				Label:   string(n.Action),
				Status:  "success",
				Preview: actionPreview(n, data),
			})
			for _, e := range outEdges[id] {
				queue = append(queue, e.Target)
			}
		default:
			for _, e := range outEdges[id] {
				queue = append(queue, e.Target)
			}
		}
	}
	return trace
}

// actionPreview renders the templatable fields an action would send (no calls).
func actionPreview(n models.AutomationNode, data map[string]any) map[string]any {
	p := map[string]any{}
	for _, k := range []string{"channel", "url", "message_template"} {
		if v := configString(n.Config, k); v != "" {
			p[k] = renderTemplate(v, data)
		}
	}
	if models.IsNativeAction(n.Action) {
		cfg := parseNativeConfig(n.Config)
		if cfg.DealName != "" {
			p["deal_name"] = renderTemplate(cfg.DealName, data)
		}
		if cfg.TaskTitle != "" {
			p["task_title"] = renderTemplate(cfg.TaskTitle, data)
		}
	}
	return p
}

// sampleEventData builds a placeholder event payload for a trigger so a dry-run
// has something to render/evaluate against when the caller supplies none.
func sampleEventData(triggerEvent string) map[string]any {
	base := map[string]any{
		"contact_email": "jane@example.com",
		"contact_id":    "00000000-0000-0000-0000-000000000001",
		"campaign_id":   "00000000-0000-0000-0000-000000000002",
		"campaign_name": "Q3 Outbound",
		"first_name":    "Jane",
		"last_name":     "Doe",
		"company":       "Example Inc",
	}
	switch triggerEvent {
	case "campaign.reply_received":
		base["intent"] = "positive"
		base["confidence"] = 0.92
		base["subject"] = "Re: quick question"
		base["snippet"] = "Sure, let's talk next week."
	case "meeting.booked", "meeting.rescheduled", "meeting.canceled":
		base["source"] = "calendly"
		base["invitee_email"] = "jane@example.com"
		base["invitee_name"] = "Jane Doe"
		base["event_name"] = "Intro call"
		base["scheduled_for"] = "2026-07-01T15:00:00Z"
		base["join_url"] = "https://example.com/join/abc"
	case "campaign.email_bounced", "deliverability.bounce", "deliverability.complaint":
		base["event_type"] = "bounce"
		base["provider"] = "ses"
		base["reason"] = "mailbox full"
	case "warmup.health_changed":
		base["email"] = "sender@example.com"
		base["new_state"] = "watch"
		base["previous_state"] = "healthy"
		base["reason"] = "spam placement rising"
	}
	return base
}

// runGraphAction executes one action node by building a synthetic dispatch
// target and reusing runAction (which applies filters, logs a sync run, and
// updates connection health) -> execAction. The automation-wide filter is
// merged into the node config so any intent/confidence gate still applies.
// runGraphAction executes one action node and returns a label + the action error
// (for run-history recording). Native (Warmbly-internal) actions run without a
// connection; everything else builds a synthetic dispatch target and reuses
// runAction -> execAction.
func (s *service) runGraphAction(ctx context.Context, a models.Automation, n models.AutomationNode, eventType string, data map[string]any) (string, error) {
	if models.IsNativeAction(n.Action) {
		return string(n.Action), s.execNativeAction(ctx, a, n, data)
	}
	if n.ConnectionID == nil {
		return string(n.Action), nil
	}
	// Re-verify the connection belongs to this automation's org at run time
	// (defense in depth — write-time validation already checks this, but never
	// trust a stored connection id to fetch another org's secrets).
	owned, oerr := s.repo.GetConnectionByID(ctx, a.OrganizationID, *n.ConnectionID)
	if oerr != nil {
		return string(n.Action), oerr
	}
	if owned == nil {
		return string(n.Action), fmt.Errorf("connection not found")
	}
	sec, err := s.repo.GetConnectionSecrets(ctx, *n.ConnectionID)
	if err != nil {
		return string(n.Action), err
	}
	if sec == nil {
		return string(n.Action), fmt.Errorf("connection secrets unavailable")
	}
	sub := models.IntegrationEventSubscription{
		ID:             uuid.New(),
		ConnectionID:   *n.ConnectionID,
		OrganizationID: a.OrganizationID,
		EventType:      eventType,
		Action:         n.Action,
		Config:         mergeFilterIntoConfig(a.Filter, n.Config),
		Enabled:        true,
		UseCase:        "automation",
	}
	return string(n.Action), s.runAction(ctx, repository.DispatchTarget{Subscription: sub, Secrets: *sec}, data)
}

// evaluateAutomationCondition resolves an IF node against the event payload. The
// generic "field" type tests data[c.Key] with the operator; the rest are legacy
// semantic shortcuts (kept working for older saved automations).
func evaluateAutomationCondition(c models.AutomationCondition, data map[string]any, seed string) bool {
	switch c.Field {
	case models.AutoCondExpression:
		return EvalExpression(c.Expression, data)
	case models.AutoCondField:
		raw, present := data[c.Key]
		switch c.Operator {
		case models.AutoOpExists:
			return present && valueString(raw) != ""
		case models.AutoOpIsTrue:
			return toBool(raw)
		case models.AutoOpNotEquals:
			return !strings.EqualFold(valueString(raw), valueString(c.Value))
		case models.AutoOpContains:
			return strings.Contains(strings.ToLower(valueString(raw)), strings.ToLower(valueString(c.Value)))
		case models.AutoOpGte:
			return toFloat(raw) >= toFloat(c.Value)
		case models.AutoOpLte:
			return toFloat(raw) <= toFloat(c.Value)
		case models.AutoOpChance:
			return chanceHit(seed, toFloat(c.Value))
		default: // equals
			return strings.EqualFold(valueString(raw), valueString(c.Value))
		}
	case models.AutoCondIntent:
		return strings.EqualFold(stringFromMap(data, "intent"), valueString(c.Value))
	case models.AutoCondSource:
		return strings.EqualFold(stringFromMap(data, "source", "campaign", "provider"), valueString(c.Value))
	case models.AutoCondConfidence:
		return toFloat(data["confidence"]) >= toFloat(c.Value)
	case models.AutoCondHasContact:
		return contactEmail(data) != ""
	case models.AutoCondRandom:
		return chanceHit(seed, toFloat(c.Value))
	default:
		return false
	}
}

// toBool coerces an event-data value to a boolean (handles bool, "true"/"yes"/"1"
// strings, and non-zero numbers).
func toBool(raw any) bool {
	switch t := raw.(type) {
	case bool:
		return t
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		return s == "true" || s == "yes" || s == "1"
	case float64:
		return t != 0
	case int:
		return t != 0
	default:
		return raw != nil && valueString(raw) != ""
	}
}

// mergeFilterIntoConfig overlays the automation-wide filter (intents,
// min_confidence) onto an action node's config without clobbering node keys.
func mergeFilterIntoConfig(filter, config json.RawMessage) json.RawMessage {
	m := map[string]any{}
	if len(config) > 0 {
		_ = json.Unmarshal(config, &m)
	}
	if len(filter) > 0 {
		f := map[string]any{}
		if json.Unmarshal(filter, &f) == nil {
			for k, v := range f {
				if _, exists := m[k]; !exists {
					m[k] = v
				}
			}
		}
	}
	out, _ := json.Marshal(m)
	return out
}

// chanceHit returns true for ~pct% of distinct seeds, deterministically.
func chanceHit(seed string, pct float64) bool {
	if pct <= 0 {
		return false
	}
	if pct >= 100 {
		return true
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return float64(h.Sum32()%100) < pct
}

// valueString renders a condition value (from jsonb, so string/number/bool) as
// a comparable string.
func valueString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}
