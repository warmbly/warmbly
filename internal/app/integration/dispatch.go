package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/pkg/tmplfuncs"
	"github.com/warmbly/warmbly/internal/repository"
)

// errReauthRequired marks an action failure caused by an unrecoverable token
// problem. accessTokenFor already flips the connection to reauth_required, so
// the dispatcher must not overwrite that status when it sees this error.
var errReauthRequired = errors.New("reauth required")

// ErrActionProviderMismatch means the action does not run on the connection's provider.
var ErrActionProviderMismatch = errors.New("this action does not run on the selected integration")

// Dispatch fans a platform event out to every matching event subscription.
// Targets are resolved synchronously (cheap, indexed) but the provider calls
// run on a detached context so the caller (an API handler or consumer) never
// blocks on a third-party round trip. Best-effort by design: a failing action
// is recorded on the connection's health and in a sync run, never propagated.
func (s *service) Dispatch(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data map[string]any) {
	// Legacy standalone subscriptions (automation_id IS NULL) + the branching
	// automations whose trigger matches. The two paths are disjoint by design
	// (MatchingDispatchTargets excludes automation-owned rows), so nothing
	// double-fires.
	targets, err := s.repo.MatchingDispatchTargets(ctx, orgID, string(eventType))
	if err != nil {
		log.Warn().Err(err).Str("event", string(eventType)).Msg("integration dispatch: failed to load targets")
	}
	// Every run announces automation.run, so starting a flow on it would loop.
	var autos []models.Automation
	if eventType != models.WebhookEventAutomationRun {
		autos, err = s.repo.ListEnabledAutomationsForEvent(ctx, orgID, string(eventType))
		if err != nil {
			log.Warn().Err(err).Str("event", string(eventType)).Msg("integration dispatch: failed to load automations")
		}
	}
	// An event raised by an automation's own action (a contact it created, a
	// deal it opened) carries its depth. Past the chain cap it still reaches
	// customer webhooks and legacy subscriptions, but runs no more flows, so
	// "on new contact, create a contact" cannot loop.
	if depth := int(toFloat(data[automationDepthKey])); depth >= maxAutomationChainDepth && len(autos) > 0 {
		log.Warn().Str("event", string(eventType)).Int("depth", depth).Msg("integration dispatch: automation chain depth limit reached; not running automations")
		autos = nil
	}
	if len(targets) == 0 && len(autos) == 0 {
		return
	}
	go func(targets []repository.DispatchTarget, autos []models.Automation) {
		bg, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for i := range targets {
			s.runAction(bg, targets[i], data)
		}
		for i := range autos {
			s.executeAutomationGraph(bg, autos[i], string(eventType), data)
		}
	}(targets, autos)
}

// DispatchAny forwards a loosely-typed event payload to Dispatch when it is a
// map. Wired into the webhook fan-out sink so any event delivered to customer
// webhooks also drives integration actions.
func (s *service) DispatchAny(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data any) {
	if m, ok := data.(map[string]any); ok {
		s.Dispatch(ctx, orgID, eventType, m)
	}
}

// runAction executes one connection-backed action, logging a sync run and
// updating connection health. It returns the action error (nil on success or a
// filtered-out no-op) so callers like the automation executor can record the
// per-node outcome; the legacy dispatch loop ignores it.
func (s *service) runAction(ctx context.Context, target repository.DispatchTarget, data map[string]any) error {
	sub := target.Subscription
	// Per-subscription filter (e.g. only "positive" replies, or a minimum
	// classifier confidence). Filtered-out events are a silent no-op — no sync
	// run, no connection-health churn.
	if !subscriptionMatchesFilter(sub, data) {
		return nil
	}
	run := &models.IntegrationSyncRun{
		ConnectionID:   sub.ConnectionID,
		OrganizationID: sub.OrganizationID,
		Kind:           "event_dispatch",
		Detail:         fmt.Sprintf("%s on %s", sub.Action, sub.EventType),
	}
	_ = s.repo.CreateSyncRun(ctx, run)

	if err := s.execAction(ctx, target, data); err != nil {
		_ = s.repo.FinishSyncRun(ctx, run.ID, "error", truncate(err.Error(), 480), 0)
		// Don't clobber a reauth_required status the token path already set.
		if !errors.Is(err, errReauthRequired) {
			_ = s.repo.SetConnectionStatus(ctx, sub.ConnectionID, models.IntegrationStatusConnected, models.IntegrationHealthDegraded, truncate(err.Error(), 480))
		}
		log.Warn().Err(err).Str("action", string(sub.Action)).Str("connection", sub.ConnectionID.String()).Msg("integration action failed")
		return err
	}
	_ = s.repo.FinishSyncRun(ctx, run.ID, "success", "", 1)
	_ = s.repo.SetConnectionStatus(ctx, sub.ConnectionID, models.IntegrationStatusConnected, models.IntegrationHealthHealthy, "")
	return nil
}

// execAction routes a subscription to its provider implementation, projecting
// the event payload through the connection's configured field map for CRM
// upserts so behaviour follows the user's configuration instead of a fixed shape.
func (s *service) execAction(ctx context.Context, target repository.DispatchTarget, data map[string]any) error {
	sub := target.Subscription
	// A connection's credentials only ever reach its own provider.
	if !models.ProviderSupportsAction(target.Secrets.Conn.Provider, sub.Action) {
		return ErrActionProviderMismatch
	}
	secretCfg, err := s.openConfig(ctx, &target.Secrets)
	if err != nil {
		return fmt.Errorf("decrypt config: %w", err)
	}
	msg := renderEventMessage(sub, data)
	autoCfg, _ := models.ParseAutomationConfig(sub.Config)

	switch sub.Action {
	case models.IntegrationActionSlackNotify:
		channel := renderTemplate(configString(sub.Config, "channel"), data)
		token, terr := s.accessTokenFor(ctx, &target.Secrets)
		if terr != nil {
			return errReauthRequired
		}
		return slackPostMessage(ctx, token, channel, msg)

	case models.IntegrationActionDiscordNotify:
		url := stringFromMap(secretCfg, "webhook_url")
		if url == "" {
			url = configString(sub.Config, "url")
		}
		if url == "" {
			return errors.New("no webhook url configured")
		}
		// Render + SSRF re-validate EVERY outbound URL (sealed connection url or
		// templatable action url) right before the request — the single guard.
		rendered, rerr := renderOutboundURL(url, data)
		if rerr != nil {
			return rerr
		}
		return discordNotify(ctx, rendered, msg)

	case models.IntegrationActionGenericWebhookPing:
		url := stringFromMap(secretCfg, "webhook_url")
		if url == "" {
			url = configString(sub.Config, "url")
		}
		if url == "" {
			return errors.New("no webhook url configured")
		}
		rendered, rerr := renderOutboundURL(url, data)
		if rerr != nil {
			return rerr
		}
		url = rendered
		// Automation tools (Zapier/Make/n8n) get the full structured + signed
		// payload; the signing secret is the connection's (empty => unsigned).
		secret := s.signingSecretFor(ctx, &target.Secrets, secretCfg)
		return automationDeliver(ctx, url, secret, sub.EventType, buildAutomationPayload(sub, data, msg))

	case models.IntegrationActionClaySendRow:
		return s.clayDeliver(ctx, sub, secretCfg, data, msg)

	case models.IntegrationActionHubSpotUpsert:
		// While HubSpot is the workspace CRM, every send, reply and contact
		// syncs on its own; this action would only add duplicate notes.
		if s.crmMode != nil && s.crmMode(ctx, target.Secrets.Conn.OrganizationID) == models.CRMProviderHubSpot {
			return nil
		}
		token, terr := s.accessTokenFor(ctx, &target.Secrets)
		if terr != nil {
			return errReauthRequired
		}
		props := s.crmProps(ctx, sub, models.IntegrationHubSpot, data, autoCfg)
		return hubspotUpsertContact(ctx, token, contactEmail(data), props, msg.plainText())

	case models.IntegrationActionPipedriveUpsert:
		// In Pipedrive mode people are created and kept current by the sync.
		if s.crmMode != nil && s.crmMode(ctx, target.Secrets.Conn.OrganizationID) == models.CRMProviderPipedrive {
			return nil
		}
		token, terr := s.accessTokenFor(ctx, &target.Secrets)
		if terr != nil {
			return errReauthRequired
		}
		props := s.crmProps(ctx, sub, models.IntegrationPipedrive, data, autoCfg)
		return pipedriveUpsertPerson(ctx, pipedriveBase(&target.Secrets.Conn), token, contactEmail(data), props)

	case models.IntegrationActionSalesforceUpsert:
		// Salesforce writes go through the native sync only.
		if s.salesforce == nil {
			return errors.New("salesforce sync is not available on this process")
		}
		ev := map[string]any{}
		for k, v := range data {
			ev[k] = v
		}
		// An automation's own field map overrides the connection's rules.
		if len(autoCfg.FieldMap) > 0 {
			ev["_salesforce_fields"] = projectFields(autoCfg.FieldMap, eventSource(data))
		}
		if err := s.salesforce.UpsertFromEvent(ctx, sub.OrganizationID, sub.ConnectionID, ev); err != nil {
			if errors.Is(err, ErrPushReauth) {
				return errReauthRequired
			}
			return err
		}
		return nil

	case models.IntegrationActionCloseUpsert:
		apiKey := stringFromMap(secretCfg, "api_key", "api_token")
		if apiKey == "" {
			return errors.New("no close api key configured")
		}
		props := s.crmProps(ctx, sub, models.IntegrationClose, data, autoCfg)
		return closeUpsertLead(ctx, apiKey, contactEmail(data), props)

	default:
		return fmt.Errorf("unknown action: %s", sub.Action)
	}
}

// crmProps resolves the effective field map for a CRM upsert (provider defaults
// < connection-default rows < subscription rows < inline config) and projects
// the event payload into provider properties.
func (s *service) crmProps(ctx context.Context, sub models.IntegrationEventSubscription, provider models.IntegrationProvider, data map[string]any, autoCfg models.AutomationConfig) map[string]any {
	rows, _ := s.repo.ListFieldMappings(ctx, sub.OrganizationID, sub.ConnectionID)
	object := defaultObject(provider)
	fm := effectiveFieldMap(provider, object, rows, sub.ID.String(), autoCfg.FieldMap)
	return projectFields(fm, eventSource(data))
}

// subscriptionMatchesFilter applies the optional, user-defined filters stored
// in a subscription's config so automations are fully customizable:
//
//   - intents: []string  — only fire when data["intent"] is in this set
//     (e.g. ["positive"] => "only notify me on positive replies").
//   - min_confidence: number (0..1) — only fire when the classifier
//     confidence meets this floor.
//
// No filters configured => always matches.
func subscriptionMatchesFilter(sub models.IntegrationEventSubscription, data map[string]any) bool {
	cfg := map[string]any{}
	if len(sub.Config) > 0 {
		_ = json.Unmarshal(sub.Config, &cfg)
	}

	if raw, ok := cfg["intents"]; ok {
		wanted := toStringSet(raw)
		if len(wanted) > 0 {
			got := strings.ToLower(stringFromMap(data, "intent"))
			if got == "" || !wanted[got] {
				return false
			}
		}
	}

	if raw, ok := cfg["min_confidence"]; ok {
		floor := toFloat(raw)
		if floor > 0 {
			if got, ok := data["confidence"]; ok {
				if toFloat(got) < floor {
					return false
				}
			}
		}
	}

	return true
}

// eventMessage is the normalized, human-readable summary an action renders.
// Custom is the rendered user-supplied template; when set it overrides the
// auto-generated Title/Detail text in notifications.
type eventMessage struct {
	Title   string
	Email   string
	Subject string
	Detail  string
	Custom  string
}

func renderEventMessage(sub models.IntegrationEventSubscription, data map[string]any) eventMessage {
	eventType := models.WebhookEventType(sub.EventType)
	email := stringFromMap(data, "contact_email", "invitee_email", "email", "recipient")
	subject := stringFromMap(data, "subject", "event_name", "campaign_name", "campaign")
	m := eventMessage{Email: email, Subject: subject}

	switch eventType {
	case models.WebhookEventCampaignReplyReceived:
		m.Title = "📨 New reply"
		// The inbox-tagging labels, when present, are what the inbox shows.
		if labels, _ := tmplfuncs.ListStrings(data["ai_labels"]); len(labels) > 0 {
			m.Title = "📨 New reply (" + strings.Join(labels, ", ") + ")"
		} else if intent := stringFromMap(data, "intent"); intent != "" {
			m.Title = "📨 New reply (" + intent + ")"
		}
	case models.WebhookEventCampaignEmailBounced, models.WebhookEventDeliverabilityBounce:
		m.Title = "⚠️ Email bounced"
	case models.WebhookEventCampaignUnsubscribed:
		m.Title = "🚫 Unsubscribe"
	case models.WebhookEventWarmupHealthChanged:
		m.Title = "🌡️ Warmup health changed"
	case models.WebhookEventDeliverabilityComplaint:
		m.Title = "❗ Spam complaint"
	case models.WebhookEventMeetingBooked:
		m.Title = "📅 Meeting booked"
	case models.WebhookEventMeetingRescheduled:
		m.Title = "🔁 Meeting rescheduled"
	case models.WebhookEventMeetingCanceled:
		m.Title = "❌ Meeting canceled"
	case models.WebhookEventCampaignEmailSent:
		m.Title = "✉️ Email sent"
	case models.WebhookEventCampaignEmailOpened:
		m.Title = "👀 Email opened"
	case models.WebhookEventCampaignEmailClicked:
		m.Title = "🔗 Link clicked"
	case models.WebhookEventCampaignStarted:
		m.Title = "▶️ Campaign started"
	case models.WebhookEventCampaignPaused:
		m.Title = "⏸️ Campaign paused"
	case models.WebhookEventCampaignCompleted:
		m.Title = "✅ Campaign completed"
	case models.WebhookEventContactCreated:
		m.Title = "🧑 Contact created"
	case models.WebhookEventFormSubmitted:
		m.Title = "📝 Form submitted"
		if n := stringFromMap(data, "form_name"); n != "" {
			m.Title = "📝 Form submitted: " + n
		}
	case models.WebhookEventContactUpdated:
		m.Title = "✏️ Contact updated"
	case models.WebhookEventCRMDealCreated:
		m.Title = "💼 Deal created"
	case models.WebhookEventCRMDealUpdated:
		m.Title = "💼 Deal updated"
	case models.WebhookEventInboxEmailReceived:
		m.Title = "📥 New email received"
	case models.WebhookEventWarmupPlacementInSpam:
		m.Title = "🧯 Warmup landed in spam"
	case models.WebhookEventWarmupQuarantined:
		m.Title = "🚧 Mailbox quarantined"
	case models.WebhookEventWarmupBlocked:
		m.Title = "⛔ Mailbox blocked from warmup"
	case models.WebhookEventCustom:
		if n := stringFromMap(data, "name"); n != "" {
			m.Title = "⚡ " + n
		} else {
			m.Title = "⚡ Custom event"
		}
	default:
		m.Title = "Warmbly event: " + string(eventType)
	}

	var parts []string
	if email != "" {
		parts = append(parts, email)
	}
	if subject != "" {
		parts = append(parts, subject)
	}
	m.Detail = strings.Join(parts, " · ")

	// Optional custom template: "{{contact_email}} replied to {{subject}}".
	// Placeholders are any key present in the event payload.
	if tmpl := configString(sub.Config, "message_template"); tmpl != "" {
		m.Custom = renderTemplate(tmpl, data)
	}
	return m
}

func (m eventMessage) plainText() string {
	if m.Custom != "" {
		return m.Custom
	}
	if m.Detail == "" {
		return m.Title
	}
	return m.Title + " — " + m.Detail
}

// buildAutomationPayload assembles the structured webhook body for a generic
// automation delivery: a stable delivery id, the event discriminator, the full
// structured event data, and the legacy flat fields for backward compatibility.
func buildAutomationPayload(sub models.IntegrationEventSubscription, data map[string]any, msg eventMessage) automationEventPayload {
	if data == nil {
		data = map[string]any{}
	}
	return automationEventPayload{
		ID:        newDeliveryID(),
		Event:     sub.EventType,
		Version:   "1",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Data:      publicEventData(data),
		Content:   msg.plainText(),
		Email:     msg.Email,
		Subject:   msg.Subject,
		Title:     msg.Title,
	}
}

// publicEventData strips internal, underscore-prefixed keys (e.g. the
// _automation_depth re-entrancy counter) from the outbound webhook body so
// engine bookkeeping never leaks to a customer's receiver. Intentionally
// forwarded keys like idempotency_key (no underscore) are kept. Returns the input
// unchanged when there is nothing internal to strip (the common case).
func publicEventData(data map[string]any) map[string]any {
	hasInternal := false
	for k := range data {
		if strings.HasPrefix(k, "_") {
			hasInternal = true
			break
		}
	}
	if !hasInternal {
		return data
	}
	out := make(map[string]any, len(data))
	for k, v := range data {
		if strings.HasPrefix(k, "_") {
			continue
		}
		out[k] = v
	}
	return out
}

// renderTemplate substitutes {{key}} placeholders with values from the event
// payload. Unknown placeholders render as empty so a typo can't leak braces.

func configString(raw json.RawMessage, key string) string {
	if len(raw) == 0 {
		return ""
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	return stringFromMap(m, key)
}

func stringFromMap(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case string:
				if strings.TrimSpace(t) != "" {
					return strings.TrimSpace(t)
				}
			case float64:
				return strconv.FormatFloat(t, 'f', -1, 64)
			case bool:
				return strconv.FormatBool(t)
			}
		}
	}
	return ""
}

func toStringSet(raw any) map[string]bool {
	out := map[string]bool{}
	switch t := raw.(type) {
	case []any:
		for _, v := range t {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				out[strings.ToLower(strings.TrimSpace(s))] = true
			}
		}
	case []string:
		for _, s := range t {
			if strings.TrimSpace(s) != "" {
				out[strings.ToLower(strings.TrimSpace(s))] = true
			}
		}
	case string:
		for _, s := range strings.Split(t, ",") {
			if strings.TrimSpace(s) != "" {
				out[strings.ToLower(strings.TrimSpace(s))] = true
			}
		}
	}
	return out
}

func toFloat(raw any) float64 {
	switch t := raw.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(t), 64); err == nil {
			return f
		}
	}
	return 0
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
