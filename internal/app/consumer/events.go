package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

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
	RegisterNewEmail(w)
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

func RegisterNewEmail(w *JobsService) {
	w.eventHandlers[models.JobEventTypeNewEmail] = func(ctx context.Context, body any) error {
		e, err := w.normalizeNewEmailEvent(ctx, body)
		if err != nil {
			return err
		}
		return w.HandleNewEmail(ctx, e)
	}
}

func (w *JobsService) normalizeNewEmailEvent(ctx context.Context, body any) (*models.JobEventNewEmail, error) {
	if data, ok := body.(*models.JobEventNewEmail); ok {
		if data != nil && data.Message != nil {
			return data, nil
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("invalid event body for type %v: %w", models.JobEventTypeNewEmail, err)
	}

	var wrapped models.JobEventNewEmail
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Message != nil {
		return &wrapped, nil
	}

	// Backward compatibility for worker events emitted before NEW_EMAIL carried
	// its user_id wrapper. Those bodies were the message payload itself; recover
	// the owner from the mailbox id so already-published JetStream messages can
	// be ingested instead of redelivering forever.
	var msg models.EmailMessageStoreData
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, fmt.Errorf("invalid event body for type %v: %w", models.JobEventTypeNewEmail, err)
	}
	if msg.ID == uuid.Nil || msg.EmailID == uuid.Nil {
		return nil, fmt.Errorf("invalid event body for type %v: missing message", models.JobEventTypeNewEmail)
	}
	if w.EmailRepository == nil {
		return nil, fmt.Errorf("invalid legacy %v body: email repository is not configured", models.JobEventTypeNewEmail)
	}
	account, repoErr := w.EmailRepository.GetByID(ctx, msg.EmailID)
	if repoErr != nil {
		return nil, repoErr
	}
	if account == nil {
		return nil, fmt.Errorf("invalid legacy %v body: mailbox %s not found", models.JobEventTypeNewEmail, msg.EmailID)
	}
	userID, err := uuid.Parse(account.UserID)
	if err != nil {
		return nil, fmt.Errorf("invalid legacy %v body: mailbox %s has invalid user_id %q: %w", models.JobEventTypeNewEmail, msg.EmailID, account.UserID, err)
	}
	return &models.JobEventNewEmail{UserID: userID, Message: &msg}, nil
}
