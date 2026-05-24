package jobs

import (
	"context"
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
	Register(w, models.JobEventTypeEmailUpdate, w.HandleUpdateEmail)
	Register(w, models.JobEventTypeFlagsAdd, w.HandleFlagsAdd)
	Register(w, models.JobEventTypeFlagsRemove, w.HandleFlagsRemove)
	Register(w, models.JobEventTypeMailboxUpdate, w.HandleMailboxUpdate)
	Register(w, models.JobEventTypeMailboxDelete, w.HandleMailboxDelete)
	Register(w, models.JobEventTypeHistoryIDUpdate, w.HandleHistoryIDUpdate)
	Register(w, models.JobEventTypeTokenUpdate, w.HandleTokenUpdate)

	// Email error handlers
	Register(w, models.JobEventTypeEmailAuthError, w.HandleEmailAuthError)
	Register(w, models.JobEventTypeEmailDisabled, w.HandleEmailDisabled)
	Register(w, models.JobEventTypeEmailRateLimited, w.HandleEmailRateLimited)
	Register(w, models.JobEventTypeEmailServerError, w.HandleEmailServerError)
}

func Register[T any](w *JobsService, eventType models.JobEventType, handler EventHandler[T]) {
	w.eventHandlers[eventType] = func(ctx context.Context, body any) error {
		data, ok := body.(T)
		if !ok {
			return fmt.Errorf("invalid event body for type %v", eventType)
		}
		return handler(ctx, data)
	}
}
