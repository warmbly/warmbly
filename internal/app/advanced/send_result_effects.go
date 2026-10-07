package advanced

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/warmbly/warmbly/internal/models"

	"github.com/warmbly/warmbly/internal/repository"
)

func (s *service) DeliverSendResultEffects(ctx context.Context) error {
	r, ok := s.repo.(repository.SendResultEffectOutbox)
	if !ok {
		return errors.New("send result outbox unavailable")
	}
	return r.DeliverSendResultEffects(ctx, func(ctx context.Context, e repository.SendResultEffect) error {
		switch e.Kind {
		case "webhook":
			if s.dispatcher == nil {
				return errors.New("send result dispatcher unavailable")
			}
			_, err := s.dispatcher.Dispatch(ctx, e.OrganizationID, e.EventType, e.Data)
			return err
		case "notification":
			if s.notifier == nil {
				return errors.New("send result notifier unavailable")
			}
			durable, ok := s.notifier.(interface {
				NotifyDurable(context.Context, uuid.UUID, *uuid.UUID, models.NotificationCategory, string, string, string, map[string]any, string) error
			})
			if !ok {
				return errors.New("durable notifier unavailable")
			}
			return durable.NotifyDurable(ctx, e.UserID, &e.OrganizationID, e.Category, e.Title, e.Body, e.Link, e.Data, e.ID.String())
		default:
			return errors.New("unknown send result effect")
		}
	})
}
