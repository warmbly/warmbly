package advanced

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// EventDispatcher fans a platform event out to customer webhooks and, via the
// webhook dispatch sink, to third-party integration actions (Slack ping, CRM
// upsert). It is satisfied by *webhook.Service. Kept as a local interface so
// the advanced package stays decoupled from the webhook package (no import
// cycle, and the consumer/backend wire whichever dispatcher they construct).
type EventDispatcher interface {
	Dispatch(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data any) (uuid.UUID, error)
}

// WireDispatcher attaches the event dispatcher after construction. Done this
// way (rather than via the constructor) so the dispatcher — which itself may
// depend on services constructed later — can be supplied once the graph is
// fully wired. No-op if never called: emit() guards on a nil dispatcher.
func (s *service) WireDispatcher(d EventDispatcher) {
	s.dispatcher = d
}

// emit dispatches a platform event, best-effort. Reply detection runs in the
// consumer's hot path, so a webhook/integration hiccup must never block inbox
// ingest — failures are swallowed (Dispatch already logs its own).
func (s *service) emit(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data map[string]any) {
	if s.dispatcher == nil || orgID == uuid.Nil {
		return
	}
	_, _ = s.dispatcher.Dispatch(ctx, orgID, eventType, data)
}

// EmitCampaignEvent dispatches a campaign event (e.g. from a sequence "notify"
// action node) to customer webhooks and wired integrations. Best-effort — a
// dispatch hiccup must never block the sending pipeline.
func (s *service) EmitCampaignEvent(ctx context.Context, orgID uuid.UUID, eventType models.WebhookEventType, data map[string]any) {
	s.emit(ctx, orgID, eventType, data)
}

// ReplyRealtimePublisher pushes an org-scoped EMAIL_REPLIED pulse to the live
// dashboard. Satisfied by *pubsub.StreamingPublisher; primitive-typed local
// interface so this package stays decoupled from the pubsub event types.
type ReplyRealtimePublisher interface {
	PublishEmailReplied(ctx context.Context, orgID, userID, campaignID, contactID, contactEmail, sequenceID string)
	// PublishCustomEvent pushes a developer-defined "fire event" to the gateway so
	// API-key websocket subscribers receive it (the campaign "fire event" step).
	PublishCustomEvent(ctx context.Context, orgID, actorID uuid.UUID, name string, payload map[string]string, source, sourceID string)
}

// WireRealtime attaches the realtime publisher after construction. No-op if
// never called: the emit site guards on nil.
func (s *service) WireRealtime(p ReplyRealtimePublisher) {
	s.realtime = p
}

// Notifier raises a per-user in-app notification (gated by the user's prefs).
// Satisfied by *notification.Service. Local interface to avoid an import cycle;
// wired post-construction in the consumer (where reply/bounce/complaint run).
type Notifier interface {
	Notify(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any)
}

// WireNotifier attaches the notification service after construction.
func (s *service) WireNotifier(n Notifier) {
	s.notifier = n
}

// AutomationRunner launches an automation graph by id, so an instant
// "run_automation" action node (reply/open/click branch) can fire the same flow
// the scheduler runs at a step boundary. Satisfied by *integration.Service;
// kept as a local interface to avoid an import cycle (integration imports
// advanced) and wired post-construction once the integration service exists.
type AutomationRunner interface {
	RunAutomationByID(ctx context.Context, orgID, automationID uuid.UUID, data map[string]any) error
}

// WireAutomationRunner attaches the automation runner after construction. No-op
// if never called: the run_automation instant case guards on a nil runner.
func (s *service) WireAutomationRunner(r AutomationRunner) {
	s.automationRunner = r
}

// notify raises an in-app notification off the hot path. It detaches from the
// request context (the ingest call may return first) and is best-effort.
func (s *service) notify(userID uuid.UUID, orgID *uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any) {
	if s.notifier == nil || userID == uuid.Nil {
		return
	}
	// Detached + time-bounded so a slow DB can't accumulate notify goroutines.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.notifier.Notify(ctx, userID, orgID, category, title, body, link, meta)
	}()
}
