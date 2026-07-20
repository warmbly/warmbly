// Package notification owns per-user notification preferences + the in-app feed.
// Notify() is the gated ingress: it checks the user's preference for a category
// and, when enabled, persists a feed row and pushes a realtime event. It is
// best-effort and must never fail the caller's hot path (it runs inside inbox
// ingest), so all errors are swallowed.
package notification

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/warmbly/warmbly/internal/errx"
	"github.com/warmbly/warmbly/internal/infrastructure/pubsub"
	"github.com/warmbly/warmbly/internal/models"
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

// MemberLookup enumerates an org's members so NotifyOrg can resolve a
// permission-targeted audience. Satisfied by the organization repository.
type MemberLookup interface {
	GetMembers(ctx context.Context, orgID uuid.UUID) ([]models.OrganizationMember, error)
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

	// NotifyOrg raises the same notification for every accepted org member
	// holding perm (never the whole org blindly), excluding exclude when set.
	// Rows share groupKey, so the email flush coalesces the event into one
	// message with every recipient in To; Slack fires at most once. Each
	// member's own preferences still gate their channels. Best-effort.
	NotifyOrg(ctx context.Context, orgID uuid.UUID, perm models.OrganizationPermission, exclude uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any, groupKey string)

	// WireDelivery attaches the email + Slack + user/member-lookup
	// dependencies (wired post-construction in both mains) and starts the
	// email digest flush loop when the email channel is deliverable. Any
	// dependency may be nil — the matching channel is then skipped.
	WireDelivery(email EmailSender, slack SlackNotifier, users UserLookup, members MemberLookup)

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
	members   MemberLookup

	push         PushSender
	deviceTokens repository.DeviceTokenRepository
	pushRedis    *redis.Client
}

func (s *service) WireDelivery(email EmailSender, slack SlackNotifier, users UserLookup, members MemberLookup) {
	s.email = email
	s.slack = slack
	s.users = users
	s.members = members
	if s.email != nil && s.users != nil {
		go s.emailFlushLoop()
	}
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

// Notify checks the user's preference for category and fans the enabled
// channels. Silent on any miss/error.
func (s *service) Notify(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any) {
	if s == nil {
		return
	}
	s.notifyOne(ctx, userID, orgID, category, title, body, link, meta, "", false)
}

// NotifyOrg resolves the permission-targeted audience and raises the
// notification for each member. Slack posts to one org workspace, so it fires
// for the first member whose prefs allow it and stays suppressed for the rest.
func (s *service) NotifyOrg(ctx context.Context, orgID uuid.UUID, perm models.OrganizationPermission, exclude uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any, groupKey string) {
	if s == nil || s.members == nil || orgID == uuid.Nil {
		return
	}
	members, err := s.members.GetMembers(ctx, orgID)
	if err != nil {
		return
	}
	org := orgID
	slackFired := false
	for _, m := range members {
		if m.AcceptedAt == nil || m.UserID == uuid.Nil || m.UserID == exclude {
			continue
		}
		if perm != 0 && !m.Permissions.HasPermission(perm) {
			continue
		}
		fired := s.notifyOne(ctx, m.UserID, &org, category, title, body, link, meta, groupKey, slackFired)
		slackFired = slackFired || fired
	}
}

// notifyOne is the per-user ingress behind Notify/NotifyOrg. The feed row is
// the delivery record for both the in-app and email channels: email-channel
// rows queue as pending with a due time from the user's digest cadence, and
// the flush loop bundles them later (see email.go). Returns whether the Slack
// channel fired, so org fan-outs post to the shared workspace only once.
func (s *service) notifyOne(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID, category models.NotificationCategory, title, body, link string, meta map[string]any, groupKey string, suppressSlack bool) bool {
	if userID == uuid.Nil {
		return false
	}
	prefs, err := s.repo.GetPreferences(ctx, userID)
	if err != nil || prefs == nil {
		return false
	}
	cat := prefs.CategoryPref(category)
	if !cat.Enabled {
		return false // category off — no channel fires
	}

	emailOn := cat.Channels.Email && s.email != nil && s.users != nil
	if cat.Channels.InApp || emailOn {
		n := &models.Notification{
			UserID:         userID,
			OrganizationID: orgID,
			Category:       category,
			Title:          title,
			Body:           body,
			Link:           link,
			Metadata:       meta,
			GroupKey:       groupKey,
			// In-app off but email on: keep the row as the email record
			// without ringing the bell.
			PreRead: !cat.Channels.InApp,
		}
		if emailOn {
			due := time.Now().Add(emailHold(prefs.EmailDigest, category))
			n.EmailState = "pending"
			n.EmailDueAt = &due
		}
		created, cerr := s.repo.Create(ctx, n)
		if cerr == nil && created != nil && cat.Channels.InApp && s.publisher != nil {
			s.publisher.PublishNotificationCreated(ctx, userID.String(), created.ID.String(), string(category), title, link)
		}
	}

	// Slack: post to the org's connected workspace (detached, best-effort).
	slackFired := false
	if cat.Channels.Slack && !suppressSlack && s.slack != nil && orgID != nil {
		slackFired = true
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
	return slackFired
}
