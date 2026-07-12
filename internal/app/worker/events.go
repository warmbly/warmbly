package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/warmbly/warmbly/internal/models"
)

type EventHandler[T any] func(ctx context.Context, event T) error

func (w *WorkerService) HandleEvent(ctx context.Context, event *models.WorkerEvent) error {
	resp, ok := w.eventHandlers[event.Type]
	if !ok {
		return errors.New("invalid event type")
	}
	return resp(ctx, event.Body)
}

func (w *WorkerService) InitEvents() {
	w.eventHandlers = make(map[models.WorkerEventType]func(ctx context.Context, body any) error)
	Register(w, models.WorkerEventTypeSendEmail, w.HandleSendEmail)
	Register(w, models.WorkerEventTypeAddEmail, w.HandleAddEmail)
	Register(w, models.WorkerEventTypeRemoveEmail, w.HandleRemoveEmail)
	Register(w, models.WorkerEventTypeEmailValidation, w.HandleEmailValidation)
	Register(w, models.WorkerEventTypeWarmupAction, w.HandleWarmupAction)
}

func Register[T any](w *WorkerService, eventType models.WorkerEventType, handler EventHandler[T]) {
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
