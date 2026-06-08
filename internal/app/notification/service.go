// Package notification owns per-user notification preferences + the in-app feed.
// Notify() is the gated ingress: it checks the user's preference for a category
// and, when enabled, persists a feed row and pushes a realtime event. It is
// best-effort and must never fail the caller's hot path (it runs inside inbox
// ingest), so all errors are swallowed.
package notification

import (
	"context"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/repository"
)

type Service interface {
	GetPreferences(ctx context.Context, userID uuid.UUID) (*models.NotificationPreferences, *errx.Error)
	UpdatePreferences(ctx context.Context, userID uuid.UUID, prefs *models.NotificationPreferences) *errx.Error
	List(ctx context.Context, userID uuid.UUID, limit int, unreadOnly bool) ([]models.Notification, *errx.Error)
	UnreadCount(ctx context.Context, userID uuid.UUID) (int, *errx.Error)
	MarkRead(ctx context.Context, userID, notifID uuid.UUID) *errx.Error
	MarkAllRead(ctx context.Context, userID uuid.UUID) *errx.Error

	// Notify is the gated ingress — best-effort, never errors out the caller.
	Notify(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any)
}

type service struct {
	repo      repository.NotificationRepository
	publisher *pubsub.StreamingPublisher
}

func NewService(repo repository.NotificationRepository, publisher *pubsub.StreamingPublisher) Service {
	return &service{repo: repo, publisher: publisher}
}

func (s *service) GetPreferences(ctx context.Context, userID uuid.UUID) (*models.NotificationPreferences, *errx.Error) {
	p, err := s.repo.GetPreferences(ctx, userID)
	if err != nil {
		return nil, errx.InternalError()
	}
	return p, nil
}

func (s *service) UpdatePreferences(ctx context.Context, userID uuid.UUID, prefs *models.NotificationPreferences) *errx.Error {
	if err := s.repo.UpdatePreferences(ctx, userID, prefs); err != nil {
		return errx.InternalError()
	}
	return nil
}

func (s *service) List(ctx context.Context, userID uuid.UUID, limit int, unreadOnly bool) ([]models.Notification, *errx.Error) {
	out, err := s.repo.List(ctx, userID, limit, unreadOnly)
	if err != nil {
		return nil, errx.InternalError()
	}
	return out, nil
}

func (s *service) UnreadCount(ctx context.Context, userID uuid.UUID) (int, *errx.Error) {
	c, err := s.repo.CountUnread(ctx, userID)
	if err != nil {
		return 0, errx.InternalError()
	}
	return c, nil
}

func (s *service) MarkRead(ctx context.Context, userID, notifID uuid.UUID) *errx.Error {
	if err := s.repo.MarkRead(ctx, userID, notifID); err != nil {
		return errx.InternalError()
	}
	return nil
}

func (s *service) MarkAllRead(ctx context.Context, userID uuid.UUID) *errx.Error {
	if err := s.repo.MarkAllRead(ctx, userID); err != nil {
		return errx.InternalError()
	}
	return nil
}

// Notify checks the user's preference for category and, when in-app is enabled,
// persists a feed row + pushes a realtime event. Silent on any miss/error.
func (s *service) Notify(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any) {
	if s == nil || userID == uuid.Nil {
		return
	}
	prefs, err := s.repo.GetPreferences(ctx, userID)
	if err != nil || prefs == nil {
		return
	}
	cat := prefs.CategoryPref(category)
	if !cat.Enabled || !cat.Channels.InApp {
		return // the gate
	}
	created, cerr := s.repo.Create(ctx, &models.Notification{
		UserID:         userID,
		OrganizationID: orgID,
		Category:       category,
		Title:          title,
		Body:           body,
		Link:           link,
		Metadata:       meta,
	})
	if cerr != nil || created == nil {
		return
	}
	if s.publisher != nil {
		s.publisher.PublishNotificationCreated(ctx, userID.String(), created.ID.String(), string(category), title, link)
	}
}
