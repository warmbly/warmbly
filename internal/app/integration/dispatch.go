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
	"github.com/warmbly/warmbly/internal/repository"
)

// errReauthRequired marks an action failure caused by an unrecoverable token
// problem. accessTokenFor already flips the connection to reauth_required, so
// the dispatcher must not overwrite that status when it sees this error.
var errReauthRequired = errors.New("reauth required")

// Dispatch fans a platform event out to every matching event subscription.
// Targets are resolved synchronously (cheap, indexed) but the provider calls
// run on a detached context so the caller (an API handler or consumer) never
// blocks on a third-party round trip. Best-effort by design: a failing action
// is recorded on the connection's health and in a sync run, never propagated.
func (s *service) Dispatch(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data map[string]any) {
	targets, err := s.repo.MatchingDispatchTargets(ctx, orgID, string(eventType))
	if err != nil {
		log.Warn().Err(err).Str("event", string(eventType)).Msg("integration dispatch: failed to load targets")
		return
	}
	if len(targets) == 0 {
		return
	}
	go func(targets []repository.DispatchTarget) {
		bg, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for i := range targets {
			s.runAction(bg, targets[i], data)
		}
	}(targets)
}

// DispatchAny forwards a loosely-typed event payload to Dispatch when it is a
// map. Wired into the webhook fan-out sink so any event delivered to customer
// webhooks also drives integration actions.
func (s *service) DispatchAny(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data any) {
	if m, ok := data.(map[string]any); ok {
		s.Dispatch(ctx, orgID, eventType, m)
	}
}

func (s *service) runAction(ctx context.Context, target repository.DispatchTarget, data map[string]any) {
	sub := target.Subscription
	// Per-subscription filter (e.g. only "positive" replies, or a minimum
	// classifier confidence). Filtered-out events are a silent no-op — no sync
	// run, no connection-health churn.
	if !subscriptionMatchesFilter(sub, data) {
		return
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
		return
	}
	_ = s.repo.FinishSyncRun(ctx, run.ID, "success", "", 1)
	_ = s.repo.SetConnectionStatus(ctx, sub.ConnectionID, models.IntegrationStatusConnected, models.IntegrationHealthHealthy, "")
}

// execAction routes a subscription to its provider implementation.
func (s *service) execAction(ctx context.Context, target repository.DispatchTarget, data map[string]any) error {
	sub := target.Subscription
	cfg, err := s.openConfig(ctx, &target.Secrets)
	if err != nil {
		return fmt.Errorf("decrypt config: %w", err)
	}
	msg := renderEventMessage(sub, data)

	switch sub.Action {
	case models.IntegrationActionSlackNotify:
		channel := configString(sub.Config, "channel")
		token, terr := s.accessTokenFor(ctx, &target.Secrets)
		if terr != nil {
			return errReauthRequired
		}
		return slackPostMessage(ctx, token, channel, msg)

	case models.IntegrationActionDiscordNotify, models.IntegrationActionGenericWebhookPing:
		url := stringFromMap(cfg, "webhook_url")
		if url == "" {
			url = configString(sub.Config, "url")
		}
		if url == "" {
			return errors.New("no webhook url configured")
		}
		return webhookPost(ctx, url, msg)

	case models.IntegrationActionHubSpotUpsert:
		token, terr := s.accessTokenFor(ctx, &target.Secrets)
		if terr != nil {
			return errReauthRequired
		}
		return hubspotUpsertContact(ctx, token, data, msg)

	case models.IntegrationActionPipedriveUpsert:
		token, terr := s.accessTokenFor(ctx, &target.Secrets)
		if terr != nil {
			return errReauthRequired
		}
		return pipedriveUpsertPerson(ctx, token, data)

	default:
		return fmt.Errorf("unknown action: %s", sub.Action)
	}
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
		if intent := stringFromMap(data, "intent"); intent != "" {
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

// renderTemplate substitutes {{key}} placeholders with values from the event
// payload. Unknown placeholders render as empty so a typo can't leak braces.
func renderTemplate(tmpl string, data map[string]any) string {
	out := tmpl
	for {
		start := strings.Index(out, "{{")
		if start < 0 {
			break
		}
		end := strings.Index(out[start:], "}}")
		if end < 0 {
			break
		}
		end += start
		key := strings.TrimSpace(out[start+2 : end])
		val := stringFromMap(data, key)
		out = out[:start] + val + out[end+2:]
	}
	return strings.TrimSpace(out)
}

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
