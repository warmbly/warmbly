package advanced

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/models"
)

// FireCampaignEvent publishes a developer-defined custom event to the realtime
// gateway from a campaign "fire event" step. The event name + each field value
// are templated against the contact; the fields become the event payload.
// Subscribers (an API key with REALTIME_SUBSCRIBE on the org websocket) receive
// it with no public URL. Best-effort — a publish hiccup never blocks sending.
func (s *service) FireCampaignEvent(ctx context.Context, orgID uuid.UUID, sourceID, name string, fields []models.ActionKV, contact *models.Contact) {
	if orgID == uuid.Nil || contact == nil {
		return
	}
	evName := strings.TrimSpace(renderContactTemplate(name, contact))
	if evName == "" {
		return
	}
	payload := make(map[string]string, len(fields))
	for _, f := range fields {
		key := strings.TrimSpace(f.Key)
		if key == "" {
			continue
		}
		payload[key] = renderContactTemplate(f.Value, contact)
	}
	// Push to the gateway (websocket subscribers) and fan out to HTTP webhooks +
	// integrations as custom.event.
	if s.realtime != nil {
		s.realtime.PublishCustomEvent(ctx, orgID, uuid.Nil, evName, payload, "campaign", sourceID)
	}
	s.emit(ctx, orgID, models.WebhookEventCustom, map[string]any{
		"name":      evName,
		"payload":   payload,
		"source":    "campaign",
		"source_id": sourceID,
	})
}
