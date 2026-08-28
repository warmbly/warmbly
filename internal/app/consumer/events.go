package jobs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/warmbly/warmbly/internal/models"
)

type EventHandler[T any] func(ctx context.Context, event T) error

func (s *JobsService) HandleEvent(ctx context.Context, event *models.JobEvent) error {
	resp, ok := s.eventHandlers[event.Type]
	if !ok {
		// Ack rather than nak: an unregistered type is not a transient failure,
		// so redelivering it just burns the retry budget and fills the log on
		// every occurrence. Warn so a genuinely missing handler still surfaces.
		log.Warn().Str("event_type", string(event.Type)).
			Msg("no handler registered for job event type, dropping")
		return nil
	}
	return resp(ctx, event.Body)
}

func (w *JobsService) InitEvents() {
	w.eventHandlers = make(map[models.JobEventType]func(ctx context.Context, body any) error)
	Register(w, models.JobEventTypeNewEmail, w.HandleNewEmail)
	Register(w, models.JobEventTypeInboundBounce, w.HandleInboundBounce)
	Register(w, models.JobEventTypeInboundComplaint, w.HandleInboundComplaint)
	Register(w, models.JobEventTypeEmailUpdate, w.HandleUpdateEmail)
	Register(w, models.JobEventTypeRemoveEmail, w.HandleRemoveEmail)
	Register(w, models.JobEventTypeFlagsAdd, w.HandleFlagsAdd)
	Register(w, models.JobEventTypeFlagsRemove, w.HandleFlagsRemove)
	Register(w, models.JobEventTypeMailboxUpdate, w.HandleMailboxUpdate)
	Register(w, models.JobEventTypeMailboxDelete, w.HandleMailboxDelete)
	Register(w, models.JobEventTypeHistoryIDUpdate, w.HandleHistoryIDUpdate)
	Register(w, models.JobEventTypeGraphDeltaUpdate, w.HandleGraphDeltaUpdate)
	Register(w, models.JobEventTypeSyncState, w.HandleSyncState)
	Register(w, models.JobEventTypeTokenUpdate, w.HandleTokenUpdate)

	// Send outcomes. A worker reports every send it was handed; a failure
	// walks back what the control plane stamped at hand-off.
	Register(w, models.JobEventTypeEmailSent, w.HandleEmailSent)
	Register(w, models.JobEventTypeEmailFailed, w.HandleEmailFailed)

	// Email error handlers
	Register(w, models.JobEventTypeEmailAuthError, w.HandleEmailAuthError)
	Register(w, models.JobEventTypeEmailDisabled, w.HandleEmailDisabled)
	Register(w, models.JobEventTypeEmailRateLimited, w.HandleEmailRateLimited)
	Register(w, models.JobEventTypeEmailServerError, w.HandleEmailServerError)

	// Per-worker telemetry. Driver for worker_capacity_view.
	Register(w, models.JobEventTypeWorkerHealth, w.HandleWorkerHealth)
}

func Register[T any](w *JobsService, eventType models.JobEventType, handler EventHandler[T]) {
	w.eventHandlers[eventType] = func(ctx context.Context, body any) error {
		if data, ok := body.(T); ok {
			return handler(ctx, data)
		}
		// The JSON codec decodes the envelope's `body` into map[string]any;
		// round-trip it into the typed payload.
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("invalid event body for type %v: %w", eventType, err)
		}
		var data T
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("invalid event body for type %v: %w", eventType, err)
		}
		return handler(ctx, data)
	}
}
