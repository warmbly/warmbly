package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/warmbly/warmbly/internal/models"
)

type EventHandler[T any] func(ctx context.Context, event T) error

func (s *JobsService) HandleEvent(ctx context.Context, event *models.JobEvent) error {
	resp, ok := s.eventHandlers[event.Type]
	if !ok {
		return errors.New("invalid event type")
	}
	return resp(ctx, event.Body)
}

func (w *JobsService) InitEvents() {
	w.eventHandlers = make(map[models.JobEventType]func(ctx context.Context, body any) error)
	Register(w, models.JobEventTypeNewEmail, w.HandleNewEmail)
	Register(w, models.JobEventTypeInboundBounce, w.HandleInboundBounce)
	Register(w, models.JobEventTypeEmailUpdate, w.HandleUpdateEmail)
	Register(w, models.JobEventTypeRemoveEmail, w.HandleRemoveEmail)
	Register(w, models.JobEventTypeFlagsAdd, w.HandleFlagsAdd)
	Register(w, models.JobEventTypeFlagsRemove, w.HandleFlagsRemove)
	Register(w, models.JobEventTypeMailboxUpdate, w.HandleMailboxUpdate)
	Register(w, models.JobEventTypeMailboxDelete, w.HandleMailboxDelete)
	Register(w, models.JobEventTypeHistoryIDUpdate, w.HandleHistoryIDUpdate)
	Register(w, models.JobEventTypeGraphDeltaUpdate, w.HandleGraphDeltaUpdate)
	Register(w, models.JobEventTypeTokenUpdate, w.HandleTokenUpdate)

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
