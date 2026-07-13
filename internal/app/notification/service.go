// Package notification owns per-user notification preferences + the in-app feed.
// Notify() is the gated ingress: it checks the user's preference for a category
// and, when enabled, persists a feed row and pushes a realtime event. It is
// best-effort and must never fail the caller's hot path (it runs inside inbox
// ingest), so all errors are swallowed.
package notification

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
	"github.com/warmbly/warmbly/internal/notify/templates"
	"github.com/warmbly/warmbly/internal/repository"
)

// EmailSender delivers a notification to a user's account email. Satisfied by
// notify.EmailNotificationService.
type EmailSender interface {
	Send(ctx context.Context, to, cc, bcc []string, subject, message string) error
}

// SlackNotifier posts to the org's connected Slack. Satisfied by the
// integration service (NotifySlack).
type SlackNotifier interface {
	NotifySlack(ctx context.Context, orgID uuid.UUID, title, body string) error
}

// UserLookup resolves a user's email + name for email delivery. Satisfied by
// the user repository.
type UserLookup interface {
	GetUser(ctx context.Context, id uuid.UUID) (*models.User, error)
}

type Service interface {
	GetPreferences(ctx context.Context, userID uuid.UUID) (*models.NotificationPreferences, *errx.Error)
	UpdatePreferences(ctx context.Context, userID uuid.UUID, prefs *models.NotificationPreferences) *errx.Error
	List(ctx context.Context, userID uuid.UUID, limit int, unreadOnly bool) ([]models.Notification, *errx.Error)
	UnreadCount(ctx context.Context, userID uuid.UUID) (int, *errx.Error)
	MarkRead(ctx context.Context, userID, notifID uuid.UUID) *errx.Error
	MarkAllRead(ctx context.Context, userID uuid.UUID) *errx.Error

	// Notify is the gated ingress — best-effort, never errors out the caller.
	Notify(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any)

	// WireDelivery attaches the email + Slack + user-lookup dependencies for
	// the email/Slack channels (wired post-construction in both mains). Any
	// may be nil — the matching channel is then skipped.
	WireDelivery(email EmailSender, slack SlackNotifier, users UserLookup)

	// WirePush attaches APNs + device-token storage + the Redis digest window
	// and starts the digest loop. Any nil dependency leaves push disabled.
	WirePush(sender PushSender, tokens repository.DeviceTokenRepository, rdb *redis.Client)

	// RegisterDevice / UnregisterDevice manage the caller's APNs tokens.
	RegisterDevice(ctx context.Context, userID uuid.UUID, platform, token, environment string) error
	UnregisterDevice(ctx context.Context, userID uuid.UUID, token string) error
}

type service struct {
	repo      repository.NotificationRepository
	publisher *pubsub.StreamingPublisher
	email     EmailSender
	slack     SlackNotifier
	users     UserLookup

	push         PushSender
	deviceTokens repository.DeviceTokenRepository
	pushRedis    *redis.Client
}

func (s *service) WireDelivery(email EmailSender, slack SlackNotifier, users UserLookup) {
	s.email = email
	s.slack = slack
	s.users = users
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
	if !cat.Enabled {
		return // category off — no channel fires
	}

	// In-app: persist the feed row + push the realtime event.
	if cat.Channels.InApp {
		created, cerr := s.repo.Create(ctx, &models.Notification{
			UserID:         userID,
			OrganizationID: orgID,
			Category:       category,
			Title:          title,
			Body:           body,
			Link:           link,
			Metadata:       meta,
		})
		if cerr == nil && created != nil && s.publisher != nil {
			s.publisher.PublishNotificationCreated(ctx, userID.String(), created.ID.String(), string(category), title, link)
		}
	}

	// Email: deliver to the user's account email (detached, best-effort).
	if cat.Channels.Email && s.email != nil && s.users != nil {
		go s.deliverEmail(userID, title, body, link)
	}

	// Slack: post to the org's connected workspace (detached, best-effort).
	if cat.Channels.Slack && s.slack != nil && orgID != nil {
		org := *orgID
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			_ = s.slack.NotifySlack(ctx, org, title, body)
		}()
	}

	// Push: immediate on a quiet window, digest-batched inside one (detached).
	if cat.Channels.Push && s.push != nil && s.deviceTokens != nil && s.pushRedis != nil {
		go s.deliverPush(userID, category, title, body, link)
	}
}

// deliverEmail renders the notification on the shared transactional
// shell and emails it to the user. Title/body are auto-escaped by the
// template; a relative link is absolutized into the CTA button.
func (s *service) deliverEmail(userID uuid.UUID, title, body, link string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	user, err := s.users.GetUser(ctx, userID)
	if err != nil || user == nil || user.Email == "" {
		return
	}
	href := link
	if href != "" && href[0] == '/' {
		base := strings.TrimRight(os.Getenv("APP_URL"), "/")
		if base == "" {
			base = "https://app.warmbly.com"
		}
		href = base + href
	}
	html, gerr := templates.GenerateNotificationHTML(title, body, href, "")
	if gerr != nil {
		return
	}
	_ = s.email.Send(ctx, []string{user.Email}, nil, nil, title, html)
}
