package advanced

import (
	"context"

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
